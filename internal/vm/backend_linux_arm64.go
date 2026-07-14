//go:build linux && arm64

package vm

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"time"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/arm64vm"
	"j5.nz/cc/internal/guestinit"
	"j5.nz/cc/internal/hv/kvm"
	"j5.nz/cc/internal/imagefs"
	"j5.nz/cc/internal/kernel/alpine"
	managedguest "j5.nz/cc/internal/managed/guest"
	"j5.nz/cc/internal/oci"
	"j5.nz/cc/internal/timing"
	"j5.nz/cc/internal/virtio"
	"j5.nz/cc/internal/vm/execplan"
	kvmhost "j5.nz/cc/internal/vm/host/kvm"
	"j5.nz/cc/internal/vm/mounts"
	"j5.nz/cc/internal/vmruntime"
)

const linuxInitReadyMarker = vmruntime.InstanceReadyMarker

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

func (b *runtimeBackend) StartBlank(ctx context.Context, req client.StartInstanceRequest) (Instance, error) {
	return b.StartBlankStream(ctx, req, nil)
}

func (b *runtimeBackend) StartStream(ctx context.Context, req client.CreateInstanceRequest, onEvent func(client.BootEvent) error) (Instance, error) {
	totalStart := time.Now()
	defer func() { timing.Since(ctx, "startup.total_ready", totalStart) }()
	if inst, ok, err := b.startBuiltinGuestStream(ctx, req, onEvent); ok || err != nil {
		return inst, err
	}
	if b == nil || b.kernel == nil || b.images == nil {
		return nil, fmt.Errorf("runtime backend is not configured")
	}
	if req.CPUs > 1 {
		return nil, fmt.Errorf("linux arm64 runtime currently supports only 1 CPU")
	}
	stageStart := time.Now()
	kernel, err := readRuntimeKernel(b.kernel, req.Kernel)
	timing.Since(ctx, "startup.kernel_read", stageStart)
	if err != nil {
		return nil, err
	}
	stageStart = time.Now()
	image, err := b.images.Open(req.Image)
	timing.Since(ctx, "startup.image_open", stageStart)
	if err != nil {
		return nil, err
	}
	image = withLinuxRuntimeMountDirs(image)
	stageStart = time.Now()
	modules, err := planRuntimeKernelModules(b.kernel, req.Kernel, linuxRuntimeConfigVars(image, req.KernelModules...), linuxRuntimeModuleMap())
	timing.Since(ctx, "startup.module_plan", stageStart)
	if err != nil {
		return nil, err
	}
	stageStart = time.Now()
	qemuX8664, err := PrepareAMD64Emulator(ctx, image, b.kernel.ExtractPackageFile)
	timing.Since(ctx, "startup.emulator_prepare", stageStart)
	if err != nil {
		return nil, err
	}
	stageStart = time.Now()
	fsdevs, rootFS, err := arm64vm.BuildFSDevices(vmruntime.RunRequest{
		Image:             image,
		AMD64EmulatorPath: qemuX8664,
		Shares:            mounts.ConvertShareMounts(req.Shares),
	}, nil)
	timing.Since(ctx, "startup.fs_devices", stageStart)
	if err != nil {
		return nil, err
	}
	stageStart = time.Now()
	initBin, err := guestinit.Build(ctx, b.guestInitCache)
	timing.Since(ctx, "startup.guest_init", stageStart)
	if err != nil {
		return nil, fmt.Errorf("build guest init: %w", err)
	}
	workDir := image.Config.WorkingDir
	if workDir == "" {
		workDir = "/"
	}
	initCfg := linuxGuestInitConfig(modules, true)
	initCfg.RootFSTag = vmruntime.RootFSTag
	if qemuX8664 != "" {
		initCfg.EmulatorTag = vmruntime.EmulatorTag
	}
	initCfg.Env = vmruntime.WithDefaultEnv(image.Config.Env)
	initCfg.WorkDir = workDir
	if strings.TrimSpace(req.SnapshotDir) != "" {
		initCfg.SnapshotMMIOBase = arm64vm.SnapshotBase
	}
	stageStart = time.Now()
	initrd, err := vmruntime.BuildInitramfs(initBin, modules, initCfg)
	timing.Since(ctx, "startup.initramfs", stageStart)
	if err != nil {
		return nil, fmt.Errorf("build initramfs: %w", err)
	}
	stageStart = time.Now()
	session, err := kvm.StartManagedSessionWithOptions(ctx, kernel, initrd, req.MemoryMB, req.Dmesg, fsdevs, kvm.ManagedSessionOptions{
		SnapshotDir: strings.TrimSpace(req.SnapshotDir), RestoreSnapshot: strings.TrimSpace(req.RestoreSnapshot),
	}, onEvent)
	timing.Since(ctx, "startup.kvm_to_ready", stageStart)
	if err != nil {
		return nil, err
	}
	return &linuxInstance{
		managedInstance: &managedInstance{
			osName:         "Linux",
			session:        session,
			root:           image.RootFS,
			baseEnv:        vmruntime.WithDefaultEnv(image.Config.Env),
			workDir:        workDir,
			caps:           linuxARM64Capabilities(),
			env:            linuxEffectiveExecEnv,
			user:           linuxResolveExecUser,
			missingRootErr: "running instance does not have a default image root filesystem",
		},
		image:  image,
		rootFS: rootFS,
		fsdevs: fsdevs,
		dmesg:  req.Dmesg,
	}, nil
}

func (b *runtimeBackend) StartBlankStream(ctx context.Context, req client.StartInstanceRequest, onEvent func(client.BootEvent) error) (Instance, error) {
	if inst, ok, err := b.startBuiltinGuestBlankStream(ctx, req, onEvent); ok || err != nil {
		return inst, err
	}
	if strings.TrimSpace(req.Image) != "" {
		return b.StartStream(ctx, client.CreateInstanceRequest{
			ID:              req.ID,
			Image:           req.Image,
			InitSystem:      req.InitSystem,
			Kernel:          req.Kernel,
			Network:         req.Network,
			KernelModules:   append([]string(nil), req.KernelModules...),
			MemoryMB:        req.MemoryMB,
			CPUs:            req.CPUs,
			NestedVirt:      req.NestedVirt,
			Dmesg:           req.Dmesg,
			TimeoutSeconds:  req.TimeoutSeconds,
			SnapshotDir:     req.SnapshotDir,
			RestoreSnapshot: req.RestoreSnapshot,
		}, onEvent)
	}
	if b == nil || b.kernel == nil || b.images == nil {
		return nil, fmt.Errorf("runtime backend is not configured")
	}
	if req.CPUs > 1 {
		return nil, fmt.Errorf("linux arm64 runtime currently supports only 1 CPU")
	}
	kernel, err := readRuntimeKernel(b.kernel, req.Kernel)
	if err != nil {
		return nil, err
	}
	var image *oci.Image
	imageName := strings.TrimSpace(req.Image)
	if imageName != "" {
		image, err = b.images.Open(imageName)
		if err != nil {
			return nil, err
		}
		image = withLinuxRuntimeMountDirs(image)
	}
	modules, err := planRuntimeKernelModules(b.kernel, req.Kernel, linuxRuntimeConfigVars(image, req.KernelModules...), linuxRuntimeModuleMap())
	if err != nil {
		return nil, err
	}
	qemuX8664, err := PrepareAMD64Emulator(ctx, image, b.kernel.ExtractPackageFile)
	if err != nil {
		return nil, err
	}
	rootFSBackend := virtio.NewImageFS(blankLinuxRuntimeRootFS(), "")
	fsdevs, rootFS, err := arm64vm.BuildFSDevices(vmruntime.RunRequest{
		RootFS:            rootFSBackend,
		AMD64EmulatorPath: qemuX8664,
	}, nil)
	if err != nil {
		return nil, err
	}
	initBin, err := guestinit.Build(ctx, b.guestInitCache)
	if err != nil {
		return nil, fmt.Errorf("build guest init: %w", err)
	}
	initCfg := linuxGuestInitConfig(modules, true)
	initCfg.RootFSTag = vmruntime.RootFSTag
	if qemuX8664 != "" {
		initCfg.EmulatorTag = vmruntime.EmulatorTag
	}
	initCfg.Env = vmruntime.WithDefaultEnv(nil)
	initCfg.WorkDir = "/"
	if strings.TrimSpace(req.SnapshotDir) != "" {
		initCfg.SnapshotMMIOBase = arm64vm.SnapshotBase
	}
	initrd, err := vmruntime.BuildInitramfs(initBin, modules, initCfg)
	if err != nil {
		return nil, fmt.Errorf("build initramfs: %w", err)
	}
	session, err := kvm.StartManagedSessionWithOptions(ctx, kernel, initrd, req.MemoryMB, req.Dmesg, fsdevs, kvm.ManagedSessionOptions{
		SnapshotDir: strings.TrimSpace(req.SnapshotDir), RestoreSnapshot: strings.TrimSpace(req.RestoreSnapshot),
	}, onEvent)
	if err != nil {
		return nil, err
	}
	inst := &linuxInstance{
		managedInstance: &managedInstance{
			osName:         "Linux",
			session:        session,
			baseEnv:        vmruntime.WithDefaultEnv(nil),
			workDir:        "/",
			caps:           linuxARM64Capabilities(),
			env:            linuxEffectiveExecEnv,
			user:           linuxResolveExecUser,
			missingRootErr: "running instance does not have a default image root filesystem",
		},
		image:  image,
		rootFS: rootFS,
		fsdevs: fsdevs,
		dmesg:  req.Dmesg,
	}
	if image != nil {
		mountPath := kvmhost.ImageMountPath(imageName)
		if err := inst.AddImage(ctx, mountPath, image); err != nil {
			_ = session.Close()
			return nil, err
		}
		inst.root = image.RootFS
		inst.defaultRootDir = mountPath
		inst.baseEnv = vmruntime.WithDefaultEnv(image.Config.Env)
		if image.Config.WorkingDir != "" {
			inst.workDir = image.Config.WorkingDir
		}
	}
	return inst, nil
}

func (b *runtimeBackend) Run(ctx context.Context, req client.RunRequest) (client.ExecResponse, error) {
	if resp, ok, err := b.runBuiltinGuest(ctx, req); ok || err != nil {
		return resp, err
	}
	if b == nil || b.kernel == nil {
		return client.ExecResponse{}, fmt.Errorf("runtime backend is not configured")
	}
	if req.CPUs > 1 {
		return client.ExecResponse{}, fmt.Errorf("linux arm64 runtime currently supports only 1 CPU")
	}

	kernel, err := readRuntimeKernel(b.kernel, req.Kernel)
	if err != nil {
		return client.ExecResponse{}, err
	}

	var (
		modules   []alpine.Module
		fsdevs    []*virtio.FS
		image     *oci.Image
		env       []string
		workDir   string
		qemuX8664 string
	)
	if req.Image != "" && b.images != nil {
		image, err = b.images.Open(req.Image)
		if err != nil {
			return client.ExecResponse{}, err
		}
		image = withLinuxRuntimeMountDirs(image)
		modules, err = planRuntimeKernelModules(
			b.kernel,
			req.Kernel,
			linuxRuntimeConfigVars(image, req.KernelModules...),
			linuxRuntimeModuleMap(),
		)
		if err != nil {
			return client.ExecResponse{}, err
		}
		qemuX8664, err = PrepareAMD64Emulator(ctx, image, b.kernel.ExtractPackageFile)
		if err != nil {
			return client.ExecResponse{}, err
		}
		if kvmhost.RootFSImageEnabled() {
			if len(req.Shares) != 0 {
				return client.ExecResponse{}, fmt.Errorf("rootfs image mode does not support runtime shares yet")
			}
			if qemuX8664 != "" {
				devs, _, err := arm64vm.BuildFSDevices(vmruntime.RunRequest{
					RootFS:            kvmhost.RuntimeImageFS(image),
					AMD64EmulatorPath: qemuX8664,
				}, nil)
				if err != nil {
					return client.ExecResponse{}, err
				}
				fsdevs = devs
			}
		} else {
			devs, _, err := arm64vm.BuildFSDevices(vmruntime.RunRequest{
				RootFS:            kvmhost.RuntimeImageFS(image),
				AMD64EmulatorPath: qemuX8664,
				Shares:            mounts.ConvertShareMounts(req.Shares),
			}, nil)
			if err != nil {
				return client.ExecResponse{}, err
			}
			fsdevs = devs
		}
		env = vmruntime.WithDefaultEnv(vmruntime.MergeEnv(image.Config.Env, req.Env))
		workDir = req.WorkDir
		if workDir == "" {
			workDir = image.Config.WorkingDir
		}
		if workDir == "" {
			workDir = "/"
		}
	}

	initBin, err := guestinit.Build(ctx, b.guestInitCache)
	if err != nil {
		return client.ExecResponse{}, fmt.Errorf("build guest init: %w", err)
	}
	initCfg := linuxGuestInitConfig(modules, len(req.Command) != 0)
	if image != nil && kvmhost.RootFSImageEnabled() {
		rootImageType, err := kvmhost.RootFSImageType()
		if err != nil {
			return client.ExecResponse{}, err
		}
		rootImage, err := kvmhost.BuildRootFSImage(ctx, image.RootFS, rootImageType)
		if err != nil {
			return client.ExecResponse{}, err
		}
		initCfg.RootFSImage = rootImage
		initCfg.RootFSImagePath = rootImageType.InitramfsPath()
		initCfg.RootFSImageType = rootImageType.String()
	} else if len(fsdevs) != 0 {
		initCfg.RootFSTag = vmruntime.RootFSTag
	}
	if qemuX8664 != "" {
		initCfg.EmulatorTag = vmruntime.EmulatorTag
	}
	initrd, err := vmruntime.BuildInitramfs(initBin, modules, initCfg)
	if err != nil {
		return client.ExecResponse{}, fmt.Errorf("build initramfs: %w", err)
	}

	if len(req.Command) != 0 {
		if image == nil {
			return client.ExecResponse{}, fmt.Errorf("linux arm64 runtime command execution requires an image store and image")
		}
		user, err := linuxResolveExecUser(req.User)
		if err != nil {
			return client.ExecResponse{}, err
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
			User:      user,
			Stdin:     append([]byte(nil), req.Stdin...),
			TTY:       req.TTY,
			ControlFD: req.ControlFD,
			Cols:      req.Cols,
			Rows:      req.Rows,
		}
		resp, serial, err := kvm.RunManagedExecWithFS(ctx, kernel, initrd, req.MemoryMB, req.Dmesg, fsdevs, execReq)
		if err != nil && resp.Output == "" {
			resp.Output = serial
		}
		return resp, err
	}

	var output string
	if len(fsdevs) != 0 {
		output, err = kvm.BootInitramfsToMarkerWithFS(ctx, kernel, initrd, req.MemoryMB, true, linuxInitReadyMarker, fsdevs)
	} else {
		output, err = kvm.BootInitramfsToMarker(ctx, kernel, initrd, req.MemoryMB, true, linuxInitReadyMarker)
	}
	if err != nil {
		return client.ExecResponse{Output: output}, err
	}
	return client.ExecResponse{
		ExitCode: 0,
		Output:   output,
	}, nil
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
		shares := req.Shares
		if session, ok := inst.(*linuxInstance); ok && session.defaultRootDir != "" {
			shares = mounts.RebaseRuntimeShares(session.defaultRootDir, shares)
		}
		if err := mounts.AddRuntimeShares(ctx, inst, shares); err != nil {
			return client.ExecResponse{}, err
		}
		return inst.Exec(ctx, runExecRequest(req))
	}

	if err := execplan.CheckAlternateImageExec(inst); err != nil {
		return client.ExecResponse{}, err
	}
	session, ok := inst.(*linuxInstance)
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
	image = withLinuxRuntimeMountDirs(image)
	mountPath := kvmhost.ImageMountPath(targetImage)
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
		shares := req.Shares
		if session, ok := inst.(*linuxInstance); ok && session.defaultRootDir != "" {
			shares = mounts.RebaseRuntimeShares(session.defaultRootDir, shares)
		}
		if err := mounts.AddRuntimeShares(ctx, inst, shares); err != nil {
			return err
		}
		return inst.ExecStream(ctx, runExecRequest(req), inputs, onEvent)
	}

	if err := execplan.CheckAlternateImageExec(inst); err != nil {
		return err
	}
	session, ok := inst.(*linuxInstance)
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
	image = withLinuxRuntimeMountDirs(image)
	mountPath := kvmhost.ImageMountPath(targetImage)
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
	session, ok := inst.(*linuxInstance)
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
	image = withLinuxRuntimeMountDirs(image)
	mountPath := kvmhost.ImageMountPath(targetImage)
	if err := mounts.MountAlternateImageWithShares(ctx, inst, session, mountPath, image, nil); err != nil {
		return err
	}
	req.RootDir = rootDirWithinMount(mountPath, req.RootDir)
	return inst.ExecStream(ctx, req, inputs, onEvent)
}

type linuxInstance struct {
	*managedInstance
	image  *oci.Image
	rootFS virtio.ShareMounter
	fsdevs []*virtio.FS
	dmesg  bool
	mounts mounts.State
}

func linuxARM64Capabilities() guestCapabilities {
	caps := managedguest.LinuxProfile.Caps
	caps.PortForward = false
	return caps
}

func (i *linuxInstance) VirtioFSStats() []virtio.FSStats {
	if i == nil {
		return nil
	}
	return virtioFSStats(i.fsdevs)
}

func (i *linuxInstance) resolveExecRequest(req client.ExecRequest) (client.ExecRequest, error) {
	if i == nil {
		return client.ExecRequest{}, fmt.Errorf("instance is not running")
	}
	return i.execRequest(req)
}

func (i *linuxInstance) AddShare(ctx context.Context, share client.ShareMount) error {
	_ = ctx
	if i == nil || i.rootFS == nil {
		return mounts.AddRuntimeShareMount(nil, nil, nil, share, "shares", nil)
	}
	return i.mounts.AddShare(i.rootFS, share, "shares", func(share client.ShareMount) (virtio.ShareMount, error) {
		return mounts.BuildRuntimeDirectoryShare(share, arm64vm.BuildShareMount)
	})
}

func (i *linuxInstance) AddPortForward(ctx context.Context, forward client.PortForward) error {
	_, _ = ctx, forward
	if i == nil {
		return execplan.UnsupportedFeature("Linux", linuxARM64Capabilities(), "port forwards")
	}
	return i.managedInstance.unsupported("port forwards")
}

func (i *linuxInstance) AllowServiceProxyPort(ctx context.Context, port int) error {
	_, _ = ctx, port
	if i == nil {
		return execplan.UnsupportedFeature("Linux", linuxARM64Capabilities(), "port forwards")
	}
	return i.managedInstance.unsupported("port forwards")
}

func (i *linuxInstance) AddImage(ctx context.Context, mountPath string, image *oci.Image) error {
	_ = ctx
	if i == nil {
		return mounts.AddImageMount(nil, nil, nil, mountPath, image, nil)
	}
	return i.mounts.AddImage(i.rootFS, mountPath, image, mounts.ImageFSBackend(image))
}

func linuxGuestInitConfig(modules []alpine.Module, managedExec bool) vmruntime.GuestInitConfig {
	cfg := vmruntime.GuestInitConfig{
		Modules:           vmruntime.ModulePaths(modules),
		ReadyMarker:       linuxInitReadyMarker,
		BeginMarker:       vmruntime.CommandBeginMarker,
		OutputMarkerPref:  vmruntime.CommandOutputMarker,
		ErrorMarkerPref:   vmruntime.CommandErrorMarker,
		ControlMarkerPref: vmruntime.CommandControlMarker,
		UsageMarkerPref:   vmruntime.CommandUsageMarker,
		ExitMarkerPrefix:  vmruntime.CommandExitMarkerPref,
		UnixTime:          time.Now().Unix(),
	}
	if managedExec {
		cfg.VsockPort = vmruntime.ControlPort
	}
	return cfg
}

func linuxRuntimeConfigVars(image *oci.Image, extraModules ...string) []string {
	vars := []string{"CONFIG_VIRTIO_MMIO", "CONFIG_FUSE_FS", "CONFIG_VIRTIO_FS", "CONFIG_VSOCKETS", "CONFIG_VIRTIO_VSOCKETS", "CONFIG_HW_RANDOM", "CONFIG_HW_RANDOM_VIRTIO", "CONFIG_OVERLAY_FS"}
	if kvmhost.RootFSImageEnabled() {
		vars = append(vars, "CONFIG_BLK_DEV_LOOP")
		rootImageType, err := kvmhost.RootFSImageType()
		if err == nil {
			vars = append(vars, kvmhost.RootFSImageConfigVars(rootImageType)...)
		}
	}
	vars = append(vars, linuxRuntimeExtraConfigVars(extraModules)...)
	if NeedsAMD64Emulation(image) {
		vars = append(vars, "CONFIG_BINFMT_MISC")
	}
	return vars
}

func linuxRuntimeExtraConfigVars(names []string) []string {
	aliases := map[string]string{
		"bridge":        "CONFIG_BRIDGE",
		"br_netfilter":  "CONFIG_BRIDGE_NETFILTER",
		"veth":          "CONFIG_VETH",
		"vxlan":         "CONFIG_VXLAN",
		"nf_conntrack":  "CONFIG_NF_CONNTRACK",
		"nf_nat":        "CONFIG_NF_NAT",
		"nf_tables":     "CONFIG_NF_TABLES",
		"nft_compat":    "CONFIG_NFT_COMPAT",
		"nft_ct":        "CONFIG_NFT_CT",
		"nft_masq":      "CONFIG_NFT_MASQ",
		"nft_nat":       "CONFIG_NFT_NAT",
		"nft_chain_nat": "MODULE:nft_chain_nat",
		"x_tables":      "CONFIG_NETFILTER_XTABLES",
		"xt_addrtype":   "CONFIG_NETFILTER_XT_MATCH_ADDRTYPE",
		"xt_comment":    "CONFIG_NETFILTER_XT_MATCH_COMMENT",
		"xt_conntrack":  "CONFIG_NETFILTER_XT_MATCH_CONNTRACK",
		"xt_mark":       "CONFIG_NETFILTER_XT_TARGET_MARK",
		"xt_multiport":  "CONFIG_NETFILTER_XT_MATCH_MULTIPORT",
		"xt_masquerade": "CONFIG_NETFILTER_XT_TARGET_MASQUERADE",
		"xt_nat":        "CONFIG_NETFILTER_XT_NAT",
		"xt_tcpudp":     "MODULE:xt_tcpudp",
		"ipt_reject":    "CONFIG_IP_NF_TARGET_REJECT",
		"ip6t_reject":   "CONFIG_IP6_NF_TARGET_REJECT",
	}
	out := make([]string, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if strings.HasPrefix(name, "CONFIG_") {
			out = append(out, name)
			continue
		}
		if configVar, ok := aliases[strings.ToLower(name)]; ok {
			out = append(out, configVar)
			continue
		}
		out = append(out, "CONFIG_"+strings.ToUpper(strings.NewReplacer("-", "_", ".", "_").Replace(name)))
	}
	return out
}

func linuxRuntimeModuleMap() map[string]string {
	return map[string]string{
		"CONFIG_VIRTIO_MMIO":                    "kernel/drivers/virtio/virtio_mmio.ko.gz",
		"CONFIG_FUSE_FS":                        "kernel/fs/fuse/fuse.ko.gz",
		"CONFIG_VIRTIO_FS":                      "kernel/fs/fuse/virtiofs.ko.gz",
		"CONFIG_VSOCKETS":                       "kernel/net/vmw_vsock/vsock.ko.gz",
		"CONFIG_VIRTIO_VSOCKETS":                "kernel/net/vmw_vsock/vmw_vsock_virtio_transport.ko.gz",
		"CONFIG_HW_RANDOM":                      "kernel/drivers/char/hw_random/rng-core.ko.gz",
		"CONFIG_HW_RANDOM_VIRTIO":               "kernel/drivers/char/hw_random/virtio-rng.ko.gz",
		"CONFIG_BINFMT_MISC":                    "kernel/fs/binfmt_misc.ko.gz",
		"CONFIG_OVERLAY_FS":                     "kernel/fs/overlayfs/overlay.ko.gz",
		"CONFIG_BRIDGE":                         "kernel/net/bridge/bridge.ko.gz",
		"CONFIG_BRIDGE_NETFILTER":               "kernel/net/bridge/br_netfilter.ko.gz",
		"CONFIG_VETH":                           "kernel/drivers/net/veth.ko.gz",
		"CONFIG_VXLAN":                          "kernel/drivers/net/vxlan/vxlan.ko.gz",
		"CONFIG_NF_CONNTRACK":                   "kernel/net/netfilter/nf_conntrack.ko.gz",
		"CONFIG_NF_NAT":                         "kernel/net/netfilter/nf_nat.ko.gz",
		"CONFIG_NF_TABLES":                      "kernel/net/netfilter/nf_tables.ko.gz",
		"CONFIG_NFT_COMPAT":                     "kernel/net/netfilter/nft_compat.ko.gz",
		"CONFIG_NFT_CT":                         "kernel/net/netfilter/nft_ct.ko.gz",
		"CONFIG_NFT_MASQ":                       "kernel/net/netfilter/nft_masq.ko.gz",
		"CONFIG_NFT_NAT":                        "kernel/net/netfilter/nft_nat.ko.gz",
		"MODULE:nft_chain_nat":                  "kernel/net/netfilter/nft_chain_nat.ko.gz",
		"CONFIG_NETFILTER_XTABLES":              "kernel/net/netfilter/x_tables.ko.gz",
		"CONFIG_NETFILTER_XT_MATCH_ADDRTYPE":    "kernel/net/netfilter/xt_addrtype.ko.gz",
		"CONFIG_NETFILTER_XT_MATCH_COMMENT":     "kernel/net/netfilter/xt_comment.ko.gz",
		"CONFIG_NETFILTER_XT_MATCH_CONNTRACK":   "kernel/net/netfilter/xt_conntrack.ko.gz",
		"CONFIG_NETFILTER_XT_TARGET_MARK":       "kernel/net/netfilter/xt_mark.ko.gz",
		"CONFIG_NETFILTER_XT_MATCH_MULTIPORT":   "kernel/net/netfilter/xt_multiport.ko.gz",
		"CONFIG_NETFILTER_XT_TARGET_MASQUERADE": "kernel/net/netfilter/xt_MASQUERADE.ko.gz",
		"CONFIG_NETFILTER_XT_NAT":               "kernel/net/netfilter/xt_nat.ko.gz",
		"MODULE:xt_tcpudp":                      "kernel/net/netfilter/xt_tcpudp.ko.gz",
		"CONFIG_IP_NF_TARGET_REJECT":            "kernel/net/ipv4/netfilter/ipt_REJECT.ko.gz",
		"CONFIG_IP6_NF_TARGET_REJECT":           "kernel/net/ipv6/netfilter/ip6t_REJECT.ko.gz",
		"CONFIG_EXT4_FS":                        "kernel/fs/ext4/ext4.ko.gz",
		"CONFIG_BLK_DEV_LOOP":                   "kernel/drivers/block/loop.ko.gz",
		"CONFIG_FAT_FS":                         "kernel/fs/fat/fat.ko.gz",
		"CONFIG_VFAT_FS":                        "kernel/fs/fat/vfat.ko.gz",
		"CONFIG_NLS_CODEPAGE_437":               "kernel/fs/nls/nls_cp437.ko.gz",
		"CONFIG_NLS_ISO8859_1":                  "kernel/fs/nls/nls_iso8859-1.ko.gz",
	}
}

func withLinuxRuntimeMountDirs(image *oci.Image) *oci.Image {
	if image == nil || image.RootFS == nil {
		return image
	}
	overlay := imagefs.NewOverlay(image.RootFS)
	for _, dir := range []string{"/dev", "/proc", "/sys", "/run", "/etc"} {
		_ = overlay.AddDir(dir, fs.ModeDir|0o755)
	}
	_ = overlay.AddDir("/tmp", fs.ModeDir|0o1777)
	kvmhost.AddRuntimeIdentityFiles(overlay, os.Getuid(), os.Getgid())
	kvmhost.AddRuntimeHostnameFiles(overlay)
	cloned := *image
	cloned.RootFS = overlay.Root()
	return &cloned
}

func blankLinuxRuntimeRootFS() imagefs.Directory {
	overlay := imagefs.NewOverlay(nil)
	for _, dir := range []string{"/dev", "/proc", "/sys", "/run", "/.ccx3", "/.ccx3/images"} {
		_ = overlay.AddDir(dir, fs.ModeDir|0o755)
	}
	_ = overlay.AddDir("/tmp", fs.ModeDir|0o1777)
	kvmhost.AddRuntimeIdentityFiles(overlay, os.Getuid(), os.Getgid())
	kvmhost.AddRuntimeHostnameFiles(overlay)
	return overlay.Root()
}

func linuxEffectiveExecEnv(base, overrides []string, replace bool) []string {
	if replace {
		return vmruntime.WithDefaultEnv(overrides)
	}
	return vmruntime.WithDefaultEnv(vmruntime.MergeEnv(base, overrides))
}

func linuxResolveExecUser(user string) (string, error) {
	return kvmhost.ResolveRuntimeExecUser("linux arm64", user)
}
