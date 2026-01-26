//go:build (darwin || linux) && arm64

package arm64

import (
	"testing"

	arm64asm "github.com/tinyrange/cc/internal/asm/arm64"
	"github.com/tinyrange/cc/internal/ir"
)

// compileAndRunProgram compiles and runs a multi-method program.
func compileAndRunProgram(t *testing.T, prog *ir.Program, args ...any) uintptr {
	t.Helper()

	asmProg, err := BuildStandaloneProgram(prog)
	if err != nil {
		t.Fatalf("BuildStandaloneProgram failed: %v", err)
	}

	fn, cleanup, err := arm64asm.PrepareAssemblyWithArgs(asmProg.Bytes(), asmProg.Relocations(), asmProg.BSSSize())
	if err != nil {
		t.Fatalf("PrepareAssemblyWithArgs failed: %v", err)
	}
	defer cleanup()

	return fn.Call(args...)
}
