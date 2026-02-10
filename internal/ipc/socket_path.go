package ipc

import (
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"
)

// socketCounter provides unique socket paths when multiple helpers are spawned concurrently.
var socketCounter atomic.Uint64

// SocketPath returns a platform-appropriate Unix domain socket path.
// On Windows, this produces a shorter path to stay within the 108-char sun_path limit.
func SocketPath() string {
	return socketPath()
}

// defaultSocketPath generates a socket path using the standard scheme.
// Used on non-Windows platforms where TempDir paths are short.
func defaultSocketPath() string {
	tmpDir := os.TempDir()
	return filepath.Join(tmpDir, fmt.Sprintf("cc-helper-%d-%d-%d.sock",
		os.Getpid(), time.Now().UnixNano(), socketCounter.Add(1)))
}
