package guestinit

import (
	"context"
	"fmt"
	"runtime"
)

func Build(ctx context.Context, cacheDir string) ([]byte, error) {
	return BuildForArch(ctx, cacheDir, runtime.GOARCH)
}

func BuildForArch(ctx context.Context, cacheDir string, arch string) ([]byte, error) {
	_ = cacheDir
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if arch == "" {
		arch = runtime.GOARCH
	}
	payload := embeddedPayload(arch)
	if len(payload) < 4 || string(payload[:4]) != "\x7fELF" {
		return nil, fmt.Errorf("OpenBSD guest init payload for %q is not embedded", arch)
	}
	return append([]byte(nil), payload...), nil
}
