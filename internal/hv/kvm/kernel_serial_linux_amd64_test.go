//go:build linux && amd64

package kvm

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"j5.nz/cc/internal/guestinit"
	"j5.nz/cc/internal/kernel/alpine"
	"j5.nz/cc/internal/vmruntime"
)

func TestKernelBootSerial(t *testing.T) {
	if os.Getenv("CCX3_KVM_BOOT") == "" {
		t.Skip("set CCX3_KVM_BOOT=1 to run the linux amd64 KVM boot probe")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	manager := alpine.NewManager(t.TempDir())
	if err := manager.Ensure(ctx); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	kernelFile, err := manager.ReadKernel()
	if err != nil {
		t.Fatalf("ReadKernel() error = %v", err)
	}
	serial, err := BootKernelToSerialWithTimeout(kernelFile, 256, true, 30*time.Second)
	if err != nil {
		t.Fatalf("BootKernelToSerialWithTimeout() error = %v\nserial:\n%s", err, serial)
	}
	if serial == "" {
		t.Fatal("BootKernelToSerialWithTimeout() produced no serial output")
	}
	t.Logf("serial output:\n%s", serial)
}

func TestInitramfsBootReadyMarker(t *testing.T) {
	if os.Getenv("CCX3_KVM_BOOT") == "" {
		t.Skip("set CCX3_KVM_BOOT=1 to run the linux amd64 KVM boot probe")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	manager := alpine.NewManager(t.TempDir())
	if err := manager.Ensure(ctx); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	kernelFile, err := manager.ReadKernel()
	if err != nil {
		t.Fatalf("ReadKernel() error = %v", err)
	}
	initBin, err := guestinit.BuildForArch(ctx, t.TempDir(), "amd64")
	if err != nil {
		t.Fatalf("guestinit.BuildForArch() error = %v", err)
	}
	initrd, err := vmruntime.BuildInitramfs(initBin, nil, vmruntime.GuestInitConfig{
		ReadyMarker: vmruntime.InstanceReadyMarker,
	})
	if err != nil {
		t.Fatalf("BuildInitramfs() error = %v", err)
	}
	serial, err := BootInitramfsToMarker(ctx, kernelFile, initrd, 256, true, vmruntime.InstanceReadyMarker)
	if err != nil {
		t.Fatalf("BootInitramfsToMarker() error = %v\nserial:\n%s", err, serial)
	}
	if !strings.Contains(serial, vmruntime.InstanceReadyMarker) {
		t.Fatalf("serial missing ready marker %q:\n%s", vmruntime.InstanceReadyMarker, serial)
	}
}

func TestProbeReportsVMCreation(t *testing.T) {
	info, err := Probe()
	if err != nil {
		if strings.Contains(err.Error(), "/dev/kvm") ||
			strings.Contains(err.Error(), "inappropriate ioctl") ||
			strings.Contains(err.Error(), "invalid argument") {
			t.Skip(err)
		}
		t.Fatalf("Probe() error = %v", err)
	}
	if !info.VMCreateOK || !info.VCPUCreateOK || !info.VCPUInitOK {
		t.Fatalf("Probe() incomplete: %+v", info)
	}
}

func TestSplitGuestMemoryMapping(t *testing.T) {
	if os.Getenv("CCX3_KVM_BOOT") == "" {
		t.Skip("set CCX3_KVM_BOOT=1 to run the linux amd64 KVM split-memory probe")
	}

	vm, err := NewVM()
	if err != nil {
		t.Fatalf("NewVM() error = %v", err)
	}
	defer vm.Close()

	mem, err := mapAMD64GuestMemory(vm, 4096)
	if err != nil {
		t.Fatalf("mapAMD64GuestMemory() error = %v", err)
	}
	if got, want := vm.lowMemLimit, uint64(3<<30); got != want {
		t.Fatalf("vm.lowMemLimit = %#x, want %#x", got, want)
	}
	if len(vm.regions) != 1 {
		t.Fatalf("len(vm.regions) = %d, want 1", len(vm.regions))
	}
	if vm.regions[0].guestPhysAddr != 4<<30 {
		t.Fatalf("high region guestPhysAddr = %#x, want %#x", vm.regions[0].guestPhysAddr, uint64(4<<30))
	}
	if got, want := len(mem), 4<<30; got != want {
		t.Fatalf("len(mem) = %#x, want %#x", got, want)
	}
	if _, _, ok := vm.findMemoryRegion((3 << 30) + (1 << 20)); ok {
		t.Fatal("PCI hole address unexpectedly resolved to guest memory")
	}
	if _, _, ok := vm.findMemoryRegion((4 << 30) + (1 << 20)); !ok {
		t.Fatal("high memory address did not resolve to guest memory")
	}
}
