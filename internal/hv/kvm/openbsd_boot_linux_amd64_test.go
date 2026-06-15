//go:build linux && amd64

package kvm

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/fsimage"
	"j5.nz/cc/internal/fsmeta"
	"j5.nz/cc/internal/imagefs"
	"j5.nz/cc/internal/netstack"
	openbsdguestinit "j5.nz/cc/internal/openbsd/guestinit"
	"j5.nz/cc/internal/virtio"
)

func TestBootOpenBSD79BSDRDToSerial(t *testing.T) {
	if os.Getenv("CC_TEST_OPENBSD_KVM") == "" {
		t.Skip("set CC_TEST_OPENBSD_KVM=1 to run OpenBSD KVM boot smoke test")
	}
	kernelPath := filepath.Join("..", "..", "..", "local", "openbsd79-amd64", "bsd.rd")
	kernel, err := os.ReadFile(kernelPath)
	if err != nil {
		if os.IsNotExist(err) {
			t.Skipf("OpenBSD fixture not present: %s", kernelPath)
		}
		t.Fatalf("read fixture: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	serial, err := BootOpenBSDKernelToMarker(ctx, kernel, 256, "(I)nstall")
	if err != nil {
		t.Fatalf("boot OpenBSD: %v\nserial:\n%s", err, serial)
	}
	if !strings.Contains(serial, "(I)nstall") {
		t.Fatalf("serial did not contain installer marker:\n%s", serial)
	}
}

func TestBootOpenBSD79BSDRDWithVirtioPCIBlock(t *testing.T) {
	if os.Getenv("CC_TEST_OPENBSD_KVM") == "" {
		t.Skip("set CC_TEST_OPENBSD_KVM=1 to run OpenBSD KVM boot smoke test")
	}
	kernelPath := filepath.Join("..", "..", "..", "local", "openbsd79-amd64", "bsd.rd")
	kernel, err := os.ReadFile(kernelPath)
	if err != nil {
		if os.IsNotExist(err) {
			t.Skipf("OpenBSD fixture not present: %s", kernelPath)
		}
		t.Fatalf("read fixture: %v", err)
	}
	diskPath := filepath.Join("..", "..", "..", "local", "openbsd79-amd64", "miniroot79.img")
	diskData, err := os.ReadFile(diskPath)
	if err != nil {
		if os.IsNotExist(err) {
			t.Skipf("OpenBSD disk fixture not present: %s", diskPath)
		}
		t.Fatalf("read disk fixture: %v", err)
	}
	disk := newTestDisk(len(diskData))
	if n, err := disk.WriteAt(diskData, 0); err != nil || n != len(diskData) {
		t.Fatalf("seed disk: n=%d err=%v", n, err)
	}
	block := virtio.NewBlock(0, 0x1000, 10, disk)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	serial, err := BootOpenBSDKernelToMarkerWithPCIBlock(ctx, kernel, 256, "vioblk0 at virtio", block)
	if err != nil {
		t.Fatalf("boot OpenBSD with virtio-pci block: %v\nserial:\n%s", err, serial)
	}
	if !strings.Contains(serial, "vioblk0 at virtio") {
		t.Fatalf("serial did not contain vioblk attachment:\n%s", serial)
	}
}

func TestBootOpenBSD79RegularKernelWithVirtioPCIBlock(t *testing.T) {
	if os.Getenv("CC_TEST_OPENBSD_KVM") == "" {
		t.Skip("set CC_TEST_OPENBSD_KVM=1 to run OpenBSD KVM boot smoke test")
	}
	kernelPath := filepath.Join("..", "..", "..", "local", "openbsd79-amd64", "bsd")
	kernel, err := os.ReadFile(kernelPath)
	if err != nil {
		if os.IsNotExist(err) {
			t.Skipf("OpenBSD fixture not present: %s", kernelPath)
		}
		t.Fatalf("read fixture: %v", err)
	}
	diskPath := filepath.Join("..", "..", "..", "local", "openbsd79-amd64", "miniroot79.img")
	diskData, err := os.ReadFile(diskPath)
	if err != nil {
		if os.IsNotExist(err) {
			t.Skipf("OpenBSD disk fixture not present: %s", diskPath)
		}
		t.Fatalf("read disk fixture: %v", err)
	}
	disk := newTestDisk(len(diskData))
	if n, err := disk.WriteAt(diskData, 0); err != nil || n != len(diskData) {
		t.Fatalf("seed disk: n=%d err=%v", n, err)
	}
	block := virtio.NewBlock(0, 0x1000, 10, disk)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	answeredRoot := false
	answeredSwap := false
	answeredDump := false
	serial, err := BootOpenBSDKernelToMarkerWithPCIBlockConsole(ctx, kernel, 256, "Automatic boot in progress", block, func(serial string) []byte {
		if !answeredRoot && strings.Contains(serial, "root device") {
			answeredRoot = true
			return []byte("sd0a\n")
		}
		if !answeredSwap && strings.Contains(serial, "swap device") {
			answeredSwap = true
			return []byte("\n")
		}
		if !answeredDump && strings.Contains(serial, "dump device") {
			answeredDump = true
			return []byte("\n")
		}
		return nil
	})
	t.Logf("regular OpenBSD serial tail:\n%s", tailString(serial, 4096))
	if err != nil && !strings.Contains(serial, "root on ") {
		t.Fatalf("boot OpenBSD regular kernel with virtio-pci block: %v\nserial:\n%s", err, serial)
	}
	if !strings.Contains(serial, "Automatic boot in progress") {
		t.Fatalf("serial did not reach miniroot init:\n%s", serial)
	}
}

func TestBootOpenBSD79RegularKernelWithGeneratedFFSRoot(t *testing.T) {
	if os.Getenv("CC_TEST_OPENBSD_KVM") == "" {
		t.Skip("set CC_TEST_OPENBSD_KVM=1 to run OpenBSD KVM boot smoke test")
	}
	kernelPath := filepath.Join("..", "..", "..", "local", "openbsd79-amd64", "bsd")
	kernel, err := os.ReadFile(kernelPath)
	if err != nil {
		if os.IsNotExist(err) {
			t.Skipf("OpenBSD fixture not present: %s", kernelPath)
		}
		t.Fatalf("read fixture: %v", err)
	}
	root := buildOpenBSDGoInitRoot(t)
	region, err := fsimage.Build(context.Background(), root, fsimage.Options{
		Type:              fsimage.TypeFFS,
		SizeBytes:         128 << 20,
		DeterministicTime: time.Unix(1700000000, 0),
	})
	if err != nil {
		t.Fatalf("build FFS root: %v", err)
	}
	block := virtio.NewBlock(0, 0x1000, 10, region)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	answeredRoot := false
	answeredSwap := false
	answeredDump := false
	serial, err := BootOpenBSDKernelToMarkerWithPCIBlockConsole(ctx, kernel, 512, "OPENBSD_GO_INIT_OK", block, func(serial string) []byte {
		if !answeredRoot && strings.Contains(serial, "root device") {
			answeredRoot = true
			return []byte("sd0a\n")
		}
		if !answeredSwap && strings.Contains(serial, "swap device") {
			answeredSwap = true
			return []byte("\n")
		}
		if !answeredDump && strings.Contains(serial, "dump device") {
			answeredDump = true
			return []byte("\n")
		}
		return nil
	})
	t.Logf("generated FFS OpenBSD serial tail:\n%s", tailString(serial, 4096))
	if err != nil {
		t.Fatalf("boot OpenBSD with generated FFS root: %v\nserial:\n%s", err, serial)
	}
}

func TestBootOpenBSD79RegularKernelWithVirtioPCINet(t *testing.T) {
	if os.Getenv("CC_TEST_OPENBSD_KVM") == "" {
		t.Skip("set CC_TEST_OPENBSD_KVM=1 to run OpenBSD KVM boot smoke test")
	}
	kernelPath := filepath.Join("..", "..", "..", "local", "openbsd79-amd64", "bsd")
	kernel, err := os.ReadFile(kernelPath)
	if err != nil {
		if os.IsNotExist(err) {
			t.Skipf("OpenBSD fixture not present: %s", kernelPath)
		}
		t.Fatalf("read fixture: %v", err)
	}
	root := buildOpenBSDNetInitRoot(t)
	region, err := fsimage.Build(context.Background(), root, fsimage.Options{
		Type:              fsimage.TypeFFS,
		SizeBytes:         128 << 20,
		DeterministicTime: time.Unix(1700000000, 0),
	})
	if err != nil {
		t.Fatalf("build FFS root: %v", err)
	}
	block := virtio.NewBlock(0, 0x1000, 10, region)
	netdev := virtio.NewNet(0, 0x1000, 11, nil, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	answeredRoot := false
	answeredSwap := false
	answeredDump := false
	serial, err := BootOpenBSDKernelToMarkerWithPCIBlockNetConsole(ctx, kernel, 512, "OPENBSD_VIO0_FOUND", block, netdev, func(serial string) []byte {
		if !answeredRoot && strings.Contains(serial, "root device") {
			answeredRoot = true
			return []byte("sd0a\n")
		}
		if !answeredSwap && strings.Contains(serial, "swap device") {
			answeredSwap = true
			return []byte("\n")
		}
		if !answeredDump && strings.Contains(serial, "dump device") {
			answeredDump = true
			return []byte("\n")
		}
		return nil
	})
	t.Logf("generated FFS OpenBSD virtio-net serial tail:\n%s", tailString(serial, 4096))
	if err != nil {
		t.Fatalf("boot OpenBSD with generated FFS root and virtio-net: %v\nserial:\n%s", err, serial)
	}
	if !strings.Contains(serial, "vio0 at virtio") {
		t.Fatalf("serial did not contain vio0 attachment:\n%s", serial)
	}
}

func TestBootOpenBSD79RegularKernelWithVirtioPCINetControl(t *testing.T) {
	if os.Getenv("CC_TEST_OPENBSD_KVM") == "" {
		t.Skip("set CC_TEST_OPENBSD_KVM=1 to run OpenBSD KVM boot smoke test")
	}
	kernelPath := filepath.Join("..", "..", "..", "local", "openbsd79-amd64", "bsd")
	kernel, err := os.ReadFile(kernelPath)
	if err != nil {
		if os.IsNotExist(err) {
			t.Skipf("OpenBSD fixture not present: %s", kernelPath)
		}
		t.Fatalf("read fixture: %v", err)
	}
	root := buildOpenBSDFullBaseSetRootWithInit(t, []byte(openBSDNetControlInitScript))
	overlay := imagefs.NewOverlay(root)
	if err := overlay.AddFile("/sbin/ccnetctl", 0o755, buildOpenBSDGoInit(t, openBSDNetControlAgentSource)); err != nil {
		t.Fatalf("overlay net control agent: %v", err)
	}
	root = overlay.Root()
	region, err := fsimage.Build(context.Background(), root, fsimage.Options{
		Type:              fsimage.TypeFFS,
		DeterministicTime: time.Unix(1700000000, 0),
	})
	if err != nil {
		t.Fatalf("build full base-set FFS root: %v", err)
	}
	netdev, stack := newOpenBSDTestNet(t)
	defer stack.Close()
	ln, err := stack.ListenInternal("tcp", ":12345")
	if err != nil {
		t.Fatalf("listen control tcp: %v", err)
	}
	defer ln.Close()
	accepted := make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			accepted <- "accept: " + err.Error()
			return
		}
		defer conn.Close()
		buf := make([]byte, 128)
		n, err := conn.Read(buf)
		if err != nil {
			accepted <- "read: " + err.Error()
			return
		}
		if _, err := conn.Write([]byte("OPENBSD_NET_CONTROL_ACK\n")); err != nil {
			accepted <- "write: " + err.Error()
			return
		}
		accepted <- string(buf[:n])
	}()
	block := virtio.NewBlock(0, 0x1000, 10, region)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	answeredRoot := false
	answeredSwap := false
	serial, err := BootOpenBSDKernelToMarkerWithPCIBlockNetConsole(ctx, kernel, 768, "OPENBSD_NET_CONTROL_OK", block, netdev, func(serial string) []byte {
		if !answeredRoot && strings.Contains(serial, "root device:") {
			answeredRoot = true
			return []byte("sd0a\n")
		}
		if answeredRoot && !answeredSwap && strings.Contains(serial, "swap device") {
			answeredSwap = true
			return []byte("\n")
		}
		return nil
	})
	t.Logf("generated FFS OpenBSD virtio-net control serial tail:\n%s", tailString(serial, 4096))
	if err != nil {
		t.Fatalf("boot OpenBSD with virtio-net control: %v\nserial:\n%s", err, serial)
	}
	select {
	case got := <-accepted:
		if strings.TrimSpace(got) != "OPENBSD_NET_CONTROL_HELLO" {
			t.Fatalf("host control received %q, want hello", got)
		}
	default:
		t.Fatal("host control listener did not accept guest connection")
	}
}

func TestOpenBSD79ManagedSessionExecAndCopy(t *testing.T) {
	if os.Getenv("CC_TEST_OPENBSD_KVM") == "" {
		t.Skip("set CC_TEST_OPENBSD_KVM=1 to run OpenBSD KVM managed session test")
	}
	kernelPath := filepath.Join("..", "..", "..", "local", "openbsd79-amd64", "bsd")
	kernel, err := os.ReadFile(kernelPath)
	if err != nil {
		if os.IsNotExist(err) {
			t.Skipf("OpenBSD fixture not present: %s", kernelPath)
		}
		t.Fatalf("read fixture: %v", err)
	}
	initBin, err := openbsdguestinit.Build(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("build OpenBSD guest init: %v", err)
	}
	root := buildOpenBSDManagedRoot(t, initBin)
	region, err := fsimage.Build(context.Background(), root, fsimage.Options{
		Type:              fsimage.TypeFFS,
		DeterministicTime: time.Unix(1700000000, 0),
	})
	if err != nil {
		t.Fatalf("build full base-set FFS root: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	session, err := StartOpenBSDManagedSession(ctx, OpenBSDManagedConfig{
		Kernel:   kernel,
		Root:     region,
		MemoryMB: 768,
	}, nil)
	if err != nil {
		t.Fatalf("start OpenBSD managed session: %v", err)
	}
	defer func() {
		if session != nil {
			_ = session.Close()
		}
	}()

	resp, err := session.Exec(ctx, client.ExecRequest{
		Command: []string{"/bin/sh", "-c", "printf 'managed:%s:' \"$PWD\"; cat"},
		WorkDir: "/tmp",
		Env:     []string{"PATH=/bin:/sbin:/usr/bin:/usr/sbin", "HOME=/root"},
		Stdin:   []byte("stdin-ok"),
	})
	if err != nil {
		t.Fatalf("managed exec: %v", err)
	}
	if resp.ExitCode != 0 || resp.Output != "managed:/tmp:stdin-ok" {
		t.Fatalf("exec response = code %d output %q", resp.ExitCode, resp.Output)
	}
	resp, err = session.Exec(ctx, client.ExecRequest{
		Command: []string{"/bin/echo", "no-stdin-ok"},
		Env:     []string{"PATH=/bin:/sbin:/usr/bin:/usr/sbin", "HOME=/root"},
	})
	if err != nil {
		t.Fatalf("managed no-stdin exec: %v", err)
	}
	if resp.ExitCode != 0 || resp.Output != "no-stdin-ok" {
		t.Fatalf("no-stdin exec response = code %d output %q", resp.ExitCode, resp.Output)
	}

	writeEvents := collectOpenBSDManagedEvents(t, session, ctx, client.ExecRequest{
		Kind:  "fs_write",
		Path:  "/tmp/copied.txt",
		Stdin: []byte("copy-through-control\n"),
	}, nil)
	if got := lastExitCode(writeEvents); got != 0 {
		t.Fatalf("fs_write exit = %d events=%+v", got, writeEvents)
	}
	archiveEvents := collectOpenBSDManagedEvents(t, session, ctx, client.ExecRequest{
		Kind: "fs_archive",
		Path: "/tmp/copied.txt",
	}, nil)
	if got := lastExitCode(archiveEvents); got != 0 {
		t.Fatalf("fs_archive exit = %d events=%+v", got, archiveEvents)
	}
	contents := untarSingleFile(t, stdoutBytes(archiveEvents), "copied.txt")
	if string(contents) != "copy-through-control\n" {
		t.Fatalf("copied contents = %q", contents)
	}

	extractTar := tarSingleFile(t, "streamed.txt", []byte("streamed-copy\n"))
	extractEvents := collectOpenBSDManagedEvents(t, session, ctx, client.ExecRequest{
		Kind:      "fs_extract",
		Path:      "/tmp",
		Directory: true,
		Stdin:     extractTar,
	}, nil)
	if got := lastExitCode(extractEvents); got != 0 {
		t.Fatalf("fs_extract exit = %d events=%+v", got, extractEvents)
	}
	extractedArchiveEvents := collectOpenBSDManagedEvents(t, session, ctx, client.ExecRequest{
		Kind: "fs_archive",
		Path: "/tmp/streamed.txt",
	}, nil)
	if got := lastExitCode(extractedArchiveEvents); got != 0 {
		t.Fatalf("fs_archive extracted exit = %d events=%+v", got, extractedArchiveEvents)
	}
	extracted := untarSingleFile(t, stdoutBytes(extractedArchiveEvents), "streamed.txt")
	if string(extracted) != "streamed-copy\n" {
		t.Fatalf("extracted contents = %q", extracted)
	}

	streamInputs := make(chan client.ExecInput, 3)
	streamTar := tarSingleFile(t, "chunked.txt", []byte("chunked-copy\n"))
	streamInputs <- client.ExecInput{Kind: "stdin", Data: streamTar[:len(streamTar)/2]}
	streamInputs <- client.ExecInput{Kind: "stdin", Data: streamTar[len(streamTar)/2:]}
	streamInputs <- client.ExecInput{Kind: "stdin_close"}
	close(streamInputs)
	streamEvents := collectOpenBSDManagedEvents(t, session, ctx, client.ExecRequest{
		Kind:      "fs_extract",
		Path:      "/tmp",
		Directory: true,
	}, streamInputs)
	if got := lastExitCode(streamEvents); got != 0 {
		t.Fatalf("streamed fs_extract exit = %d events=%+v", got, streamEvents)
	}
	streamArchiveEvents := collectOpenBSDManagedEvents(t, session, ctx, client.ExecRequest{
		Kind: "fs_archive",
		Path: "/tmp/chunked.txt",
	}, nil)
	if got := lastExitCode(streamArchiveEvents); got != 0 {
		t.Fatalf("fs_archive streamed exit = %d events=%+v", got, streamArchiveEvents)
	}
	streamed := untarSingleFile(t, stdoutBytes(streamArchiveEvents), "chunked.txt")
	if string(streamed) != "chunked-copy\n" {
		t.Fatalf("streamed contents = %q", streamed)
	}

	if err := session.Flush(ctx); err != nil {
		t.Fatalf("flush OpenBSD managed session: %v", err)
	}
	resp, err = session.Exec(ctx, client.ExecRequest{
		Command: []string{"/sbin/mount", "-ur", "/"},
		Env:     []string{"PATH=/bin:/sbin:/usr/bin:/usr/sbin", "HOME=/root"},
	})
	if err != nil {
		t.Fatalf("remount root read-only: %v", err)
	}
	if resp.ExitCode != 0 {
		t.Fatalf("remount root read-only exit = %d output %q", resp.ExitCode, resp.Output)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("close first OpenBSD managed session: %v", err)
	}
	session = nil

	session, err = StartOpenBSDManagedSession(ctx, OpenBSDManagedConfig{
		Kernel:   kernel,
		Root:     region,
		MemoryMB: 768,
	}, nil)
	if err != nil {
		t.Fatalf("restart OpenBSD managed session: %v", err)
	}
	rebootArchiveEvents := collectOpenBSDManagedEvents(t, session, ctx, client.ExecRequest{
		Kind: "fs_archive",
		Path: "/tmp/copied.txt",
	}, nil)
	if got := lastExitCode(rebootArchiveEvents); got != 0 {
		t.Fatalf("fs_archive after reboot exit = %d events=%+v", got, rebootArchiveEvents)
	}
	rebootContents := untarSingleFile(t, stdoutBytes(rebootArchiveEvents), "copied.txt")
	if string(rebootContents) != "copy-through-control\n" {
		t.Fatalf("reboot copied contents = %q", rebootContents)
	}
	rebootStreamArchiveEvents := collectOpenBSDManagedEvents(t, session, ctx, client.ExecRequest{
		Kind: "fs_archive",
		Path: "/tmp/chunked.txt",
	}, nil)
	if got := lastExitCode(rebootStreamArchiveEvents); got != 0 {
		t.Fatalf("fs_archive streamed after reboot exit = %d events=%+v", got, rebootStreamArchiveEvents)
	}
	rebootStreamed := untarSingleFile(t, stdoutBytes(rebootStreamArchiveEvents), "chunked.txt")
	if string(rebootStreamed) != "chunked-copy\n" {
		t.Fatalf("reboot streamed contents = %q", rebootStreamed)
	}
	if err := session.Flush(ctx); err != nil {
		t.Fatalf("flush restarted OpenBSD managed session: %v", err)
	}
	resp, err = session.Exec(ctx, client.ExecRequest{
		Command: []string{"/sbin/mount", "-ur", "/"},
		Env:     []string{"PATH=/bin:/sbin:/usr/bin:/usr/sbin", "HOME=/root"},
	})
	if err != nil {
		t.Fatalf("remount restarted root read-only: %v", err)
	}
	if resp.ExitCode != 0 {
		t.Fatalf("remount restarted root read-only exit = %d output %q", resp.ExitCode, resp.Output)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("close restarted OpenBSD managed session: %v", err)
	}
	session = nil

	fsckKernelPath := filepath.Join("..", "..", "..", "local", "openbsd79-amd64", "bsd.rd")
	fsckKernel, err := os.ReadFile(fsckKernelPath)
	if err != nil {
		if os.IsNotExist(err) {
			t.Skipf("OpenBSD bsd.rd fixture not present: %s", fsckKernelPath)
		}
		t.Fatalf("read bsd.rd fixture: %v", err)
	}
	assertOpenBSDFFSReadOnlyFsckClean(t, fsckKernel, region, 768, "OPENBSD_MANAGED_FSCK_FFS_DONE")
}

func TestBootOpenBSD79RegularKernelWithBaseSetRootAndGoInit(t *testing.T) {
	if os.Getenv("CC_TEST_OPENBSD_KVM") == "" {
		t.Skip("set CC_TEST_OPENBSD_KVM=1 to run OpenBSD KVM boot smoke test")
	}
	kernelPath := filepath.Join("..", "..", "..", "local", "openbsd79-amd64", "bsd")
	kernel, err := os.ReadFile(kernelPath)
	if err != nil {
		if os.IsNotExist(err) {
			t.Skipf("OpenBSD fixture not present: %s", kernelPath)
		}
		t.Fatalf("read fixture: %v", err)
	}
	root := buildOpenBSDBaseSetRoot(t)
	region, err := fsimage.Build(context.Background(), root, fsimage.Options{
		Type:              fsimage.TypeFFS,
		SizeBytes:         224 << 20,
		DeterministicTime: time.Unix(1700000000, 0),
	})
	if err != nil {
		t.Fatalf("build FFS root: %v", err)
	}
	block := virtio.NewBlock(0, 0x1000, 10, region)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	answeredRoot := false
	answeredSwap := false
	answeredDump := false
	serial, err := BootOpenBSDKernelToMarkerWithPCIBlockConsole(ctx, kernel, 512, "OPENBSD_GO_INIT_OK", block, func(serial string) []byte {
		if !answeredRoot && strings.Contains(serial, "root device") {
			answeredRoot = true
			return []byte("sd0a\n")
		}
		if !answeredSwap && strings.Contains(serial, "swap device") {
			answeredSwap = true
			return []byte("\n")
		}
		if !answeredDump && strings.Contains(serial, "dump device") {
			answeredDump = true
			return []byte("\n")
		}
		return nil
	})
	t.Logf("generated FFS OpenBSD base-set serial tail:\n%s", tailString(serial, 4096))
	if err != nil {
		t.Fatalf("boot OpenBSD with base-set FFS root: %v\nserial:\n%s", err, serial)
	}
}

func TestBootOpenBSD79RegularKernelWithFullBaseSetRootAndShellInit(t *testing.T) {
	if os.Getenv("CC_TEST_OPENBSD_KVM") == "" {
		t.Skip("set CC_TEST_OPENBSD_KVM=1 to run OpenBSD KVM boot smoke test")
	}
	kernelPath := filepath.Join("..", "..", "..", "local", "openbsd79-amd64", "bsd")
	kernel, err := os.ReadFile(kernelPath)
	if err != nil {
		if os.IsNotExist(err) {
			t.Skipf("OpenBSD fixture not present: %s", kernelPath)
		}
		t.Fatalf("read fixture: %v", err)
	}
	root := buildOpenBSDFullBaseSetRoot(t)
	region, err := fsimage.Build(context.Background(), root, fsimage.Options{
		Type:              fsimage.TypeFFS,
		DeterministicTime: time.Unix(1700000000, 0),
	})
	if err != nil {
		t.Fatalf("build full base-set FFS root: %v", err)
	}
	block := virtio.NewBlock(0, 0x1000, 10, region)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	answeredRoot := false
	answeredSwap := false
	serial, err := BootOpenBSDKernelToMarkerWithPCIBlockConsole(ctx, kernel, 768, "OPENBSD_FULL_BASE_INIT_OK", block, func(serial string) []byte {
		if !answeredRoot && strings.Contains(serial, "root device:") {
			answeredRoot = true
			return []byte("sd0a\n")
		}
		if answeredRoot && !answeredSwap && strings.Contains(serial, "swap device") {
			answeredSwap = true
			return []byte("\n")
		}
		return nil
	})
	t.Logf("generated FFS OpenBSD full base-set serial tail:\n%s", tailString(serial, 4096))
	if err != nil {
		t.Fatalf("boot OpenBSD with full base-set FFS root: %v\nserial:\n%s", err, serial)
	}
}

func TestOpenBSD79FsckFFSGeneratedBaseSetRoot(t *testing.T) {
	if os.Getenv("CC_TEST_OPENBSD_KVM") == "" {
		t.Skip("set CC_TEST_OPENBSD_KVM=1 to run OpenBSD KVM boot smoke test")
	}
	kernelPath := filepath.Join("..", "..", "..", "local", "openbsd79-amd64", "bsd.rd")
	kernel, err := os.ReadFile(kernelPath)
	if err != nil {
		if os.IsNotExist(err) {
			t.Skipf("OpenBSD fixture not present: %s", kernelPath)
		}
		t.Fatalf("read fixture: %v", err)
	}

	candidateOverlay := imagefs.NewOverlay(buildOpenBSDBaseSetRoot(t))
	if err := candidateOverlay.AddFile("/aaa/blob", 0o644, []byte("layout-sensitive file\n")); err != nil {
		t.Fatalf("add layout-sensitive candidate file: %v", err)
	}
	candidateRegion, err := fsimage.Build(context.Background(), candidateOverlay.Root(), fsimage.Options{
		Type:              fsimage.TypeFFS,
		SizeBytes:         224 << 20,
		DeterministicTime: time.Unix(1700000000, 0),
	})
	if err != nil {
		t.Fatalf("build candidate FFS root: %v", err)
	}

	assertOpenBSDFFSReadOnlyFsckClean(t, kernel, candidateRegion, 512, "OPENBSD_FSCK_FFS_DONE")
}

func TestOpenBSD79FsckFFSGeneratedFullBaseSetRoot(t *testing.T) {
	if os.Getenv("CC_TEST_OPENBSD_KVM") == "" || os.Getenv("CC_TEST_OPENBSD_FULL_BASE_FFS") == "" {
		t.Skip("set CC_TEST_OPENBSD_KVM=1 and CC_TEST_OPENBSD_FULL_BASE_FFS=1 to run full OpenBSD FFS fsck smoke test")
	}
	kernelPath := filepath.Join("..", "..", "..", "local", "openbsd79-amd64", "bsd.rd")
	kernel, err := os.ReadFile(kernelPath)
	if err != nil {
		if os.IsNotExist(err) {
			t.Skipf("OpenBSD fixture not present: %s", kernelPath)
		}
		t.Fatalf("read fixture: %v", err)
	}

	candidateRegion, err := fsimage.Build(context.Background(), buildOpenBSDFullBaseSetRoot(t), fsimage.Options{
		Type:              fsimage.TypeFFS,
		DeterministicTime: time.Unix(1700000000, 0),
	})
	if err != nil {
		t.Fatalf("build full candidate FFS root: %v", err)
	}

	assertOpenBSDFFSReadOnlyFsckClean(t, kernel, candidateRegion, 768, "OPENBSD_FULL_FSCK_FFS_DONE")
}

func assertOpenBSDFFSReadOnlyFsckClean(t *testing.T, kernel []byte, region fsimage.FilesystemRegion, memoryMB uint64, marker string) {
	t.Helper()
	block := virtio.NewBlock(0, 0x1000, 10, region)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	answeredShell := false
	ranFsck := false
	serial, err := BootOpenBSDKernelToMarkerWithPCIBlockConsole(ctx, kernel, memoryMB, marker, block, func(serial string) []byte {
		if !answeredShell && strings.Contains(serial, "(I)nstall") {
			answeredShell = true
			return []byte("S\n")
		}
		if answeredShell && !ranFsck && strings.Contains(serial, "# ") {
			ranFsck = true
			return []byte("cd /dev && sh MAKEDEV sd0; fsck_ffs -fn /dev/rsd0a; echo " + marker + "\n")
		}
		return nil
	})
	t.Logf("OpenBSD fsck_ffs validator serial tail:\n%s", tailString(serial, 8192))
	if err != nil {
		t.Fatalf("boot OpenBSD fsck_ffs validator: %v\nserial:\n%s", err, serial)
	}
	if !strings.Contains(serial, marker) {
		t.Fatalf("OpenBSD fsck_ffs did not complete for generated FFS image:\n%s", serial)
	}
	for _, bad := range []string{
		"BAD SUPER BLOCK",
		"INCORRECT",
		"UNREF",
		"BAD/DUP",
		"FREE BLK COUNT(S) WRONG",
		"SUMMARY INFORMATION BAD",
		"BLK(S) MISSING IN BIT MAPS",
		"FILE SYSTEM WAS MODIFIED",
	} {
		if strings.Contains(serial, bad) {
			t.Fatalf("OpenBSD fsck_ffs found FFS inconsistency %q:\n%s", bad, serial)
		}
	}
}

func tailString(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[len(s)-max:]
}

func buildOpenBSDGoInitRoot(t *testing.T) imagefs.Directory {
	t.Helper()
	return buildOpenBSDRoot(t, openBSDGoInitSource)
}

func buildOpenBSDNetInitRoot(t *testing.T) imagefs.Directory {
	t.Helper()
	return buildOpenBSDRoot(t, openBSDNetInitSource)
}

func buildOpenBSDBaseSetRoot(t *testing.T) imagefs.Directory {
	t.Helper()
	f, err := os.Open(openBSDBaseSetPath(t))
	if err != nil {
		t.Fatalf("open OpenBSD base set: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("read OpenBSD base gzip: %v", err)
	}
	t.Cleanup(func() { _ = gz.Close() })
	tfs, err := imagefs.NewTarFSWithOptions(context.Background(), gz, imagefs.TarFSOptions{
		Include: includeOpenBSDBaseBootFile,
	})
	if err != nil {
		t.Fatalf("read OpenBSD base set: %v", err)
	}
	t.Cleanup(func() { _ = tfs.Close() })
	overlay := imagefs.NewOverlay(tfs.Root())
	addOpenBSDRuntimeLibraryLinks(t, overlay, tfs.Root())
	if err := overlay.AddDevice("/dev/console", fs.ModeDevice|fs.ModeCharDevice|0o600, 0); err != nil {
		t.Fatalf("add console device: %v", err)
	}
	if err := overlay.AddDevice("/dev/null", fs.ModeDevice|fs.ModeCharDevice|0o666, 514); err != nil {
		t.Fatalf("add null device: %v", err)
	}
	initBin := buildOpenBSDGoInit(t)
	if err := overlay.AddFile("/sbin/init", 0o755, initBin); err != nil {
		t.Fatalf("overlay Go init: %v", err)
	}
	return overlay.Root()
}

func buildOpenBSDFullBaseSetRoot(t *testing.T) imagefs.Directory {
	t.Helper()
	return buildOpenBSDFullBaseSetRootWithInit(t, []byte("#!/bin/sh\n/bin/echo OPENBSD_FULL_BASE_INIT_OK >/dev/console\nwhile :; do /bin/sleep 3600; done\n"))
}

func buildOpenBSDFullBaseSetRootWithGoInit(t *testing.T, source string) imagefs.Directory {
	t.Helper()
	return buildOpenBSDFullBaseSetRootWithInit(t, buildOpenBSDGoInit(t, source))
}

func buildOpenBSDFullBaseSetRootWithInit(t *testing.T, init []byte) imagefs.Directory {
	t.Helper()
	f, err := os.Open(openBSDBaseSetPath(t))
	if err != nil {
		t.Fatalf("open OpenBSD base set: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("read OpenBSD base gzip: %v", err)
	}
	t.Cleanup(func() { _ = gz.Close() })
	tfs, err := imagefs.NewTarFS(context.Background(), gz)
	if err != nil {
		t.Fatalf("read full OpenBSD base set: %v", err)
	}
	t.Cleanup(func() { _ = tfs.Close() })
	overlay := imagefs.NewOverlay(tfs.Root())
	addOpenBSDRuntimeLibraryLinks(t, overlay, tfs.Root())
	for _, dev := range []struct {
		path string
		mode fs.FileMode
		rdev uint32
	}{
		{"/dev/console", fs.ModeDevice | fs.ModeCharDevice | 0o600, 0},
		{"/dev/null", fs.ModeDevice | fs.ModeCharDevice | 0o666, 514},
		{"/dev/zero", fs.ModeDevice | fs.ModeCharDevice | 0o666, 515},
		{"/dev/random", fs.ModeDevice | fs.ModeCharDevice | 0o644, 565},
		{"/dev/urandom", fs.ModeDevice | fs.ModeCharDevice | 0o644, 566},
		{"/dev/sd0a", fs.ModeDevice | 0o640, 1024},
		{"/dev/sd0b", fs.ModeDevice | 0o640, 1025},
	} {
		if err := overlay.AddDevice(dev.path, dev.mode, dev.rdev); err != nil {
			t.Fatalf("add %s: %v", dev.path, err)
		}
	}
	if err := overlay.AddFile("/sbin/init", 0o755, init); err != nil {
		t.Fatalf("overlay init: %v", err)
	}
	return overlay.Root()
}

func buildOpenBSDManagedRoot(t *testing.T, initBin []byte) imagefs.Directory {
	t.Helper()
	root := buildOpenBSDFullBaseSetRootWithInit(t, []byte(openBSDManagedInitScript))
	overlay := imagefs.NewOverlay(root)
	for _, file := range []struct {
		path string
		mode fs.FileMode
		data []byte
	}{
		{"/sbin/cc-openbsd-init", 0o755, initBin},
		{"/etc/fstab", 0o644, []byte("/dev/sd0a / ffs rw 1 1\n")},
		{"/etc/myname", 0o644, []byte("cc-openbsd\n")},
		{"/etc/resolv.conf", 0o644, []byte("nameserver 10.42.0.1\n")},
		{"/etc/hosts", 0o644, []byte("127.0.0.1 localhost\n10.42.0.2 cc-openbsd\n")},
	} {
		if err := overlay.AddFile(file.path, file.mode, file.data); err != nil {
			t.Fatalf("overlay %s: %v", file.path, err)
		}
	}
	return overlay.Root()
}

type openBSDNetStackBackend struct {
	iface *netstack.NetworkInterface
}

func (b openBSDNetStackBackend) HandleTxPacket(packet []byte) error {
	return b.iface.DeliverGuestPacket(packet, true)
}

func newOpenBSDTestNet(t *testing.T) (*virtio.Net, *netstack.NetStack) {
	t.Helper()
	mac := net.HardwareAddr{0x02, 0x42, 0x0a, 0x2a, 0x00, 0x02}
	stack := netstack.New(slog.Default())
	if err := stack.SetGuestMAC(mac); err != nil {
		t.Fatalf("set guest mac: %v", err)
	}
	if err := stack.SetGuestIPv4(net.IPv4(10, 42, 0, 2)); err != nil {
		t.Fatalf("set guest ipv4: %v", err)
	}
	iface, err := stack.AttachNetworkInterface()
	if err != nil {
		t.Fatalf("attach netstack interface: %v", err)
	}
	dev := virtio.NewNet(0, 0x1000, 11, mac, openBSDNetStackBackend{iface: iface})
	iface.AttachVirtioBackend(func(frame []byte) error {
		copied := append([]byte(nil), frame...)
		go func() {
			_ = dev.EnqueueRxPacketOwned(copied)
		}()
		return nil
	})
	return dev, stack
}

func collectOpenBSDManagedEvents(t *testing.T, session *ManagedSession, ctx context.Context, req client.ExecRequest, inputs <-chan client.ExecInput) []client.ExecEvent {
	t.Helper()
	var events []client.ExecEvent
	if err := session.ExecStream(ctx, req, inputs, func(event client.ExecEvent) error {
		events = append(events, event)
		return nil
	}); err != nil {
		t.Fatalf("exec stream %s: %v", req.Kind, err)
	}
	return events
}

func lastExitCode(events []client.ExecEvent) int {
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Kind == "exit" {
			return events[i].ExitCode
		}
	}
	return -1
}

func stdoutBytes(events []client.ExecEvent) []byte {
	var out []byte
	for _, event := range events {
		if event.Kind == "stdout" {
			out = append(out, event.Data...)
		}
	}
	return out
}

func tarSingleFile(t *testing.T, name string, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{
		Name: name,
		Mode: 0o644,
		Size: int64(len(data)),
	}); err != nil {
		t.Fatalf("write tar header: %v", err)
	}
	if _, err := tw.Write(data); err != nil {
		t.Fatalf("write tar payload: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	return buf.Bytes()
}

func untarSingleFile(t *testing.T, data []byte, wantName string) []byte {
	t.Helper()
	tr := tar.NewReader(bytes.NewReader(data))
	hdr, err := tr.Next()
	if err != nil {
		t.Fatalf("read tar header: %v", err)
	}
	if hdr.Name != wantName {
		t.Fatalf("tar name = %q, want %q", hdr.Name, wantName)
	}
	contents, err := io.ReadAll(tr)
	if err != nil {
		t.Fatalf("read tar payload: %v", err)
	}
	return contents
}

func buildOpenBSDFsckRoot(t *testing.T) imagefs.Directory {
	t.Helper()
	rootDir := t.TempDir()
	for _, dir := range []string{"dev", "sbin"} {
		if err := os.MkdirAll(filepath.Join(rootDir, filepath.FromSlash(dir)), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	initBin := buildOpenBSDGoInit(t, openBSDFsckInitSource)
	if err := os.WriteFile(filepath.Join(rootDir, "sbin", "init"), initBin, 0o755); err != nil {
		t.Fatalf("write OpenBSD fsck init: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "dev", "console"), nil, 0o600); err != nil {
		t.Fatalf("write dev console placeholder: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "dev", "null"), nil, 0o666); err != nil {
		t.Fatalf("write dev null placeholder: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "dev", "rsd1a"), nil, 0o600); err != nil {
		t.Fatalf("write dev rsd1a placeholder: %v", err)
	}
	if err := extractOpenBSDRuntime(rootDir, openBSDBaseSetPath(t), "/sbin/fsck_ffs"); err != nil {
		t.Fatal(err)
	}
	meta := map[string]fsmeta.Entry{
		"/dev/console": {Mode: fsmeta.LinuxModeFromFileMode(fs.ModeDevice | fs.ModeCharDevice | 0o600), RDev: 0},
		"/dev/null":    {Mode: fsmeta.LinuxModeFromFileMode(fs.ModeDevice | fs.ModeCharDevice | 0o666), RDev: 514},
		"/dev/rsd1a":   {Mode: fsmeta.LinuxModeFromFileMode(fs.ModeDevice | fs.ModeCharDevice | 0o600), RDev: 13<<8 | 16},
	}
	return imagefs.NewHostFS(rootDir, meta)
}

func addOpenBSDRuntimeLibraryLinks(t *testing.T, overlay *imagefs.Overlay, root imagefs.Directory) {
	t.Helper()
	entry, err := imagefs.LookupPath(root, "/usr/lib")
	if err != nil {
		t.Fatalf("lookup /usr/lib: %v", err)
	}
	if entry.Dir == nil {
		t.Fatal("/usr/lib is not a directory")
	}
	entries, err := entry.Dir.ReadDir()
	if err != nil {
		t.Fatalf("read /usr/lib: %v", err)
	}
	var libcName, libpthreadName string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name, "libc.so.") {
			libcName = entry.Name
		}
		if strings.HasPrefix(entry.Name, "libpthread.so.") {
			libpthreadName = entry.Name
		}
	}
	if libcName == "" || libpthreadName == "" {
		t.Fatalf("runtime libraries missing: libc=%q libpthread=%q", libcName, libpthreadName)
	}
	if err := overlay.AddSymlink("/usr/lib/libc.so", libcName); err != nil {
		t.Fatalf("add libc.so symlink: %v", err)
	}
	if err := overlay.AddSymlink("/usr/lib/libpthread.so", libpthreadName); err != nil {
		t.Fatalf("add libpthread.so symlink: %v", err)
	}
}

func includeOpenBSDBaseBootFile(name string, hdr *tar.Header) bool {
	switch name {
	case "/usr/libexec/ld.so":
		return true
	}
	base := path.Base(name)
	return strings.HasPrefix(name, "/usr/lib/") && (strings.HasPrefix(base, "libc.so.") || strings.HasPrefix(base, "libpthread.so."))
}

func buildOpenBSDRoot(t *testing.T, initSource string, extraBaseFiles ...string) imagefs.Directory {
	t.Helper()
	rootDir := t.TempDir()
	for _, dir := range []string{"dev", "sbin", "usr/libexec", "usr/lib"} {
		if err := os.MkdirAll(filepath.Join(rootDir, filepath.FromSlash(dir)), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	initBin := buildOpenBSDGoInit(t, initSource)
	if err := os.WriteFile(filepath.Join(rootDir, "sbin", "init"), initBin, 0o755); err != nil {
		t.Fatalf("write OpenBSD init: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "dev", "console"), nil, 0o600); err != nil {
		t.Fatalf("write dev console placeholder: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "dev", "null"), nil, 0o666); err != nil {
		t.Fatalf("write dev null placeholder: %v", err)
	}
	if err := extractOpenBSDRuntime(rootDir, openBSDBaseSetPath(t), extraBaseFiles...); err != nil {
		t.Fatal(err)
	}
	meta := map[string]fsmeta.Entry{
		"/dev/console": {Mode: fsmeta.LinuxModeFromFileMode(fs.ModeDevice | fs.ModeCharDevice | 0o600), RDev: 0},
		"/dev/null":    {Mode: fsmeta.LinuxModeFromFileMode(fs.ModeDevice | fs.ModeCharDevice | 0o666), RDev: 514},
	}
	return imagefs.NewHostFS(rootDir, meta)
}

func buildOpenBSDGoInit(t *testing.T, source ...string) []byte {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "init.go")
	initSource := openBSDGoInitSource
	if len(source) > 0 {
		initSource = source[0]
	}
	if err := os.WriteFile(src, []byte(initSource), 0o644); err != nil {
		t.Fatalf("write init source: %v", err)
	}
	out := filepath.Join(dir, "init")
	cmd := exec.Command("go", "build", "-o", out, src)
	cmd.Env = append(os.Environ(), "GOOS=openbsd", "GOARCH=amd64", "CGO_ENABLED=0")
	if data, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build OpenBSD init: %v\n%s", err, data)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read OpenBSD init: %v", err)
	}
	return data
}

func openBSDBaseSetPath(t *testing.T) string {
	t.Helper()
	candidates := []string{}
	if env := strings.TrimSpace(os.Getenv("CC_TEST_OPENBSD_BASE79")); env != "" {
		candidates = append(candidates, env)
	}
	candidates = append(candidates, filepath.Join("..", "..", "..", ".cache", "openbsd79", "base79.tgz"))
	for _, candidate := range candidates {
		if st, err := os.Stat(candidate); err == nil && st.Size() > 0 {
			return candidate
		}
	}
	t.Skip("OpenBSD base79.tgz not present; set CC_TEST_OPENBSD_BASE79 or place it in .cache/openbsd79/base79.tgz")
	return ""
}

func extractOpenBSDRuntime(rootDir, baseSet string, extraFiles ...string) error {
	f, err := os.Open(baseSet)
	if err != nil {
		return fmt.Errorf("open %s: %w", baseSet, err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("read %s gzip: %w", baseSet, err)
	}
	defer gz.Close()
	want := map[string]bool{
		"/usr/libexec/ld.so": false,
	}
	for _, file := range extraFiles {
		want[openBSDSetPath(file)] = false
	}
	var libcName, libpthreadName string
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read %s tar: %w", baseSet, err)
		}
		name := openBSDSetPath(hdr.Name)
		_, wantFile := want[name]
		if name == "/usr/libexec/ld.so" || strings.HasPrefix(name, "/usr/lib/libc.so.") || strings.HasPrefix(name, "/usr/lib/libpthread.so.") || wantFile {
			target := filepath.Join(rootDir, filepath.FromSlash(strings.TrimPrefix(name, "/")))
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			switch hdr.Typeflag {
			case tar.TypeReg, tar.TypeRegA:
				out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, fs.FileMode(hdr.Mode)&0o7777)
				if err != nil {
					return err
				}
				if _, err := io.Copy(out, tr); err != nil {
					_ = out.Close()
					return err
				}
				if err := out.Close(); err != nil {
					return err
				}
			case tar.TypeSymlink:
				_ = os.Remove(target)
				if err := os.Symlink(hdr.Linkname, target); err != nil {
					return err
				}
			default:
				continue
			}
			if name == "/usr/libexec/ld.so" {
				want["/usr/libexec/ld.so"] = true
			}
			if _, ok := want[name]; ok {
				want[name] = true
			}
			if strings.HasPrefix(name, "/usr/lib/libc.so.") {
				libcName = filepath.Base(name)
			}
			if strings.HasPrefix(name, "/usr/lib/libpthread.so.") {
				libpthreadName = filepath.Base(name)
			}
		}
	}
	for file, ok := range want {
		if !ok {
			return fmt.Errorf("%s missing from %s", file, baseSet)
		}
	}
	if libcName == "" || libpthreadName == "" {
		return fmt.Errorf("OpenBSD runtime libraries missing from %s", baseSet)
	}
	for link, target := range map[string]string{
		filepath.Join(rootDir, "usr", "lib", "libc.so"):       libcName,
		filepath.Join(rootDir, "usr", "lib", "libpthread.so"): libpthreadName,
	} {
		_ = os.Remove(link)
		if err := os.Symlink(target, link); err != nil {
			return err
		}
	}
	return nil
}

func openBSDSetPath(name string) string {
	return path.Clean("/" + strings.TrimPrefix(strings.TrimPrefix(name, "."), "/"))
}

const openBSDGoInitSource = `package main

import (
	"os"
	"time"
)

func main() {
	console, err := os.OpenFile("/dev/console", os.O_RDWR, 0)
	if err == nil {
		console.WriteString("OPENBSD_GO_INIT_OK\n")
	} else {
		os.Stdout.WriteString("OPENBSD_GO_INIT_OK\n")
	}
	for {
		time.Sleep(time.Hour)
	}
}
`

const openBSDNetInitSource = `package main

import (
	"net"
	"os"
	"time"
)

func writeConsole(s string) {
	console, err := os.OpenFile("/dev/console", os.O_RDWR, 0)
	if err == nil {
		defer console.Close()
		console.WriteString(s)
		return
	}
	os.Stdout.WriteString(s)
}

func main() {
	iface, err := net.InterfaceByName("vio0")
	if err != nil {
		writeConsole("OPENBSD_VIO0_LOOKUP_FAILED: " + err.Error() + "\n")
	} else {
		writeConsole("OPENBSD_VIO0_FOUND " + iface.HardwareAddr.String() + "\n")
	}
	for {
		time.Sleep(time.Hour)
	}
}
`

const openBSDNetControlInitScript = `#!/bin/sh
exec >/dev/console 2>&1
/sbin/ifconfig vio0 inet 10.42.0.2 netmask 255.255.255.0 up || {
	echo OPENBSD_NET_IFCONFIG_FAILED
	while :; do /bin/sleep 3600; done
}
/sbin/route add default 10.42.0.1 || true
/sbin/ccnetctl
while :; do /bin/sleep 3600; done
`

const openBSDNetControlAgentSource = `package main

import (
	"bufio"
	"net"
	"os"
	"time"
)

func writeConsole(s string) {
	console, err := os.OpenFile("/dev/console", os.O_RDWR, 0)
	if err == nil {
		defer console.Close()
		console.WriteString(s)
		return
	}
	os.Stdout.WriteString(s)
}

func main() {
	var lastErr error
	for i := 0; i < 40; i++ {
		conn, err := net.DialTimeout("tcp4", "10.42.0.1:12345", time.Second)
		if err != nil {
			lastErr = err
			time.Sleep(250 * time.Millisecond)
			continue
		}
		_, _ = conn.Write([]byte("OPENBSD_NET_CONTROL_HELLO\n"))
		line, err := bufio.NewReader(conn).ReadString('\n')
		_ = conn.Close()
		if err != nil {
			lastErr = err
			time.Sleep(250 * time.Millisecond)
			continue
		}
		if line == "OPENBSD_NET_CONTROL_ACK\n" {
			writeConsole("OPENBSD_NET_CONTROL_OK\n")
		} else {
			writeConsole("OPENBSD_NET_CONTROL_BAD_ACK: " + line)
		}
		return
	}
	if lastErr != nil {
		writeConsole("OPENBSD_NET_CONTROL_FAILED: " + lastErr.Error() + "\n")
	} else {
		writeConsole("OPENBSD_NET_CONTROL_FAILED\n")
	}
}
`

const openBSDManagedInitScript = `#!/bin/sh
exec >/dev/console 2>&1
/sbin/mount -uw / || {
	echo OPENBSD_MANAGED_REMOUNT_FAILED
	while :; do /bin/sleep 3600; done
}
/sbin/ifconfig vio0 inet 10.42.0.2 netmask 255.255.255.0 up || {
	echo OPENBSD_MANAGED_IFCONFIG_FAILED
	while :; do /bin/sleep 3600; done
}
/sbin/route add default 10.42.0.1 || true
exec /sbin/cc-openbsd-init
`

const openBSDFsckInitSource = `package main

import (
	"bytes"
	"os"
	"os/exec"
	"time"
)

func writeConsole(s string) {
	console, err := os.OpenFile("/dev/console", os.O_RDWR, 0)
	if err == nil {
		defer console.Close()
		console.WriteString(s)
		return
	}
	os.Stdout.WriteString(s)
}

func main() {
	cmd := exec.Command("/sbin/fsck_ffs", "-fn", "/dev/rsd1a")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	writeConsole(out.String())
	if err != nil {
		writeConsole("OPENBSD_FSCK_FFS_FAILED: " + err.Error() + "\n")
	} else {
		writeConsole("OPENBSD_FSCK_FFS_OK\n")
	}
	writeConsole("OPENBSD_FSCK_FFS_DONE\n")
	for {
		time.Sleep(time.Hour)
	}
}
`
