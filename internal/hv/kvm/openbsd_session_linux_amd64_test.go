//go:build linux && amd64

package kvm

import (
	"errors"
	"net"
	"testing"
)

type openBSDManagedTestRoot struct{}

func (openBSDManagedTestRoot) ReadAt([]byte, int64) (int, error)  { return 0, nil }
func (openBSDManagedTestRoot) WriteAt([]byte, int64) (int, error) { return 0, nil }
func (openBSDManagedTestRoot) Size() int64                        { return 512 }

func TestNormalizeOpenBSDManagedConfig(t *testing.T) {
	root := openBSDManagedTestRoot{}
	if _, err := normalizeOpenBSDManagedConfig(OpenBSDManagedConfig{Root: root}); err == nil {
		t.Fatalf("missing kernel error = %v", err)
	}
	if _, err := normalizeOpenBSDManagedConfig(OpenBSDManagedConfig{Kernel: []byte("kernel")}); err == nil {
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
	if got := cfg.GuestIPv4.String(); got != "10.42.0.2" {
		t.Fatalf("default GuestIPv4 = %s, want 10.42.0.2", got)
	}
	if got := cfg.GuestMAC.String(); got != "02:42:0a:2a:00:02" {
		t.Fatalf("default GuestMAC = %s, want 02:42:0a:2a:00:02", got)
	}

	cfg, err = normalizeOpenBSDManagedConfig(OpenBSDManagedConfig{
		Kernel:    []byte("kernel"),
		Root:      root,
		MemoryMB:  1024,
		GuestIPv4: net.IPv4(10, 42, 0, 9),
		GuestMAC:  net.HardwareAddr{0x02, 0x42, 0x0a, 0x2a, 0x00, 0x09},
	})
	if err != nil {
		t.Fatalf("normalize explicit memory config: %v", err)
	}
	if cfg.MemoryMB != 1024 {
		t.Fatalf("explicit MemoryMB = %d, want 1024", cfg.MemoryMB)
	}
	if got := cfg.GuestIPv4.String(); got != "10.42.0.9" {
		t.Fatalf("explicit GuestIPv4 = %s, want 10.42.0.9", got)
	}
	if got := cfg.GuestMAC.String(); got != "02:42:0a:2a:00:09" {
		t.Fatalf("explicit GuestMAC = %s, want 02:42:0a:2a:00:09", got)
	}
}

func TestOpenBSDStartupErrorIncludesTranscripts(t *testing.T) {
	err := openBSDStartupError(errors.New("OpenBSD guest did not connect to control TCP port 10777"), "serial tail", "control tail")
	if err == nil {
		t.Fatal("startup error was nil")
	}
	text := err.Error()
	want := "OpenBSD guest did not connect to control TCP port 10777\nserial:\nserial tail\ncontrol:\ncontrol tail"
	if text != want {
		t.Fatalf("startup error = %q, want %q", text, want)
	}
}
