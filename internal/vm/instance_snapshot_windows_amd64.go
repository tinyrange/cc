//go:build windows && amd64

package vm

import (
	"context"

	"j5.nz/cc/internal/imagefs"
)

func (i *windowsInstance) Flush(ctx context.Context) error {
	return i.core().Flush(ctx)
}

func (i *windowsInstance) RootSnapshot() (imagefs.Directory, error) {
	if i == nil || i.rootFS == nil {
		return managedRootSnapshot(nil, "")
	}
	return managedRootSnapshotWithCapabilities("Linux", i.ManagedCapabilities(), i.rootFS, "")
}

func (i *windowsInstance) SnapshotImage(imageName string) (imagefs.Directory, error) {
	if i == nil || i.rootFS == nil {
		return managedRootSnapshot(nil, "")
	}
	if i.image != nil && i.image.Name == imageName {
		return i.RootSnapshot()
	}
	return managedImageSnapshotWithCapabilities("Linux", i.ManagedCapabilities(), i.rootFS, imageName, windowsImageMountPath(imageName))
}
