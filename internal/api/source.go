package api

import (
	"github.com/tinyrange/cc/internal/fslayer"
	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/oci"
)

// ociSource implements cc.InstanceSource for OCI images.
// It also implements FilesystemSnapshot as a base snapshot (no parent).
type ociSource struct {
	image    *oci.Image
	cfs      *oci.ContainerFS
	arch     hv.CpuArchitecture
	imageRef string // Original image reference for cache key derivation
}

// IsInstanceSource marks this as an InstanceSource.
func (*ociSource) IsInstanceSource() {}

// CacheKey returns a unique key for this base image.
func (s *ociSource) CacheKey() string {
	return fslayer.BaseKey(s.imageRef, string(s.arch))
}

// Parent returns nil since OCI images are base snapshots.
func (s *ociSource) Parent() FilesystemSnapshot {
	return nil
}

// Close releases resources held by the source.
func (s *ociSource) Close() error {
	if s.cfs != nil {
		return s.cfs.Close()
	}
	return nil
}

// Ensure ociSource implements FilesystemSnapshot
var _ FilesystemSnapshot = (*ociSource)(nil)
