//go:build !darwin || !arm64

package vm

import (
	"j5.nz/cc/internal/kernel/alpine"
	"j5.nz/cc/internal/oci"
)

func NewRuntimeManager(kernel *alpine.Manager, images *oci.Store, guestInitCache string, rootCache string, worker bool) *Manager {
	_ = rootCache
	_ = worker
	return NewManagerWithBackend(NewRuntimeBackend(kernel, images, guestInitCache))
}
