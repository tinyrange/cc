//go:build darwin && arm64

package vm

import (
	"context"
	"fmt"
	"path/filepath"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/guestinit"
	"j5.nz/cc/internal/hv/hvf"
	"j5.nz/cc/internal/kernel/alpine"
	"j5.nz/cc/internal/oci"
)

type runtimeBackend struct {
	kernel *alpine.Manager
	images *oci.Store
}

func NewRuntimeBackend(kernel *alpine.Manager, images *oci.Store) Backend {
	return &runtimeBackend{kernel: kernel, images: images}
}

func (b *runtimeBackend) Start(ctx context.Context, req client.CreateInstanceRequest) (Instance, error) {
	runReq, err := b.buildStartRequest(ctx, req)
	if err != nil {
		return nil, err
	}
	session, err := hvf.StartContainer(ctx, runReq)
	if err != nil {
		return nil, err
	}
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

func (b *runtimeBackend) buildBaseRequest(ctx context.Context, imageName string, memoryMB uint64, cpus int, dmesg bool) (hvf.ContainerRunRequest, error) {
	if b.kernel == nil || b.images == nil {
		return hvf.ContainerRunRequest{}, fmt.Errorf("runtime backend is not configured")
	}
	image, err := b.images.Open(imageName)
	if err != nil {
		return hvf.ContainerRunRequest{}, err
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
	if needsAMD64Emulation(image) {
		configVars = append(configVars, "CONFIG_BINFMT_MISC")
		moduleMap["CONFIG_BINFMT_MISC"] = "kernel/fs/binfmt_misc.ko.gz"
	}
	modules, err := b.kernel.PlanModuleLoad(configVars, moduleMap)
	if err != nil {
		return hvf.ContainerRunRequest{}, err
	}
	qemuX8664, err := loadAMD64Emulator(ctx, image, b.kernel.ReadPackageFile)
	if err != nil {
		return hvf.ContainerRunRequest{}, err
	}
	initBin, err := guestinit.Build(ctx, filepath.Join(b.images.Root(), "_guestinit"))
	if err != nil {
		return hvf.ContainerRunRequest{}, err
	}
	return hvf.ContainerRunRequest{
		Kernel:        kernel,
		Init:          initBin,
		AMD64Emulator: qemuX8664,
		Modules:       modules,
		Image:         image,
		MemoryMB:      memoryMB,
		CPUs:          cpus,
		Dmesg:         dmesg,
	}, ctx.Err()
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
