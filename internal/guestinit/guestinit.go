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
	return BuildForArch(ctx, cacheDir, runtime.GOARCH)
}

func BuildForArch(ctx context.Context, cacheDir, goarch string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if goarch == "" {
		goarch = runtime.GOARCH
	}
	if cacheDir == "" {
		var err error
		cacheDir, err = os.MkdirTemp("", "cc-linux-guestinit-*")
		if err != nil {
			return nil, fmt.Errorf("create guest init cache: %w", err)
		}
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return nil, fmt.Errorf("create guest init cache: %w", err)
	}
	out := filepath.Join(cacheDir, "guest-init-linux-"+goarch)
	if data, err := os.ReadFile(out); err == nil && validateGuestInitPayload(goarch, data) == nil {
		return data, nil
	}
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return nil, fmt.Errorf("locate guest init package")
	}
	moduleRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	cmd := exec.CommandContext(ctx, "go", "build", "-trimpath", "-ldflags", "-s -w", "-o", out, "./internal/cmd/init")
	cmd.Env = append(os.Environ(), "GOOS=linux", "GOARCH="+goarch, "CGO_ENABLED=0")
	cmd.Dir = moduleRoot
	data, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("build Linux guest init: %w\n%s", err, data)
	}
	bin, err := os.ReadFile(out)
	if err != nil {
		return nil, fmt.Errorf("read Linux guest init: %w", err)
	}
	if err := validateGuestInitPayload(goarch, bin); err != nil {
		return nil, err
	}
	return bin, nil
}

func validateGuestInitPayload(goarch string, payload []byte) error {
	if len(payload) == 0 {
		return fmt.Errorf("guest init payload for %q is empty", goarch)
	}
	if len(payload) < 4 || string(payload[:4]) != "\x7fELF" {
		return fmt.Errorf("guest init payload for %q is not a static Linux ELF", goarch)
	}
	return nil
}
