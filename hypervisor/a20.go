package hypervisor

import "j5.nz/cc/hypervisor/x86state"

// A20Layout clears address bit 20 while retaining device holes and shared RAM
// offsets. It describes mappings only and does not mutate the backing memory.
func A20Layout(regions []RAMRegion) ([]RAMRegion, error) {
	return x86state.A20Layout(regions)
}
