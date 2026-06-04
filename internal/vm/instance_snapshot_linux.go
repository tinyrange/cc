//go:build linux

package vm

import (
	"context"
	"fmt"

	"j5.nz/cc/internal/imagefs"
)

func (i *linuxInstance) Flush(ctx context.Context) error {
	if i == nil || i.session == nil {
		return fmt.Errorf("instance is not running")
	}
	return i.session.Flush(ctx)
}

func (i *linuxInstance) RootSnapshot() (imagefs.Directory, error) {
	if i == nil || i.rootFS == nil {
		return nil, fmt.Errorf("root filesystem cannot be snapshotted")
	}
	snapshotter, ok := i.rootFS.(interface {
		RootSnapshot() (imagefs.Directory, error)
	})
	if !ok {
		return nil, fmt.Errorf("root filesystem cannot be snapshotted")
	}
	return snapshotter.RootSnapshot()
}
