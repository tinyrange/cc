//go:build linux && arm64

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
)

func TestRuntimeBackendKernelSerial(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping linux arm64 KVM integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	root := t.TempDir()
	kernel := alpine.NewManager(filepath.Join(root, "kernel"))
	if err := kernel.Ensure(ctx); err != nil {
		t.Fatalf("kernel.Ensure() error = %v", err)
	}

	backend := NewRuntimeBackend(kernel, nil, "")
	resp, err := backend.Run(ctx, client.RunRequest{
		Image:    "alpine",
		MemoryMB: 256,
		Dmesg:    true,
	})
	if err != nil {
		t.Fatalf("backend.Run() error = %v\noutput:\n%s", err, resp.Output)
	}
	if resp.Output == "" {
		t.Fatal("backend.Run().Output was empty")
	}
	if !strings.Contains(resp.Output, linuxInitReadyMarker) {
		t.Fatalf("backend.Run().Output did not contain ready marker\noutput:\n%s", resp.Output)
	}
}

func TestRuntimeBackendRunCommand(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping linux arm64 KVM integration test in short mode")
	}
	fixture := filepath.Join("..", "..", "fixtures", "alpine.simg")
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

	backend := NewRuntimeBackend(kernel, store, "")
	resp, err := backend.Run(ctx, client.RunRequest{
		Image:    "alpine",
		Command:  []string{"sh", "-c", "echo linux-arm64-ok"},
		MemoryMB: 256,
	})
	if err != nil {
		t.Fatalf("backend.Run() error = %v\noutput:\n%s", err, resp.Output)
	}
	if resp.ExitCode != 0 {
		t.Fatalf("backend.Run().ExitCode = %d, want 0\noutput:\n%s", resp.ExitCode, resp.Output)
	}
	if strings.TrimSpace(resp.Output) != "linux-arm64-ok" {
		t.Fatalf("backend.Run().Output = %q, want linux-arm64-ok", resp.Output)
	}
}

func TestRuntimeBackendStartThenExec(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping linux arm64 KVM integration test in short mode")
	}
	fixture := filepath.Join("..", "..", "fixtures", "alpine.simg")
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
		Command: []string{"sh", "-c", "echo linux-start-ok"},
	})
	if err != nil {
		t.Fatalf("mgr.Run() error = %v\noutput:\n%s", err, resp.Output)
	}
	if resp.ExitCode != 0 {
		t.Fatalf("mgr.Run().ExitCode = %d, want 0\noutput:\n%s", resp.ExitCode, resp.Output)
	}
	if strings.TrimSpace(resp.Output) != "linux-start-ok" {
		t.Fatalf("mgr.Run().Output = %q, want linux-start-ok", resp.Output)
	}
}

func TestRuntimeBackendRunNiimathFromLocalSIMGPath(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping linux arm64 KVM integration test in short mode")
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

func TestRuntimeBackendStartNiimathFromLocalSIMGPath(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping linux arm64 KVM integration test in short mode")
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

	mgr := NewManagerWithBackend(NewRuntimeBackend(kernel, store, filepath.Join(store.Root(), "_guestinit")))
	state, err := mgr.Start(ctx, client.CreateInstanceRequest{
		Image:    "niimath",
		MemoryMB: 1024,
	})
	if err != nil {
		t.Fatalf("mgr.Start() error = %v", err)
	}
	if state.Status != "running" {
		t.Fatalf("mgr.Start().Status = %q, want running", state.Status)
	}
	defer mgr.Shutdown(context.Background())

	resp, err := mgr.Run(ctx, client.RunRequest{
		Image:   "niimath",
		Command: []string{"niimath", "-help"},
	})
	if err != nil {
		t.Fatalf("mgr.Run() error = %v\noutput:\n%s", err, resp.Output)
	}
	if resp.ExitCode == 126 || resp.ExitCode == 127 {
		t.Fatalf("mgr.Run().ExitCode = %d, want niimath to be found in PATH\noutput:\n%s", resp.ExitCode, resp.Output)
	}
	if !strings.Contains(strings.ToLower(resp.Output), "usage: niimath") {
		t.Fatalf("mgr.Run() output did not contain niimath help\noutput:\n%s", resp.Output)
	}
}
