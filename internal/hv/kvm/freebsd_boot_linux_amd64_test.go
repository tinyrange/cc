//go:build linux && amd64

package kvm

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestBootFreeBSDKernelToSerialFromEnv(t *testing.T) {
	kernelPath := os.Getenv("CC_FREEBSD_KERNEL")
	if kernelPath == "" {
		t.Skip("CC_FREEBSD_KERNEL is not set")
	}
	kernel, err := os.ReadFile(kernelPath)
	if err != nil {
		t.Fatalf("read kernel: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	serial, err := BootFreeBSDKernelToMarker(ctx, kernel, 1024, "FreeBSD")
	if err != nil {
		t.Fatalf("boot FreeBSD kernel: %v\nserial:\n%s", err, serial)
	}
	if serial == "" {
		t.Fatalf("expected serial output")
	}
	t.Logf("serial:\n%s", serial)
}
