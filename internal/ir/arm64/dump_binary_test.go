package arm64

import (
	"os"
	"testing"

	arm64asm "github.com/tinyrange/cc/internal/asm/arm64"
	"github.com/tinyrange/cc/internal/ir"
)

// TestDumpBinaryForDisassembly compiles a method that mimics the init_source.go
// pattern and dumps the binary to a file for manual disassembly.
//
// Run with: ./tools/build.go -test ./internal/ir/arm64 -run DumpBinary -v
// Then: llvm-objdump -d --triple=aarch64 /tmp/capture_pattern.bin
func TestDumpBinaryForDisassembly(t *testing.T) {
	flags := ir.Var("flags")
	captureStdout := ir.Var("captureStdout")
	pipeResult := ir.Var("pipeResult")
	pipeRead := ir.Var("pipeRead")
	pipeWrite := ir.Var("pipeWrite")
	savedStdout := ir.Var("savedStdout")
	marker1 := ir.Var("marker1")
	marker2 := ir.Var("marker2")
	tmp := ir.Var("tmp")

	// This mimics the init_source.go pattern without syscalls
	// Using simple operations instead of syscalls to test the control flow
	method := ir.Method{
		ir.Assign(flags, ir.Int64(0x1)),
		ir.Assign(captureStdout, ir.Op(ir.OpAnd, flags, ir.Int64(0x1))),
		ir.Assign(marker1, ir.Int64(0)),
		ir.Assign(marker2, ir.Int64(0)),
		ir.Assign(pipeResult, ir.Int64(0)),
		ir.Assign(pipeRead, ir.Int64(0)),
		ir.Assign(pipeWrite, ir.Int64(0)),
		ir.Assign(savedStdout, ir.Int64(-1)),
		ir.Assign(tmp, ir.Int64(0)),

		// if captureStdout != 0 {
		ir.If(ir.IsNotEqual(captureStdout, ir.Int64(0)), ir.Block{
			// Simulate syscall result
			ir.Assign(pipeResult, ir.Int64(5)),

			// if pipeResult >= 0 {
			ir.If(ir.IsGreaterOrEqual(pipeResult, ir.Int64(0)), ir.Block{
				ir.Assign(pipeRead, ir.Int64(5)),
				ir.Assign(pipeWrite, ir.Int64(6)),

				// Simulate fcntl result
				ir.Assign(savedStdout, ir.Int64(10)),

				// Multiple sequential operations (like syscalls without assignments)
				ir.Assign(tmp, ir.Op(ir.OpAdd, tmp, ir.Int64(1))),
				ir.Assign(tmp, ir.Op(ir.OpAdd, tmp, ir.Int64(1))),
				ir.Assign(tmp, ir.Op(ir.OpAdd, tmp, ir.Int64(1))),

				ir.Assign(pipeWrite, ir.Int64(-1)),
			}),
		}),

		// This MUST execute after the if
		ir.Assign(marker1, ir.Int64(1)),

		// Second if
		ir.If(ir.IsNotEqual(captureStdout, ir.Int64(0)), ir.Block{
			ir.Assign(marker2, ir.Int64(1)),
		}),

		ir.Return(ir.Op(ir.OpAdd, marker1, ir.Op(ir.OpShl, marker2, ir.Int64(8)))),
	}

	// Compile to IR
	frag, err := Compile(method)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	// Emit to binary
	prog, err := arm64asm.EmitProgram(frag)
	if err != nil {
		t.Fatalf("EmitProgram: %v", err)
	}

	binary := prog.Bytes()
	t.Logf("Binary size: %d bytes", len(binary))

	// Write to file
	outPath := "/tmp/capture_pattern.bin"
	if err := os.WriteFile(outPath, binary, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	t.Logf("Wrote binary to %s", outPath)
	t.Logf("Disassemble with: llvm-objdump -d --triple=aarch64 %s", outPath)

	// Also run it to see if it works
	res := compileAndRun(t, method)
	t.Logf("Execution result: %d (marker1=%d, marker2=%d)", res, res&0xFF, (res>>8)&0xFF)

	// Expected: marker1=1, marker2=1, so result = 1 + 256 = 257
	if res != 257 {
		t.Errorf("Expected 257, got %d", res)
	}
}
