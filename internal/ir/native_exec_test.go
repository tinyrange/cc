//go:build ((darwin || linux) && arm64) || (linux && amd64)

package ir_test

import (
	"testing"

	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/ir"

	// Import backend packages to trigger init() registration
	_ "github.com/tinyrange/cc/internal/ir/amd64"
	_ "github.com/tinyrange/cc/internal/ir/arm64"
)

// compileAndRun compiles and runs a single method on the native architecture.
func compileAndRun(t *testing.T, method ir.Method, args ...any) uintptr {
	t.Helper()

	prog := &ir.Program{
		Entrypoint: "main",
		Methods: map[string]ir.Method{
			"main": method,
		},
	}

	return compileAndRunProgram(t, prog, args...)
}

// compileAndRunProgram compiles and runs a multi-method program on the native architecture.
func compileAndRunProgram(t *testing.T, prog *ir.Program, args ...any) uintptr {
	t.Helper()

	backend, err := ir.LookupNativeBackend(hv.ArchitectureNative)
	if err != nil {
		t.Fatalf("LookupNativeBackend failed: %v", err)
	}

	asmProg, err := backend.BuildStandaloneProgram(prog)
	if err != nil {
		t.Fatalf("BuildStandaloneProgram failed: %v", err)
	}

	fn, cleanup, err := backend.PrepareNativeExecution(asmProg)
	if err != nil {
		t.Fatalf("PrepareNativeExecution failed: %v", err)
	}
	defer cleanup()

	return fn.Call(args...)
}
