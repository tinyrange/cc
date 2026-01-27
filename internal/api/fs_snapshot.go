package api

import (
	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/oci"
)

// fsSnapshotSource implements FilesystemSnapshot for in-memory filesystem snapshots.
type fsSnapshotSource struct {
	// lcfs is the layered container filesystem with snapshot layers
	lcfs *oci.LayeredContainerFS
	// baseImage is the original OCI image
	baseImage *oci.Image
	// parent is the parent snapshot (or nil for base)
	parent FilesystemSnapshot
	// cacheKey uniquely identifies this snapshot
	cacheKey string
	// arch is the target architecture
	arch hv.CpuArchitecture
	// layers contains the hashes of all snapshot layers (not including base OCI layers)
	layers []string
}

// IsInstanceSource marks this as an InstanceSource.
func (*fsSnapshotSource) IsInstanceSource() {}

// CacheKey returns the unique cache key for this snapshot.
func (s *fsSnapshotSource) CacheKey() string {
	return s.cacheKey
}

// Parent returns the parent snapshot, or nil if this is based directly on an OCI image.
func (s *fsSnapshotSource) Parent() FilesystemSnapshot {
	return s.parent
}

// Close releases resources held by the snapshot.
func (s *fsSnapshotSource) Close() error {
	if s.lcfs != nil {
		return s.lcfs.Close()
	}
	return nil
}

// Image returns the base OCI image.
func (s *fsSnapshotSource) Image() *oci.Image {
	return s.baseImage
}

// LayeredContainerFS returns the layered container filesystem.
func (s *fsSnapshotSource) LayeredContainerFS() *oci.LayeredContainerFS {
	return s.lcfs
}

// Architecture returns the target architecture.
func (s *fsSnapshotSource) Architecture() hv.CpuArchitecture {
	return s.arch
}

// Layers returns the snapshot layer hashes.
func (s *fsSnapshotSource) Layers() []string {
	return s.layers
}

var _ FilesystemSnapshot = (*fsSnapshotSource)(nil)
