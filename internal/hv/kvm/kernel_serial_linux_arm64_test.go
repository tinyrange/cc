//go:build linux && arm64

package kvm

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"j5.nz/cc/internal/kernel/alpine"
)

func TestKernelBootSerial(t *testing.T) {
	if os.Getenv("CCX3_KVM_BOOT") == "" {
		t.Skip("set CCX3_KVM_BOOT=1 to run the linux arm64 KVM boot probe")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	kernelRoot := t.TempDir()
	manager := alpine.NewManager(kernelRoot)
	if err := manager.Ensure(ctx); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	kernelFile, err := manager.ReadKernel()
	if err != nil {
		t.Fatalf("ReadKernel() error = %v", err)
	}
	serial, err := BootKernelToSerialWithTimeout(kernelFile, 256, true, 30*time.Second)
	if err != nil {
		t.Fatalf("BootKernelToSerialWithTimeout() error = %v\nserial:\n%s", err, serial)
	}
	if serial == "" {
		t.Fatal("BootKernelToSerialWithTimeout() produced no serial output")
	}
	t.Logf("serial output:\n%s", serial)
}

func TestProbeReportsVMCreation(t *testing.T) {
	info, err := Probe()
	if err != nil {
		if strings.Contains(err.Error(), "/dev/kvm") {
			t.Skip(err)
		}
		t.Fatalf("Probe() error = %v", err)
	}
	if !info.VMCreateOK || !info.VCPUCreateOK || !info.VCPUInitOK {
		t.Fatalf("Probe() incomplete: %+v", info)
	}
}
