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
	"path"
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
