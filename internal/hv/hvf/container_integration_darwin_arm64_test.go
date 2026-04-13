//go:build darwin && arm64

package hvf

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"j5.nz/cc/internal/guestinit"
	"j5.nz/cc/internal/kernel/alpine"
	"j5.nz/cc/internal/oci"
)

func TestRunAlpineUname(t *testing.T) {
	if os.Getenv("CCX3_RUN_ALPINE") == "" {
		t.Skip("set CCX3_RUN_ALPINE=1 to run the live alpine container boot test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), liveTestTimeout())
	defer cancel()

	root := t.TempDir()
	kernel := alpine.NewManager(filepath.Join(root, "kernel"))
	if err := kernel.Ensure(ctx); err != nil {
		t.Fatalf("kernel.Ensure() error = %v", err)
	}
	kernelBytes, err := kernel.ReadKernel()
	if err != nil {
		t.Fatalf("kernel.ReadKernel() error = %v", err)
	}
	modules, err := kernel.PlanModuleLoad(
		[]string{"CONFIG_VIRTIO_MMIO", "CONFIG_FUSE_FS", "CONFIG_VIRTIO_FS"},
		map[string]string{
			"CONFIG_VIRTIO_MMIO": "kernel/drivers/virtio/virtio_mmio.ko.gz",
			"CONFIG_FUSE_FS":     "kernel/fs/fuse/fuse.ko.gz",
			"CONFIG_VIRTIO_FS":   "kernel/fs/fuse/virtiofs.ko.gz",
		},
	)
	if err != nil {
		t.Fatalf("kernel.PlanModuleLoad() error = %v", err)
	}
	store := oci.NewStore(filepath.Join(root, "images"))
	if _, err := store.Pull(ctx, "alpine", "alpine:latest"); err != nil {
		t.Fatalf("store.Pull() error = %v", err)
	}
	image, err := store.Open("alpine")
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	initBin, err := guestinit.Build(ctx, filepath.Join(root, "guestinit"))
	if err != nil {
		t.Fatalf("guestinit.Build() error = %v", err)
	}

	result, err := RunContainer(ctx, ContainerRunRequest{
		Kernel:  kernelBytes,
		Init:    initBin,
		Modules: modules,
		Image:   image,
		Command: []string{"uname", "-a"},
		Dmesg:   true,
	})
	if err != nil {
		t.Fatalf("RunContainer() error = %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("RunContainer().ExitCode = %d, want 0\ntranscript:\n%s", result.ExitCode, result.Transcript)
	}
	if !strings.Contains(result.Transcript, "Linux") {
		t.Fatalf("transcript did not contain uname output\ntranscript:\n%s", result.Transcript)
	}
}

func TestRunAlpineShowsLoadedVirtioFSModules(t *testing.T) {
	if os.Getenv("CCX3_RUN_ALPINE") == "" {
		t.Skip("set CCX3_RUN_ALPINE=1 to run the live alpine container boot test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), liveTestTimeout())
	defer cancel()

	root := t.TempDir()
	kernel := alpine.NewManager(filepath.Join(root, "kernel"))
	if err := kernel.Ensure(ctx); err != nil {
		t.Fatalf("kernel.Ensure() error = %v", err)
	}
	kernelBytes, err := kernel.ReadKernel()
	if err != nil {
		t.Fatalf("kernel.ReadKernel() error = %v", err)
	}
	modules, err := kernel.PlanModuleLoad(
		[]string{"CONFIG_VIRTIO_MMIO", "CONFIG_FUSE_FS", "CONFIG_VIRTIO_FS"},
		map[string]string{
			"CONFIG_VIRTIO_MMIO": "kernel/drivers/virtio/virtio_mmio.ko.gz",
			"CONFIG_FUSE_FS":     "kernel/fs/fuse/fuse.ko.gz",
			"CONFIG_VIRTIO_FS":   "kernel/fs/fuse/virtiofs.ko.gz",
		},
	)
	if err != nil {
		t.Fatalf("kernel.PlanModuleLoad() error = %v", err)
	}

	store := oci.NewStore(filepath.Join(root, "images"))
	if _, err := store.Pull(ctx, "alpine", "alpine:latest"); err != nil {
		t.Fatalf("store.Pull() error = %v", err)
	}
	image, err := store.Open("alpine")
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	initBin, err := guestinit.Build(ctx, filepath.Join(root, "guestinit"))
	if err != nil {
		t.Fatalf("guestinit.Build() error = %v", err)
	}

	result, err := RunContainer(ctx, ContainerRunRequest{
		Kernel:  kernelBytes,
		Init:    initBin,
		Modules: modules,
		Image:   image,
		Command: []string{"/bin/cat", "/proc/modules"},
		Dmesg:   false,
	})
	if err != nil {
		t.Fatalf("RunContainer() error = %v", err)
	}
	for _, want := range []string{"virtio_mmio", "fuse", "virtiofs"} {
		if !strings.Contains(result.Output, want) {
			t.Fatalf("module output missing %q\noutput:\n%s", want, result.Output)
		}
	}
}

func TestRunAlpineSeesVirtioConsole(t *testing.T) {
	if os.Getenv("CCX3_RUN_ALPINE") == "" {
		t.Skip("set CCX3_RUN_ALPINE=1 to run the live alpine container boot test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), liveTestTimeout())
	defer cancel()

	root := t.TempDir()
	kernel := alpine.NewManager(filepath.Join(root, "kernel"))
	if err := kernel.Ensure(ctx); err != nil {
		t.Fatalf("kernel.Ensure() error = %v", err)
	}
	kernelBytes, err := kernel.ReadKernel()
	if err != nil {
		t.Fatalf("kernel.ReadKernel() error = %v", err)
	}
	modules, err := kernel.PlanModuleLoad(
		[]string{"CONFIG_VIRTIO_MMIO", "CONFIG_FUSE_FS", "CONFIG_VIRTIO_FS"},
		map[string]string{
			"CONFIG_VIRTIO_MMIO": "kernel/drivers/virtio/virtio_mmio.ko.gz",
			"CONFIG_FUSE_FS":     "kernel/fs/fuse/fuse.ko.gz",
			"CONFIG_VIRTIO_FS":   "kernel/fs/fuse/virtiofs.ko.gz",
		},
	)
	if err != nil {
		t.Fatalf("kernel.PlanModuleLoad() error = %v", err)
	}

	store := oci.NewStore(filepath.Join(root, "images"))
	if _, err := store.Pull(ctx, "alpine", "alpine:latest"); err != nil {
		t.Fatalf("store.Pull() error = %v", err)
	}
	image, err := store.Open("alpine")
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	initBin, err := guestinit.Build(ctx, filepath.Join(root, "guestinit"))
	if err != nil {
		t.Fatalf("guestinit.Build() error = %v", err)
	}

	result, err := RunContainer(ctx, ContainerRunRequest{
		Kernel:  kernelBytes,
		Init:    initBin,
		Modules: modules,
		Image:   image,
		Command: []string{
			"/bin/sh", "-lc",
			"test -e /sys/class/tty/hvc0/dev && cat /sys/class/tty/hvc0/dev",
		},
		Dmesg: false,
	})
	if err != nil {
		t.Fatalf("RunContainer() error = %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("RunContainer().ExitCode = %d, want 0\ntranscript:\n%s", result.ExitCode, result.Transcript)
	}
	if !strings.Contains(strings.TrimSpace(result.Output), ":") {
		t.Fatalf("virtio console output = %q, want tty major:minor", result.Output)
	}
}

func liveTestTimeout() time.Duration {
	if raw := strings.TrimSpace(os.Getenv("CCX3_TEST_TIMEOUT_SEC")); raw != "" {
		if secs, err := strconv.Atoi(raw); err == nil && secs > 0 {
			return time.Duration(secs) * time.Second
		}
	}
	return 90 * time.Second
}
