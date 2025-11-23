//go:build linux

package kvm

import (
	"testing"

	"github.com/tinyrange/cc/internal/hv"
)

func BenchmarkKVMOpen(b *testing.B) {
	checkKVMAvailable(b)

	for i := 0; i < b.N; i++ {
		hv, err := Open()
		if err != nil {
			b.Fatalf("Open KVM hypervisor: %v", err)
		}
		if err := hv.Close(); err != nil {
			b.Fatalf("Close KVM hypervisor: %v", err)
		}
	}
}

func BenchmarkKVMNewVirtualMachine(b *testing.B) {
	checkKVMAvailable(b)

	kvm, err := Open()
	if err != nil {
		b.Fatalf("Open KVM hypervisor: %v", err)
	}
	defer kvm.Close()

	for b.Loop() {
		vm, err := kvm.NewVirtualMachine(hv.SimpleVMConfig{
			NumCPUs: 1,
			MemSize: 64 * 1024 * 1024,
			MemBase: 0x100000,
		})
		if err != nil {
			b.Fatalf("Create KVM virtual machine: %v", err)
		}

		if err := vm.Close(); err != nil {
			b.Fatalf("Close KVM virtual machine: %v", err)
		}
	}
}
