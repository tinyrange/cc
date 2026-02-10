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

func TestNewVirtualMachineMultiCPU(t *testing.T) {
	checkKVMAvailable(t)

	kvm, err := Open()
	if err != nil {
		t.Fatalf("Open KVM hypervisor: %v", err)
	}
	defer kvm.Close()

	for _, numCPUs := range []int{2, 4, 8} {
		t.Run("CPUs="+string(rune('0'+numCPUs)), func(t *testing.T) {
			vm, err := kvm.NewVirtualMachine(hv.SimpleVMConfig{
				NumCPUs: numCPUs,
				MemSize: 0x200000,
				MemBase: 0,
			})
			if err != nil {
				t.Fatalf("Create KVM virtual machine with %d CPUs: %v", numCPUs, err)
			}
			defer vm.Close()

			// Verify each vCPU exists and has correct ID
			for i := 0; i < numCPUs; i++ {
				err := vm.VirtualCPUCall(i, func(vcpu hv.VirtualCPU) error {
					if vcpu.ID() != i {
						t.Errorf("vCPU %d has wrong ID: got %d", i, vcpu.ID())
					}
					return nil
				})
				if err != nil {
					t.Errorf("VirtualCPUCall(%d) failed: %v", i, err)
				}
			}
		})
	}
}
