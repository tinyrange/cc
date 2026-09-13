//go:build !linux || !amd64

package hypervisor

import (
	"context"
	"fmt"
)

func NewX86(context.Context) (X86, error) {
	return nil, fmt.Errorf("cc x86 native execution requires Linux/amd64 KVM")
}
