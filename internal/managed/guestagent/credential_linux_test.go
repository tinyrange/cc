//go:build linux

package guestagent

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestEnsureCredentialUserCreatesMissingHomeForExistingPasswdUser(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("test uses the current unprivileged uid to avoid requiring chown privileges")
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "etc"), 0o755); err != nil {
		t.Fatalf("mkdir etc: %v", err)
	}
	uid := os.Getuid()
	gid := os.Getgid()
	passwd := fmt.Sprintf("ubuntu:x:%d:%d:Ubuntu:/home/ubuntu:/bin/sh\n", uid, gid)
	if err := os.WriteFile(filepath.Join(root, "etc", "passwd"), []byte(passwd), 0o644); err != nil {
		t.Fatalf("write passwd: %v", err)
	}
	group := fmt.Sprintf("ubuntu:x:%d:\n", gid)
	if err := os.WriteFile(filepath.Join(root, "etc", "group"), []byte(group), 0o644); err != nil {
		t.Fatalf("write group: %v", err)
	}

	cred := &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)}
	if err := EnsureCredentialUser(root, cred); err != nil {
		t.Fatalf("ensure user: %v", err)
	}
	home := filepath.Join(root, "home", "ubuntu")
	info, err := os.Stat(home)
	if err != nil {
		t.Fatalf("stat home: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("home is not a directory: %s", home)
	}
}

func TestEnsureCredentialWorkDirCreatesHomeSubdirectory(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("test uses the current unprivileged uid to avoid requiring chown privileges")
	}
	root := t.TempDir()
	cred := &syscall.Credential{Uid: uint32(os.Getuid()), Gid: uint32(os.Getgid())}
	if err := EnsureCredentialWorkDir(root, "/home/ubuntu", cred); err != nil {
		t.Fatalf("ensure workdir: %v", err)
	}
	info, err := os.Stat(filepath.Join(root, "home", "ubuntu"))
	if err != nil {
		t.Fatalf("stat workdir: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("workdir is not a directory")
	}
	if err := EnsureCredentialWorkDir(root, "/work/project", cred); err != nil {
		t.Fatalf("ensure non-home workdir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "work", "project")); !os.IsNotExist(err) {
		t.Fatalf("non-home workdir stat err = %v, want not created", err)
	}
}

func TestCredentialForUser(t *testing.T) {
	root, err := CredentialForUser("root")
	if err != nil || root != nil {
		t.Fatalf("root credential = %+v, %v; want nil, nil", root, err)
	}
	cred, err := CredentialForUser("1000:1001")
	if err != nil {
		t.Fatalf("CredentialForUser: %v", err)
	}
	if cred == nil || cred.Uid != 1000 || cred.Gid != 1001 {
		t.Fatalf("credential = %+v, want 1000:1001", cred)
	}
	if _, err := CredentialForUser("1000:"); err == nil {
		t.Fatalf("CredentialForUser accepted empty gid")
	}
	if _, err := CredentialForUser("not-a-uid"); err == nil {
		t.Fatalf("CredentialForUser accepted non-numeric uid")
	}
}
