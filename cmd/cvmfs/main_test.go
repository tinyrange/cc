package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestRunLSAndCatLocal(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dir := filepath.Join(root, "dir")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := run([]string{"ls", dir}, &out); err != nil {
		t.Fatalf("run ls: %v", err)
	}
	if got := out.String(); got != "hello.txt\n" {
		t.Fatalf("ls output = %q", got)
	}

	out.Reset()
	if err := run([]string{"cat", filepath.Join(dir, "hello.txt")}, &out); err != nil {
		t.Fatalf("run cat: %v", err)
	}
	if got := out.String(); got != "hello" {
		t.Fatalf("cat output = %q", got)
	}
}
