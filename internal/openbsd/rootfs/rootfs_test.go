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

	"j5.nz/cc/internal/imagefs"
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
	root, err := BuildManagedRoot(context.Background(), baseTGZ, []byte("#!/bin/sh\necho test init\n"))
	if err != nil {
		t.Fatal(err)
	}
	baseTar := strings.TrimSuffix(baseTGZ, filepath.Ext(baseTGZ)) + ".tar"
	if st, err := os.Stat(baseTar); err != nil || st.Size() == 0 {
		t.Fatalf("decompressed base tar was not cached: stat=%v err=%v", st, err)
	}
	for _, guestPath := range []string{"/bin/sh", "/sbin/init", "/sbin/cc-openbsd-init", "/usr/lib/libc.so", "/usr/lib/libpthread.so", "/dev/console"} {
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
	if !strings.Contains(string(data), "cc-openbsd-init") {
		t.Fatalf("unexpected /sbin/init overlay: %q", data)
	}
	agentEntry, err := imagefs.LookupPath(root, "/sbin/cc-openbsd-init")
	if err != nil {
		t.Fatal(err)
	}
	agentData, err := agentEntry.File.ReadAt(0, 64)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(agentData), "test init") {
		t.Fatalf("unexpected /sbin/cc-openbsd-init overlay: %q", agentData)
	}
	initScript := readRootFile(t, root, "/sbin/init")
	if !strings.Contains(initScript, "ifconfig vio0 inet 10.42.0.2 ") {
		t.Fatalf("default init script does not configure default IP: %q", initScript)
	}
	hosts := readRootFile(t, root, "/etc/hosts")
	if !strings.Contains(hosts, "10.42.0.2 cc-openbsd") {
		t.Fatalf("default hosts does not contain default IP: %q", hosts)
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
	root, closeRoot, err := buildManagedRoot(context.Background(), baseTGZ, []byte("#!/bin/sh\n"), "10.42.0.7")
	if err != nil {
		t.Fatal(err)
	}
	defer closeRoot()
	initScript := readRootFile(t, root, "/sbin/init")
	if !strings.Contains(initScript, "ifconfig vio0 inet 10.42.0.7 ") {
		t.Fatalf("init script does not configure leased IP: %q", initScript)
	}
	hosts := readRootFile(t, root, "/etc/hosts")
	if !strings.Contains(hosts, "10.42.0.7 cc-openbsd") {
		t.Fatalf("hosts does not contain leased IP: %q", hosts)
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
	tgzPath := filepath.Join(t.TempDir(), "base79.tgz")
	if err := os.WriteFile(tgzPath, gzBuf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return tgzPath
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
