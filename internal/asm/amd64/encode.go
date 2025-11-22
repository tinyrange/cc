package amd64

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/tinyrange/cc/internal/asm"
)

type rexState struct {
	w     bool
	r     bool
	x     bool
	b     bool
	force bool
}

func (r rexState) prefix() byte {
	if !r.w && !r.r && !r.x && !r.b && !r.force {
		return 0
	}
	p := byte(0x40)
	if r.w {
		p |= 0x08
	}
	if r.r {
		p |= 0x04
	}
	if r.x {
		p |= 0x02
	}
	if r.b {
		p |= 0x01
	}
	return p
}

func needsByteREX(id asm.Variable) bool {
	switch id {
	case RSP, RBP, RSI, RDI:
		return true
	}
	if id >= R8 && id <= R15 {
		return true
	}
	return false
}

func operandPrefix(size operandSize) (byte, bool) {
	if size == size16 {
		return 0x66, true
	}
	return 0x00, false
}

func regEncoding(reg Reg) (registerCode, error) {
	info, err := regInfo(reg.id)
	if err != nil {
		return registerCode{}, err
	}
	return info, nil
}

type memEncoding struct {
	modrm byte
	sib   []byte
	disp  []byte
	rex   rexState
}

func encodeMemoryOperand(mem Memory) (memEncoding, error) {
	if err := mem.validate(); err != nil {
		return memEncoding{}, err
	}

	baseInfo, err := regEncoding(mem.base)
	if err != nil {
		return memEncoding{}, err
	}

	var indexInfo registerCode
	if mem.hasIndex {
		indexInfo, err = regEncoding(mem.index)
		if err != nil {
			return memEncoding{}, err
		}
		if indexInfo.code == 4 {
			return memEncoding{}, fmt.Errorf("rsp cannot be used as index register")
		}
	}

	enc := memEncoding{
		rex: rexState{
			b: baseInfo.high,
			x: mem.hasIndex && indexInfo.high,
			force: (mem.hasIndex && indexInfo.needsRex) ||
				baseInfo.needsRex,
		},
	}

	rm := baseInfo.code

	disp := mem.disp
	switch {
	case disp == 0 && rm != 5:
		enc.modrm = 0x00
	case disp >= -128 && disp <= 127:
		enc.modrm = 0x40
		enc.disp = []byte{byte(disp)}
	default:
		enc.modrm = 0x80
		var buf [4]byte
		binary.LittleEndian.PutUint32(buf[:], uint32(disp))
		enc.disp = buf[:]
	}

	useSIB := mem.hasIndex || rm == 4
	if useSIB {
		rm = 4
		enc.rex.force = enc.rex.force || needsByteREX(mem.base.id) || (mem.hasIndex && needsByteREX(mem.index.id))

		indexCode := byte(4)
		if mem.hasIndex {
			indexCode = indexInfo.code
		}

		baseCode := baseInfo.code
		if !mem.hasBase {
			baseCode = 5
			if enc.modrm == 0x00 {
				enc.modrm = 0x00
				if enc.disp == nil {
					enc.disp = []byte{0, 0, 0, 0}
				} else if len(enc.disp) == 1 {
					d := int32(int8(enc.disp[0]))
					var buf [4]byte
					binary.LittleEndian.PutUint32(buf[:], uint32(d))
					enc.disp = buf[:]
				}
			}
		}

		if enc.modrm == 0x00 && baseCode == 5 {
			enc.modrm = 0x40
			enc.disp = []byte{0}
		}

		scaleBits := byte(0)
		switch mem.scale {
		case 1:
			scaleBits = 0
		case 2:
			scaleBits = 1
		case 4:
			scaleBits = 2
		case 8:
			scaleBits = 3
		default:
			return memEncoding{}, fmt.Errorf("invalid scale %d", mem.scale)
		}

		enc.sib = []byte{byte(scaleBits<<6) | byte(indexCode<<3) | byte(baseCode)}
	} else if enc.modrm == 0x00 && rm == 5 {
		// [rbp] / [r13] with zero displacement must use 8-bit displacement zero.
		enc.modrm = 0x40
		enc.disp = []byte{0}
	}

	enc.modrm |= rm
	return enc, nil
}

func encodeMovRegImm(reg Reg, value int64) ([]byte, error) {
	info, err := regEncoding(reg)
	if err != nil {
		return nil, err
	}

	prefix, hasPrefix := operandPrefix(reg.size)
	rex := rexState{
		w:     reg.size == size64,
		b:     info.high,
		force: info.needsRex && reg.size == size8,
	}

	opcode := byte(0)
	var imm []byte

	switch reg.size {
	case size64:
		opcode = 0xB8 + info.code
		imm = make([]byte, 8)
		binary.LittleEndian.PutUint64(imm, uint64(value))
	case size32:
		opcode = 0xB8 + info.code
		imm = make([]byte, 4)
		binary.LittleEndian.PutUint32(imm, uint32(value))
	case size16:
		opcode = 0xB8 + info.code
		imm = make([]byte, 2)
		binary.LittleEndian.PutUint16(imm, uint16(value))
	case size8:
		opcode = 0xB0 + info.code
		imm = []byte{byte(value)}
	default:
		return nil, fmt.Errorf("unsupported register width %d", reg.size)
	}

	out := make([]byte, 0, 1+1+len(imm))
	if hasPrefix {
		out = append(out, prefix)
	}
	if rexByte := rex.prefix(); rexByte != 0 {
		out = append(out, rexByte)
	}
	out = append(out, opcode)
	out = append(out, imm...)
	return out, nil
}

func encodeMovRegReg(dst, src Reg) ([]byte, error) {
	if dst.size != src.size {
		return nil, fmt.Errorf("mismatched register widths: %d vs %d", dst.size, src.size)
	}

	dstInfo, err := regEncoding(dst)
	if err != nil {
		return nil, err
	}
	srcInfo, err := regEncoding(src)
	if err != nil {
		return nil, err
	}

	prefix, hasPrefix := operandPrefix(dst.size)
	rex := rexState{
		w:     dst.size == size64,
		r:     srcInfo.high,
		b:     dstInfo.high,
		force: (dst.size == size8 && (dstInfo.needsRex || srcInfo.needsRex)),
	}

	opcode := byte(0x89)
	if dst.size == size8 {
		opcode = 0x88
	}

	modrm := byte(0xC0 | (srcInfo.code << 3) | dstInfo.code)

	out := make([]byte, 0, 4)
	if hasPrefix {
		out = append(out, prefix)
	}
	if rexByte := rex.prefix(); rexByte != 0 {
		out = append(out, rexByte)
	}
	out = append(out, opcode, modrm)
	return out, nil
}

func encodeCallReg(target Reg) ([]byte, error) {
	if target.size != size64 {
		return nil, fmt.Errorf("call target must be a 64-bit register")
	}

	info, err := regEncoding(target)
	if err != nil {
		return nil, err
	}

	rex := rexState{
		b:     info.high,
		force: info.needsRex,
	}

	out := make([]byte, 0, 3)
	if rexByte := rex.prefix(); rexByte != 0 {
		out = append(out, rexByte)
	}
	out = append(out, 0xFF)
	modrm := byte(0xD0 | info.code)
	out = append(out, modrm)
	return out, nil
}

func encodeMovMemReg(mem Memory, src Reg) ([]byte, error) {
	if src.size != size64 && src.size != size32 && src.size != size16 && src.size != size8 {
		return nil, fmt.Errorf("unsupported register width %d", src.size)
	}

	srcInfo, err := regEncoding(src)
	if err != nil {
		return nil, err
	}

	memEnc, err := encodeMemoryOperand(mem)
	if err != nil {
		return nil, err
	}

	prefix, hasPrefix := operandPrefix(src.size)
	rex := memEnc.rex
	rex.r = srcInfo.high
	rex.w = src.size == size64
	rex.force = rex.force || (src.size == size8 && srcInfo.needsRex)

	opcode := byte(0x89)
	if src.size == size8 {
		opcode = 0x88
	}

	out := make([]byte, 0, 8)
	if hasPrefix {
		out = append(out, prefix)
	}
	if rexByte := rex.prefix(); rexByte != 0 {
		out = append(out, rexByte)
	}

	modrm := memEnc.modrm | (srcInfo.code << 3)
	out = append(out, opcode, modrm)
	if len(memEnc.sib) > 0 {
		out = append(out, memEnc.sib...)
	}
	if len(memEnc.disp) > 0 {
		out = append(out, memEnc.disp...)
	}
	return out, nil
}

func encodeMovRegMem(dst Reg, mem Memory) ([]byte, error) {
	if dst.size != size64 && dst.size != size32 && dst.size != size16 && dst.size != size8 {
		return nil, fmt.Errorf("unsupported register width %d", dst.size)
	}

	dstInfo, err := regEncoding(dst)
	if err != nil {
		return nil, err
	}

	memEnc, err := encodeMemoryOperand(mem)
	if err != nil {
		return nil, err
	}

	prefix, hasPrefix := operandPrefix(dst.size)
	rex := memEnc.rex
	rex.r = dstInfo.high
	rex.w = dst.size == size64
	rex.force = rex.force || (dst.size == size8 && dstInfo.needsRex)

	opcode := byte(0x8B)
	if dst.size == size8 {
		opcode = 0x8A
	}

	out := make([]byte, 0, 8)
	if hasPrefix {
		out = append(out, prefix)
	}
	if rexByte := rex.prefix(); rexByte != 0 {
		out = append(out, rexByte)
	}

	modrm := memEnc.modrm | (dstInfo.code << 3)
	out = append(out, opcode, modrm)
	if len(memEnc.sib) > 0 {
		out = append(out, memEnc.sib...)
	}
	if len(memEnc.disp) > 0 {
		out = append(out, memEnc.disp...)
	}
	return out, nil
}

func encodeMovZXRegMem(dst Reg, mem Memory, srcSize operandSize) ([]byte, error) {
	if dst.size != size32 && dst.size != size64 {
		return nil, fmt.Errorf("movzx requires 32- or 64-bit destination, got %d", dst.size*8)
	}
	if srcSize != size8 && srcSize != size16 {
		return nil, fmt.Errorf("movzx supports 8- or 16-bit source, got %d", srcSize*8)
	}

	dstInfo, err := regEncoding(dst)
	if err != nil {
		return nil, err
	}

	memEnc, err := encodeMemoryOperand(mem)
	if err != nil {
		return nil, err
	}

	rex := memEnc.rex
	rex.r = dstInfo.high
	rex.w = dst.size == size64
	rex.force = rex.force || dstInfo.needsRex

	opcode := []byte{0x0F, 0xB6}
	if srcSize == size16 {
		opcode[1] = 0xB7
	}

	out := make([]byte, 0, 10)
	if rexByte := rex.prefix(); rexByte != 0 {
		out = append(out, rexByte)
	}
	out = append(out, opcode...)
	modrm := memEnc.modrm | (dstInfo.code << 3)
	out = append(out, modrm)
	if len(memEnc.sib) > 0 {
		out = append(out, memEnc.sib...)
	}
	if len(memEnc.disp) > 0 {
		out = append(out, memEnc.disp...)
	}
	return out, nil
}

func encodeALURegImm(op byte, reg Reg, value int32) ([]byte, error) {
	info, err := regEncoding(reg)
	if err != nil {
		return nil, err
	}

	prefix, hasPrefix := operandPrefix(reg.size)
	rex := rexState{
		w:     reg.size == size64,
		b:     info.high,
		force: info.needsRex && reg.size == size8,
	}

	out := make([]byte, 0, 10)
	if hasPrefix {
		out = append(out, prefix)
	}

	opcode := byte(0x81)
	var imm []byte

	switch reg.size {
	case size8:
		opcode = 0x80
		imm = []byte{byte(value)}
	case size16, size32, size64:
		if value >= math.MinInt8 && value <= math.MaxInt8 {
			opcode = 0x83
			imm = []byte{byte(value)}
		} else {
			opcode = 0x81
			imm = make([]byte, 4)
			binary.LittleEndian.PutUint32(imm, uint32(value))
		}
	default:
		return nil, fmt.Errorf("unsupported width %d", reg.size)
	}

	if rexByte := rex.prefix(); rexByte != 0 {
		out = append(out, rexByte)
	}
	out = append(out, opcode)
	modrm := byte(0xC0 | (op << 3) | info.code)
	out = append(out, modrm)
	out = append(out, imm...)
	return out, nil
}

func encodeALURegReg(opcode byte, dst, src Reg) ([]byte, error) {
	if dst.size != src.size {
		return nil, fmt.Errorf("mismatched register widths: %d vs %d", dst.size, src.size)
	}

	dstInfo, err := regEncoding(dst)
	if err != nil {
		return nil, err
	}
	srcInfo, err := regEncoding(src)
	if err != nil {
		return nil, err
	}

	prefix, hasPrefix := operandPrefix(dst.size)
	rex := rexState{
		w:     dst.size == size64,
		r:     srcInfo.high,
		b:     dstInfo.high,
		force: (dst.size == size8 && (dstInfo.needsRex || srcInfo.needsRex)),
	}

	out := make([]byte, 0, 4)
	if hasPrefix {
		out = append(out, prefix)
	}
	if rexByte := rex.prefix(); rexByte != 0 {
		out = append(out, rexByte)
	}

	modrm := byte(0xC0 | (srcInfo.code << 3) | dstInfo.code)
	out = append(out, opcode, modrm)
	return out, nil
}

func encodeTestRegRegSized(dst, src Reg) ([]byte, error) {
	if dst.size != src.size {
		return nil, fmt.Errorf("mismatched register widths: %d vs %d", dst.size, src.size)
	}
	dstInfo, err := regEncoding(dst)
	if err != nil {
		return nil, err
	}
	srcInfo, err := regEncoding(src)
	if err != nil {
		return nil, err
	}

	prefix, hasPrefix := operandPrefix(dst.size)
	rex := rexState{
		w:     dst.size == size64,
		r:     srcInfo.high,
		b:     dstInfo.high,
		force: (dst.size == size8 && (dstInfo.needsRex || srcInfo.needsRex)),
	}

	opcode := byte(0x85)
	if dst.size == size8 {
		opcode = 0x84
	}

	out := make([]byte, 0, 4)
	if hasPrefix {
		out = append(out, prefix)
	}
	if rexByte := rex.prefix(); rexByte != 0 {
		out = append(out, rexByte)
	}
	modrm := byte(0xC0 | (srcInfo.code << 3) | dstInfo.code)
	out = append(out, opcode, modrm)
	return out, nil
}

func chooseOpcode(size operandSize, wide, narrow byte) byte {
	if size == size8 {
		return narrow
	}
	return wide
}

func encodeImulRegImm(dst, src Reg, value int32) ([]byte, error) {
	if dst.size != src.size {
		return nil, fmt.Errorf("imul requires matching operand widths")
	}
	if dst.size != size16 && dst.size != size32 && dst.size != size64 {
		return nil, fmt.Errorf("imul unsupported width %d", dst.size*8)
	}

	dstInfo, err := regEncoding(dst)
	if err != nil {
		return nil, err
	}
	srcInfo, err := regEncoding(src)
	if err != nil {
		return nil, err
	}

	prefix, hasPrefix := operandPrefix(dst.size)
	rex := rexState{
		w:     dst.size == size64,
		r:     dstInfo.high,
		b:     srcInfo.high,
		force: (dst.size == size16 && (dstInfo.needsRex || srcInfo.needsRex)),
	}

	out := make([]byte, 0, 10)
	if hasPrefix {
		out = append(out, prefix)
	}
	if rexByte := rex.prefix(); rexByte != 0 {
		out = append(out, rexByte)
	}

	var opcode byte
	var imm []byte
	if value >= math.MinInt8 && value <= math.MaxInt8 {
		opcode = 0x6B
		imm = []byte{byte(value)}
	} else {
		opcode = 0x69
		imm = make([]byte, 4)
		binary.LittleEndian.PutUint32(imm, uint32(value))
	}

	out = append(out, opcode)
	modrm := byte(0xC0 | (dstInfo.code << 3) | srcInfo.code)
	out = append(out, modrm)
	out = append(out, imm...)
	return out, nil
}

func encodeMovMemImm8(mem Memory, value byte) ([]byte, error) {
	memEnc, err := encodeMemoryOperand(mem)
	if err != nil {
		return nil, err
	}

	rex := memEnc.rex
	rex.force = rex.force || needsByteREX(mem.base.id)

	out := make([]byte, 0, 8)
	if rexByte := rex.prefix(); rexByte != 0 {
		out = append(out, rexByte)
	}
	out = append(out, 0xC6)
	modrm := memEnc.modrm
	out = append(out, modrm)
	if len(memEnc.sib) > 0 {
		out = append(out, memEnc.sib...)
	}
	if len(memEnc.disp) > 0 {
		out = append(out, memEnc.disp...)
	}
	out = append(out, value)
	return out, nil
}

func encodeAndRegReg(dst, src Reg) ([]byte, error) {
	return encodeALURegReg(chooseOpcode(dst.size, 0x21, 0x20), dst, src)
}

func encodeOrRegImm(reg Reg, value int32) ([]byte, error) {
	return encodeALURegImm(0x01, reg, value)
}

func encodeOrRegReg(dst, src Reg) ([]byte, error) {
	return encodeALURegReg(chooseOpcode(dst.size, 0x09, 0x08), dst, src)
}

func encodeAndRegImm(reg Reg, value int32) ([]byte, error) {
	return encodeALURegImm(0x04, reg, value)
}

func encodeAddRegImm(reg Reg, value int32) ([]byte, error) {
	return encodeALURegImm(0x00, reg, value)
}

func encodeCmpRegImm(reg Reg, value int32) ([]byte, error) {
	return encodeALURegImm(0x07, reg, value)
}

func encodeCmpRegReg(dst, src Reg) ([]byte, error) {
	return encodeALURegReg(chooseOpcode(dst.size, 0x39, 0x38), dst, src)
}

func encodeAddRegReg(dst, src Reg) ([]byte, error) {
	return encodeALURegReg(chooseOpcode(dst.size, 0x01, 0x00), dst, src)
}

func encodeSubRegReg(dst, src Reg) ([]byte, error) {
	return encodeALURegReg(chooseOpcode(dst.size, 0x29, 0x28), dst, src)
}

func encodeXorRegRegSized(dst, src Reg) ([]byte, error) {
	return encodeALURegReg(chooseOpcode(dst.size, 0x31, 0x30), dst, src)
}

func encodeOutDXAL() []byte {
	return []byte{0xEE}
}

func encodeRdmsr() []byte {
	return []byte{0x0F, 0x32}
}

func encodeWrmsr() []byte {
	return []byte{0x0F, 0x30}
}

func encodeHlt() []byte {
	return []byte{0xF4}
}

func encodeShiftRegImm(reg Reg, count uint8, subcode byte) ([]byte, error) {
	if count == 0 {
		return nil, fmt.Errorf("shift count must be non-zero")
	}
	if reg.size != size8 && reg.size != size16 && reg.size != size32 && reg.size != size64 {
		return nil, fmt.Errorf("shift unsupported width %d", reg.size*8)
	}

	info, err := regEncoding(reg)
	if err != nil {
		return nil, err
	}

	prefix, hasPrefix := operandPrefix(reg.size)
	rex := rexState{
		w:     reg.size == size64,
		b:     info.high,
		force: info.needsRex,
	}

	opcode := byte(0xC1)
	if reg.size == size8 {
		opcode = 0xC0
	}

	out := make([]byte, 0, 6)
	if hasPrefix {
		out = append(out, prefix)
	}
	if rexByte := rex.prefix(); rexByte != 0 {
		out = append(out, rexByte)
	}

	modrm := byte(0xC0 | (subcode << 3) | info.code)

	out = append(out, opcode, modrm, byte(count))
	return out, nil
}

func encodeShrRegImm(reg Reg, count uint8) ([]byte, error) {
	return encodeShiftRegImm(reg, count, 5)
}

func encodeShlRegImm(reg Reg, count uint8) ([]byte, error) {
	return encodeShiftRegImm(reg, count, 4)
}
