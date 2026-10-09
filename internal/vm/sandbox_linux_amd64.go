//go:build linux && amd64

package vm

import (
	"context"
	"errors"
	"j5.nz/cc/client"
	"j5.nz/cc/internal/virtio"
	"net"
)

type sandboxInitKey struct{}
type sandboxInitOverride struct {
	payload  []byte
	protocol int
}

// WithSandboxInit supplies an immutable init payload for this boot only.
func WithSandboxInit(ctx context.Context, payload []byte, protocol int) context.Context {
	return context.WithValue(ctx, sandboxInitKey{}, sandboxInitOverride{append([]byte(nil), payload...), protocol})
}

func sandboxInitFromContext(ctx context.Context) sandboxInitOverride {
	v, _ := ctx.Value(sandboxInitKey{}).(sandboxInitOverride)
	return v
}

// StartSandbox attaches caller-owned filesystem capabilities directly to the
// in-process Linux backend. It does not expose directory shares or a daemon.
func StartSandbox(ctx context.Context, backend Backend, req client.CreateInstanceRequest, shares []virtio.ShareMount, progress func(client.BootEvent) error) (Instance, error) {
	return backend.StartStream(withHostMounts(ctx, shares), req, progress)
}

// DialGuestContext can only send TCP into this VM's independent network stack.
// It cannot dial a host socket, resolve host DNS or select another guest.
func (i *linuxInstance) DialGuestContext(ctx context.Context, network, address string) (net.Conn, error) {
	if i == nil || i.managedInstance == nil {
		return nil, errors.New("guest unavailable")
	}
	i.netMu.RLock()
	n := i.managedInstance.network
	i.netMu.RUnlock()
	if n == nil || n.networkRuntime == nil {
		return nil, errors.New("guest network unavailable")
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.Equal(n.ip) {
		return nil, errors.New("destination is not this guest")
	}
	return n.stack.DialInternalContext(ctx, network, net.JoinHostPort(n.ip.String(), port))
}
