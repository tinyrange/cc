package test

import (
	"testing"

	"github.com/tinyrange/cc/internal/linux/defs"
	linux "github.com/tinyrange/cc/internal/linux/defs/amd64"
	"github.com/tinyrange/cc/internal/linux/defs/arm64"
)

func TestSyscalls(t *testing.T) {
	for i := defs.Syscall(0); i < defs.MaximumSyscall; i++ {
		// check that it exists in x86_64
		if _, ok := linux.SyscallMap[i]; !ok {
			t.Errorf("syscall %s missing in x86_64", i)
		}

		// check that it exists in arm64
		if _, ok := arm64.SyscallMap[i]; !ok {
			t.Errorf("syscall %s missing in arm64", i)
		}
	}
}
