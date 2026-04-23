package guestinit

import (
	"context"
)

func Build(ctx context.Context, cacheDir string) ([]byte, error) {
	if len(guestInitLinuxARM64) == 0 {
		return buildFromSource(ctx, cacheDir)
	}
	return append([]byte(nil), guestInitLinuxARM64...), nil
}
