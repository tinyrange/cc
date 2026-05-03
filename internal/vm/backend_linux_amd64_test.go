//go:build linux && amd64

package vm

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/imagefs"
	"j5.nz/cc/internal/oci"
	"j5.nz/cc/internal/virtio"
	"j5.nz/cc/internal/vmruntime"
)

func TestEnsureLinuxAMD64ImageRejectsKnownForeignArchitecture(t *testing.T) {
	err := ensureLinuxAMD64Image(&oci.Image{Name: "tool", Architecture: "arm64"})
	if err == nil {
		t.Fatal("ensureLinuxAMD64Image() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "linux/amd64 runtime supports only amd64 images") {
		t.Fatalf("ensureLinuxAMD64Image() error = %q", err)
	}
}

func TestEnsureLinuxAMD64ImageAllowsAMD64AndUnknown(t *testing.T) {
	for _, image := range []*oci.Image{
		{Name: "tool", Architecture: "amd64"},
		{Name: "legacy"},
		nil,
	} {
		if err := ensureLinuxAMD64Image(image); err != nil {
			t.Fatalf("ensureLinuxAMD64Image(%#v) error = %v", image, err)
		}
	}
}

func TestLinuxRuntimeMountDirsProvidesResolvableHostname(t *testing.T) {
	rootFS := blankLinuxRuntimeRootFS()
	hostname := readImageFile(t, rootFS, "/etc/hostname")
	hosts := readImageFile(t, rootFS, "/etc/hosts")

	want := vmruntime.DefaultHostname("")
	if strings.TrimSpace(hostname) != want {
		t.Fatalf("/etc/hostname = %q, want %q", hostname, want)
	}
	if !strings.Contains(hosts, "127.0.0.1\tlocalhost "+want) {
		t.Fatalf("/etc/hosts does not map hostname to localhost: %q", hosts)
	}
}

func TestLinuxResolveExecUserDefaultsToHostUser(t *testing.T) {
	got, err := linuxResolveExecUser("")
	if err != nil {
		t.Fatalf("linuxResolveExecUser(default) error = %v", err)
	}
	if got == "" {
		t.Fatal("linuxResolveExecUser(default) = empty")
	}
	if strings.Contains(got, "root") {
		t.Fatalf("linuxResolveExecUser(default) = %q, want numeric uid:gid", got)
	}
}

func TestLinuxResolveExecUserAcceptsExplicitRootAndNumeric(t *testing.T) {
	for _, tt := range []struct {
		in   string
		want string
	}{
		{in: "root", want: "0:0"},
		{in: "0", want: "0:0"},
		{in: "1001", want: "1001:1001"},
		{in: "1001:1002", want: "1001:1002"},
	} {
		got, err := linuxResolveExecUser(tt.in)
		if err != nil {
			t.Fatalf("linuxResolveExecUser(%q) error = %v", tt.in, err)
		}
		if got != tt.want {
			t.Fatalf("linuxResolveExecUser(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestLinuxResolveExecUserRejectsNames(t *testing.T) {
	errUser, err := linuxResolveExecUser("nobody")
	if err == nil {
		t.Fatalf("linuxResolveExecUser(nobody) = %q, nil error", errUser)
	}
}

func TestLinuxInstanceAddShareIsIdempotent(t *testing.T) {
	rootFS := &recordingShareMounter{}
	inst := &linuxInstance{rootFS: rootFS}
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

func TestLinuxInstanceAddShareRejectsConflictingMount(t *testing.T) {
	rootFS := &recordingShareMounter{}
	inst := &linuxInstance{rootFS: rootFS}
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

func TestRebaseRuntimeShares(t *testing.T) {
	shares := []client.ShareMount{
		{Source: "/host/work", Mount: "/.hostcwd/abc", Writable: true},
		{Source: "/host/data", Mount: "/data", Writable: false},
	}

	got := rebaseRuntimeShares("/.ccx3/images/niimath", shares)

	if got[0].Mount != "/.ccx3/images/niimath/.hostcwd/abc" {
		t.Fatalf("rebased mount[0] = %q", got[0].Mount)
	}
	if got[1].Mount != "/.ccx3/images/niimath/data" {
		t.Fatalf("rebased mount[1] = %q", got[1].Mount)
	}
	if shares[0].Mount != "/.hostcwd/abc" || shares[1].Mount != "/data" {
		t.Fatalf("original shares were mutated: %#v", shares)
	}
}

type recordingShareMounter struct {
	calls  int
	mounts map[string]virtio.ShareMount
}

func readImageFile(t *testing.T, root imagefs.Directory, guestPath string) string {
	t.Helper()
	entry, err := imagefs.LookupPath(root, guestPath)
	if err != nil {
		t.Fatalf("LookupPath(%s) error = %v", guestPath, err)
	}
	if entry.File == nil {
		t.Fatalf("LookupPath(%s) is not a file", guestPath)
	}
	size, _ := entry.File.Stat()
	data, err := entry.File.ReadAt(0, uint32(size))
	if err != nil && err != io.EOF {
		t.Fatalf("ReadAt(%s) error = %v", guestPath, err)
	}
	return string(data)
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
