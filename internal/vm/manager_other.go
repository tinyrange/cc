//go:build !darwin || !arm64

package vm

import (
	"context"
	"os"
	"strings"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/kernel/alpine"
	"j5.nz/cc/internal/oci"
)

func NewRuntimeManager(kernel *alpine.Manager, images *oci.Store, guestInitCache string, rootCache string, worker bool) *Manager {
	backend := NewRuntimeBackend(kernel, images, guestInitCache)
	if worker || strings.TrimSpace(os.Getenv(sidecarDisableEnv)) != "" || strings.TrimSpace(os.Getenv(sidecarEnableEnv)) == "" {
		return NewManagerWithBackend(backend)
	}
	mgr := NewManagerWithHosts(
		newInProcessVMHost(backend, HostCapabilities),
		NewLocalSidecarVMHost(rootCache, kernel, images, guestInitCache),
	)
	mgr.capabilities = func() client.CapabilitiesResponse {
		caps := HostCapabilities()
		hostCaps := mgr.host.HostCapabilities(context.Background())
		caps.Backend = hostCaps.Backend
		caps.MaxInstances = hostCaps.MaxVMs
		caps.Notes = append(caps.Notes, "local sidecar worker processes are enabled")
		return caps
	}
	return mgr
}
