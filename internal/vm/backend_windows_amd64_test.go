//go:build windows && amd64

package vm

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/oci"
	"j5.nz/cc/internal/virtio"
)

func TestEnsureWindowsAMD64ImageRejectsKnownForeignArchitecture(t *testing.T) {
	err := ensureWindowsAMD64Image(&oci.Image{Name: "tool", Architecture: "arm64"})
	if err == nil {
		t.Fatal("ensureWindowsAMD64Image() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "windows/amd64 runtime supports only amd64 images") {
		t.Fatalf("ensureWindowsAMD64Image() error = %q", err)
	}
}

func TestEnsureWindowsAMD64ImageAllowsAMD64AndUnknown(t *testing.T) {
	for _, image := range []*oci.Image{
		{Name: "tool", Architecture: "amd64"},
		{Name: "legacy"},
		nil,
	} {
		if err := ensureWindowsAMD64Image(image); err != nil {
			t.Fatalf("ensureWindowsAMD64Image(%#v) error = %v", image, err)
		}
	}
}

func TestWindowsImageMountPathUsesLinuxGuestSeparators(t *testing.T) {
	got := windowsImageMountPath("registry.example/niimath:latest")

	if got != "/.ccx3/images/registry.example_niimath_latest" {
		t.Fatalf("windowsImageMountPath() = %q", got)
	}
	if strings.Contains(got, `\`) {
		t.Fatalf("windowsImageMountPath() used host separators: %q", got)
	}
}

func TestWindowsGuestInitConfigIncludesUnixTime(t *testing.T) {
	before := time.Now().Unix()
	got := windowsGuestInitConfig(nil, true)
	after := time.Now().Unix()

	if got.UnixTime < before || got.UnixTime > after {
		t.Fatalf("UnixTime = %d, want between %d and %d", got.UnixTime, before, after)
	}
}

func TestWindowsInstanceAddShareIsIdempotent(t *testing.T) {
	rootFS := &recordingShareMounter{}
	inst := &windowsInstance{rootFS: rootFS}
	source := t.TempDir()
	share := client.ShareMount{Source: source, Mount: "/work", Writable: true}

	if err := inst.AddShare(context.Background(), share); err != nil {
		t.Fatalf("AddShare() error = %v", err)
	}
	if err := inst.AddShare(context.Background(), share); err != nil {
		t.Fatalf("AddShare() repeat error = %v", err)
	}
	if rootFS.calls != 1 {
		t.Fatalf("rootFS.AddShare() calls = %d, want 1", rootFS.calls)
	}
}

func TestWindowsInstanceAddShareRejectsConflictingMount(t *testing.T) {
	rootFS := &recordingShareMounter{}
	inst := &windowsInstance{rootFS: rootFS}
	first := client.ShareMount{Source: t.TempDir(), Mount: "/work", Writable: true}
	second := client.ShareMount{Source: t.TempDir(), Mount: "/work", Writable: true}

	if err := inst.AddShare(context.Background(), first); err != nil {
		t.Fatalf("AddShare() error = %v", err)
	}
	err := inst.AddShare(context.Background(), second)
	if err == nil {
		t.Fatal("AddShare() conflicting error = nil, want error")
	}
	if !strings.Contains(err.Error(), `share mount "/work" already exists`) {
		t.Fatalf("AddShare() conflicting error = %q", err)
	}
	if rootFS.calls != 1 {
		t.Fatalf("rootFS.AddShare() calls = %d, want 1", rootFS.calls)
	}
}

type recordingShareMounter struct {
	calls  int
	mounts map[string]virtio.ShareMount
}

func (m *recordingShareMounter) AddShare(share virtio.ShareMount) error {
	if m.mounts == nil {
		m.mounts = make(map[string]virtio.ShareMount)
	}
	if _, ok := m.mounts[share.GuestPath]; ok {
		return fmt.Errorf("duplicate mount")
	}
	m.calls++
	m.mounts[share.GuestPath] = share
	return nil
}
