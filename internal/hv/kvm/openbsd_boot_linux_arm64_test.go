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

func TestBootOpenBSD79Arm64BSDToSerial(t *testing.T) {
	if os.Getenv("CC_TEST_OPENBSD_KVM") == "" {
		t.Skip("set CC_TEST_OPENBSD_KVM=1 to run OpenBSD KVM boot smoke test")
	}
	kernelPath := filepath.Join("..", "..", "..", "local", "openbsd79-arm64", "bsd")
	kernel, err := os.ReadFile(kernelPath)
	if err != nil {
		if os.IsNotExist(err) {
			t.Skipf("OpenBSD fixture not present: %s", kernelPath)
		}
		t.Fatalf("read fixture: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	serial, err := BootOpenBSDKernelToSerial(ctx, kernel, 768)
	if err != nil {
		t.Fatalf("boot OpenBSD to serial: %v\nserial:\n%s", err, serial)
	}
	if strings.TrimSpace(serial) == "" {
		t.Fatalf("serial output is empty")
	}
}
