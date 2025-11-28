package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/tinyrange/cc/internal/asm"
	"github.com/tinyrange/cc/internal/asm/amd64"
	"github.com/tinyrange/cc/internal/asm/arm64"
	"github.com/tinyrange/cc/internal/devices/amd64/serial"
	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/hv/factory"
	"github.com/tinyrange/cc/internal/hv/helpers"
	"github.com/tinyrange/cc/internal/ir"
	amd64ir "github.com/tinyrange/cc/internal/ir/amd64"
	arm64ir "github.com/tinyrange/cc/internal/ir/arm64"
	"github.com/tinyrange/cc/internal/linux/boot"
	amd64defs "github.com/tinyrange/cc/internal/linux/defs/amd64"
)

const (
	psciSystemOff   = 0x84000008
	arm64MMIOAddr   = 0xdead0000
	arm64MessageBuf = 0x2000
)

const (
	arm64VectorEntrySize  = 0x80
	arm64VectorEntryCount = 16
	arm64NopEncoding      = 0xd503201f
)

var arm64VectorTableBytes = mustBuildArm64VectorTable()

type bringUpQuest struct {
	dev hv.Hypervisor
}

func arm64ExceptionVectorInit(loader *helpers.ProgramLoader) {
	if loader == nil {
		panic("arm64ExceptionVectorInit requires a loader")
	}
	loader.Arm64ExceptionVectors = &helpers.Arm64ExceptionVectorConfig{
		Table: arm64VectorTableBytes,
		Align: arm64VectorEntrySize * arm64VectorEntryCount,
	}
}

func mustBuildArm64VectorTable() []byte {
	handlerBytes, err := arm64.EmitBytes(arm64VectorHandlerFragment())
	if err != nil {
		panic(fmt.Errorf("build arm64 vector handler: %w", err))
	}
	if len(handlerBytes) > arm64VectorEntrySize {
		panic(fmt.Errorf("arm64 vector handler too large (%d bytes)", len(handlerBytes)))
	}

	entry := make([]byte, arm64VectorEntrySize)
	copy(entry, handlerBytes)
	for off := len(handlerBytes); off < arm64VectorEntrySize; off += 4 {
		binary.LittleEndian.PutUint32(entry[off:], arm64NopEncoding)
	}

	table := make([]byte, arm64VectorEntrySize*arm64VectorEntryCount)
	for i := range arm64VectorEntryCount {
		copy(table[i*arm64VectorEntrySize:], entry)
	}
	return table
}

func arm64VectorHandlerFragment() asm.Fragment {
	handlerLoop := asm.Label("arm64_vector_loop")
	return asm.Group{
		asm.MarkLabel(handlerLoop),
		arm64.MovImmediate(arm64.Reg64(arm64.X0), psciSystemOff),
		arm64.Hvc(),
		arm64.Jump(handlerLoop),
	}
}

func (q *bringUpQuest) runTask(name string, f func() error) error {
	if err := f(); err != nil {
		slog.Error("Task failed", "name", name, "error", err)
		return err
	}

	return nil
}

func (q *bringUpQuest) runArchitectureTask(name string, arch hv.CpuArchitecture, f func() error) error {
	if q.dev == nil {
		return fmt.Errorf("hypervisor device not initialized")
	}

	if arch != q.dev.Architecture() {
		return nil
	}

	return q.runTask(fmt.Sprintf("%s (%s)", name, arch), f)
}

func (q *bringUpQuest) runVMTask(
	name string,
	arch hv.CpuArchitecture,
	prog ir.Program,
	f func(cpu hv.VirtualCPU) error,
	devs ...hv.Device,
) error {
	return q.runArchitectureTask(name, arch, func() error {
		loader := helpers.ProgramLoader{
			Program:           prog,
			BaseAddr:          0,
			Mode:              helpers.Mode64BitIdentityMapping,
			MaxLoopIterations: 128,
		}
		if arch == hv.ArchitectureARM64 {
			arm64ExceptionVectorInit(&loader)
		}

		vm, err := q.dev.NewVirtualMachine(hv.SimpleVMConfig{
			NumCPUs: 1,
			MemSize: 64 * 1024 * 1024,
			MemBase: 0,

			VMLoader: &loader,
		})
		if err != nil {
			return fmt.Errorf("create virtual machine: %w", err)
		}
		defer vm.Close()

		for _, dev := range devs {
			if err := vm.AddDevice(dev); err != nil {
				return fmt.Errorf("add device to virtual machine: %w", err)
			}
		}

		err = vm.Run(context.Background(), &loader)
		if !errors.Is(err, hv.ErrVMHalted) {
			return fmt.Errorf("run virtual machine: %w", err)
		}

		if err := vm.VirtualCPUCall(0, f); err != nil {
			return fmt.Errorf("sync vCPU: %w", err)
		}

		return nil
	})
}

func (q *bringUpQuest) runVMTaskWithTimeout(
	name string,
	arch hv.CpuArchitecture,
	timeout time.Duration,
	prog ir.Program,
	f func(cpu hv.VirtualCPU) error,
	devs ...hv.Device,
) error {
	return q.runArchitectureTask(name, arch, func() error {
		loader := helpers.ProgramLoader{
			Program:           prog,
			BaseAddr:          0,
			Mode:              helpers.Mode64BitIdentityMapping,
			MaxLoopIterations: 128,
		}
		if arch == hv.ArchitectureARM64 {
			arm64ExceptionVectorInit(&loader)
		}

		vm, err := q.dev.NewVirtualMachine(hv.SimpleVMConfig{
			NumCPUs: 1,
			MemSize: 64 * 1024 * 1024,
			MemBase: 0,

			VMLoader: &loader,
		})
		if err != nil {
			return fmt.Errorf("create virtual machine: %w", err)
		}
		defer vm.Close()

		for _, dev := range devs {
			if err := vm.AddDevice(dev); err != nil {
				return fmt.Errorf("add device to virtual machine: %w", err)
			}
		}

		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()

		closed := make(chan struct{})

		// just in case the timeout doesn't work, we set a deadline on the process
		go func() {
			select {
			case <-closed:
				return
			case <-time.After(timeout * 4):
				slog.Error("VM task timed out, killing process", "name", name)
				os.Exit(1)
			}
		}()

		err = vm.Run(ctx, &loader)
		if !errors.Is(err, hv.ErrVMHalted) && !errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("run virtual machine: %w", err)
		}

		if err := vm.VirtualCPUCall(0, f); err != nil {
			return fmt.Errorf("sync vCPU: %w", err)
		}

		close(closed)

		return nil
	})
}

func (q *bringUpQuest) Run() error {
	slog.Info("Starting Bringup Quest")

	if err := q.runTask("Compile x86_64 test program", func() error {
		frag, err := amd64ir.Compile(ir.Method{
			ir.Printf("bringup-quest-ok\n"),
			ir.Return(ir.Int64(42)),
		})
		if err != nil {
			return fmt.Errorf("compile test program: %w", err)
		}

		if _, err := amd64.EmitStandaloneELF(frag); err != nil {
			return fmt.Errorf("emit ELF: %w", err)
		}
		return nil
	}); err != nil {
		return err
	}

	if err := q.runTask("Compile arm64 test program", func() error {
		frag, err := arm64ir.Compile(ir.Method{
			ir.Printf("bringup-quest-ok\n"),
			ir.Return(ir.Int64(42)),
		})
		if err != nil {
			return fmt.Errorf("compile test program: %w", err)
		}

		if _, err := arm64.EmitStandaloneELF(frag); err != nil {
			return fmt.Errorf("emit ELF: %w", err)
		}
		return nil
	}); err != nil {
		return err
	}

	dev, err := factory.Open()
	if err != nil {
		return fmt.Errorf("open hypervisor factory: %w", err)
	}
	q.dev = dev
	defer q.dev.Close()

	if err := q.runVMTask("HLT Test", hv.ArchitectureX86_64, ir.Program{
		Entrypoint: "main",
		Methods: map[string]ir.Method{
			"main": {
				amd64.Hlt(),
			},
		},
	}, func(cpu hv.VirtualCPU) error {
		return nil
	}); err != nil {
		return err
	}

	if err := q.runVMTask("Addition Test", hv.ArchitectureX86_64, ir.Program{
		Entrypoint: "main",
		Methods: map[string]ir.Method{
			"main": {
				amd64.MovImmediate(amd64.Reg32(amd64.RAX), 40),
				amd64.AddRegImm(amd64.Reg32(amd64.RAX), 2),
				amd64.Hlt(),
			},
		},
	}, func(cpu hv.VirtualCPU) error {
		regs := map[hv.Register]hv.RegisterValue{
			hv.RegisterAMD64Rax: hv.Register64(0),
		}

		if err := cpu.GetRegisters(regs); err != nil {
			return fmt.Errorf("get RAX register: %w", err)
		}

		rax := uint64(regs[hv.RegisterAMD64Rax].(hv.Register64))
		if rax != 42 {
			return fmt.Errorf("unexpected RAX value: got %d, want 42", rax)
		}

		return nil
	}); err != nil {
		return err
	}

	// Test reading and writing memory
	if err := q.runVMTask("Memory Read/Write Test", hv.ArchitectureX86_64, ir.Program{
		Entrypoint: "main",
		Methods: map[string]ir.Method{
			"main": {
				// load RIP+0x1000 into RCX
				amd64.LeaRelative(amd64.Reg64(amd64.R8), 0x1000),
				// write value to memory at address in RCX
				// load 0xcafebabe into RAX
				amd64.MovImmediate(amd64.Reg64(amd64.R9), 0x12345678),
				amd64.MovToMemory(amd64.Mem(amd64.Reg64(amd64.R8)), amd64.Reg64(amd64.R9)),
				amd64.MovFromMemory(amd64.Reg64(amd64.R10), amd64.Mem(amd64.Reg64(amd64.R8))),
				amd64.Hlt(),
			},
		},
	}, func(cpu hv.VirtualCPU) error {
		regs := map[hv.Register]hv.RegisterValue{
			hv.RegisterAMD64R9:  hv.Register64(0),
			hv.RegisterAMD64R10: hv.Register64(0),
		}

		if err := cpu.GetRegisters(regs); err != nil {
			return fmt.Errorf("get RBX register: %w", err)
		}

		r9 := uint64(regs[hv.RegisterAMD64R9].(hv.Register64))
		if r9 != 0x12345678 {
			return fmt.Errorf("unexpected R9 value: got 0x%08x, want 0x12345678", r9)
		}

		r10 := uint64(regs[hv.RegisterAMD64R10].(hv.Register64))
		if r10 != 0x12345678 {
			return fmt.Errorf("unexpected R10 value: got 0x%08x, want 0x12345678", r10)
		}

		return nil
	}); err != nil {
		return err
	}

	// Test simple I/O
	const dataMessage asm.Variable = 100
	loop := asm.Label("next_char")
	done := asm.Label("done")

	ioHelloWorld := ir.Program{
		Entrypoint: "main",
		Methods: map[string]ir.Method{
			"main": {
				asm.Group{
					amd64.LoadConstantBytes(dataMessage, append([]byte("Hello, World!"), 0)),
					amd64.LoadAddress(amd64.Reg64(amd64.RSI), dataMessage),
					amd64.MovImmediate(amd64.Reg16(amd64.RDX), 0x3f8),
					asm.MarkLabel(loop),
					amd64.MovFromMemory(amd64.Reg8(amd64.RAX), amd64.Mem(amd64.Reg64(amd64.RSI))),
					amd64.AddRegImm(amd64.Reg64(amd64.RSI), 1),
					amd64.CmpRegImm(amd64.Reg8(amd64.RAX), 0),
					amd64.JumpIfZero(done),
					amd64.OutDXAL(),
					amd64.Jump(loop),
					asm.MarkLabel(done),
					amd64.Hlt(),
				},
			},
		},
	}

	result := make([]byte, 0, 64)
	if err := q.runVMTask("I/O Test", hv.ArchitectureX86_64, ioHelloWorld, func(cpu hv.VirtualCPU) error {
		if !bytes.Equal(result, []byte("Hello, World!")) {
			return fmt.Errorf("unexpected I/O result: got %q, want %q", result, "Hello, World!")
		}

		return nil
	}, hv.SimpleX86IOPortDevice{
		Ports: []uint16{0x3f8},
		WriteFunc: func(port uint16, data []byte) error {
			result = append(result, data...)
			return nil
		},
	}); err != nil {
		return err
	}

	serialBuf := &bytes.Buffer{}
	serialDev := serial.NewSerial16550(0x3f8, 4, serialBuf)
	if err := q.runVMTask("Serial Port Test", hv.ArchitectureX86_64, ioHelloWorld, func(cpu hv.VirtualCPU) error {
		serialOutput := serialBuf.String()
		if serialOutput != "Hello, World!" {
			return fmt.Errorf("unexpected serial output: got %q, want %q", serialOutput, "Hello, World!")
		}
		return nil
	}, serialDev); err != nil {
		return err
	}

	// Test simple MMIO
	result = make([]byte, 0, 64)

	if err := q.runVMTask("MMIO Test", hv.ArchitectureX86_64, ir.Program{
		Entrypoint: "main",
		Methods: map[string]ir.Method{
			"main": {
				asm.Group{
					amd64.LoadConstantBytes(dataMessage, append([]byte("Hello"), 0)),
					amd64.LoadAddress(amd64.Reg64(amd64.RSI), dataMessage),
					amd64.MovImmediate(amd64.Reg64(amd64.RDX), 0xdead0000),
					amd64.MovImmediate(amd64.Reg64(amd64.RAX), 0),
					asm.MarkLabel(loop),
					amd64.MovFromMemory(amd64.Reg8(amd64.RAX), amd64.Mem(amd64.Reg64(amd64.RSI))),
					amd64.AddRegImm(amd64.Reg64(amd64.RSI), 1),
					amd64.CmpRegImm(amd64.Reg8(amd64.RAX), 0),
					amd64.JumpIfZero(done),
					amd64.MovToMemory(amd64.Mem(amd64.Reg64(amd64.RDX)), amd64.Reg8(amd64.RAX)),
					amd64.Jump(loop),
					asm.MarkLabel(done),
					amd64.Hlt(),
				},
			},
		},
	}, func(cpu hv.VirtualCPU) error {
		if !bytes.Equal(result, []byte("Hello")) {
			return fmt.Errorf("unexpected MMIO result: got %q, want %q", result, "Hello, World!")
		}

		return nil
	}, hv.SimpleMMIODevice{
		Regions: []hv.MMIORegion{
			{Address: 0xdead0000, Size: 0x1000},
		},
		WriteFunc: func(addr uint64, data []byte) error {
			// Collect the MMIO writes to verify the output
			if addr == 0xdead0000 {
				result = append(result, data[0])
				return nil
			}

			return fmt.Errorf("unexpected MMIO write to address 0x%08x", addr)
		},
	}); err != nil {
		return err
	}

	// Timeout test
	if err := q.runVMTaskWithTimeout("Timeout Test", hv.ArchitectureX86_64, 100*time.Millisecond, ir.Program{
		Entrypoint: "main",
		Methods: map[string]ir.Method{
			"main": {
				asm.MarkLabel(asm.Label("loop")),
				amd64.Jump(asm.Label("loop")),
			},
		},
	}, func(cpu hv.VirtualCPU) error {
		return nil
	}); err != nil {
		return err
	}

	// ARM64 tests
	if err := q.runVMTask("PSCI SYSTEM_OFF Test", hv.ArchitectureARM64, ir.Program{
		Entrypoint: "main",
		Methods: map[string]ir.Method{
			"main": {
				arm64.MovImmediate(arm64.Reg64(arm64.X0), psciSystemOff),
				arm64.Hvc(),
			},
		},
	}, func(cpu hv.VirtualCPU) error {
		return nil
	}); err != nil {
		return err
	}

	if err := q.runVMTask("Addition Test", hv.ArchitectureARM64, ir.Program{
		Entrypoint: "main",
		Methods: map[string]ir.Method{
			"main": {
				arm64.MovImmediate(arm64.Reg64(arm64.X5), 40),
				arm64.AddRegImm(arm64.Reg64(arm64.X5), 2),
				arm64.MovImmediate(arm64.Reg64(arm64.X0), psciSystemOff),
				arm64.Hvc(),
			},
		},
	}, func(cpu hv.VirtualCPU) error {
		regs := map[hv.Register]hv.RegisterValue{
			hv.RegisterARM64X5: hv.Register64(0),
		}

		if err := cpu.GetRegisters(regs); err != nil {
			return fmt.Errorf("get X5 register: %w", err)
		}

		x5 := uint64(regs[hv.RegisterARM64X5].(hv.Register64))
		if x5 != 42 {
			return fmt.Errorf("unexpected X5 value: got %d, want 42", x5)
		}

		return nil
	}); err != nil {
		return err
	}

	if err := q.runVMTask("Memory Read/Write Test", hv.ArchitectureARM64, ir.Program{
		Entrypoint: "main",
		Methods: map[string]ir.Method{
			"main": {
				asm.Group{
					arm64.MovImmediate(arm64.Reg64(arm64.X10), 0x1000),
					arm64.MovImmediate(arm64.Reg64(arm64.X11), 0xcafebabe),
					arm64.MovToMemory(arm64.Mem(arm64.Reg64(arm64.X10)), arm64.Reg64(arm64.X11)),
					arm64.MovFromMemory(arm64.Reg64(arm64.X12), arm64.Mem(arm64.Reg64(arm64.X10))),
					arm64.MovImmediate(arm64.Reg64(arm64.X0), psciSystemOff),
					arm64.Hvc(),
				},
			},
		},
	}, func(cpu hv.VirtualCPU) error {
		regs := map[hv.Register]hv.RegisterValue{
			hv.RegisterARM64X10: hv.Register64(0),
			hv.RegisterARM64X11: hv.Register64(0),
			hv.RegisterARM64X12: hv.Register64(0),
		}

		if err := cpu.GetRegisters(regs); err != nil {
			return fmt.Errorf("get X12 register: %w", err)
		}

		x10 := uint64(regs[hv.RegisterARM64X10].(hv.Register64))
		x11 := uint64(regs[hv.RegisterARM64X11].(hv.Register64))
		if x11 != 0xcafebabe {
			return fmt.Errorf("unexpected X11 value: got 0x%08x (X10=0x%016x), want 0xcafebabe", x11, x10)
		}

		x12 := uint64(regs[hv.RegisterARM64X12].(hv.Register64))
		if x12 != 0xcafebabe {
			return fmt.Errorf("unexpected X12 value: got 0x%08x, want 0xcafebabe", x12)
		}

		return nil
	}); err != nil {
		return err
	}

	const arm64MMIOMessage asm.Variable = 201
	arm64Loop := asm.Label("arm64_mmio_loop")
	arm64Done := asm.Label("arm64_mmio_done")

	arm64Result := make([]byte, 0, 64)

	arm64Message := append([]byte("Hello, World!"), 0)
	arm64MMIOInit := make([]asm.Fragment, 0, len(arm64Message)*2+1)
	arm64MMIOInit = append(arm64MMIOInit, arm64.MovImmediate(arm64.Reg64(arm64.X1), arm64MessageBuf))
	for idx, b := range arm64Message {
		arm64MMIOInit = append(arm64MMIOInit,
			arm64.MovImmediate(arm64.Reg32(arm64.X3), int64(b)),
			arm64.MovToMemory8(arm64.Mem(arm64.Reg64(arm64.X1)).WithDisp(int32(idx)), arm64.Reg32(arm64.X3)),
		)
	}
	arm64MMIOBody := append(arm64MMIOInit, []asm.Fragment{
		arm64.MovImmediate(arm64.Reg64(arm64.X1), arm64MessageBuf),
		arm64.MovImmediate(arm64.Reg64(arm64.X2), arm64MMIOAddr),
		asm.MarkLabel(arm64Loop),
		arm64.MovZX8(arm64.Reg64(arm64.X3), arm64.Mem(arm64.Reg64(arm64.X1))),
		arm64.CmpRegImm(arm64.Reg64(arm64.X3), 0),
		arm64.JumpIfZero(arm64Done),
		arm64.MovToMemory8(arm64.Mem(arm64.Reg64(arm64.X2)), arm64.Reg32(arm64.X3)),
		arm64.AddRegImm(arm64.Reg64(arm64.X1), 1),
		arm64.Jump(arm64Loop),
		asm.MarkLabel(arm64Done),
		arm64.MovImmediate(arm64.Reg64(arm64.X0), psciSystemOff),
		arm64.Hvc(),
	}...)

	if err := q.runVMTask("MMIO Test", hv.ArchitectureARM64, ir.Program{
		Entrypoint: "main",
		Methods: map[string]ir.Method{
			"main": {
				asm.Group(arm64MMIOBody),
			},
		},
	}, func(cpu hv.VirtualCPU) error {
		if !bytes.Equal(arm64Result, []byte("Hello, World!")) {
			return fmt.Errorf("unexpected MMIO result: got %q, want %q", arm64Result, "Hello, World!")
		}

		return nil
	}, hv.SimpleMMIODevice{
		Regions: []hv.MMIORegion{
			{Address: arm64MMIOAddr, Size: 0x1000},
		},
		WriteFunc: func(addr uint64, data []byte) error {
			if addr == arm64MMIOAddr {
				arm64Result = append(arm64Result, data[0])
				return nil
			}

			return fmt.Errorf("unexpected MMIO write to address 0x%08x", addr)
		},
	}); err != nil {
		return err
	}

	// Timeout test (ARM64)
	if err := q.runVMTaskWithTimeout("Timeout Test (ARM64)", hv.ArchitectureARM64, 100*time.Millisecond, ir.Program{
		Entrypoint: "main",
		Methods: map[string]ir.Method{
			"main": {
				asm.MarkLabel(asm.Label("loop")),
				arm64.Jump(asm.Label("loop")),
			},
		},
	}, func(cpu hv.VirtualCPU) error {
		return nil
	}); err != nil {
		return err
	}

	slog.Info("Bringup Quest Completed")

	return nil
}

func (q *bringUpQuest) RunLinux() error {
	slog.Info("Starting Bringup Quest: Linux Boot")

	dev, err := factory.Open()
	if err != nil {
		return fmt.Errorf("open hypervisor factory: %w", err)
	}
	q.dev = dev
	defer q.dev.Close()

	if dev.Architecture() != hv.ArchitectureX86_64 {
		return fmt.Errorf("linux boot only supported on x86_64 hypervisors")
	}

	loader := &boot.LinuxLoader{
		NumCPUs: 1,
		MemSize: 256 * 1024 * 1024, // 256 MiB

		Cmdline: []string{
			"console=ttyS0",
			"earlycon=uart8250,io,0x3f8,115200,keep",
			"i8042.nopnp",
		},

		GetKernel: func() (io.ReaderAt, int64, error) {
			f, err := os.Open(filepath.Join("local", "vmlinux"))
			if err != nil {
				return nil, 0, fmt.Errorf("open kernel image: %w", err)
			}
			info, err := f.Stat()
			if err != nil {
				return nil, 0, fmt.Errorf("stat kernel image: %w", err)
			}
			return f, info.Size(), nil
		},

		GetSystemMap: func() (io.ReaderAt, error) {
			f, err := os.Open(filepath.Join("local", "System.map"))
			if err != nil {
				return nil, fmt.Errorf("open System.map: %w", err)
			}
			return f, nil
		},

		Init: ir.Program{
			Entrypoint: "main",
			Methods: map[string]ir.Method{
				"main": {
					ir.Printf("Hello, World\n"),
					ir.Syscall(
						amd64defs.SYS_REBOOT,
						ir.Int64(amd64defs.LINUX_REBOOT_MAGIC1),
						ir.Int64(amd64defs.LINUX_REBOOT_MAGIC2),
						ir.Int64(amd64defs.LINUX_REBOOT_CMD_RESTART),
						ir.Int64(0),
					),
				},
			},
		},

		Stdout: os.Stdout,
	}

	vm, err := q.dev.NewVirtualMachine(loader)
	if err != nil {
		return fmt.Errorf("create virtual machine: %w", err)
	}
	defer vm.Close()

	run, err := loader.RunConfig()
	if err != nil {
		return fmt.Errorf("prepare linux boot: %w", err)
	}

	if err := vm.Run(context.Background(), run); err != nil {
		return fmt.Errorf("run linux boot: %w", err)
	}

	slog.Info("Linux Boot Completed")

	return nil
}

func main() {
	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)

	linux := fs.Bool("linux", false, "Try booting Linux")

	if err := fs.Parse(os.Args[1:]); err != nil {
		slog.Error("failed to parse flags", "error", err)
		os.Exit(1)
	}

	q := &bringUpQuest{}

	if *linux {
		if err := q.RunLinux(); err != nil {
			slog.Error("failed bringup quest linux", "error", err)
			os.Exit(1)
		}
		return
	}

	if err := q.Run(); err != nil {
		slog.Error("failed bringup quest", "error", err)
		os.Exit(1)
	}
}
