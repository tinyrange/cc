//go:build linux && arm64

package vm

import (
	"context"
	"fmt"

	"j5.nz/cc/internal/imagefs"
	kvmhost "j5.nz/cc/internal/vm/host/kvm"
	hostmanaged "j5.nz/cc/internal/vm/host/managed"
	"j5.nz/cc/internal/vm/mounts"
)

func (i *linuxInstance) Flush(ctx context.Context) error {
	if i == nil {
		return hostmanaged.FlushSession(ctx, nil)
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
	return mounts.RootSnapshotWithCapabilities("Linux", i.caps, i.rootFS, i.defaultRootDir)
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
			return mounts.RootSnapshotWithCapabilities("Linux", i.caps, i.rootFS, i.defaultRootDir)
		}
		return i.RootSnapshot()
	}
	return mounts.ImageSnapshotWithCapabilities("Linux", i.caps, i.rootFS, imageName, kvmhost.ImageMountPath(imageName))
}
