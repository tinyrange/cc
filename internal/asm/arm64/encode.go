package arm64

import (
	"fmt"
)

func encodeAddImm64(dst, src Reg, imm uint16) (uint32, error) {
	if dst.size != size64 || src.size != size64 {
		return 0, fmt.Errorf("arm64 asm: ADD immediate requires 64-bit registers")
	}
	if imm > 0xFFF {
		return 0, fmt.Errorf("arm64 asm: immediate out of range for ADD (%d)", imm)
	}
	return 0x91000000 | (uint32(imm) << 10) | (uint32(src.id) << 5) | uint32(dst.id), nil
}

func encodeSubImm64(dst, src Reg, imm uint16) (uint32, error) {
	if dst.size != size64 || src.size != size64 {
		return 0, fmt.Errorf("arm64 asm: SUB immediate requires 64-bit registers")
	}
	if imm > 0xFFF {
		return 0, fmt.Errorf("arm64 asm: immediate out of range for SUB (%d)", imm)
	}
	return 0xD1000000 | (uint32(imm) << 10) | (uint32(src.id) << 5) | uint32(dst.id), nil
}

func encodeAddReg64(dst, left, right Reg) (uint32, error) {
	if dst.size != size64 || left.size != size64 || right.size != size64 {
		return 0, fmt.Errorf("arm64 asm: ADD register requires 64-bit operands")
	}
	return 0x8B000000 | (uint32(right.id) << 16) | (uint32(left.id) << 5) | uint32(dst.id), nil
}

func encodeSubReg64(dst, left, right Reg) (uint32, error) {
	if dst.size != size64 || left.size != size64 || right.size != size64 {
		return 0, fmt.Errorf("arm64 asm: SUB register requires 64-bit operands")
	}
	return 0xCB000000 | (uint32(right.id) << 16) | (uint32(left.id) << 5) | uint32(dst.id), nil
}

func encodeCmpReg64(left, right Reg) (uint32, error) {
	if left.size != size64 || right.size != size64 {
		return 0, fmt.Errorf("arm64 asm: CMP register requires 64-bit operands")
	}
	return 0xEB00001F | (uint32(right.id) << 16) | (uint32(left.id) << 5), nil
}

func encodeCmpImm64(reg Reg, imm uint16) (uint32, error) {
	if reg.size != size64 {
		return 0, fmt.Errorf("arm64 asm: CMP immediate requires 64-bit operand")
	}
	if imm > 0xFFF {
		return 0, fmt.Errorf("arm64 asm: immediate out of range for CMP (%d)", imm)
	}
	return 0xF100001F | (uint32(imm) << 10) | (uint32(reg.id) << 5), nil
}

func encodeTestZero(reg Reg) (uint32, error) {
	if reg.size != size64 {
		return 0, fmt.Errorf("arm64 asm: TST requires 64-bit operand")
	}
	return 0xEA00001F | (uint32(reg.id) << 16) | (uint32(reg.id) << 5), nil
}

func encodeMoveReg(dst, src Reg) (uint32, error) {
	switch {
	case dst.size == size64 && src.size == size64:
		return 0xAA0003E0 | (uint32(src.id) << 16) | uint32(dst.id), nil
	case dst.size <= size32 && src.size <= size32:
		return 0x2A0003E0 | (uint32(src.id) << 16) | uint32(dst.id), nil
	case dst.size == size64 && src.size <= size32:
		return 0x2A0003E0 | (uint32(src.id) << 16) | uint32(dst.id), nil
	default:
		return 0, fmt.Errorf("arm64 asm: unsupported MOV width dst=%d src=%d", dst.size, src.size)
	}
}

func encodeMovz(dst Reg, imm uint16, shift uint32) (uint32, error) {
	if dst.size != size64 {
		return 0, fmt.Errorf("arm64 asm: MOVZ requires 64-bit destination")
	}
	if shift%16 != 0 || shift > 48 {
		return 0, fmt.Errorf("arm64 asm: invalid MOVZ shift %d", shift)
	}
	hw := shift / 16
	return 0xD2800000 | (hw << 21) | (uint32(imm) << 5) | uint32(dst.id), nil
}

func encodeMovk(dst Reg, imm uint16, shift uint32) (uint32, error) {
	if dst.size != size64 {
		return 0, fmt.Errorf("arm64 asm: MOVK requires 64-bit destination")
	}
	if shift%16 != 0 || shift > 48 {
		return 0, fmt.Errorf("arm64 asm: invalid MOVK shift %d", shift)
	}
	hw := shift / 16
	return 0xF2800000 | (hw << 21) | (uint32(imm) << 5) | uint32(dst.id), nil
}

func encodeLogicalShift(dst, src Reg, shift uint32, right bool) (uint32, error) {
	if dst.size != size64 || src.size != size64 {
		return 0, fmt.Errorf("arm64 asm: shift instructions require 64-bit operands")
	}
	if shift > 63 {
		return 0, fmt.Errorf("arm64 asm: shift amount out of range (%d)", shift)
	}
	var immr, imms uint32
	if right {
		immr = shift & 63
		imms = 63
	} else {
		immr = (64 - shift) & 63
		imms = 63 - shift
	}
	return 0xD3400000 | (immr << 16) | (imms << 10) | (uint32(src.id) << 5) | uint32(dst.id), nil
}

func encodeAndReg(dst, left, right Reg) (uint32, error) {
	if dst.size != size64 || left.size != size64 || right.size != size64 {
		return 0, fmt.Errorf("arm64 asm: AND register requires 64-bit operands")
	}
	return 0x8A000000 | (uint32(right.id) << 16) | (uint32(left.id) << 5) | uint32(dst.id), nil
}

func encodeOrrRegZero(dst, src Reg) (uint32, error) {
	if dst.size != size64 || src.size != size64 {
		return 0, fmt.Errorf("arm64 asm: ORR requires 64-bit operands")
	}
	return 0xAA0003E0 | (uint32(src.id) << 16) | uint32(dst.id), nil
}

func encodeLoadStoreUnsigned(reg Reg, mem Memory, size literalWidth, store bool) (uint32, error) {
	if err := mem.validate(); err != nil {
		return 0, err
	}
	if mem.disp < 0 {
		return 0, fmt.Errorf("arm64 asm: negative offsets not supported in unsigned load/store")
	}
	var scale uint32
	var base uint32
	switch size {
	case literal64:
		scale = 3
		if store {
			base = 0xF9000000
		} else {
			base = 0xF9400000
		}
	case literal32:
		scale = 2
		if store {
			base = 0xB9000000
		} else {
			base = 0xB9400000
		}
	case literal16:
		scale = 1
		if store {
			base = 0x79000000
		} else {
			base = 0x79400000
		}
	case literal8:
		scale = 0
		if store {
			base = 0x39000000
		} else {
			base = 0x39400000
		}
	default:
		return 0, fmt.Errorf("arm64 asm: unsupported load/store width %d", size)
	}
	if dstSize := reg.size; (size == literal32 && dstSize != size32) ||
		(size == literal64 && dstSize != size64) ||
		(size == literal8 && dstSize != size32) ||
		(size == literal16 && dstSize != size32) {
		return 0, fmt.Errorf("arm64 asm: register width mismatch for load/store")
	}
	if mem.disp%int32(1<<scale) != 0 {
		return 0, fmt.Errorf("arm64 asm: misaligned offset %d", mem.disp)
	}
	imm := mem.disp / int32(1<<scale)
	if imm < 0 || imm > 0xFFF {
		return 0, fmt.Errorf("arm64 asm: offset out of range (%d)", mem.disp)
	}
	return base | (uint32(imm) << 10) | (uint32(mem.base.id) << 5) | uint32(reg.id), nil
}

func encodeLiteralLoad(reg Reg, width literalWidth) (uint32, error) {
	switch width {
	case literal64:
		if reg.size != size64 {
			return 0, fmt.Errorf("arm64 asm: literal 64-bit load requires X register")
		}
		return 0x58000000 | uint32(reg.id), nil
	case literal32:
		if reg.size != size32 {
			return 0, fmt.Errorf("arm64 asm: literal 32-bit load requires W register")
		}
		return 0x18000000 | uint32(reg.id), nil
	default:
		return 0, fmt.Errorf("arm64 asm: unsupported literal load width %d", width)
	}
}
