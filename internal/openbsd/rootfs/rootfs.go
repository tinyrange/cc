package rootfs

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"j5.nz/cc/internal/fsimage"
	"j5.nz/cc/internal/imagefs"
	managedguest "j5.nz/cc/internal/managed/guest"
	"j5.nz/cc/internal/managed/machine"
	"j5.nz/cc/internal/managed/release"
	"j5.nz/cc/internal/managed/rootartifact"
	"j5.nz/cc/internal/managed/rootplan"
	openbsdguestinit "j5.nz/cc/internal/openbsd/guestinit"
)

const (
	BuiltInImageName = managedguest.OpenBSDImageName
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
	Network   machine.NetworkSpec
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

func (r *Runtime) Artifact() rootartifact.Artifact {
	if r == nil {
		return rootartifact.Artifact{}
	}
	return rootartifact.Artifact{
		Kernel:    append([]byte(nil), r.Kernel...),
		RootBlock: r.Root,
		RootFS:    r.RootFS,
		Cleanup:   r.Close,
		Metadata: map[string]string{
			"guest": "openbsd",
		},
	}
}

func IsBuiltInImage(name string) bool {
	return managedguest.OpenBSDProfile.Match(name)
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
	root, closeRoot, err := buildManagedRoot(ctx, basePath, initBin, openBSDNetworkSpec(cfg))
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
	root, _, err := buildManagedRoot(ctx, baseSetPath, initBin, machine.NetworkSpec{})
	return root, err
}

func buildManagedRoot(ctx context.Context, baseSetPath string, initBin []byte, network machine.NetworkSpec) (imagefs.Directory, func() error, error) {
	network = normalizeOpenBSDNetwork(network)
	root, closeRoot, err := buildBaseRoot(ctx, baseSetPath, []byte(fmt.Sprintf(managedInitScript, network.Interface, network.GuestIPv4, network.GatewayIPv4)))
	if err != nil {
		return nil, nil, err
	}
	overlay := imagefs.NewOverlay(root)
	if err := rootplan.AddFiles(overlay, []rootplan.File{
		{"/sbin/cc-openbsd-init", 0o755, initBin},
		{"/etc/fstab", 0o644, []byte("/dev/sd0a / ffs rw 1 1\n")},
		{"/etc/myname", 0o644, []byte(network.Hostname + "\n")},
		{"/etc/resolv.conf", 0o644, []byte("nameserver " + network.DNSIPv4 + "\n")},
		{"/etc/hosts", 0o644, []byte(fmt.Sprintf("127.0.0.1 localhost\n%s %s\n", network.GuestIPv4, network.Hostname))},
	}); err != nil {
		_ = closeRoot()
		return nil, nil, err
	}
	return overlay.Root(), closeRoot, nil
}

func openBSDNetworkSpec(cfg Config) machine.NetworkSpec {
	network := cfg.Network
	if strings.TrimSpace(network.GuestIPv4) == "" {
		network.GuestIPv4 = cfg.GuestIPv4
	}
	return normalizeOpenBSDNetwork(network)
}

func normalizeOpenBSDNetwork(network machine.NetworkSpec) machine.NetworkSpec {
	if strings.TrimSpace(network.GuestIPv4) == "" {
		network.GuestIPv4 = "10.42.0.2"
	}
	if strings.TrimSpace(network.GatewayIPv4) == "" {
		network.GatewayIPv4 = "10.42.0.1"
	}
	if strings.TrimSpace(network.DNSIPv4) == "" {
		network.DNSIPv4 = network.GatewayIPv4
	}
	if strings.TrimSpace(network.Hostname) == "" {
		network.Hostname = "cc-openbsd"
	}
	if strings.TrimSpace(network.Interface) == "" {
		network.Interface = "vio0"
	}
	return network
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
	if err := rootplan.AddDevices(overlay, []rootplan.Device{
		{"/dev/console", fs.ModeDevice | fs.ModeCharDevice | 0o600, 0},
		{"/dev/null", fs.ModeDevice | fs.ModeCharDevice | 0o666, 514},
		{"/dev/zero", fs.ModeDevice | fs.ModeCharDevice | 0o666, 515},
		{"/dev/random", fs.ModeDevice | fs.ModeCharDevice | 0o644, 565},
		{"/dev/urandom", fs.ModeDevice | fs.ModeCharDevice | 0o644, 566},
		{"/dev/sd0a", fs.ModeDevice | 0o640, 1024},
		{"/dev/sd0b", fs.ModeDevice | 0o640, 1025},
	}); err != nil {
		_ = tfs.Close()
		return nil, nil, err
	}
	if err := overlay.AddFile("/sbin/init", 0o755, init); err != nil {
		_ = tfs.Close()
		return nil, nil, fmt.Errorf("overlay /sbin/init: %w", err)
	}
	return overlay.Root(), tfs.Close, nil
}

func ensureDecompressedTar(ctx context.Context, source string) (string, error) {
	target, err := release.EnsureDecompressed(ctx, source, "", func(r io.Reader) (io.ReadCloser, error) {
		gz, err := gzip.NewReader(r)
		if err != nil {
			return nil, fmt.Errorf("read OpenBSD base gzip %s: %w", source, err)
		}
		return gz, nil
	})
	if err != nil {
		return "", fmt.Errorf("decompress OpenBSD base set %s: %w", source, err)
	}
	return target, nil
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
	path, err := release.EnsureArtifact(ctx, release.Artifact{
		CacheDir:        cfg.CacheDir,
		Family:          "openbsd",
		Version:         cfg.Version,
		Arch:            cfg.Arch,
		Mirror:          cfg.Mirror,
		Name:            name,
		URLPath:         cfg.Version + "/" + cfg.Arch + "/" + name,
		LocalCandidates: localFixturePaths(cfg, name),
	})
	if err != nil {
		return "", fmt.Errorf("ensure OpenBSD artifact %s: %w", name, err)
	}
	return path, nil
}

func localFixturePaths(cfg Config, name string) []string {
	return []string{
		filepath.Join("local", "openbsd"+versionNoDot(cfg.Version)+"-"+cfg.Arch, name),
		filepath.Join(".cache", "openbsd"+versionNoDot(cfg.Version), name),
	}
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
	return rootplan.AddSymlinks(overlay, []rootplan.Symlink{
		{Path: "/usr/lib/libc.so", Target: libcName},
		{Path: "/usr/lib/libpthread.so", Target: libpthreadName},
	})
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
/sbin/ifconfig %s inet %s netmask 255.255.255.0 up || {
	echo OPENBSD_MANAGED_IFCONFIG_FAILED
	while :; do /bin/sleep 3600; done
}
/sbin/route add default %s || true
exec /sbin/cc-openbsd-init
`
