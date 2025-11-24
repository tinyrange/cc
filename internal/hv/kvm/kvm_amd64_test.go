//go:build linux && amd64

package kvm

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/tinyrange/cc/internal/asm/amd64"
	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/hv/helpers"
	"github.com/tinyrange/cc/internal/ir"
	_ "github.com/tinyrange/cc/internal/ir/amd64"
)

func TestRunSimpleHalt(t *testing.T) {
	checkKVMAvailable(t)

	kvm, err := Open()
	if err != nil {
		t.Fatalf("Open KVM hypervisor: %v", err)
	}
	defer kvm.Close()

	loader := helpers.ProgramLoader{
		Program: ir.Program{
			Entrypoint: "main",
			Methods: map[string]ir.Method{
				"main": {
					amd64.Hlt(),
				},
			},
		},
		BaseAddr:          0x100000,
		Mode:              helpers.ModeProtectedMode,
		MaxLoopIterations: 1,
	}

	vm, err := kvm.NewVirtualMachine(hv.SimpleVMConfig{
		NumCPUs: 1,
		MemSize: 0x200000,
		MemBase: 0x100000,

		VMLoader: &loader,
	})
	if err != nil {
		t.Fatalf("Create KVM virtual machine: %v", err)
	}
	defer vm.Close()

	err = vm.Run(context.Background(), &loader)
	if !errors.Is(err, hv.ErrVMHalted) {
		t.Fatalf("Run KVM virtual machine: %v", err)
	}
}

func TestRunSimpleAddition(t *testing.T) {
	checkKVMAvailable(t)

	kvm, err := Open()
	if err != nil {
		t.Fatalf("Open KVM hypervisor: %v", err)
	}
	defer kvm.Close()

	loader := helpers.ProgramLoader{
		Program: ir.Program{
			Entrypoint: "main",
			Methods: map[string]ir.Method{
				"main": {
					amd64.MovImmediate(amd64.Reg32(amd64.RAX), 40),
					amd64.AddRegImm(amd64.Reg32(amd64.RAX), 2),
					amd64.Hlt(),
				},
			},
		},
		BaseAddr:          0x100000,
		Mode:              helpers.Mode64BitIdentityMapping,
		MaxLoopIterations: 1,
	}

	vm, err := kvm.NewVirtualMachine(hv.SimpleVMConfig{
		NumCPUs: 1,
		MemSize: 0x200000,
		MemBase: 0x100000,

		VMLoader: &loader,
	})
	if err != nil {
		t.Fatalf("Create KVM virtual machine: %v", err)
	}
	defer vm.Close()

	err = vm.Run(context.Background(), &loader)
	if !errors.Is(err, hv.ErrVMHalted) {
		t.Fatalf("Run KVM virtual machine: %v", err)
	}

	if err := vm.VirtualCPUCall(0, func(vcpu hv.VirtualCPU) error {
		regs := map[hv.Register]hv.RegisterValue{
			hv.RegisterAMD64Rax: hv.Register64(0),
		}

		if err := vcpu.GetRegisters(regs); err != nil {
			return fmt.Errorf("Get RAX register: %w", err)
		}

		rax := uint64(regs[hv.RegisterAMD64Rax].(hv.Register64))
		if rax != 42 {
			return fmt.Errorf("unexpected RAX value: got %d, want 42", rax)
		}

		return nil
	}); err != nil {
		t.Fatalf("Sync vCPU registers: %v", err)
	}
}
