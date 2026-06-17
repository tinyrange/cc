//go:build linux && amd64

package kvm

import (
	"context"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"j5.nz/cc/client"
	netbsdrootfs "j5.nz/cc/internal/netbsd/rootfs"
	"j5.nz/cc/internal/virtio"
)

func TestNetBSDManagedSessionExec(t *testing.T) {
	if os.Getenv("CC_TEST_NETBSD_ROOTFS") == "" {
		t.Skip("set CC_TEST_NETBSD_ROOTFS=1 to build and boot the full NetBSD rootfs")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	rt, err := netbsdrootfs.BuildManagedRuntime(ctx, netbsdrootfs.Config{})
	if err != nil {
		t.Fatalf("build NetBSD runtime: %v", err)
	}
	defer rt.Close()
	start := time.Now()
	session, err := StartNetBSDManagedSession(ctx, NetBSDManagedConfig{
		Kernel:   rt.Kernel,
		Root:     rt.Root,
		MemoryMB: 1024,
	}, nil)
	if err != nil {
		t.Fatalf("start NetBSD managed session: %v", err)
	}
	defer session.Close()
	elapsed := time.Since(start)
	t.Logf("NetBSD managed session ready in %s", elapsed.Round(time.Millisecond))
	if elapsed > 2*time.Second {
		t.Fatalf("NetBSD managed session startup took %s, want under 2s", elapsed.Round(time.Millisecond))
	}
	resp, err := session.Exec(ctx, client.ExecRequest{
		Command: []string{"/bin/sh", "-c", "printf 'netbsd-managed:'; printf %s \"$(uname -s)\"; printf ':copy:'; cat"},
		Stdin:   []byte("stdin-ok"),
		WorkDir: "/tmp",
	})
	if err != nil {
		t.Fatalf("NetBSD managed exec: %v", err)
	}
	if resp.ExitCode != 0 || strings.TrimSpace(resp.Output) != "netbsd-managed:NetBSD:copy:stdin-ok" {
		t.Fatalf("NetBSD exec response = code %d output %q", resp.ExitCode, resp.Output)
	}
}

func TestNetBSDKernelSerial(t *testing.T) {
	if os.Getenv("CC_TEST_NETBSD_ROOTFS") == "" {
		t.Skip("set CC_TEST_NETBSD_ROOTFS=1 to build and boot the NetBSD kernel")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rt, err := netbsdrootfs.BuildManagedRuntime(ctx, netbsdrootfs.Config{})
	if err != nil {
		t.Fatalf("build NetBSD runtime: %v", err)
	}
	defer rt.Close()
	serial, err := BootNetBSDKernelToSerial(ctx, rt.Kernel, 1024)
	t.Logf("NetBSD serial:\n%s", serial)
	if err != nil && !strings.Contains(serial, "NetBSD 10.1") {
		t.Fatalf("boot NetBSD to serial: %v", err)
	}
	if !strings.Contains(serial, "NetBSD") {
		t.Fatalf("serial does not identify NetBSD:\n%s", serial)
	}
}

func TestNetBSDKernelRootSerial(t *testing.T) {
	if os.Getenv("CC_TEST_NETBSD_ROOTFS") == "" {
		t.Skip("set CC_TEST_NETBSD_ROOTFS=1 to build and boot the NetBSD rootfs")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	rt, err := netbsdrootfs.BuildManagedRuntime(ctx, netbsdrootfs.Config{})
	if err != nil {
		t.Fatalf("build NetBSD runtime: %v", err)
	}
	defer rt.Close()
	netdev, stack := newNetBSDManagedNet(net.IPv4(10, 42, 0, 2), net.HardwareAddr{0x02, 0x42, 0x0a, 0x2a, 0x00, 0x02})
	defer stack.Close()
	block := virtio.NewBlock(0, 0x1000, 10, rt.Root)
	serial, err := BootNetBSDKernelWithPCIBlockNetToSerial(ctx, rt.Kernel, 1024, block, netdev)
	t.Logf("NetBSD root serial:\n%s", serial)
	if err != nil && ctx.Err() == nil {
		t.Fatalf("boot NetBSD with root devices to serial: %v", err)
	}
	if !strings.Contains(serial, "ld0") {
		t.Fatalf("serial does not show virtio block attachment:\n%s", serial)
	}
	if !strings.Contains(serial, "root on ld0") {
		t.Fatalf("serial does not show ld0 root selection:\n%s", serial)
	}
}
