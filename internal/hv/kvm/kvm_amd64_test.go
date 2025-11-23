//go:build linux && amd64

package kvm

import (
	"context"
	"errors"
	"testing"

	"github.com/tinyrange/cc/internal/asm/amd64"
	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/hv/helpers"
	"github.com/tinyrange/cc/internal/ir"
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
		BaseAddr: 0x100000,
		Mode:     helpers.ModeProtectedMode,
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
