package rootfs

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/ulikunitz/xz"
	freebsdguestinit "j5.nz/cc/internal/freebsd/guestinit"
	"j5.nz/cc/internal/fsimage"
	ffsimage "j5.nz/cc/internal/fsimage/ffs"
	"j5.nz/cc/internal/imagefs"
	managedguest "j5.nz/cc/internal/managed/guest"
	"j5.nz/cc/internal/managed/machine"
	"j5.nz/cc/internal/managed/release"
	"j5.nz/cc/internal/managed/rootartifact"
	"j5.nz/cc/internal/managed/rootplan"
)

const (
	BuiltInImageName = managedguest.FreeBSDImageName
	defaultVersion   = "15.1"
	defaultArch      = "amd64"
	defaultMirror    = "https://mirror.aarnet.edu.au/pub/FreeBSD/releases"
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
			"guest": "freebsd",
		},
	}
}

func IsBuiltInImage(name string) bool {
	return managedguest.FreeBSDProfile.Match(name)
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
	initBin, err := freebsdguestinit.BuildForArch(ctx, filepath.Join(cfg.CacheDir, "guestinit"), cfg.Arch)
	if err != nil {
		return nil, err
	}
	root, closeRoot, err := buildManagedRoot(ctx, baseTXZ, initBin, freeBSDNetworkSpec(cfg))
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

func BuildManagedRuntimeFromOCI(ctx context.Context, cfg Config, kernel []byte, rootLayerPath string) (*Runtime, error) {
	cfg = normalizeConfig(cfg)
	baseTar, err := ensureGzipDecompressedTar(ctx, rootLayerPath)
	if err != nil {
		return nil, err
	}
	tfs, err := imagefs.NewSeekableTarFS(ctx, baseTar)
	if err != nil {
		return nil, fmt.Errorf("read FreeBSD OCI root layer %s: %w", baseTar, err)
	}
	initBin, err := freebsdguestinit.BuildForArch(ctx, filepath.Join(cfg.CacheDir, "guestinit"), cfg.Arch)
	if err != nil {
		_ = tfs.Close()
		return nil, err
	}
	root, closeRoot, err := buildManagedRootFromBase(tfs.Root(), tfs.Close, initBin, freeBSDNetworkSpec(cfg))
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
	return &Runtime{Kernel: append([]byte(nil), kernel...), Root: region, RootFS: root, close: closeRoot}, nil
}

func BuildManagedRoot(ctx context.Context, baseSetPath string, initBin []byte) (imagefs.Directory, error) {
	root, _, err := buildManagedRoot(ctx, baseSetPath, initBin, machine.NetworkSpec{})
	return root, err
}

func buildManagedRoot(ctx context.Context, baseSetPath string, initBin []byte, network machine.NetworkSpec) (imagefs.Directory, func() error, error) {
	network = normalizeFreeBSDNetwork(network)
	root, closeRoot, err := buildBaseRoot(ctx, baseSetPath)
	if err != nil {
		return nil, nil, err
	}
	return buildManagedRootFromBase(root, closeRoot, initBin, network)
}

func buildManagedRootFromBase(root imagefs.Directory, closeRoot func() error, initBin []byte, network machine.NetworkSpec) (imagefs.Directory, func() error, error) {
	network = normalizeFreeBSDNetwork(network)
	overlay := imagefs.NewOverlay(root)
	if err := rootplan.AddFiles(overlay, []rootplan.File{
		{"/sbin/init", 0o755, []byte(fmt.Sprintf(managedInitScript, network.Interface, network.GuestIPv4, network.GatewayIPv4))},
		{"/sbin/cc-freebsd-init", 0o755, initBin},
		{"/etc/fstab", 0o644, []byte("/dev/nda0 / ufs rw 1 1\n")},
		{"/etc/rc.conf", 0o644, []byte(fmt.Sprintf("hostname=\"%s\"\nifconfig_%s=\"inet %s netmask 255.255.255.0\"\ndefaultrouter=\"%s\"\n", network.Hostname, network.Interface, network.GuestIPv4, network.GatewayIPv4))},
		{"/etc/resolv.conf", 0o644, []byte("nameserver " + network.DNSIPv4 + "\n")},
		{"/etc/hosts", 0o644, []byte(fmt.Sprintf("127.0.0.1 localhost\n%s %s\n", network.GuestIPv4, network.Hostname))},
		{"/etc/services", 0o644, []byte(bsdNetworkServices)},
	}); err != nil {
		_ = closeRoot()
		return nil, nil, err
	}
	if err := rootplan.AddDevices(overlay, []rootplan.Device{
		{"/dev/console", fs.ModeDevice | fs.ModeCharDevice | 0o600, 0},
		{"/dev/null", fs.ModeDevice | fs.ModeCharDevice | 0o666, 2},
		{"/dev/zero", fs.ModeDevice | fs.ModeCharDevice | 0o666, 12},
	}); err != nil {
		_ = closeRoot()
		return nil, nil, err
	}
	return overlay.Root(), closeRoot, nil
}

const bsdNetworkServices = `sunrpc		111/tcp
sunrpc		111/udp
portmap		111/tcp
portmap		111/udp
nfs		2049/tcp
nfs		2049/udp
mountd		20048/tcp
mountd		20048/udp
`

func freeBSDNetworkSpec(cfg Config) machine.NetworkSpec {
	network := cfg.Network
	if strings.TrimSpace(network.GuestIPv4) == "" {
		network.GuestIPv4 = cfg.GuestIPv4
	}
	return normalizeFreeBSDNetwork(network)
}

func normalizeFreeBSDNetwork(network machine.NetworkSpec) machine.NetworkSpec {
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
		network.Hostname = "cc-freebsd"
	}
	if strings.TrimSpace(network.Interface) == "" {
		network.Interface = "vtnet0"
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
		return nil, nil, fmt.Errorf("read FreeBSD base set %s: %w", baseTar, err)
	}
	return tfs.Root(), tfs.Close, nil
}

func ensureDecompressedTar(ctx context.Context, source string) (string, error) {
	target, err := release.EnsureDecompressed(ctx, source, "", func(r io.Reader) (io.ReadCloser, error) {
		xzr, err := xz.NewReader(r)
		if err != nil {
			return nil, fmt.Errorf("read FreeBSD release xz %s: %w", source, err)
		}
		return io.NopCloser(xzr), nil
	})
	if err != nil {
		return "", fmt.Errorf("decompress FreeBSD release set %s: %w", source, err)
	}
	return target, nil
}

func ensureGzipDecompressedTar(ctx context.Context, source string) (string, error) {
	target, err := release.EnsureDecompressed(ctx, source, "", func(r io.Reader) (io.ReadCloser, error) {
		gz, err := gzip.NewReader(r)
		if err != nil {
			return nil, fmt.Errorf("read FreeBSD OCI root gzip %s: %w", source, err)
		}
		return gz, nil
	})
	if err != nil {
		return "", fmt.Errorf("decompress FreeBSD OCI root layer %s: %w", source, err)
	}
	return target, nil
}

func ExtractKernel(kernelSetPath string) ([]byte, error) {
	kernelTar, err := ensureDecompressedTar(context.Background(), kernelSetPath)
	if err != nil {
		return nil, err
	}
	return extractKernelFromTar(kernelTar)
}

func extractKernelFromTar(kernelSetPath string) ([]byte, error) {
	f, err := os.Open(kernelSetPath)
	if err != nil {
		return nil, fmt.Errorf("open FreeBSD kernel set %s: %w", kernelSetPath, err)
	}
	defer f.Close()
	tr := tar.NewReader(f)
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
	path, err := release.EnsureArtifact(ctx, release.Artifact{
		CacheDir:        cfg.CacheDir,
		Family:          "freebsd",
		Version:         cfg.Version,
		Arch:            cfg.Arch,
		Mirror:          cfg.Mirror,
		Name:            name,
		URLPath:         cfg.Arch + "/" + cfg.Version + "-RELEASE/" + name,
		LocalCandidates: localFixturePaths(cfg, name),
	})
	if err != nil {
		return "", fmt.Errorf("ensure FreeBSD artifact %s: %w", name, err)
	}
	return path, nil
}

func localFixturePaths(cfg Config, name string) []string {
	return []string{
		filepath.Join("local", "freebsd"+versionNoDot(cfg.Version)+"-"+cfg.Arch, name),
		filepath.Join(".cache", "freebsd"+versionNoDot(cfg.Version), name),
	}
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
/sbin/mount -u -o rw / || :
/sbin/mount -t devfs devfs /dev || :
/bin/chmod 1777 /tmp || :
/bin/chmod 700 /root || :
/sbin/ifconfig %s inet %s netmask 255.255.255.0 up || {
	while :; do /bin/sleep 3600; done
}
/sbin/route add default %s || true
exec /sbin/cc-freebsd-init
`
