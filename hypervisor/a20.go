package hypervisor

import "fmt"

// A20Layout clears physical address bit 20 for CPU RAM accesses. Device holes
// are retained as holes; the PC device layer handles the corresponding MMIO
// alias. Regions are split at source boundaries as well as address-line edges.
func A20Layout(regions []RAMRegion) ([]RAMRegion, error) {
	const bit = uint64(1 << 20)
	var out []RAMRegion
	for _, r := range regions {
		if r.Size == 0 || r.Address > ^uint64(0)-r.Size {
			return nil, fmt.Errorf("invalid A20 region")
		}
		for address := r.Address; address < r.Address+r.Size; {
			size := min(r.Address+r.Size-address, bit-(address&(bit-1)))
			source := address &^ bit
			var mapped *RAMRegion
			for i := range regions {
				candidate := &regions[i]
				if source >= candidate.Address && source-candidate.Address < candidate.Size {
					mapped = candidate
					break
				}
				if candidate.Address > source && candidate.Address-source < size {
					size = candidate.Address - source
				}
			}
			if mapped != nil {
				size = min(size, mapped.Size-(source-mapped.Address))
				next := RAMRegion{Address: address, Offset: mapped.Offset + source - mapped.Address, Size: size}
				if len(out) != 0 && out[len(out)-1].Address+out[len(out)-1].Size == next.Address && out[len(out)-1].Offset+out[len(out)-1].Size == next.Offset {
					out[len(out)-1].Size += next.Size
				} else {
					out = append(out, next)
				}
			}
			address += size
		}
	}
	return out, nil
}
