//go:build !darwin || !arm64

package main

import (
	"github.com/tinyrange/cc/internal/initx"
)

func getSnapshotIO() initx.SnapshotIO {
	return nil // Snapshot caching not available on this platform
}
