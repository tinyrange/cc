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
	if i.defaultRootDir != "" {
		snapshotter, ok := i.rootFS.(interface {
			RootSnapshotAt(string) (imagefs.Directory, error)
		})
		if !ok {
			return nil, fmt.Errorf("root filesystem cannot be snapshotted")
		}
		return snapshotter.RootSnapshotAt(i.defaultRootDir)
	}
	snapshotter, ok := i.rootFS.(interface {
		RootSnapshot() (imagefs.Directory, error)
	})
	if !ok {
		return nil, fmt.Errorf("root filesystem cannot be snapshotted")
	}
	return snapshotter.RootSnapshot()
}

func (i *linuxInstance) SnapshotImage(imageName string) (imagefs.Directory, error) {
	if i == nil || i.rootFS == nil {
		return nil, fmt.Errorf("root filesystem cannot be snapshotted")
	}
	if i.image != nil && i.image.Name == imageName {
		if i.defaultRootDir != "" {
			snapshotter, ok := i.rootFS.(interface {
				RootSnapshotAt(string) (imagefs.Directory, error)
			})
			if !ok {
				return nil, fmt.Errorf("image mount %q cannot be snapshotted", imageName)
			}
			return snapshotter.RootSnapshotAt(i.defaultRootDir)
		}
		return i.RootSnapshot()
	}
	snapshotter, ok := i.rootFS.(interface {
		RootSnapshotAt(string) (imagefs.Directory, error)
	})
	if !ok {
		return nil, fmt.Errorf("image mount %q cannot be snapshotted", imageName)
	}
	return snapshotter.RootSnapshotAt(linuxImageMountPath(imageName))
}
