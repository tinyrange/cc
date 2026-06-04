//go:build linux && arm64

package vm

import (
	"io"
	"strings"
	"testing"

	"j5.nz/cc/internal/imagefs"
	"j5.nz/cc/internal/vmruntime"
)

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

func TestLinuxRuntimeMountDirsInjectsDefaultUserLookupFiles(t *testing.T) {
	rootFS := blankLinuxRuntimeRootFS()
	passwd := readImageFile(t, rootFS, "/etc/passwd")
	group := readImageFile(t, rootFS, "/etc/group")

	if !strings.Contains(passwd, ":x:") {
		t.Fatalf("/etc/passwd missing injected user entry:\n%s", passwd)
	}
	if !strings.Contains(group, ":x:") {
		t.Fatalf("/etc/group missing injected group entry:\n%s", group)
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
