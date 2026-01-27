package initx

import (
	"os"
	"testing"

	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/ir"
	_ "github.com/tinyrange/cc/internal/ir/arm64"
)

// TestDumpInitBinary compiles the init_source.go using RTG and dumps the binary
// to a file for manual disassembly.
//
// Run with: ./tools/build.go -test ./internal/initx -run DumpInitBinary -v
// Then: llvm-objdump -d --triple=aarch64 /tmp/init_binary.bin
func TestDumpInitBinary(t *testing.T) {
	cfg := BuilderConfig{
		Arch:      hv.ArchitectureARM64,
		VsockPort: 9998,
	}

	prog, err := BuildFromRTG(cfg)
	if err != nil {
		t.Fatalf("BuildFromRTG: %v", err)
	}

	t.Logf("Program entrypoint: %s", prog.Entrypoint)
	t.Logf("Number of methods: %d", len(prog.Methods))

	// Build standalone program for ARM64
	asmProg, err := ir.BuildStandaloneProgramForArch(hv.ArchitectureARM64, prog)
	if err != nil {
		t.Fatalf("BuildStandaloneProgramForArch: %v", err)
	}

	binary := asmProg.Bytes()
	t.Logf("Binary size: %d bytes", len(binary))

	// Write to file
	outPath := t.TempDir() + "/init_binary.bin"
	if err := os.WriteFile(outPath, binary, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	t.Logf("Wrote binary to %s", outPath)
	t.Logf("Disassemble with: llvm-objdump -d --triple=aarch64 %s", outPath)
}
