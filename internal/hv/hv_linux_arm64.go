//go:build linux && arm64

package hv

import (
	"j5.nz/cc/internal/hv/kvm"
)

func Supports() error {
	_, err := kvm.Probe()
	return err
}
