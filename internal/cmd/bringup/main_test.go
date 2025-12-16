//go:build guest

package main

import (
	"os"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

func TestHello(t *testing.T) {
	// This is a placeholder test.
}

func TestInterruptMapping(t *testing.T) {
	// mount /proc if not already mounted
	if _, err := os.Stat("/proc/interrupts"); os.IsNotExist(err) {
		if err := os.MkdirAll("/proc", 0755); err != nil {
			t.Fatalf("failed to create /proc directory: %v", err)
		}

		err = syscall.Mount("proc", "/proc", "proc", 0, "")
		if err != nil {
			t.Fatalf("failed to mount /proc: %v", err)
		}

		defer syscall.Unmount("/proc", 0)
	}

	data, err := os.ReadFile("/proc/interrupts")
	if err != nil {
		t.Fatalf("failed to read /proc/interrupts: %v", err)
	}

	t.Logf("guest /proc/interrupts:\n%s", data)
}

func TestKernelLog(t *testing.T) {
	// use syscalls to read kernel log
	const klogSize = 1024 * 1024
	buf := make([]byte, klogSize)
	n, err := unix.Klogctl(unix.SYSLOG_ACTION_READ_ALL, buf)
	if err != nil {
		t.Fatalf("failed to read kernel log: %v", err)
	}

	logData := buf[:n]
	t.Logf("kernel log:\n%s", logData)
}

func TestVirtioFs(t *testing.T) {
	// mount the virtio-fs filesystem and verify it works
	tmpDir := "/mnt/virtiofs"
	err := os.MkdirAll(tmpDir, 0755)
	if err != nil {
		t.Fatalf("failed to create mount point: %v", err)
	}

	err = syscall.Mount("bringup", tmpDir, "virtiofs", 0, "")
	if err != nil {
		t.Fatalf("failed to mount virtio-fs: %v", err)
	}
	defer syscall.Unmount(tmpDir, 0)

	t.Logf("virtio-fs mounted at %s", tmpDir)

	testFS(t, tmpDir)
}
