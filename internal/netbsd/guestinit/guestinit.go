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
		cacheDir, err = os.MkdirTemp("", "cc-netbsd-guestinit-*")
		if err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return nil, err
	}
	out := filepath.Join(cacheDir, "guest-init-netbsd-amd64")
	if data, err := os.ReadFile(out); err == nil && len(data) != 0 {
		return data, nil
	}
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return nil, fmt.Errorf("locate NetBSD guest init package")
	}
	moduleRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
	cmd := exec.CommandContext(ctx, "go", "build", "-trimpath", "-o", out, "./internal/cmd/netbsd-init")
	cmd.Dir = moduleRoot
	cmd.Env = append(os.Environ(), "GOOS=netbsd", "GOARCH=amd64", "CGO_ENABLED=0")
	if output, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("build NetBSD guest init: %w\n%s", err, output)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		return nil, fmt.Errorf("read NetBSD guest init: %w", err)
	}
	return data, nil
}
