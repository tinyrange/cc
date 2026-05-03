package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigureMemoryOvercommit(t *testing.T) {
	procRoot := t.TempDir()
	vmDir := filepath.Join(procRoot, "sys", "vm")
	if err := os.MkdirAll(vmDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	target := filepath.Join(vmDir, "overcommit_memory")
	if err := os.WriteFile(target, []byte("0\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	configureMemoryOvercommit(procRoot)

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(got) != "1\n" {
		t.Fatalf("overcommit_memory = %q, want %q", got, "1\n")
	}
}
