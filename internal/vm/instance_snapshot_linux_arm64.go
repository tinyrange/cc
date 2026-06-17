//go:build linux && arm64

package vm

import (
	"context"

	"j5.nz/cc/internal/imagefs"
	"j5.nz/cc/internal/vm/execplan"
	hostmanaged "j5.nz/cc/internal/vm/host/managed"
)

func (i *linuxInstance) Flush(ctx context.Context) error {
	if i == nil {
		return hostmanaged.FlushSession(ctx, nil)
	}
	return i.managedInstance.Flush(ctx)
}

func (i *linuxInstance) RootSnapshot() (imagefs.Directory, error) {
	return nil, execplan.UnsupportedFeature("Linux", i.ManagedCapabilities(), "root snapshots")
}

func (i *linuxInstance) SnapshotImage(string) (imagefs.Directory, error) {
	return nil, execplan.UnsupportedFeature("Linux", i.ManagedCapabilities(), "image snapshots")
}
