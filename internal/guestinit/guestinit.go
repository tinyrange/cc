package guestinit

import (
	"context"
	_ "embed"
	"fmt"
)

// guestInitLinuxARM64 contains the prebuilt Linux arm64 guest init binary
// bundled directly into the ccvm executable so daemon startup never shells out
// to the Go toolchain.
//
//go:embed guest-init-linux-arm64
var guestInitLinuxARM64 []byte

func Build(_ context.Context, _ string) ([]byte, error) {
	if len(guestInitLinuxARM64) == 0 {
		return nil, fmt.Errorf("embedded guest init payload is empty")
	}
	return append([]byte(nil), guestInitLinuxARM64...), nil
}
