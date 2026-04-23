package guestinit

import (
	"context"
	"runtime"
)

func Build(ctx context.Context, cacheDir string) ([]byte, error) {
	return BuildForArch(ctx, cacheDir, runtime.GOARCH)
}

func BuildForArch(ctx context.Context, cacheDir, goarch string) ([]byte, error) {
	payload := embeddedPayload(goarch)
	if len(payload) == 0 {
		return buildFromSource(ctx, cacheDir, goarch)
	}
	return append([]byte(nil), payload...), nil
}

func embeddedPayload(goarch string) []byte {
	switch goarch {
	case "arm64":
		return guestInitLinuxARM64
	case "amd64":
		return guestInitLinuxAMD64
	default:
		return nil
	}
}
