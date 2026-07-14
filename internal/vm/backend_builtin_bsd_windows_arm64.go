//go:build windows && arm64

package vm

import (
	"context"
	"fmt"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/hv/whp"
	"j5.nz/cc/internal/imagefs"
	managedguest "j5.nz/cc/internal/managed/guest"
	"j5.nz/cc/internal/managed/machine"
	managedruntime "j5.nz/cc/internal/managed/runtime"
	managedsession "j5.nz/cc/internal/managed/session"
	"j5.nz/cc/internal/nfs"
	"j5.nz/cc/internal/vm/builtin"
	hostmanaged "j5.nz/cc/internal/vm/host/managed"
	"j5.nz/cc/internal/vm/netstate"
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
		return nil, fmt.Errorf("managed guest profile %q is not supported on windows/arm64", profile.Canonical)
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
	return b.startBSDManagedStream(ctx, req, onEvent, builtin.OpenBSDDefinitionForArch(b.guestInitCache, "arm64"))
}

func managedBSDNetworkConfig(cfg *client.NetworkConfig) *client.NetworkConfig {
	if cfg == nil {
		return &client.NetworkConfig{Enabled: true, AllowInternet: true}
	}
	copyCfg := *cfg
	return &copyCfg
}

func (b *runtimeBackend) startFreeBSDStream(ctx context.Context, req client.CreateInstanceRequest, onEvent func(client.BootEvent) error) (Instance, error) {
	return b.startBSDManagedStream(ctx, req, onEvent, builtin.FreeBSDDefinitionForArch(b.guestInitCache, "arm64"))
}

func (b *runtimeBackend) startNetBSDStream(ctx context.Context, req client.CreateInstanceRequest, onEvent func(client.BootEvent) error) (Instance, error) {
	return b.startBSDManagedStream(ctx, req, onEvent, builtin.NetBSDDefinitionForArch(b.guestInitCache, "evbarm-aarch64"))
}

func (b *runtimeBackend) startBSDManagedStream(ctx context.Context, req client.CreateInstanceRequest, onEvent func(client.BootEvent) error, def builtin.BSDDefinition) (inst Instance, err error) {
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
	network, err := newWindowsARM64NetworkRuntime(managedBSDNetworkConfig(req.Network))
	if err != nil {
		return nil, err
	}
	if network == nil {
		return nil, fmt.Errorf("%s runtime requires virtio-net for the managed control channel", displayName)
	}
	defer func() {
		if err != nil {
			_ = network.Close()
		}
	}()
	networkSpec := machine.NetworkSpec{
		GuestIPv4:   network.GuestAddress(),
		GatewayIPv4: "10.42.0.1",
		GatewayMAC:  defaultGatewayMAC,
		DNSIPv4:     "10.42.0.1",
		Hostname:    def.Hostname,
		Interface:   def.Interface,
		MAC:         network.mac.String(),
	}
	artifact, err := def.BuildArtifact(ctx, def.CacheDir, networkSpec)
	if err != nil {
		return nil, err
	}
	started, err := (managedruntime.Service{}).Start(ctx, managedruntime.StartRequest{
		Profile: def.Profile,
		Host:    whp.Host{},
		Spec: machine.Spec{
			Guest:    def.Profile.Name,
			Arch:     "arm64",
			MemoryMB: req.MemoryMB,
			Dmesg:    req.Dmesg,
			Boot:     machine.BootSpec{Kind: def.BootKind},
			Network:  &networkSpec,
		},
		Artifact: artifact,
		Attachments: whp.BSDManagedAttachments{
			GuestIPv4: network.ip,
			GuestMAC:  network.mac,
			NetDevice: network.Device(),
			NetStack:  network.stack,
		},
	}, onEvent)
	if err != nil {
		_ = artifact.Close()
		return nil, err
	}
	nfsServer := nfs.New(network.stack)
	if err := nfsServer.Start(); err != nil {
		_ = started.Session.Close()
		_ = artifact.Close()
		return nil, err
	}
	inst = newWindowsBSDInstance(windowsBSDInstanceConfig{
		OSName:       displayName,
		Session:      started.Session,
		CloseRuntime: artifact.Close,
		Root:         started.Artifact.RootFS,
		Network:      network,
		NFS:          nfsServer,
		Capabilities: def.Profile.Caps,
	})
	for _, share := range req.Shares {
		if err := inst.AddShare(ctx, share); err != nil {
			_ = inst.Close()
			return nil, err
		}
	}
	return inst, nil
}

func builtinGuestCapabilities(image string) guestCapabilities {
	profile, ok := builtinGuestForImage(image)
	if !ok {
		return guestCapabilities{}
	}
	return profile.Caps
}

type windowsBSDInstanceConfig struct {
	OSName       string
	Session      managedsession.Session
	CloseRuntime func() error
	Root         imagefs.Directory
	Network      *windowsNetworkRuntime
	NFS          *nfs.Server
	Capabilities guestCapabilities
}

type windowsBSDInstance struct {
	*managedInstanceCore
	session      managedsession.Session
	closeRuntime func() error
	network      *windowsNetworkRuntime
	nfs          *nfs.Server
	osName       string
}

func newWindowsBSDInstance(cfg windowsBSDInstanceConfig) *windowsBSDInstance {
	return &windowsBSDInstance{
		managedInstanceCore: hostmanaged.NewCore(hostmanaged.Config{
			OSName:       cfg.OSName,
			Session:      cfg.Session,
			Root:         cfg.Root,
			BaseEnv:      builtin.EffectiveExecEnv(nil, nil, false),
			WorkDir:      "/",
			Capabilities: cfg.Capabilities,
			Env:          builtin.EffectiveExecEnv,
		}),
		session:      cfg.Session,
		closeRuntime: cfg.CloseRuntime,
		network:      cfg.Network,
		nfs:          cfg.NFS,
		osName:       cfg.OSName,
	}
}

func (i *windowsBSDInstance) AddShare(ctx context.Context, share client.ShareMount) error {
	if i == nil || i.nfs == nil {
		return i.managedInstanceCore.AddShare(ctx, share)
	}
	exp, err := i.nfs.AddShare(share)
	if err != nil {
		return err
	}
	return nfs.MountShare(ctx, i.osName, i.Exec, exp)
}

func (i *windowsBSDInstance) AddPortForward(ctx context.Context, forward client.PortForward) error {
	if i == nil || i.network == nil {
		return netstate.AddManagedNetworkPortForward(ctx, nil, forward)
	}
	return netstate.AddManagedNetworkPortForward(ctx, i.network.networkRuntime, forward)
}

func (i *windowsBSDInstance) AllowServiceProxyPort(ctx context.Context, port int) error {
	if i == nil || i.network == nil {
		return netstate.AllowManagedNetworkServiceProxyPort(ctx, nil, port)
	}
	return netstate.AllowManagedNetworkServiceProxyPort(ctx, i.network.networkRuntime, port)
}

func (i *windowsBSDInstance) NetworkIPv4() string {
	if i == nil || i.network == nil {
		return ""
	}
	return netstate.IPv4(i.network.networkRuntime, "")
}

func (i *windowsBSDInstance) Close() error {
	if i == nil {
		return nil
	}
	return hostmanaged.CloseSession(i.session, func() error {
		if i.nfs != nil {
			return i.nfs.Close()
		}
		return nil
	}, func() error {
		if i.network != nil {
			return i.network.Close()
		}
		return nil
	}, i.closeRuntime)
}
