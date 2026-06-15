//go:build linux && amd64

package kvm

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"j5.nz/cc/client"
	freebsdrootfs "j5.nz/cc/internal/freebsd/rootfs"
	"j5.nz/cc/internal/virtio"
)

func TestBootFreeBSDKernelToSerialFromEnv(t *testing.T) {
	kernelPath := os.Getenv("CC_FREEBSD_KERNEL")
	if kernelPath == "" {
		t.Skip("CC_FREEBSD_KERNEL is not set")
	}
	kernel, err := os.ReadFile(kernelPath)
	if err != nil {
		t.Fatalf("read kernel: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	marker := os.Getenv("CC_FREEBSD_MARKER")
	if marker == "" {
		marker = "FreeBSD"
	}
	serial, err := BootFreeBSDKernelToMarker(ctx, kernel, 1024, marker)
	if err != nil {
		t.Fatalf("boot FreeBSD kernel: %v\nserial:\n%s", err, serial)
	}
	if serial == "" {
		t.Fatalf("expected serial output")
	}
	t.Logf("serial:\n%s", serial)
}

func TestBootFreeBSDFullBaseRootFromReleaseSets(t *testing.T) {
	if os.Getenv("CC_TEST_FREEBSD_ROOTFS") == "" {
		t.Skip("set CC_TEST_FREEBSD_ROOTFS=1 to build and boot the full FreeBSD rootfs")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	rt, err := freebsdrootfs.BuildManagedRuntime(ctx, freebsdrootfs.Config{})
	if err != nil {
		t.Fatalf("build FreeBSD runtime: %v", err)
	}
	defer rt.Close()
	block := virtio.NewBlock(0, 0x1000, 10, rt.Root)
	serial, err := BootFreeBSDKernelToMarkerWithPCIBlockNetConsole(ctx, rt.Kernel, 1024, "random: unblocking device.", block, nil, nil)
	t.Logf("serial tail:\n%s", tailString(serial, 8192))
	if err != nil {
		t.Fatalf("boot FreeBSD full base root: %v\nserial:\n%s", err, serial)
	}
	if !strings.Contains(serial, "start_init: trying /sbin/init") {
		t.Fatalf("FreeBSD did not start /sbin/init:\n%s", serial)
	}
	if strings.Contains(serial, "init died") {
		t.Fatalf("FreeBSD init died:\n%s", serial)
	}
}

func TestFreeBSDManagedSessionExec(t *testing.T) {
	if os.Getenv("CC_TEST_FREEBSD_ROOTFS") == "" {
		t.Skip("set CC_TEST_FREEBSD_ROOTFS=1 to build and boot the full FreeBSD rootfs")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	rt, err := freebsdrootfs.BuildManagedRuntime(ctx, freebsdrootfs.Config{})
	if err != nil {
		t.Fatalf("build FreeBSD runtime: %v", err)
	}
	defer rt.Close()
	session, err := StartFreeBSDManagedSession(ctx, FreeBSDManagedConfig{
		Kernel:   rt.Kernel,
		Root:     rt.Root,
		MemoryMB: 1024,
	}, nil)
	if err != nil {
		t.Fatalf("start FreeBSD managed session: %v", err)
	}
	defer session.Close()
	resp, err := session.Exec(ctx, client.ExecRequest{
		Command: []string{"/bin/sh", "-c", "printf 'freebsd-managed:'; printf %s \"$(uname -s)\"; printf ':copy:'; cat"},
		Stdin:   []byte("stdin-ok"),
		WorkDir: "/tmp",
	})
	if err != nil {
		t.Fatalf("FreeBSD managed exec: %v", err)
	}
	if resp.ExitCode != 0 || strings.TrimSpace(resp.Output) != "freebsd-managed:FreeBSD:copy:stdin-ok" {
		t.Fatalf("FreeBSD exec response = code %d output %q", resp.ExitCode, resp.Output)
	}
}
