//go:build darwin && arm64

package hv

import (
	"context"
	"fmt"

	"j5.nz/cc/internal/hv/hvf"
)

func newAnonymousMemoryBenchmarkVM(memorySize uint64) (*memoryBenchmarkVM, error) {
	vm, err := hvf.NewVMWithOptions(context.Background(), hvf.VMOptions{CPUs: 1})
	if err != nil {
		return nil, err
	}
	mem, err := vm.MapAnonymousMemory(uintptr(memorySize), hvf.IPA(memoryBenchmarkBase), hvf.MemoryFlags(0x7))
	if err != nil {
		_ = vm.Close()
		return nil, err
	}
	return newHVFMemoryBenchmarkVM(vm, mem), nil
}

func newMappedMemoryBenchmarkVM(mem []byte) (*memoryBenchmarkVM, error) {
	vm, err := hvf.NewVMWithOptions(context.Background(), hvf.VMOptions{CPUs: 1})
	if err != nil {
		return nil, err
	}
	if err := vm.MapMemory(mem, hvf.IPA(memoryBenchmarkBase), hvf.MemoryFlags(0x7)); err != nil {
		_ = vm.Close()
		return nil, err
	}
	return newHVFMemoryBenchmarkVM(vm, mem), nil
}

func newHVFMemoryBenchmarkVM(vm *hvf.VM, mem []byte) *memoryBenchmarkVM {
	return &memoryBenchmarkVM{
		memory: mem,
		close:  vm.Close,
		setEntry: func(entry, stackTop uint64) error {
			return vm.ConfigureLinuxBootState(entry, stackTop, 0)
		},
		runUntilExit: func() error {
			exit, err := vm.Run()
			if err != nil {
				return err
			}
			if hvf.DecodeExceptionClass(exit.Exception.Syndrome) != hvf.ExceptionClassDataAbortLowerEL {
				return fmt.Errorf("unexpected VM exception syndrome=%#x", exit.Exception.Syndrome)
			}
			abort, err := hvf.DecodeDataAbort(exit.Exception.Syndrome)
			if err != nil {
				return err
			}
			if !abort.Write || uint64(exit.Exception.PhysicalAddress) != memoryBenchmarkExitAddr {
				return fmt.Errorf("unexpected data abort addr=%#x write=%t", exit.Exception.PhysicalAddress, abort.Write)
			}
			return nil
		},
	}
}
