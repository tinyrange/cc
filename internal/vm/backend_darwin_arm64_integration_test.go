//go:build darwin && arm64

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

func TestRunAMD64AlpineSimpleCommand(t *testing.T) {
	if strings.TrimSpace(os.Getenv("CCX3_RUN_AMD64_ALPINE")) == "" {
		t.Skip("set CCX3_RUN_AMD64_ALPINE=1 to run the live amd64 alpine guest test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	root := t.TempDir()
	kernel := alpine.NewManager(filepath.Join(root, "kernel"))
	if err := kernel.Ensure(ctx); err != nil {
		t.Fatalf("kernel.Ensure() error = %v", err)
	}

	store := oci.NewStore(filepath.Join(root, "images"))
	if _, err := store.Pull(ctx, "amd64-alpine", "amd64/alpine:latest"); err != nil {
		t.Fatalf("store.Pull() error = %v", err)
	}
	image, err := store.Open("amd64-alpine")
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	if image.Architecture != "amd64" {
		t.Fatalf("image.Architecture = %q, want amd64", image.Architecture)
	}

	backend := NewRuntimeBackend(kernel, store, filepath.Join(store.Root(), "_guestinit"))
	resp, err := backend.Run(ctx, client.RunRequest{
		Image:   "amd64-alpine",
		Command: []string{"/bin/sh", "-lc", "echo hello-amd64 && uname -m"},
	})
	if err != nil {
		t.Fatalf("backend.Run() error = %v", err)
	}
	if resp.ExitCode != 0 {
		t.Fatalf("backend.Run().ExitCode = %d, want 0\noutput:\n%s", resp.ExitCode, resp.Output)
	}
	if !strings.Contains(resp.Output, "hello-amd64") {
		t.Fatalf("backend output missing hello-amd64\noutput:\n%s", resp.Output)
	}
	if !strings.Contains(resp.Output, "x86_64") {
		t.Fatalf("backend output missing x86_64\noutput:\n%s", resp.Output)
	}
}

func TestRunNiimathFromLocalSIMGPath(t *testing.T) {
	if strings.TrimSpace(os.Getenv("CCX3_RUN_NIIMATH")) == "" {
		t.Skip("set CCX3_RUN_NIIMATH=1 to run the live niimath .simg guest test")
	}

	fixture, err := filepath.Abs(filepath.Join("..", "..", "local", "niimath_1.0.20250804_20251016.simg"))
	if err != nil {
		t.Fatalf("Abs(niimath fixture) error = %v", err)
	}
	if _, err := os.Stat(fixture); err != nil {
		t.Fatalf("Stat(niimath fixture) error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
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
	image, err := store.Open("niimath")
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	if image.SourceKind != oci.SourceKindSIMG {
		t.Fatalf("image.SourceKind = %q, want %q", image.SourceKind, oci.SourceKindSIMG)
	}

	backend := NewRuntimeBackend(kernel, store, filepath.Join(store.Root(), "_guestinit"))
	resp, err := backend.Run(ctx, client.RunRequest{
		Image:   "niimath",
		Command: []string{"niimath", "-help"},
	})
	if err != nil {
		t.Fatalf("backend.Run() error = %v", err)
	}
	if resp.ExitCode == 126 || resp.ExitCode == 127 {
		t.Fatalf("backend.Run().ExitCode = %d, want niimath to be found in PATH\noutput:\n%s", resp.ExitCode, resp.Output)
	}
	lower := strings.ToLower(resp.Output)
	if !strings.Contains(lower, "usage: niimath") {
		t.Fatalf("backend output did not contain niimath help\noutput:\n%s", resp.Output)
	}
}

func TestStartNiimathFromLocalSIMGPath(t *testing.T) {
	if strings.TrimSpace(os.Getenv("CCX3_RUN_NIIMATH")) == "" {
		t.Skip("set CCX3_RUN_NIIMATH=1 to run the live niimath .simg guest test")
	}

	fixture, err := filepath.Abs(filepath.Join("..", "..", "local", "niimath_1.0.20250804_20251016.simg"))
	if err != nil {
		t.Fatalf("Abs(niimath fixture) error = %v", err)
	}
	if _, err := os.Stat(fixture); err != nil {
		t.Fatalf("Stat(niimath fixture) error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
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
	state, err := mgr.Start(ctx, client.CreateInstanceRequest{Image: "niimath"})
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
		t.Fatalf("mgr.Run() error = %v", err)
	}
	if resp.ExitCode == 126 || resp.ExitCode == 127 {
		t.Fatalf("mgr.Run().ExitCode = %d, want niimath to be found in PATH\noutput:\n%s", resp.ExitCode, resp.Output)
	}
	if !strings.Contains(strings.ToLower(resp.Output), "usage: niimath") {
		t.Fatalf("mgr.Run() output did not contain niimath help\noutput:\n%s", resp.Output)
	}
}

func TestStartAlpineWithWritableShare(t *testing.T) {
	if strings.TrimSpace(os.Getenv("CCX3_RUN_ALPINE")) == "" {
		t.Skip("set CCX3_RUN_ALPINE=1 to run the live alpine guest test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
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
	if _, err := store.Pull(ctx, "alpine", "alpine:latest"); err != nil {
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
		Command: []string{"/bin/sh", "-lc", "echo hello-share > /work/hello.txt && cat /work/hello.txt"},
	})
	if err != nil {
		t.Fatalf("mgr.Run() error = %v", err)
	}
	if resp.ExitCode != 0 {
		t.Fatalf("mgr.Run().ExitCode = %d, want 0\noutput:\n%s", resp.ExitCode, resp.Output)
	}
	if !strings.Contains(resp.Output, "hello-share") {
		t.Fatalf("mgr.Run() output = %q, want hello-share", resp.Output)
	}
	buf, err := os.ReadFile(filepath.Join(shareDir, "hello.txt"))
	if err != nil {
		t.Fatalf("ReadFile(host share) error = %v", err)
	}
	if strings.TrimSpace(string(buf)) != "hello-share" {
		t.Fatalf("host share contents = %q, want hello-share", string(buf))
	}
}
