//go:build (darwin || linux) && arm64

package arm64

import (
	"github.com/tinyrange/cc/internal/asm"
	arm64asm "github.com/tinyrange/cc/internal/asm/arm64"
)

// PrepareNativeExecution prepares a compiled program for native execution on ARM64.
// Returns a callable function, a cleanup function, and an error.
func (backend) PrepareNativeExecution(prog asm.Program) (asm.NativeFunc, func(), error) {
	return arm64asm.PrepareAssemblyWithArgs(prog.Bytes(), prog.Relocations(), prog.BSSSize())
}
