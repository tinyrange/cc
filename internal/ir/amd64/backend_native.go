//go:build linux && amd64

package amd64

import (
	"github.com/tinyrange/cc/internal/asm"
	amd64asm "github.com/tinyrange/cc/internal/asm/amd64"
)

// PrepareNativeExecution prepares a compiled program for native execution on AMD64.
// Returns a callable function, a cleanup function, and an error.
func (backend) PrepareNativeExecution(prog asm.Program) (asm.NativeFunc, func(), error) {
	return amd64asm.PrepareAssemblyWithArgs(prog.Bytes(), prog.Relocations(), prog.BSSSize())
}
