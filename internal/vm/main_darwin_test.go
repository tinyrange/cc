//go:build darwin

package vm

import (
	"os"
	"testing"

	"j5.nz/cc/internal/macos"
)

func TestMain(m *testing.M) {
	if err := macos.EnsureExecutableIsSigned(); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}
