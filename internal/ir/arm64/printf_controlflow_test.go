//go:build arm64

package arm64

import (
	"os"
	"testing"

	"github.com/tinyrange/cc/internal/ir"
)

// TestPrintf_DumpBinary dumps the binary for a nested if with Printf pattern
// for manual disassembly.
func TestPrintf_DumpBinary(t *testing.T) {
	flags := ir.Var("flags")
	captureStdout := ir.Var("captureStdout")
	pipeResult := ir.Var("pipeResult")
	marker := ir.Var("marker")

	method := ir.Method{
		ir.Assign(flags, ir.Int64(0x1)),
		ir.Assign(marker, ir.Int64(0)),
		ir.Assign(pipeResult, ir.Int64(0)),

		ir.Assign(captureStdout, ir.Op(ir.OpAnd, flags, ir.Int64(0x1))),

		// Outer if
		ir.If(ir.IsNotEqual(captureStdout, ir.Int64(0)), ir.Block{
			ir.Assign(pipeResult, ir.Int64(5)),

			// Nested if
			ir.If(ir.IsGreaterOrEqual(pipeResult, ir.Int64(0)), ir.Block{
				ir.Printf("nested\n"),
			}),
		}),

		// After outer if - this MUST execute
		ir.Assign(marker, ir.Int64(1)),
		ir.Printf("done\n"),

		ir.Return(marker),
	}

	prog := &ir.Program{
		Entrypoint: "main",
		Methods: map[string]ir.Method{
			"main": method,
		},
	}

	asmProg, err := BuildStandaloneProgram(prog)
	if err != nil {
		t.Fatalf("BuildStandaloneProgram failed: %v", err)
	}

	binary := asmProg.Bytes()
	t.Logf("Binary size: %d bytes", len(binary))

	outPath := "/tmp/printf_nested_if.bin"
	if err := os.WriteFile(outPath, binary, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	t.Logf("Wrote binary to %s", outPath)
	t.Logf("Disassemble with: llvm-objdump -d --triple=aarch64 %s", outPath)
}
