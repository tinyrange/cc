//go:build linux && arm64

package kvm

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBootFreeBSD151Arm64KernelToSerial(t *testing.T) {
	if os.Getenv("CC_TEST_FREEBSD_KVM") == "" {
		t.Skip("set CC_TEST_FREEBSD_KVM=1 to run FreeBSD KVM boot smoke test")
	}
	kernelPath := filepath.Join("..", "..", "..", "local", "freebsd151-arm64", "kernel")
	kernel, err := os.ReadFile(kernelPath)
	if err != nil {
		if os.IsNotExist(err) {
			t.Skipf("FreeBSD fixture not present: %s", kernelPath)
		}
		t.Fatalf("read fixture: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	serial, err := BootFreeBSDKernelToSerial(ctx, kernel, 1024)
	if err != nil {
		t.Fatalf("boot FreeBSD to serial: %v\nserial:\n%s", err, serial)
	}
	if strings.TrimSpace(serial) == "" {
		t.Fatalf("serial output is empty")
	}
}
