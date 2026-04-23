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

func TestGuestInitReadyMarkerOverVsock(t *testing.T) {
	if os.Getenv("CCX3_KVM_BOOT") == "" {
		t.Skip("set CCX3_KVM_BOOT=1 to run the linux arm64 vsock probe")
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

	modules, err := manager.PlanModuleLoad(
		[]string{"CONFIG_VIRTIO_MMIO", "CONFIG_VSOCKETS", "CONFIG_VIRTIO_VSOCKETS"},
		map[string]string{
			"CONFIG_VIRTIO_MMIO":     "kernel/drivers/virtio/virtio_mmio.ko.gz",
			"CONFIG_VSOCKETS":        "kernel/net/vmw_vsock/vsock.ko.gz",
			"CONFIG_VIRTIO_VSOCKETS": "kernel/net/vmw_vsock/vmw_vsock_virtio_transport.ko.gz",
		},
	)
	if err != nil {
		t.Fatalf("PlanModuleLoad() error = %v", err)
	}

	initBin, err := guestinit.Build(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("guestinit.Build() error = %v", err)
	}

	const readyMarker = "__CCX3_READY__"
	initrd, err := vmruntime.BuildInitramfs(initBin, modules, vmruntime.GuestInitConfig{
		Modules:     vmruntime.ModulePaths(modules),
		VsockPort:   vmruntime.ControlPort,
		ReadyMarker: readyMarker,
		BeginMarker: vmruntime.CommandBeginMarker,
	})
	if err != nil {
		t.Fatalf("BuildInitramfs() error = %v", err)
	}

	serial, control, err := BootInitramfsToVsockMarkerWithFS(ctx, kernelFile, initrd, 256, true, vmruntime.ControlPort, readyMarker, nil)
	if err != nil {
		t.Fatalf("BootInitramfsToVsockMarkerWithFS() error = %v\nserial:\n%s\ncontrol:\n%s", err, serial, control)
	}
	if !strings.Contains(control, readyMarker) {
		t.Fatalf("control output did not contain ready marker\nserial:\n%s\ncontrol:\n%s", serial, control)
	}
}
