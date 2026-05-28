//go:build linux

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

func TestConfigureMemoryOvercommit(t *testing.T) {
	procRoot := t.TempDir()
	vmDir := filepath.Join(procRoot, "sys", "vm")
	if err := os.MkdirAll(vmDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	target := filepath.Join(vmDir, "overcommit_memory")
	if err := os.WriteFile(target, []byte("0\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	configureMemoryOvercommit(procRoot)

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(got) != "1\n" {
		t.Fatalf("overcommit_memory = %q, want %q", got, "1\n")
	}
}

func TestConfigureClockSkipsUnsetTime(t *testing.T) {
	called := false
	old := setTimeOfDay
	setTimeOfDay = func(*unix.Timeval) error {
		called = true
		return nil
	}
	t.Cleanup(func() { setTimeOfDay = old })

	if err := configureClock(0); err != nil {
		t.Fatalf("configureClock(0) error = %v", err)
	}
	if called {
		t.Fatal("configureClock(0) called setTimeOfDay")
	}
}

func TestConfigureClockSetsUnixTime(t *testing.T) {
	const wantUnix = int64(1_700_000_123)
	var got unix.Timeval
	old := setTimeOfDay
	setTimeOfDay = func(tv *unix.Timeval) error {
		got = *tv
		return nil
	}
	t.Cleanup(func() { setTimeOfDay = old })

	if err := configureClock(wantUnix); err != nil {
		t.Fatalf("configureClock() error = %v", err)
	}
	if got.Sec != wantUnix || got.Usec != 0 {
		t.Fatalf("timeval = {%d, %d}, want {%d, 0}", got.Sec, got.Usec, wantUnix)
	}
}

func TestConfigureClockReportsSettimeofdayError(t *testing.T) {
	want := errors.New("boom")
	old := setTimeOfDay
	setTimeOfDay = func(*unix.Timeval) error {
		return want
	}
	t.Cleanup(func() { setTimeOfDay = old })

	err := configureClock(1_700_000_123)
	if !errors.Is(err, want) {
		t.Fatalf("configureClock() error = %v, want wrapping %v", err, want)
	}
}

func TestEnsureCredentialUserCreatesPasswdGroupAndHome(t *testing.T) {
	root := t.TempDir()
	etc := filepath.Join(root, "etc")
	if err := os.MkdirAll(etc, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(etc, "passwd"), []byte("root:x:0:0:root:/root:/bin/sh\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(passwd) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(etc, "group"), []byte("root:x:0:\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(group) error = %v", err)
	}
	uid := uint32(os.Getuid())
	gid := uint32(os.Getgid())
	if uid == 0 {
		uid = 1000
		gid = 1000
	}

	if err := ensureCredentialUser(root, &syscall.Credential{Uid: uid, Gid: gid}); err != nil {
		t.Fatalf("ensureCredentialUser() error = %v", err)
	}
	wantUser := "cc:x:" + fmtUint32(uid) + ":" + fmtUint32(gid) + ":ccvm user:/home/cc:/bin/sh"
	passwd, err := os.ReadFile(filepath.Join(etc, "passwd"))
	if err != nil {
		t.Fatalf("ReadFile(passwd) error = %v", err)
	}
	if !strings.Contains(string(passwd), wantUser) {
		t.Fatalf("passwd = %q, want line %q", passwd, wantUser)
	}
	group, err := os.ReadFile(filepath.Join(etc, "group"))
	if err != nil {
		t.Fatalf("ReadFile(group) error = %v", err)
	}
	if !strings.Contains(string(group), "cc:x:"+fmtUint32(gid)+":") {
		t.Fatalf("group = %q, want cc gid", group)
	}
	if info, err := os.Stat(filepath.Join(root, "home", "cc")); err != nil || !info.IsDir() {
		t.Fatalf("home stat = %v, %v; want dir", info, err)
	}
}

func TestCredentialForRootPreservesInitCredentials(t *testing.T) {
	for _, user := range []string{"root", "0", "0:0"} {
		cred, err := credentialForUser(user)
		if err != nil {
			t.Fatalf("credentialForUser(%q) error = %v", user, err)
		}
		if cred != nil {
			t.Fatalf("credentialForUser(%q) = %#v, want nil", user, cred)
		}
	}
}

func TestConfigurePackageManagersWritesAptNetstackConfig(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "usr", "bin"), 0o755); err != nil {
		t.Fatalf("MkdirAll(/usr/bin) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "usr", "bin", "apt"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(apt) error = %v", err)
	}
	if err := configurePackageManagers(root); err != nil {
		t.Fatalf("configurePackageManagers() error = %v", err)
	}
	got, err := os.ReadFile(filepath.Join(root, "etc", "apt", "apt.conf.d", "99ccvm-netstack"))
	if err != nil {
		t.Fatalf("ReadFile(apt config) error = %v", err)
	}
	text := string(got)
	for _, want := range []string{
		`Acquire::Queue-Mode "access";`,
		`Acquire::http::Pipeline-Depth "0";`,
		`Acquire::https::Pipeline-Depth "0";`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("apt config = %q, want %q", text, want)
		}
	}
}

func fmtUint32(v uint32) string {
	return itoa(int(v))
}
