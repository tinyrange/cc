//go:build darwin && arm64

package factory

import (
	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/hv/hvf"
)

func Open() (hv.Hypervisor, error) {
	return hvf.Open()
}
