//go:build linux && arm64

package vm

import (
	"context"
	"fmt"
	"io/fs"
	"strings"

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
	qemuX8664, err := loadAMD64Emulator(ctx, image, b.kernel.ExtractPackageFile)
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
		qemuX8664, err = loadAMD64Emulator(ctx, image, b.kernel.ExtractPackageFile)
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

type linuxInstance struct {
	session *kvm.ManagedSession
	image   *oci.Image
	baseEnv []string
	workDir string
	rootFS  virtio.ShareMounter
	dmesg   bool
}

func (i *linuxInstance) Exec(ctx context.Context, req client.ExecRequest) (client.ExecResponse, error) {
	if i == nil || i.session == nil {
		return client.ExecResponse{}, fmt.Errorf("instance is not running")
	}
	user := strings.TrimSpace(req.User)
	if user != "" && user != "root" && user != "0" && user != "0:0" {
		return client.ExecResponse{}, fmt.Errorf("only root user is supported")
	}
	env := vmruntime.WithDefaultEnv(vmruntime.MergeEnv(i.baseEnv, req.Env))
	command, err := imagefs.ResolveCommand(i.image.RootFS, req.Command, env)
	if err != nil {
		return client.ExecResponse{}, err
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
		Command: command,
		Env:     env,
		WorkDir: workDir,
		Stdin:   append([]byte(nil), req.Stdin...),
		TTY:     req.TTY,
		Cols:    req.Cols,
		Rows:    req.Rows,
	})
}

func (i *linuxInstance) ExecStream(ctx context.Context, req client.ExecRequest, inputs <-chan client.ExecInput, onEvent func(client.ExecEvent) error) error {
	_ = inputs
	_ = onEvent
	_, err := i.Exec(ctx, req)
	return err
}

func (i *linuxInstance) AddShare(ctx context.Context, share client.ShareMount) error {
	_ = ctx
	if i == nil || i.rootFS == nil {
		return fmt.Errorf("instance rootfs does not support shares")
	}
	mount, err := arm64vm.BuildShareMount(0, vmruntime.DirectoryShare{
		Source:   share.Source,
		Mount:    share.Mount,
		Writable: share.Writable,
	})
	if err != nil {
		return err
	}
	return i.rootFS.AddShare(mount)
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
	if needsAMD64Emulation(image) {
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
