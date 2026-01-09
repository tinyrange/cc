//go:build windows && arm64

package initx

import (
	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/hv/whp"
)

type whpSnapshotIO struct{}

func (whpSnapshotIO) SaveSnapshot(path string, snap hv.Snapshot) error {
	return whp.SaveSnapshot(path, snap)
}

func (whpSnapshotIO) LoadSnapshot(path string) (hv.Snapshot, error) {
	return whp.LoadSnapshot(path)
}

// GetSnapshotIO returns the platform-specific snapshot IO implementation.
func GetSnapshotIO() SnapshotIO {
	return whpSnapshotIO{}
}
