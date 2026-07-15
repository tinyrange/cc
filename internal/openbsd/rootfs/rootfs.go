package rootfs

import (
	"archive/tar"
	"bytes"
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
	BuiltInImageName  = managedguest.OpenBSDImageName
	defaultVersion    = "7.9"
	defaultArch       = "amd64"
	defaultMirror     = "https://mirror.aarnet.edu.au/pub/OpenBSD"
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
	initBin, err := openbsdguestinit.BuildForArch(ctx, filepath.Join(cfg.CacheDir, "guestinit"), cfg.Arch)
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

func BuildManagedRuntimeFromOCI(ctx context.Context, cfg Config, kernel []byte, rootLayerPath string) (*Runtime, error) {
	cfg = normalizeConfig(cfg)
	baseTar, err := ensureDecompressedTar(ctx, rootLayerPath)
	if err != nil {
		return nil, err
	}
	tfs, err := imagefs.NewSeekableTarFS(ctx, baseTar)
	if err != nil {
		return nil, fmt.Errorf("read OpenBSD OCI root layer %s: %w", baseTar, err)
	}
	initBin, err := openbsdguestinit.BuildForArch(ctx, filepath.Join(cfg.CacheDir, "guestinit"), cfg.Arch)
	if err != nil {
		_ = tfs.Close()
		return nil, err
	}
	root, closeRoot, err := buildManagedRootFromBase(ctx, tfs.Root(), tfs.Close, initBin, openBSDNetworkSpec(cfg))
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
	return &Runtime{Kernel: append([]byte(nil), kernel...), Root: region, RootFS: root, close: closeRoot}, nil
}

func BuildManagedRoot(ctx context.Context, baseSetPath string, initBin []byte) (imagefs.Directory, error) {
	root, _, err := buildManagedRoot(ctx, baseSetPath, initBin, machine.NetworkSpec{})
	return root, err
}

func buildManagedRoot(ctx context.Context, baseSetPath string, initBin []byte, network machine.NetworkSpec) (imagefs.Directory, func() error, error) {
	network = normalizeOpenBSDNetwork(network)
	root, closeRoot, err := buildBaseRoot(ctx, baseSetPath, []byte(fmt.Sprintf(managedInitScript, managedInitDate(), network.Interface, network.GuestIPv4, network.GatewayIPv4, network.GatewayMAC, network.GatewayIPv4)))
	if err != nil {
		return nil, nil, err
	}
	return buildManagedRootFromPreparedBase(root, closeRoot, initBin, network)
}

func buildManagedRootFromBase(ctx context.Context, base imagefs.Directory, closeBase func() error, initBin []byte, network machine.NetworkSpec) (imagefs.Directory, func() error, error) {
	network = normalizeOpenBSDNetwork(network)
	overlay := imagefs.NewOverlay(base)
	if err := overlayOpenBSDEtcSet(ctx, overlay, base); err != nil {
		_ = closeBase()
		return nil, nil, err
	}
	if err := addRuntimeLibraryLinks(overlay, base); err != nil {
		_ = closeBase()
		return nil, nil, err
	}
	if err := rootplan.AddDevices(overlay, openBSDManagedDevices()); err != nil {
		_ = closeBase()
		return nil, nil, err
	}
	if err := overlay.AddFile("/sbin/init", 0o755, []byte(fmt.Sprintf(managedInitScript, managedInitDate(), network.Interface, network.GuestIPv4, network.GatewayIPv4, network.GatewayMAC, network.GatewayIPv4))); err != nil {
		_ = closeBase()
		return nil, nil, fmt.Errorf("overlay /sbin/init: %w", err)
	}
	return buildManagedRootFromPreparedBase(overlay.Root(), closeBase, initBin, network)
}

func buildManagedRootFromPreparedBase(root imagefs.Directory, closeRoot func() error, initBin []byte, network machine.NetworkSpec) (imagefs.Directory, func() error, error) {
	overlay := imagefs.NewOverlay(root)
	if err := rootplan.AddFiles(overlay, []rootplan.File{
		{Path: "/sbin/cc-openbsd-init", Mode: 0o755, Data: initBin},
		{Path: "/etc/fstab", Mode: 0o644, Data: []byte("/dev/sd0a / ffs rw,noatime 1 1\n")},
		{Path: "/etc/myname", Mode: 0o644, Data: []byte(network.Hostname + "\n")},
		{Path: "/etc/resolv.conf", Mode: 0o644, Data: []byte("nameserver " + network.DNSIPv4 + "\n")},
		{Path: "/etc/hosts", Mode: 0o644, Data: []byte(fmt.Sprintf("127.0.0.1 localhost\n%s %s\n", network.GuestIPv4, network.Hostname))},
		{Path: "/etc/services", Mode: 0o644, Data: []byte(bsdNetworkServices)},
	}); err != nil {
		_ = closeRoot()
		return nil, nil, err
	}
	return overlay.Root(), closeRoot, nil
}

func overlayOpenBSDEtcSet(ctx context.Context, overlay *imagefs.Overlay, root imagefs.Directory) error {
	entry, err := imagefs.LookupPath(root, "/var/sysmerge/etc.tgz")
	if err != nil {
		return nil
	}
	if entry.File == nil {
		return fmt.Errorf("OpenBSD /var/sysmerge/etc.tgz is not a file")
	}
	size, _ := entry.File.Stat()
	if size > uint64(^uint32(0)) {
		return fmt.Errorf("OpenBSD /var/sysmerge/etc.tgz is too large: %d bytes", size)
	}
	data, err := entry.File.ReadAt(0, uint32(size))
	if err != nil {
		return fmt.Errorf("read OpenBSD /var/sysmerge/etc.tgz: %w", err)
	}
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("read OpenBSD /var/sysmerge/etc.tgz gzip: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read OpenBSD etc set: %w", err)
		}
		guestPath := openBSDEtcSetPath(hdr.Name)
		if guestPath == "" {
			continue
		}
		mode := fs.FileMode(hdr.Mode) & 0o7777
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := overlay.AddDir(guestPath, mode); err != nil {
				return fmt.Errorf("overlay OpenBSD etc dir %s: %w", guestPath, err)
			}
		case tar.TypeReg, tar.TypeRegA:
			fileData, err := io.ReadAll(tr)
			if err != nil {
				return fmt.Errorf("read OpenBSD etc file %s: %w", guestPath, err)
			}
			if err := overlay.AddFile(guestPath, mode, fileData); err != nil {
				return fmt.Errorf("overlay OpenBSD etc file %s: %w", guestPath, err)
			}
		case tar.TypeSymlink:
			if err := overlay.AddSymlink(guestPath, hdr.Linkname); err != nil {
				return fmt.Errorf("overlay OpenBSD etc symlink %s: %w", guestPath, err)
			}
		}
	}
}

func openBSDEtcSetPath(name string) string {
	name = strings.TrimPrefix(name, "./")
	name = strings.TrimPrefix(name, "/")
	clean := path.Clean("/" + name)
	if clean == "/" || clean == "/." {
		return ""
	}
	return clean
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
	if strings.TrimSpace(network.GatewayMAC) == "" {
		network.GatewayMAC = defaultGatewayMAC
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
	if err := overlayOpenBSDEtcSet(ctx, overlay, tfs.Root()); err != nil {
		_ = tfs.Close()
		return nil, nil, err
	}
	if err := addRuntimeLibraryLinks(overlay, tfs.Root()); err != nil {
		_ = tfs.Close()
		return nil, nil, err
	}
	if err := rootplan.AddDevices(overlay, openBSDManagedDevices()); err != nil {
		_ = tfs.Close()
		return nil, nil, err
	}
	if err := overlay.AddFile("/sbin/init", 0o755, init); err != nil {
		_ = tfs.Close()
		return nil, nil, fmt.Errorf("overlay /sbin/init: %w", err)
	}
	return overlay.Root(), tfs.Close, nil
}

func openBSDManagedDevices() []rootplan.Device {
	devices := []rootplan.Device{
		{Path: "/dev/console", Mode: fs.ModeDevice | fs.ModeCharDevice | 0o600, RDev: 0},
		{Path: "/dev/null", Mode: fs.ModeDevice | fs.ModeCharDevice | 0o666, RDev: 514},
		{Path: "/dev/zero", Mode: fs.ModeDevice | fs.ModeCharDevice | 0o666, RDev: 515},
		{Path: "/dev/random", Mode: fs.ModeDevice | fs.ModeCharDevice | 0o644, RDev: 565},
		{Path: "/dev/urandom", Mode: fs.ModeDevice | fs.ModeCharDevice | 0o644, RDev: 566},
		{Path: "/dev/ptm", Mode: fs.ModeDevice | fs.ModeCharDevice | 0o666, RDev: 81 << 8},
		{Path: "/dev/sd0a", Mode: fs.ModeDevice | 0o640, RDev: 1024},
		{Path: "/dev/sd0b", Mode: fs.ModeDevice | 0o640, RDev: 1025},
	}
	for minor, suffix := range "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ" {
		devices = append(devices,
			rootplan.Device{Path: fmt.Sprintf("/dev/ttyp%c", suffix), Mode: fs.ModeDevice | fs.ModeCharDevice | 0o666, RDev: uint32(5<<8 | minor)},
			rootplan.Device{Path: fmt.Sprintf("/dev/ptyp%c", suffix), Mode: fs.ModeDevice | fs.ModeCharDevice | 0o666, RDev: uint32(6<<8 | minor)},
		)
	}
	return devices
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

func managedInitDate() string {
	return time.Now().UTC().Format("200601021504.05")
}

const managedInitScript = `#!/bin/sh
exec >/dev/console 2>&1
/sbin/mount -u -o rw,noatime / || {
	echo OPENBSD_MANAGED_REMOUNT_FAILED
	while :; do /bin/sleep 3600; done
}
/bin/date -u %s >/dev/null 2>&1 || true
/sbin/ifconfig %s inet %s netmask 255.255.255.0 up || {
	echo OPENBSD_MANAGED_IFCONFIG_FAILED
	while :; do /bin/sleep 3600; done
}
/usr/sbin/arp -s %s %s >/dev/null 2>&1 || true
/sbin/route add default %s || true
exec /sbin/cc-openbsd-init
`
