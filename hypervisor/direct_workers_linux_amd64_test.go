//go:build linux && amd64

package hypervisor

import (
	"context"
	"os"
	"testing"
)

func TestDirectWorkersReuseOwner(t *testing.T) {
	if _, err := os.Stat("/dev/kvm"); err != nil {
		t.Skip("KVM unavailable")
	}
	workers, err := NewDirectWorkers(1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = workers.Close() })
	newCPU := func() *directCPU {
		t.Helper()
		cpu, err := workers.NewCPU(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = cpu.Close() })
		return cpu.(*directCPU)
	}
	first, second := newCPU(), newCPU()
	if first.RunSlot() != second.RunSlot() {
		t.Fatal("VMs on one worker have different execution slots")
	}
	if err := workers.Close(); err == nil {
		t.Fatal("closed a pool with live VMs")
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := second.Registers(); err != nil {
		t.Fatalf("closing the first VM stopped its shared worker: %v", err)
	}
}
