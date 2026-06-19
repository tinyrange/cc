//go:build darwin && arm64

package vm

import (
	"context"
	"fmt"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/hv/hvf"
	managedguest "j5.nz/cc/internal/managed/guest"
	"j5.nz/cc/internal/managed/machine"
	"j5.nz/cc/internal/vm/builtin"
)

func builtinGuestForImage(image string) (managedguest.Profile, bool) {
	return builtin.GuestForImage(image)
}

func isBuiltinGuestImage(image string) bool {
	return builtin.IsGuestImage(image)
}

func (b *runtimeBackend) startBuiltinGuestProfile(ctx context.Context, profile managedguest.Profile, req client.CreateInstanceRequest, onEvent func(client.BootEvent) error) (Instance, error) {
	switch profile.Canonical {
	case managedguest.OpenBSDImageName:
		return b.startOpenBSDStream(ctx, req, onEvent)
	case managedguest.FreeBSDImageName:
		return b.startFreeBSDStream(ctx, req, onEvent)
	case managedguest.NetBSDImageName:
		return b.startNetBSDStream(ctx, req, onEvent)
	default:
		return nil, fmt.Errorf("managed guest profile %q is not supported on darwin/arm64", profile.Canonical)
	}
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
		Shares:         append([]client.ShareMount(nil), req.Shares...),
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
	inst, err := b.startBuiltinGuestProfile(ctx, profile, client.CreateInstanceRequest{
		ID:             req.ID,
		Image:          profile.Canonical,
		InitSystem:     req.InitSystem,
		Network:        req.Network,
		MemoryMB:       req.MemoryMB,
		CPUs:           req.CPUs,
		Dmesg:          req.Dmesg,
		TimeoutSeconds: req.TimeoutSeconds,
	}, nil)
	if err != nil {
		return client.ExecResponse{}, true, err
	}
	defer inst.Close()
	if len(req.Command) == 0 {
		if history, ok := inst.(consoleHistoryProvider); ok {
			output, _ := history.ConsoleHistory(ctx)
			return client.ExecResponse{ExitCode: 0, Output: output}, true, nil
		}
		return client.ExecResponse{}, true, nil
	}
	resp, err := inst.Exec(ctx, runExecRequest(req))
	return resp, true, err
}

func (b *runtimeBackend) startOpenBSDStream(ctx context.Context, req client.CreateInstanceRequest, onEvent func(client.BootEvent) error) (Instance, error) {
	return b.startOpenBSDManagedStream(ctx, req, onEvent, builtin.OpenBSDDefinitionForArch(b.guestInitCache, "arm64"))
}

func (b *runtimeBackend) startFreeBSDStream(ctx context.Context, req client.CreateInstanceRequest, onEvent func(client.BootEvent) error) (Instance, error) {
	return b.startFreeBSDManagedStream(ctx, req, onEvent, builtin.FreeBSDDefinitionForArch(b.guestInitCache, "arm64"))
}

func (b *runtimeBackend) startNetBSDStream(ctx context.Context, req client.CreateInstanceRequest, onEvent func(client.BootEvent) error) (Instance, error) {
	return b.startNetBSDManagedStream(ctx, req, onEvent, builtin.NetBSDDefinitionForArch(b.guestInitCache, "evbarm-aarch64"))
}

func managedBSDNetworkConfig(cfg *client.NetworkConfig) *client.NetworkConfig {
	if cfg == nil {
		return &client.NetworkConfig{Enabled: true, AllowInternet: true}
	}
	copyCfg := *cfg
	return &copyCfg
}

func (b *runtimeBackend) startOpenBSDManagedStream(ctx context.Context, req client.CreateInstanceRequest, onEvent func(client.BootEvent) error, def builtin.BSDDefinition) (Instance, error) {
	return b.startBSDArm64ManagedStream(ctx, req, onEvent, def, func(ctx context.Context, cfg hvf.OpenBSDManagedConfig, onEvent func(client.BootEvent) error) (*hvf.ManagedSession, error) {
		return hvf.StartOpenBSDManagedSession(ctx, cfg, onEvent)
	})
}

func (b *runtimeBackend) startFreeBSDManagedStream(ctx context.Context, req client.CreateInstanceRequest, onEvent func(client.BootEvent) error, def builtin.BSDDefinition) (Instance, error) {
	return b.startBSDArm64ManagedStream(ctx, req, onEvent, def, func(ctx context.Context, cfg hvf.OpenBSDManagedConfig, onEvent func(client.BootEvent) error) (*hvf.ManagedSession, error) {
		return hvf.StartFreeBSDManagedSession(ctx, hvf.FreeBSDManagedConfig{
			Kernel:    cfg.Kernel,
			Root:      cfg.Root,
			MemoryMB:  cfg.MemoryMB,
			Dmesg:     cfg.Dmesg,
			GuestIPv4: cfg.GuestIPv4,
			GuestMAC:  cfg.GuestMAC,
			NetDevice: cfg.NetDevice,
			NetStack:  cfg.NetStack,
		}, onEvent)
	})
}

func (b *runtimeBackend) startNetBSDManagedStream(ctx context.Context, req client.CreateInstanceRequest, onEvent func(client.BootEvent) error, def builtin.BSDDefinition) (Instance, error) {
	return b.startBSDArm64ManagedStream(ctx, req, onEvent, def, func(ctx context.Context, cfg hvf.OpenBSDManagedConfig, onEvent func(client.BootEvent) error) (*hvf.ManagedSession, error) {
		return hvf.StartNetBSDManagedSession(ctx, hvf.NetBSDManagedConfig{
			Kernel:    cfg.Kernel,
			Root:      cfg.Root,
			MemoryMB:  cfg.MemoryMB,
			Dmesg:     cfg.Dmesg,
			GuestIPv4: cfg.GuestIPv4,
			GuestMAC:  cfg.GuestMAC,
			NetDevice: cfg.NetDevice,
			NetStack:  cfg.NetStack,
		}, onEvent)
	})
}

func (b *runtimeBackend) startBSDArm64ManagedStream(ctx context.Context, req client.CreateInstanceRequest, onEvent func(client.BootEvent) error, def builtin.BSDDefinition, start func(context.Context, hvf.OpenBSDManagedConfig, func(client.BootEvent) error) (*hvf.ManagedSession, error)) (inst Instance, err error) {
	if b == nil {
		return nil, fmt.Errorf("runtime backend is not configured")
	}
	displayName := def.Profile.Name
	if displayName == "" {
		displayName = def.BootKind
	}
	if req.Network != nil && !req.Network.Enabled {
		return nil, fmt.Errorf("%s runtime requires virtio-net for the managed control channel", displayName)
	}
	if req.CPUs > 1 {
		return nil, fmt.Errorf("%s runtime currently supports one vCPU", displayName)
	}
	if req.NestedVirt {
		return nil, fmt.Errorf("%s runtime does not support nested virtualization", displayName)
	}
	if def.BuildArtifact == nil {
		return nil, fmt.Errorf("%s runtime root builder is not configured", displayName)
	}
	network, err := newDarwinARM64NetworkRuntime(managedBSDNetworkConfig(req.Network))
	if err != nil {
		return nil, err
	}
	if network == nil {
		return nil, fmt.Errorf("%s runtime requires virtio-net for the managed control channel", displayName)
	}
	defer func() {
		if err != nil && network != nil {
			_ = network.Close()
		}
	}()
	networkSpec := machine.NetworkSpec{
		GuestIPv4:   network.GuestAddress(),
		GatewayIPv4: "10.42.0.1",
		DNSIPv4:     "10.42.0.1",
		Hostname:    def.Hostname,
		Interface:   def.Interface,
		MAC:         network.mac.String(),
	}
	artifact, err := def.BuildArtifact(ctx, def.CacheDir, networkSpec)
	if err != nil {
		return nil, err
	}
	session, err := start(ctx, hvf.OpenBSDManagedConfig{
		Kernel:    artifact.Kernel,
		Root:      artifact.RootBlock,
		MemoryMB:  req.MemoryMB,
		Dmesg:     req.Dmesg,
		GuestIPv4: network.ip,
		GuestMAC:  network.mac,
		NetDevice: network.Device(),
		NetStack:  network.stack,
	}, onEvent)
	if err != nil {
		_ = artifact.Close()
		return nil, err
	}
	nfsServer, err := startBSDNFSServer(network.stack)
	if err != nil {
		_ = session.Close()
		_ = artifact.Close()
		return nil, err
	}
	base := newDarwinBSDInstance(darwinBSDInstanceConfig{
		OSName:       displayName,
		Session:      session,
		CloseRuntime: closeBSDNFSRuntime(nfsServer, artifact.Close),
		Root:         artifact.RootFS,
		BaseEnv:      builtin.EffectiveExecEnv(nil, nil, false),
		WorkDir:      "/",
		Network:      network,
		Capabilities: def.Profile.Caps,
		Env:          builtin.EffectiveExecEnv,
	})
	return wrapBSDNFSInstance(ctx, displayName, base, nfsServer, req.Shares)
}

func builtinGuestCapabilities(image string) guestCapabilities {
	profile, ok := builtinGuestForImage(image)
	if !ok {
		return guestCapabilities{}
	}
	return profile.Caps
}
