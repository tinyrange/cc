//go:build linux && arm64

package factory

import (
	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/hv/kvm"
)

func Open() (hv.Hypervisor, error) {
	return kvm.Open()
}
