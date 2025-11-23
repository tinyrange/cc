//go:build linux

package kvm

import (
	"testing"

	"github.com/tinyrange/cc/internal/hv"
)

func checkKVMAvailable(t testing.TB) {
	t.Helper()

	hv, err := Open()
	if err != nil {
		t.Skipf("KVM not available: %v", err)
	}
	if err := hv.Close(); err != nil {
		t.Fatalf("Close KVM hypervisor: %v", err)
	}
}

func TestOpen(t *testing.T) {
	checkKVMAvailable(t)

	hv, err := Open()
	if err != nil {
		t.Fatalf("Open KVM hypervisor: %v", err)
	}

	if err := hv.Close(); err != nil {
		t.Fatalf("Close KVM hypervisor: %v", err)
	}
}

func TestNewVirtualMachine(t *testing.T) {
	checkKVMAvailable(t)

	kvm, err := Open()
	if err != nil {
		t.Fatalf("Open KVM hypervisor: %v", err)
	}
	defer kvm.Close()

	vm, err := kvm.NewVirtualMachine(hv.SimpleVMConfig{
		NumCPUs: 1,
		MemSize: 0x200000,
		MemBase: 0,
	})
	if err != nil {
		t.Fatalf("Create KVM virtual machine: %v", err)
	}

	if err := vm.Close(); err != nil {
		t.Fatalf("Close KVM virtual machine: %v", err)
	}
}
