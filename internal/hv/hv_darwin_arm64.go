//go:build darwin && arm64

package hv

import "j5.nz/cc/internal/hv/hvf"

func Supports() error {
	return nil
}

func NestedVirtualizationSupported() (bool, error) {
	return hvf.NestedVirtualizationSupported()
}
