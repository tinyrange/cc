//go:build darwin && arm64

package hv

import (
	"fmt"
	"os"
	"testing"

	"j5.nz/cc/internal/macos"
)

func TestMain(m *testing.M) {
	if err := macos.EnsureExecutableIsSigned(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}
