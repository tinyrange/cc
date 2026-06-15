package rootfs

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/ulikunitz/xz"
	"j5.nz/cc/internal/fsimage"
	ffsimage "j5.nz/cc/internal/fsimage/ffs"
	"j5.nz/cc/internal/imagefs"
)

const (
	BuiltInImageName = "@freebsd"
	defaultVersion   = "15.1"
	defaultArch      = "amd64"
	defaultMirror    = "https://mirror.aarnet.edu.au/pub/FreeBSD/releases"
)

type Config struct {
	CacheDir string
	Version  string
	Arch     string
	Mirror   string
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
	case BuiltInImageName, "freebsd":
		return true
	default:
		return false
	}
}

func BuildManagedRuntime(ctx context.Context, cfg Config) (*Runtime, error) {
	cfg = normalizeConfig(cfg)
	kernelTXZ, err := ensureArtifact(ctx, cfg, "kernel.txz")
	if err != nil {
		return nil, err
	}
	baseTXZ, err := ensureArtifact(ctx, cfg, "base.txz")
	if err != nil {
		return nil, err
	}
	kernel, err := ExtractKernel(kernelTXZ)
	if err != nil {
		return nil, err
	}
	root, closeRoot, err := buildManagedRoot(ctx, baseTXZ, []byte(managedInitScript))
	if err != nil {
		return nil, err
	}
	region, err := fsimage.Build(ctx, root, fsimage.Options{
		Type:              fsimage.TypeFFS,
		FFSLayout:         ffsimage.LayoutRaw,
		DeterministicTime: time.Unix(1700000000, 0),
		ExtraBytes:        128 << 20,
	})
	if err != nil {
		_ = closeRoot()
		return nil, fmt.Errorf("build FreeBSD FFS root: %w", err)
	}
	return &Runtime{Kernel: kernel, Root: region, RootFS: root, close: closeRoot}, nil
}

func BuildManagedRoot(ctx context.Context, baseSetPath string, init []byte) (imagefs.Directory, error) {
	root, _, err := buildManagedRoot(ctx, baseSetPath, init)
	return root, err
}

func buildManagedRoot(ctx context.Context, baseSetPath string, init []byte) (imagefs.Directory, func() error, error) {
	root, closeRoot, err := buildBaseRoot(ctx, baseSetPath)
	if err != nil {
		return nil, nil, err
	}
	overlay := imagefs.NewOverlay(root)
	for _, file := range []struct {
		path string
		mode fs.FileMode
		data []byte
	}{
		{"/sbin/init", 0o755, init},
		{"/etc/fstab", 0o644, []byte("/dev/vtbd0 / ufs rw 1 1\n")},
		{"/etc/rc.conf", 0o644, []byte("hostname=\"cc-freebsd\"\nifconfig_vtnet0=\"inet 10.42.0.2 netmask 255.255.255.0\"\ndefaultrouter=\"10.42.0.1\"\n")},
		{"/etc/resolv.conf", 0o644, []byte("nameserver 10.42.0.1\n")},
		{"/etc/hosts", 0o644, []byte("127.0.0.1 localhost\n10.42.0.2 cc-freebsd\n")},
	} {
		if err := overlay.AddFile(file.path, file.mode, file.data); err != nil {
			_ = closeRoot()
			return nil, nil, fmt.Errorf("overlay %s: %w", file.path, err)
		}
	}
	for _, dev := range []struct {
		path string
		mode fs.FileMode
		rdev uint32
	}{
		{"/dev/console", fs.ModeDevice | fs.ModeCharDevice | 0o600, 0},
		{"/dev/null", fs.ModeDevice | fs.ModeCharDevice | 0o666, 2},
		{"/dev/zero", fs.ModeDevice | fs.ModeCharDevice | 0o666, 12},
	} {
		if err := overlay.AddDevice(dev.path, dev.mode, dev.rdev); err != nil {
			_ = closeRoot()
			return nil, nil, fmt.Errorf("add %s: %w", dev.path, err)
		}
	}
	return overlay.Root(), closeRoot, nil
}

func BuildBaseRoot(ctx context.Context, baseSetPath string) (imagefs.Directory, error) {
	root, _, err := buildBaseRoot(ctx, baseSetPath)
	return root, err
}

func buildBaseRoot(ctx context.Context, baseSetPath string) (imagefs.Directory, func() error, error) {
	f, err := os.Open(baseSetPath)
	if err != nil {
		return nil, nil, fmt.Errorf("open FreeBSD base set %s: %w", baseSetPath, err)
	}
	defer f.Close()
	xzr, err := xz.NewReader(f)
	if err != nil {
		return nil, nil, fmt.Errorf("read FreeBSD base xz %s: %w", baseSetPath, err)
	}
	tfs, err := imagefs.NewTarFS(ctx, xzr)
	if err != nil {
		return nil, nil, fmt.Errorf("read FreeBSD base set %s: %w", baseSetPath, err)
	}
	return tfs.Root(), tfs.Close, nil
}

func ExtractKernel(kernelSetPath string) ([]byte, error) {
	f, err := os.Open(kernelSetPath)
	if err != nil {
		return nil, fmt.Errorf("open FreeBSD kernel set %s: %w", kernelSetPath, err)
	}
	defer f.Close()
	xzr, err := xz.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("read FreeBSD kernel xz %s: %w", kernelSetPath, err)
	}
	tr := tar.NewReader(xzr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("FreeBSD kernel set %s does not contain boot/kernel/kernel", kernelSetPath)
		}
		if err != nil {
			return nil, fmt.Errorf("read FreeBSD kernel set: %w", err)
		}
		if cleanTarPath(hdr.Name) != "/boot/kernel/kernel" {
			continue
		}
		if hdr.Typeflag != tar.TypeReg && hdr.Typeflag != tar.TypeRegA {
			return nil, fmt.Errorf("FreeBSD kernel entry has tar type %q", hdr.Typeflag)
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			return nil, fmt.Errorf("read FreeBSD kernel payload: %w", err)
		}
		return data, nil
	}
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
		cfg.CacheDir = filepath.Join(os.TempDir(), "cc-freebsd")
	}
	cfg.Mirror = strings.TrimRight(cfg.Mirror, "/")
	return cfg
}

func ensureArtifact(ctx context.Context, cfg Config, name string) (string, error) {
	if local := localFixturePath(cfg, name); local != "" {
		return local, nil
	}
	dir := filepath.Join(cfg.CacheDir, "freebsd", cfg.Version, cfg.Arch)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create FreeBSD cache dir: %w", err)
	}
	target := filepath.Join(dir, name)
	if st, err := os.Stat(target); err == nil && st.Size() > 0 {
		return target, nil
	}
	url := cfg.Mirror + "/" + cfg.Arch + "/" + cfg.Version + "-RELEASE/" + name
	if err := downloadFile(ctx, url, target); err != nil {
		return "", err
	}
	return target, nil
}

func localFixturePath(cfg Config, name string) string {
	for _, candidate := range []string{
		filepath.Join("local", "freebsd"+versionNoDot(cfg.Version)+"-"+cfg.Arch, name),
		filepath.Join(".cache", "freebsd"+versionNoDot(cfg.Version), name),
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
		return fmt.Errorf("create FreeBSD download request: %w", err)
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

func cleanTarPath(name string) string {
	clean := path.Clean("/" + strings.TrimPrefix(name, "/"))
	if clean == "." {
		return "/"
	}
	return clean
}

func versionNoDot(version string) string {
	return strings.ReplaceAll(version, ".", "")
}

const managedInitScript = `#!/bin/sh
/sbin/mount -t devfs devfs /dev || :
echo FREEBSD_FULL_BASE_INIT_OK >/dev/console 2>&1 || :
while :; do
	/bin/sleep 3600
done
`
