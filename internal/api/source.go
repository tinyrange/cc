package api

import (
	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/oci"
)

// ociSource implements cc.InstanceSource for OCI images.
type ociSource struct {
	image *oci.Image
	cfs   *oci.ContainerFS
	arch  hv.CpuArchitecture
}

// IsInstanceSource marks this as an InstanceSource.
func (*ociSource) IsInstanceSource() {}

// Close releases resources held by the source.
func (s *ociSource) Close() error {
	if s.cfs != nil {
		return s.cfs.Close()
	}
	return nil
}
