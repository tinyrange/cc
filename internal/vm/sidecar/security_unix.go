//go:build !windows

package sidecar

import (
	"fmt"
	"os"
)

func validateWorkerPrivateFile(path, kind string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat %s: %w", kind, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s must be a regular file", kind)
	}
	if permissions := info.Mode().Perm(); permissions&0o077 != 0 {
		return fmt.Errorf("%s mode is %04o, want owner-only permissions", kind, permissions)
	}
	return nil
}
