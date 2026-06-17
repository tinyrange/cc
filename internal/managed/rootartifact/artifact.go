package rootartifact

import (
	"j5.nz/cc/internal/imagefs"
	"j5.nz/cc/internal/virtio"
)

type Artifact struct {
	Kernel      []byte
	Initrd      []byte
	RootBlock   virtio.BlockBackend
	RootFS      imagefs.Directory
	ExtraBlocks []virtio.BlockBackend
	Metadata    map[string]string
	Cleanup     func() error
}

func (a Artifact) Close() error {
	if a.Cleanup == nil {
		return nil
	}
	return a.Cleanup()
}
