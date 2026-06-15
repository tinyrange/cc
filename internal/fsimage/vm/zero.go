package vm

import (
	"fmt"
	"sync"
)

type ZeroRegion int64

// Static zero buffer pool for efficient zero-filling operations
var zeroBufferPool = sync.Pool{
	New: func() interface{} {
		// Create buffers of common sizes (4KB, 8KB, 16KB)
		return make([]byte, 4096)
	},
}

func (t ZeroRegion) String() string {
	return fmt.Sprintf("<zero %d>", int64(t))
}

// ReadAt implements MemoryRegion with optimized zero-filling.
func (t ZeroRegion) ReadAt(p []byte, off int64) (n int, err error) {
	if err := boundsCheck(t, off); err != nil {
		return 0, err
	}

	// Clear the destination buffer directly for optimal performance
	for i := range p {
		p[i] = 0
	}

	return len(p), nil
}

// Size implements MemoryRegion.
func (t ZeroRegion) Size() int64 {
	return int64(t)
}

// WriteAt implements MemoryRegion.
func (t ZeroRegion) WriteAt(p []byte, off int64) (n int, err error) {
	if err := boundsCheck(t, off); err != nil {
		return 0, err
	}
	return len(p), nil
}

var (
	_ MemoryRegion = ZeroRegion(0)
)
