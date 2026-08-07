//go:build linux && amd64

// Package kernelvm exposes CC's minimal direct-kernel test surface without
// leaking its internal KVM implementation.
package kernelvm

import (
	"context"
	"fmt"
	"strings"

	"j5.nz/cc/internal/hv/kvm"
)

type Options struct {
	MemoryMB uint64
	Marker   string
}

func RunELF(ctx context.Context, kernel []byte, opts Options) (string, error) {
	if len(kernel) == 0 {
		return "", fmt.Errorf("kernel ELF is empty")
	}
	if strings.TrimSpace(opts.Marker) == "" {
		return "", fmt.Errorf("serial marker is required")
	}
	if opts.MemoryMB == 0 {
		opts.MemoryMB = 64
	}
	return kvm.BootFreestandingELFToMarker(ctx, kernel, opts.MemoryMB, opts.Marker)
}
