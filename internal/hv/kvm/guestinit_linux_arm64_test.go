//go:build linux && arm64

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

func TestGuestInitReadyMarker(t *testing.T) {
	if os.Getenv("CCX3_KVM_BOOT") == "" {
		t.Skip("set CCX3_KVM_BOOT=1 to run the linux arm64 guest-init probe")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	kernelRoot := t.TempDir()
	manager := alpine.NewManager(kernelRoot)
	if err := manager.Ensure(ctx); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	kernelFile, err := manager.ReadKernel()
	if err != nil {
		t.Fatalf("ReadKernel() error = %v", err)
	}

	initBin, err := guestinit.Build(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("guestinit.Build() error = %v", err)
	}

	const readyMarker = "__CCX3_READY__"
	initrd, err := vmruntime.BuildInitramfs(initBin, nil, vmruntime.GuestInitConfig{
		ReadyMarker: readyMarker,
		BeginMarker: vmruntime.CommandBeginMarker,
	})
	if err != nil {
		t.Fatalf("BuildInitramfs() error = %v", err)
	}

	serial, err := BootInitramfsToMarkerWithTimeout(kernelFile, initrd, 256, true, readyMarker, 30*time.Second)
	if err != nil {
		t.Fatalf("BootInitramfsToMarkerWithTimeout() error = %v\nserial:\n%s", err, serial)
	}
	if !strings.Contains(serial, readyMarker) {
		t.Fatalf("serial output did not contain ready marker\nserial:\n%s", serial)
	}
}
