//go:build linux && arm64

package kvm

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"j5.nz/cc/internal/arm64vm"
	"j5.nz/cc/internal/guestinit"
	"j5.nz/cc/internal/imagefs"
	"j5.nz/cc/internal/kernel/alpine"
	"j5.nz/cc/internal/virtio"
	"j5.nz/cc/internal/vmruntime"
)

func TestGuestInitMountsRootFS(t *testing.T) {
	if os.Getenv("CCX3_KVM_BOOT") == "" {
		t.Skip("set CCX3_KVM_BOOT=1 to run the linux arm64 rootfs probe")
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
		[]string{"CONFIG_VIRTIO_MMIO", "CONFIG_FUSE_FS", "CONFIG_VIRTIO_FS"},
		map[string]string{
			"CONFIG_VIRTIO_MMIO": "kernel/drivers/virtio/virtio_mmio.ko.gz",
			"CONFIG_FUSE_FS":     "kernel/fs/fuse/fuse.ko.gz",
			"CONFIG_VIRTIO_FS":   "kernel/fs/fuse/virtiofs.ko.gz",
		},
	)
	if err != nil {
		t.Fatalf("PlanModuleLoad() error = %v", err)
	}

	rootDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootDir, "hello.txt"), []byte("hello from rootfs\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(hello.txt) error = %v", err)
	}

	initBin, err := guestinit.Build(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("guestinit.Build() error = %v", err)
	}

	const readyMarker = "__CCX3_READY__"
	initrd, err := vmruntime.BuildInitramfs(initBin, modules, vmruntime.GuestInitConfig{
		Modules:     vmruntime.ModulePaths(modules),
		RootFSTag:   vmruntime.RootFSTag,
		ReadyMarker: readyMarker,
		BeginMarker: vmruntime.CommandBeginMarker,
	})
	if err != nil {
		t.Fatalf("BuildInitramfs() error = %v", err)
	}

	fsdevs, _, err := arm64vm.BuildFSDevices(vmruntime.RunRequest{
		RootFS: virtio.NewImageFS(imagefs.NewHostFS(rootDir, nil), rootDir),
	}, nil)
	if err != nil {
		t.Fatalf("BuildFSDevices() error = %v", err)
	}

	serial, err := BootInitramfsToMarkerWithFS(ctx, kernelFile, initrd, 256, true, readyMarker, fsdevs)
	if err != nil {
		t.Fatalf("BootInitramfsToMarkerWithFS() error = %v\nserial:\n%s", err, serial)
	}
	for _, want := range []string{
		"mounting rootfs",
		"rootfs mounted",
		readyMarker,
	} {
		if !strings.Contains(serial, want) {
			t.Fatalf("serial output did not contain %q\nserial:\n%s", want, serial)
		}
	}
}
