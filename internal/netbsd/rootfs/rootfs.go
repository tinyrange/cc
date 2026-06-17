package rootfs

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ulikunitz/xz"
	"j5.nz/cc/internal/fsimage"
	ffsimage "j5.nz/cc/internal/fsimage/ffs"
	"j5.nz/cc/internal/imagefs"
	managedguest "j5.nz/cc/internal/managed/guest"
	"j5.nz/cc/internal/managed/machine"
	"j5.nz/cc/internal/managed/release"
	"j5.nz/cc/internal/managed/rootartifact"
	"j5.nz/cc/internal/managed/rootplan"
	netbsdguestinit "j5.nz/cc/internal/netbsd/guestinit"
)

const (
	BuiltInImageName = managedguest.NetBSDImageName
	defaultVersion   = "10.1"
	defaultArch      = "amd64"
	defaultMirror    = "https://mirror.aarnet.edu.au/pub/netbsd"
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
			"guest": "netbsd",
		},
	}
}

func IsBuiltInImage(name string) bool {
	return managedguest.NetBSDProfile.Match(name)
}

func BuildManagedRuntime(ctx context.Context, cfg Config) (*Runtime, error) {
	cfg = normalizeConfig(cfg)
	kernelGZ, err := ensureArtifact(ctx, cfg, "netbsd-GENERIC.gz", "kernel")
	if err != nil {
		return nil, err
	}
	baseTXZ, err := ensureArtifact(ctx, cfg, "base.tar.xz", "sets")
	if err != nil {
		return nil, err
	}
	kernel, err := ReadKernel(kernelGZ)
	if err != nil {
		return nil, err
	}
	initBin, err := netbsdguestinit.Build(ctx, filepath.Join(cfg.CacheDir, "guestinit"))
	if err != nil {
		return nil, err
	}
	root, closeRoot, err := buildManagedRoot(ctx, baseTXZ, initBin, netBSDNetworkSpec(cfg))
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
		return nil, fmt.Errorf("build NetBSD FFS root: %w", err)
	}
	return &Runtime{Kernel: kernel, Root: region, RootFS: root, close: closeRoot}, nil
}

func BuildManagedRoot(ctx context.Context, baseSetPath string, initBin []byte) (imagefs.Directory, error) {
	root, _, err := buildManagedRoot(ctx, baseSetPath, initBin, machine.NetworkSpec{})
	return root, err
}

func buildManagedRoot(ctx context.Context, baseSetPath string, initBin []byte, network machine.NetworkSpec) (imagefs.Directory, func() error, error) {
	network = normalizeNetBSDNetwork(network)
	root, closeRoot, err := buildBaseRoot(ctx, baseSetPath)
	if err != nil {
		return nil, nil, err
	}
	overlay := imagefs.NewOverlay(root)
	if err := rootplan.AddFiles(overlay, []rootplan.File{
		{"/sbin/init", 0o755, []byte(fmt.Sprintf(managedInitScript, network.Interface, network.GuestIPv4, network.GatewayIPv4))},
		{"/sbin/cc-netbsd-init", 0o755, initBin},
		{"/etc/fstab", 0o644, []byte("/dev/ld0a / ffs rw 1 1\n")},
		{"/etc/rc.conf", 0o644, []byte(fmt.Sprintf("rc_configured=YES\nhostname=\"%s\"\ndefaultroute=\"%s\"\n", network.Hostname, network.GatewayIPv4))},
		{"/etc/ifconfig." + network.Interface, 0o644, []byte(fmt.Sprintf("inet %s netmask 255.255.255.0\n", network.GuestIPv4))},
		{"/etc/resolv.conf", 0o644, []byte("nameserver " + network.DNSIPv4 + "\n")},
		{"/etc/hosts", 0o644, []byte(fmt.Sprintf("127.0.0.1 localhost\n%s %s\n", network.GuestIPv4, network.Hostname))},
		{"/root/.profile", 0o644, []byte(`export PKG_PATH=${PKG_PATH:-https://cdn.NetBSD.org/pub/pkgsrc/packages/NetBSD/x86_64/10.1/All}
export PATH=/bin:/sbin:/usr/bin:/usr/sbin:/usr/pkg/bin:/usr/pkg/sbin
`)},
	}); err != nil {
		_ = closeRoot()
		return nil, nil, err
	}
	if err := rootplan.AddDevices(overlay, []rootplan.Device{
		{"/dev/console", fs.ModeDevice | fs.ModeCharDevice | 0o600, rdev(0, 0)},
		{"/dev/constty", fs.ModeDevice | fs.ModeCharDevice | 0o600, rdev(0, 1)},
		{"/dev/tty", fs.ModeDevice | fs.ModeCharDevice | 0o666, rdev(1, 0)},
		{"/dev/null", fs.ModeDevice | fs.ModeCharDevice | 0o666, rdev(2, 2)},
		{"/dev/zero", fs.ModeDevice | fs.ModeCharDevice | 0o666, rdev(2, 12)},
		{"/dev/random", fs.ModeDevice | fs.ModeCharDevice | 0o444, rdev(46, 0)},
		{"/dev/urandom", fs.ModeDevice | fs.ModeCharDevice | 0o644, rdev(46, 1)},
		{"/dev/ld0", fs.ModeDevice | 0o640, rdev(19, 3)},
		{"/dev/ld0a", fs.ModeDevice | 0o640, rdev(19, 0)},
		{"/dev/ld0d", fs.ModeDevice | 0o640, rdev(19, 3)},
		{"/dev/rld0", fs.ModeDevice | fs.ModeCharDevice | 0o640, rdev(69, 3)},
		{"/dev/rld0a", fs.ModeDevice | fs.ModeCharDevice | 0o640, rdev(69, 0)},
		{"/dev/rld0d", fs.ModeDevice | fs.ModeCharDevice | 0o640, rdev(69, 3)},
	}); err != nil {
		_ = closeRoot()
		return nil, nil, err
	}
	return overlay.Root(), closeRoot, nil
}

func rdev(major, minor uint32) uint32 {
	return major<<8 | minor
}

func netBSDNetworkSpec(cfg Config) machine.NetworkSpec {
	network := cfg.Network
	if strings.TrimSpace(network.GuestIPv4) == "" {
		network.GuestIPv4 = cfg.GuestIPv4
	}
	return normalizeNetBSDNetwork(network)
}

func normalizeNetBSDNetwork(network machine.NetworkSpec) machine.NetworkSpec {
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
		network.Hostname = "cc-netbsd"
	}
	if strings.TrimSpace(network.Interface) == "" {
		network.Interface = "vioif0"
	}
	return network
}

func BuildBaseRoot(ctx context.Context, baseSetPath string) (imagefs.Directory, error) {
	root, _, err := buildBaseRoot(ctx, baseSetPath)
	return root, err
}

func buildBaseRoot(ctx context.Context, baseSetPath string) (imagefs.Directory, func() error, error) {
	baseTar, err := ensureDecompressedTar(ctx, baseSetPath)
	if err != nil {
		return nil, nil, err
	}
	tfs, err := imagefs.NewSeekableTarFS(ctx, baseTar)
	if err != nil {
		return nil, nil, fmt.Errorf("read NetBSD base set %s: %w", baseTar, err)
	}
	return tfs.Root(), tfs.Close, nil
}

func ensureDecompressedTar(ctx context.Context, source string) (string, error) {
	target, err := release.EnsureDecompressed(ctx, source, "", func(r io.Reader) (io.ReadCloser, error) {
		xzr, err := xz.NewReader(r)
		if err != nil {
			return nil, fmt.Errorf("read NetBSD release xz %s: %w", source, err)
		}
		return io.NopCloser(xzr), nil
	})
	if err != nil {
		return "", fmt.Errorf("decompress NetBSD release set %s: %w", source, err)
	}
	return target, nil
}

func ReadKernel(kernelPath string) ([]byte, error) {
	data, err := os.ReadFile(kernelPath)
	if err != nil {
		return nil, fmt.Errorf("read NetBSD kernel %s: %w", kernelPath, err)
	}
	if len(data) < 2 || data[0] != 0x1f || data[1] != 0x8b {
		return data, nil
	}
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("open NetBSD kernel gzip %s: %w", kernelPath, err)
	}
	defer gz.Close()
	out, err := io.ReadAll(gz)
	if err != nil {
		return nil, fmt.Errorf("decompress NetBSD kernel %s: %w", kernelPath, err)
	}
	return out, nil
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
		cfg.CacheDir = filepath.Join(os.TempDir(), "cc-netbsd")
	}
	cfg.Mirror = strings.TrimRight(cfg.Mirror, "/")
	return cfg
}

func ensureArtifact(ctx context.Context, cfg Config, name, subdir string) (string, error) {
	path, err := release.EnsureArtifact(ctx, release.Artifact{
		CacheDir:        cfg.CacheDir,
		Family:          "netbsd",
		Version:         cfg.Version,
		Arch:            cfg.Arch,
		Mirror:          cfg.Mirror,
		Name:            name,
		URLPath:         "NetBSD-" + cfg.Version + "/" + cfg.Arch + "/binary/" + subdir + "/" + name,
		LocalCandidates: localFixturePaths(cfg, name),
	})
	if err != nil {
		return "", fmt.Errorf("ensure NetBSD artifact %s: %w", name, err)
	}
	return path, nil
}

func localFixturePaths(cfg Config, name string) []string {
	return []string{
		filepath.Join("local", "netbsd"+versionNoDot(cfg.Version)+"-"+cfg.Arch, name),
		filepath.Join(".cache", "netbsd"+versionNoDot(cfg.Version), name),
	}
}

func versionNoDot(version string) string {
	return strings.ReplaceAll(version, ".", "")
}

const managedInitScript = `#!/bin/sh
exec >/dev/console 2>&1
/sbin/mount -u -o rw / || {
	echo NETBSD_MANAGED_REMOUNT_FAILED
	while :; do /bin/sleep 3600; done
}
/sbin/sysctl -w net.inet.ip.dad_count=0 >/dev/null 2>&1 || true
/sbin/ifconfig %s inet %s netmask 255.255.255.0 up || {
	echo NETBSD_MANAGED_IFCONFIG_FAILED
	while :; do /bin/sleep 3600; done
}
/sbin/route add default %s || true
exec /sbin/cc-netbsd-init
`
