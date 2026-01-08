package bench

import (
	"context"
	"errors"
	"testing"

	"github.com/tinyrange/cc/internal/asm"
	"github.com/tinyrange/cc/internal/asm/amd64"
	"github.com/tinyrange/cc/internal/asm/arm64"
	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/hv/factory"
	"github.com/tinyrange/cc/internal/hv/helpers"
	"github.com/tinyrange/cc/internal/ir"

	_ "github.com/tinyrange/cc/internal/ir/amd64"
	_ "github.com/tinyrange/cc/internal/ir/arm64"
)

const (
	psciSystemOff = 0x84000008
)

func BenchmarkVMExit(b *testing.B) {
	hyper, err := factory.Open()
	if err != nil {
		b.Skipf("Open hypervisor: %v", err)
	}
	defer hyper.Close()

	var prog ir.Program
	switch hyper.Architecture() {
	case hv.ArchitectureARM64:
		prog = ir.Program{
			Entrypoint: "main",
			Methods: map[string]ir.Method{
				"main": {
					asm.Group{
						arm64.MovImmediate(arm64.Reg64(arm64.X0), psciSystemOff),
						arm64.Hvc(),
					},
				},
			},
		}
	case hv.ArchitectureX86_64:
		prog = ir.Program{
			Entrypoint: "main",
			Methods: map[string]ir.Method{
				"main": {
					asm.Group{
						amd64.Hlt(),
					},
				},
			},
		}
	default:
		b.Skipf("Unsupported architecture: %v", hyper.Architecture())
	}

	loader := helpers.ProgramLoader{
		Program:  prog,
		BaseAddr: 0x100000,
		Mode:     helpers.Mode64BitIdentityMapping,
	}

	vm, err := hyper.NewVirtualMachine(hv.SimpleVMConfig{
		NumCPUs: 1,
		MemSize: 64 * 1024 * 1024,
		MemBase: 0x100000,

		VMLoader: &loader,
	})
	if err != nil {
		b.Fatalf("Create virtual machine: %v", err)
	}
	defer vm.Close()

	for b.Loop() {
		err := vm.Run(context.Background(), &loader)
		if err != nil {
			if errors.Is(err, hv.ErrVMHalted) {
				continue
			}
			b.Fatalf("Run virtual machine: %v", err)
		}
	}
}

func BenchmarkCaptureSnapshot(b *testing.B) {
	hyper, err := factory.Open()
	if err != nil {
		b.Skipf("Open hypervisor: %v", err)
	}
	defer hyper.Close()

	vm, err := hyper.NewVirtualMachine(hv.SimpleVMConfig{
		NumCPUs:          1,
		MemSize:          64 * 1024 * 1024, // 64 MiB
		MemBase:          0x80000000,
		InterruptSupport: true,
	})
	if err != nil {
		b.Skipf("Create VM: %v", err)
	}
	defer vm.Close()

	b.ResetTimer()
	for b.Loop() {
		snap, err := vm.CaptureSnapshot()
		if err != nil {
			b.Fatalf("CaptureSnapshot: %v", err)
		}
		_ = snap
	}
}

func BenchmarkRestoreSnapshot(b *testing.B) {
	hyper, err := factory.Open()
	if err != nil {
		b.Skipf("Open hypervisor: %v", err)
	}
	defer hyper.Close()

	vm, err := hyper.NewVirtualMachine(hv.SimpleVMConfig{
		NumCPUs:          1,
		MemSize:          64 * 1024 * 1024, // 64 MiB
		MemBase:          0x80000000,
		InterruptSupport: true,
	})
	if err != nil {
		b.Skipf("Create VM: %v", err)
	}
	defer vm.Close()

	snap, err := vm.CaptureSnapshot()
	if err != nil {
		b.Fatalf("CaptureSnapshot: %v", err)
	}

	b.ResetTimer()
	for b.Loop() {
		if err := vm.RestoreSnapshot(snap); err != nil {
			b.Fatalf("RestoreSnapshot: %v", err)
		}
	}
}

func BenchmarkSnapshotRoundtrip(b *testing.B) {
	hyper, err := factory.Open()
	if err != nil {
		b.Skipf("Open hypervisor: %v", err)
	}
	defer hyper.Close()

	vm, err := hyper.NewVirtualMachine(hv.SimpleVMConfig{
		NumCPUs:          1,
		MemSize:          64 * 1024 * 1024, // 64 MiB
		MemBase:          0x80000000,
		InterruptSupport: true,
	})
	if err != nil {
		b.Skipf("Create VM: %v", err)
	}
	defer vm.Close()

	b.ResetTimer()
	for b.Loop() {
		snap, err := vm.CaptureSnapshot()
		if err != nil {
			b.Fatalf("CaptureSnapshot: %v", err)
		}
		if err := vm.RestoreSnapshot(snap); err != nil {
			b.Fatalf("RestoreSnapshot: %v", err)
		}
	}
}
