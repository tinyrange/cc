//go:build !linux || !amd64

package kernelvm

import (
	"context"
	"fmt"
)

func runBSD(context.Context, []byte, BSDOptions) (string, error) {
	return "", fmt.Errorf("direct BSD kernel boot requires a linux/amd64 KVM host")
}
