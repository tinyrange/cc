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
		VsockPort:   1024,
		ReadyMarker: vmruntime.InstanceReadyMarker,
	})
	if err != nil {
		t.Fatalf("BuildInitramfs() error = %v", err)
	}
	serial, control, err := BootInitramfsToVsockMarker(ctx, kernelFile, initrd, 256, true, 1024, vmruntime.InstanceReadyMarker)
	if err != nil {
		t.Fatalf("BootInitramfsToVsockMarker() error = %v\nserial:\n%s\ncontrol:\n%s", err, serial, control)
	}
	if !strings.Contains(control, vmruntime.InstanceReadyMarker) {
		t.Fatalf("control missing ready marker %q:\nserial:\n%s\ncontrol:\n%s", vmruntime.InstanceReadyMarker, serial, control)
	}
}
