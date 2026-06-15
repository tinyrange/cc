//go:build linux && amd64

package kvm

import (
	"net"
	"strings"
	"testing"
)

type freeBSDManagedTestRoot struct{}

func (freeBSDManagedTestRoot) ReadAt([]byte, int64) (int, error)  { return 0, nil }
func (freeBSDManagedTestRoot) WriteAt([]byte, int64) (int, error) { return 0, nil }
func (freeBSDManagedTestRoot) Size() int64                        { return 512 }

func TestNormalizeFreeBSDManagedConfig(t *testing.T) {
	root := freeBSDManagedTestRoot{}
	if _, err := normalizeFreeBSDManagedConfig(FreeBSDManagedConfig{Root: root}); err == nil || !strings.Contains(err.Error(), "kernel is required") {
		t.Fatalf("missing kernel error = %v", err)
	}
	if _, err := normalizeFreeBSDManagedConfig(FreeBSDManagedConfig{Kernel: []byte("kernel")}); err == nil || !strings.Contains(err.Error(), "root filesystem is required") {
		t.Fatalf("missing root error = %v", err)
	}

	cfg, err := normalizeFreeBSDManagedConfig(FreeBSDManagedConfig{
		Kernel: []byte("kernel"),
		Root:   root,
	})
	if err != nil {
		t.Fatalf("normalize valid config: %v", err)
	}
	if cfg.MemoryMB != 1024 {
		t.Fatalf("default MemoryMB = %d, want 1024", cfg.MemoryMB)
	}
	if got := cfg.GuestIPv4.String(); got != "10.42.0.2" {
		t.Fatalf("default GuestIPv4 = %s, want 10.42.0.2", got)
	}
	if got := cfg.GuestMAC.String(); got != "02:42:0a:2a:00:02" {
		t.Fatalf("default GuestMAC = %s, want 02:42:0a:2a:00:02", got)
	}

	cfg, err = normalizeFreeBSDManagedConfig(FreeBSDManagedConfig{
		Kernel:    []byte("kernel"),
		Root:      root,
		MemoryMB:  1536,
		GuestIPv4: net.IPv4(10, 42, 0, 10),
		GuestMAC:  net.HardwareAddr{0x02, 0x42, 0x0a, 0x2a, 0x00, 0x0a},
	})
	if err != nil {
		t.Fatalf("normalize explicit config: %v", err)
	}
	if cfg.MemoryMB != 1536 {
		t.Fatalf("explicit MemoryMB = %d, want 1536", cfg.MemoryMB)
	}
	if got := cfg.GuestIPv4.String(); got != "10.42.0.10" {
		t.Fatalf("explicit GuestIPv4 = %s, want 10.42.0.10", got)
	}
	if got := cfg.GuestMAC.String(); got != "02:42:0a:2a:00:0a" {
		t.Fatalf("explicit GuestMAC = %s, want 02:42:0a:2a:00:0a", got)
	}
}
