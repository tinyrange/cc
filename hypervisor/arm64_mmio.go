package hypervisor

import "fmt"

// ARM64MMIOInstruction is a scalar load/store with an immediate address.
// Offset is signed; Mode is 0 (unscaled), 1 (post-index), or 3 (pre-index).
type ARM64MMIOInstruction struct {
	Size, Target, Base  int
	Write, Signed, Wide bool
	Offset              int64
	Mode                uint32
}

// DecodeARM64MMIOInstruction supplies the information absent from an ISV=0
// data abort. SIMD, atomics, register offsets and unpredictable writebacks
// fail explicitly rather than being mistaken for ordinary scalar accesses.
func DecodeARM64MMIOInstruction(word uint32) (ARM64MMIOInstruction, error) {
	if word&0x3f200000 != 0x38000000 || (word>>10)&3 == 2 {
		return ARM64MMIOInstruction{}, fmt.Errorf("unsupported ARM64 MMIO instruction %#08x", word)
	}
	size, opc := int(word>>30), (word>>22)&3
	i := ARM64MMIOInstruction{Size: 1 << size, Target: int(word & 31), Base: int((word >> 5) & 31), Mode: (word >> 10) & 3, Offset: int64(int32(word<<11) >> 23)}
	i.Write, i.Signed, i.Wide = opc == 0, opc >= 2, size == 3 || opc == 2
	if (opc == 2 && size == 3) || (opc == 3 && size >= 2) || (i.Mode != 0 && i.Base == i.Target && i.Base != 31) {
		return ARM64MMIOInstruction{}, fmt.Errorf("invalid ARM64 MMIO instruction %#08x", word)
	}
	return i, nil
}
