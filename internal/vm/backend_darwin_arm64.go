//go:build darwin && arm64

package vm

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/guestinit"
	"j5.nz/cc/internal/hv/hvf"
	"j5.nz/cc/internal/imagefs"
	"j5.nz/cc/internal/kernel/alpine"
	"j5.nz/cc/internal/oci"
	"j5.nz/cc/internal/virtio"
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
	start := time.Now()
	runReq, err := b.buildStartRequest(ctx, req)
	if err != nil {
		return nil, err
	}
	timingLog("runtime.Start buildStartRequest took=%s image=%q", time.Since(start), req.Image)
	session, err := hvf.StartContainerStream(ctx, runReq, onEvent)
	if err != nil {
		return nil, err
	}
	timingLog("runtime.Start hvf.StartContainer took=%s image=%q", time.Since(start), req.Image)
	return session, nil
}

func (b *runtimeBackend) StartBlankStream(
	ctx context.Context,
	req client.StartInstanceRequest,
	onEvent func(client.BootEvent) error,
) (Instance, error) {
	start := time.Now()
	runReq, err := b.buildBlankStartRequest(ctx, req)
	if err != nil {
		return nil, err
	}
	timingLog("runtime.StartBlank buildBlankStartRequest took=%s", time.Since(start))
	session, err := hvf.StartContainerStream(ctx, runReq, onEvent)
	if err != nil {
		return nil, err
	}
	timingLog("runtime.StartBlank hvf.StartContainer took=%s", time.Since(start))
	return session, nil
}

func (b *runtimeBackend) Run(ctx context.Context, req client.RunRequest) (client.ExecResponse, error) {
	runReq, err := b.buildRunRequest(ctx, req)
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

func (b *runtimeBackend) RunInInstance(
	ctx context.Context,
	inst Instance,
	runningImage string,
	req client.RunRequest,
) (client.ExecResponse, error) {
	for _, share := range req.Shares {
		if err := inst.AddShare(ctx, share); err != nil {
			return client.ExecResponse{}, err
		}
	}

	targetImage := strings.TrimSpace(req.Image)
	if targetImage == "" || targetImage == runningImage {
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

	session, ok := inst.(*hvf.ContainerSession)
	if !ok {
		return client.ExecResponse{}, fmt.Errorf("running instance does not support image mounts")
	}

	image, err := b.images.Open(targetImage)
	if err != nil {
		return client.ExecResponse{}, err
	}
	image = withRuntimeMountDirs(image)
	mountPath := imageMountPath(targetImage)
	if err := session.AddImage(ctx, mountPath, image); err != nil {
		return client.ExecResponse{}, err
	}

	env := append([]string(nil), image.Config.Env...)
	env = mergeRuntimeEnv(env, req.Env)
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

func (b *runtimeBackend) buildBaseRequest(ctx context.Context, imageName string, memoryMB uint64, cpus int, dmesg bool) (hvf.ContainerRunRequest, error) {
	start := time.Now()
	if b.kernel == nil || b.images == nil {
		return hvf.ContainerRunRequest{}, fmt.Errorf("runtime backend is not configured")
	}
	image, err := b.images.Open(imageName)
	if err != nil {
		return hvf.ContainerRunRequest{}, err
	}
	image = withRuntimeMountDirs(image)
	timingLog("buildBaseRequest image open took=%s image=%q", time.Since(start), imageName)
	kernel, err := b.kernel.ReadKernel()
	if err != nil {
		return hvf.ContainerRunRequest{}, err
	}
	timingLog("buildBaseRequest ReadKernel took=%s image=%q", time.Since(start), imageName)
	configVars := []string{"CONFIG_VIRTIO_MMIO", "CONFIG_FUSE_FS", "CONFIG_VIRTIO_FS", "CONFIG_VSOCKETS", "CONFIG_VIRTIO_VSOCKETS"}
	moduleMap := map[string]string{
		"CONFIG_VIRTIO_MMIO":     "kernel/drivers/virtio/virtio_mmio.ko.gz",
		"CONFIG_FUSE_FS":         "kernel/fs/fuse/fuse.ko.gz",
		"CONFIG_VIRTIO_FS":       "kernel/fs/fuse/virtiofs.ko.gz",
		"CONFIG_VSOCKETS":        "kernel/net/vmw_vsock/vsock.ko.gz",
		"CONFIG_VIRTIO_VSOCKETS": "kernel/net/vmw_vsock/vmw_vsock_virtio_transport.ko.gz",
	}
	if NeedsAMD64Emulation(image) {
		configVars = append(configVars, "CONFIG_BINFMT_MISC")
		moduleMap["CONFIG_BINFMT_MISC"] = "kernel/fs/binfmt_misc.ko.gz"
	}
	modules, err := b.kernel.PlanModuleLoad(configVars, moduleMap)
	if err != nil {
		return hvf.ContainerRunRequest{}, err
	}
	timingLog("buildBaseRequest PlanModuleLoad took=%s image=%q modules=%d", time.Since(start), imageName, len(modules))
	qemuX8664, err := PrepareAMD64Emulator(ctx, image, b.kernel.ExtractPackageFile)
	if err != nil {
		return hvf.ContainerRunRequest{}, err
	}
	timingLog("buildBaseRequest loadAMD64Emulator took=%s image=%q emulator_path=%q", time.Since(start), imageName, qemuX8664)
	guestInitCache := b.guestInitCache
	if guestInitCache == "" {
		guestInitCache = filepath.Join(b.images.Root(), "_guestinit")
	}
	initBin, err := guestinit.Build(ctx, guestInitCache)
	if err != nil {
		return hvf.ContainerRunRequest{}, err
	}
	timingLog("buildBaseRequest guestinit.Build took=%s image=%q init_bytes=%d", time.Since(start), imageName, len(initBin))
	return hvf.ContainerRunRequest{
		Kernel:            kernel,
		Init:              initBin,
		AMD64EmulatorPath: qemuX8664,
		Modules:           modules,
		Image:             image,
		MemoryMB:          memoryMB,
		CPUs:              cpus,
		Dmesg:             dmesg,
	}, ctx.Err()
}

func blankRuntimeRootFS() imagefs.Directory {
	overlay := imagefs.NewOverlay(nil)
	for _, dir := range []string{"/dev", "/proc", "/sys", "/run", "/tmp", "/.ccx3", "/.ccx3/images"} {
		_ = overlay.AddDir(dir, fs.ModeDir|0o755)
	}
	return overlay.Root()
}

func withRuntimeMountDirs(image *oci.Image) *oci.Image {
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

func (b *runtimeBackend) buildStartRequest(ctx context.Context, req client.CreateInstanceRequest) (hvf.ContainerRunRequest, error) {
	runReq, err := b.buildBaseRequest(ctx, req.Image, req.MemoryMB, req.CPUs, req.Dmesg)
	if err != nil {
		return hvf.ContainerRunRequest{}, err
	}
	runReq.Shares = append(runReq.Shares, convertShareMounts(req.Shares)...)
	runReq.Persistent = true
	return runReq, nil
}

func (b *runtimeBackend) buildBlankStartRequest(ctx context.Context, req client.StartInstanceRequest) (hvf.ContainerRunRequest, error) {
	if b.kernel == nil || b.images == nil {
		return hvf.ContainerRunRequest{}, fmt.Errorf("runtime backend is not configured")
	}
	kernel, err := b.kernel.ReadKernel()
	if err != nil {
		return hvf.ContainerRunRequest{}, err
	}
	configVars := []string{"CONFIG_VIRTIO_MMIO", "CONFIG_FUSE_FS", "CONFIG_VIRTIO_FS", "CONFIG_VSOCKETS", "CONFIG_VIRTIO_VSOCKETS"}
	moduleMap := map[string]string{
		"CONFIG_VIRTIO_MMIO":     "kernel/drivers/virtio/virtio_mmio.ko.gz",
		"CONFIG_FUSE_FS":         "kernel/fs/fuse/fuse.ko.gz",
		"CONFIG_VIRTIO_FS":       "kernel/fs/fuse/virtiofs.ko.gz",
		"CONFIG_VSOCKETS":        "kernel/net/vmw_vsock/vsock.ko.gz",
		"CONFIG_VIRTIO_VSOCKETS": "kernel/net/vmw_vsock/vmw_vsock_virtio_transport.ko.gz",
	}
	modules, err := b.kernel.PlanModuleLoad(configVars, moduleMap)
	if err != nil {
		return hvf.ContainerRunRequest{}, err
	}
	guestInitCache := b.guestInitCache
	if guestInitCache == "" {
		guestInitCache = filepath.Join(b.images.Root(), "_guestinit")
	}
	initBin, err := guestinit.Build(ctx, guestInitCache)
	if err != nil {
		return hvf.ContainerRunRequest{}, err
	}
	return hvf.ContainerRunRequest{
		Kernel:     kernel,
		Init:       initBin,
		Modules:    modules,
		RootFS:     virtio.NewImageFS(blankRuntimeRootFS(), ""),
		MemoryMB:   req.MemoryMB,
		CPUs:       req.CPUs,
		Dmesg:      req.Dmesg,
		Persistent: true,
	}, ctx.Err()
}

func (b *runtimeBackend) buildRunRequest(ctx context.Context, req client.RunRequest) (hvf.ContainerRunRequest, error) {
	runReq, err := b.buildBaseRequest(ctx, req.Image, req.MemoryMB, req.CPUs, req.Dmesg)
	if err != nil {
		return hvf.ContainerRunRequest{}, err
	}
	runReq.Shares = append(runReq.Shares, convertShareMounts(req.Shares)...)
	runReq.Command = append([]string(nil), req.Command...)
	runReq.Env = append([]string(nil), req.Env...)
	runReq.WorkDir = req.WorkDir
	runReq.User = req.User
	return runReq, nil
}

func convertShareMounts(shares []client.ShareMount) []hvf.DirectoryShare {
	if len(shares) == 0 {
		return nil
	}
	out := make([]hvf.DirectoryShare, 0, len(shares))
	for _, share := range shares {
		out = append(out, hvf.DirectoryShare{
			Source:   share.Source,
			Mount:    share.Mount,
			Writable: share.Writable,
		})
	}
	return out
}

func imageMountPath(image string) string {
	replacer := strings.NewReplacer("/", "_", ":", "_", "@", "_", " ", "_")
	return filepath.Join("/.ccx3", "images", replacer.Replace(image))
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
