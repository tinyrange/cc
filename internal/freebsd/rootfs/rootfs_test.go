package rootfs

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ulikunitz/xz"
	"j5.nz/cc/internal/imagefs"
	"j5.nz/cc/internal/managed/machine"
)

func TestExtractKernelFromFreeBSDReleaseSet(t *testing.T) {
	kernelTXZ := writeTXZFixture(t, []tarFixtureEntry{
		{name: "boot/kernel/kernel", mode: 0o555, data: []byte("\x7fELFfreebsd-kernel")},
	})
	kernel, err := ExtractKernel(kernelTXZ)
	if err != nil {
		t.Fatal(err)
	}
	if len(kernel) < 4 || string(kernel[:4]) != "\x7fELF" {
		t.Fatalf("kernel does not start with ELF magic: %x", kernel[:min(len(kernel), 4)])
	}
	kernelTar := strings.TrimSuffix(kernelTXZ, filepath.Ext(kernelTXZ)) + ".tar"
	if st, err := os.Stat(kernelTar); err != nil || st.Size() == 0 {
		t.Fatalf("decompressed kernel tar was not cached: stat=%v err=%v", st, err)
	}
}

func TestBuildManagedRootFromFreeBSDBaseSet(t *testing.T) {
	baseTXZ := writeTXZFixture(t, []tarFixtureEntry{
		{name: "bin", mode: 0o755, dir: true},
		{name: "bin/sh", mode: 0o555, data: []byte("#!/bin/sh\n")},
		{name: "sbin", mode: 0o755, dir: true},
		{name: "sbin/init", mode: 0o555, data: []byte("base init\n")},
		{name: "etc", mode: 0o755, dir: true},
		{name: "dev", mode: 0o755, dir: true},
	})
	root, closeRoot, err := buildManagedRoot(context.Background(), baseTXZ, []byte("#!/bin/sh\necho test init\n"), machine.NetworkSpec{})
	if err != nil {
		t.Fatal(err)
	}
	defer closeRoot()
	baseTar := strings.TrimSuffix(baseTXZ, filepath.Ext(baseTXZ)) + ".tar"
	if st, err := os.Stat(baseTar); err != nil || st.Size() == 0 {
		t.Fatalf("decompressed base tar was not cached: stat=%v err=%v", st, err)
	}
	for _, guestPath := range []string{"/bin/sh", "/sbin/init", "/sbin/cc-freebsd-init", "/etc/fstab", "/dev/console"} {
		if _, err := imagefs.LookupPath(root, guestPath); err != nil {
			t.Fatalf("lookup %s: %v", guestPath, err)
		}
	}
	initEntry, err := imagefs.LookupPath(root, "/sbin/init")
	if err != nil {
		t.Fatal(err)
	}
	initSize, _ := initEntry.File.Stat()
	data, err := initEntry.File.ReadAt(0, uint32(initSize))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "cc-freebsd-init") {
		t.Fatalf("unexpected /sbin/init overlay: %q", data)
	}
	agentEntry, err := imagefs.LookupPath(root, "/sbin/cc-freebsd-init")
	if err != nil {
		t.Fatal(err)
	}
	agentData, err := agentEntry.File.ReadAt(0, 64)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(agentData), "test init") {
		t.Fatalf("unexpected /sbin/cc-freebsd-init overlay: %q", agentData)
	}
	initScript := readRootFile(t, root, "/sbin/init")
	if !strings.Contains(initScript, "ifconfig vtnet0 inet 10.42.0.2 ") {
		t.Fatalf("default init script does not configure default IP: %q", initScript)
	}
	rcConf := readRootFile(t, root, "/etc/rc.conf")
	if !strings.Contains(rcConf, `ifconfig_vtnet0="inet 10.42.0.2 netmask 255.255.255.0"`) {
		t.Fatalf("default rc.conf does not contain default IP: %q", rcConf)
	}
}

func TestBuildManagedRootFromFreeBSDBaseSetUsesGuestIPv4(t *testing.T) {
	baseTXZ := writeTXZFixture(t, []tarFixtureEntry{
		{name: "sbin", mode: 0o755, dir: true},
		{name: "sbin/init", mode: 0o555, data: []byte("base init\n")},
		{name: "etc", mode: 0o755, dir: true},
		{name: "dev", mode: 0o755, dir: true},
	})
	root, closeRoot, err := buildManagedRoot(context.Background(), baseTXZ, []byte("#!/bin/sh\n"), machine.NetworkSpec{
		GuestIPv4:   "10.42.0.8",
		GatewayIPv4: "10.42.0.9",
		DNSIPv4:     "10.42.0.10",
		Hostname:    "test-freebsd",
		Interface:   "vtnet1",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer closeRoot()
	initScript := readRootFile(t, root, "/sbin/init")
	if !strings.Contains(initScript, "ifconfig vtnet1 inet 10.42.0.8 ") {
		t.Fatalf("init script does not configure leased IP: %q", initScript)
	}
	if !strings.Contains(initScript, "route add default 10.42.0.9") {
		t.Fatalf("init script does not configure gateway: %q", initScript)
	}
	rcConf := readRootFile(t, root, "/etc/rc.conf")
	if !strings.Contains(rcConf, `ifconfig_vtnet1="inet 10.42.0.8 netmask 255.255.255.0"`) {
		t.Fatalf("rc.conf does not contain leased IP: %q", rcConf)
	}
	if !strings.Contains(rcConf, `hostname="test-freebsd"`) || !strings.Contains(rcConf, `defaultrouter="10.42.0.9"`) {
		t.Fatalf("rc.conf does not contain structured network identity: %q", rcConf)
	}
	hosts := readRootFile(t, root, "/etc/hosts")
	if !strings.Contains(hosts, "10.42.0.8 test-freebsd") {
		t.Fatalf("hosts does not contain leased IP: %q", hosts)
	}
	resolv := readRootFile(t, root, "/etc/resolv.conf")
	if !strings.Contains(resolv, "nameserver 10.42.0.10") {
		t.Fatalf("resolv.conf does not contain DNS IP: %q", resolv)
	}
	services := readRootFile(t, root, "/etc/services")
	if !strings.Contains(services, "nfs") || !strings.Contains(services, "2049/tcp") || !strings.Contains(services, "sunrpc") {
		t.Fatalf("services does not contain NFS RPC entries: %q", services)
	}
}

func TestBuildManagedRuntimeFromFreeBSDReleaseSets(t *testing.T) {
	if _, ok := os.LookupEnv("CC_TEST_FREEBSD_ROOTFS"); !ok {
		t.Skip("set CC_TEST_FREEBSD_ROOTFS=1 to build the full FreeBSD rootfs")
	}
	rt, err := BuildManagedRuntime(context.Background(), Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	if len(rt.Kernel) < 4 || string(rt.Kernel[:4]) != "\x7fELF" {
		t.Fatalf("kernel does not start with ELF magic")
	}
	if rt.Root == nil || rt.Root.Size() == 0 {
		t.Fatal("root filesystem was not built")
	}
}

type tarFixtureEntry struct {
	name string
	mode int64
	dir  bool
	data []byte
}

func writeTXZFixture(t *testing.T, entries []tarFixtureEntry) string {
	t.Helper()
	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)
	for _, entry := range entries {
		typ := byte(tar.TypeReg)
		size := int64(len(entry.data))
		name := strings.TrimPrefix(entry.name, "/")
		if entry.dir {
			typ = tar.TypeDir
			size = 0
			name = strings.TrimSuffix(name, "/") + "/"
		}
		if err := tw.WriteHeader(&tar.Header{
			Name:     name,
			Mode:     entry.mode,
			Size:     size,
			Typeflag: typ,
			ModTime:  time.Unix(1700000000, 0),
		}); err != nil {
			t.Fatal(err)
		}
		if !entry.dir {
			if _, err := io.Copy(tw, bytes.NewReader(entry.data)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	var xzBuf bytes.Buffer
	xzw, err := xz.NewWriter(&xzBuf)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(xzw, bytes.NewReader(tarBuf.Bytes())); err != nil {
		t.Fatal(err)
	}
	if err := xzw.Close(); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "fixture.txz")
	if err := os.WriteFile(out, xzBuf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return out
}

func localFreeBSDFixture(t *testing.T, name string) string {
	t.Helper()
	for _, candidate := range []string{
		filepath.Join(".cache", "freebsd151", name),
		filepath.Join("..", "..", "..", ".cache", "freebsd151", name),
		filepath.Join("local", "freebsd151-amd64", name),
	} {
		if st, err := os.Stat(candidate); err == nil && st.Size() > 0 {
			return candidate
		}
	}
	t.Skipf("FreeBSD fixture %s not present", name)
	return ""
}

func readRootFile(t *testing.T, root imagefs.Directory, guestPath string) string {
	t.Helper()
	entry, err := imagefs.LookupPath(root, guestPath)
	if err != nil {
		t.Fatalf("lookup %s: %v", guestPath, err)
	}
	if entry.File == nil {
		t.Fatalf("%s is not a file", guestPath)
	}
	size, _ := entry.File.Stat()
	data, err := entry.File.ReadAt(0, uint32(size))
	if err != nil {
		t.Fatalf("read %s: %v", guestPath, err)
	}
	return string(data)
}
