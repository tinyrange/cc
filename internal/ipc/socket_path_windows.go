//go:build windows

package ipc

import (
	"fmt"
	"os"
	"path/filepath"
)

func socketPath() string {
	// On Windows, os.TempDir() can return long paths like
	// C:\Users\username\AppData\Local\Temp\ which combined with the
	// socket filename can exceed the 108-character sun_path limit.
	// Use a short subdirectory under the temp dir.
	tmpDir := filepath.Join(os.TempDir(), "cc")
	os.MkdirAll(tmpDir, 0o700)

	// Use a shorter name format (pid + counter) to minimize path length.
	return filepath.Join(tmpDir, fmt.Sprintf("h-%d-%d.sock",
		os.Getpid(), socketCounter.Add(1)))
}
