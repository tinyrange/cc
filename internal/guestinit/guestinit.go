package guestinit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
)

var guestInitBuildIdentity struct {
	sync.Once
	value string
	err   error
}

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
	identity, err := currentBuildIdentity()
	if err != nil {
		return nil, fmt.Errorf("identify ccvm build: %w", err)
	}
	out := filepath.Join(cacheDir, "guest-init-linux-"+goarch+"-"+identity)
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

func currentBuildIdentity() (string, error) {
	guestInitBuildIdentity.Do(func() {
		executable, err := os.Executable()
		if err != nil {
			guestInitBuildIdentity.err = err
			return
		}
		file, err := os.Open(executable)
		if err != nil {
			guestInitBuildIdentity.err = err
			return
		}
		defer file.Close()
		hash := sha256.New()
		if _, err := io.Copy(hash, file); err != nil {
			guestInitBuildIdentity.err = err
			return
		}
		guestInitBuildIdentity.value = hex.EncodeToString(hash.Sum(nil)[:16])
	})
	return guestInitBuildIdentity.value, guestInitBuildIdentity.err
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
