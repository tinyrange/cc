package hv

import (
	"fmt"
	"sync"
)

// AddressSpace manages physical address allocation for a VM.
// It tracks RAM regions and allocates MMIO regions above RAM to avoid conflicts.
type AddressSpace struct {
	mu sync.Mutex

	arch    CpuArchitecture
	ramBase uint64
	ramSize uint64

	// Split memory layout (x86_64 only, for >3GB RAM)
	// When isSplit is true, RAM is split around the PCI hole:
	//   - Low memory: [ramBase, ramBase+lowMemSize)
	//   - High memory: [highMemBase, highMemBase+highMemSize)
	isSplit     bool
	lowMemSize  uint64
	highMemBase uint64
	highMemSize uint64

	// nextMMIO is the next available address for MMIO allocation (above RAM)
	nextMMIO uint64

	// allocations holds all dynamically allocated MMIO regions
	allocations []MMIOAllocation

	// fixedRegions holds pre-determined MMIO regions (GIC, UART, HPET, etc.)
	fixedRegions []MMIOAllocation
}

// NewAddressSpace creates a new physical address allocator for a VM.
// MMIO allocations will start above ramBase+ramSize.
func NewAddressSpace(arch CpuArchitecture, ramBase, ramSize uint64) *AddressSpace {
	a := &AddressSpace{
		arch:    arch,
		ramBase: ramBase,
		ramSize: ramSize,
	}
	// Start MMIO allocation above RAM, aligned to 4KB
	a.nextMMIO = alignUp(ramBase+ramSize, 0x1000)
	return a
}

// NewAddressSpaceSplit creates a physical address allocator for split memory layouts.
// This is used on x86_64 when RAM exceeds the PCI hole (3GB-4GB).
// Low memory: [lowBase, lowBase+lowSize)
// High memory: [highBase, highBase+highSize)
// MMIO allocations start above the high memory region.
func NewAddressSpaceSplit(arch CpuArchitecture, lowBase, lowSize, highBase, highSize uint64) *AddressSpace {
	a := &AddressSpace{
		arch:        arch,
		ramBase:     lowBase,
		ramSize:     lowSize + highSize, // Total RAM for reporting purposes
		isSplit:     true,
		lowMemSize:  lowSize,
		highMemBase: highBase,
		highMemSize: highSize,
	}
	// For split memory, MMIO allocations start above high memory
	a.nextMMIO = alignUp(highBase+highSize, 0x1000)
	return a
}

// Allocate allocates an MMIO region with the specified requirements.
// The region is placed above RAM and aligned to the requested alignment.
func (a *AddressSpace) Allocate(req MMIOAllocationRequest) (MMIOAllocation, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if req.Size == 0 {
		return MMIOAllocation{}, fmt.Errorf("address_space: cannot allocate zero-size region for %s", req.Name)
	}

	alignment := req.Alignment
	if alignment == 0 {
		alignment = 0x1000 // Default to 4KB alignment
	}

	// Ensure alignment is a power of 2
	if alignment&(alignment-1) != 0 {
		return MMIOAllocation{}, fmt.Errorf("address_space: alignment 0x%x is not a power of 2 for %s", alignment, req.Name)
	}

	// Align the base address
	base := alignUp(a.nextMMIO, alignment)

	// Align the size up to alignment boundary
	size := alignUp(req.Size, alignment)

	alloc := MMIOAllocation{
		Name: req.Name,
		Base: base,
		Size: size,
	}

	a.allocations = append(a.allocations, alloc)
	a.nextMMIO = base + size

	return alloc, nil
}

// RegisterFixed registers a pre-determined MMIO region.
// Returns error if the region overlaps with RAM.
func (a *AddressSpace) RegisterFixed(name string, base, size uint64) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if size == 0 {
		return fmt.Errorf("address_space: cannot register zero-size fixed region %s", name)
	}

	regionEnd := base + size

	if a.isSplit {
		// Split memory layout: check overlap with both low and high memory regions
		lowMemEnd := a.ramBase + a.lowMemSize
		highMemEnd := a.highMemBase + a.highMemSize

		// Check overlap with low memory
		if base < lowMemEnd && regionEnd > a.ramBase {
			return fmt.Errorf("address_space: fixed region %s [0x%x-0x%x) overlaps low RAM [0x%x-0x%x)",
				name, base, regionEnd, a.ramBase, lowMemEnd)
		}

		// Check overlap with high memory
		if base < highMemEnd && regionEnd > a.highMemBase {
			return fmt.Errorf("address_space: fixed region %s [0x%x-0x%x) overlaps high RAM [0x%x-0x%x)",
				name, base, regionEnd, a.highMemBase, highMemEnd)
		}
	} else {
		// Contiguous memory layout
		ramEnd := a.ramBase + a.ramSize

		// Check for overlap with RAM
		if base < ramEnd && regionEnd > a.ramBase {
			return fmt.Errorf("address_space: fixed region %s [0x%x-0x%x) overlaps RAM [0x%x-0x%x)",
				name, base, regionEnd, a.ramBase, ramEnd)
		}
	}

	a.fixedRegions = append(a.fixedRegions, MMIOAllocation{
		Name: name,
		Base: base,
		Size: size,
	})

	return nil
}

// Allocations returns a copy of all dynamically allocated MMIO regions.
func (a *AddressSpace) Allocations() []MMIOAllocation {
	a.mu.Lock()
	defer a.mu.Unlock()

	result := make([]MMIOAllocation, len(a.allocations))
	copy(result, a.allocations)
	return result
}

// FixedRegions returns a copy of all fixed MMIO regions.
func (a *AddressSpace) FixedRegions() []MMIOAllocation {
	a.mu.Lock()
	defer a.mu.Unlock()

	result := make([]MMIOAllocation, len(a.fixedRegions))
	copy(result, a.fixedRegions)
	return result
}

// RAMBase returns the RAM base address.
func (a *AddressSpace) RAMBase() uint64 {
	return a.ramBase
}

// RAMSize returns the RAM size.
func (a *AddressSpace) RAMSize() uint64 {
	return a.ramSize
}

// RAMEnd returns the first address after RAM.
func (a *AddressSpace) RAMEnd() uint64 {
	return a.ramBase + a.ramSize
}

// Architecture returns the CPU architecture.
func (a *AddressSpace) Architecture() CpuArchitecture {
	return a.arch
}

// alignUp aligns value up to the specified alignment.
func alignUp(value, align uint64) uint64 {
	if align == 0 {
		return value
	}
	mask := align - 1
	return (value + mask) &^ mask
}
