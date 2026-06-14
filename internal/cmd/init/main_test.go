//go:build linux

package main

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
	if err := ensureCredentialUser(root, cred); err != nil {
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
	if err := ensureCredentialWorkDir(root, "/home/ubuntu", cred); err != nil {
		t.Fatalf("ensure workdir: %v", err)
	}
	info, err := os.Stat(filepath.Join(root, "home", "ubuntu"))
	if err != nil {
		t.Fatalf("stat workdir: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("workdir is not a directory")
	}
	if err := ensureCredentialWorkDir(root, "/work/project", cred); err != nil {
		t.Fatalf("ensure non-home workdir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "work", "project")); !os.IsNotExist(err) {
		t.Fatalf("non-home workdir stat err = %v, want not created", err)
	}
}

func TestCommandNeedsSystemdReady(t *testing.T) {
	tests := []struct {
		name string
		argv []string
		want bool
	}{
		{name: "empty", argv: nil, want: false},
		{name: "ordinary command", argv: []string{"nproc"}, want: false},
		{name: "direct systemctl", argv: []string{"systemctl", "status"}, want: true},
		{name: "path systemctl", argv: []string{"/usr/bin/systemctl", "status"}, want: true},
		{name: "journalctl", argv: []string{"journalctl", "-b"}, want: true},
		{name: "service", argv: []string{"service", "ssh", "status"}, want: true},
		{name: "service help", argv: []string{"service", "--help"}, want: false},
		{name: "shell ordinary", argv: []string{"sh", "-lc", "printf ok"}, want: false},
		{name: "shell systemctl", argv: []string{"sh", "-lc", "systemctl status"}, want: true},
		{name: "shell path systemctl", argv: []string{"bash", "-c", "/bin/systemctl status"}, want: true},
		{name: "sudo systemctl", argv: []string{"sudo", "-u", "root", "systemctl", "status"}, want: true},
		{name: "env systemctl", argv: []string{"env", "LC_ALL=C", "systemctl", "status"}, want: true},
		{name: "env ordinary", argv: []string{"env", "LC_ALL=C", "nproc"}, want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := commandNeedsSystemdReady(test.argv)
			if got != test.want {
				t.Fatalf("commandNeedsSystemdReady(%q) = %v, want %v", test.argv, got, test.want)
			}
		})
	}
}
