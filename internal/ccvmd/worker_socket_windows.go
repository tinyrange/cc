//go:build windows

package ccvmd

import "os"

func workerSocketOwnedByCurrentUser(os.FileInfo) (bool, error) {
	// Access to the path is governed by the worker directory ACL on Windows.
	return true, nil
}
