//go:build windows && amd64

package whp

import (
	"context"
	"os"
	"strings"
	"testing"

	"j5.nz/cc/internal/guestinit"
	"j5.nz/cc/internal/kernel/alpine"
	"j5.nz/cc/internal/vmruntime"
)

func TestKernelBootFirstSerialByte(t *testing.T) {
	if os.Getenv("CCX3_WHP_BOOT") == "" {
		t.Skip("set CCX3_WHP_BOOT=1 to run the windows amd64 WHP boot probe")
	}
	ctx, cancel := context.WithTimeout(context.Background(), whpBootTestTimeout(t))
	defer cancel()
	manager := alpine.NewManager(t.TempDir())
	if err := manager.Ensure(ctx); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	kernelFile, err := manager.ReadKernel()
	if err != nil {
		t.Fatalf("ReadKernel() error = %v", err)
	}
	serial, err := BootKernelToSerialWithTimeout(kernelFile, 256, true, whpBootTestTimeout(t))
	if err != nil {
		t.Fatalf("BootKernelToSerialWithTimeout() error = %v\nserial:\n%s", err, serial)
	}
	if serial == "" {
		t.Fatal("BootKernelToSerialWithTimeout() produced no serial output")
	}
	t.Logf("first serial output: %q", serial)
}

func TestInitramfsBootReadyMarker(t *testing.T) {
	if os.Getenv("CCX3_WHP_BOOT") == "" {
		t.Skip("set CCX3_WHP_BOOT=1 to run the windows amd64 WHP boot probe")
	}
	ctx, cancel := context.WithTimeout(context.Background(), whpBootTestTimeout(t))
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
