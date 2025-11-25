//go:build !darwin || !arm64

package hvf

import "github.com/tinyrange/cc/internal/hv"

func Open() (hv.Hypervisor, error) {
	return nil, hv.ErrHypervisorUnsupported
}
