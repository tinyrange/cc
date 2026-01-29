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

// Config returns the OCI image configuration metadata.
func (s *ociSource) Config() *ImageConfig {
	return &ImageConfig{
		Architecture: s.image.Config.Architecture,
		Env:          append([]string{}, s.image.Config.Env...),
		WorkingDir:   s.image.Config.WorkingDir,
		Entrypoint:   append([]string{}, s.image.Config.Entrypoint...),
		Cmd:          append([]string{}, s.image.Config.Cmd...),
		User:         s.image.Config.User,
		Labels:       copyLabels(s.image.Config.Labels),
	}
}

// copyLabels creates a copy of a labels map.
func copyLabels(labels map[string]string) map[string]string {
	if labels == nil {
		return nil
	}
	result := make(map[string]string, len(labels))
	for k, v := range labels {
		result[k] = v
	}
	return result
}

// Ensure ociSource implements FilesystemSnapshot and OCISource
var _ FilesystemSnapshot = (*ociSource)(nil)
var _ OCISource = (*ociSource)(nil)

// SourceConfig returns the ImageConfig for a source, or nil if unavailable.
// This is a convenience function that performs a type assertion to OCISource.
func SourceConfig(source InstanceSource) *ImageConfig {
	if ociSrc, ok := source.(OCISource); ok {
		return ociSrc.Config()
	}
	return nil
}
