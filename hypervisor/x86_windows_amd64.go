//go:build windows && amd64

package hypervisor

import (
	"context"
	"j5.nz/cc/internal/hv/whp"
)

func NewX86(ctx context.Context) (X86, error) {
	cpus, err := NewX86WithCPUs(ctx, 1)
	if err != nil {
		return nil, err
	}
	return cpus[0], nil
}

func NewX86WithCPUs(ctx context.Context, count int) ([]X86, error) {
	native, err := whp.NewNativeCPUs(ctx, count)
	if err != nil {
		return nil, err
	}
	cpus := make([]X86, len(native))
	for i, cpu := range native {
		cpus[i] = cpu
	}
	return cpus, nil
}
