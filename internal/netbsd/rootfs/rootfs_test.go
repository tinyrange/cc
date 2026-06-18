package rootfs

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
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

func TestIsBuiltInImage(t *testing.T) {
	for _, image := range []string{"@netbsd", "netbsd", "  @NetBSD  "} {
		if !IsBuiltInImage(image) {
			t.Fatalf("IsBuiltInImage(%q) = false", image)
		}
	}
	for _, image := range []string{"", "alpine", "@openbsd", "netbsd:10.1"} {
		if IsBuiltInImage(image) {
			t.Fatalf("IsBuiltInImage(%q) = true", image)
		}
	}
}

func TestReadKernelGzip(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write([]byte("\x7fELFnetbsd-kernel")); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	kernelPath := filepath.Join(t.TempDir(), "netbsd-GENERIC.gz")
	if err := os.WriteFile(kernelPath, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	kernel, err := ReadKernel(kernelPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(kernel[:4]) != "\x7fELF" {
		t.Fatalf("kernel magic = %x", kernel[:4])
	}
}

func TestBuildManagedRootFromNetBSDBaseSet(t *testing.T) {
	baseTXZ := writeTXZFixture(t, []tarFixtureEntry{
		{name: "bin", mode: 0o755, dir: true},
		{name: "bin/sh", mode: 0o555, data: []byte("#!/bin/sh\n")},
		{name: "sbin", mode: 0o755, dir: true},
		{name: "sbin/init", mode: 0o555, data: []byte("base init\n")},
		{name: "etc", mode: 0o755, dir: true},
		{name: "dev", mode: 0o755, dir: true},
		{name: "root", mode: 0o700, dir: true},
	})
	root, err := BuildManagedRoot(context.Background(), baseTXZ, []byte("#!/bin/sh\necho test init\n"))
	if err != nil {
		t.Fatal(err)
	}
	baseTar := strings.TrimSuffix(baseTXZ, filepath.Ext(baseTXZ)) + ".tar"
	if st, err := os.Stat(baseTar); err != nil || st.Size() == 0 {
		t.Fatalf("decompressed base tar was not cached: stat=%v err=%v", st, err)
	}
	for _, guestPath := range []string{"/bin/sh", "/sbin/init", "/sbin/cc-netbsd-init", "/etc/fstab", "/dev/console", "/dev/ld0a"} {
		if _, err := imagefs.LookupPath(root, guestPath); err != nil {
			t.Fatalf("lookup %s: %v", guestPath, err)
		}
	}
	initScript := readRootFile(t, root, "/sbin/init")
	if !strings.Contains(initScript, "cc-netbsd-init") {
		t.Fatalf("unexpected /sbin/init overlay: %q", initScript)
	}
	if !strings.Contains(initScript, "ifconfig vioif0 inet 10.42.0.2 ") {
		t.Fatalf("default init script does not configure default IP: %q", initScript)
	}
	if !strings.Contains(initScript, "sysctl -w net.inet.ip.dad_count=0") {
		t.Fatalf("default init script does not disable IPv4 DAD before static addressing: %q", initScript)
	}
	fstab := readRootFile(t, root, "/etc/fstab")
	if !strings.Contains(fstab, "/dev/ld0a / ffs rw") {
		t.Fatalf("fstab does not mount ld0a: %q", fstab)
	}
}

func TestBuildManagedRootFromNetBSDBaseSetUsesNetworkSpec(t *testing.T) {
	baseTXZ := writeTXZFixture(t, []tarFixtureEntry{
		{name: "sbin", mode: 0o755, dir: true},
		{name: "sbin/init", mode: 0o555, data: []byte("base init\n")},
		{name: "etc", mode: 0o755, dir: true},
		{name: "dev", mode: 0o755, dir: true},
		{name: "root", mode: 0o700, dir: true},
	})
	root, closeRoot, err := buildManagedRoot(context.Background(), baseTXZ, []byte("#!/bin/sh\n"), defaultArch, machine.NetworkSpec{
		GuestIPv4:   "10.42.0.8",
		GatewayIPv4: "10.42.0.9",
		DNSIPv4:     "10.42.0.10",
		Hostname:    "test-netbsd",
		Interface:   "vioif1",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer closeRoot()
	initScript := readRootFile(t, root, "/sbin/init")
	if !strings.Contains(initScript, "ifconfig vioif1 inet 10.42.0.8 ") {
		t.Fatalf("init script does not configure leased IP: %q", initScript)
	}
	if !strings.Contains(initScript, "sysctl -w net.inet.ip.dad_count=0") {
		t.Fatalf("init script does not disable IPv4 DAD before static addressing: %q", initScript)
	}
	if !strings.Contains(initScript, "route add default 10.42.0.9") {
		t.Fatalf("init script does not configure gateway: %q", initScript)
	}
	ifconfig := readRootFile(t, root, "/etc/ifconfig.vioif1")
	if !strings.Contains(ifconfig, "inet 10.42.0.8 netmask 255.255.255.0") {
		t.Fatalf("ifconfig file does not contain leased IP: %q", ifconfig)
	}
	hosts := readRootFile(t, root, "/etc/hosts")
	if !strings.Contains(hosts, "10.42.0.8 test-netbsd") {
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
	protocols := readRootFile(t, root, "/etc/protocols")
	if !strings.Contains(protocols, "tcp") || !strings.Contains(protocols, "udp") {
		t.Fatalf("protocols does not contain TCP/UDP entries: %q", protocols)
	}
	netconfig := readRootFile(t, root, "/etc/netconfig")
	if !strings.Contains(netconfig, "tcp") || !strings.Contains(netconfig, "tpi_cots_ord") {
		t.Fatalf("netconfig does not contain TCP RPC transport: %q", netconfig)
	}
}

func TestBuildManagedRuntimeFromNetBSDReleaseSets(t *testing.T) {
	if _, ok := os.LookupEnv("CC_TEST_NETBSD_ROOTFS"); !ok {
		t.Skip("set CC_TEST_NETBSD_ROOTFS=1 to build the full NetBSD rootfs")
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
	out := filepath.Join(t.TempDir(), "fixture.tar.xz")
	if err := os.WriteFile(out, xzBuf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return out
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
