package factory

import (
	"fmt"

	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/hv/riscv"
)

// NewWithArchitecture selects a hypervisor backend for the requested guest
// architecture. Host-accelerated backends are used for native architectures,
// while the RISC-V path uses the built-in ccvm interpreter.
func NewWithArchitecture(arch hv.CpuArchitecture) (hv.Hypervisor, error) {
	switch arch {
	case hv.ArchitectureRISCV64:
		return riscv.Open()
	case hv.ArchitectureX86_64, hv.ArchitectureARM64:
		return Open()
	default:
		return nil, fmt.Errorf("unsupported architecture %q", arch)
	}
}

// OpenWithArchitecture mirrors NewWithArchitecture but treats an invalid
// architecture as "use the host default".
func OpenWithArchitecture(arch hv.CpuArchitecture) (hv.Hypervisor, error) {
	if arch == hv.ArchitectureInvalid {
		return Open()
	}
	return NewWithArchitecture(arch)
}
