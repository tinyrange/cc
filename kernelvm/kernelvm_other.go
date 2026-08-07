//go:build !linux || !amd64

package kernelvm

import (
	"context"
	"fmt"
)

type Options struct {
	MemoryMB uint64
	Marker   string
}

func RunELF(context.Context, []byte, Options) (string, error) {
	return "", fmt.Errorf("direct freestanding ELF boot currently requires a linux/amd64 KVM host")
}
