//go:build windows && amd64

package factory

import (
	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/hv/whp"
)

func Open() (hv.Hypervisor, error) {
	return whp.Open()
}
