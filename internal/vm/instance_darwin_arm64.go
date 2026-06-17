//go:build darwin && arm64

package vm

import (
	"context"
	"strings"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/hv/hvf"
	"j5.nz/cc/internal/imagefs"
	managedguest "j5.nz/cc/internal/managed/guest"
	"j5.nz/cc/internal/oci"
	"j5.nz/cc/internal/virtio"
	"j5.nz/cc/internal/vmruntime"
)

type darwinInstance struct {
	*managedInstanceCore
	session   *hvf.ContainerSession
	network   *darwinNetworkRuntime
	imageName string
}

func newDarwinInstance(session *hvf.ContainerSession, network *darwinNetworkRuntime, imageName string) *darwinInstance {
	inst := &darwinInstance{
		session:   session,
		network:   network,
		imageName: imageName,
	}
	inst.managedInstanceCore = newDarwinManagedCore(session)
	return inst
}

func newDarwinManagedCore(session *hvf.ContainerSession) *managedInstanceCore {
	if session == nil {
		return nil
	}
	metadata := session.ManagedMetadata()
	return &managedInstanceCore{
		osName:         "Linux",
		session:        session,
		root:           metadata.Root,
		baseEnv:        metadata.BaseEnv,
		workDir:        metadata.WorkDir,
		caps:           managedguest.LinuxProfile.Caps,
		env:            mergeDarwinManagedEnv,
		missingRootErr: "running instance does not have a default image root filesystem",
		markResolved:   true,
	}
}

func (i *darwinInstance) ManagedCapabilities() guestCapabilities {
	return i.managedCore().ManagedCapabilities()
}

func (i *darwinInstance) managedCore() *managedInstanceCore {
	if i == nil {
		return nil
	}
	if i.managedInstanceCore != nil {
		return i.managedInstanceCore
	}
	return newDarwinManagedCore(i.session)
}

func (i *darwinInstance) AddShare(ctx context.Context, share client.ShareMount) error {
	if i == nil {
		return addDelegatedRuntimeShare(ctx, nil, share, "runtime shares")
	}
	return addDelegatedRuntimeShare(ctx, i.session, share, "runtime shares")
}

func (i *darwinInstance) AddImage(ctx context.Context, mountPath string, image *oci.Image) error {
	if i == nil {
		return addDelegatedRuntimeImage(ctx, nil, mountPath, image)
	}
	return addDelegatedRuntimeImage(ctx, i.session, mountPath, image)
}

func (i *darwinInstance) AddPortForward(ctx context.Context, forward client.PortForward) error {
	if i == nil {
		return addManagedNetworkPortForwardWithFallback(ctx, nil, forward, nil)
	}
	var runtime *networkRuntime
	if i.network != nil {
		runtime = i.network.networkRuntime
	}
	return addManagedNetworkPortForwardWithFallback(ctx, runtime, forward, i.session)
}

func (i *darwinInstance) AllowServiceProxyPort(ctx context.Context, port int) error {
	if i == nil || i.network == nil {
		return allowManagedNetworkServiceProxyPort(ctx, nil, port)
	}
	return allowManagedNetworkServiceProxyPort(ctx, i.network.networkRuntime, port)
}

func (i *darwinInstance) VirtioFSStats() []virtio.FSStats {
	if i == nil || i.session == nil {
		return nil
	}
	return i.session.VirtioFSStats()
}

func (i *darwinInstance) Exec(ctx context.Context, req client.ExecRequest) (client.ExecResponse, error) {
	return i.managedCore().Exec(ctx, req)
}

func (i *darwinInstance) ExecStream(
	ctx context.Context,
	req client.ExecRequest,
	inputs <-chan client.ExecInput,
	onEvent func(client.ExecEvent) error,
) error {
	return i.managedCore().ExecStream(ctx, req, inputs, onEvent)
}

func (i *darwinInstance) Flush(ctx context.Context) error {
	return i.managedCore().Flush(ctx)
}

func (i *darwinInstance) ConsoleHistory(ctx context.Context) (string, error) {
	return i.managedCore().ConsoleHistory(ctx)
}

func (i *darwinInstance) RootSnapshot() (imagefs.Directory, error) {
	if i == nil || i.session == nil {
		return managedRootSnapshot(nil, "")
	}
	return managedRootSnapshotWithCapabilities("Linux", i.ManagedCapabilities(), i.session, "")
}

func (i *darwinInstance) SnapshotImage(imageName string) (imagefs.Directory, error) {
	if i == nil || i.session == nil {
		return managedRootSnapshot(nil, "")
	}
	if strings.TrimSpace(i.imageName) == imageName {
		return i.RootSnapshot()
	}
	return managedImageSnapshotWithCapabilities("Linux", i.ManagedCapabilities(), i.session, imageName, imageMountPath(imageName))
}

func (i *darwinInstance) Wait() error {
	if i == nil {
		return nil
	}
	return i.managedCore().Wait()
}

func (i *darwinInstance) Close() error {
	if i == nil {
		return nil
	}
	return closeManagedSessionWithNetwork(i.session, i.network)
}

func (i *darwinInstance) NetworkIPv4() string {
	if i == nil || i.network == nil {
		return ""
	}
	return managedNetworkIPv4(i.network.networkRuntime, "")
}

func darwinContainerSession(inst Instance) (*hvf.ContainerSession, bool) {
	if session, ok := inst.(*hvf.ContainerSession); ok {
		return session, true
	}
	if wrapped, ok := inst.(*darwinInstance); ok && wrapped.session != nil {
		return wrapped.session, true
	}
	return nil, false
}

func networkDeviceDarwin(network *darwinNetworkRuntime) *virtio.Net {
	if network == nil {
		return nil
	}
	return network.dev
}

func mergeDarwinManagedEnv(base, overrides []string, replace bool) []string {
	if replace {
		return vmruntime.WithDefaultEnv(overrides)
	}
	return vmruntime.WithDefaultEnv(vmruntime.MergeEnv(base, overrides))
}
