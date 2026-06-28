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
	"j5.nz/cc/internal/fsimage"
	ffsimage "j5.nz/cc/internal/fsimage/ffs"
	"j5.nz/cc/internal/nvme"
	"j5.nz/cc/internal/virtio"
)

// These KVM boot tests consume unstructured firmware/kernel serial logs.
// Substring checks here synchronize with guest prompts and markers rather than
// freezing user-facing copy.
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
	block := nvme.NewController(rt.Root)
	serial, err := BootFreeBSDKernelToMarkerWithNVMENetConsole(ctx, rt.Kernel, 1024, "Dual Console: Serial Primary", block, nil, nil)
	t.Logf("serial tail:\n%s", tailString(serial, 8192))
	if err != nil {
		t.Fatalf("boot FreeBSD full base root: %v\nserial:\n%s", err, serial)
	}
	if !strings.Contains(serial, "nvme0:") || !strings.Contains(serial, "Trying to mount root from ufs:/dev/nda0") {
		t.Fatalf("FreeBSD did not attach and mount root NVMe device:\n%s", serial)
	}
	if strings.Contains(serial, "mountroot>") || strings.Contains(serial, "failed with error") {
		t.Fatalf("FreeBSD failed to mount root block device:\n%s", serial)
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
	resp, err = session.Exec(ctx, client.ExecRequest{
		Command: []string{"/bin/sh", "-c", strings.Join([]string{
			"set -eu",
			"mount | grep -E ' on /(tmp|root) ' && exit 1 || true",
			"printf write-ok > /tmp/cc-freebsd-write-test",
			"printf root-write-ok > /root/cc-freebsd-root-write-test",
			"cat /tmp/cc-freebsd-write-test",
			"printf ':'",
			"cat /root/cc-freebsd-root-write-test",
		}, "\n")},
		WorkDir: "/tmp",
	})
	if err != nil {
		t.Fatalf("FreeBSD managed write exec: %v", err)
	}
	if resp.ExitCode != 0 || strings.TrimSpace(resp.Output) != "write-ok:root-write-ok" {
		t.Fatalf("FreeBSD write response = code %d output %q", resp.ExitCode, resp.Output)
	}
}

func TestFreeBSDManagedSessionFsckFFSGeneratedRootOnSecondDisk(t *testing.T) {
	if os.Getenv("CC_TEST_FREEBSD_ROOTFS") == "" {
		t.Skip("set CC_TEST_FREEBSD_ROOTFS=1 to build and fsck the full FreeBSD rootfs")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	t.Log("building FreeBSD managed runtime")
	rt, err := freebsdrootfs.BuildManagedRuntime(ctx, freebsdrootfs.Config{})
	if err != nil {
		t.Fatalf("build FreeBSD runtime: %v", err)
	}
	defer rt.Close()

	t.Log("building FreeBSD fsck candidate root")
	candidateRegion, err := fsimage.Build(ctx, rt.RootFS, fsimage.Options{
		Type:              fsimage.TypeFFS,
		FFSLayout:         ffsimage.LayoutRaw,
		DeterministicTime: time.Unix(1700000001, 0),
		ExtraBytes:        128 << 20,
	})
	if err != nil {
		t.Fatalf("build FreeBSD fsck candidate root: %v", err)
	}

	t.Log("booting FreeBSD managed session with candidate root on nda1")
	session, err := StartFreeBSDManagedSession(ctx, FreeBSDManagedConfig{
		Kernel:      rt.Kernel,
		Root:        rt.Root,
		ExtraBlocks: []virtio.BlockBackend{candidateRegion},
		MemoryMB:    1024,
	}, nil)
	if err != nil {
		t.Fatalf("start FreeBSD managed fsck session: %v", err)
	}
	defer session.Close()

	resp, err := session.Exec(ctx, client.ExecRequest{
		Command: []string{"/bin/sh", "-c", "/sbin/fsck_ffs -fn /dev/nda1"},
		WorkDir: "/tmp",
	})
	if err != nil {
		t.Fatalf("FreeBSD pre-write fsck exec: %v", err)
	}
	assertFreeBSDFsckClean(t, "pre-write", resp)

	resp, err = session.Exec(ctx, client.ExecRequest{
		Command: []string{"/bin/sh", "-c", strings.Join([]string{
			"set -eu",
			"mkdir -p /tmp/cc-ufs-write",
			"mount -t ufs /dev/nda1 /tmp/cc-ufs-write",
			"trap 'umount /tmp/cc-ufs-write || true' EXIT",
			"mkdir -p /tmp/cc-ufs-write/cc-write-test/nested",
			"printf 'freebsd-write-ok' > /tmp/cc-ufs-write/cc-write-test/nested/payload.txt",
			"dd if=/dev/zero bs=4096 count=3 2>/dev/null | tr '\\000' A > /tmp/cc-ufs-write/cc-write-test/nested/blob.bin",
			"sync",
			"cat /tmp/cc-ufs-write/cc-write-test/nested/payload.txt",
			"test \"$(wc -c < /tmp/cc-ufs-write/cc-write-test/nested/blob.bin | tr -d ' ')\" = 12288",
			"umount /tmp/cc-ufs-write",
			"trap - EXIT",
		}, "\n")},
		WorkDir: "/tmp",
	})
	if err != nil {
		t.Fatalf("FreeBSD write/mount exec: %v", err)
	}
	if resp.ExitCode != 0 || resp.Output != "freebsd-write-ok" {
		t.Fatalf("FreeBSD write/mount response = code %d output:\n%s", resp.ExitCode, resp.Output)
	}

	resp, err = session.Exec(ctx, client.ExecRequest{
		Command: []string{"/bin/sh", "-c", "/sbin/fsck_ffs -fn /dev/nda1"},
		WorkDir: "/tmp",
	})
	if err != nil {
		t.Fatalf("FreeBSD post-write fsck exec: %v", err)
	}
	assertFreeBSDFsckClean(t, "post-write", resp)
}

func assertFreeBSDFsckClean(t *testing.T, label string, resp client.ExecResponse) {
	t.Helper()
	t.Logf("FreeBSD %s fsck_ffs output:\n%s", label, resp.Output)
	if resp.ExitCode != 0 {
		t.Fatalf("FreeBSD %s fsck_ffs exit code = %d output:\n%s", label, resp.ExitCode, resp.Output)
	}
	// fsck_ffs produces unstructured diagnostic text. These are negative
	// consistency markers rather than user-facing text contracts.
	for _, bad := range []string{
		"BAD SUPER BLOCK",
		"INCORRECT",
		"UNREF",
		"BAD/DUP",
		"FREE BLK COUNT(S) WRONG",
		"SUMMARY INFORMATION BAD",
		"BLK(S) MISSING IN BIT MAPS",
		"FILE SYSTEM WAS MODIFIED",
		"INTEGRITY CHECK FAILED",
		"REBUILD CYLINDER GROUP",
		"MARKED",
		"failed:",
		"PHASE 5 SKIPPED",
		"UPDATE FILESYSTEM",
	} {
		if strings.Contains(resp.Output, bad) {
			t.Fatalf("FreeBSD %s fsck_ffs found FFS inconsistency %q:\n%s", label, bad, resp.Output)
		}
	}
}
