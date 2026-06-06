//go:build !darwin || !arm64

package vm

import (
	"context"
	"fmt"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/virtio"
)

func prepareSidecarCreateResources(h *sidecarVMHost, ctx context.Context, req client.CreateInstanceRequest) (sidecarStartResources, error) {
	_ = h
	_ = ctx
	_ = req
	return sidecarStartResources{}, nil
}

func prepareSidecarBlankResources(h *sidecarVMHost, ctx context.Context, req client.StartInstanceRequest) (sidecarStartResources, error) {
	_ = h
	_ = ctx
	_ = req
	return sidecarStartResources{}, nil
}

func sidecarRuntimeShareMount(share client.ShareMount) (virtio.ShareMount, error) {
	_ = share
	return virtio.ShareMount{}, fmt.Errorf("sidecar runtime shares are not supported on this platform")
}
