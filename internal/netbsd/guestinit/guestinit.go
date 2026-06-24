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
	return BuildForArch(ctx, cacheDir, "amd64")
}

func BuildForArch(ctx context.Context, cacheDir string, arch string) ([]byte, error) {
	if arch == "" {
		arch = "amd64"
	}
	if payload := embeddedPayload(arch); len(payload) != 0 {
		return append([]byte(nil), payload...), nil
	}
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
	out := filepath.Join(cacheDir, "guest-init-netbsd-"+arch)
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
	cmd.Env = append(os.Environ(), "GOOS=netbsd", "GOARCH="+arch, "CGO_ENABLED=0")
	if output, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("build NetBSD guest init: %w\n%s", err, output)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		return nil, fmt.Errorf("read NetBSD guest init: %w", err)
	}
	return data, nil
}

func RequireEmbeddedForArch(arch string) error {
	return validateEmbeddedPayload("NetBSD", arch, embeddedPayload(arch))
}

func validateEmbeddedPayload(name, arch string, payload []byte) error {
	if len(payload) == 0 {
		return fmt.Errorf("%s guest init payload for %q is not embedded; build ccvm with -tags embed_guestinit", name, arch)
	}
	if len(payload) < 4 || string(payload[:4]) != "\x7fELF" {
		return fmt.Errorf("%s guest init payload for %q is not an ELF binary", name, arch)
	}
	return nil
}
