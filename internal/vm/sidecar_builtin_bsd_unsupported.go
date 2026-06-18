//go:build !(darwin && arm64) && !(linux && amd64) && !(linux && arm64)

package vm

import (
	"context"

	"j5.nz/cc/client"
)

func (h *sidecarVMHost) startBuiltinGuestStream(context.Context, client.CreateInstanceRequest, func(client.BootEvent) error) (Instance, bool, error) {
	return nil, false, nil
}

func (h *sidecarVMHost) startBuiltinGuestBlankStream(context.Context, client.StartInstanceRequest, func(client.BootEvent) error) (Instance, bool, error) {
	return nil, false, nil
}

func (h *sidecarVMHost) rejectBuiltinGuestAlternateImage(string) error {
	return nil
}
