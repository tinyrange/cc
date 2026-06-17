//go:build linux && amd64

package vm

import (
	"context"
	"fmt"

	"j5.nz/cc/internal/imagefs"
)

func (i *linuxInstance) Flush(ctx context.Context) error {
	if i == nil || i.managedInstance == nil {
		return fmt.Errorf("instance is not running")
	}
	return i.managedInstance.Flush(ctx)
}

func (i *linuxInstance) RootSnapshot() (imagefs.Directory, error) {
	if i == nil || i.rootFS == nil {
		return nil, fmt.Errorf("root filesystem cannot be snapshotted")
	}
	if i.managedInstance == nil {
		return nil, fmt.Errorf("instance is not running")
	}
	return managedRootSnapshotWithCapabilities("Linux", i.caps, i.rootFS, i.defaultRootDir)
}

func (i *linuxInstance) SnapshotImage(imageName string) (imagefs.Directory, error) {
	if i == nil || i.rootFS == nil {
		return nil, fmt.Errorf("root filesystem cannot be snapshotted")
	}
	if i.managedInstance == nil {
		return nil, fmt.Errorf("instance is not running")
	}
	if i.image != nil && i.image.Name == imageName {
		if i.defaultRootDir != "" {
			return managedRootSnapshotWithCapabilities("Linux", i.caps, i.rootFS, i.defaultRootDir)
		}
		return i.RootSnapshot()
	}
	return managedImageSnapshotWithCapabilities("Linux", i.caps, i.rootFS, imageName, linuxImageMountPath(imageName))
}
