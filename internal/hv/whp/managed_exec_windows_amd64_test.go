//go:build windows && amd64

package whp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/amd64vm"
	"j5.nz/cc/internal/guestinit"
	"j5.nz/cc/internal/kernel/alpine"
	"j5.nz/cc/internal/oci"
	"j5.nz/cc/internal/virtio"
	"j5.nz/cc/internal/vmruntime"
)

func TestRunManagedExecWithAlpineRootFS(t *testing.T) {
	kernelFile, initrd, fsdevs := prepareManagedAlpineRootFS(t)

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	resp, serial, err := RunManagedExecWithFS(ctx, kernelFile, initrd, 256, false, fsdevs, client.ExecRequest{
		Command: []string{"/bin/sh", "-c", "whoami; uname -a"},
		Env:     vmruntime.WithDefaultEnv(nil),
		WorkDir: "/",
	})
	if err != nil {
		t.Fatalf("RunManagedExecWithFS() error = %v\nserial:\n%s", err, serial)
	}
	if resp.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0; output:\n%s", resp.ExitCode, resp.Output)
	}
	if !strings.Contains(resp.Output, "root") {
		t.Fatalf("managed exec output missing whoami output:\n%s", resp.Output)
	}
	if !strings.Contains(resp.Output, "Linux ") || !strings.Contains(resp.Output, "x86_64 Linux") {
		t.Fatalf("managed exec output missing uname output:\n%s", resp.Output)
	}
}

func TestManagedSessionExecWithAlpineRootFS(t *testing.T) {
	kernelFile, initrd, fsdevs := prepareManagedAlpineRootFS(t)

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	session, err := StartManagedSession(ctx, kernelFile, initrd, 256, false, fsdevs, nil)
	if err != nil {
		t.Fatalf("StartManagedSession() error = %v", err)
	}
	defer session.Close()

	resp, err := session.Exec(ctx, client.ExecRequest{
		Command: []string{"/bin/sh", "-c", "printf session-ok"},
		Env:     vmruntime.WithDefaultEnv(nil),
		WorkDir: "/",
	})
	if err != nil {
		t.Fatalf("Exec() error = %v", err)
	}
	if resp.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0; output:\n%s", resp.ExitCode, resp.Output)
	}
	if strings.TrimSpace(resp.Output) != "session-ok" {
		t.Fatalf("Output = %q, want session-ok", resp.Output)
	}
}

func TestManagedSessionExecStreamForwardsStdin(t *testing.T) {
	kernelFile, initrd, fsdevs := prepareManagedAlpineRootFS(t)

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	session, err := StartManagedSession(ctx, kernelFile, initrd, 256, false, fsdevs, nil)
	if err != nil {
		t.Fatalf("StartManagedSession() error = %v", err)
	}
	defer session.Close()

	inputs := make(chan client.ExecInput, 2)
	inputs <- client.ExecInput{Kind: "stdin", Input: "stream-input\n"}
	close(inputs)

	var events []client.ExecEvent
	err = session.ExecStream(ctx, client.ExecRequest{
		Command: []string{"/bin/sh", "-c", "cat"},
		Env:     vmruntime.WithDefaultEnv(nil),
		WorkDir: "/",
	}, inputs, func(event client.ExecEvent) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatalf("ExecStream() error = %v", err)
	}
	var output strings.Builder
	exitSeen := false
	for _, event := range events {
		if event.Kind == "stdout" {
			output.WriteString(event.Output)
		}
		if event.Kind == "exit" {
			exitSeen = true
			if event.ExitCode != 0 {
				t.Fatalf("stream exit code = %d, want 0", event.ExitCode)
			}
		}
	}
	if !exitSeen {
		t.Fatalf("stream did not emit exit event: %#v", events)
	}
	if output.String() != "stream-input\n" {
		t.Fatalf("stream output = %q, want stream-input\\n", output.String())
	}
}

func TestManagedSessionExecStreamReturnsWhenVMExits(t *testing.T) {
	session := &ManagedSession{
		doneCh:     make(chan error, 1),
		transcript: vmruntime.NewSerialTranscript(),
		serialOut:  vmruntime.NewSerialTranscript(),
	}
	vmErr := errors.New("vm stopped")
	session.doneCh <- vmErr

	err := session.streamExecEvents(context.Background(), 0, "1", nil)
	if err == nil {
		t.Fatal("streamExecEvents() error = nil, want VM exit error")
	}
	if !strings.Contains(err.Error(), "VM exited during exec") || !strings.Contains(err.Error(), "vm stopped") {
		t.Fatalf("streamExecEvents() error = %q", err)
	}
	select {
	case got := <-session.doneCh:
		if !errors.Is(got, vmErr) {
			t.Fatalf("doneCh error = %v, want %v", got, vmErr)
		}
	default:
		t.Fatal("streamExecEvents() did not preserve doneCh result")
	}
}

func prepareManagedAlpineRootFS(t *testing.T) ([]byte, []byte, []*virtio.FS) {
	t.Helper()
	if os.Getenv("CCX3_WHP_BOOT") == "" {
		t.Skip("set CCX3_WHP_BOOT=1 to run the windows amd64 WHP managed exec probe")
	}
	fixture := filepath.Join("..", "..", "..", "fixtures", "alpine.simg")
	if _, err := os.Stat(fixture); err != nil {
		t.Skipf("local alpine fixture unavailable: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
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
		[]string{"CONFIG_VIRTIO_MMIO", "CONFIG_FUSE_FS", "CONFIG_VIRTIO_FS", "CONFIG_VSOCKETS", "CONFIG_VIRTIO_VSOCKETS"},
		map[string]string{
			"CONFIG_VIRTIO_MMIO":     "kernel/drivers/virtio/virtio_mmio.ko.gz",
			"CONFIG_FUSE_FS":         "kernel/fs/fuse/fuse.ko.gz",
			"CONFIG_VIRTIO_FS":       "kernel/fs/fuse/virtiofs.ko.gz",
			"CONFIG_VSOCKETS":        "kernel/net/vmw_vsock/vsock.ko.gz",
			"CONFIG_VIRTIO_VSOCKETS": "kernel/net/vmw_vsock/vmw_vsock_virtio_transport.ko.gz",
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
	initrd, err := vmruntime.BuildInitramfs(initBin, modules, vmruntime.GuestInitConfig{
		Env:              vmruntime.WithDefaultEnv(nil),
		Modules:          vmruntime.ModulePaths(modules),
		RootFSTag:        vmruntime.RootFSTag,
		VsockPort:        vmruntime.ControlPort,
		ReadyMarker:      vmruntime.InstanceReadyMarker,
		BeginMarker:      vmruntime.CommandBeginMarker,
		OutputMarkerPref: vmruntime.CommandOutputMarker,
		ErrorMarkerPref:  vmruntime.CommandErrorMarker,
		ExitMarkerPrefix: vmruntime.CommandExitMarkerPref,
	})
	if err != nil {
		t.Fatalf("BuildInitramfs() error = %v", err)
	}
	return kernelFile, initrd, fsdevs
}
