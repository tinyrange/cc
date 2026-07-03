//go:build linux && arm64

package hv

import (
	"fmt"

	"j5.nz/cc/internal/arm64vm"
	"j5.nz/cc/internal/hv/kvm"
)

func newAnonymousMemoryBenchmarkVM(memorySize uint64) (*memoryBenchmarkVM, error) {
	vm, err := kvm.NewVM()
	if err != nil {
		return nil, err
	}
	mem, err := vm.MapAnonymousMemory(memorySize, memoryBenchmarkBase)
	if err != nil {
		_ = vm.Close()
		return nil, err
	}
	return newKVMMemoryBenchmarkVM(vm, mem), nil
}

func newMappedMemoryBenchmarkVM(mem []byte) (*memoryBenchmarkVM, error) {
	vm, err := kvm.NewVM()
	if err != nil {
		return nil, err
	}
	if err := vm.MapMemoryAlias(0, memoryBenchmarkBase, mem); err != nil {
		_ = vm.Close()
		return nil, err
	}
	return newKVMMemoryBenchmarkVM(vm, mem), nil
}

func newKVMMemoryBenchmarkVM(vm *kvm.VM, mem []byte) *memoryBenchmarkVM {
	return &memoryBenchmarkVM{
		memory: mem,
		close:  vm.Close,
		setEntry: func(entry, stackTop uint64) error {
			if err := vm.SetPC(entry); err != nil {
				return fmt.Errorf("set PC: %w", err)
			}
			if err := vm.SetPState(arm64vm.DefaultPStateBits); err != nil {
				return fmt.Errorf("set PSTATE: %w", err)
			}
			if err := vm.SetSpEl1(stackTop); err != nil {
				return fmt.Errorf("set SP_EL1: %w", err)
			}
			return nil
		},
		runUntilExit: func() error {
			var exit kvm.Exit
			if err := vm.Run(&exit); err != nil {
				return err
			}
			if exit.Reason != kvm.ExitMMIO {
				return fmt.Errorf("unexpected VM exit reason %d", exit.Reason)
			}
			if exit.MMIO.Addr != memoryBenchmarkExitAddr || !exit.MMIO.Write {
				return fmt.Errorf("unexpected MMIO exit addr=%#x write=%t", exit.MMIO.Addr, exit.MMIO.Write)
			}
			return nil
		},
	}
}
