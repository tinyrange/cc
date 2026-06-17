//go:build (!darwin || !arm64) && (!linux || (!arm64 && !amd64)) && (!windows || !amd64)

package vm

import (
	"j5.nz/cc/internal/kernel/alpine"
	"j5.nz/cc/internal/oci"
	vmhost "j5.nz/cc/internal/vm/host"
)

func NewRuntimeBackend(kernel *alpine.Manager, images *oci.Store, guestInitCache string) Backend {
	_ = kernel
	_ = images
	_ = guestInitCache
	return vmhost.UnsupportedBackend{}
}
