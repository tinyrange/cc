package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/tinyrange/cc/internal/asm"
	"github.com/tinyrange/cc/internal/asm/amd64"
	"github.com/tinyrange/cc/internal/asm/arm64"
	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/hv/factory"
	"github.com/tinyrange/cc/internal/hv/helpers"
	"github.com/tinyrange/cc/internal/ir"
	amd64ir "github.com/tinyrange/cc/internal/ir/amd64"
	arm64ir "github.com/tinyrange/cc/internal/ir/arm64"
)

type bringUpQuest struct {
	dev hv.Hypervisor
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

	if q.dev.Architecture() != arch {
		slog.Info("Skipping task for unsupported architecture", "name", name, "arch", arch)
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

func (q *bringUpQuest) Run() error {
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

	result := make([]byte, 0, 64)
	if err := q.runVMTask("I/O Test", hv.ArchitectureX86_64, ir.Program{
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
	}, func(cpu hv.VirtualCPU) error {
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

	// Test simple MMIO
	result = make([]byte, 0, 64)

	if err := q.runVMTask("MMIO Test", hv.ArchitectureX86_64, ir.Program{
		Entrypoint: "main",
		Methods: map[string]ir.Method{
			"main": {
				asm.Group{
					amd64.LoadConstantBytes(dataMessage, append([]byte("Hello, World!"), 0)),
					amd64.LoadAddress(amd64.Reg64(amd64.RSI), dataMessage),
					amd64.MovImmediate(amd64.Reg64(amd64.RDX), 0xdead0000),
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
		if !bytes.Equal(result, []byte("Hello, World!")) {
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

	slog.Info("Bringup Quest Completed")

	return nil
}

func main() {
	q := &bringUpQuest{}

	if err := q.Run(); err != nil {
		slog.Error("failed bringup quest", "error", err)
		os.Exit(1)
	}
}
