//go:build linux && arm64

package arm64

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/tinyrange/cc/internal/asm"
	"github.com/tinyrange/cc/internal/linux/defs"
)

func TestStandaloneELFExecutes(t *testing.T) {
	elfBytes, err := EmitStandaloneELF(asm.Group{
		SyscallWriteString(asm.Immediate(1), "standalone-arm64-ok\n"),
		Syscall(defs.SYS_EXIT, asm.Immediate(0)),
	})
	if err != nil {
		t.Fatalf("EmitStandaloneELF failed: %v", err)
	}

	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "standalone-arm64")
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
	if got, want := string(out), "standalone-arm64-ok\n"; got != want {
		t.Fatalf("stdout=%q, want %q", got, want)
	}
}
