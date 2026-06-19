//go:build windows && amd64

package vm

import (
	"context"
	"fmt"
	"io/fs"
	"strings"
	"time"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/amd64vm"
	"j5.nz/cc/internal/guestinit"
	"j5.nz/cc/internal/hv/whp"
	"j5.nz/cc/internal/imagefs"
	"j5.nz/cc/internal/kernel/alpine"
	managedguest "j5.nz/cc/internal/managed/guest"
	"j5.nz/cc/internal/managed/machine"
	"j5.nz/cc/internal/managed/rootartifact"
	managedruntime "j5.nz/cc/internal/managed/runtime"
	managedsession "j5.nz/cc/internal/managed/session"
	"j5.nz/cc/internal/oci"
	"j5.nz/cc/internal/virtio"
	"j5.nz/cc/internal/vm/execplan"
	hostmanaged "j5.nz/cc/internal/vm/host/managed"
	whphost "j5.nz/cc/internal/vm/host/whp"
	"j5.nz/cc/internal/vm/mounts"
	"j5.nz/cc/internal/vm/netstate"
	"j5.nz/cc/internal/vmruntime"
)

const windowsInitReadyMarker = vmruntime.InstanceReadyMarker

type runtimeBackend struct {
	kernel         *alpine.Manager
	images         *oci.Store
	guestInitCache string
}

func NewRuntimeBackend(kernel *alpine.Manager, images *oci.Store, guestInitCache string) Backend {
	return &runtimeBackend{kernel: kernel, images: images, guestInitCache: guestInitCache}
}

func (b *runtimeBackend) Start(ctx context.Context, req client.CreateInstanceRequest) (Instance, error) {
	return b.StartStream(ctx, req, nil)
}

func (b *runtimeBackend) StartStream(ctx context.Context, req client.CreateInstanceRequest, onEvent func(client.BootEvent) error) (Instance, error) {
	if inst, ok, err := b.startBuiltinGuestStream(ctx, req, onEvent); ok || err != nil {
		return inst, err
	}
	if b == nil || b.kernel == nil || b.images == nil {
		return nil, fmt.Errorf("runtime backend is not configured")
	}
	if req.CPUs > 1 {
		return nil, fmt.Errorf("windows amd64 runtime currently supports only 1 CPU")
	}
	kernel, err := b.kernel.ReadKernel()
	if err != nil {
		return nil, err
	}
	image, err := b.images.Open(req.Image)
	if err != nil {
		return nil, err
	}
	if err := ensureWindowsAMD64Image(image); err != nil {
		return nil, err
	}
	image = withWindowsRuntimeMountDirs(image)
	modules, err := b.kernel.PlanModuleLoad(windowsRuntimeConfigVars(), windowsRuntimeModuleMap())
	if err != nil {
		return nil, err
	}
	network, err := newWindowsAMD64NetworkRuntime(req.Network)
	if err != nil {
		return nil, err
	}
	if network != nil {
		defer func() {
			if err != nil {
				_ = network.Close()
			}
		}()
	}
	fsdevs, rootFS, err := amd64vm.BuildFSDevices(vmruntime.RunRequest{
		Image:  image,
		Shares: mounts.ConvertShareMounts(req.Shares),
	}, nil)
	if err != nil {
		return nil, err
	}
	initBin, err := guestinit.BuildForArch(ctx, b.guestInitCache, "amd64")
	if err != nil {
		return nil, fmt.Errorf("build guest init: %w", err)
	}
	workDir := image.Config.WorkingDir
	if workDir == "" {
		workDir = "/"
	}
	initCfg := windowsGuestInitConfig(modules, true)
	initCfg.RootFSTag = vmruntime.RootFSTag
	initCfg.Env = vmruntime.WithDefaultEnv(image.Config.Env)
	initCfg.WorkDir = workDir
	initCfg.Network = windowsNetworkGuestInitConfig(network)
	initrd, err := vmruntime.BuildInitramfs(initBin, modules, initCfg)
	if err != nil {
		return nil, fmt.Errorf("build initramfs: %w", err)
	}
	started, err := (managedruntime.Service{}).Start(ctx, managedruntime.StartRequest{
		Profile: managedguest.LinuxProfile,
		Host:    whp.Host{},
		Spec:    windowsLinuxMachineSpec(req.MemoryMB, req.CPUs, req.Dmesg),
		Artifact: rootartifact.Artifact{
			Kernel: kernel,
			Initrd: initrd,
		},
		Attachments: whp.LinuxManagedAttachments{
			FSDevices: fsdevs,
			NetDevice: windowsNetworkDevice(network),
		},
	}, onEvent)
	if err != nil {
		return nil, err
	}
	return &windowsInstance{
		managedInstanceCore: newWindowsManagedCore(started.Session, image, vmruntime.WithDefaultEnv(image.Config.Env), workDir),
		image:               image,
		rootFS:              rootFS,
		fsdevs:              fsdevs,
		network:             network,
		dmesg:               req.Dmesg,
	}, nil
}

func (b *runtimeBackend) StartBlank(ctx context.Context, req client.StartInstanceRequest) (Instance, error) {
	return b.StartBlankStream(ctx, req, nil)
}

func (b *runtimeBackend) StartBlankStream(ctx context.Context, req client.StartInstanceRequest, onEvent func(client.BootEvent) error) (Instance, error) {
	if inst, ok, err := b.startBuiltinGuestBlankStream(ctx, req, onEvent); ok || err != nil {
		return inst, err
	}
	if b == nil || b.kernel == nil || b.images == nil {
		return nil, fmt.Errorf("runtime backend is not configured")
	}
	if req.CPUs > 1 {
		return nil, fmt.Errorf("windows amd64 runtime currently supports only 1 CPU")
	}
	if strings.TrimSpace(req.Image) != "" {
		return b.StartStream(ctx, client.CreateInstanceRequest{
			ID:             req.ID,
			Image:          req.Image,
			InitSystem:     req.InitSystem,
			Kernel:         req.Kernel,
			Network:        req.Network,
			KernelModules:  append([]string(nil), req.KernelModules...),
			MemoryMB:       req.MemoryMB,
			CPUs:           req.CPUs,
			NestedVirt:     req.NestedVirt,
			Dmesg:          req.Dmesg,
			TimeoutSeconds: req.TimeoutSeconds,
		}, onEvent)
	}
	kernel, err := b.kernel.ReadKernel()
	if err != nil {
		return nil, err
	}
	modules, err := b.kernel.PlanModuleLoad(windowsRuntimeConfigVars(), windowsRuntimeModuleMap())
	if err != nil {
		return nil, err
	}
	network, err := newWindowsAMD64NetworkRuntime(req.Network)
	if err != nil {
		return nil, err
	}
	if network != nil {
		defer func() {
			if err != nil {
				_ = network.Close()
			}
		}()
	}
	rootFSBackend := virtio.NewImageFS(blankWindowsRuntimeRootFS(), "")
	fsdevs, rootFS, err := amd64vm.BuildFSDevices(vmruntime.RunRequest{
		RootFS: rootFSBackend,
	}, nil)
	if err != nil {
		return nil, err
	}
	initBin, err := guestinit.BuildForArch(ctx, b.guestInitCache, "amd64")
	if err != nil {
		return nil, fmt.Errorf("build guest init: %w", err)
	}
	initCfg := windowsGuestInitConfig(modules, true)
	initCfg.RootFSTag = vmruntime.RootFSTag
	initCfg.Env = vmruntime.WithDefaultEnv(nil)
	initCfg.WorkDir = "/"
	initCfg.Network = windowsNetworkGuestInitConfig(network)
	initrd, err := vmruntime.BuildInitramfs(initBin, modules, initCfg)
	if err != nil {
		return nil, fmt.Errorf("build initramfs: %w", err)
	}
	started, err := (managedruntime.Service{}).Start(ctx, managedruntime.StartRequest{
		Profile: managedguest.LinuxProfile,
		Host:    whp.Host{},
		Spec:    windowsLinuxMachineSpec(req.MemoryMB, req.CPUs, req.Dmesg),
		Artifact: rootartifact.Artifact{
			Kernel: kernel,
			Initrd: initrd,
		},
		Attachments: whp.LinuxManagedAttachments{
			FSDevices: fsdevs,
			NetDevice: windowsNetworkDevice(network),
		},
	}, onEvent)
	if err != nil {
		return nil, err
	}
	return &windowsInstance{
		managedInstanceCore: newWindowsManagedCore(started.Session, nil, vmruntime.WithDefaultEnv(nil), "/"),
		rootFS:              rootFS,
		fsdevs:              fsdevs,
		network:             network,
		dmesg:               req.Dmesg,
	}, nil
}

func (b *runtimeBackend) Run(ctx context.Context, req client.RunRequest) (client.ExecResponse, error) {
	if resp, ok, err := b.runBuiltinGuest(ctx, req); ok || err != nil {
		return resp, err
	}
	if b == nil || b.kernel == nil {
		return client.ExecResponse{}, fmt.Errorf("runtime backend is not configured")
	}
	if req.CPUs > 1 {
		return client.ExecResponse{}, fmt.Errorf("windows amd64 runtime currently supports only 1 CPU")
	}
	kernel, err := b.kernel.ReadKernel()
	if err != nil {
		return client.ExecResponse{}, err
	}

	var (
		modules []alpine.Module
		fsdevs  []*virtio.FS
		image   *oci.Image
		env     []string
		workDir string
	)
	if req.Image != "" {
		if b.images == nil {
			return client.ExecResponse{}, fmt.Errorf("windows amd64 runtime command execution requires an image store and image")
		}
		image, err = b.images.Open(req.Image)
		if err != nil {
			return client.ExecResponse{}, err
		}
		if err := ensureWindowsAMD64Image(image); err != nil {
			return client.ExecResponse{}, err
		}
		image = withWindowsRuntimeMountDirs(image)
		modules, err = b.kernel.PlanModuleLoad(windowsRuntimeConfigVars(), windowsRuntimeModuleMap())
		if err != nil {
			return client.ExecResponse{}, err
		}
		devs, _, err := amd64vm.BuildFSDevices(vmruntime.RunRequest{
			Image:  image,
			Shares: mounts.ConvertShareMounts(req.Shares),
		}, nil)
		if err != nil {
			return client.ExecResponse{}, err
		}
		fsdevs = devs
		env = vmruntime.WithDefaultEnv(vmruntime.MergeEnv(image.Config.Env, req.Env))
		workDir = req.WorkDir
		if workDir == "" {
			workDir = image.Config.WorkingDir
		}
		if workDir == "" {
			workDir = "/"
		}
	}

	initBin, err := guestinit.BuildForArch(ctx, b.guestInitCache, "amd64")
	if err != nil {
		return client.ExecResponse{}, fmt.Errorf("build guest init: %w", err)
	}
	network, err := newWindowsAMD64NetworkRuntime(req.Network)
	if err != nil {
		return client.ExecResponse{}, err
	}
	if network != nil {
		defer network.Close()
	}
	initCfg := windowsGuestInitConfig(modules, len(req.Command) != 0)
	if len(fsdevs) != 0 {
		initCfg.RootFSTag = vmruntime.RootFSTag
	}
	initCfg.Network = windowsNetworkGuestInitConfig(network)
	initrd, err := vmruntime.BuildInitramfs(initBin, modules, initCfg)
	if err != nil {
		return client.ExecResponse{}, fmt.Errorf("build initramfs: %w", err)
	}

	if len(req.Command) != 0 {
		if image == nil {
			return client.ExecResponse{}, fmt.Errorf("windows amd64 runtime command execution requires an image store and image")
		}
		if !strings.HasPrefix(workDir, "/") {
			return client.ExecResponse{}, fmt.Errorf("workdir must be absolute")
		}
		command, err := imagefs.ResolveCommand(image.RootFS, req.Command, env)
		if err != nil {
			return client.ExecResponse{}, err
		}
		execReq := client.ExecRequest{
			Command:   command,
			Env:       env,
			WorkDir:   workDir,
			User:      req.User,
			Stdin:     append([]byte(nil), req.Stdin...),
			TTY:       req.TTY,
			ControlFD: req.ControlFD,
			Cols:      req.Cols,
			Rows:      req.Rows,
		}
		resp, serial, err := whp.RunManagedExecWithFSAndNet(ctx, kernel, initrd, req.MemoryMB, req.Dmesg, fsdevs, windowsNetworkDevice(network), execReq)
		if err != nil && resp.Output == "" {
			resp.Output = serial
		}
		return resp, err
	}

	var output string
	if len(fsdevs) != 0 || network != nil {
		output, err = whp.BootInitramfsToMarkerWithFSAndNet(ctx, kernel, initrd, req.MemoryMB, true, windowsInitReadyMarker, fsdevs, windowsNetworkDevice(network))
	} else {
		output, err = whp.BootInitramfsToMarker(ctx, kernel, initrd, req.MemoryMB, true, windowsInitReadyMarker)
	}
	if err != nil {
		return client.ExecResponse{Output: output}, err
	}
	return client.ExecResponse{ExitCode: 0, Output: output}, nil
}

func (b *runtimeBackend) RunStream(ctx context.Context, req client.RunRequest, inputs <-chan client.ExecInput, onEvent func(client.ExecEvent) error) error {
	inst, err := b.StartStream(ctx, client.CreateInstanceRequest{
		Image:         req.Image,
		InitSystem:    req.InitSystem,
		Kernel:        req.Kernel,
		Shares:        append([]client.ShareMount(nil), req.Shares...),
		Network:       req.Network,
		KernelModules: append([]string(nil), req.KernelModules...),
		MemoryMB:      req.MemoryMB,
		CPUs:          req.CPUs,
		NestedVirt:    req.NestedVirt,
		Dmesg:         req.Dmesg,
	}, nil)
	if err != nil {
		return err
	}
	defer inst.Close()
	return inst.ExecStream(ctx, runExecRequest(req), inputs, onEvent)
}

func (b *runtimeBackend) RunInInstance(ctx context.Context, inst Instance, runningImage string, req client.RunRequest) (client.ExecResponse, error) {
	targetImage := strings.TrimSpace(req.Image)
	if targetImage == "" || targetImage == runningImage {
		if err := mounts.AddRuntimeShares(ctx, inst, req.Shares); err != nil {
			return client.ExecResponse{}, err
		}
		return inst.Exec(ctx, runExecRequest(req))
	}

	if err := execplan.CheckAlternateImageExec(inst); err != nil {
		return client.ExecResponse{}, err
	}
	session, ok := inst.(*windowsInstance)
	if !ok {
		return client.ExecResponse{}, fmt.Errorf("running instance does not support image mounts")
	}
	if b == nil || b.images == nil {
		return client.ExecResponse{}, fmt.Errorf("runtime backend is not configured")
	}
	image, err := b.images.Open(targetImage)
	if err != nil {
		return client.ExecResponse{}, err
	}
	if err := ensureWindowsAMD64Image(image); err != nil {
		return client.ExecResponse{}, err
	}
	image = withWindowsRuntimeMountDirs(image)
	mountPath := whphost.ImageMountPath(targetImage)
	if err := mounts.MountAlternateImageWithShares(ctx, inst, session, mountPath, image, req.Shares); err != nil {
		return client.ExecResponse{}, err
	}

	execReq, err := execplan.ResolveRunRequest(req, mountPath, execplan.Resolver{
		Root:           image.RootFS,
		BaseEnv:        image.Config.Env,
		DefaultWorkDir: image.Config.WorkingDir,
		Env:            mergeImageRunEnv,
	})
	if err != nil {
		return client.ExecResponse{}, err
	}
	return inst.Exec(ctx, execReq)
}

func (b *runtimeBackend) RunInInstanceStream(ctx context.Context, inst Instance, runningImage string, req client.RunRequest, inputs <-chan client.ExecInput, onEvent func(client.ExecEvent) error) error {
	targetImage := strings.TrimSpace(req.Image)
	if targetImage == "" || targetImage == runningImage {
		if err := mounts.AddRuntimeShares(ctx, inst, req.Shares); err != nil {
			return err
		}
		return inst.ExecStream(ctx, runExecRequest(req), inputs, onEvent)
	}

	if err := execplan.CheckAlternateImageExec(inst); err != nil {
		return err
	}
	session, ok := inst.(*windowsInstance)
	if !ok {
		return fmt.Errorf("running instance does not support image mounts")
	}
	if b == nil || b.images == nil {
		return fmt.Errorf("runtime backend is not configured")
	}
	image, err := b.images.Open(targetImage)
	if err != nil {
		return err
	}
	if err := ensureWindowsAMD64Image(image); err != nil {
		return err
	}
	image = withWindowsRuntimeMountDirs(image)
	mountPath := whphost.ImageMountPath(targetImage)
	if err := mounts.MountAlternateImageWithShares(ctx, inst, session, mountPath, image, req.Shares); err != nil {
		return err
	}

	execReq, err := execplan.ResolveRunRequest(req, mountPath, execplan.Resolver{
		Root:           image.RootFS,
		BaseEnv:        image.Config.Env,
		DefaultWorkDir: image.Config.WorkingDir,
		Env:            mergeImageRunEnv,
	})
	if err != nil {
		return err
	}
	return inst.ExecStream(ctx, execReq, inputs, onEvent)
}

func (b *runtimeBackend) ExecInInstanceStream(ctx context.Context, inst Instance, runningImage string, req client.ExecRequest, inputs <-chan client.ExecInput, onEvent func(client.ExecEvent) error) error {
	targetImage := strings.TrimSpace(req.Image)
	req.Image = ""
	if targetImage == "" || targetImage == runningImage {
		return inst.ExecStream(ctx, req, inputs, onEvent)
	}
	if err := execplan.CheckAlternateImageExec(inst); err != nil {
		return err
	}
	session, ok := inst.(*windowsInstance)
	if !ok {
		return fmt.Errorf("running instance does not support image mounts")
	}
	if b == nil || b.images == nil {
		return fmt.Errorf("runtime backend is not configured")
	}
	image, err := b.images.Open(targetImage)
	if err != nil {
		return err
	}
	if err := ensureWindowsAMD64Image(image); err != nil {
		return err
	}
	image = withWindowsRuntimeMountDirs(image)
	mountPath := whphost.ImageMountPath(targetImage)
	if err := mounts.MountAlternateImageWithShares(ctx, inst, session, mountPath, image, nil); err != nil {
		return err
	}
	req.RootDir = rootDirWithinMount(mountPath, req.RootDir)
	return inst.ExecStream(ctx, req, inputs, onEvent)
}

func ensureWindowsAMD64Image(image *oci.Image) error {
	if image == nil {
		return nil
	}
	arch := strings.TrimSpace(image.Architecture)
	if arch == "" || arch == "amd64" {
		return nil
	}
	name := strings.TrimSpace(image.Name)
	if name == "" {
		name = "image"
	}
	return fmt.Errorf("windows/amd64 runtime supports only amd64 images; %s is %s", name, arch)
}

type windowsInstance struct {
	*managedInstanceCore
	image   *oci.Image
	rootFS  virtio.ShareMounter
	fsdevs  []*virtio.FS
	dmesg   bool
	network *windowsNetworkRuntime
	mounts  mounts.State
}

func (i *windowsInstance) ManagedCapabilities() guestCapabilities {
	return managedguest.LinuxProfile.Caps
}

func newWindowsManagedCore(session managedsession.Session, image *oci.Image, baseEnv []string, workDir string) *managedInstanceCore {
	var root imagefs.Directory
	if image != nil {
		root = image.RootFS
	}
	return hostmanaged.NewCore(hostmanaged.Config{
		OSName:         "Linux",
		Session:        session,
		Root:           root,
		BaseEnv:        baseEnv,
		WorkDir:        workDir,
		Capabilities:   managedguest.LinuxProfile.Caps,
		Env:            windowsEffectiveExecEnv,
		MissingRootErr: "running instance does not have a default image root filesystem",
	})
}

func (i *windowsInstance) core() *managedInstanceCore {
	if i == nil {
		return nil
	}
	return i.managedInstanceCore
}

func windowsLinuxMachineSpec(memoryMB uint64, cpus int, dmesg bool) machine.Spec {
	return machine.Spec{
		Guest:    "Linux",
		Arch:     "amd64",
		MemoryMB: memoryMB,
		CPUs:     cpus,
		Dmesg:    dmesg,
		Boot:     machine.BootSpec{Kind: "linux"},
		Control:  machine.ControlSpec{Kind: "vsock", Port: vmruntime.ControlPort},
	}
}

func (i *windowsInstance) VirtioFSStats() []virtio.FSStats {
	if i == nil {
		return nil
	}
	return virtioFSStats(i.fsdevs)
}

func (i *windowsInstance) Exec(ctx context.Context, req client.ExecRequest) (client.ExecResponse, error) {
	return i.core().Exec(ctx, req)
}

func (i *windowsInstance) ExecStream(ctx context.Context, req client.ExecRequest, inputs <-chan client.ExecInput, onEvent func(client.ExecEvent) error) error {
	return i.core().ExecStream(ctx, req, inputs, onEvent)
}

func (i *windowsInstance) resolveExecRequest(req client.ExecRequest) (client.ExecRequest, error) {
	return i.core().ExecRequest(req)
}

func (i *windowsInstance) ConsoleHistory(ctx context.Context) (string, error) {
	return i.core().ConsoleHistory(ctx)
}

func (i *windowsInstance) AddShare(ctx context.Context, share client.ShareMount) error {
	_ = ctx
	if i == nil || i.rootFS == nil {
		return mounts.AddRuntimeShareMount(nil, nil, nil, share, "shares", nil)
	}
	return i.mounts.AddShare(i.rootFS, share, "shares", func(share client.ShareMount) (virtio.ShareMount, error) {
		return mounts.BuildRuntimeDirectoryShare(share, amd64vm.BuildShareMount)
	})
}

func (i *windowsInstance) AddPortForward(ctx context.Context, forward client.PortForward) error {
	if i == nil || i.network == nil {
		return netstate.AddManagedNetworkPortForward(ctx, nil, forward)
	}
	return netstate.AddManagedNetworkPortForward(ctx, i.network.networkRuntime, forward)
}

func (i *windowsInstance) AllowServiceProxyPort(ctx context.Context, port int) error {
	if i == nil || i.network == nil {
		return netstate.AllowManagedNetworkServiceProxyPort(ctx, nil, port)
	}
	return netstate.AllowManagedNetworkServiceProxyPort(ctx, i.network.networkRuntime, port)
}

func (i *windowsInstance) AddImage(ctx context.Context, mountPath string, image *oci.Image) error {
	_ = ctx
	if i == nil {
		return mounts.AddImageMount(nil, nil, nil, mountPath, image, nil)
	}
	return i.mounts.AddImage(i.rootFS, mountPath, image, mounts.ImageFSBackend(image))
}

func (i *windowsInstance) Wait() error {
	if i == nil {
		return nil
	}
	return i.core().Wait()
}

func (i *windowsInstance) Close() error {
	if i == nil {
		return nil
	}
	var session managedsession.Session
	if core := i.core(); core != nil {
		session = core.Session()
	}
	var network hostmanaged.NetworkCloser
	if i.network != nil {
		network = i.network
	}
	return hostmanaged.CloseSessionWithNetwork(session, network)
}

func (i *windowsInstance) NetworkIPv4() string {
	if i == nil || i.network == nil {
		return ""
	}
	return netstate.IPv4(i.network.networkRuntime, "")
}

func windowsGuestInitConfig(modules []alpine.Module, managedExec bool) vmruntime.GuestInitConfig {
	cfg := vmruntime.GuestInitConfig{
		Modules:            vmruntime.ModulePaths(modules),
		ReadyMarker:        windowsInitReadyMarker,
		BeginMarker:        vmruntime.CommandBeginMarker,
		OutputMarkerPref:   vmruntime.CommandOutputMarker,
		ErrorMarkerPref:    vmruntime.CommandErrorMarker,
		ControlMarkerPref:  vmruntime.CommandControlMarker,
		UsageMarkerPref:    vmruntime.CommandUsageMarker,
		ExitMarkerPrefix:   vmruntime.CommandExitMarkerPref,
		DisableCgroupMount: true,
		UnixTime:           time.Now().Unix(),
	}
	if managedExec {
		cfg.VsockPort = vmruntime.ControlPort
	}
	return cfg
}

func windowsRuntimeConfigVars() []string {
	return []string{"CONFIG_VIRTIO_MMIO", "CONFIG_FUSE_FS", "CONFIG_VIRTIO_FS", "CONFIG_VSOCKETS", "CONFIG_VIRTIO_VSOCKETS", "CONFIG_HW_RANDOM", "CONFIG_HW_RANDOM_VIRTIO", "CONFIG_VIRTIO_NET", "CONFIG_OVERLAY_FS"}
}

func windowsRuntimeModuleMap() map[string]string {
	return map[string]string{
		"CONFIG_VIRTIO_MMIO":      "kernel/drivers/virtio/virtio_mmio.ko.gz",
		"CONFIG_FUSE_FS":          "kernel/fs/fuse/fuse.ko.gz",
		"CONFIG_VIRTIO_FS":        "kernel/fs/fuse/virtiofs.ko.gz",
		"CONFIG_VSOCKETS":         "kernel/net/vmw_vsock/vsock.ko.gz",
		"CONFIG_VIRTIO_VSOCKETS":  "kernel/net/vmw_vsock/vmw_vsock_virtio_transport.ko.gz",
		"CONFIG_HW_RANDOM":        "kernel/drivers/char/hw_random/rng-core.ko.gz",
		"CONFIG_HW_RANDOM_VIRTIO": "kernel/drivers/char/hw_random/virtio-rng.ko.gz",
		"CONFIG_VIRTIO_NET":       "kernel/drivers/net/virtio_net.ko.gz",
		"CONFIG_OVERLAY_FS":       "kernel/fs/overlayfs/overlay.ko.gz",
	}
}

func withWindowsRuntimeMountDirs(image *oci.Image) *oci.Image {
	if image == nil || image.RootFS == nil {
		return image
	}
	overlay := imagefs.NewOverlay(image.RootFS)
	for _, dir := range []string{"/dev", "/proc", "/sys", "/run"} {
		_ = overlay.AddDir(dir, fs.ModeDir|0o755)
	}
	_ = overlay.AddDir("/tmp", fs.ModeDir|0o1777)
	cloned := *image
	cloned.RootFS = overlay.Root()
	return &cloned
}

func blankWindowsRuntimeRootFS() imagefs.Directory {
	overlay := imagefs.NewOverlay(nil)
	for _, dir := range []string{"/dev", "/proc", "/sys", "/run", "/.ccx3", "/.ccx3/images"} {
		_ = overlay.AddDir(dir, fs.ModeDir|0o755)
	}
	_ = overlay.AddDir("/tmp", fs.ModeDir|0o1777)
	return overlay.Root()
}

func windowsEffectiveExecEnv(base, overrides []string, replace bool) []string {
	if replace {
		return vmruntime.WithDefaultEnv(overrides)
	}
	return vmruntime.WithDefaultEnv(vmruntime.MergeEnv(base, overrides))
}
