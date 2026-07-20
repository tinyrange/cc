package mounts

import (
	"context"
	"fmt"
	"strings"

	"j5.nz/cc/internal/imagefs"
	managedguest "j5.nz/cc/internal/managed/guest"
)

type RootSnapshotter interface {
	RootSnapshot() (imagefs.Directory, error)
}

type RootSnapshotAtter interface {
	RootSnapshotAt(string) (imagefs.Directory, error)
}

type RootSnapshotContextProvider interface {
	RootSnapshotContext(context.Context) (imagefs.Directory, error)
}

type RootSnapshotAtContextProvider interface {
	RootSnapshotAtContext(context.Context, string) (imagefs.Directory, error)
}

func RootSnapshot(rootFS any, rootDir string) (imagefs.Directory, error) {
	return RootSnapshotContext(context.Background(), rootFS, rootDir)
}

func RootSnapshotContext(ctx context.Context, rootFS any, rootDir string) (imagefs.Directory, error) {
	if rootFS == nil {
		return nil, fmt.Errorf("root filesystem cannot be snapshotted")
	}
	if rootDir != "" {
		if snapshotter, ok := rootFS.(RootSnapshotAtContextProvider); ok {
			return snapshotter.RootSnapshotAtContext(ctx, rootDir)
		}
		snapshotter, ok := rootFS.(RootSnapshotAtter)
		if !ok {
			return nil, fmt.Errorf("root filesystem cannot be snapshotted")
		}
		return snapshotter.RootSnapshotAt(rootDir)
	}
	if snapshotter, ok := rootFS.(RootSnapshotContextProvider); ok {
		return snapshotter.RootSnapshotContext(ctx)
	}
	snapshotter, ok := rootFS.(RootSnapshotter)
	if !ok {
		return nil, fmt.Errorf("root filesystem cannot be snapshotted")
	}
	return snapshotter.RootSnapshot()
}

func ImageSnapshot(rootFS any, imageName, mountPath string) (imagefs.Directory, error) {
	if rootFS == nil {
		return nil, fmt.Errorf("root filesystem cannot be snapshotted")
	}
	snapshotter, ok := rootFS.(RootSnapshotAtter)
	if !ok {
		return nil, fmt.Errorf("image mount %q cannot be snapshotted", imageName)
	}
	return snapshotter.RootSnapshotAt(mountPath)
}

func RootSnapshotWithCapabilities(display string, caps managedguest.Capabilities, rootFS any, rootDir string) (imagefs.Directory, error) {
	if !caps.RootSnapshot {
		return nil, unsupportedFeature(display, caps, "root snapshots")
	}
	return RootSnapshot(rootFS, rootDir)
}

func ImageSnapshotWithCapabilities(display string, caps managedguest.Capabilities, rootFS any, imageName, mountPath string) (imagefs.Directory, error) {
	if !caps.ImageSnapshot {
		return nil, unsupportedFeature(display, caps, "image snapshots")
	}
	return ImageSnapshot(rootFS, imageName, mountPath)
}

func unsupportedFeature(runtimeName string, caps managedguest.Capabilities, feature string) error {
	runtimeName = strings.TrimSpace(runtimeName)
	if runtimeName == "" {
		runtimeName = "managed guest"
	}
	feature = strings.TrimSpace(feature)
	if advertisedCapability(caps, feature) {
		return fmt.Errorf("%s runtime advertises %s but no implementation is wired", runtimeName, feature)
	}
	return fmt.Errorf("%s runtime does not support %s yet", runtimeName, feature)
}

func advertisedCapability(caps managedguest.Capabilities, feature string) bool {
	switch strings.TrimSpace(feature) {
	case "root snapshots":
		return caps.RootSnapshot
	case "image snapshots":
		return caps.ImageSnapshot
	default:
		return false
	}
}
