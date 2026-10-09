// Package sandbox exposes narrow in-process Linux VM integration. It is a
// trusted host library, not an agent-facing image catalog or policy engine.
// Callers must enforce admission, ownership, leases and host resource limits.
package sandbox

import (
	"context"
	"errors"
	"j5.nz/cc/client"
	"j5.nz/cc/internal/virtio"
	vmhost "j5.nz/cc/internal/vm/host"
	"net"
	"sync"
)

type FSBackend = virtio.FSBackend
type FuseAttr = virtio.FuseAttr
type FSCachePolicy = virtio.FSCachePolicy
type ShareMount = virtio.ShareMount

type Config struct {
	ID string
	// Image is a trusted administrator's OCI reference pinned by digest.
	Image    string
	Platform string
	MemoryMB uint64
	CPUs     int
	Internet bool
	Mounts   []ShareMount
	// Init overrides cc's init with a caller-supplied static Linux amd64 ELF.
	Init []byte
	// SandboxProtocol requires Init and is verified in the booted guest.
	SandboxProtocol int
}
type Identity struct{ Reference, Digest, Platform string }
type Progress struct {
	Stage   string
	Message string
	Image   *client.ProgressEvent
}
type Sandbox struct {
	mu       sync.Mutex
	closing  bool
	instance vmhost.Instance
	identity Identity
}

func (s *Sandbox) Identity() Identity { return s.identity }
func (s *Sandbox) current() (vmhost.Instance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing || s.instance == nil {
		return nil, errors.New("sandbox closed")
	}
	return s.instance, nil
}

// ExecStream forwards cc's guest-agent requests. Explicit process modes require
// the matching sandbox protocol; empty mode retains cc's legacy semantics.
func (s *Sandbox) ExecStream(ctx context.Context, req client.ExecRequest, inputs <-chan client.ExecInput, onEvent func(client.ExecEvent) error) error {
	i, err := s.current()
	if err != nil {
		return err
	}
	if req.User == "" {
		req.User = "root"
	}
	// Commands can live in the writable root or live draft, not just the image.
	req.SkipResolve = true
	return i.ExecStream(ctx, req, inputs, onEvent)
}
func (s *Sandbox) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closing = true
	if s.instance == nil {
		return nil
	}
	if err := s.instance.Close(); err != nil {
		return err
	}
	s.instance = nil
	return nil
}
func (s *Sandbox) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	i, err := s.current()
	if err != nil {
		return nil, err
	}
	dial, ok := i.(interface {
		DialGuestContext(context.Context, string, string) (net.Conn, error)
	})
	if !ok {
		return nil, errors.New("independent guest dialer unavailable")
	}
	return dial.DialGuestContext(ctx, network, address)
}
func (s *Sandbox) GuestIPv4() string {
	i, err := s.current()
	if err != nil {
		return ""
	}
	if n, ok := i.(interface{ NetworkIPv4() string }); ok {
		return n.NetworkIPv4()
	}
	return ""
}
