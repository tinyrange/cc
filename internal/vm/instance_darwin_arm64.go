//go:build darwin && arm64

package vm

import (
	"context"
	"fmt"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/hv/hvf"
	"j5.nz/cc/internal/imagefs"
	"j5.nz/cc/internal/virtio"
)

type darwinInstance struct {
	session *hvf.ContainerSession
	network *darwinNetworkRuntime
}

func (i *darwinInstance) AddShare(ctx context.Context, share client.ShareMount) error {
	return i.session.AddShare(ctx, share)
}

func (i *darwinInstance) AddPortForward(ctx context.Context, forward client.PortForward) error {
	_ = ctx
	if i == nil || i.network == nil {
		return i.session.AddPortForward(ctx, forward)
	}
	return i.network.AddPortForward(forward)
}

func (i *darwinInstance) VirtioFSStats() []virtio.FSStats {
	if i == nil || i.session == nil {
		return nil
	}
	return i.session.VirtioFSStats()
}

func (i *darwinInstance) Exec(ctx context.Context, req client.ExecRequest) (client.ExecResponse, error) {
	return i.session.Exec(ctx, req)
}

func (i *darwinInstance) ExecStream(
	ctx context.Context,
	req client.ExecRequest,
	inputs <-chan client.ExecInput,
	onEvent func(client.ExecEvent) error,
) error {
	return i.session.ExecStream(ctx, req, inputs, onEvent)
}

func (i *darwinInstance) Flush(ctx context.Context) error {
	if i == nil || i.session == nil {
		return fmt.Errorf("instance is not running")
	}
	return i.session.Flush(ctx)
}

func (i *darwinInstance) RootSnapshot() (imagefs.Directory, error) {
	if i == nil || i.session == nil {
		return nil, fmt.Errorf("root filesystem cannot be snapshotted")
	}
	return i.session.RootSnapshot()
}

func (i *darwinInstance) Wait() error {
	return i.session.Wait()
}

func (i *darwinInstance) Close() error {
	var err error
	if i.session != nil {
		err = i.session.Close()
	}
	if i.network != nil {
		if networkErr := i.network.Close(); err == nil {
			err = networkErr
		}
	}
	return err
}

func (i *darwinInstance) NetworkIPv4() string {
	if i == nil || i.network == nil {
		return ""
	}
	return darwinNetworkGuestAddress(i.network)
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
