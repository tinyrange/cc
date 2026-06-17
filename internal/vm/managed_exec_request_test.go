package vm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/imagefs"
)

func TestResolveManagedExecRequest(t *testing.T) {
	rootDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(rootDir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "bin", "tool"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := resolveManagedExecRequest(client.ExecRequest{
		Command: []string{"tool", "arg"},
		Env:     []string{"EXTRA=1"},
		Stdin:   []byte("input"),
	}, managedExecResolver{
		root:           imagefs.NewHostFS(rootDir, nil),
		baseEnv:        []string{"PATH=/bin"},
		defaultWorkDir: "/work",
		defaultRootDir: "/rootfs",
		env: func(base, overrides []string, replace bool) []string {
			return append(append([]string(nil), base...), overrides...)
		},
		user: func(user string) (string, error) {
			if user == "" {
				return "1000:1000", nil
			}
			return user, nil
		},
	})
	if err != nil {
		t.Fatalf("resolveManagedExecRequest: %v", err)
	}
	if strings.Join(got.Command, " ") != "/bin/tool arg" {
		t.Fatalf("command = %#v", got.Command)
	}
	if got.WorkDir != "/work" {
		t.Fatalf("workdir = %q", got.WorkDir)
	}
	if got.RootDir != "/rootfs" {
		t.Fatalf("rootdir = %q", got.RootDir)
	}
	if got.User != "1000:1000" {
		t.Fatalf("user = %q", got.User)
	}
	if string(got.Stdin) != "input" {
		t.Fatalf("stdin = %q", got.Stdin)
	}
}

func TestResolveManagedExecRequestValidation(t *testing.T) {
	_, err := resolveManagedExecRequest(client.ExecRequest{Command: []string{"tool"}}, managedExecResolver{
		missingRootErr: "missing root",
	})
	if err == nil || err.Error() != "missing root" {
		t.Fatalf("missing root error = %v", err)
	}
	_, err = resolveManagedExecRequest(client.ExecRequest{
		Command:     []string{"tool"},
		SkipResolve: true,
		WorkDir:     "relative",
	}, managedExecResolver{})
	if err == nil || !strings.Contains(err.Error(), "workdir must be absolute") {
		t.Fatalf("relative workdir error = %v", err)
	}
}

func TestResolveManagedExecRequestCanMarkHostResolved(t *testing.T) {
	rootDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(rootDir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "bin", "tool"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := resolveManagedExecRequest(client.ExecRequest{
		Command: []string{"tool"},
	}, managedExecResolver{
		root:           imagefs.NewHostFS(rootDir, nil),
		baseEnv:        []string{"PATH=/bin"},
		defaultWorkDir: "/",
		markResolved:   true,
	})
	if err != nil {
		t.Fatalf("resolveManagedExecRequest: %v", err)
	}
	if strings.Join(got.Command, " ") != "/bin/tool" {
		t.Fatalf("command = %#v", got.Command)
	}
	if !got.SkipResolve {
		t.Fatalf("SkipResolve = false")
	}
}

func TestResolveRunExecRequestMarksResolvedAlternateRoot(t *testing.T) {
	rootDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(rootDir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "bin", "tool"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := resolveRunExecRequest(client.RunRequest{
		Command: []string{"tool"},
		Env:     []string{"EXTRA=1"},
		Stdin:   []byte("input"),
	}, "/run/image", managedExecResolver{
		root:           imagefs.NewHostFS(rootDir, nil),
		baseEnv:        []string{"PATH=/bin", "BASE=1"},
		defaultWorkDir: "/workspace",
		env:            mergeImageRunEnv,
	})
	if err != nil {
		t.Fatalf("resolveRunExecRequest: %v", err)
	}
	if strings.Join(got.Command, " ") != "/bin/tool" {
		t.Fatalf("command = %#v", got.Command)
	}
	if got.RootDir != "/run/image" {
		t.Fatalf("rootdir = %q", got.RootDir)
	}
	if got.WorkDir != "/workspace" {
		t.Fatalf("workdir = %q", got.WorkDir)
	}
	if !got.ReplaceEnv || !got.SkipResolve {
		t.Fatalf("ReplaceEnv/SkipResolve = %v/%v", got.ReplaceEnv, got.SkipResolve)
	}
	if string(got.Stdin) != "input" {
		t.Fatalf("stdin = %q", got.Stdin)
	}
}

func TestControlExecRequest(t *testing.T) {
	stdin := []byte("payload")
	got := controlExecRequest(client.ExecRequest{
		Kind:      "fs_write",
		RootDir:   "/root",
		Path:      "/file",
		Directory: true,
		User:      "1000:1000",
		Stdin:     stdin,
	}, "/work")
	if got.Kind != "fs_write" || got.RootDir != "/root" || got.Path != "/file" || !got.Directory || got.User != "1000:1000" {
		t.Fatalf("control request = %+v", got)
	}
	if got.WorkDir != "/work" {
		t.Fatalf("workdir = %q", got.WorkDir)
	}
	stdin[0] = 'X'
	if string(got.Stdin) != "payload" {
		t.Fatalf("stdin was not copied: %q", got.Stdin)
	}

	got = controlExecRequest(client.ExecRequest{Kind: "sync", WorkDir: "/explicit"}, "/work")
	if got.WorkDir != "/explicit" {
		t.Fatalf("explicit workdir = %q", got.WorkDir)
	}
}
