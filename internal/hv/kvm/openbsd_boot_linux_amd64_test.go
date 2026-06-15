//go:build linux && amd64

package kvm

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"j5.nz/cc/internal/fsimage"
	"j5.nz/cc/internal/fsmeta"
	"j5.nz/cc/internal/imagefs"
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
	disk := newTestDisk(int(region.Size()))
	if _, err := io.Copy(io.NewOffsetWriter(disk, 0), io.NewSectionReader(region, 0, region.Size())); err != nil {
		t.Fatalf("seed FFS disk: %v", err)
	}
	block := virtio.NewBlock(0, 0x1000, 10, disk)
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

func tailString(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[len(s)-max:]
}

func buildOpenBSDGoInitRoot(t *testing.T) imagefs.Directory {
	t.Helper()
	rootDir := t.TempDir()
	for _, dir := range []string{"dev", "sbin", "usr/libexec", "usr/lib"} {
		if err := os.MkdirAll(filepath.Join(rootDir, filepath.FromSlash(dir)), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	initBin := buildOpenBSDGoInit(t)
	if err := os.WriteFile(filepath.Join(rootDir, "sbin", "init"), initBin, 0o755); err != nil {
		t.Fatalf("write OpenBSD init: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "dev", "console"), nil, 0o600); err != nil {
		t.Fatalf("write dev console placeholder: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "dev", "null"), nil, 0o666); err != nil {
		t.Fatalf("write dev null placeholder: %v", err)
	}
	if err := extractOpenBSDRuntime(rootDir, openBSDBaseSetPath(t)); err != nil {
		t.Fatal(err)
	}
	meta := map[string]fsmeta.Entry{
		"/dev/console": {Mode: fsmeta.LinuxModeFromFileMode(fs.ModeDevice | fs.ModeCharDevice | 0o600), RDev: 0},
		"/dev/null":    {Mode: fsmeta.LinuxModeFromFileMode(fs.ModeDevice | fs.ModeCharDevice | 0o666), RDev: 514},
	}
	return imagefs.NewHostFS(rootDir, meta)
}

func buildOpenBSDGoInit(t *testing.T) []byte {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "init.go")
	if err := os.WriteFile(src, []byte(openBSDGoInitSource), 0o644); err != nil {
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

func extractOpenBSDRuntime(rootDir, baseSet string) error {
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
		"./usr/libexec/ld.so": false,
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
		name := strings.TrimPrefix(hdr.Name, ".")
		if name == "/usr/libexec/ld.so" || strings.HasPrefix(name, "/usr/lib/libc.so.") || strings.HasPrefix(name, "/usr/lib/libpthread.so.") {
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
				want["./usr/libexec/ld.so"] = true
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
