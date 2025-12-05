package syscallnum

import (
	"fmt"

	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/linux/defs"
	amd64defs "github.com/tinyrange/cc/internal/linux/defs/amd64"
	arm64defs "github.com/tinyrange/cc/internal/linux/defs/arm64"
)

// Number returns the architecture-specific syscall number for the given logical
// syscall identifier.
func Number(arch hv.CpuArchitecture, sc defs.Syscall) (int, error) {
	switch arch {
	case hv.ArchitectureX86_64:
		if n, ok := amd64defs.SyscallMap[sc]; ok {
			return int(n), nil
		}
		return 0, fmt.Errorf("syscallnum: unknown syscall %v for amd64", sc)
	case hv.ArchitectureARM64:
		if n, ok := arm64defs.SyscallMap[sc]; ok {
			return int(n), nil
		}
		return 0, fmt.Errorf("syscallnum: unknown syscall %v for arm64", sc)
	default:
		return 0, fmt.Errorf("syscallnum: unsupported architecture %v", arch)
	}
}

// MustNumber panics on lookup failure. Useful in tests where failure should
// abort immediately.
func MustNumber(arch hv.CpuArchitecture, sc defs.Syscall) int {
	num, err := Number(arch, sc)
	if err != nil {
		panic(err)
	}
	return num
}
