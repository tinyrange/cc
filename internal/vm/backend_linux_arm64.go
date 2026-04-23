//go:build linux && arm64

package vm

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"sync"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/arm64vm"
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

func (b *runtimeBackend) StartBlank(ctx context.Context, req client.StartInstanceRequest) (Instance, error) {
	return b.StartBlankStream(ctx, req, nil)
}

func (b *runtimeBackend) StartStream(ctx context.Context, req client.CreateInstanceRequest, onEvent func(client.BootEvent) error) (Instance, error) {
	if b == nil || b.kernel == nil || b.images == nil {
		return nil, fmt.Errorf("runtime backend is not configured")
	}
	if req.CPUs > 1 {
		return nil, fmt.Errorf("linux arm64 runtime currently supports only 1 CPU")
	}
	kernel, err := b.kernel.ReadKernel()
	if err != nil {
		return nil, err
	}
	image, err := b.images.Open(req.Image)
	if err != nil {
		return nil, err
	}
	image = withLinuxRuntimeMountDirs(image)
	modules, err := b.kernel.PlanModuleLoad(linuxRuntimeConfigVars(image), linuxRuntimeModuleMap())
	if err != nil {
		return nil, err
	}
	qemuX8664, err := PrepareAMD64Emulator(ctx, image, b.kernel.ExtractPackageFile)
	if err != nil {
		return nil, err
	}
	fsdevs, rootFS, err := arm64vm.BuildFSDevices(vmruntime.RunRequest{
		Image:             image,
		AMD64EmulatorPath: qemuX8664,
		Shares:            convertLinuxShareMounts(req.Shares),
	}, nil)
	if err != nil {
		return nil, err
	}
	initBin, err := guestinit.Build(ctx, b.guestInitCache)
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
	initrd, err := vmruntime.BuildInitramfs(initBin, modules, initCfg)
	if err != nil {
		return nil, fmt.Errorf("build initramfs: %w", err)
	}
	session, err := kvm.StartManagedSession(ctx, kernel, initrd, req.MemoryMB, req.Dmesg, fsdevs, onEvent)
	if err != nil {
		return nil, err
	}
	return &linuxInstance{
		session: session,
		image:   image,
		baseEnv: vmruntime.WithDefaultEnv(image.Config.Env),
		workDir: workDir,
		rootFS:  rootFS,
		dmesg:   req.Dmesg,
	}, nil
}

func (b *runtimeBackend) StartBlankStream(ctx context.Context, req client.StartInstanceRequest, onEvent func(client.BootEvent) error) (Instance, error) {
	if b == nil || b.kernel == nil || b.images == nil {
		return nil, fmt.Errorf("runtime backend is not configured")
	}
	if req.CPUs > 1 {
		return nil, fmt.Errorf("linux arm64 runtime currently supports only 1 CPU")
	}
	kernel, err := b.kernel.ReadKernel()
	if err != nil {
		return nil, err
	}
	modules, err := b.kernel.PlanModuleLoad(linuxRuntimeConfigVars(nil), linuxRuntimeModuleMap())
	if err != nil {
		return nil, err
	}
	rootFSBackend := virtio.NewImageFS(blankLinuxRuntimeRootFS(), "")
	fsdevs, rootFS, err := arm64vm.BuildFSDevices(vmruntime.RunRequest{
		RootFS: rootFSBackend,
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
	initCfg.Env = vmruntime.WithDefaultEnv(nil)
	initCfg.WorkDir = "/"
	initrd, err := vmruntime.BuildInitramfs(initBin, modules, initCfg)
	if err != nil {
		return nil, fmt.Errorf("build initramfs: %w", err)
	}
	session, err := kvm.StartManagedSession(ctx, kernel, initrd, req.MemoryMB, req.Dmesg, fsdevs, onEvent)
	if err != nil {
		return nil, err
	}
	return &linuxInstance{
		session: session,
		baseEnv: vmruntime.WithDefaultEnv(nil),
		workDir: "/",
		rootFS:  rootFS,
		dmesg:   req.Dmesg,
	}, nil
}

func (b *runtimeBackend) Run(ctx context.Context, req client.RunRequest) (client.ExecResponse, error) {
	if b == nil || b.kernel == nil {
		return client.ExecResponse{}, fmt.Errorf("runtime backend is not configured")
	}
	if req.CPUs > 1 {
		return client.ExecResponse{}, fmt.Errorf("linux arm64 runtime currently supports only 1 CPU")
	}

	kernel, err := b.kernel.ReadKernel()
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
		modules, err = b.kernel.PlanModuleLoad(
			linuxRuntimeConfigVars(image),
			linuxRuntimeModuleMap(),
		)
		if err != nil {
			return client.ExecResponse{}, err
		}
		qemuX8664, err = PrepareAMD64Emulator(ctx, image, b.kernel.ExtractPackageFile)
		if err != nil {
			return client.ExecResponse{}, err
		}
		devs, _, err := arm64vm.BuildFSDevices(vmruntime.RunRequest{
			Image:             image,
			AMD64EmulatorPath: qemuX8664,
			Shares:            convertLinuxShareMounts(req.Shares),
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

	initBin, err := guestinit.Build(ctx, b.guestInitCache)
	if err != nil {
		return client.ExecResponse{}, fmt.Errorf("build guest init: %w", err)
	}
	initCfg := linuxGuestInitConfig(modules, len(req.Command) != 0)
	if len(fsdevs) != 0 {
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
		user := strings.TrimSpace(req.User)
		if user != "" && user != "root" && user != "0" && user != "0:0" {
			return client.ExecResponse{}, fmt.Errorf("only root user is supported")
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
			Stdin:   append([]byte(nil), req.Stdin...),
			TTY:     req.TTY,
			Cols:    req.Cols,
			Rows:    req.Rows,
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

type linuxInstance struct {
	session     *kvm.ManagedSession
	image       *oci.Image
	baseEnv     []string
	workDir     string
	rootFS      virtio.ShareMounter
	dmesg       bool
	shareMu     sync.Mutex
	shares      map[string]client.ShareMount
	imageMounts map[string]string
}

func (i *linuxInstance) Exec(ctx context.Context, req client.ExecRequest) (client.ExecResponse, error) {
	if i == nil || i.session == nil {
		return client.ExecResponse{}, fmt.Errorf("instance is not running")
	}
	user := strings.TrimSpace(req.User)
	if user != "" && user != "root" && user != "0" && user != "0:0" {
		return client.ExecResponse{}, fmt.Errorf("only root user is supported")
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
		Stdin:       append([]byte(nil), req.Stdin...),
		TTY:         req.TTY,
		Cols:        req.Cols,
		Rows:        req.Rows,
	})
}

func (i *linuxInstance) ExecStream(ctx context.Context, req client.ExecRequest, inputs <-chan client.ExecInput, onEvent func(client.ExecEvent) error) error {
	_ = inputs
	resp, err := i.Exec(ctx, req)
	if err != nil {
		return err
	}
	if onEvent != nil && resp.Output != "" {
		if err := onEvent(client.ExecEvent{Kind: "stdout", Stream: "stdout", Output: resp.Output, Data: []byte(resp.Output)}); err != nil {
			return err
		}
	}
	if onEvent != nil {
		return onEvent(client.ExecEvent{Kind: "exit", ExitCode: resp.ExitCode})
	}
	return nil
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
	mount, err := arm64vm.BuildShareMount(0, vmruntime.DirectoryShare{
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
		Backend:   virtio.NewImageFS(image.RootFS, image.RootFSDir),
		Writable:  false,
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
	return i.session.Close()
}

func linuxGuestInitConfig(modules []alpine.Module, managedExec bool) vmruntime.GuestInitConfig {
	cfg := vmruntime.GuestInitConfig{
		Modules:          vmruntime.ModulePaths(modules),
		ReadyMarker:      linuxInitReadyMarker,
		BeginMarker:      vmruntime.CommandBeginMarker,
		OutputMarkerPref: vmruntime.CommandOutputMarker,
		ErrorMarkerPref:  vmruntime.CommandErrorMarker,
		ExitMarkerPrefix: vmruntime.CommandExitMarkerPref,
	}
	if managedExec {
		cfg.VsockPort = vmruntime.ControlPort
	}
	return cfg
}

func linuxRuntimeConfigVars(image *oci.Image) []string {
	vars := []string{"CONFIG_VIRTIO_MMIO", "CONFIG_FUSE_FS", "CONFIG_VIRTIO_FS", "CONFIG_VSOCKETS", "CONFIG_VIRTIO_VSOCKETS"}
	if NeedsAMD64Emulation(image) {
		vars = append(vars, "CONFIG_BINFMT_MISC")
	}
	return vars
}

func linuxRuntimeModuleMap() map[string]string {
	return map[string]string{
		"CONFIG_VIRTIO_MMIO":     "kernel/drivers/virtio/virtio_mmio.ko.gz",
		"CONFIG_FUSE_FS":         "kernel/fs/fuse/fuse.ko.gz",
		"CONFIG_VIRTIO_FS":       "kernel/fs/fuse/virtiofs.ko.gz",
		"CONFIG_VSOCKETS":        "kernel/net/vmw_vsock/vsock.ko.gz",
		"CONFIG_VIRTIO_VSOCKETS": "kernel/net/vmw_vsock/vmw_vsock_virtio_transport.ko.gz",
		"CONFIG_BINFMT_MISC":     "kernel/fs/binfmt_misc.ko.gz",
	}
}

func withLinuxRuntimeMountDirs(image *oci.Image) *oci.Image {
	if image == nil || image.RootFS == nil {
		return image
	}
	overlay := imagefs.NewOverlay(image.RootFS)
	for _, dir := range []string{"/dev", "/proc", "/sys", "/run", "/tmp"} {
		_ = overlay.AddDir(dir, fs.ModeDir|0o755)
	}
	cloned := *image
	cloned.RootFS = overlay.Root()
	return &cloned
}

func blankLinuxRuntimeRootFS() imagefs.Directory {
	overlay := imagefs.NewOverlay(nil)
	for _, dir := range []string{"/dev", "/proc", "/sys", "/run", "/tmp", "/.ccx3", "/.ccx3/images"} {
		_ = overlay.AddDir(dir, fs.ModeDir|0o755)
	}
	return overlay.Root()
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
