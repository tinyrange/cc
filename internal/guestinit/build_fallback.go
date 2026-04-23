package guestinit

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func buildFromSource(ctx context.Context, cacheDir string) ([]byte, error) {
	if cacheDir == "" {
		cacheDir = filepath.Join(os.TempDir(), "ccx3-guestinit")
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return nil, fmt.Errorf("create guestinit cache dir: %w", err)
	}

	outPath := filepath.Join(cacheDir, "guest-init-linux-arm64")
	cmd := exec.CommandContext(ctx, "go", "build", "-o", outPath, "./internal/cmd/init")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH=arm64")
	if wd, err := repoRoot(); err == nil {
		cmd.Dir = wd
	}
	if output, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("build guest init: %w\n%s", err, string(output))
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		return nil, fmt.Errorf("read built guest init: %w", err)
	}
	if len(data) < 4 || string(data[:4]) != "\x7fELF" {
		return nil, fmt.Errorf("built guest init is not an ELF binary")
	}
	return data, nil
}

func repoRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("repo root not found from %q", wd)
		}
		dir = parent
	}
}
