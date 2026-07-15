package guestinit

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	guestbuildid "j5.nz/cc/internal/guestinit/buildid"
)

func Build(ctx context.Context, cacheDir string) ([]byte, error) {
	return BuildForArch(ctx, cacheDir, "amd64")
}

func BuildForArch(ctx context.Context, cacheDir string, arch string) ([]byte, error) {
	if arch == "" {
		arch = "amd64"
	}
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
	identity, err := guestbuildid.Resolve(ctx, moduleRoot, "openbsd", arch, "./internal/cmd/openbsd-init")
	if err != nil {
		return nil, fmt.Errorf("identify OpenBSD guest init build: %w", err)
	}
	out := filepath.Join(cacheDir, "guest-init-openbsd-"+arch+"-"+identity)
	if data, err := os.ReadFile(out); err == nil && len(data) != 0 {
		return data, nil
	}
	cmd := exec.CommandContext(ctx, "go", "build", "-trimpath", "-ldflags", "-s -w", "-o", out, "./internal/cmd/openbsd-init")
	cmd.Env = append(os.Environ(), "GOOS=openbsd", "GOARCH="+arch, "CGO_ENABLED=0")
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
