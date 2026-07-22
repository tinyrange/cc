package rootfs

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"j5.nz/cc/internal/imagefs"
	"j5.nz/cc/internal/managed/machine"
)

func TestIsBuiltInImage(t *testing.T) {
	for _, image := range []string{"@openbsd", "openbsd", "  @OpenBSD  "} {
		if !IsBuiltInImage(image) {
			t.Fatalf("IsBuiltInImage(%q) = false", image)
		}
	}
	for _, image := range []string{"", "alpine", "@linux", "openbsd:7.9"} {
		if IsBuiltInImage(image) {
			t.Fatalf("IsBuiltInImage(%q) = true", image)
		}
	}
}

func TestBuildManagedRootFromOpenBSDBaseSetCachesSeekableTar(t *testing.T) {
	baseTGZ := writeTGZFixture(t, []tarFixtureEntry{
		{name: "bin", mode: 0o755, dir: true},
		{name: "bin/sh", mode: 0o555, data: []byte("#!/bin/sh\n")},
		{name: "sbin", mode: 0o755, dir: true},
		{name: "sbin/init", mode: 0o555, data: []byte("base init\n")},
		{name: "usr", mode: 0o755, dir: true},
		{name: "usr/lib", mode: 0o755, dir: true},
		{name: "usr/lib/libc.so.99.0", mode: 0o444, data: []byte("libc")},
		{name: "usr/lib/libpthread.so.99.0", mode: 0o444, data: []byte("libpthread")},
		{name: "etc", mode: 0o755, dir: true},
		{name: "dev", mode: 0o755, dir: true},
	})
	root, closeRoot, err := buildManagedRoot(context.Background(), baseTGZ, []byte("#!/bin/sh\necho test init\n"), machine.NetworkSpec{})
	if err != nil {
		t.Fatal(err)
	}
	defer closeRoot()
	baseTar := strings.TrimSuffix(baseTGZ, filepath.Ext(baseTGZ)) + ".tar"
	if st, err := os.Stat(baseTar); err != nil || st.Size() == 0 {
		t.Fatalf("decompressed base tar was not cached: stat=%v err=%v", st, err)
	}
	for _, guestPath := range []string{"/bin/sh", "/sbin/init", "/sbin/cc-openbsd-init", "/usr/lib/libc.so", "/usr/lib/libpthread.so", "/dev/console", "/dev/ptm", "/dev/ptyp0", "/dev/ttyp0", "/dev/ptypZ", "/dev/ttypZ"} {
		if _, err := imagefs.LookupPath(root, guestPath); err != nil {
			t.Fatalf("lookup %s: %v", guestPath, err)
		}
	}
	ptm, err := imagefs.LookupPath(root, "/dev/ptm")
	if err != nil {
		t.Fatal(err)
	}
	_, ptmMode := ptm.File.Stat()
	if ptmMode != fs.ModeDevice|fs.ModeCharDevice|0o666 || ptm.File.RDev() != 81<<8 {
		t.Fatalf("/dev/ptm mode=%v rdev=%d", ptmMode, ptm.File.RDev())
	}
	for guestPath, wantRDev := range map[string]uint32{
		"/dev/ttyp0": 5 << 8,
		"/dev/ptyp0": 6 << 8,
		"/dev/ttypZ": 5<<8 | 61,
		"/dev/ptypZ": 6<<8 | 61,
	} {
		entry, err := imagefs.LookupPath(root, guestPath)
		if err != nil {
			t.Fatal(err)
		}
		_, mode := entry.File.Stat()
		if mode != fs.ModeDevice|fs.ModeCharDevice|0o666 || entry.File.RDev() != wantRDev {
			t.Fatalf("%s mode=%v rdev=%d, want rdev=%d", guestPath, mode, entry.File.RDev(), wantRDev)
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
	if !textHasFields(string(data), "/sbin/ifconfig", "vio0", "inet", "10.42.0.2", "netmask", "255.255.255.0", "up", "||", "{") ||
		!textHasFields(string(data), "/usr/sbin/arp", "-s", "10.42.0.1", "02:42:0a:2a:00:01", ">/dev/null", "2>&1", "||", "true") ||
		!textHasFields(string(data), "/sbin/route", "add", "default", "10.42.0.1", "||", "true") {
		t.Fatalf("init script does not configure the leased network: %q", data)
	}
	agentEntry, err := imagefs.LookupPath(root, "/sbin/cc-openbsd-init")
	if err != nil {
		t.Fatal(err)
	}
	agentData, err := agentEntry.File.ReadAt(0, 64)
	if err != nil {
		t.Fatal(err)
	}
	if string(agentData) != "#!/bin/sh\necho test init\n" {
		t.Fatalf("unexpected /sbin/cc-openbsd-init overlay: %q", agentData)
	}
	hosts := readRootFile(t, root, "/etc/hosts")
	if !textHasLine(hosts, "10.42.0.2 cc-openbsd") {
		t.Fatalf("default hosts does not contain default IP: %q", hosts)
	}
	localtime, err := imagefs.LookupPath(root, "/etc/localtime")
	if err != nil {
		t.Fatal(err)
	}
	if localtime.Symlink == nil || localtime.Symlink.Target() != "/usr/share/zoneinfo/UTC" {
		t.Fatalf("OpenBSD localtime = %#v, want UTC symlink", localtime)
	}
}

func TestBuildManagedRootFromOpenBSDBaseSetUsesGuestIPv4(t *testing.T) {
	baseTGZ := writeTGZFixture(t, []tarFixtureEntry{
		{name: "sbin", mode: 0o755, dir: true},
		{name: "sbin/init", mode: 0o555, data: []byte("base init\n")},
		{name: "usr", mode: 0o755, dir: true},
		{name: "usr/lib", mode: 0o755, dir: true},
		{name: "usr/lib/libc.so.99.0", mode: 0o444, data: []byte("libc")},
		{name: "usr/lib/libpthread.so.99.0", mode: 0o444, data: []byte("libpthread")},
		{name: "etc", mode: 0o755, dir: true},
		{name: "dev", mode: 0o755, dir: true},
	})
	root, closeRoot, err := buildManagedRoot(context.Background(), baseTGZ, []byte("#!/bin/sh\n"), machine.NetworkSpec{
		GuestIPv4: "10.42.0.7",
		DNSIPv4:   "10.42.0.9",
		Hostname:  "test-openbsd",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer closeRoot()
	hosts := readRootFile(t, root, "/etc/hosts")
	if !textHasLine(hosts, "10.42.0.7 test-openbsd") {
		t.Fatalf("hosts does not contain leased IP: %q", hosts)
	}
	resolv := readRootFile(t, root, "/etc/resolv.conf")
	if !textHasLine(resolv, "nameserver 10.42.0.9") {
		t.Fatalf("resolv.conf does not contain DNS IP: %q", resolv)
	}
	services := readRootFile(t, root, "/etc/services")
	if !textHasFields(services, "nfs", "2049/tcp") || !textHasFields(services, "sunrpc", "111/tcp") {
		t.Fatalf("services does not contain NFS RPC entries: %q", services)
	}
	myname := readRootFile(t, root, "/etc/myname")
	if strings.TrimSpace(myname) != "test-openbsd" {
		t.Fatalf("myname = %q", myname)
	}
}

func TestBuildManagedRootFromOpenBSDBaseSetExtractsEtcSet(t *testing.T) {
	etcTGZ := buildTGZFixtureData(t, []tarFixtureEntry{
		{name: ".profile", mode: 0o644, data: []byte("root profile\n")},
		{name: "etc", mode: 0o755, dir: true},
		{name: "etc/ssl", mode: 0o755, dir: true},
		{name: "etc/ssl/cert.pem", mode: 0o644, data: []byte("test certificate bundle\n")},
		{name: "etc/resolv.conf", mode: 0o644, data: []byte("nameserver 192.0.2.1\n")},
	})
	baseTGZ := writeTGZFixture(t, []tarFixtureEntry{
		{name: "sbin", mode: 0o755, dir: true},
		{name: "sbin/init", mode: 0o555, data: []byte("base init\n")},
		{name: "usr", mode: 0o755, dir: true},
		{name: "usr/lib", mode: 0o755, dir: true},
		{name: "usr/lib/libc.so.99.0", mode: 0o444, data: []byte("libc")},
		{name: "usr/lib/libpthread.so.99.0", mode: 0o444, data: []byte("libpthread")},
		{name: "var", mode: 0o755, dir: true},
		{name: "var/sysmerge", mode: 0o755, dir: true},
		{name: "var/sysmerge/etc.tgz", mode: 0o644, data: etcTGZ},
		{name: "etc", mode: 0o755, dir: true},
		{name: "dev", mode: 0o755, dir: true},
	})
	root, closeRoot, err := buildManagedRoot(context.Background(), baseTGZ, []byte("#!/bin/sh\n"), machine.NetworkSpec{})
	if err != nil {
		t.Fatal(err)
	}
	defer closeRoot()
	if got := readRootFile(t, root, "/etc/ssl/cert.pem"); got != "test certificate bundle\n" {
		t.Fatalf("cert.pem = %q, want etc set certificate bundle", got)
	}
	if got := readRootFile(t, root, "/.profile"); got != "root profile\n" {
		t.Fatalf(".profile = %q, want root profile from etc set", got)
	}
	resolv := readRootFile(t, root, "/etc/resolv.conf")
	if strings.Contains(resolv, "192.0.2.1") || !strings.Contains(resolv, "nameserver 10.42.0.1") {
		t.Fatalf("resolv.conf = %q, want generated DNS override", resolv)
	}
}

func TestBuildManagedRootFromOpenBSDBaseSetUsesStructuredNetwork(t *testing.T) {
	baseTGZ := writeTGZFixture(t, []tarFixtureEntry{
		{name: "sbin", mode: 0o755, dir: true},
		{name: "sbin/init", mode: 0o555, data: []byte("base init\n")},
		{name: "usr", mode: 0o755, dir: true},
		{name: "usr/lib", mode: 0o755, dir: true},
		{name: "usr/lib/libc.so.99.0", mode: 0o444, data: []byte("libc")},
		{name: "usr/lib/libpthread.so.99.0", mode: 0o444, data: []byte("libpthread")},
		{name: "etc", mode: 0o755, dir: true},
		{name: "dev", mode: 0o755, dir: true},
	})
	root, closeRoot, err := buildManagedRoot(context.Background(), baseTGZ, []byte("#!/bin/sh\n"), machine.NetworkSpec{
		GuestIPv4:   "10.42.0.11",
		GatewayIPv4: "10.42.0.12",
		DNSIPv4:     "10.42.0.13",
		Hostname:    "structured-openbsd",
		Interface:   "vio1",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer closeRoot()
	resolv := readRootFile(t, root, "/etc/resolv.conf")
	if !textHasLine(resolv, "nameserver 10.42.0.13") {
		t.Fatalf("resolv.conf does not contain structured DNS IP: %q", resolv)
	}
}

type tarFixtureEntry struct {
	name string
	mode int64
	dir  bool
	data []byte
}

func writeTGZFixture(t *testing.T, entries []tarFixtureEntry) string {
	t.Helper()
	gzData := buildTGZFixtureData(t, entries)
	tgzPath := filepath.Join(t.TempDir(), "base79.tgz")
	if err := os.WriteFile(tgzPath, gzData, 0o644); err != nil {
		t.Fatal(err)
	}
	return tgzPath
}

func buildTGZFixtureData(t *testing.T, entries []tarFixtureEntry) []byte {
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
	var gzBuf bytes.Buffer
	gzw := gzip.NewWriter(&gzBuf)
	if _, err := io.Copy(gzw, bytes.NewReader(tarBuf.Bytes())); err != nil {
		t.Fatal(err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatal(err)
	}
	return gzBuf.Bytes()
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

func textHasLine(text, want string) bool {
	for _, line := range strings.Split(text, "\n") {
		if line == want {
			return true
		}
	}
	return false
}

func textHasFields(text string, want ...string) bool {
	for _, line := range strings.Split(text, "\n") {
		if reflect.DeepEqual(strings.Fields(line), want) {
			return true
		}
	}
	return false
}
