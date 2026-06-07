package guestinit

import (
	"context"
	"fmt"
	"runtime"
)

func Build(ctx context.Context, cacheDir string) ([]byte, error) {
	return BuildForArch(ctx, cacheDir, runtime.GOARCH)
}

func BuildForArch(ctx context.Context, cacheDir, goarch string) ([]byte, error) {
	_, _ = ctx, cacheDir
	payload := embeddedPayload(goarch)
	if err := validateEmbeddedPayload(goarch, payload); err != nil {
		return nil, err
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

func RequireEmbedded() error {
	for _, goarch := range []string{"arm64", "amd64"} {
		if err := validateEmbeddedPayload(goarch, embeddedPayload(goarch)); err != nil {
			return err
		}
	}
	return nil
}

func validateEmbeddedPayload(goarch string, payload []byte) error {
	if len(payload) == 0 {
		return fmt.Errorf("guest init payload for %q is not embedded; build ccvm with -tags embed_guestinit", goarch)
	}
	if len(payload) < 4 || string(payload[:4]) != "\x7fELF" {
		return fmt.Errorf("guest init payload for %q is not a static Linux ELF; rebuild it with CGO_ENABLED=0 GOOS=linux GOARCH=%s", goarch, goarch)
	}
	return nil
}
