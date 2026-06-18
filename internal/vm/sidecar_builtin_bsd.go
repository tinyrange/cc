//go:build (darwin && arm64) || (linux && amd64) || (linux && arm64)

package vm

import (
	"context"
	"fmt"

	"j5.nz/cc/client"
	managedguest "j5.nz/cc/internal/managed/guest"
	"j5.nz/cc/internal/vm/builtin"
)

func (h *sidecarVMHost) startBuiltinGuestStream(ctx context.Context, req client.CreateInstanceRequest, onEvent func(client.BootEvent) error) (Instance, bool, error) {
	profile, ok := builtinGuestForImage(req.Image)
	if !ok {
		return nil, false, nil
	}
	req.Image = profile.Canonical
	req.ID = DefaultInstanceID
	sidecar, err := h.launch(ctx, nil)
	if err != nil {
		return nil, true, err
	}
	if _, err := sidecar.Worker().Start(ctx, req, onEvent); err != nil {
		_ = sidecar.Close()
		return nil, true, err
	}
	return newSidecarInstance(DefaultInstanceID, sidecar, req.Image, builtinSidecarResources(profile)), true, nil
}

func (h *sidecarVMHost) startBuiltinGuestBlankStream(ctx context.Context, req client.StartInstanceRequest, onEvent func(client.BootEvent) error) (Instance, bool, error) {
	profile, ok := builtinGuestForImage(req.Image)
	if !ok {
		return nil, false, nil
	}
	req.Image = profile.Canonical
	req.ID = DefaultInstanceID
	sidecar, err := h.launch(ctx, nil)
	if err != nil {
		return nil, true, err
	}
	if _, err := sidecar.Worker().StartBlank(ctx, req, onEvent); err != nil {
		_ = sidecar.Close()
		return nil, true, err
	}
	return newSidecarInstance(DefaultInstanceID, sidecar, req.Image, builtinSidecarResources(profile)), true, nil
}

func builtinSidecarResources(profile managedguest.Profile) sidecarStartResources {
	return sidecarStartResources{
		osName:       profile.Name,
		capabilities: profile.Caps,
		execEnv:      builtin.EffectiveExecEnv,
	}
}

func (h *sidecarVMHost) rejectBuiltinGuestAlternateImage(image string) error {
	if isBuiltinGuestImage(image) {
		return fmt.Errorf("managed guest image %q cannot be mounted as an alternate Linux root", image)
	}
	return nil
}
