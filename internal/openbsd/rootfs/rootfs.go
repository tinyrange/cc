package rootfs

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"j5.nz/cc/internal/fsimage"
	"j5.nz/cc/internal/imagefs"
	openbsdguestinit "j5.nz/cc/internal/openbsd/guestinit"
)

const (
	BuiltInImageName = "@openbsd"
	defaultVersion   = "7.9"
	defaultArch      = "amd64"
	defaultMirror    = "https://mirror.aarnet.edu.au/pub/OpenBSD"
)

type Config struct {
	CacheDir  string
	Version   string
	Arch      string
	Mirror    string
	GuestIPv4 string
}

type Runtime struct {
	Kernel []byte
	Root   fsimage.FilesystemRegion
	RootFS imagefs.Directory
	close  func() error
}

func (r *Runtime) Close() error {
	if r == nil || r.close == nil {
		return nil
	}
	return r.close()
}

func IsBuiltInImage(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case BuiltInImageName, "openbsd":
		return true
	default:
		return false
	}
}

func BuildManagedRuntime(ctx context.Context, cfg Config) (*Runtime, error) {
	cfg = normalizeConfig(cfg)
	kernelPath, err := ensureArtifact(ctx, cfg, "bsd")
	if err != nil {
		return nil, err
	}
	basePath, err := ensureArtifact(ctx, cfg, "base"+versionNoDot(cfg.Version)+".tgz")
	if err != nil {
		return nil, err
	}
	kernel, err := os.ReadFile(kernelPath)
	if err != nil {
		return nil, fmt.Errorf("read OpenBSD kernel %s: %w", kernelPath, err)
	}
	initBin, err := openbsdguestinit.Build(ctx, filepath.Join(cfg.CacheDir, "guestinit"))
	if err != nil {
		return nil, err
	}
	root, closeRoot, err := buildManagedRoot(ctx, basePath, initBin, cfg.GuestIPv4)
	if err != nil {
		return nil, err
	}
	region, err := fsimage.Build(ctx, root, fsimage.Options{
		Type:              fsimage.TypeFFS,
		DeterministicTime: time.Unix(1700000000, 0),
	})
	if err != nil {
		_ = closeRoot()
		return nil, fmt.Errorf("build OpenBSD FFS root: %w", err)
	}
	return &Runtime{Kernel: kernel, Root: region, RootFS: root, close: closeRoot}, nil
}

func BuildManagedRoot(ctx context.Context, baseSetPath string, initBin []byte) (imagefs.Directory, error) {
	root, _, err := buildManagedRoot(ctx, baseSetPath, initBin, "")
	return root, err
}

func buildManagedRoot(ctx context.Context, baseSetPath string, initBin []byte, guestIPv4 string) (imagefs.Directory, func() error, error) {
	guestIPv4 = normalizeGuestIPv4(guestIPv4)
	root, closeRoot, err := buildBaseRoot(ctx, baseSetPath, []byte(fmt.Sprintf(managedInitScript, guestIPv4)))
	if err != nil {
		return nil, nil, err
	}
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
		{"/etc/hosts", 0o644, []byte(fmt.Sprintf("127.0.0.1 localhost\n%s cc-openbsd\n", guestIPv4))},
	} {
		if err := overlay.AddFile(file.path, file.mode, file.data); err != nil {
			_ = closeRoot()
			return nil, nil, fmt.Errorf("overlay %s: %w", file.path, err)
		}
	}
	return overlay.Root(), closeRoot, nil
}

func normalizeGuestIPv4(ip string) string {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return "10.42.0.2"
	}
	return ip
}

func BuildBaseRoot(ctx context.Context, baseSetPath string, init []byte) (imagefs.Directory, error) {
	root, _, err := buildBaseRoot(ctx, baseSetPath, init)
	return root, err
}

func buildBaseRoot(ctx context.Context, baseSetPath string, init []byte) (imagefs.Directory, func() error, error) {
	baseTar, err := ensureDecompressedTar(ctx, baseSetPath)
	if err != nil {
		return nil, nil, err
	}
	tfs, err := imagefs.NewSeekableTarFS(ctx, baseTar)
	if err != nil {
		return nil, nil, fmt.Errorf("read OpenBSD base set %s: %w", baseTar, err)
	}
	overlay := imagefs.NewOverlay(tfs.Root())
	if err := addRuntimeLibraryLinks(overlay, tfs.Root()); err != nil {
		_ = tfs.Close()
		return nil, nil, err
	}
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
			_ = tfs.Close()
			return nil, nil, fmt.Errorf("add %s: %w", dev.path, err)
		}
	}
	if err := overlay.AddFile("/sbin/init", 0o755, init); err != nil {
		_ = tfs.Close()
		return nil, nil, fmt.Errorf("overlay /sbin/init: %w", err)
	}
	return overlay.Root(), tfs.Close, nil
}

func ensureDecompressedTar(ctx context.Context, source string) (string, error) {
	target := strings.TrimSuffix(source, filepath.Ext(source)) + ".tar"
	if st, err := os.Stat(target); err == nil && st.Size() > 0 {
		if src, srcErr := os.Stat(source); srcErr == nil && !st.ModTime().Before(src.ModTime()) {
			return target, nil
		}
	}
	f, err := os.Open(source)
	if err != nil {
		return "", fmt.Errorf("open OpenBSD base set %s: %w", source, err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", fmt.Errorf("read OpenBSD base gzip %s: %w", source, err)
	}
	defer gz.Close()
	tmp := target + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return "", fmt.Errorf("create decompressed OpenBSD base set %s: %w", tmp, err)
	}
	_, copyErr := io.Copy(out, contextReader{ctx: ctx, r: gz})
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("decompress OpenBSD base set %s: %w", source, copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("close decompressed OpenBSD base set %s: %w", tmp, closeErr)
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("install decompressed OpenBSD base set %s: %w", target, err)
	}
	return target, nil
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

func normalizeConfig(cfg Config) Config {
	if cfg.Version == "" {
		cfg.Version = defaultVersion
	}
	if cfg.Arch == "" {
		cfg.Arch = defaultArch
	}
	if cfg.Mirror == "" {
		cfg.Mirror = defaultMirror
	}
	if cfg.CacheDir == "" {
		cfg.CacheDir = filepath.Join(os.TempDir(), "cc-openbsd")
	}
	cfg.Mirror = strings.TrimRight(cfg.Mirror, "/")
	return cfg
}

func ensureArtifact(ctx context.Context, cfg Config, name string) (string, error) {
	if local := localFixturePath(cfg, name); local != "" {
		return local, nil
	}
	dir := filepath.Join(cfg.CacheDir, "openbsd", cfg.Version, cfg.Arch)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create OpenBSD cache dir: %w", err)
	}
	target := filepath.Join(dir, name)
	if st, err := os.Stat(target); err == nil && st.Size() > 0 {
		return target, nil
	}
	url := cfg.Mirror + "/" + cfg.Version + "/" + cfg.Arch + "/" + name
	if err := downloadFile(ctx, url, target); err != nil {
		return "", err
	}
	return target, nil
}

func localFixturePath(cfg Config, name string) string {
	for _, candidate := range []string{
		filepath.Join("local", "openbsd"+versionNoDot(cfg.Version)+"-"+cfg.Arch, name),
		filepath.Join(".cache", "openbsd"+versionNoDot(cfg.Version), name),
	} {
		if st, err := os.Stat(candidate); err == nil && st.Size() > 0 {
			return candidate
		}
	}
	return ""
}

func downloadFile(ctx context.Context, url, target string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create OpenBSD download request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: unexpected HTTP status %s", url, resp.Status)
	}
	tmp := target + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("create %s: %w", tmp, err)
	}
	_, copyErr := io.Copy(out, resp.Body)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("write %s: %w", tmp, copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("close %s: %w", tmp, closeErr)
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("install %s: %w", target, err)
	}
	return nil
}

func addRuntimeLibraryLinks(overlay *imagefs.Overlay, root imagefs.Directory) error {
	entry, err := imagefs.LookupPath(root, "/usr/lib")
	if err != nil {
		return fmt.Errorf("lookup /usr/lib: %w", err)
	}
	if entry.Dir == nil {
		return fmt.Errorf("/usr/lib is not a directory")
	}
	entries, err := entry.Dir.ReadDir()
	if err != nil {
		return fmt.Errorf("read /usr/lib: %w", err)
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
		return fmt.Errorf("OpenBSD runtime libraries missing: libc=%q libpthread=%q", libcName, libpthreadName)
	}
	if err := overlay.AddSymlink("/usr/lib/libc.so", libcName); err != nil {
		return fmt.Errorf("add libc.so symlink: %w", err)
	}
	if err := overlay.AddSymlink("/usr/lib/libpthread.so", libpthreadName); err != nil {
		return fmt.Errorf("add libpthread.so symlink: %w", err)
	}
	return nil
}

func versionNoDot(version string) string {
	return strings.ReplaceAll(version, ".", "")
}

const managedInitScript = `#!/bin/sh
exec >/dev/console 2>&1
/sbin/mount -uw / || {
	echo OPENBSD_MANAGED_REMOUNT_FAILED
	while :; do /bin/sleep 3600; done
}
/sbin/ifconfig vio0 inet %s netmask 255.255.255.0 up || {
	echo OPENBSD_MANAGED_IFCONFIG_FAILED
	while :; do /bin/sleep 3600; done
}
/sbin/route add default 10.42.0.1 || true
exec /sbin/cc-openbsd-init
`
