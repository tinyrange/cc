//go:build linux && amd64

package kvm

import (
	"errors"
	"strings"
	"testing"
)

type openBSDManagedTestRoot struct{}

func (openBSDManagedTestRoot) ReadAt([]byte, int64) (int, error)  { return 0, nil }
func (openBSDManagedTestRoot) WriteAt([]byte, int64) (int, error) { return 0, nil }
func (openBSDManagedTestRoot) Size() int64                        { return 512 }

func TestNormalizeOpenBSDManagedConfig(t *testing.T) {
	root := openBSDManagedTestRoot{}
	if _, err := normalizeOpenBSDManagedConfig(OpenBSDManagedConfig{Root: root}); err == nil || !strings.Contains(err.Error(), "kernel is required") {
		t.Fatalf("missing kernel error = %v", err)
	}
	if _, err := normalizeOpenBSDManagedConfig(OpenBSDManagedConfig{Kernel: []byte("kernel")}); err == nil || !strings.Contains(err.Error(), "root filesystem is required") {
		t.Fatalf("missing root error = %v", err)
	}

	cfg, err := normalizeOpenBSDManagedConfig(OpenBSDManagedConfig{
		Kernel: []byte("kernel"),
		Root:   root,
	})
	if err != nil {
		t.Fatalf("normalize valid config: %v", err)
	}
	if cfg.MemoryMB != 768 {
		t.Fatalf("default MemoryMB = %d, want 768", cfg.MemoryMB)
	}

	cfg, err = normalizeOpenBSDManagedConfig(OpenBSDManagedConfig{
		Kernel:   []byte("kernel"),
		Root:     root,
		MemoryMB: 1024,
	})
	if err != nil {
		t.Fatalf("normalize explicit memory config: %v", err)
	}
	if cfg.MemoryMB != 1024 {
		t.Fatalf("explicit MemoryMB = %d, want 1024", cfg.MemoryMB)
	}
}

func TestOpenBSDStartupErrorIncludesTranscripts(t *testing.T) {
	err := openBSDStartupError(errors.New("OpenBSD guest did not connect to control TCP port 10777"), "serial tail", "control tail")
	if err == nil {
		t.Fatal("startup error was nil")
	}
	text := err.Error()
	for _, want := range []string{
		"OpenBSD guest did not connect to control TCP port 10777",
		"serial:\nserial tail",
		"control:\ncontrol tail",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("startup error %q does not contain %q", text, want)
		}
	}
}
