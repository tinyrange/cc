package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/tinyrange/cc/internal/asm"
	"github.com/tinyrange/cc/internal/asm/amd64"
	"github.com/tinyrange/cc/internal/asm/arm64"
	riscvasm "github.com/tinyrange/cc/internal/asm/riscv"
	amd64serial "github.com/tinyrange/cc/internal/devices/amd64/serial"
	"github.com/tinyrange/cc/internal/devices/virtio"
	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/hv/factory"
	"github.com/tinyrange/cc/internal/hv/helpers"
	"github.com/tinyrange/cc/internal/initx"
	"github.com/tinyrange/cc/internal/ir"
	amd64ir "github.com/tinyrange/cc/internal/ir/amd64"
	arm64ir "github.com/tinyrange/cc/internal/ir/arm64"
	_ "github.com/tinyrange/cc/internal/ir/riscv"
	"github.com/tinyrange/cc/internal/linux/boot"
	"github.com/tinyrange/cc/internal/linux/defs"
	amd64defs "github.com/tinyrange/cc/internal/linux/defs/amd64"
	"github.com/tinyrange/cc/internal/linux/kernel"
)

const (
	psciSystemOff         = 0x84000008
	arm64MMIOAddr         = 0xdead0000
	arm64MessageBuf       = 0x2000
	arm64UARTMMIOBase     = 0x09000000
	arm64SnapshotMMIOAddr = 0xf0000000
	riscvMemoryBase       = 0x80000000
)

const (
	arm64VectorEntrySize  = 0x80
	arm64VectorEntryCount = 16
	arm64NopEncoding      = 0xd503201f
)

var arm64VectorTableBytes = mustBuildArm64VectorTable()

type bringUpQuest struct {
	dev          hv.Hypervisor
	riscv        hv.Hypervisor
	architecture hv.CpuArchitecture
}

func (q *bringUpQuest) hypervisorForArch(arch hv.CpuArchitecture) hv.Hypervisor {
	if q.dev != nil && q.dev.Architecture() == arch {
		return q.dev
	}
	if q.riscv != nil && q.riscv.Architecture() == arch {
		return q.riscv
	}
	return nil
}

type resumeRunConfig struct{}

// Run implements hv.RunConfig.
func (resumeRunConfig) Run(ctx context.Context, vcpu hv.VirtualCPU) error {
	if err := vcpu.Run(ctx); err != nil {
		if errors.Is(err, hv.ErrVMHalted) ||
			errors.Is(err, hv.ErrGuestRequestedReboot) ||
			errors.Is(err, hv.ErrYield) {
			return nil
		}

		return err
	}

	return nil
}

func runWithTiming(name string, fn func() error) error {
	start := time.Now()
	err := fn()
	duration := time.Since(start)
	if err != nil {
		slog.Error("Step failed", "step", name, "duration", duration, "error", err)
		return err
	}
	slog.Info("Step completed", "step", name, "duration", duration)
	return nil
}

func linuxMemoryBaseForArch(arch hv.CpuArchitecture) uint64 {
	switch arch {
	case hv.ArchitectureARM64:
		return 0x80000000
	case hv.ArchitectureRISCV64:
		return riscvMemoryBase
	default:
		return 0
	}
}

func hostArchitecture() (hv.CpuArchitecture, error) {
	switch runtime.GOARCH {
	case "amd64":
		return hv.ArchitectureX86_64, nil
	case "arm64":
		return hv.ArchitectureARM64, nil
	default:
		return hv.ArchitectureInvalid, fmt.Errorf("unsupported host architecture %q", runtime.GOARCH)
	}
}

func parseArchitecture(value string) (hv.CpuArchitecture, error) {
	switch value {
	case "", "all":
		return hv.ArchitectureInvalid, nil
	case "x86_64", "amd64":
		return hv.ArchitectureX86_64, nil
	case "arm64", "aarch64":
		return hv.ArchitectureARM64, nil
	case "riscv64":
		return hv.ArchitectureRISCV64, nil
	default:
		return hv.ArchitectureInvalid, fmt.Errorf("unknown architecture %q", value)
	}
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
	return runWithTiming(name, f)
}

func (q *bringUpQuest) runArchitectureTask(name string, arch hv.CpuArchitecture, f func() error) error {
	if q.architecture != hv.ArchitectureInvalid && q.architecture != arch {
		return nil
	}
	if q.hypervisorForArch(arch) == nil {
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
		dev := q.hypervisorForArch(arch)
		if dev == nil {
			return fmt.Errorf("hypervisor not initialized for %s", arch)
		}

		baseAddr := uint64(0)
		if arch == hv.ArchitectureRISCV64 {
			baseAddr = linuxMemoryBaseForArch(arch)
		}

		loader := helpers.ProgramLoader{
			Program:           prog,
			BaseAddr:          baseAddr,
			Mode:              helpers.Mode64BitIdentityMapping,
			MaxLoopIterations: 128,
		}
		if arch == hv.ArchitectureARM64 {
			arm64ExceptionVectorInit(&loader)
		}

		vm, err := dev.NewVirtualMachine(hv.SimpleVMConfig{
			NumCPUs: 1,
			MemSize: 64 * 1024 * 1024,
			MemBase: baseAddr,

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
		dev := q.hypervisorForArch(arch)
		if dev == nil {
			return fmt.Errorf("hypervisor not initialized for %s", arch)
		}

		baseAddr := uint64(0)
		if arch == hv.ArchitectureRISCV64 {
			baseAddr = linuxMemoryBaseForArch(arch)
		}

		loader := helpers.ProgramLoader{
			Program:           prog,
			BaseAddr:          baseAddr,
			Mode:              helpers.Mode64BitIdentityMapping,
			MaxLoopIterations: 128,
		}
		if arch == hv.ArchitectureARM64 {
			arm64ExceptionVectorInit(&loader)
		}

		vm, err := dev.NewVirtualMachine(hv.SimpleVMConfig{
			NumCPUs: 1,
			MemSize: 64 * 1024 * 1024,
			MemBase: baseAddr,

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

	hostArch, err := hostArchitecture()
	if err != nil {
		return fmt.Errorf("detect host architecture: %w", err)
	}

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

	if q.architecture != hv.ArchitectureRISCV64 {
		if err := runWithTiming("Open Hypervisor", func() error {
			dev, err := factory.Open()
			if err != nil {
				return err
			}
			q.dev = dev
			return nil
		}); err != nil {
			return fmt.Errorf("open hypervisor factory: %w", err)
		}
		defer q.dev.Close()
		if q.architecture != hv.ArchitectureInvalid && q.architecture != q.dev.Architecture() {
			return fmt.Errorf("requested architecture %s not available on this host (%s)", q.architecture, hostArch)
		}
	}

	if q.architecture == hv.ArchitectureInvalid || q.architecture == hv.ArchitectureRISCV64 {
		if err := q.runTask("Open RISC-V Hypervisor", func() error {
			dev, err := factory.NewWithArchitecture(hv.ArchitectureRISCV64)
			if err != nil {
				return err
			}
			q.riscv = dev
			return nil
		}); err != nil {
			return fmt.Errorf("open riscv hypervisor: %w", err)
		}
		if q.riscv != nil {
			defer q.riscv.Close()
		}
	}

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
	serialDev := amd64serial.NewSerial16550(0x3f8, 4, serialBuf)
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

	// Snapshot quest
	if err := q.runArchitectureTask("Snapshot Quest", hv.ArchitectureX86_64, func() error {
		loader := &helpers.ProgramLoader{
			Program: ir.Program{
				Entrypoint: "main",
				Methods: map[string]ir.Method{
					"main": {
						// Set RCX to 0 to indicate start
						amd64.MovImmediate(amd64.Reg64(amd64.RCX), 0),

						// do a raw memory write to 0xf000_0000 to trigger the yield
						amd64.MovImmediate(amd64.Reg64(amd64.RAX), 0xdeadbeef),
						amd64.MovImmediate(amd64.Reg64(amd64.RBX), 0xf0000000),
						amd64.MovToMemory(amd64.Mem(amd64.Reg64(amd64.RBX)), amd64.Reg64(amd64.RAX)),

						// Set RCX to 42 to indicate success
						amd64.MovImmediate(amd64.Reg64(amd64.RCX), 42),

						// Do a raw memory write to 0xf000_0000 again to trigger another yield
						amd64.MovImmediate(amd64.Reg64(amd64.RAX), 0xdeadbeef),
						amd64.MovImmediate(amd64.Reg64(amd64.RBX), 0xf0000000),
						amd64.MovToMemory(amd64.Mem(amd64.Reg64(amd64.RBX)), amd64.Reg64(amd64.RAX)),

						// Halt the CPU
						amd64.Hlt(),
					},
				},
			},
			BaseAddr:          0,
			Mode:              helpers.Mode64BitIdentityMapping,
			MaxLoopIterations: 1,
		}

		vm, err := q.dev.NewVirtualMachine(hv.SimpleVMConfig{
			NumCPUs:  1,
			MemSize:  64 * 1024 * 1024,
			MemBase:  0,
			VMLoader: loader,
		})
		if err != nil {
			return fmt.Errorf("create virtual machine: %w", err)
		}
		defer vm.Close()

		if err := vm.AddDevice(hv.SimpleMMIODevice{
			Regions: []hv.MMIORegion{
				{Address: 0xf000_0000, Size: 4096},
			},
			WriteFunc: func(addr uint64, data []byte) error {
				if addr != 0xf000_0000 {
					return fmt.Errorf("unexpected MMIO write to address 0x%08x", addr)
				}

				// get the data as a uint32
				value := binary.LittleEndian.Uint32(data)

				if value != 0xdeadbeef {
					return fmt.Errorf("unexpected MMIO write value: 0x%08x", value)
				}

				// Yield the VM
				return hv.ErrYield
			},
		}); err != nil {
			return fmt.Errorf("add MMIO device: %w", err)
		}

		if err := vm.Run(context.Background(), loader); err != nil && !errors.Is(err, hv.ErrYield) {
			return fmt.Errorf("run virtual machine: %w", err)
		}

		if err := vm.VirtualCPUCall(0, func(cpu hv.VirtualCPU) error {
			// Verify that RCX is 0
			regs := map[hv.Register]hv.RegisterValue{
				hv.RegisterAMD64Rcx: hv.Register64(0),
			}

			if err := cpu.GetRegisters(regs); err != nil {
				return fmt.Errorf("get RCX register: %w", err)
			}

			rcx := uint64(regs[hv.RegisterAMD64Rcx].(hv.Register64))
			if rcx != 0 {
				return fmt.Errorf("unexpected RCX value after first yield: got %d, want 0", rcx)
			}

			return nil
		}); err != nil {
			return fmt.Errorf("sync vCPU after first yield: %w", err)
		}

		// Capture an initial snapshot
		snapshot, err := vm.CaptureSnapshot()
		if err != nil {
			return fmt.Errorf("create snapshot: %w", err)
		}

		// Let the virtual machine continue and yield again
		if err := vm.Run(context.Background(), resumeRunConfig{}); err != nil {
			return fmt.Errorf("run virtual machine: %w", err)
		}

		// Verify that RCX is 42
		if err := vm.VirtualCPUCall(0, func(cpu hv.VirtualCPU) error {
			regs := map[hv.Register]hv.RegisterValue{
				hv.RegisterAMD64Rcx: hv.Register64(0),
			}

			if err := cpu.GetRegisters(regs); err != nil {
				return fmt.Errorf("get RCX register: %w", err)
			}

			rcx := uint64(regs[hv.RegisterAMD64Rcx].(hv.Register64))
			if rcx != 42 {
				return fmt.Errorf("unexpected RCX value after second yield: got %d, want 42", rcx)
			}

			return nil
		}); err != nil {
			return fmt.Errorf("sync vCPU after second yield: %w", err)
		}

		// Restore from the snapshot
		if err := vm.RestoreSnapshot(snapshot); err != nil {
			return fmt.Errorf("restore snapshot: %w", err)
		}

		// Verify that RCX is back to 0
		if err := vm.VirtualCPUCall(0, func(cpu hv.VirtualCPU) error {
			regs := map[hv.Register]hv.RegisterValue{
				hv.RegisterAMD64Rcx: hv.Register64(0),
			}

			if err := cpu.GetRegisters(regs); err != nil {
				return fmt.Errorf("get RCX register: %w", err)
			}

			rcx := uint64(regs[hv.RegisterAMD64Rcx].(hv.Register64))
			if rcx != 0 {
				return fmt.Errorf("unexpected RCX value after restore: got %d, want 0", rcx)
			}

			return nil
		}); err != nil {
			return fmt.Errorf("sync vCPU after restore: %w", err)
		}

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

	if err := q.runVMTask("HLT Test", hv.ArchitectureRISCV64, ir.Program{
		Entrypoint: "main",
		Methods: map[string]ir.Method{
			"main": {
				riscvasm.Halt(),
			},
		},
	}, func(cpu hv.VirtualCPU) error {
		return nil
	}); err != nil {
		return err
	}

	if err := q.runVMTask("Addition Test", hv.ArchitectureRISCV64, ir.Program{
		Entrypoint: "main",
		Methods: map[string]ir.Method{
			"main": {
				riscvasm.MovImmediate(riscvasm.Reg64(riscvasm.X5), 40),
				riscvasm.AddRegImm(riscvasm.Reg64(riscvasm.X5), 2),
				riscvasm.Halt(),
			},
		},
	}, func(cpu hv.VirtualCPU) error {
		regs := map[hv.Register]hv.RegisterValue{
			hv.RegisterRISCVX5: hv.Register64(0),
		}

		if err := cpu.GetRegisters(regs); err != nil {
			return fmt.Errorf("get X5 register: %w", err)
		}

		x5 := uint64(regs[hv.RegisterRISCVX5].(hv.Register64))
		if x5 != 42 {
			return fmt.Errorf("unexpected X5 value: got %d, want 42", x5)
		}

		return nil
	}); err != nil {
		return err
	}

	if err := q.runVMTask("Memory Read/Write Test", hv.ArchitectureRISCV64, ir.Program{
		Entrypoint: "main",
		Methods: map[string]ir.Method{
			"main": {
				riscvasm.MovImmediate(riscvasm.Reg64(riscvasm.X10), int64(riscvMemoryBase+0x1000)),
				riscvasm.MovImmediate(riscvasm.Reg64(riscvasm.X11), 0xcafebabe),
				riscvasm.MovToMemory(riscvasm.Reg64(riscvasm.X10), riscvasm.Reg64(riscvasm.X11), 0),
				riscvasm.MovFromMemory(riscvasm.Reg64(riscvasm.X12), riscvasm.Reg64(riscvasm.X10), 0),
				riscvasm.Halt(),
			},
		},
	}, func(cpu hv.VirtualCPU) error {
		regs := map[hv.Register]hv.RegisterValue{
			hv.RegisterRISCVX10: hv.Register64(0),
			hv.RegisterRISCVX11: hv.Register64(0),
			hv.RegisterRISCVX12: hv.Register64(0),
		}

		if err := cpu.GetRegisters(regs); err != nil {
			return fmt.Errorf("get riscv registers: %w", err)
		}

		if val := uint64(regs[hv.RegisterRISCVX11].(hv.Register64)); val != 0xcafebabe {
			return fmt.Errorf("unexpected X11 value: got 0x%08x, want 0xcafebabe", val)
		}

		if val := uint64(regs[hv.RegisterRISCVX12].(hv.Register64)); val != 0xcafebabe {
			return fmt.Errorf("unexpected X12 value: got 0x%08x, want 0xcafebabe", val)
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
	if err := q.runVMTaskWithTimeout("Timeout Test", hv.ArchitectureARM64, 100*time.Millisecond, ir.Program{
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

	// Snapshot quest (ARM64)
	if err := q.runArchitectureTask("Snapshot Quest", hv.ArchitectureARM64, func() error {
		loader := &helpers.ProgramLoader{
			Program: ir.Program{
				Entrypoint: "main",
				Methods: map[string]ir.Method{
					"main": {
						arm64.MovImmediate(arm64.Reg64(arm64.X1), 0),
						arm64.MovImmediate(arm64.Reg64(arm64.X2), arm64SnapshotMMIOAddr),
						arm64.MovImmediate(arm64.Reg32(arm64.X3), 0xdeadbeef),

						// Trigger first yield.
						arm64.MovToMemory32(arm64.Mem(arm64.Reg64(arm64.X2)), arm64.Reg32(arm64.X3)),

						// Indicate success and trigger another yield.
						arm64.MovImmediate(arm64.Reg64(arm64.X1), 42),
						arm64.MovToMemory32(arm64.Mem(arm64.Reg64(arm64.X2)), arm64.Reg32(arm64.X3)),

						arm64.MovImmediate(arm64.Reg64(arm64.X0), psciSystemOff),
						arm64.Hvc(),
					},
				},
			},
			BaseAddr:          linuxMemoryBaseForArch(hv.ArchitectureARM64),
			Mode:              helpers.Mode64BitIdentityMapping,
			MaxLoopIterations: 1,
		}

		vm, err := q.dev.NewVirtualMachine(hv.SimpleVMConfig{
			NumCPUs:  1,
			MemSize:  64 * 1024 * 1024,
			MemBase:  linuxMemoryBaseForArch(hv.ArchitectureARM64),
			VMLoader: loader,
		})
		if err != nil {
			return fmt.Errorf("create virtual machine: %w", err)
		}
		defer vm.Close()

		if err := vm.AddDevice(hv.SimpleMMIODevice{
			Regions: []hv.MMIORegion{
				{Address: arm64SnapshotMMIOAddr, Size: 4096},
			},
			WriteFunc: func(addr uint64, data []byte) error {
				if addr != arm64SnapshotMMIOAddr {
					return fmt.Errorf("unexpected MMIO write to address 0x%08x", addr)
				}
				if len(data) < 4 {
					return fmt.Errorf("unexpected MMIO data size: got %d, want >=4", len(data))
				}

				value := binary.LittleEndian.Uint32(data)
				if value != 0xdeadbeef {
					return fmt.Errorf("unexpected MMIO write value: 0x%08x", value)
				}

				return hv.ErrYield
			},
		}); err != nil {
			return fmt.Errorf("add MMIO device: %w", err)
		}

		if err := vm.Run(context.Background(), loader); err != nil && !errors.Is(err, hv.ErrYield) {
			return fmt.Errorf("run virtual machine: %w", err)
		}

		if err := vm.VirtualCPUCall(0, func(cpu hv.VirtualCPU) error {
			regs := map[hv.Register]hv.RegisterValue{
				hv.RegisterARM64X1: hv.Register64(0),
			}
			if err := cpu.GetRegisters(regs); err != nil {
				return fmt.Errorf("get X1 register: %w", err)
			}

			if val := uint64(regs[hv.RegisterARM64X1].(hv.Register64)); val != 0 {
				return fmt.Errorf("unexpected X1 value after first yield: got %d, want 0", val)
			}
			return nil
		}); err != nil {
			return fmt.Errorf("sync vCPU after first yield: %w", err)
		}

		snapshot, err := vm.CaptureSnapshot()
		if err != nil {
			return fmt.Errorf("create snapshot: %w", err)
		}

		if err := vm.Run(context.Background(), resumeRunConfig{}); err != nil {
			return fmt.Errorf("run virtual machine: %w", err)
		}

		if err := vm.VirtualCPUCall(0, func(cpu hv.VirtualCPU) error {
			regs := map[hv.Register]hv.RegisterValue{
				hv.RegisterARM64X1: hv.Register64(0),
			}
			if err := cpu.GetRegisters(regs); err != nil {
				return fmt.Errorf("get X1 register: %w", err)
			}
			if val := uint64(regs[hv.RegisterARM64X1].(hv.Register64)); val != 42 {
				return fmt.Errorf("unexpected X1 value after second yield: got %d, want 42", val)
			}
			return nil
		}); err != nil {
			return fmt.Errorf("sync vCPU after second yield: %w", err)
		}

		if err := vm.RestoreSnapshot(snapshot); err != nil {
			return fmt.Errorf("restore snapshot: %w", err)
		}

		if err := vm.VirtualCPUCall(0, func(cpu hv.VirtualCPU) error {
			regs := map[hv.Register]hv.RegisterValue{
				hv.RegisterARM64X1: hv.Register64(0),
			}
			if err := cpu.GetRegisters(regs); err != nil {
				return fmt.Errorf("get X1 register: %w", err)
			}
			if val := uint64(regs[hv.RegisterARM64X1].(hv.Register64)); val != 0 {
				return fmt.Errorf("unexpected X1 value after restore: got %d, want 0", val)
			}
			return nil
		}); err != nil {
			return fmt.Errorf("sync vCPU after restore: %w", err)
		}

		return nil
	}); err != nil {
		return err
	}

	slog.Info("Bringup Quest Completed")

	return q.RunLinux()
}

func (q *bringUpQuest) RunLinux() error {
	slog.Info("Starting Bringup Quest: Linux Boot")

	hostArch, err := hostArchitecture()
	if err != nil {
		return fmt.Errorf("detect host architecture: %w", err)
	}

	targetArch := q.architecture
	if targetArch == hv.ArchitectureInvalid {
		targetArch = hostArch
	}

	dev, err := factory.OpenWithArchitecture(targetArch)
	if err != nil {
		return fmt.Errorf("open hypervisor factory: %w", err)
	}
	q.dev = dev
	defer q.dev.Close()

	if targetArch != q.dev.Architecture() {
		return fmt.Errorf("linux boot requested for %s but host hypervisor is %s", targetArch, q.dev.Architecture())
	}

	buf := &bytes.Buffer{}
	var cmdline []string
	switch q.dev.Architecture() {
	case hv.ArchitectureARM64:
		cmdline = []string{
			"console=ttyS0,115200n8",
			fmt.Sprintf("earlycon=uart8250,mmio,0x%x", arm64UARTMMIOBase),
		}
	case hv.ArchitectureX86_64:
		cmdline = []string{
			"console=hvc0",
			"quiet",
			"reboot=k",
			"panic=-1",
			"tsc=reliable",
			"tsc_early_khz=3000000",
		}
	case hv.ArchitectureRISCV64:
		cmdline = []string{
			"console=hvc0",
			"panic=-1",
		}
	default:
		return fmt.Errorf("unsupported architecture for linux boot: %s", q.dev.Architecture())
	}

	devices := []hv.DeviceTemplate{
		virtio.ConsoleTemplate{Out: io.MultiWriter(os.Stdout, buf), In: os.Stdin, Arch: dev.Architecture()},
	}

	loader := &boot.LinuxLoader{
		NumCPUs: 1,
		MemSize: 256 * 1024 * 1024, // 256 MiB
		MemBase: linuxMemoryBaseForArch(q.dev.Architecture()),

		GetCmdline: func(arch hv.CpuArchitecture) ([]string, error) {
			return cmdline, nil
		},

		GetKernel: func() (io.ReaderAt, int64, error) {
			kernel, err := kernel.LoadForArchitecture(q.dev.Architecture())
			if err != nil {
				return nil, 0, fmt.Errorf("load kernel for architecture %s: %w", q.dev.Architecture(), err)
			}

			f, err := kernel.Open()
			if err != nil {
				return nil, 0, fmt.Errorf("open kernel image: %w", err)
			}

			size, err := kernel.Size()
			if err != nil {
				return nil, 0, fmt.Errorf("get kernel size: %w", err)
			}

			return f, size, nil
		},

		GetSystemMap: func() (io.ReaderAt, error) {
			f, err := os.Open(filepath.Join("local", "System.map"))
			if err != nil {
				return nil, fmt.Errorf("open System.map: %w", err)
			}
			return f, nil
		},

		GetInit: func(arch hv.CpuArchitecture) (*ir.Program, error) {
			switch arch {
			case hv.ArchitectureX86_64:
				return &ir.Program{
					Entrypoint: "main",
					Methods: map[string]ir.Method{
						"main": {
							ir.Printf("Hello, World\n"),
							ir.Syscall(
								defs.SYS_EXIT,
								0,
							),
							// ir.Syscall(
							// 	amd64defs.SYS_REBOOT,
							// 	ir.Int64(amd64defs.LINUX_REBOOT_MAGIC1),
							// 	ir.Int64(amd64defs.LINUX_REBOOT_MAGIC2),
							// 	ir.Int64(amd64defs.LINUX_REBOOT_CMD_POWER_OFF),
							// 	ir.Int64(0),
							// ),
						},
					},
				}, nil
			case hv.ArchitectureARM64:
				return &ir.Program{
					Entrypoint: "main",
					Methods: map[string]ir.Method{
						"main": {
							ir.Printf("Hello, World\n"),
							ir.Syscall(
								defs.SYS_REBOOT,
								ir.Int64(amd64defs.LINUX_REBOOT_MAGIC1),
								ir.Int64(amd64defs.LINUX_REBOOT_MAGIC2),
								ir.Int64(amd64defs.LINUX_REBOOT_CMD_RESTART),
								ir.Int64(0),
							),
						},
					},
				}, nil
			case hv.ArchitectureRISCV64:
				return &ir.Program{
					Entrypoint: "main",
					Methods: map[string]ir.Method{
						"main": {
							ir.Printf("Hello, World\n"),
							ir.Syscall(
								defs.SYS_REBOOT,
								ir.Int64(amd64defs.LINUX_REBOOT_MAGIC1),
								ir.Int64(amd64defs.LINUX_REBOOT_MAGIC2),
								ir.Int64(amd64defs.LINUX_REBOOT_CMD_RESTART),
								ir.Int64(0),
							),
						},
					},
				}, nil
			default:
				return nil, fmt.Errorf("unsupported architecture for init program: %s", arch)
			}
		},

		SerialStdout: os.Stdout,

		Devices: devices,
	}

	var vm hv.VirtualMachine
	if err := runWithTiming("Create Virtual Machine", func() error {
		var err error
		vm, err = q.dev.NewVirtualMachine(loader)
		return err
	}); err != nil {
		return fmt.Errorf("create virtual machine: %w", err)
	}
	defer vm.Close()

	var run hv.RunConfig
	if err := runWithTiming("Prepare Run Config", func() error {
		var err error
		run, err = loader.RunConfig()
		return err
	}); err != nil {
		return fmt.Errorf("prepare linux boot: %w", err)
	}

	if err := runWithTiming("Run Linux VM", func() error {
		return vm.Run(context.Background(), run)
	}); err != nil {
		return fmt.Errorf("run linux boot: %w", err)
	}

	if !bytes.Contains(buf.Bytes(), []byte("Hello, World")) {
		return fmt.Errorf("linux did not print expected output")
	}

	slog.Info("Linux Boot Completed Successfully")

	return nil
}

func RunInitX(debug bool) error {
	slog.Info("Starting Bringup Quest: InitX Boot")

	hv, err := factory.Open()
	if err != nil {
		return fmt.Errorf("open hypervisor factory: %w", err)
	}
	defer hv.Close()

	kernel, err := kernel.LoadForArchitecture(hv.Architecture())
	if err != nil {
		return fmt.Errorf("load kernel for architecture %s: %w", hv.Architecture(), err)
	}

	vm, err := initx.NewVirtualMachine(hv, 1, 256, kernel,
		initx.WithDebugLogging(debug),
	)
	if err != nil {
		return fmt.Errorf("create initx virtual machine: %w", err)
	}
	defer vm.Close()

	for i := range 10 {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)

		err := vm.Run(
			ctx,
			&ir.Program{
				Entrypoint: "main",
				Methods: map[string]ir.Method{
					"main": {
						ir.Printf(fmt.Sprintf("initx-bringup-quest-%d\n", i)),
						ir.Return(ir.Int64(0)),
					},
				},
			},
		)
		cancel()
		if err != nil {
			return fmt.Errorf("run initx virtual machine: %w", err)
		}
	}

	slog.Info("InitX Bringup Quest Completed Successfully")

	return nil
}

func RunExecutable(path string) error {
	slog.Info("Starting Bringup Quest: Run Executable", "path", path)

	hv, err := factory.Open()
	if err != nil {
		return fmt.Errorf("open hypervisor factory: %w", err)
	}
	defer hv.Close()

	kernel, err := kernel.LoadForArchitecture(hv.Architecture())
	if err != nil {
		return fmt.Errorf("load kernel for architecture %s: %w", hv.Architecture(), err)
	}

	fileData, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read executable file: %w", err)
	}

	vm, err := initx.NewVirtualMachine(hv, 1, 256, kernel,
		initx.WithFileFromBytes("/initx-exec", fileData, fs.FileMode(0755)),
	)
	if err != nil {
		return fmt.Errorf("create initx virtual machine: %w", err)
	}
	defer vm.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	slog.Info("Running Executable in InitX Virtual Machine", "path", path)

	if err := vm.Spawn(ctx, "/initx-exec", "-test.v"); err != nil {
		return fmt.Errorf("run executable in initx virtual machine: %w", err)
	}

	slog.Info("Executable Run Completed Successfully")

	return nil
}

func main() {
	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)

	linux := fs.Bool("linux", false, "Try booting Linux")
	initX := fs.Bool("initx", false, "Run bringup tests for initx")
	exec := fs.String("exec", "", "Run the executable using initx")
	debug := fs.Bool("debug", false, "Enable debug logging")
	hostArch, err := hostArchitecture()
	if err != nil {
		slog.Error("failed to determine host architecture", "error", err)
		os.Exit(1)
	}
	archFlag := fs.String("arch", string(hostArch), "Architecture to run (x86_64|arm64|riscv64|all)")

	if err := fs.Parse(os.Args[1:]); err != nil {
		slog.Error("failed to parse flags", "error", err)
		os.Exit(1)
	}

	selectedArch, err := parseArchitecture(*archFlag)
	if err != nil {
		slog.Error("invalid architecture", "arch", *archFlag, "error", err)
		os.Exit(1)
	}
	if selectedArch != hv.ArchitectureInvalid &&
		selectedArch != hv.ArchitectureRISCV64 &&
		selectedArch != hostArch {
		slog.Error("requested architecture not supported on this host", "arch", selectedArch, "host", hostArch)
		os.Exit(1)
	}

	q := &bringUpQuest{
		architecture: selectedArch,
	}

	if *initX {
		if err := RunInitX(*debug); err != nil {
			slog.Error("failed bringup quest initx", "error", err)
			os.Exit(1)
		}
		return
	}

	if *exec != "" {
		if err := RunExecutable(*exec); err != nil {
			slog.Error("failed to run executable", "error", err)
			os.Exit(1)
		}
		return
	}

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
