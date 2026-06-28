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

// This boot smoke test consumes an unstructured kernel serial transcript.
// The substring check confirms the guest reached a NetBSD boot path.
func TestBootNetBSD101Arm64KernelToSerial(t *testing.T) {
	if os.Getenv("CC_TEST_NETBSD_KVM") == "" {
		t.Skip("set CC_TEST_NETBSD_KVM=1 to run NetBSD KVM boot smoke test")
	}
	kernelPath := filepath.Join("..", "..", "..", "local", "netbsd101-evbarm-aarch64", "netbsd-GENERIC64.img")
	kernel, err := os.ReadFile(kernelPath)
	if err != nil {
		if os.IsNotExist(err) {
			t.Skipf("NetBSD fixture not present: %s", kernelPath)
		}
		t.Fatalf("read fixture: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	serial, err := BootNetBSDKernelToMarker(ctx, kernel, 1024, "NetBSD/evbarm")
	if err != nil {
		t.Fatalf("boot NetBSD to serial: %v\nserial:\n%s", err, serial)
	}
	if !strings.Contains(serial, "NetBSD") {
		t.Fatalf("serial does not identify NetBSD:\n%s", serial)
	}
}
