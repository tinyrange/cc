//go:build !linux

package virtio

import "os"

// Tail truncation and free-page reuse in imageDataStore provide the portable
// recovery path. Platforms with a range deallocation primitive can add it here
// without changing filesystem semantics.
func reclaimFileRange(*os.File, int64, int64) error { return errRangeReclaimUnsupported }

func allocatedFileBytes(file *os.File) (uint64, error) {
	info, err := file.Stat()
	if err != nil {
		return 0, err
	}
	return uint64(info.Size()), nil
}
