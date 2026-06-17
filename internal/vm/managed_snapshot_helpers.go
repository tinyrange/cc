package vm

import (
	"fmt"

	"j5.nz/cc/internal/imagefs"
)

type managedRootSnapshotter interface {
	RootSnapshot() (imagefs.Directory, error)
}

type managedRootSnapshotAtter interface {
	RootSnapshotAt(string) (imagefs.Directory, error)
}

func managedRootSnapshot(rootFS any, rootDir string) (imagefs.Directory, error) {
	if rootFS == nil {
		return nil, fmt.Errorf("root filesystem cannot be snapshotted")
	}
	if rootDir != "" {
		snapshotter, ok := rootFS.(managedRootSnapshotAtter)
		if !ok {
			return nil, fmt.Errorf("root filesystem cannot be snapshotted")
		}
		return snapshotter.RootSnapshotAt(rootDir)
	}
	snapshotter, ok := rootFS.(managedRootSnapshotter)
	if !ok {
		return nil, fmt.Errorf("root filesystem cannot be snapshotted")
	}
	return snapshotter.RootSnapshot()
}

func managedImageSnapshot(rootFS any, imageName, mountPath string) (imagefs.Directory, error) {
	if rootFS == nil {
		return nil, fmt.Errorf("root filesystem cannot be snapshotted")
	}
	snapshotter, ok := rootFS.(managedRootSnapshotAtter)
	if !ok {
		return nil, fmt.Errorf("image mount %q cannot be snapshotted", imageName)
	}
	return snapshotter.RootSnapshotAt(mountPath)
}

func managedRootSnapshotWithCapabilities(display string, caps guestCapabilities, rootFS any, rootDir string) (imagefs.Directory, error) {
	if !caps.RootSnapshot {
		return nil, unsupportedManagedFeature(display, caps, "root snapshots")
	}
	return managedRootSnapshot(rootFS, rootDir)
}

func managedImageSnapshotWithCapabilities(display string, caps guestCapabilities, rootFS any, imageName, mountPath string) (imagefs.Directory, error) {
	if !caps.ImageSnapshot {
		return nil, unsupportedManagedFeature(display, caps, "image snapshots")
	}
	return managedImageSnapshot(rootFS, imageName, mountPath)
}
