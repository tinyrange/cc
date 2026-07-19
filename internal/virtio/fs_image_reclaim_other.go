//go:build !linux

package virtio

import "os"

// Tail truncation and free-page reuse in imageDataStore provide the portable
// recovery path. Platforms with a range deallocation primitive can add it here
// without changing filesystem semantics.
func reclaimFileRange(*os.File, int64, int64) error { return errRangeReclaimUnsupported }
