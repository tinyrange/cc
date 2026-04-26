//go:build windows && amd64

package hv

import "j5.nz/cc/internal/hv/whp"

func Supports() error {
	return whp.Supports()
}
