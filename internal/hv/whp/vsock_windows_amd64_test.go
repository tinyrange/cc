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

func TestGuestInitReadyMarkerOverVsock(t *testing.T) {
	if os.Getenv("CCX3_WHP_BOOT") == "" {
		t.Skip("set CCX3_WHP_BOOT=1 to run the windows amd64 WHP vsock probe")
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

	initBin, err := guestinit.BuildForArch(ctx, t.TempDir(), "amd64")
	if err != nil {
		t.Fatalf("guestinit.BuildForArch() error = %v", err)
	}
	const readyMarker = "__CCX3_WHP_VSOCK_READY__"
	initrd, err := vmruntime.BuildInitramfs(initBin, modules, vmruntime.GuestInitConfig{
		Modules:            vmruntime.ModulePaths(modules),
		VsockPort:          vmruntime.ControlPort,
		ReadyMarker:        readyMarker,
		BeginMarker:        vmruntime.CommandBeginMarker,
		DisableCgroupMount: true,
	})
	if err != nil {
		t.Fatalf("BuildInitramfs() error = %v", err)
	}

	serial, control, err := BootInitramfsToVsockMarker(ctx, kernelFile, initrd, 256, true, vmruntime.ControlPort, readyMarker)
	if err != nil {
		t.Fatalf("BootInitramfsToVsockMarker() error = %v\nserial:\n%s\ncontrol:\n%s", err, serial, control)
	}
	if !strings.Contains(control, readyMarker) {
		t.Fatalf("control output missing ready marker %q\nserial:\n%s\ncontrol:\n%s", readyMarker, serial, control)
	}
}
