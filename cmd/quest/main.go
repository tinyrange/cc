package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/tinyrange/cc/internal/asm/amd64"
	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/hv/factory"
	"github.com/tinyrange/cc/internal/hv/helpers"
	"github.com/tinyrange/cc/internal/ir"
	amd64ir "github.com/tinyrange/cc/internal/ir/amd64"
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

	return q.runTask(fmt.Sprintf("%s (%s)", name, arch), f)
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

	dev, err := factory.Open()
	if err != nil {
		return fmt.Errorf("open hypervisor factory: %w", err)
	}
	q.dev = dev
	defer q.dev.Close()

	if err := q.runArchitectureTask("Run amd64 VM", hv.ArchitectureX86_64, func() error {
		loader := helpers.ProgramLoader{
			Program: ir.Program{
				Entrypoint: "main",
				Methods: map[string]ir.Method{
					"main": {
						amd64.Hlt(),
					},
				},
			},
			BaseAddr: 0x100000,
			Mode:     helpers.ModeProtectedMode,
		}

		vm, err := q.dev.NewVirtualMachine(hv.SimpleVMConfig{
			NumCPUs: 1,
			MemSize: 0x200000,
			MemBase: 0x100000,

			VMLoader: &loader,
		})
		if err != nil {
			return fmt.Errorf("create KVM virtual machine: %w", err)
		}
		defer vm.Close()

		err = vm.Run(context.Background(), &loader)
		if !errors.Is(err, hv.ErrVMHalted) {
			return fmt.Errorf("run KVM virtual machine: %w", err)
		}

		return nil
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
