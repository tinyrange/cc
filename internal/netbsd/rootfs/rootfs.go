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
	BuiltInImageName  = managedguest.NetBSDImageName
	defaultVersion    = "10.1"
	defaultArch       = "amd64"
	defaultMirror     = "https://mirror.aarnet.edu.au/pub/netbsd"
	defaultGatewayMAC = "02:42:0a:2a:00:01"
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
	kernelGZ, err := ensureArtifact(ctx, cfg, kernelArtifactName(cfg), "kernel")
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
	initBin, err := netbsdguestinit.BuildForArch(ctx, filepath.Join(cfg.CacheDir, "guestinit"), goArchForNetBSD(cfg.Arch))
	if err != nil {
		return nil, err
	}
	root, closeRoot, err := buildManagedRoot(ctx, baseTXZ, initBin, cfg.Arch, netBSDNetworkSpec(cfg), rootDeviceForNetBSD(cfg.Arch))
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

func BuildManagedRuntimeFromOCI(ctx context.Context, cfg Config, kernel []byte, rootLayerPath string) (*Runtime, error) {
	cfg = normalizeConfig(cfg)
	baseTar, err := ensureGzipDecompressedTar(ctx, rootLayerPath)
	if err != nil {
		return nil, err
	}
	tfs, err := imagefs.NewSeekableTarFS(ctx, baseTar)
	if err != nil {
		return nil, fmt.Errorf("read NetBSD OCI root layer %s: %w", baseTar, err)
	}
	initBin, err := netbsdguestinit.BuildForArch(ctx, filepath.Join(cfg.CacheDir, "guestinit"), goArchForNetBSD(cfg.Arch))
	if err != nil {
		_ = tfs.Close()
		return nil, err
	}
	root, closeRoot, err := buildManagedRootFromBase(tfs.Root(), tfs.Close, initBin, cfg.Arch, netBSDNetworkSpec(cfg), rootDeviceForNetBSD(cfg.Arch))
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
	return &Runtime{Kernel: append([]byte(nil), kernel...), Root: region, RootFS: root, close: closeRoot}, nil
}

func BuildManagedRoot(ctx context.Context, baseSetPath string, initBin []byte) (imagefs.Directory, error) {
	root, _, err := buildManagedRoot(ctx, baseSetPath, initBin, defaultArch, machine.NetworkSpec{}, "ld0a")
	return root, err
}

func buildManagedRoot(ctx context.Context, baseSetPath string, initBin []byte, arch string, network machine.NetworkSpec, rootDevices ...string) (imagefs.Directory, func() error, error) {
	network = normalizeNetBSDNetwork(network)
	rootDevice := ""
	if len(rootDevices) > 0 {
		rootDevice = rootDevices[0]
	}
	if strings.TrimSpace(rootDevice) == "" {
		rootDevice = "ld0a"
	}
	root, closeRoot, err := buildBaseRoot(ctx, baseSetPath)
	if err != nil {
		return nil, nil, err
	}
	return buildManagedRootFromBase(root, closeRoot, initBin, arch, network, rootDevice)
}

func buildManagedRootFromBase(root imagefs.Directory, closeRoot func() error, initBin []byte, arch string, network machine.NetworkSpec, rootDevices ...string) (imagefs.Directory, func() error, error) {
	network = normalizeNetBSDNetwork(network)
	rootDevice := ""
	if len(rootDevices) > 0 {
		rootDevice = rootDevices[0]
	}
	if strings.TrimSpace(rootDevice) == "" {
		rootDevice = "ld0a"
	}
	overlay := imagefs.NewOverlay(root)
	if err := rootplan.AddFiles(overlay, []rootplan.File{
		{Path: "/sbin/init", Mode: 0o755, Data: []byte(fmt.Sprintf(managedInitScript, managedInitDate(), network.Interface, network.GuestIPv4, network.GatewayIPv4, network.GatewayMAC, network.GatewayIPv4))},
		{Path: "/sbin/cc-netbsd-init", Mode: 0o755, Data: initBin},
		{Path: "/etc/fstab", Mode: 0o644, Data: []byte(fmt.Sprintf("/dev/%s / ffs rw 1 1\n", rootDevice))},
		{Path: "/etc/rc.conf", Mode: 0o644, Data: []byte(fmt.Sprintf("rc_configured=YES\nhostname=\"%s\"\ndefaultroute=\"%s\"\n", network.Hostname, network.GatewayIPv4))},
		{Path: "/etc/ifconfig." + network.Interface, Mode: 0o644, Data: []byte(fmt.Sprintf("inet %s netmask 255.255.255.0\n", network.GuestIPv4))},
		{Path: "/etc/resolv.conf", Mode: 0o644, Data: []byte("nameserver " + network.DNSIPv4 + "\n")},
		{Path: "/etc/hosts", Mode: 0o644, Data: []byte(fmt.Sprintf("127.0.0.1 localhost\n%s %s\n", network.GuestIPv4, network.Hostname))},
		{Path: "/etc/services", Mode: 0o644, Data: []byte(bsdNetworkServices)},
		{Path: "/etc/protocols", Mode: 0o644, Data: []byte(bsdNetworkProtocols)},
		{Path: "/etc/netconfig", Mode: 0o644, Data: []byte(netBSDNetconfig)},
		{Path: "/root/.profile", Mode: 0o644, Data: []byte(`export PKG_PATH=${PKG_PATH:-https://cdn.NetBSD.org/pub/pkgsrc/packages/NetBSD/x86_64/10.1/All}
export PATH=/bin:/sbin:/usr/bin:/usr/sbin:/usr/pkg/bin:/usr/pkg/sbin
`)},
	}); err != nil {
		_ = closeRoot()
		return nil, nil, err
	}
	if err := rootplan.AddDevices(overlay, netBSDManagedDevices(arch)); err != nil {
		_ = closeRoot()
		return nil, nil, err
	}
	return overlay.Root(), closeRoot, nil
}

type netBSDDeviceMajors struct {
	consChar uint32
	cttyChar uint32
	memChar  uint32
	rndChar  uint32
	ldBlock  uint32
	ldChar   uint32
}

func netBSDDeviceMajorsForArch(arch string) netBSDDeviceMajors {
	if arch == "evbarm-aarch64" {
		return netBSDDeviceMajors{
			consChar: 2,
			cttyChar: 3,
			memChar:  0,
			rndChar:  52,
			ldBlock:  92,
			ldChar:   92,
		}
	}
	return netBSDDeviceMajors{
		consChar: 0,
		cttyChar: 1,
		memChar:  2,
		rndChar:  46,
		ldBlock:  19,
		ldChar:   69,
	}
}

func netBSDManagedDevices(arch string) []rootplan.Device {
	maj := netBSDDeviceMajorsForArch(arch)
	return []rootplan.Device{
		{Path: "/dev/console", Mode: fs.ModeDevice | fs.ModeCharDevice | 0o600, RDev: rdev(maj.consChar, 0)},
		{Path: "/dev/constty", Mode: fs.ModeDevice | fs.ModeCharDevice | 0o600, RDev: rdev(maj.consChar, 1)},
		{Path: "/dev/tty", Mode: fs.ModeDevice | fs.ModeCharDevice | 0o666, RDev: rdev(maj.cttyChar, 0)},
		{Path: "/dev/null", Mode: fs.ModeDevice | fs.ModeCharDevice | 0o666, RDev: rdev(maj.memChar, 2)},
		{Path: "/dev/zero", Mode: fs.ModeDevice | fs.ModeCharDevice | 0o666, RDev: rdev(maj.memChar, 12)},
		{Path: "/dev/random", Mode: fs.ModeDevice | fs.ModeCharDevice | 0o444, RDev: rdev(maj.rndChar, 0)},
		{Path: "/dev/urandom", Mode: fs.ModeDevice | fs.ModeCharDevice | 0o644, RDev: rdev(maj.rndChar, 1)},
		{Path: "/dev/ld0", Mode: fs.ModeDevice | 0o640, RDev: rdev(maj.ldBlock, 3)},
		{Path: "/dev/ld0a", Mode: fs.ModeDevice | 0o640, RDev: rdev(maj.ldBlock, 0)},
		{Path: "/dev/ld0d", Mode: fs.ModeDevice | 0o640, RDev: rdev(maj.ldBlock, 3)},
		{Path: "/dev/rld0", Mode: fs.ModeDevice | fs.ModeCharDevice | 0o640, RDev: rdev(maj.ldChar, 3)},
		{Path: "/dev/rld0a", Mode: fs.ModeDevice | fs.ModeCharDevice | 0o640, RDev: rdev(maj.ldChar, 0)},
		{Path: "/dev/rld0d", Mode: fs.ModeDevice | fs.ModeCharDevice | 0o640, RDev: rdev(maj.ldChar, 3)},
		{Path: "/dev/ld4", Mode: fs.ModeDevice | 0o640, RDev: rdev(maj.ldBlock, 35)},
		{Path: "/dev/ld4a", Mode: fs.ModeDevice | 0o640, RDev: rdev(maj.ldBlock, 32)},
		{Path: "/dev/ld4d", Mode: fs.ModeDevice | 0o640, RDev: rdev(maj.ldBlock, 35)},
		{Path: "/dev/rld4", Mode: fs.ModeDevice | fs.ModeCharDevice | 0o640, RDev: rdev(maj.ldChar, 35)},
		{Path: "/dev/rld4a", Mode: fs.ModeDevice | fs.ModeCharDevice | 0o640, RDev: rdev(maj.ldChar, 32)},
		{Path: "/dev/rld4d", Mode: fs.ModeDevice | fs.ModeCharDevice | 0o640, RDev: rdev(maj.ldChar, 35)},
	}
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

const bsdNetworkProtocols = `ip	0	IP
icmp	1	ICMP
tcp	6	TCP
udp	17	UDP
`

const netBSDNetconfig = `udp	tpi_clts	v	inet	udp	-	-
tcp	tpi_cots_ord	v	inet	tcp	-	-
udp6	tpi_clts	v	inet6	udp	-	-
tcp6	tpi_cots_ord	v	inet6	tcp	-	-
`

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
	if strings.TrimSpace(network.GatewayMAC) == "" {
		network.GatewayMAC = defaultGatewayMAC
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

func ensureGzipDecompressedTar(ctx context.Context, source string) (string, error) {
	target, err := release.EnsureDecompressed(ctx, source, "", func(r io.Reader) (io.ReadCloser, error) {
		gz, err := gzip.NewReader(r)
		if err != nil {
			return nil, fmt.Errorf("read NetBSD OCI root gzip %s: %w", source, err)
		}
		return gz, nil
	})
	if err != nil {
		return "", fmt.Errorf("decompress NetBSD OCI root layer %s: %w", source, err)
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

func kernelArtifactName(cfg Config) string {
	if cfg.Arch == "evbarm-aarch64" {
		return "netbsd-GENERIC64.img.gz"
	}
	return "netbsd-GENERIC.gz"
}

func goArchForNetBSD(arch string) string {
	if arch == "evbarm-aarch64" {
		return "arm64"
	}
	return arch
}

func rootDeviceForNetBSD(arch string) string {
	if arch == "evbarm-aarch64" {
		return "ld4a"
	}
	return "ld0a"
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

func managedInitDate() string {
	return time.Now().UTC().Format("200601021504.05")
}

const managedInitScript = `#!/bin/sh
exec >/dev/console 2>&1
/sbin/mount -u -o rw / || {
	echo NETBSD_MANAGED_REMOUNT_FAILED
	while :; do /bin/sleep 3600; done
}
/bin/date -u %s >/dev/null 2>&1 || true
/sbin/sysctl -w net.inet.ip.dad_count=0 >/dev/null 2>&1 || true
/sbin/ifconfig %s inet %s netmask 255.255.255.0 up || {
	echo NETBSD_MANAGED_IFCONFIG_FAILED
	while :; do /bin/sleep 3600; done
}
/usr/sbin/arp -s %s %s >/dev/null 2>&1 || true
/sbin/route add default %s || true
exec /sbin/cc-netbsd-init
`
