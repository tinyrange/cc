//go:build linux && amd64

package vm

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	osuser "os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/amd64vm"
	"j5.nz/cc/internal/guestinit"
	"j5.nz/cc/internal/hv/kvm"
	"j5.nz/cc/internal/imagefs"
	"j5.nz/cc/internal/kernel/alpine"
	"j5.nz/cc/internal/oci"
	"j5.nz/cc/internal/virtio"
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

func (b *runtimeBackend) StartStream(ctx context.Context, req client.CreateInstanceRequest, onEvent func(client.BootEvent) error) (Instance, error) {
	if b == nil || b.kernel == nil || b.images == nil {
		return nil, fmt.Errorf("runtime backend is not configured")
	}
	kernel, err := b.kernel.ReadKernel()
	if err != nil {
		return nil, err
	}
	image, err := b.images.Open(req.Image)
	if err != nil {
		return nil, err
	}
	if err := ensureLinuxAMD64Image(image); err != nil {
		return nil, err
	}
	image = withLinuxRuntimeMountDirs(image)
	modules, err := b.kernel.PlanModuleLoad(linuxRuntimeConfigVars(image, req.KernelModules...), linuxRuntimeModuleMap())
	if err != nil {
		return nil, err
	}
	network, err := newLinuxAMD64NetworkRuntime(req.ID, req.Network)
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
		RootFS: linuxRuntimeImageFS(image),
		Shares: convertLinuxShareMounts(req.Shares),
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
	initCfg := linuxGuestInitConfig(modules, true, req.Network, network)
	initCfg.RootFSTag = vmruntime.RootFSTag
	initCfg.Env = vmruntime.WithDefaultEnv(image.Config.Env)
	initCfg.WorkDir = workDir
	initrd, err := vmruntime.BuildInitramfs(initBin, modules, initCfg)
	if err != nil {
		return nil, fmt.Errorf("build initramfs: %w", err)
	}
	session, err := kvm.StartManagedSessionWithNet(ctx, kernel, initrd, req.MemoryMB, req.CPUs, req.Dmesg, fsdevs, networkDevice(network), onEvent)
	if err != nil {
		return nil, err
	}
	return &linuxInstance{
		session: session,
		image:   image,
		baseEnv: vmruntime.WithDefaultEnv(image.Config.Env),
		workDir: workDir,
		rootFS:  rootFS,
		fsdevs:  fsdevs,
		network: network,
		dmesg:   req.Dmesg,
	}, nil
}

func (b *runtimeBackend) StartBlank(ctx context.Context, req client.StartInstanceRequest) (Instance, error) {
	return b.StartBlankStream(ctx, req, nil)
}

func (b *runtimeBackend) StartBlankStream(ctx context.Context, req client.StartInstanceRequest, onEvent func(client.BootEvent) error) (Instance, error) {
	if b == nil || b.kernel == nil || b.images == nil {
		return nil, fmt.Errorf("runtime backend is not configured")
	}
	kernel, err := b.kernel.ReadKernel()
	if err != nil {
		return nil, err
	}
	modules, err := b.kernel.PlanModuleLoad(linuxRuntimeConfigVars(nil, req.KernelModules...), linuxRuntimeModuleMap())
	if err != nil {
		return nil, err
	}
	network, err := newLinuxAMD64NetworkRuntime(req.ID, req.Network)
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
	rootFSBackend := virtio.NewImageFS(blankLinuxRuntimeRootFS(), "")
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
	initCfg := linuxGuestInitConfig(modules, true, req.Network, network)
	initCfg.RootFSTag = vmruntime.RootFSTag
	initCfg.Env = vmruntime.WithDefaultEnv(nil)
	initCfg.WorkDir = "/"
	initrd, err := vmruntime.BuildInitramfs(initBin, modules, initCfg)
	if err != nil {
		return nil, fmt.Errorf("build initramfs: %w", err)
	}
	session, err := kvm.StartManagedSessionWithNet(ctx, kernel, initrd, req.MemoryMB, req.CPUs, req.Dmesg, fsdevs, networkDevice(network), onEvent)
	if err != nil {
		return nil, err
	}
	return &linuxInstance{
		session: session,
		baseEnv: vmruntime.WithDefaultEnv(nil),
		workDir: "/",
		rootFS:  rootFS,
		fsdevs:  fsdevs,
		network: network,
		dmesg:   req.Dmesg,
	}, nil
}

func (b *runtimeBackend) Run(ctx context.Context, req client.RunRequest) (client.ExecResponse, error) {
	if b == nil || b.kernel == nil {
		return client.ExecResponse{}, fmt.Errorf("runtime backend is not configured")
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
			return client.ExecResponse{}, fmt.Errorf("linux amd64 runtime command execution requires an image store and image")
		}
		image, err = b.images.Open(req.Image)
		if err != nil {
			return client.ExecResponse{}, err
		}
		if err := ensureLinuxAMD64Image(image); err != nil {
			return client.ExecResponse{}, err
		}
		image = withLinuxRuntimeMountDirs(image)
		modules, err = b.kernel.PlanModuleLoad(
			linuxRuntimeConfigVars(image, req.KernelModules...),
			linuxRuntimeModuleMap(),
		)
		if err != nil {
			return client.ExecResponse{}, err
		}
		devs, _, err := amd64vm.BuildFSDevices(vmruntime.RunRequest{
			RootFS: linuxRuntimeImageFS(image),
			Shares: convertLinuxShareMounts(req.Shares),
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
	network, err := newLinuxAMD64NetworkRuntime(req.ID, req.Network)
	if err != nil {
		return client.ExecResponse{}, err
	}
	if network != nil {
		defer network.Close()
	}
	initCfg := linuxGuestInitConfig(modules, len(req.Command) != 0, req.Network, network)
	if len(fsdevs) != 0 {
		initCfg.RootFSTag = vmruntime.RootFSTag
	}
	initrd, err := vmruntime.BuildInitramfs(initBin, modules, initCfg)
	if err != nil {
		return client.ExecResponse{}, fmt.Errorf("build initramfs: %w", err)
	}

	if len(req.Command) != 0 {
		if image == nil {
			return client.ExecResponse{}, fmt.Errorf("linux amd64 runtime command execution requires an image store and image")
		}
		user := strings.TrimSpace(req.User)
		resolvedUser, err := linuxResolveExecUser(user)
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
			Command: command,
			Env:     env,
			WorkDir: workDir,
			User:    resolvedUser,
			Stdin:   append([]byte(nil), req.Stdin...),
			TTY:     req.TTY,
			Cols:    req.Cols,
			Rows:    req.Rows,
		}
		resp, serial, err := kvm.RunManagedExecWithFSAndNet(ctx, kernel, initrd, req.MemoryMB, req.CPUs, req.Dmesg, fsdevs, networkDevice(network), execReq)
		if err != nil && resp.Output == "" {
			resp.Output = serial
		}
		return resp, err
	}

	var output string
	if len(fsdevs) != 0 {
		output, err = kvm.BootInitramfsToMarkerWithFSAndNet(ctx, kernel, initrd, req.MemoryMB, true, linuxInitReadyMarker, fsdevs, networkDevice(network))
	} else {
		output, err = kvm.BootInitramfsToMarker(ctx, kernel, initrd, req.MemoryMB, true, linuxInitReadyMarker)
	}
	if err != nil {
		return client.ExecResponse{Output: output}, err
	}
	return client.ExecResponse{ExitCode: 0, Output: output}, nil
}

func (b *runtimeBackend) RunStream(ctx context.Context, req client.RunRequest, inputs <-chan client.ExecInput, onEvent func(client.ExecEvent) error) error {
	inst, err := b.StartStream(ctx, client.CreateInstanceRequest{
		ID:       req.ID,
		Image:    req.Image,
		Shares:   append([]client.ShareMount(nil), req.Shares...),
		Network:  req.Network,
		MemoryMB: req.MemoryMB,
		CPUs:     req.CPUs,
		Dmesg:    req.Dmesg,
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
		if err := addRuntimeShares(ctx, inst, req.Shares); err != nil {
			return client.ExecResponse{}, err
		}
		return inst.Exec(ctx, client.ExecRequest{
			Command:    append([]string(nil), req.Command...),
			Env:        append([]string(nil), req.Env...),
			RootDir:    req.RootDir,
			ReplaceEnv: req.ReplaceEnv,
			WorkDir:    req.WorkDir,
			User:       req.User,
			Stdin:      append([]byte(nil), req.Stdin...),
			TTY:        req.TTY,
			Cols:       req.Cols,
			Rows:       req.Rows,
		})
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
	if err := ensureLinuxAMD64Image(image); err != nil {
		return client.ExecResponse{}, err
	}
	image = withLinuxRuntimeMountDirs(image)
	mountPath := linuxImageMountPath(targetImage)
	if err := session.AddImage(ctx, mountPath, image); err != nil {
		return client.ExecResponse{}, err
	}
	if err := addRuntimeShares(ctx, inst, rebaseRuntimeShares(mountPath, req.Shares)); err != nil {
		return client.ExecResponse{}, err
	}

	env := vmruntime.WithDefaultEnv(vmruntime.MergeEnv(image.Config.Env, req.Env))
	command, err := imagefs.ResolveCommand(image.RootFS, req.Command, env)
	if err != nil {
		return client.ExecResponse{}, err
	}
	workDir := req.WorkDir
	if workDir == "" {
		workDir = image.Config.WorkingDir
	}
	if workDir == "" {
		workDir = "/"
	}

	return inst.Exec(ctx, client.ExecRequest{
		Command:     command,
		Env:         env,
		RootDir:     mountPath,
		ReplaceEnv:  true,
		SkipResolve: true,
		WorkDir:     workDir,
		User:        req.User,
		Stdin:       append([]byte(nil), req.Stdin...),
		TTY:         req.TTY,
		Cols:        req.Cols,
		Rows:        req.Rows,
	})
}

func (b *runtimeBackend) RunInInstanceStream(ctx context.Context, inst Instance, runningImage string, req client.RunRequest, inputs <-chan client.ExecInput, onEvent func(client.ExecEvent) error) error {
	targetImage := strings.TrimSpace(req.Image)
	if targetImage == "" || targetImage == runningImage {
		if err := addRuntimeShares(ctx, inst, req.Shares); err != nil {
			return err
		}
		return inst.ExecStream(ctx, runExecRequest(req), inputs, onEvent)
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
	if err := ensureLinuxAMD64Image(image); err != nil {
		return err
	}
	image = withLinuxRuntimeMountDirs(image)
	mountPath := linuxImageMountPath(targetImage)
	if err := session.AddImage(ctx, mountPath, image); err != nil {
		return err
	}
	if err := addRuntimeShares(ctx, inst, rebaseRuntimeShares(mountPath, req.Shares)); err != nil {
		return err
	}

	env := vmruntime.WithDefaultEnv(vmruntime.MergeEnv(image.Config.Env, req.Env))
	command, err := imagefs.ResolveCommand(image.RootFS, req.Command, env)
	if err != nil {
		return err
	}
	workDir := req.WorkDir
	if workDir == "" {
		workDir = image.Config.WorkingDir
	}
	if workDir == "" {
		workDir = "/"
	}

	return inst.ExecStream(ctx, client.ExecRequest{
		Command:     command,
		Env:         env,
		RootDir:     mountPath,
		ReplaceEnv:  true,
		SkipResolve: true,
		WorkDir:     workDir,
		User:        req.User,
		Stdin:       append([]byte(nil), req.Stdin...),
		TTY:         req.TTY,
		Cols:        req.Cols,
		Rows:        req.Rows,
	}, inputs, onEvent)
}

func linuxAMD64NotImplemented() error {
	return fmt.Errorf("linux/amd64 VM runtime is not implemented yet")
}

func ensureLinuxAMD64Image(image *oci.Image) error {
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
	return fmt.Errorf("linux/amd64 runtime supports only amd64 images; %s is %s", name, arch)
}

type linuxInstance struct {
	session     *kvm.ManagedSession
	image       *oci.Image
	baseEnv     []string
	workDir     string
	rootFS      virtio.ShareMounter
	fsdevs      []*virtio.FS
	network     *linuxNetworkRuntime
	dmesg       bool
	shareMu     sync.Mutex
	shares      map[string]client.ShareMount
	imageMounts map[string]string
}

func (i *linuxInstance) VirtioFSStats() []virtio.FSStats {
	if i == nil || len(i.fsdevs) == 0 {
		return nil
	}
	out := make([]virtio.FSStats, 0, len(i.fsdevs))
	for _, fsdev := range i.fsdevs {
		if fsdev == nil {
			continue
		}
		out = append(out, fsdev.Stats())
	}
	return out
}

func (i *linuxInstance) Exec(ctx context.Context, req client.ExecRequest) (client.ExecResponse, error) {
	if i == nil || i.session == nil {
		return client.ExecResponse{}, fmt.Errorf("instance is not running")
	}
	user, err := linuxResolveExecUser(req.User)
	if err != nil {
		return client.ExecResponse{}, err
	}
	env := linuxEffectiveExecEnv(i.baseEnv, req.Env, req.ReplaceEnv)
	command := append([]string(nil), req.Command...)
	if !req.SkipResolve {
		if i.image == nil || i.image.RootFS == nil {
			return client.ExecResponse{}, fmt.Errorf("running instance does not have a default image root filesystem")
		}
		var err error
		command, err = imagefs.ResolveCommand(i.image.RootFS, req.Command, env)
		if err != nil {
			return client.ExecResponse{}, err
		}
	}
	workDir := req.WorkDir
	if workDir == "" {
		workDir = i.workDir
	}
	if workDir == "" {
		workDir = "/"
	}
	if !strings.HasPrefix(workDir, "/") {
		return client.ExecResponse{}, fmt.Errorf("workdir must be absolute")
	}
	return i.session.Exec(ctx, client.ExecRequest{
		Command:     command,
		Env:         env,
		RootDir:     req.RootDir,
		ReplaceEnv:  req.ReplaceEnv,
		SkipResolve: req.SkipResolve,
		WorkDir:     workDir,
		User:        user,
		Stdin:       append([]byte(nil), req.Stdin...),
		TTY:         req.TTY,
		Cols:        req.Cols,
		Rows:        req.Rows,
	})
}

func (i *linuxInstance) ExecStream(ctx context.Context, req client.ExecRequest, inputs <-chan client.ExecInput, onEvent func(client.ExecEvent) error) error {
	if i == nil || i.session == nil {
		return fmt.Errorf("instance is not running")
	}
	user, err := linuxResolveExecUser(req.User)
	if err != nil {
		return err
	}
	env := linuxEffectiveExecEnv(i.baseEnv, req.Env, req.ReplaceEnv)
	command := append([]string(nil), req.Command...)
	if !req.SkipResolve {
		if i.image == nil || i.image.RootFS == nil {
			return fmt.Errorf("running instance does not have a default image root filesystem")
		}
		var err error
		command, err = imagefs.ResolveCommand(i.image.RootFS, req.Command, env)
		if err != nil {
			return err
		}
	}
	workDir := req.WorkDir
	if workDir == "" {
		workDir = i.workDir
	}
	if workDir == "" {
		workDir = "/"
	}
	if !strings.HasPrefix(workDir, "/") {
		return fmt.Errorf("workdir must be absolute")
	}
	return i.session.ExecStream(ctx, client.ExecRequest{
		Command:     command,
		Env:         env,
		RootDir:     req.RootDir,
		ReplaceEnv:  req.ReplaceEnv,
		SkipResolve: req.SkipResolve,
		WorkDir:     workDir,
		User:        user,
		Stdin:       append([]byte(nil), req.Stdin...),
		TTY:         req.TTY,
		Cols:        req.Cols,
		Rows:        req.Rows,
	}, inputs, onEvent)
}

func (i *linuxInstance) AddShare(ctx context.Context, share client.ShareMount) error {
	_ = ctx
	if i == nil || i.rootFS == nil {
		return fmt.Errorf("instance rootfs does not support shares")
	}
	key := strings.TrimSpace(share.Mount)
	if key == "" {
		return fmt.Errorf("share mount path is required")
	}
	i.shareMu.Lock()
	if existing, ok := i.shares[key]; ok {
		i.shareMu.Unlock()
		if existing.Source == share.Source && existing.Writable == share.Writable {
			return nil
		}
		return fmt.Errorf("share mount %q already exists", key)
	}
	i.shareMu.Unlock()
	mount, err := amd64vm.BuildShareMount(0, vmruntime.DirectoryShare{
		Source:   share.Source,
		Mount:    share.Mount,
		Writable: share.Writable,
	})
	if err != nil {
		return err
	}
	if err := i.rootFS.AddShare(mount); err != nil {
		return err
	}
	i.shareMu.Lock()
	if i.shares == nil {
		i.shares = make(map[string]client.ShareMount)
	}
	i.shares[key] = share
	i.shareMu.Unlock()
	return nil
}

func (i *linuxInstance) AddPortForward(ctx context.Context, forward client.PortForward) error {
	_ = ctx
	if i == nil || i.network == nil {
		return fmt.Errorf("instance network is not enabled")
	}
	return i.network.AddPortForward(forward)
}

func (i *linuxInstance) NetworkIPv4() string {
	if i == nil || i.network == nil {
		return ""
	}
	return networkGuestAddress(i.network)
}

func (i *linuxInstance) AddImage(ctx context.Context, mountPath string, image *oci.Image) error {
	_ = ctx
	if i == nil || i.rootFS == nil {
		return fmt.Errorf("instance rootfs does not support image mounts")
	}
	if strings.TrimSpace(mountPath) == "" || !strings.HasPrefix(mountPath, "/") {
		return fmt.Errorf("image mount path must be absolute")
	}
	if image == nil || image.RootFS == nil {
		return fmt.Errorf("image root filesystem is not available")
	}
	i.shareMu.Lock()
	if existing, ok := i.imageMounts[mountPath]; ok {
		i.shareMu.Unlock()
		if existing == image.Name {
			return nil
		}
		return fmt.Errorf("image mount %q already exists", mountPath)
	}
	i.shareMu.Unlock()
	if err := i.rootFS.AddShare(virtio.ShareMount{
		GuestPath: mountPath,
		Backend:   linuxRuntimeImageFS(image),
		Writable:  true,
	}); err != nil {
		return err
	}
	i.shareMu.Lock()
	if i.imageMounts == nil {
		i.imageMounts = make(map[string]string)
	}
	i.imageMounts[mountPath] = image.Name
	i.shareMu.Unlock()
	return nil
}

func (i *linuxInstance) Wait() error {
	if i == nil || i.session == nil {
		return nil
	}
	return i.session.Wait()
}

func (i *linuxInstance) Close() error {
	if i == nil || i.session == nil {
		return nil
	}
	err := i.session.Close()
	if netErr := i.network.Close(); err == nil {
		err = netErr
	}
	return err
}

func linuxGuestInitConfig(modules []alpine.Module, managedExec bool, network *client.NetworkConfig, runtime *linuxNetworkRuntime) vmruntime.GuestInitConfig {
	cfg := vmruntime.GuestInitConfig{
		Hostname:         vmruntime.DefaultHostname(""),
		Modules:          vmruntime.ModulePaths(modules),
		ReadyMarker:      linuxInitReadyMarker,
		BeginMarker:      vmruntime.CommandBeginMarker,
		OutputMarkerPref: vmruntime.CommandOutputMarker,
		ErrorMarkerPref:  vmruntime.CommandErrorMarker,
		UsageMarkerPref:  vmruntime.CommandUsageMarker,
		ExitMarkerPrefix: vmruntime.CommandExitMarkerPref,
	}
	if managedExec {
		cfg.VsockPort = vmruntime.ControlPort
	}
	if network != nil && network.Enabled {
		cfg.Network = &vmruntime.GuestNetworkConfig{
			Interface: "eth0",
			Address:   networkGuestCIDR(runtime),
			Gateway:   "10.42.0.1",
			DNS:       "10.42.0.1",
		}
	}
	return cfg
}

func linuxRuntimeImageFS(image *oci.Image) virtio.FSBackend {
	if image == nil {
		return nil
	}
	uid, gid := os.Getuid(), os.Getgid()
	if uid < 0 || gid < 0 {
		return virtio.NewImageFS(image.RootFS, image.RootFSDir)
	}
	return virtio.NewImageFSWithOwner(image.RootFS, image.RootFSDir, uint32(uid), uint32(gid))
}

func linuxRuntimeConfigVars(image *oci.Image, extraModules ...string) []string {
	vars := []string{"CONFIG_VIRTIO_MMIO", "CONFIG_FUSE_FS", "CONFIG_VIRTIO_FS", "CONFIG_VSOCKETS", "CONFIG_VIRTIO_VSOCKETS", "CONFIG_HW_RANDOM", "CONFIG_HW_RANDOM_VIRTIO", "CONFIG_VIRTIO_NET", "CONFIG_OVERLAY_FS"}
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
		"CONFIG_VIRTIO_NET":                     "kernel/drivers/net/virtio_net.ko.gz",
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
	}
}

func withLinuxRuntimeMountDirs(image *oci.Image) *oci.Image {
	if image == nil || image.RootFS == nil {
		return image
	}
	overlay := imagefs.NewOverlay(image.RootFS)
	for _, dir := range []string{"/dev", "/proc", "/sys", "/run", "/tmp", "/etc"} {
		_ = overlay.AddDir(dir, fs.ModeDir|0o755)
	}
	addLinuxRuntimeIdentityFiles(overlay, os.Getuid(), os.Getgid())
	addLinuxRuntimeHostnameFiles(overlay)
	cloned := *image
	cloned.RootFS = overlay.Root()
	return &cloned
}

func blankLinuxRuntimeRootFS() imagefs.Directory {
	overlay := imagefs.NewOverlay(nil)
	for _, dir := range []string{"/dev", "/proc", "/sys", "/run", "/tmp", "/etc", "/.ccx3", "/.ccx3/images"} {
		_ = overlay.AddDir(dir, fs.ModeDir|0o755)
	}
	addLinuxRuntimeIdentityFiles(overlay, os.Getuid(), os.Getgid())
	addLinuxRuntimeHostnameFiles(overlay)
	return overlay.Root()
}

func addLinuxRuntimeIdentityFiles(overlay *imagefs.Overlay, uid, gid int) {
	if overlay == nil {
		return
	}
	identity := linuxRuntimeHostIdentity(uid, gid)
	addLinuxRuntimeIdentityFilesForUser(overlay, identity)
}

type linuxRuntimeIdentity struct {
	Name   string
	UID    int
	GID    int
	Gecos  string
	Home   string
	Shell  string
	Groups []linuxRuntimeGroup
}

type linuxRuntimeGroup struct {
	Name string
	GID  int
}

func linuxRuntimeHostIdentity(uid, gid int) linuxRuntimeIdentity {
	name := "ccx3"
	home := "/home/ccx3"
	gecos := "ccx3 user"
	shell := "/bin/sh"
	if u, err := osuser.LookupId(strconv.Itoa(uid)); err == nil {
		if u.Username != "" {
			name = u.Username
		}
		if u.Name != "" {
			gecos = u.Name
		}
		if u.HomeDir != "" {
			home = u.HomeDir
		}
		if parsed := linuxRuntimeHostPasswdIdentity(uid); parsed.Shell != "" {
			shell = parsed.Shell
			if parsed.Gecos != "" {
				gecos = parsed.Gecos
			}
		}
	}
	groups := []linuxRuntimeGroup{linuxRuntimeHostGroup(gid, name)}
	for _, groupID := range linuxRuntimeHostGroups() {
		if groupID == gid {
			continue
		}
		groups = append(groups, linuxRuntimeHostGroup(groupID, name))
	}
	return linuxRuntimeIdentity{
		Name:   name,
		UID:    uid,
		GID:    gid,
		Gecos:  gecos,
		Home:   home,
		Shell:  shell,
		Groups: groups,
	}
}

func linuxRuntimeHostPasswdIdentity(uid int) linuxRuntimeIdentity {
	passwd, err := os.ReadFile("/etc/passwd")
	if err != nil {
		return linuxRuntimeIdentity{}
	}
	uidText := strconv.Itoa(uid)
	for _, line := range strings.Split(string(passwd), "\n") {
		fields := strings.Split(line, ":")
		if len(fields) >= 7 && fields[2] == uidText {
			return linuxRuntimeIdentity{Gecos: fields[4], Shell: fields[6]}
		}
	}
	return linuxRuntimeIdentity{}
}

func linuxRuntimeHostGroups() []int {
	groupIDs, err := os.Getgroups()
	if err != nil {
		return nil
	}
	return groupIDs
}

func linuxRuntimeHostGroup(gid int, fallbackName string) linuxRuntimeGroup {
	name := fallbackName
	if g, err := osuser.LookupGroupId(strconv.Itoa(gid)); err == nil && g.Name != "" {
		name = g.Name
	}
	return linuxRuntimeGroup{Name: name, GID: gid}
}

func addLinuxRuntimeIdentityFilesForUser(overlay *imagefs.Overlay, identity linuxRuntimeIdentity) {
	if overlay == nil {
		return
	}
	passwd := readLinuxRuntimeTextFile(overlay.Root(), "/etc/passwd")
	group := readLinuxRuntimeTextFile(overlay.Root(), "/etc/group")

	if strings.TrimSpace(passwd) == "" {
		passwd = "root:x:0:0:root:/root:/bin/sh\n"
	}
	if strings.TrimSpace(group) == "" {
		group = "root:x:0:\n"
	}

	_ = overlay.AddFile("/etc/passwd", 0o644, []byte(linuxRuntimePasswdContent(passwd, identity)))
	_ = overlay.AddFile("/etc/group", 0o644, []byte(linuxRuntimeGroupContent(group, identity)))
}

func readLinuxRuntimeTextFile(root imagefs.Directory, guestPath string) string {
	entry, err := imagefs.LookupPath(root, guestPath)
	if err != nil || entry.File == nil {
		return ""
	}
	size, _ := entry.File.Stat()
	if size == 0 {
		return ""
	}
	data, err := entry.File.ReadAt(0, uint32(size))
	if err != nil && len(data) == 0 {
		return ""
	}
	return string(data)
}

func linuxRuntimePasswdContent(passwd string, identity linuxRuntimeIdentity) string {
	lines := strings.Split(strings.TrimSuffix(passwd, "\n"), "\n")
	uidText := strconv.Itoa(identity.UID)
	for i, line := range lines {
		if line == "" {
			continue
		}
		fields := strings.Split(line, ":")
		if len(fields) >= 7 && fields[2] == uidText {
			lines[i] = strings.Join([]string{
				identity.Name,
				"x",
				uidText,
				fields[3],
				identity.Gecos,
				identity.Home,
				fields[6],
			}, ":")
			return strings.Join(append(lines, ""), "\n")
		}
	}
	return ensureTrailingNewline(passwd) + linuxRuntimePasswdLine(identity)
}

func linuxRuntimePasswdLine(identity linuxRuntimeIdentity) string {
	return fmt.Sprintf("%s:x:%d:%d:%s:%s:%s\n", identity.Name, identity.UID, identity.GID, identity.Gecos, identity.Home, identity.Shell)
}

func linuxRuntimeGroupContent(group string, identity linuxRuntimeIdentity) string {
	group = ensureTrailingNewline(group)
	seen := map[string]bool{}
	for _, hostGroup := range identity.Groups {
		line := fmt.Sprintf("%s:x:%d:%s\n", hostGroup.Name, hostGroup.GID, identity.Name)
		if !seen[line] {
			seen[line] = true
			group += line
		}
	}
	return group
}

func ensureTrailingNewline(value string) string {
	if value == "" || strings.HasSuffix(value, "\n") {
		return value
	}
	return value + "\n"
}

func addLinuxRuntimeHostnameFiles(overlay *imagefs.Overlay) {
	hostname := vmruntime.DefaultHostname("")
	_ = overlay.AddFile("/etc/hostname", 0o644, []byte(hostname+"\n"))
	hosts := "127.0.0.1\tlocalhost " + hostname + "\n::1\tlocalhost ip6-localhost ip6-loopback " + hostname + "\n"
	_ = overlay.AddFile("/etc/hosts", 0o644, []byte(hosts))
}

func linuxImageMountPath(image string) string {
	replacer := strings.NewReplacer("/", "_", ":", "_", "@", "_", " ", "_")
	return filepath.Join("/.ccx3", "images", replacer.Replace(image))
}

func linuxEffectiveExecEnv(base, overrides []string, replace bool) []string {
	if replace {
		return vmruntime.WithDefaultEnv(overrides)
	}
	return vmruntime.WithDefaultEnv(vmruntime.MergeEnv(base, overrides))
}

func linuxResolveExecUser(user string) (string, error) {
	user = strings.TrimSpace(user)
	if user == "" {
		uid := os.Getuid()
		gid := os.Getgid()
		if uid <= 0 {
			return "0:0", nil
		}
		return strconv.Itoa(uid) + ":" + strconv.Itoa(gid), nil
	}
	if user == "root" || user == "0" || user == "0:0" {
		return "0:0", nil
	}
	uidPart, gidPart, hasGID := strings.Cut(user, ":")
	if uidPart == "" {
		return "", fmt.Errorf("invalid user %q", user)
	}
	if _, err := strconv.ParseUint(uidPart, 10, 32); err != nil {
		return "", fmt.Errorf("linux amd64 runtime supports numeric users only: %q", user)
	}
	if hasGID {
		if gidPart == "" {
			return "", fmt.Errorf("invalid user %q", user)
		}
		if _, err := strconv.ParseUint(gidPart, 10, 32); err != nil {
			return "", fmt.Errorf("linux amd64 runtime supports numeric users only: %q", user)
		}
		return uidPart + ":" + gidPart, nil
	}
	return uidPart + ":" + uidPart, nil
}

func convertLinuxShareMounts(shares []client.ShareMount) []vmruntime.DirectoryShare {
	if len(shares) == 0 {
		return nil
	}
	out := make([]vmruntime.DirectoryShare, 0, len(shares))
	for _, share := range shares {
		out = append(out, vmruntime.DirectoryShare{
			Source:   share.Source,
			Mount:    share.Mount,
			Writable: share.Writable,
		})
	}
	return out
}
