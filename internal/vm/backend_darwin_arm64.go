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

func NewRuntimeBackend(kernel *alpine.Manager, images *oci.Store, guestInitCache string) Backend {
	return &runtimeBackend{kernel: kernel, images: images, guestInitCache: guestInitCache}
}

func (b *runtimeBackend) Start(ctx context.Context, req client.CreateInstanceRequest) (Instance, error) {
	return b.StartStream(ctx, req, nil)
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

func (b *runtimeBackend) buildBaseRequest(ctx context.Context, imageName string, memoryMB uint64, cpus int, dmesg bool) (vmruntime.RunRequest, error) {
	start := time.Now()
	if b.kernel == nil || b.images == nil {
		return vmruntime.RunRequest{}, fmt.Errorf("runtime backend is not configured")
	}
	image, err := b.images.Open(imageName)
	if err != nil {
		return vmruntime.RunRequest{}, err
	}
	image = withRuntimeMountDirs(image)
	timingLog("buildBaseRequest image open took=%s image=%q", time.Since(start), imageName)
	kernel, err := b.kernel.ReadKernel()
	if err != nil {
		return vmruntime.RunRequest{}, err
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
	if needsAMD64Emulation(image) {
		configVars = append(configVars, "CONFIG_BINFMT_MISC")
		moduleMap["CONFIG_BINFMT_MISC"] = "kernel/fs/binfmt_misc.ko.gz"
	}
	modules, err := b.kernel.PlanModuleLoad(configVars, moduleMap)
	if err != nil {
		return vmruntime.RunRequest{}, err
	}
	timingLog("buildBaseRequest PlanModuleLoad took=%s image=%q modules=%d", time.Since(start), imageName, len(modules))
	qemuX8664, err := loadAMD64Emulator(ctx, image, b.kernel.ExtractPackageFile)
	if err != nil {
		return vmruntime.RunRequest{}, err
	}
	timingLog("buildBaseRequest loadAMD64Emulator took=%s image=%q emulator_path=%q", time.Since(start), imageName, qemuX8664)
	guestInitCache := b.guestInitCache
	if guestInitCache == "" {
		guestInitCache = filepath.Join(b.images.Root(), "_guestinit")
	}
	initBin, err := guestinit.Build(ctx, guestInitCache)
	if err != nil {
		return vmruntime.RunRequest{}, err
	}
	timingLog("buildBaseRequest guestinit.Build took=%s image=%q init_bytes=%d", time.Since(start), imageName, len(initBin))
	return vmruntime.RunRequest{
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

func (b *runtimeBackend) buildStartRequest(ctx context.Context, req client.CreateInstanceRequest) (vmruntime.RunRequest, error) {
	runReq, err := b.buildBaseRequest(ctx, req.Image, req.MemoryMB, req.CPUs, req.Dmesg)
	if err != nil {
		return vmruntime.RunRequest{}, err
	}
	runReq.Shares = append(runReq.Shares, convertShareMounts(req.Shares)...)
	runReq.Persistent = true
	return runReq, nil
}

func (b *runtimeBackend) buildRunRequest(ctx context.Context, req client.RunRequest) (vmruntime.RunRequest, error) {
	runReq, err := b.buildBaseRequest(ctx, req.Image, req.MemoryMB, req.CPUs, req.Dmesg)
	if err != nil {
		return vmruntime.RunRequest{}, err
	}
	runReq.Shares = append(runReq.Shares, convertShareMounts(req.Shares)...)
	runReq.Command = append([]string(nil), req.Command...)
	runReq.Env = append([]string(nil), req.Env...)
	runReq.WorkDir = req.WorkDir
	runReq.User = req.User
	return runReq, nil
}

func convertShareMounts(shares []client.ShareMount) []vmruntime.DirectoryShare {
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
