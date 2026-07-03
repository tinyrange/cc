//go:build windows && arm64

package hv

import (
	"fmt"

	"j5.nz/cc/internal/arm64vm"
	"j5.nz/cc/internal/hv/whp"
)

func newAnonymousMemoryBenchmarkVM(memorySize uint64) (*memoryBenchmarkVM, error) {
	vm, err := whp.NewVM(memorySize, memoryBenchmarkBase)
	if err != nil {
		return nil, err
	}
	return newWHPMemoryBenchmarkVM(vm), nil
}

func newMappedMemoryBenchmarkVM(mem []byte) (*memoryBenchmarkVM, error) {
	vm, err := whp.NewVMWithMemory(mem, memoryBenchmarkBase)
	if err != nil {
		return nil, err
	}
	return newWHPMemoryBenchmarkVM(vm), nil
}

func newWHPMemoryBenchmarkVM(vm *whp.VM) *memoryBenchmarkVM {
	return &memoryBenchmarkVM{
		memory: vm.Memory(),
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
			var exit whp.Exit
			if err := vm.Run(&exit); err != nil {
				return err
			}
			if exit.MMIO.Addr != memoryBenchmarkExitAddr || !exit.MMIO.Write {
				return fmt.Errorf("unexpected MMIO exit addr=%#x write=%t", exit.MMIO.Addr, exit.MMIO.Write)
			}
			return nil
		},
	}
}
