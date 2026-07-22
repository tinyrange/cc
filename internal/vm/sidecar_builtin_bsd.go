//go:build (darwin && arm64) || (linux && amd64) || (linux && arm64)

package vm

import (
	"context"
	"fmt"

	"j5.nz/cc/client"
	managedguest "j5.nz/cc/internal/managed/guest"
	"j5.nz/cc/internal/vm/builtin"
	"j5.nz/cc/internal/vm/mounts"
)

func (h *sidecarVMHost) startBuiltinGuestStream(ctx context.Context, req client.CreateInstanceRequest, onEvent func(client.BootEvent) error) (Instance, bool, error) {
	profile, ok := builtinGuestForImage(req.Image)
	if !ok {
		return nil, false, nil
	}
	mountState, err := mounts.NewState(req.Shares)
	if err != nil {
		return nil, true, err
	}
	networkID := req.ID
	req.Image = profile.Canonical
	req.ID = DefaultInstanceID
	netResources, err := prepareSidecarBuiltinGuestResources(h, networkID, req.Network)
	if err != nil {
		return nil, true, err
	}
	sidecar, err := h.launch(ctx, netResources.env)
	if err != nil {
		netResources.closeAll()
		return nil, true, err
	}
	sidecar.AddCleanup(netResources.close)
	state, err := sidecar.Worker().Start(ctx, req, onEvent)
	if err != nil {
		_ = sidecar.Close()
		return nil, true, err
	}
	resources := combineSidecarResources(builtinSidecarResources(profile), netResources)
	resources.networkIPv4 = state.NetworkIPv4
	return newSidecarInstance(DefaultInstanceID, sidecar, req.Image, mountState, resources), true, nil
}

func (h *sidecarVMHost) startBuiltinGuestBlankStream(ctx context.Context, req client.StartInstanceRequest, onEvent func(client.BootEvent) error) (Instance, bool, error) {
	profile, ok := builtinGuestForImage(req.Image)
	if !ok {
		return nil, false, nil
	}
	mountState, err := mounts.NewState(req.Shares)
	if err != nil {
		return nil, true, err
	}
	networkID := req.ID
	req.Image = profile.Canonical
	req.ID = DefaultInstanceID
	netResources, err := prepareSidecarBuiltinGuestResources(h, networkID, req.Network)
	if err != nil {
		return nil, true, err
	}
	sidecar, err := h.launch(ctx, netResources.env)
	if err != nil {
		netResources.closeAll()
		return nil, true, err
	}
	sidecar.AddCleanup(netResources.close)
	state, err := sidecar.Worker().StartBlank(ctx, req, onEvent)
	if err != nil {
		_ = sidecar.Close()
		return nil, true, err
	}
	resources := combineSidecarResources(builtinSidecarResources(profile), netResources)
	resources.networkIPv4 = state.NetworkIPv4
	return newSidecarInstance(DefaultInstanceID, sidecar, req.Image, mountState, resources), true, nil
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
