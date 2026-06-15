package vm

import (
	"fmt"
	"io"
)

// VirtualRegion provides a bounds-checked view into a parent VirtualStorage
// All operations are automatically translated to parent coordinates with bounds checking
type VirtualRegion struct {
	parent VirtualStorage
	offset int64
	size   int64
}

// NewVirtualRegion creates a new VirtualRegion that provides bounded access to a parent VirtualStorage
func NewVirtualRegion(parent VirtualStorage, offset, size int64) (*VirtualRegion, error) {
	if offset < 0 {
		return nil, fmt.Errorf("offset cannot be negative: %d", offset)
	}
	if size <= 0 {
		return nil, fmt.Errorf("size must be positive: %d", size)
	}
	if offset+size > parent.Size() {
		return nil, fmt.Errorf("region extends beyond parent bounds: offset=%d size=%d parent_size=%d", offset, size, parent.Size())
	}

	return &VirtualRegion{
		parent: parent,
		offset: offset,
		size:   size,
	}, nil
}

// Size returns the size of this virtual region
func (vr *VirtualRegion) Size() int64 {
	return vr.size
}

// PageSize returns the parent's page size
func (vr *VirtualRegion) PageSize() uint32 {
	return vr.parent.PageSize()
}

// ReadAt reads data from the virtual region at the specified offset
func (vr *VirtualRegion) ReadAt(p []byte, off int64) (n int, err error) {
	if err := vr.boundsCheck(off); err != nil {
		return 0, err
	}

	// Limit read to region boundaries
	if off+int64(len(p)) > vr.size {
		p = p[:vr.size-off]
	}

	// Translate to parent coordinates
	return vr.parent.ReadAt(p, vr.offset+off)
}

// WriteAt writes data to the virtual region at the specified offset
func (vr *VirtualRegion) WriteAt(p []byte, off int64) (n int, err error) {
	if err := vr.boundsCheck(off); err != nil {
		return 0, err
	}

	// Limit write to region boundaries
	if off+int64(len(p)) > vr.size {
		p = p[:vr.size-off]
	}

	// Translate to parent coordinates
	return vr.parent.WriteAt(p, vr.offset+off)
}

// Map maps a memory region at the specified offset within this virtual region
func (vr *VirtualRegion) Map(region MemoryRegion, offset int64) error {
	if err := vr.boundsCheck(offset); err != nil {
		return fmt.Errorf("map offset out of bounds: %w", err)
	}

	if offset+region.Size() > vr.size {
		return fmt.Errorf("mapped region extends beyond virtual region bounds: offset=%d region_size=%d vr_size=%d",
			offset, region.Size(), vr.size)
	}

	// Translate to parent coordinates
	return vr.parent.Map(region, vr.offset+offset)
}

// MapSlice maps a slice of a memory region at the specified offset within this virtual region
func (vr *VirtualRegion) MapSlice(region MemoryRegion, srcOffset, size, dstOffset int64) error {
	if err := vr.boundsCheck(dstOffset); err != nil {
		return fmt.Errorf("map slice offset out of bounds: %w", err)
	}

	if dstOffset+size > vr.size {
		return fmt.Errorf("mapped slice extends beyond virtual region bounds: offset=%d size=%d vr_size=%d",
			dstOffset, size, vr.size)
	}

	// Check if parent supports MapSlice
	if mapper, ok := vr.parent.(interface {
		MapSlice(region MemoryRegion, srcOffset, size, dstOffset int64) error
	}); ok {
		// Translate to parent coordinates
		return mapper.MapSlice(region, srcOffset, size, vr.offset+dstOffset)
	}

	// Fallback: create slice region and use regular Map
	sliceRegion := &directSliceRegion{
		base:   region,
		offset: srcOffset,
		size:   size,
	}
	return vr.parent.Map(sliceRegion, vr.offset+dstOffset)
}

// Reinterpret reinterprets memory at the specified offset within this virtual region
func (vr *VirtualRegion) Reinterpret(newRegion MemoryRegion, offset int64) error {
	if err := vr.boundsCheck(offset); err != nil {
		return fmt.Errorf("reinterpret offset out of bounds: %w", err)
	}

	if offset+newRegion.Size() > vr.size {
		return fmt.Errorf("reinterpreted region extends beyond virtual region bounds: offset=%d region_size=%d vr_size=%d",
			offset, newRegion.Size(), vr.size)
	}

	// Translate to parent coordinates
	return vr.parent.Reinterpret(newRegion, vr.offset+offset)
}

// boundsCheck verifies that an offset is within this virtual region
func (vr *VirtualRegion) boundsCheck(off int64) error {
	if off < 0 {
		return fmt.Errorf("offset cannot be negative: %d", off)
	}
	if off >= vr.size {
		return io.EOF
	}
	return nil
}

// Compile-time check that VirtualRegion implements VirtualStorage
var _ VirtualStorage = (*VirtualRegion)(nil)
