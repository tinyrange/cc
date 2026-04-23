//go:build (!darwin || !arm64) && (!linux || (!arm64 && !amd64))

package vm

import (
	"j5.nz/cc/internal/kernel/alpine"
	"j5.nz/cc/internal/oci"
)

func NewRuntimeBackend(kernel *alpine.Manager, images *oci.Store, guestInitCache string) Backend {
	_ = kernel
	_ = images
	_ = guestInitCache
	return unsupportedBackend{}
}
