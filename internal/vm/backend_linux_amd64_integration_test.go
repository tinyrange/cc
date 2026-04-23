//go:build linux && amd64

package vm

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/kernel/alpine"
	"j5.nz/cc/internal/oci"
	"j5.nz/cc/internal/vmruntime"
)

func TestRuntimeBackendInitramfsReady(t *testing.T) {
	if os.Getenv("CCX3_KVM_BOOT") == "" {
		t.Skip("set CCX3_KVM_BOOT=1 to run the linux amd64 KVM boot probe")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	kernel := alpine.NewManager(t.TempDir())
	if err := kernel.Ensure(ctx); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	backend := NewRuntimeBackend(kernel, nil, t.TempDir())
	resp, err := backend.Run(ctx, client.RunRequest{MemoryMB: 256, Dmesg: true})
	if err != nil {
		t.Fatalf("backend.Run() error = %v\noutput:\n%s", err, resp.Output)
	}
	if resp.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\noutput:\n%s", resp.ExitCode, resp.Output)
	}
	if !strings.Contains(resp.Output, vmruntime.InstanceReadyMarker) {
		t.Fatalf("output missing ready marker %q:\n%s", vmruntime.InstanceReadyMarker, resp.Output)
	}
}

func TestRuntimeBackendRunCommand(t *testing.T) {
	if os.Getenv("CCX3_KVM_BOOT") == "" {
		t.Skip("set CCX3_KVM_BOOT=1 to run the linux amd64 KVM boot probe")
	}
	fixture := filepath.Join("..", "..", "local", "alpine.simg")
	if _, err := os.Stat(fixture); err != nil {
		t.Skipf("local alpine fixture unavailable: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	root := t.TempDir()
	kernel := alpine.NewManager(filepath.Join(root, "kernel"))
	if err := kernel.Ensure(ctx); err != nil {
		t.Fatalf("kernel.Ensure() error = %v", err)
	}
	store := oci.NewStore(filepath.Join(root, "images"))
	if _, err := store.Pull(ctx, "alpine", fixture); err != nil {
		t.Fatalf("store.Pull() error = %v", err)
	}

	backend := NewRuntimeBackend(kernel, store, filepath.Join(root, "guestinit"))
	resp, err := backend.Run(ctx, client.RunRequest{
		Image:    "alpine",
		Command:  []string{"sh", "-c", "echo linux-amd64-ok"},
		MemoryMB: 256,
	})
	if err != nil {
		t.Fatalf("backend.Run() error = %v\noutput:\n%s", err, resp.Output)
	}
	if resp.ExitCode != 0 {
		t.Fatalf("backend.Run().ExitCode = %d, want 0\noutput:\n%s", resp.ExitCode, resp.Output)
	}
	if strings.TrimSpace(resp.Output) != "linux-amd64-ok" {
		t.Fatalf("backend.Run().Output = %q, want linux-amd64-ok", resp.Output)
	}
}

func TestRuntimeBackendStartThenExec(t *testing.T) {
	if os.Getenv("CCX3_KVM_BOOT") == "" {
		t.Skip("set CCX3_KVM_BOOT=1 to run the linux amd64 KVM boot probe")
	}
	fixture := filepath.Join("..", "..", "local", "alpine.simg")
	if _, err := os.Stat(fixture); err != nil {
		t.Skipf("local alpine fixture unavailable: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	root := t.TempDir()
	kernel := alpine.NewManager(filepath.Join(root, "kernel"))
	if err := kernel.Ensure(ctx); err != nil {
		t.Fatalf("kernel.Ensure() error = %v", err)
	}
	store := oci.NewStore(filepath.Join(root, "images"))
	if _, err := store.Pull(ctx, "alpine", fixture); err != nil {
		t.Fatalf("store.Pull() error = %v", err)
	}

	mgr := NewManagerWithBackend(NewRuntimeBackend(kernel, store, filepath.Join(store.Root(), "_guestinit")))
	state, err := mgr.Start(ctx, client.CreateInstanceRequest{
		Image:    "alpine",
		MemoryMB: 256,
	})
	if err != nil {
		t.Fatalf("mgr.Start() error = %v", err)
	}
	if state.Status != "running" {
		t.Fatalf("mgr.Start().Status = %q, want running", state.Status)
	}
	defer mgr.Shutdown(context.Background())

	resp, err := mgr.Run(ctx, client.RunRequest{
		Image:   "alpine",
		Command: []string{"sh", "-c", "echo linux-amd64-start-ok"},
	})
	if err != nil {
		t.Fatalf("mgr.Run() error = %v\noutput:\n%s", err, resp.Output)
	}
	if resp.ExitCode != 0 {
		t.Fatalf("mgr.Run().ExitCode = %d, want 0\noutput:\n%s", resp.ExitCode, resp.Output)
	}
	if strings.TrimSpace(resp.Output) != "linux-amd64-start-ok" {
		t.Fatalf("mgr.Run().Output = %q, want linux-amd64-start-ok", resp.Output)
	}
}

func TestRuntimeBackendStartWithWritableShare(t *testing.T) {
	if os.Getenv("CCX3_KVM_BOOT") == "" {
		t.Skip("set CCX3_KVM_BOOT=1 to run the linux amd64 KVM boot probe")
	}
	fixture := filepath.Join("..", "..", "local", "alpine.simg")
	if _, err := os.Stat(fixture); err != nil {
		t.Skipf("local alpine fixture unavailable: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	root := t.TempDir()
	shareDir := filepath.Join(root, "share")
	if err := os.MkdirAll(shareDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(share) error = %v", err)
	}

	kernel := alpine.NewManager(filepath.Join(root, "kernel"))
	if err := kernel.Ensure(ctx); err != nil {
		t.Fatalf("kernel.Ensure() error = %v", err)
	}
	store := oci.NewStore(filepath.Join(root, "images"))
	if _, err := store.Pull(ctx, "alpine", fixture); err != nil {
		t.Fatalf("store.Pull() error = %v", err)
	}

	mgr := NewManagerWithBackend(NewRuntimeBackend(kernel, store, filepath.Join(store.Root(), "_guestinit")))
	state, err := mgr.Start(ctx, client.CreateInstanceRequest{
		Image: "alpine",
		Shares: []client.ShareMount{{
			Source:   shareDir,
			Mount:    "/work",
			Writable: true,
		}},
		MemoryMB: 256,
	})
	if err != nil {
		t.Fatalf("mgr.Start() error = %v", err)
	}
	if state.Status != "running" {
		t.Fatalf("mgr.Start().Status = %q, want running", state.Status)
	}
	defer mgr.Shutdown(context.Background())

	resp, err := mgr.Run(ctx, client.RunRequest{
		Image:   "alpine",
		Command: []string{"/bin/sh", "-lc", "echo hello-amd64-share > /work/hello.txt && cat /work/hello.txt"},
	})
	if err != nil {
		t.Fatalf("mgr.Run() error = %v\noutput:\n%s", err, resp.Output)
	}
	if resp.ExitCode != 0 {
		t.Fatalf("mgr.Run().ExitCode = %d, want 0\noutput:\n%s", resp.ExitCode, resp.Output)
	}
	if strings.TrimSpace(resp.Output) != "hello-amd64-share" {
		t.Fatalf("mgr.Run().Output = %q, want hello-amd64-share", resp.Output)
	}
	buf, err := os.ReadFile(filepath.Join(shareDir, "hello.txt"))
	if err != nil {
		t.Fatalf("ReadFile(host share) error = %v", err)
	}
	if strings.TrimSpace(string(buf)) != "hello-amd64-share" {
		t.Fatalf("host share contents = %q, want hello-amd64-share", string(buf))
	}
}

func TestRuntimeBackendStartBlankThenRunImage(t *testing.T) {
	if os.Getenv("CCX3_KVM_BOOT") == "" {
		t.Skip("set CCX3_KVM_BOOT=1 to run the linux amd64 KVM boot probe")
	}
	fixture := filepath.Join("..", "..", "local", "alpine.simg")
	if _, err := os.Stat(fixture); err != nil {
		t.Skipf("local alpine fixture unavailable: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	root := t.TempDir()
	kernel := alpine.NewManager(filepath.Join(root, "kernel"))
	if err := kernel.Ensure(ctx); err != nil {
		t.Fatalf("kernel.Ensure() error = %v", err)
	}
	store := oci.NewStore(filepath.Join(root, "images"))
	if _, err := store.Pull(ctx, "alpine", fixture); err != nil {
		t.Fatalf("store.Pull() error = %v", err)
	}

	mgr := NewManagerWithBackend(NewRuntimeBackend(kernel, store, filepath.Join(store.Root(), "_guestinit")))
	state, err := mgr.StartBlank(ctx, client.StartInstanceRequest{MemoryMB: 256})
	if err != nil {
		t.Fatalf("mgr.StartBlank() error = %v", err)
	}
	if state.Status != "running" {
		t.Fatalf("mgr.StartBlank().Status = %q, want running", state.Status)
	}
	defer mgr.Shutdown(context.Background())

	resp, err := mgr.Run(ctx, client.RunRequest{
		Image:   "alpine",
		Command: []string{"sh", "-c", "echo linux-amd64-blank-image-ok"},
	})
	if err != nil {
		t.Fatalf("mgr.Run() error = %v\noutput:\n%s", err, resp.Output)
	}
	if resp.ExitCode != 0 {
		t.Fatalf("mgr.Run().ExitCode = %d, want 0\noutput:\n%s", resp.ExitCode, resp.Output)
	}
	if strings.TrimSpace(resp.Output) != "linux-amd64-blank-image-ok" {
		t.Fatalf("mgr.Run().Output = %q, want linux-amd64-blank-image-ok", resp.Output)
	}
}

func TestRuntimeBackendRunNiimathFromLocalSIMGPath(t *testing.T) {
	if os.Getenv("CCX3_KVM_BOOT") == "" {
		t.Skip("set CCX3_KVM_BOOT=1 to run the linux amd64 KVM boot probe")
	}
	fixture := filepath.Join("..", "..", "local", "niimath_1.0.20250804_20251016.simg")
	if _, err := os.Stat(fixture); err != nil {
		t.Skipf("local niimath fixture unavailable: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	root := t.TempDir()
	kernel := alpine.NewManager(filepath.Join(root, "kernel"))
	if err := kernel.Ensure(ctx); err != nil {
		t.Fatalf("kernel.Ensure() error = %v", err)
	}
	store := oci.NewStore(filepath.Join(root, "images"))
	if _, err := store.Pull(ctx, "niimath", fixture); err != nil {
		t.Fatalf("store.Pull() error = %v", err)
	}

	backend := NewRuntimeBackend(kernel, store, filepath.Join(store.Root(), "_guestinit"))
	resp, err := backend.Run(ctx, client.RunRequest{
		Image:    "niimath",
		Command:  []string{"niimath", "-help"},
		MemoryMB: 1024,
	})
	if err != nil {
		t.Fatalf("backend.Run() error = %v\noutput:\n%s", err, resp.Output)
	}
	if resp.ExitCode == 126 || resp.ExitCode == 127 {
		t.Fatalf("backend.Run().ExitCode = %d, want niimath to be found in PATH\noutput:\n%s", resp.ExitCode, resp.Output)
	}
	if !strings.Contains(strings.ToLower(resp.Output), "usage: niimath") {
		t.Fatalf("backend output did not contain niimath help\noutput:\n%s", resp.Output)
	}
}
