//go:build !windows

package ccvmd

import (
	"fmt"
	"os"
	"syscall"
)

func workerSocketOwnedByCurrentUser(info os.FileInfo) (bool, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return false, fmt.Errorf("worker socket has unsupported stat metadata")
	}
	return stat.Uid == uint32(os.Geteuid()), nil
}
