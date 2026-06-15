package guestinit

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

func Build(ctx context.Context, cacheDir string) ([]byte, error) {
	if cacheDir == "" {
		var err error
		cacheDir, err = os.MkdirTemp("", "cc-openbsd-guestinit-*")
		if err != nil {
			return nil, fmt.Errorf("create guest init cache: %w", err)
		}
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return nil, fmt.Errorf("create guest init cache: %w", err)
	}
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return nil, fmt.Errorf("locate OpenBSD guest init package")
	}
	moduleRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
	out := filepath.Join(cacheDir, "guest-init-openbsd-amd64")
	cmd := exec.CommandContext(ctx, "go", "build", "-trimpath", "-o", out, "./internal/cmd/openbsd-init")
	cmd.Env = append(os.Environ(), "GOOS=openbsd", "GOARCH=amd64", "CGO_ENABLED=0")
	cmd.Dir = moduleRoot
	data, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("build OpenBSD guest init: %w\n%s", err, data)
	}
	bin, err := os.ReadFile(out)
	if err != nil {
		return nil, fmt.Errorf("read OpenBSD guest init: %w", err)
	}
	return bin, nil
}
