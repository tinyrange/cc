//go:build windows

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
	return nil
}
