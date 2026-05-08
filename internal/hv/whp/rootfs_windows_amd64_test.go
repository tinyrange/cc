//go:build windows && amd64

package whp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"j5.nz/cc/internal/amd64vm"
	"j5.nz/cc/internal/guestinit"
	"j5.nz/cc/internal/kernel/alpine"
	"j5.nz/cc/internal/oci"
	"j5.nz/cc/internal/vmruntime"
)

func TestGuestInitRunsCommandFromAlpineRootFS(t *testing.T) {
	if os.Getenv("CCX3_WHP_BOOT") == "" {
		t.Skip("set CCX3_WHP_BOOT=1 to run the windows amd64 WHP virtio-fs probe")
	}
	fixture := filepath.Join("..", "..", "..", "fixtures", "alpine.simg")
	if _, err := os.Stat(fixture); err != nil {
		t.Skipf("local alpine fixture unavailable: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), whpBootTestTimeout(t))
	defer cancel()

	root := t.TempDir()
	manager := alpine.NewManager(filepath.Join(root, "kernel"))
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

	store := oci.NewStore(filepath.Join(root, "images"))
	if _, err := store.Pull(ctx, "alpine", fixture); err != nil {
		t.Fatalf("store.Pull() error = %v", err)
	}
	img, err := store.Open("alpine")
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	fsdevs, _, err := amd64vm.BuildFSDevices(vmruntime.RunRequest{Image: img}, nil)
	if err != nil {
		t.Fatalf("BuildFSDevices() error = %v", err)
	}

	initBin, err := guestinit.BuildForArch(ctx, filepath.Join(root, "guestinit"), "amd64")
	if err != nil {
		t.Fatalf("guestinit.BuildForArch() error = %v", err)
	}
	const commandMarker = "whp-virtiofs-command-ok"
	const exitMarker = "__CCX3_WHP_VIRTIOFS_EXIT__:"
	initrd, err := vmruntime.BuildInitramfs(initBin, modules, vmruntime.GuestInitConfig{
		Command:          []string{"/bin/sh", "-c", "{ printf 'WHOWHO:'; whoami; printf 'UNAMEUNAME:'; uname -a; printf 'whp-virtiofs-%s\\n' command-ok; } > /dev/kmsg"},
		Env:              vmruntime.WithDefaultEnv(nil),
		Modules:          vmruntime.ModulePaths(modules),
		RootFSTag:        vmruntime.RootFSTag,
		BeginMarker:      vmruntime.CommandBeginMarker,
		ExitMarkerPrefix: exitMarker,
	})
	if err != nil {
		t.Fatalf("BuildInitramfs() error = %v", err)
	}

	serial, err := BootInitramfsToMarkerWithFS(ctx, kernelFile, initrd, 256, true, commandMarker, fsdevs)
	if err != nil {
		t.Fatalf("BootInitramfsToMarkerWithFS() error = %v\nserial:\n%s", err, serial)
	}
	if !strings.Contains(serial, commandMarker) {
		t.Fatalf("serial output missing command output\nserial:\n%s", serial)
	}
	if !strings.Contains(serial, "WHOWHO:") || !strings.Contains(serial, "root") {
		t.Fatalf("serial output missing whoami output\nserial:\n%s", serial)
	}
	if !strings.Contains(serial, "UNAMEUNAME:") || !strings.Contains(serial, "Linux (none) ") || !strings.Contains(serial, "x86_64 Linux") {
		t.Fatalf("serial output missing uname output\nserial:\n%s", serial)
	}
}
