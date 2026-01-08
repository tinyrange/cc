//go:build darwin && arm64

package initx

import (
	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/hv/hvf"
)

type hvfSnapshotIO struct{}

func (hvfSnapshotIO) SaveSnapshot(path string, snap hv.Snapshot) error {
	return hvf.SaveSnapshot(path, snap)
}

func (hvfSnapshotIO) LoadSnapshot(path string) (hv.Snapshot, error) {
	return hvf.LoadSnapshot(path)
}

// GetSnapshotIO returns the platform-specific snapshot IO implementation.
func GetSnapshotIO() SnapshotIO {
	return hvfSnapshotIO{}
}
