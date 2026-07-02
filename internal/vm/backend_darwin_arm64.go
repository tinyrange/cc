//go:build darwin && arm64

package vm

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/guestinit"
	"j5.nz/cc/internal/hv/hvf"
	"j5.nz/cc/internal/imagefs"
	"j5.nz/cc/internal/kernel/alpine"
	"j5.nz/cc/internal/kernel/ubuntu"
	managedguest "j5.nz/cc/internal/managed/guest"
	"j5.nz/cc/internal/managed/machine"
	managedruntime "j5.nz/cc/internal/managed/runtime"
	"j5.nz/cc/internal/oci"
	"j5.nz/cc/internal/timing"
	"j5.nz/cc/internal/virtio"
	"j5.nz/cc/internal/vm/execplan"
	hvfhost "j5.nz/cc/internal/vm/host/hvf"
	"j5.nz/cc/internal/vm/mounts"
	"j5.nz/cc/internal/vmruntime"
)

var debugTiming = strings.TrimSpace(os.Getenv("CCX3_DEBUG_TIMING")) != ""

func timingLog(format string, args ...any) {
	if !debugTiming {
		return
	}
	fmt.Fprintf(os.Stderr, "ccx3 timing: "+format+"\n", args...)
}

type runtimeBackend struct {
	kernel         *alpine.Manager
	images         *oci.Store
	guestInitCache string
}

type runtimeKernelProvider interface {
	ReadKernel() ([]byte, error)
	PlanModuleLoad([]string, map[string]string) ([]alpine.Module, error)
}

func NewRuntimeBackend(kernel *alpine.Manager, images *oci.Store, guestInitCache string) Backend {
	return &runtimeBackend{kernel: kernel, images: images, guestInitCache: guestInitCache}
}

func (b *runtimeBackend) kernelProvider(flavor string) runtimeKernelProvider {
	if normalizeRuntimeKernel(flavor) == "ubuntu" && b.images != nil {
		return ubuntu.NewManager(filepath.Join(b.images.Root(), "_kernels", "ubuntu"))
	}
	return b.kernel
}

func normalizeRuntimeKernel(flavor string) string {
	flavor = strings.ToLower(strings.TrimSpace(flavor))
	switch flavor {
	case "", "default", "alpine":
		return ""
	default:
		return flavor
	}
}

func runtimeKernelRequirements(flavor string, image *oci.Image, network bool, extra []string) ([]string, map[string]string) {
	if normalizeRuntimeKernel(flavor) == "ubuntu" {
		return ubuntuRuntimeKernelRequirements(extra)
	}
	return alpineRuntimeKernelRequirements(network, extra)
}

func alpineRuntimeKernelRequirements(network bool, extra []string) ([]string, map[string]string) {
	configVars := []string{"CONFIG_VIRTIO_MMIO", "CONFIG_FUSE_FS", "CONFIG_VIRTIO_FS", "CONFIG_VSOCKETS", "CONFIG_VIRTIO_VSOCKETS", "CONFIG_HW_RANDOM", "CONFIG_HW_RANDOM_VIRTIO"}
	if network {
		configVars = append(configVars, "CONFIG_VIRTIO_NET")
	}
	configVars = append(configVars, extra...)
	moduleMap := map[string]string{
		"CONFIG_VIRTIO_MMIO":      "kernel/drivers/virtio/virtio_mmio.ko.gz",
		"CONFIG_FUSE_FS":          "kernel/fs/fuse/fuse.ko.gz",
		"CONFIG_VIRTIO_FS":        "kernel/fs/fuse/virtiofs.ko.gz",
		"CONFIG_VSOCKETS":         "kernel/net/vmw_vsock/vsock.ko.gz",
		"CONFIG_VIRTIO_VSOCKETS":  "kernel/net/vmw_vsock/vmw_vsock_virtio_transport.ko.gz",
		"CONFIG_HW_RANDOM":        "kernel/drivers/char/hw_random/rng-core.ko.gz",
		"CONFIG_HW_RANDOM_VIRTIO": "kernel/drivers/char/hw_random/virtio-rng.ko.gz",
		"CONFIG_VIRTIO_NET":       "kernel/drivers/net/virtio_net.ko.gz",
		"CONFIG_BINFMT_MISC":      "kernel/fs/binfmt_misc.ko.gz",
	}
	return configVars, moduleMap
}

func ubuntuRuntimeKernelRequirements(extra []string) ([]string, map[string]string) {
	configVars := []string{
		"CONFIG_VIRTIO_MMIO",
		"CONFIG_FUSE_FS",
		"CONFIG_VIRTIO_FS",
		"CONFIG_VSOCKETS",
		"CONFIG_VIRTIO_VSOCKETS",
		"CONFIG_HW_RANDOM",
		"CONFIG_HW_RANDOM_VIRTIO",
		"CONFIG_VIRTIO_NET",
		"CONFIG_OVERLAY_FS",
		"CONFIG_NF_TABLES",
		"CONFIG_IP_NF_IPTABLES",
		"CONFIG_BINFMT_MISC",
		"MODULE:autofs4",
	}
	configVars = append(configVars, extra...)
	moduleMap := map[string]string{
		"CONFIG_VIRTIO_FS":        "kernel/fs/fuse/virtiofs.ko.zst",
		"CONFIG_VSOCKETS":         "kernel/net/vmw_vsock/vsock.ko.zst",
		"CONFIG_VIRTIO_VSOCKETS":  "kernel/net/vmw_vsock/vmw_vsock_virtio_transport.ko.zst",
		"CONFIG_HW_RANDOM_VIRTIO": "kernel/drivers/char/hw_random/virtio-rng.ko.zst",
		"CONFIG_VIRTIO_NET":       "kernel/drivers/net/virtio_net.ko.zst",
		"CONFIG_OVERLAY_FS":       "kernel/fs/overlayfs/overlay.ko.zst",
		"CONFIG_NF_TABLES":        "kernel/net/netfilter/nf_tables.ko.zst",
		"CONFIG_IP_NF_IPTABLES":   "kernel/net/ipv4/netfilter/ip_tables.ko.zst",
		"CONFIG_BINFMT_MISC":      "kernel/fs/binfmt_misc.ko.zst",
		"MODULE:autofs4":          "kernel/fs/autofs/autofs4.ko.zst",
	}
	return configVars, moduleMap
}

func (b *runtimeBackend) Start(ctx context.Context, req client.CreateInstanceRequest) (Instance, error) {
	return b.StartStream(ctx, req, nil)
}

func (b *runtimeBackend) StartBlank(ctx context.Context, req client.StartInstanceRequest) (Instance, error) {
	return b.StartBlankStream(ctx, req, nil)
}

func (b *runtimeBackend) StartStream(ctx context.Context, req client.CreateInstanceRequest, onEvent func(client.BootEvent) error) (Instance, error) {
	if inst, ok, err := b.startBuiltinGuestStream(ctx, req, onEvent); ok || err != nil {
		return inst, err
	}
	start := time.Now()
	network, err := newDarwinARM64NetworkRuntime(req.ID, req.Network)
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
	runReq, err := b.buildStartRequest(ctx, req, network)
	if err != nil {
		return nil, err
	}
	timingLog("runtime.Start buildStartRequest took=%s image=%q", time.Since(start), req.Image)
	started, err := (managedruntime.Service{}).Start(ctx, managedruntime.StartRequest{
		Profile:     managedguest.LinuxProfile,
		Host:        hvf.Host{},
		Spec:        darwinLinuxMachineSpec(req.MemoryMB, req.CPUs, req.Dmesg),
		Attachments: hvf.LinuxManagedAttachments{RunRequest: runReq},
	}, onEvent)
	if err != nil {
		return nil, err
	}
	containerSession, ok := started.Session.(*hvf.ContainerSession)
	if !ok {
		_ = started.Session.Close()
		return nil, fmt.Errorf("hvf host returned %T, want *hvf.ContainerSession", started.Session)
	}
	timingLog("runtime.Start hvf.StartContainer took=%s image=%q", time.Since(start), req.Image)
	return newDarwinInstance(containerSession, network, strings.TrimSpace(req.Image)), nil
}

func (b *runtimeBackend) StartBlankStream(
	ctx context.Context,
	req client.StartInstanceRequest,
	onEvent func(client.BootEvent) error,
) (Instance, error) {
	if inst, ok, err := b.startBuiltinGuestBlankStream(ctx, req, onEvent); ok || err != nil {
		return inst, err
	}
	start := time.Now()
	network, err := newDarwinARM64NetworkRuntime(req.ID, req.Network)
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
	runReq, err := b.buildBlankStartRequest(ctx, req, network)
	if err != nil {
		return nil, err
	}
	timingLog("runtime.StartBlank buildBlankStartRequest took=%s", time.Since(start))
	started, err := (managedruntime.Service{}).Start(ctx, managedruntime.StartRequest{
		Profile:     managedguest.LinuxProfile,
		Host:        hvf.Host{},
		Spec:        darwinLinuxMachineSpec(req.MemoryMB, req.CPUs, req.Dmesg),
		Attachments: hvf.LinuxManagedAttachments{RunRequest: runReq},
	}, onEvent)
	if err != nil {
		return nil, err
	}
	containerSession, ok := started.Session.(*hvf.ContainerSession)
	if !ok {
		_ = started.Session.Close()
		return nil, fmt.Errorf("hvf host returned %T, want *hvf.ContainerSession", started.Session)
	}
	timingLog("runtime.StartBlank hvf.StartContainer took=%s", time.Since(start))
	return newDarwinInstance(containerSession, network, strings.TrimSpace(req.Image)), nil
}

func (b *runtimeBackend) Run(ctx context.Context, req client.RunRequest) (client.ExecResponse, error) {
	if resp, ok, err := b.runBuiltinGuest(ctx, req); ok || err != nil {
		return resp, err
	}
	network, err := newDarwinARM64NetworkRuntime(req.ID, req.Network)
	if err != nil {
		return client.ExecResponse{}, err
	}
	if network != nil {
		defer network.Close()
	}
	runReq, err := b.buildRunRequest(ctx, req, network)
	if err != nil {
		return client.ExecResponse{}, err
	}
	result, err := hvf.RunContainer(ctx, runReq)
	if err != nil {
		return client.ExecResponse{}, err
	}
	return client.ExecResponse{
		ExitCode: result.ExitCode,
		Output:   result.Output,
	}, nil
}

func (b *runtimeBackend) RunStream(ctx context.Context, req client.RunRequest, inputs <-chan client.ExecInput, onEvent func(client.ExecEvent) error) error {
	inst, err := b.StartStream(ctx, client.CreateInstanceRequest{
		Image:          req.Image,
		InitSystem:     req.InitSystem,
		Kernel:         req.Kernel,
		Shares:         append([]client.ShareMount(nil), req.Shares...),
		Network:        req.Network,
		KernelModules:  append([]string(nil), req.KernelModules...),
		MemoryMB:       req.MemoryMB,
		CPUs:           req.CPUs,
		NestedVirt:     req.NestedVirt,
		Dmesg:          req.Dmesg,
		TimeoutSeconds: req.TimeoutSeconds,
	}, nil)
	if err != nil {
		return err
	}
	defer inst.Close()
	return inst.ExecStream(ctx, runExecRequest(req), inputs, onEvent)
}

func (b *runtimeBackend) RunInInstance(
	ctx context.Context,
	inst Instance,
	runningImage string,
	req client.RunRequest,
) (client.ExecResponse, error) {
	targetImage := strings.TrimSpace(req.Image)
	if sameRuntimeImage(targetImage, runningImage) {
		if err := mounts.AddRuntimeShares(ctx, inst, req.Shares); err != nil {
			return client.ExecResponse{}, err
		}
		return inst.Exec(ctx, runExecRequest(req))
	}

	if err := execplan.CheckAlternateImageExec(inst); err != nil {
		return client.ExecResponse{}, err
	}
	if isBuiltinGuestImage(targetImage) {
		return client.ExecResponse{}, fmt.Errorf("managed guest image %q cannot be mounted as an alternate Linux root", targetImage)
	}
	session, ok := darwinContainerSession(inst)
	if !ok {
		return client.ExecResponse{}, fmt.Errorf("running instance does not support image mounts")
	}

	image, err := b.images.Open(targetImage)
	if err != nil {
		return client.ExecResponse{}, err
	}
	image = withRuntimeMountDirs(image)
	mountPath := hvfhost.ImageMountPath(targetImage)
	if err := mounts.MountAlternateImageWithShares(ctx, inst, session, mountPath, image, req.Shares); err != nil {
		return client.ExecResponse{}, err
	}

	execReq, err := execplan.ResolveRunRequest(req, mountPath, execplan.Resolver{
		Root:           image.RootFS,
		BaseEnv:        image.Config.Env,
		DefaultWorkDir: image.Config.WorkingDir,
		Env: func(base, overrides []string, _ bool) []string {
			return mergeRuntimeEnv(append([]string(nil), base...), overrides)
		},
	})
	if err != nil {
		return client.ExecResponse{}, err
	}
	return inst.Exec(ctx, execReq)
}

func (b *runtimeBackend) RunInInstanceStream(
	ctx context.Context,
	inst Instance,
	runningImage string,
	req client.RunRequest,
	inputs <-chan client.ExecInput,
	onEvent func(client.ExecEvent) error,
) error {
	targetImage := strings.TrimSpace(req.Image)
	if sameRuntimeImage(targetImage, runningImage) {
		if err := mounts.AddRuntimeShares(ctx, inst, req.Shares); err != nil {
			return err
		}
		return inst.ExecStream(ctx, runExecRequest(req), inputs, onEvent)
	}

	if err := execplan.CheckAlternateImageExec(inst); err != nil {
		return err
	}
	if isBuiltinGuestImage(targetImage) {
		return fmt.Errorf("managed guest image %q cannot be mounted as an alternate Linux root", targetImage)
	}
	session, ok := darwinContainerSession(inst)
	if !ok {
		return fmt.Errorf("running instance does not support image mounts")
	}

	image, err := b.images.Open(targetImage)
	if err != nil {
		return err
	}
	image = withRuntimeMountDirs(image)
	mountPath := hvfhost.ImageMountPath(targetImage)
	if err := mounts.MountAlternateImageWithShares(ctx, inst, session, mountPath, image, req.Shares); err != nil {
		return err
	}

	execReq, err := execplan.ResolveRunRequest(req, mountPath, execplan.Resolver{
		Root:           image.RootFS,
		BaseEnv:        image.Config.Env,
		DefaultWorkDir: image.Config.WorkingDir,
		Env: func(base, overrides []string, _ bool) []string {
			return mergeRuntimeEnv(append([]string(nil), base...), overrides)
		},
	})
	if err != nil {
		return err
	}
	return inst.ExecStream(ctx, execReq, inputs, onEvent)
}

func (b *runtimeBackend) ExecInInstanceStream(ctx context.Context, inst Instance, runningImage string, req client.ExecRequest, inputs <-chan client.ExecInput, onEvent func(client.ExecEvent) error) error {
	targetImage := strings.TrimSpace(req.Image)
	req.Image = ""
	if sameRuntimeImage(targetImage, runningImage) {
		return inst.ExecStream(ctx, req, inputs, onEvent)
	}

	if err := execplan.CheckAlternateImageExec(inst); err != nil {
		return err
	}
	if isBuiltinGuestImage(targetImage) {
		return fmt.Errorf("managed guest image %q cannot be mounted as an alternate Linux root", targetImage)
	}
	session, ok := darwinContainerSession(inst)
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
	image = withRuntimeMountDirs(image)
	mountPath := hvfhost.ImageMountPath(targetImage)
	if err := mounts.MountAlternateImageWithShares(ctx, inst, session, mountPath, image, nil); err != nil {
		return err
	}
	req.RootDir = rootDirWithinMount(mountPath, req.RootDir)
	return inst.ExecStream(ctx, req, inputs, onEvent)
}

func (b *runtimeBackend) buildBaseRequest(ctx context.Context, imageName string, initSystem string, kernelFlavor string, kernelModules []string, memoryMB uint64, cpus int, nestedVirt bool, dmesg bool, network *darwinNetworkRuntime) (vmruntime.RunRequest, error) {
	start := time.Now()
	if bundle, err := workerBootBundle(); err != nil {
		return vmruntime.RunRequest{}, err
	} else if bundle != nil {
		return vmruntime.RunRequest{
			Kernel:            append([]byte(nil), bundle.Kernel...),
			Init:              append([]byte(nil), bundle.Init...),
			AMD64EmulatorPath: bundle.AMD64EmulatorPath,
			Modules:           append([]alpine.Module(nil), bundle.Modules...),
			Image:             sidecarBundleImage(bundle),
			InitSystem:        initSystem,
			MemoryMB:          memoryMB,
			CPUs:              cpus,
			NestedVirt:        nestedVirt,
			Dmesg:             dmesg,
			Network:           network.guestInitConfig(),
			NetDevice:         networkDeviceDarwin(network),
			UnixTime:          time.Now().Unix(),
		}, ctx.Err()
	}
	if b.kernel == nil || b.images == nil {
		return vmruntime.RunRequest{}, fmt.Errorf("runtime backend is not configured")
	}
	image, err := b.images.Open(imageName)
	if err != nil {
		return vmruntime.RunRequest{}, err
	}
	image = withRuntimeMountDirs(image)
	timing.Since(ctx, "backend.image_open", start)
	timingLog("buildBaseRequest image open took=%s image=%q", time.Since(start), imageName)
	start = time.Now()
	kernelProvider := b.kernelProvider(kernelFlavor)
	kernel, err := kernelProvider.ReadKernel()
	if err != nil {
		return vmruntime.RunRequest{}, err
	}
	timing.Since(ctx, "backend.read_kernel", start)
	timingLog("buildBaseRequest ReadKernel took=%s image=%q", time.Since(start), imageName)
	start = time.Now()
	configVars, moduleMap := runtimeKernelRequirements(kernelFlavor, image, network != nil, kernelModules)
	if NeedsAMD64Emulation(image) {
		configVars = append(configVars, "CONFIG_BINFMT_MISC")
	}
	modules, err := kernelProvider.PlanModuleLoad(configVars, moduleMap)
	if err != nil {
		return vmruntime.RunRequest{}, err
	}
	timing.Since(ctx, "backend.plan_module_load", start)
	timingLog("buildBaseRequest PlanModuleLoad took=%s image=%q modules=%d", time.Since(start), imageName, len(modules))
	start = time.Now()
	qemuX8664, err := PrepareAMD64Emulator(ctx, image, b.kernel.ExtractPackageFile)
	if err != nil {
		return vmruntime.RunRequest{}, err
	}
	timing.Since(ctx, "backend.prepare_amd64_emulator", start)
	timingLog("buildBaseRequest loadAMD64Emulator took=%s image=%q emulator_path=%q", time.Since(start), imageName, qemuX8664)
	start = time.Now()
	guestInitCache := b.guestInitCache
	if guestInitCache == "" {
		guestInitCache = filepath.Join(b.images.Root(), "_guestinit")
	}
	initBin, err := guestinit.Build(ctx, guestInitCache)
	if err != nil {
		return vmruntime.RunRequest{}, err
	}
	timing.Since(ctx, "backend.guestinit_build", start)
	timingLog("buildBaseRequest guestinit.Build took=%s image=%q init_bytes=%d", time.Since(start), imageName, len(initBin))
	return vmruntime.RunRequest{
		Kernel:            kernel,
		Init:              initBin,
		AMD64EmulatorPath: qemuX8664,
		Modules:           modules,
		Image:             image,
		InitSystem:        initSystem,
		MemoryMB:          memoryMB,
		CPUs:              cpus,
		NestedVirt:        nestedVirt,
		Dmesg:             dmesg,
		Network:           network.guestInitConfig(),
		NetDevice:         networkDeviceDarwin(network),
		UnixTime:          time.Now().Unix(),
	}, ctx.Err()
}

func blankRuntimeRootFS() imagefs.Directory {
	overlay := imagefs.NewOverlay(nil)
	for _, dir := range []string{"/dev", "/proc", "/sys", "/run", "/.ccx3", "/.ccx3/images"} {
		_ = overlay.AddDir(dir, fs.ModeDir|0o755)
	}
	_ = overlay.AddDir("/tmp", fs.ModeDir|0o1777)
	return overlay.Root()
}

func darwinLinuxMachineSpec(memoryMB uint64, cpus int, dmesg bool) machine.Spec {
	return machine.Spec{
		Guest:    "Linux",
		Arch:     "arm64",
		MemoryMB: memoryMB,
		CPUs:     cpus,
		Dmesg:    dmesg,
		Boot:     machine.BootSpec{Kind: "linux"},
		Control:  machine.ControlSpec{Kind: "vsock", Port: vmruntime.ControlPort},
	}
}

func withRuntimeMountDirs(image *oci.Image) *oci.Image {
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

func (b *runtimeBackend) buildStartRequest(ctx context.Context, req client.CreateInstanceRequest, networks ...*darwinNetworkRuntime) (vmruntime.RunRequest, error) {
	start := time.Now()
	var network *darwinNetworkRuntime
	if len(networks) > 0 {
		network = networks[0]
	}
	runReq, err := b.buildBaseRequest(ctx, req.Image, req.InitSystem, req.Kernel, req.KernelModules, req.MemoryMB, req.CPUs, req.NestedVirt, req.Dmesg, network)
	if err != nil {
		return vmruntime.RunRequest{}, err
	}
	if remoteRoot, err := workerRemoteRootFS(); err != nil {
		return vmruntime.RunRequest{}, err
	} else if remoteRoot != nil {
		runReq.RootFS = remoteRoot
		runReq.Shares = nil
	}
	shareStart := time.Now()
	if runReq.RootFS == nil {
		runReq.Shares = append(runReq.Shares, mounts.ConvertShareMounts(req.Shares)...)
	}
	timing.Since(ctx, "backend.convert_share_mounts", shareStart)
	runReq.Persistent = true
	applyStartupSnapshotOptions(&runReq, req.SnapshotDir, req.RestoreSnapshot)
	timing.Since(ctx, "backend.build_start_request", start)
	return runReq, nil
}

func (b *runtimeBackend) buildBlankStartRequest(ctx context.Context, req client.StartInstanceRequest, network *darwinNetworkRuntime) (vmruntime.RunRequest, error) {
	if strings.TrimSpace(req.RestoreSnapshot) != "" {
		return b.buildBlankRestoreRequest(ctx, req, network)
	}
	if bundle, err := workerBootBundle(); err != nil {
		return vmruntime.RunRequest{}, err
	} else if bundle != nil {
		rootFS, err := workerRemoteRootFS()
		if err != nil {
			return vmruntime.RunRequest{}, err
		}
		shares := mounts.ConvertShareMounts(req.Shares)
		if rootFS == nil {
			rootFS = virtio.NewImageFS(blankRuntimeRootFS(), "")
		} else {
			shares = nil
		}
		return vmruntime.RunRequest{
			Kernel:            append([]byte(nil), bundle.Kernel...),
			Init:              append([]byte(nil), bundle.Init...),
			AMD64EmulatorPath: bundle.AMD64EmulatorPath,
			Modules:           append([]alpine.Module(nil), bundle.Modules...),
			Image:             sidecarBundleImage(bundle),
			InitSystem:        req.InitSystem,
			RootFS:            rootFS,
			Shares:            shares,
			MemoryMB:          req.MemoryMB,
			CPUs:              req.CPUs,
			NestedVirt:        req.NestedVirt,
			Dmesg:             req.Dmesg,
			Persistent:        true,
			Network:           network.guestInitConfig(),
			NetDevice:         networkDeviceDarwin(network),
			SnapshotDir:       strings.TrimSpace(req.SnapshotDir),
			RestoreSnapshot:   strings.TrimSpace(req.RestoreSnapshot),
			UnixTime:          time.Now().Unix(),
		}, ctx.Err()
	}
	if b.kernel == nil || b.images == nil {
		return vmruntime.RunRequest{}, fmt.Errorf("runtime backend is not configured")
	}
	var image *oci.Image
	imageName := strings.TrimSpace(req.Image)
	if imageName != "" {
		var err error
		image, err = b.images.Open(imageName)
		if err != nil {
			return vmruntime.RunRequest{}, err
		}
		image = withRuntimeMountDirs(image)
	}
	kernelProvider := b.kernelProvider(req.Kernel)
	kernel, err := kernelProvider.ReadKernel()
	if err != nil {
		return vmruntime.RunRequest{}, err
	}
	configVars, moduleMap := runtimeKernelRequirements(req.Kernel, image, network != nil, req.KernelModules)
	if NeedsAMD64Emulation(image) {
		configVars = append(configVars, "CONFIG_BINFMT_MISC")
	}
	modules, err := kernelProvider.PlanModuleLoad(configVars, moduleMap)
	if err != nil {
		return vmruntime.RunRequest{}, err
	}
	qemuX8664, err := PrepareAMD64Emulator(ctx, image, b.kernel.ExtractPackageFile)
	if err != nil {
		return vmruntime.RunRequest{}, err
	}
	guestInitCache := b.guestInitCache
	if guestInitCache == "" {
		guestInitCache = filepath.Join(b.images.Root(), "_guestinit")
	}
	initBin, err := guestinit.Build(ctx, guestInitCache)
	if err != nil {
		return vmruntime.RunRequest{}, err
	}
	rootFS, err := workerRemoteRootFS()
	if err != nil {
		return vmruntime.RunRequest{}, err
	}
	shares := mounts.ConvertShareMounts(req.Shares)
	if rootFS == nil && image == nil {
		rootFS = virtio.NewImageFS(blankRuntimeRootFS(), "")
	} else if rootFS != nil {
		shares = nil
	}
	return vmruntime.RunRequest{
		Kernel:            kernel,
		Init:              initBin,
		AMD64EmulatorPath: qemuX8664,
		Modules:           modules,
		Image:             image,
		InitSystem:        req.InitSystem,
		RootFS:            rootFS,
		Shares:            shares,
		MemoryMB:          req.MemoryMB,
		CPUs:              req.CPUs,
		NestedVirt:        req.NestedVirt,
		Dmesg:             req.Dmesg,
		Persistent:        true,
		Network:           network.guestInitConfig(),
		NetDevice:         networkDeviceDarwin(network),
		SnapshotDir:       strings.TrimSpace(req.SnapshotDir),
		RestoreSnapshot:   strings.TrimSpace(req.RestoreSnapshot),
		UnixTime:          time.Now().Unix(),
	}, ctx.Err()
}

func (b *runtimeBackend) buildBlankRestoreRequest(ctx context.Context, req client.StartInstanceRequest, network *darwinNetworkRuntime) (vmruntime.RunRequest, error) {
	var image *oci.Image
	imageName := strings.TrimSpace(req.Image)
	if imageName != "" {
		if b.images == nil {
			return vmruntime.RunRequest{}, fmt.Errorf("runtime backend is not configured")
		}
		var err error
		image, err = b.images.Open(imageName)
		if err != nil {
			return vmruntime.RunRequest{}, err
		}
		image = withRuntimeMountDirs(image)
	}
	rootFS, err := workerRemoteRootFS()
	if err != nil {
		return vmruntime.RunRequest{}, err
	}
	shares := mounts.ConvertShareMounts(req.Shares)
	if rootFS == nil {
		if image != nil {
			rootFS = virtio.NewImageFS(image.RootFS, image.RootFSDir)
		} else {
			rootFS = virtio.NewImageFS(blankRuntimeRootFS(), "")
		}
	} else {
		shares = nil
	}
	return vmruntime.RunRequest{
		Image:           image,
		InitSystem:      req.InitSystem,
		RootFS:          rootFS,
		Shares:          shares,
		MemoryMB:        req.MemoryMB,
		CPUs:            req.CPUs,
		NestedVirt:      req.NestedVirt,
		Dmesg:           req.Dmesg,
		Persistent:      true,
		Network:         network.guestInitConfig(),
		NetDevice:       networkDeviceDarwin(network),
		SnapshotDir:     strings.TrimSpace(req.SnapshotDir),
		RestoreSnapshot: strings.TrimSpace(req.RestoreSnapshot),
		UnixTime:        time.Now().Unix(),
	}, ctx.Err()
}

func applyStartupSnapshotOptions(req *vmruntime.RunRequest, snapshotDir, restoreSnapshot string) {
	req.SnapshotDir = strings.TrimSpace(snapshotDir)
	req.RestoreSnapshot = strings.TrimSpace(restoreSnapshot)
}

func workerRemoteRootFS() (virtio.FSBackend, error) {
	socketPath := strings.TrimSpace(os.Getenv(sidecarFSSocketEnv))
	if socketPath == "" {
		return nil, nil
	}
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("dial coordinator fs backend: %w", err)
	}
	return virtio.NewFSRemoteBackend(conn), nil
}

func workerBootBundle() (*sidecarBootBundle, error) {
	socketPath := strings.TrimSpace(os.Getenv(sidecarBootSocketEnv))
	if socketPath == "" {
		return nil, nil
	}
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("dial coordinator boot bundle: %w", err)
	}
	defer conn.Close()
	bundle, err := readSidecarBootBundle(conn)
	if err != nil {
		return nil, fmt.Errorf("decode coordinator boot bundle: %w", err)
	}
	return bundle, nil
}

func readSidecarBootBundle(r io.Reader) (*sidecarBootBundle, error) {
	var metadata *sidecarBootBundleMetadata
	var bundle sidecarBootBundle
	var modules [][]byte
	for {
		typ, data, err := readSidecarBootTLV(r)
		if err != nil {
			return nil, err
		}
		switch typ {
		case sidecarBootTLVEnd:
			if metadata == nil {
				return nil, fmt.Errorf("boot bundle metadata missing")
			}
			if len(modules) != len(metadata.ModuleNames) {
				return nil, fmt.Errorf("boot bundle has %d module payloads for %d module names", len(modules), len(metadata.ModuleNames))
			}
			bundle.ImageName = metadata.ImageName
			bundle.Architecture = metadata.Architecture
			bundle.Config = metadata.Config
			bundle.AMD64EmulatorPath = metadata.AMD64EmulatorPath
			bundle.NeedsAMD64Emulator = metadata.NeedsAMD64Emulator
			bundle.Modules = make([]alpine.Module, 0, len(modules))
			for i, data := range modules {
				bundle.Modules = append(bundle.Modules, alpine.Module{Name: metadata.ModuleNames[i], Data: data})
			}
			return &bundle, nil
		case sidecarBootTLVMetadata:
			var meta sidecarBootBundleMetadata
			if err := json.Unmarshal(data, &meta); err != nil {
				return nil, fmt.Errorf("decode boot bundle metadata: %w", err)
			}
			metadata = &meta
		case sidecarBootTLVKernel:
			bundle.Kernel = append(bundle.Kernel[:0], data...)
		case sidecarBootTLVInit:
			bundle.Init = append(bundle.Init[:0], data...)
		case sidecarBootTLVModule:
			modules = append(modules, data)
		default:
			return nil, fmt.Errorf("unknown boot bundle TLV type %d", typ)
		}
	}
}

func readSidecarBootTLV(r io.Reader) (uint16, []byte, error) {
	var header [10]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return 0, nil, err
	}
	typ := binary.BigEndian.Uint16(header[:2])
	size := binary.BigEndian.Uint64(header[2:])
	if size > 512*1024*1024 {
		return 0, nil, fmt.Errorf("boot bundle TLV type %d too large: %d bytes", typ, size)
	}
	if size == 0 {
		return typ, nil, nil
	}
	data := make([]byte, int(size))
	if _, err := io.ReadFull(r, data); err != nil {
		return 0, nil, err
	}
	return typ, data, nil
}

func sidecarBundleImage(bundle *sidecarBootBundle) *oci.Image {
	if bundle == nil || strings.TrimSpace(bundle.ImageName) == "" {
		return nil
	}
	return &oci.Image{
		Name:         bundle.ImageName,
		Architecture: bundle.Architecture,
		Config:       bundle.Config,
	}
}

func (b *runtimeBackend) buildRunRequest(ctx context.Context, req client.RunRequest, network *darwinNetworkRuntime) (vmruntime.RunRequest, error) {
	runReq, err := b.buildBaseRequest(ctx, req.Image, req.InitSystem, req.Kernel, req.KernelModules, req.MemoryMB, req.CPUs, req.NestedVirt, req.Dmesg, network)
	if err != nil {
		return vmruntime.RunRequest{}, err
	}
	runReq.Shares = append(runReq.Shares, mounts.ConvertShareMounts(req.Shares)...)
	runReq.Command = append([]string(nil), req.Command...)
	runReq.Env = append([]string(nil), req.Env...)
	runReq.WorkDir = req.WorkDir
	runReq.User = req.User
	return runReq, nil
}

func mergeRuntimeEnv(base []string, extra []string) []string {
	if len(extra) == 0 {
		return append([]string(nil), base...)
	}
	index := map[string]int{}
	out := make([]string, 0, len(base)+len(extra))
	for _, kv := range base {
		key, _, ok := strings.Cut(kv, "=")
		if !ok || key == "" {
			continue
		}
		index[key] = len(out)
		out = append(out, kv)
	}
	for _, kv := range extra {
		key, _, ok := strings.Cut(kv, "=")
		if !ok || key == "" {
			continue
		}
		if idx, ok := index[key]; ok {
			out[idx] = kv
			continue
		}
		index[key] = len(out)
		out = append(out, kv)
	}
	return out
}
