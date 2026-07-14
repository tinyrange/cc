//go:build windows && arm64

package vm

import (
	"context"

	"j5.nz/cc/internal/imagefs"
	"j5.nz/cc/internal/vm/mounts"
)

func (i *windowsInstance) Flush(ctx context.Context) error {
	return i.core().Flush(ctx)
}

func (i *windowsInstance) RootSnapshot() (imagefs.Directory, error) {
	if i == nil || i.rootFS == nil {
		return mounts.RootSnapshot(nil, "")
	}
	return mounts.RootSnapshotWithCapabilities("Linux", i.ManagedCapabilities(), i.rootFS, "")
}

func (i *windowsInstance) SnapshotImage(imageName string) (imagefs.Directory, error) {
	if i == nil || i.rootFS == nil {
		return mounts.RootSnapshot(nil, "")
	}
	if i.image != nil && i.image.Name == imageName {
		return i.RootSnapshot()
	}
	return mounts.ImageSnapshotWithCapabilities("Linux", i.ManagedCapabilities(), i.rootFS, imageName, windowsImageMountPath(imageName))
}
