//go:build windows && arm64

package hv

import "j5.nz/cc/internal/hv/whp"

func Supports() error {
	return whp.Supports()
}

func NestedVirtualizationSupported() (bool, error) {
	return false, nil
}
