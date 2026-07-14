//go:build darwin && arm64

package vm

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/guestinit"
	"j5.nz/cc/internal/imagefs"
	"j5.nz/cc/internal/kernel/ubuntu"
	"j5.nz/cc/internal/oci"
	"j5.nz/cc/internal/virtio"
	"j5.nz/cc/internal/vm/mounts"
	"j5.nz/cc/internal/vmruntime"
)

const sidecarFSSocketEnv = "CCX3_WORKER_FS_SOCKET"
const sidecarBootSocketEnv = "CCX3_WORKER_BOOT_SOCKET"

const (
	sidecarNetSocketEnv = "CCX3_WORKER_NET_SOCKET"
	sidecarNetIPv4Env   = "CCX3_WORKER_NET_IPV4"
	sidecarNetMACEnv    = "CCX3_WORKER_NET_MAC"
	sidecarNetModeEnv   = "CCX3_WORKER_NET_MODE"
)

const (
	sidecarBootTLVEnd      uint16 = 0
	sidecarBootTLVMetadata uint16 = 1
	sidecarBootTLVKernel   uint16 = 2
	sidecarBootTLVInit     uint16 = 3
	sidecarBootTLVModule   uint16 = 4
)

type sidecarBootBundleMetadata struct {
	ImageName          string            `json:"image_name,omitempty"`
	Architecture       string            `json:"architecture,omitempty"`
	Config             oci.RuntimeConfig `json:"config,omitempty"`
	AMD64EmulatorPath  string            `json:"amd64_emulator_path,omitempty"`
	ModuleNames        []string          `json:"module_names,omitempty"`
	NeedsAMD64Emulator bool              `json:"needs_amd64_emulator,omitempty"`
}

func prepareSidecarCreateResources(h *sidecarVMHost, ctx context.Context, req client.CreateInstanceRequest) (sidecarStartResources, error) {
	_ = ctx
	if isBuiltinGuestImage(req.Image) {
		return sidecarStartResources{}, fmt.Errorf("built-in guest image %q must be started by the managed guest runtime, not sidecar OCI resources", req.Image)
	}
	if h.images == nil {
		return sidecarStartResources{}, fmt.Errorf("sidecar image store is not configured")
	}
	image, err := h.images.Open(req.Image)
	if err != nil {
		return sidecarStartResources{}, err
	}
	image = withRuntimeMountDirs(image)
	bootResources, err := prepareSidecarBootResources(ctx, h, image, req.Kernel, req.KernelModules, req.Network)
	if err != nil {
		return sidecarStartResources{}, err
	}
	root := virtio.NewImageFS(image.RootFS, image.RootFSDir)
	shares, err := sidecarShareMounts(req.Shares)
	if err != nil {
		return sidecarStartResources{}, err
	}
	rootFS, err := sidecarSnapshotRoot(virtio.NewMountedFS(root, shares))
	if err != nil {
		return sidecarStartResources{}, err
	}
	fsResources, err := serveSidecarFS(h.cacheDir, rootFS)
	if err != nil {
		return sidecarStartResources{}, err
	}
	fsResources.resolver = newSidecarCommandResolver(image)
	netResources, err := prepareSidecarNetResources(h.cacheDir, req.ID, req.Network)
	if err != nil {
		fsResources.closeAll()
		bootResources.closeAll()
		return sidecarStartResources{}, err
	}
	return combineSidecarResources(fsResources, bootResources, netResources), nil
}

func prepareSidecarBlankResources(h *sidecarVMHost, ctx context.Context, req client.StartInstanceRequest) (sidecarStartResources, error) {
	_ = ctx
	var root virtio.FSBackend
	var resolver *sidecarCommandResolver
	var image *oci.Image
	if imageName := strings.TrimSpace(req.Image); imageName != "" {
		if isBuiltinGuestImage(imageName) {
			return sidecarStartResources{}, fmt.Errorf("built-in guest image %q must be started by the managed guest runtime, not sidecar OCI resources", imageName)
		}
		if h.images == nil {
			return sidecarStartResources{}, fmt.Errorf("sidecar image store is not configured")
		}
		var err error
		image, err = h.images.Open(imageName)
		if err != nil {
			return sidecarStartResources{}, err
		}
		image = withRuntimeMountDirs(image)
		root = virtio.NewImageFS(image.RootFS, image.RootFSDir)
		resolver = newSidecarCommandResolver(image)
	} else {
		root = virtio.NewImageFS(blankRuntimeRootFS(), "")
	}
	shares, err := sidecarShareMounts(req.Shares)
	if err != nil {
		return sidecarStartResources{}, err
	}
	rootFS, err := sidecarSnapshotRoot(virtio.NewMountedFS(root, shares))
	if err != nil {
		return sidecarStartResources{}, err
	}
	fsResources, err := serveSidecarFS(h.cacheDir, rootFS)
	if err != nil {
		return sidecarStartResources{}, err
	}
	fsResources.resolver = resolver
	netResources, err := prepareSidecarNetResources(h.cacheDir, req.ID, req.Network)
	if err != nil {
		fsResources.closeAll()
		return sidecarStartResources{}, err
	}
	if strings.TrimSpace(req.RestoreSnapshot) != "" {
		return combineSidecarResources(fsResources, netResources), nil
	}
	bootResources, err := prepareSidecarBootResources(ctx, h, image, req.Kernel, req.KernelModules, req.Network)
	if err != nil {
		fsResources.closeAll()
		netResources.closeAll()
		return sidecarStartResources{}, err
	}
	return combineSidecarResources(fsResources, bootResources, netResources), nil
}

func prepareSidecarBuiltinGuestResources(h *sidecarVMHost, id string, cfg *client.NetworkConfig) (sidecarStartResources, error) {
	if h == nil {
		return sidecarStartResources{}, nil
	}
	return prepareSidecarNetResourcesWithMode(h.cacheDir, id, cfg, "bridge")
}

func sidecarSnapshotRoot(backend virtio.FSBackend) (sidecarRootFS, error) {
	rootFS, ok := backend.(sidecarRootFS)
	if !ok {
		return nil, fmt.Errorf("sidecar rootfs backend cannot be snapshotted")
	}
	return rootFS, nil
}

func sidecarKernelProvider(h *sidecarVMHost, flavor string) runtimeKernelProvider {
	if path, ok := customKernelPath(flavor); ok {
		if h == nil {
			return customKernelProvider{path: path}
		}
		return customKernelProvider{path: path, modules: h.kernel}
	}
	if h != nil && h.images != nil && normalizeRuntimeKernel(flavor) == "ubuntu" {
		return ubuntu.NewManager(filepath.Join(h.images.Root(), "_kernels", "ubuntu"))
	}
	if h == nil {
		return nil
	}
	return h.kernel
}

func prepareSidecarBootResources(ctx context.Context, h *sidecarVMHost, image *oci.Image, kernelFlavor string, kernelModules []string, cfg *client.NetworkConfig) (sidecarStartResources, error) {
	if h.kernel == nil {
		return sidecarStartResources{}, fmt.Errorf("sidecar kernel manager is not configured")
	}
	if h.images == nil {
		return sidecarStartResources{}, fmt.Errorf("sidecar image store is not configured")
	}
	kernelProvider := sidecarKernelProvider(h, kernelFlavor)
	if kernelProvider == nil {
		return sidecarStartResources{}, fmt.Errorf("sidecar kernel provider is not configured")
	}
	kernel, err := kernelProvider.ReadKernel()
	if err != nil {
		return sidecarStartResources{}, err
	}
	configVars, moduleMap := runtimeKernelRequirements(kernelFlavor, image, cfg != nil && cfg.Enabled, kernelModules)
	needsAMD64 := NeedsAMD64Emulation(image)
	if needsAMD64 {
		configVars = append(configVars, "CONFIG_BINFMT_MISC")
	}
	modules, err := kernelProvider.PlanModuleLoad(configVars, moduleMap)
	if err != nil {
		return sidecarStartResources{}, err
	}
	qemuX8664, err := PrepareAMD64Emulator(ctx, image, h.kernel.ExtractPackageFile)
	if err != nil {
		return sidecarStartResources{}, err
	}
	guestInitCache := h.guestInitCache
	if guestInitCache == "" {
		guestInitCache = filepath.Join(h.images.Root(), "_guestinit")
	}
	initBin, err := guestinit.Build(ctx, guestInitCache)
	if err != nil {
		return sidecarStartResources{}, err
	}
	bundle := sidecarBootBundle{
		Kernel:             kernel,
		Init:               initBin,
		AMD64EmulatorPath:  qemuX8664,
		Modules:            modules,
		NeedsAMD64Emulator: needsAMD64,
	}
	if image != nil {
		bundle.ImageName = image.Name
		bundle.Architecture = image.Architecture
		bundle.Config = image.Config
	}
	return serveSidecarBootBundle(h.cacheDir, bundle)
}

func serveSidecarBootBundle(cacheDir string, bundle sidecarBootBundle) (sidecarStartResources, error) {
	socketPath, closeFn, err := serveSidecarUnixOnce(cacheDir, "boot", func(conn net.Conn) error {
		return writeSidecarBootBundle(conn, bundle)
	})
	if err != nil {
		return sidecarStartResources{}, err
	}
	return sidecarStartResources{
		env:    []string{sidecarBootSocketEnv + "=" + socketPath},
		close:  closeFn,
		remote: true,
	}, nil
}

func writeSidecarBootBundle(w io.Writer, bundle sidecarBootBundle) error {
	moduleNames := make([]string, 0, len(bundle.Modules))
	for _, module := range bundle.Modules {
		moduleNames = append(moduleNames, module.Name)
	}
	metadata, err := json.Marshal(sidecarBootBundleMetadata{
		ImageName:          bundle.ImageName,
		Architecture:       bundle.Architecture,
		Config:             bundle.Config,
		AMD64EmulatorPath:  bundle.AMD64EmulatorPath,
		ModuleNames:        moduleNames,
		NeedsAMD64Emulator: bundle.NeedsAMD64Emulator,
	})
	if err != nil {
		return err
	}
	if err := writeSidecarBootTLV(w, sidecarBootTLVMetadata, metadata); err != nil {
		return err
	}
	if err := writeSidecarBootTLV(w, sidecarBootTLVKernel, bundle.Kernel); err != nil {
		return err
	}
	if err := writeSidecarBootTLV(w, sidecarBootTLVInit, bundle.Init); err != nil {
		return err
	}
	for _, module := range bundle.Modules {
		if err := writeSidecarBootTLV(w, sidecarBootTLVModule, module.Data); err != nil {
			return err
		}
	}
	return writeSidecarBootTLV(w, sidecarBootTLVEnd, nil)
}

func writeSidecarBootTLV(w io.Writer, typ uint16, data []byte) error {
	var header [10]byte
	binary.BigEndian.PutUint16(header[:2], typ)
	binary.BigEndian.PutUint64(header[2:], uint64(len(data)))
	if _, err := w.Write(header[:]); err != nil {
		return err
	}
	if len(data) == 0 {
		return nil
	}
	_, err := w.Write(data)
	return err
}

func serveSidecarFS(cacheDir string, backend sidecarRootFS) (sidecarStartResources, error) {
	socketPath, closeFn, err := serveSidecarUnixOnce(cacheDir, "fs", func(conn net.Conn) error {
		return virtio.ServeFSBackend(conn, backend)
	})
	if err != nil {
		return sidecarStartResources{}, err
	}
	return sidecarStartResources{
		env:    []string{sidecarFSSocketEnv + "=" + socketPath},
		close:  closeFn,
		remote: true,
		rootFS: backend,
	}, nil
}

func prepareSidecarNetResources(cacheDir, id string, cfg *client.NetworkConfig) (sidecarStartResources, error) {
	mode := ""
	if cfg != nil {
		switch strings.ToLower(strings.TrimSpace(cfg.Mode)) {
		case "":
			mode = "bridge"
		case "bridge":
			mode = "bridge"
		case "remote":
		default:
			return sidecarStartResources{}, fmt.Errorf("unsupported sidecar network mode %q", cfg.Mode)
		}
	}
	return prepareSidecarNetResourcesWithMode(cacheDir, id, cfg, mode)
}

func prepareSidecarNetResourcesWithMode(cacheDir, id string, cfg *client.NetworkConfig, mode string) (sidecarStartResources, error) {
	if cfg == nil || !cfg.Enabled {
		return sidecarStartResources{}, nil
	}
	lease, explicit := darwinSidecarLeaseFromConfig(id, cfg)
	if !explicit {
		lease = defaultDarwinSidecarSwitch.Register(id)
	}
	var codecMu sync.Mutex
	var codec *virtio.NetPacketCodec
	runtime, err := newNetworkRuntime(networkDeviceConfig{
		ID:     lease.id,
		Config: cfg,
		IP:     lease.ip,
		MAC:    lease.mac,
		TXHook: func(packet []byte) {
			defaultDarwinSidecarSwitch.Forward(lease.id, packet)
		},
		RXHook: func(frame []byte) error {
			codecMu.Lock()
			defer codecMu.Unlock()
			if codec == nil {
				return io.ErrClosedPipe
			}
			return codec.Send(virtio.NetPacket{
				Kind:     virtio.NetPacketRX,
				VMID:     lease.id,
				DeviceID: "eth0",
				Frame:    append([]byte(nil), frame...),
			})
		},
		Cleanup: func() {
			if !explicit {
				defaultDarwinSidecarSwitch.Unregister(lease.id)
			}
		},
	})
	if err != nil {
		if !explicit {
			defaultDarwinSidecarSwitch.Unregister(lease.id)
		}
		return sidecarStartResources{}, err
	}
	socketPath, cleanupListener, err := serveSidecarUnixOnceConn(cacheDir, "net", false, func(conn net.Conn) error {
		netCodec := virtio.NewNetPacketCodec(conn)
		codecMu.Lock()
		codec = netCodec
		codecMu.Unlock()
		defaultDarwinSidecarSwitch.Attach(darwinSidecarEndpoint{
			id:  lease.id,
			ip:  lease.ip,
			mac: lease.mac,
			rx: func(frame []byte) {
				_ = netCodec.Send(virtio.NetPacket{
					Kind:     virtio.NetPacketRX,
					VMID:     lease.id,
					DeviceID: "eth0",
					Frame:    append([]byte(nil), frame...),
				})
			},
		})
		return virtio.ReceiveNetPackets(context.Background(), netCodec, func(packet virtio.NetPacket) error {
			if packet.Kind != virtio.NetPacketTX {
				return nil
			}
			defaultDarwinSidecarSwitch.Forward(lease.id, packet.Frame)
			if mode == "bridge" {
				return nil
			}
			if runtime == nil {
				return nil
			}
			return runtime.ifaceDeliver(packet.Frame)
		})
	})
	if err != nil {
		_ = runtime.Close()
		return sidecarStartResources{}, err
	}
	closeFn := func() {
		cleanupListener()
		codecMu.Lock()
		if codec != nil {
			_ = codec.Close()
			codec = nil
		}
		codecMu.Unlock()
		defaultDarwinSidecarSwitch.Unregister(lease.id)
		if runtime != nil {
			_ = runtime.Close()
		}
	}
	return sidecarStartResources{
		env: []string{
			sidecarNetSocketEnv + "=" + socketPath,
			sidecarNetIPv4Env + "=" + lease.ip.String(),
			sidecarNetMACEnv + "=" + lease.mac.String(),
			sidecarNetModeEnv + "=" + mode,
		},
		close:       closeFn,
		remote:      true,
		networkIPv4: lease.ip.String(),
		network:     runtime,
	}, nil
}

func darwinSidecarLeaseFromConfig(id string, cfg *client.NetworkConfig) (darwinSidecarLease, bool) {
	if cfg == nil {
		return darwinSidecarLease{}, false
	}
	ip := net.ParseIP(strings.TrimSpace(cfg.GuestIPv4)).To4()
	if ip == nil {
		return darwinSidecarLease{}, false
	}
	macText := strings.TrimSpace(cfg.GuestMAC)
	if macText == "" {
		ip4 := ip.To4()
		macText = net.HardwareAddr{0x02, 0x42, 0x0a, 0x2a, 0x00, ip4[3]}.String()
	}
	mac, err := net.ParseMAC(macText)
	if err != nil {
		return darwinSidecarLease{}, false
	}
	id = strings.TrimSpace(id)
	if id == "" {
		id = DefaultInstanceID
	}
	return darwinSidecarLease{id: id, ip: ip, mac: mac}, true
}

type darwinSidecarSwitch struct {
	mu        sync.Mutex
	leases    map[string]darwinSidecarLease
	endpoints map[string]darwinSidecarEndpoint
}

type darwinSidecarLease struct {
	id  string
	ip  net.IP
	mac net.HardwareAddr
}

type darwinSidecarEndpoint struct {
	id  string
	ip  net.IP
	mac net.HardwareAddr
	rx  func([]byte)
}

var defaultDarwinSidecarSwitch = &darwinSidecarSwitch{
	leases:    make(map[string]darwinSidecarLease),
	endpoints: make(map[string]darwinSidecarEndpoint),
}

func (s *darwinSidecarSwitch) Register(id string) darwinSidecarLease {
	id = strings.TrimSpace(id)
	if id == "" {
		id = DefaultInstanceID
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if lease, ok := s.leases[id]; ok {
		return lease
	}
	if endpoint, ok := s.endpoints[id]; ok {
		return darwinSidecarLease{id: endpoint.id, ip: endpoint.ip, mac: endpoint.mac}
	}
	used := map[byte]bool{1: true}
	for _, lease := range s.leases {
		if ip4 := lease.ip.To4(); ip4 != nil {
			used[ip4[3]] = true
		}
	}
	for _, endpoint := range s.endpoints {
		if ip4 := endpoint.ip.To4(); ip4 != nil {
			used[ip4[3]] = true
		}
	}
	host := byte(2)
	for ; host <= 254; host++ {
		if !used[host] {
			break
		}
	}
	lease := darwinSidecarLease{
		id:  id,
		ip:  net.IPv4(10, 42, 0, host),
		mac: net.HardwareAddr{0x02, 0x42, 0x0a, 0x2a, 0x00, host},
	}
	s.leases[id] = lease
	return lease
}

func (s *darwinSidecarSwitch) Attach(endpoint darwinSidecarEndpoint) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.leases, endpoint.id)
	s.endpoints[endpoint.id] = endpoint
}

func (s *darwinSidecarSwitch) Unregister(id string) {
	id = strings.TrimSpace(id)
	if id == "" {
		id = DefaultInstanceID
	}
	s.mu.Lock()
	delete(s.leases, id)
	delete(s.endpoints, id)
	s.mu.Unlock()
}

func (s *darwinSidecarSwitch) Forward(sourceID string, frame []byte) {
	if len(frame) < 14 {
		return
	}
	dst := append(net.HardwareAddr(nil), frame[0:6]...)
	if binary.BigEndian.Uint16(frame[12:14]) == 0x0806 && len(frame) >= 42 {
		targetIP := net.IP(frame[38:42]).To4()
		if targetIP != nil {
			if target := s.endpointByIP(sourceID, targetIP); target.rx != nil {
				target.rx(frame)
				return
			}
		}
	}
	if isDarwinSidecarBroadcastMAC(dst) || isDarwinSidecarMulticastMAC(dst) {
		s.forwardToAll(sourceID, frame)
		return
	}
	if target := s.endpointByMAC(sourceID, dst); target.rx != nil {
		target.rx(frame)
	}
}

func (s *darwinSidecarSwitch) endpointByIP(sourceID string, ip net.IP) darwinSidecarEndpoint {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, endpoint := range s.endpoints {
		if id == sourceID {
			continue
		}
		if endpoint.ip.Equal(ip) {
			return endpoint
		}
	}
	return darwinSidecarEndpoint{}
}

func (s *darwinSidecarSwitch) endpointByMAC(sourceID string, mac net.HardwareAddr) darwinSidecarEndpoint {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, endpoint := range s.endpoints {
		if id == sourceID {
			continue
		}
		if equalDarwinSidecarMAC(endpoint.mac, mac) {
			return endpoint
		}
	}
	return darwinSidecarEndpoint{}
}

func (s *darwinSidecarSwitch) forwardToAll(sourceID string, frame []byte) {
	s.mu.Lock()
	targets := make([]darwinSidecarEndpoint, 0, len(s.endpoints))
	for id, endpoint := range s.endpoints {
		if id != sourceID {
			targets = append(targets, endpoint)
		}
	}
	s.mu.Unlock()
	for _, target := range targets {
		if target.rx != nil {
			target.rx(frame)
		}
	}
}

func isDarwinSidecarBroadcastMAC(mac net.HardwareAddr) bool {
	if len(mac) != 6 {
		return false
	}
	for _, b := range mac {
		if b != 0xff {
			return false
		}
	}
	return true
}

func isDarwinSidecarMulticastMAC(mac net.HardwareAddr) bool {
	return len(mac) == 6 && mac[0]&1 == 1
}

func equalDarwinSidecarMAC(a, b net.HardwareAddr) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func sidecarShareMounts(shares []client.ShareMount) ([]virtio.ShareMount, error) {
	if len(shares) == 0 {
		return nil, nil
	}
	out := make([]virtio.ShareMount, 0, len(shares))
	for i, share := range mounts.ConvertShareMounts(shares) {
		mount, err := arm64ShareMount(share)
		if err != nil {
			return nil, fmt.Errorf("share %d: %w", i, err)
		}
		out = append(out, mount)
	}
	return out, nil
}

func sidecarRuntimeShareMount(share client.ShareMount) (virtio.ShareMount, error) {
	shares := mounts.ConvertShareMounts([]client.ShareMount{share})
	if len(shares) != 1 {
		return virtio.ShareMount{}, fmt.Errorf("runtime share is required")
	}
	return arm64ShareMount(shares[0])
}

func arm64ShareMount(share vmruntime.DirectoryShare) (virtio.ShareMount, error) {
	_, backend, err := buildDarwinShareBackend(share)
	if err != nil {
		return virtio.ShareMount{}, err
	}
	return virtio.ShareMount{
		GuestPath: share.Mount,
		Backend:   backend,
		Writable:  share.Writable,
		CacheMode: share.Cache,
	}, nil
}

func buildDarwinShareBackend(share vmruntime.DirectoryShare) (string, virtio.FSBackend, error) {
	source := strings.TrimSpace(share.Source)
	if source == "" {
		return "", nil, fmt.Errorf("share source is required")
	}
	info, err := os.Stat(source)
	if err != nil {
		return "", nil, err
	}
	if !info.IsDir() {
		return "", nil, fmt.Errorf("share source must be a directory")
	}
	if share.Writable {
		if share.MapOwner {
			return "", virtio.NewPassthroughFSWithOwner(source, nil, share.OwnerUID, share.OwnerGID), nil
		}
		return "", virtio.NewPassthroughFS(source, nil), nil
	}
	if share.MapOwner {
		return "", virtio.NewImageFSWithOwner(imagefs.NewHostFS(source, nil), source, share.OwnerUID, share.OwnerGID), nil
	}
	return "", virtio.NewImageFS(imagefs.NewHostFS(source, nil), source), nil
}
