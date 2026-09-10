//go:build !darwin || !arm64

package hypervisor

import (
	"context"
	"fmt"
)

func NewARM64(context.Context) (ARM64, error) {
	return nil, fmt.Errorf("ARM64 native continuation requires darwin/arm64")
}
