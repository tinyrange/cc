//go:build windows && amd64

package vm

import (
	"context"
	"fmt"

	"j5.nz/cc/client"
	managedguest "j5.nz/cc/internal/managed/guest"
	"j5.nz/cc/internal/vm/builtin"
)

func builtinGuestForImage(image string) (managedguest.Profile, bool) {
	return builtin.GuestForImage(image)
}

func isBuiltinGuestImage(image string) bool {
	return builtin.IsGuestImage(image)
}

func (b *runtimeBackend) startBuiltinGuestProfile(context.Context, managedguest.Profile, client.CreateInstanceRequest, func(client.BootEvent) error) (Instance, error) {
	return nil, fmt.Errorf("WHP managed BSD guests are not implemented on windows/amd64")
}

func (b *runtimeBackend) startBuiltinGuestStream(ctx context.Context, req client.CreateInstanceRequest, onEvent func(client.BootEvent) error) (Instance, bool, error) {
	profile, ok := builtinGuestForImage(req.Image)
	if !ok {
		return nil, false, nil
	}
	req.Image = profile.Canonical
	inst, err := b.startBuiltinGuestProfile(ctx, profile, req, onEvent)
	return inst, true, err
}

func (b *runtimeBackend) startBuiltinGuestBlankStream(ctx context.Context, req client.StartInstanceRequest, onEvent func(client.BootEvent) error) (Instance, bool, error) {
	profile, ok := builtinGuestForImage(req.Image)
	if !ok {
		return nil, false, nil
	}
	inst, err := b.startBuiltinGuestProfile(ctx, profile, client.CreateInstanceRequest{
		ID:             req.ID,
		Image:          profile.Canonical,
		InitSystem:     req.InitSystem,
		Kernel:         req.Kernel,
		Network:        req.Network,
		KernelModules:  append([]string(nil), req.KernelModules...),
		MemoryMB:       req.MemoryMB,
		CPUs:           req.CPUs,
		NestedVirt:     req.NestedVirt,
		Dmesg:          req.Dmesg,
		TimeoutSeconds: req.TimeoutSeconds,
	}, onEvent)
	return inst, true, err
}

func (b *runtimeBackend) runBuiltinGuest(ctx context.Context, req client.RunRequest) (client.ExecResponse, bool, error) {
	profile, ok := builtinGuestForImage(req.Image)
	if !ok {
		return client.ExecResponse{}, false, nil
	}
	_, err := b.startBuiltinGuestProfile(ctx, profile, client.CreateInstanceRequest{
		ID:             req.ID,
		Image:          profile.Canonical,
		InitSystem:     req.InitSystem,
		Network:        req.Network,
		MemoryMB:       req.MemoryMB,
		CPUs:           req.CPUs,
		Dmesg:          req.Dmesg,
		TimeoutSeconds: req.TimeoutSeconds,
	}, nil)
	return client.ExecResponse{}, true, err
}

func builtinGuestCapabilities(image string) guestCapabilities {
	profile, ok := builtinGuestForImage(image)
	if !ok {
		return guestCapabilities{}
	}
	return profile.Caps
}
