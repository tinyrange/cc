//go:build linux && amd64

package vm

import (
	"context"
	"fmt"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/kernel/alpine"
	"j5.nz/cc/internal/oci"
)

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
	_ = ctx
	_ = req
	_ = onEvent
	return nil, linuxAMD64NotImplemented()
}

func (b *runtimeBackend) StartBlank(ctx context.Context, req client.StartInstanceRequest) (Instance, error) {
	return b.StartBlankStream(ctx, req, nil)
}

func (b *runtimeBackend) StartBlankStream(ctx context.Context, req client.StartInstanceRequest, onEvent func(client.BootEvent) error) (Instance, error) {
	_ = ctx
	_ = req
	_ = onEvent
	return nil, linuxAMD64NotImplemented()
}

func (b *runtimeBackend) Run(ctx context.Context, req client.RunRequest) (client.ExecResponse, error) {
	_ = ctx
	_ = req
	return client.ExecResponse{}, linuxAMD64NotImplemented()
}

func (b *runtimeBackend) RunInInstance(ctx context.Context, inst Instance, runningImage string, req client.RunRequest) (client.ExecResponse, error) {
	_ = ctx
	_ = inst
	_ = runningImage
	_ = req
	return client.ExecResponse{}, linuxAMD64NotImplemented()
}

func linuxAMD64NotImplemented() error {
	return fmt.Errorf("linux/amd64 VM runtime is not implemented yet")
}
