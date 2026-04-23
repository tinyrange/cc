//go:build linux && amd64

package vm

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/kernel/alpine"
	"j5.nz/cc/internal/vmruntime"
)

func TestRuntimeBackendInitramfsReady(t *testing.T) {
	if os.Getenv("CCX3_KVM_BOOT") == "" {
		t.Skip("set CCX3_KVM_BOOT=1 to run the linux amd64 KVM boot probe")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	kernel := alpine.NewManager(t.TempDir())
	if err := kernel.Ensure(ctx); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	backend := NewRuntimeBackend(kernel, nil, t.TempDir())
	resp, err := backend.Run(ctx, client.RunRequest{MemoryMB: 256, Dmesg: true})
	if err != nil {
		t.Fatalf("backend.Run() error = %v\noutput:\n%s", err, resp.Output)
	}
	if resp.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\noutput:\n%s", resp.ExitCode, resp.Output)
	}
	if !strings.Contains(resp.Output, vmruntime.InstanceReadyMarker) {
		t.Fatalf("output missing ready marker %q:\n%s", vmruntime.InstanceReadyMarker, resp.Output)
	}
}
