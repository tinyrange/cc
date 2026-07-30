//go:build !darwin

package virgl

import (
	"errors"

	"j5.nz/cc/internal/virtio"
)

func NewHostRenderer() (virtio.GPURenderer, error) {
	return nil, errors.New("first-party VirGL host rendering is currently available on Darwin only")
}

func NewHostRendererWithShareGroup(uintptr, uintptr) (virtio.GPURenderer, error) {
	return NewHostRenderer()
}
