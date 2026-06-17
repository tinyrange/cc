//go:build linux && arm64

package vm

import (
	"context"

	"j5.nz/cc/internal/imagefs"
)

func (i *linuxInstance) Flush(ctx context.Context) error {
	if i == nil {
		return flushManagedSession(ctx, nil)
	}
	return i.managedInstance.Flush(ctx)
}

func (i *linuxInstance) RootSnapshot() (imagefs.Directory, error) {
	return nil, unsupportedManagedFeature("Linux", i.ManagedCapabilities(), "root snapshots")
}

func (i *linuxInstance) SnapshotImage(string) (imagefs.Directory, error) {
	return nil, unsupportedManagedFeature("Linux", i.ManagedCapabilities(), "image snapshots")
}
