//go:build !darwin || !arm64

package vm

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestRuntimeManagerUsesInProcessHostByDefault(t *testing.T) {
	t.Setenv(sidecarEnableEnv, "")
	t.Setenv(sidecarDisableEnv, "")

	mgr := NewRuntimeManager(nil, nil, t.TempDir(), t.TempDir(), false)
	caps := mgr.host.HostCapabilities(context.Background())
	if caps.Locality != "in-process" {
		t.Fatalf("HostCapabilities().Locality = %q, want in-process", caps.Locality)
	}
}

func TestRuntimeManagerAddsSidecarHostWhenEnabled(t *testing.T) {
	t.Setenv(sidecarEnableEnv, "1")
	t.Setenv(sidecarDisableEnv, "")

	mgr := NewRuntimeManager(nil, nil, t.TempDir(), t.TempDir(), false)
	caps := mgr.host.HostCapabilities(context.Background())
	if caps.Backend != "placement" || caps.Locality != "mixed" {
		t.Fatalf("HostCapabilities() = %#v, want placement/mixed", caps)
	}
	if !strings.Contains(strings.Join(mgr.Capabilities().Notes, "\n"), "sidecar") {
		t.Fatalf("Capabilities().Notes = %v, want sidecar note", mgr.Capabilities().Notes)
	}
}

func TestRuntimeManagerDoesNotAddSidecarHostForWorkers(t *testing.T) {
	t.Setenv(sidecarEnableEnv, "1")
	t.Setenv(sidecarDisableEnv, "")

	mgr := NewRuntimeManager(nil, nil, t.TempDir(), t.TempDir(), true)
	caps := mgr.host.HostCapabilities(context.Background())
	if caps.Locality != "in-process" {
		t.Fatalf("worker HostCapabilities().Locality = %q, want in-process", caps.Locality)
	}
}

func TestRuntimeManagerSidecarDisableWins(t *testing.T) {
	t.Setenv(sidecarEnableEnv, "1")
	t.Setenv(sidecarDisableEnv, "1")

	mgr := NewRuntimeManager(nil, nil, t.TempDir(), t.TempDir(), false)
	caps := mgr.host.HostCapabilities(context.Background())
	if caps.Locality != "in-process" {
		t.Fatalf("disabled HostCapabilities().Locality = %q, want in-process", caps.Locality)
	}
}

func TestSidecarControlSocketPathUsesTCPLoopbackOnWindows(t *testing.T) {
	host := &sidecarVMHost{cacheDir: t.TempDir()}
	got, err := host.sidecarControlSocketPath()
	if err != nil {
		t.Fatalf("sidecarControlSocketPath() error = %v", err)
	}
	if os.PathSeparator == '\\' {
		if got != "tcp://127.0.0.1:0" {
			t.Fatalf("sidecarControlSocketPath() = %q, want tcp loopback", got)
		}
		return
	}
	if !strings.HasSuffix(got, ".sock") {
		t.Fatalf("sidecarControlSocketPath() = %q, want unix socket path", got)
	}
}
