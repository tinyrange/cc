//go:build windows && amd64

package vm

import (
	"context"
	"fmt"

	"j5.nz/cc/internal/imagefs"
)

func (i *windowsInstance) Flush(ctx context.Context) error {
	if i == nil || i.session == nil {
		return fmt.Errorf("instance is not running")
	}
	return i.session.Flush(ctx)
}

func (i *windowsInstance) RootSnapshot() (imagefs.Directory, error) {
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

func (i *windowsInstance) SnapshotImage(imageName string) (imagefs.Directory, error) {
	if i == nil || i.rootFS == nil {
		return nil, fmt.Errorf("root filesystem cannot be snapshotted")
	}
	if i.image != nil && i.image.Name == imageName {
		return i.RootSnapshot()
	}
	snapshotter, ok := i.rootFS.(interface {
		RootSnapshotAt(string) (imagefs.Directory, error)
	})
	if !ok {
		return nil, fmt.Errorf("image mount %q cannot be snapshotted", imageName)
	}
	return snapshotter.RootSnapshotAt(windowsImageMountPath(imageName))
}
