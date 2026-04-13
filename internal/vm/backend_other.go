//go:build !darwin || !arm64

package vm

import (
	"j5.nz/cc/internal/kernel/alpine"
	"j5.nz/cc/internal/oci"
)

func NewRuntimeBackend(kernel *alpine.Manager, images *oci.Store) Backend {
	_ = kernel
	_ = images
	return unsupportedBackend{}
}
