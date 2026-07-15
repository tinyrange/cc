//go:build windows

package ccvmd

import (
	"errors"
	"os"
	"syscall"
)

const wsaeconnrefused syscall.Errno = 10061

func workerSocketConnectionRefused(err error) bool {
	return errors.Is(err, wsaeconnrefused)
}

func workerSocketOwnedByCurrentUser(os.FileInfo) (bool, error) {
	// Access to the path is governed by the worker directory ACL on Windows.
	return true, nil
}
