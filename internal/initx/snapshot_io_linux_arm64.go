//go:build linux && arm64

package initx

import (
	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/hv/kvm"
)

type kvmSnapshotIO struct{}

func (kvmSnapshotIO) SaveSnapshot(path string, snap hv.Snapshot) error {
	return kvm.SaveSnapshot(path, snap)
}

func (kvmSnapshotIO) LoadSnapshot(path string) (hv.Snapshot, error) {
	return kvm.LoadSnapshot(path)
}

// GetSnapshotIO returns the platform-specific snapshot IO implementation.
func GetSnapshotIO() SnapshotIO {
	return kvmSnapshotIO{}
}
