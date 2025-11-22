//go:build linux && amd64

package amd64

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/tinyrange/cc/internal/asm"
)

func TestStandaloneELFExecutes(t *testing.T) {
	elfBytes, err := EmitStandaloneELF(asm.Group{
		SyscallWriteString(asm.Immediate(1), "standalone-ok\n"),
		Exit(0),
	})
	if err != nil {
		t.Fatalf("EmitStandaloneELF failed: %v", err)
	}

	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "standalone")
	if err := os.WriteFile(path, elfBytes, 0o755); err != nil {
		t.Fatalf("write ELF: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("executing standalone ELF failed: %v (output: %s)", err, out)
	}
	if got, want := string(out), "standalone-ok\n"; got != want {
		t.Fatalf("stdout=%q, want %q", got, want)
	}
}
