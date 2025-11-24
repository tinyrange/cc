//go:build !((linux && amd64) || (linux && arm64) || (windows && amd64))

package factory

import "github.com/tinyrange/cc/internal/hv"

func Open() (hv.Hypervisor, error) {
	return nil, hv.ErrHypervisorUnsupported
}
