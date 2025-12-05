//go:build linux && amd64

package rtg

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/tinyrange/cc/internal/asm/amd64"
	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/ir"
	_ "github.com/tinyrange/cc/internal/ir/amd64"
	"github.com/tinyrange/cc/internal/linux/defs"
)

func TestRTGProgramExecutesStandalone(t *testing.T) {
	src := `package main
func main() int64 {
	printf("rtg-exec-ok\n")
	syscall(SYS_EXIT, 0)
	return 0
}`

	prog, err := CompileProgram(src)
	if err != nil {
		t.Fatalf("CompileProgram: %v", err)
	}

	wrapped := &ir.Program{
		Entrypoint: "_start",
		Methods: map[string]ir.Method{
			"_start": {
				ir.CallMethod("main"),
				ir.Syscall(defs.SYS_EXIT, ir.Int64(0)),
			},
		},
	}
	for name, method := range prog.Methods {
		wrapped.Methods[name] = method
	}

	asmProg, err := ir.BuildStandaloneProgramForArch(hv.ArchitectureX86_64, wrapped)
	if err != nil {
		t.Fatalf("BuildStandaloneProgramForArch: %v", err)
	}

	elfBytes, err := amd64.StandaloneELF(asmProg)
	if err != nil {
		t.Fatalf("StandaloneELF: %v", err)
	}

	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "rtg-standalone")
	if err := os.WriteFile(path, elfBytes, 0o755); err != nil {
		t.Fatalf("write ELF: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("executing rtg program failed: %v (output: %s)", err, out)
	}
	if got, want := string(out), "rtg-exec-ok\n"; got != want {
		t.Fatalf("stdout=%q, want %q", got, want)
	}
}
