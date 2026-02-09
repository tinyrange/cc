package api

// virtualSource implements InstanceSource for IPC-based helper instances.
// It carries the metadata needed to create an instance via the IPC protocol.
type virtualSource struct {
	sourceType uint8  // 0=tar, 1=dir, 2=ref
	sourcePath string // path for tar/dir sources
	imageRef   string // OCI image reference for ref sources
	cacheDir   string // cache directory for ref sources
}

func (s *virtualSource) IsInstanceSource() {}
