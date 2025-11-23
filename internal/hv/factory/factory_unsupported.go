//go:build !linux || !amd64

package factory

import "github.com/tinyrange/cc/internal/hv"

func Open() (hv.Hypervisor, error) {
	return nil, hv.ErrHypervisorUnsupported
}
