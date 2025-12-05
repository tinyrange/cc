package riscv

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/tinyrange/cc/internal/asm"
)

const (
	X0 asm.Variable = iota
	X1
	X2
	X3
	X4
	X5
	X6
	X7
	X8
	X9
	X10
	X11
	X12
	X13
	X14
	X15
	X16
	X17
	X18
	X19
	X20
	X21
	X22
	X23
	X24
	X25
	X26
	X27
	X28
	X29
	X30
	X31
)

func Reg64(reg asm.Variable) asm.Variable { return reg }

type addImmediate struct {
	rd  asm.Variable
	rs1 asm.Variable
	imm int32
}

type shiftImmediate struct {
	rd    asm.Variable
	shamt uint32
	f3    uint32
	op    uint32
}

// AddRegImm emits ADDI rd, rs1, imm.
func AddRegImm(rd asm.Variable, imm int32) asm.Fragment {
	return addImmediate{rd: rd, rs1: rd, imm: imm}
}

// MovImmediate loads an immediate into rd, using ADDI when possible and LUI+ADDI
// for wider values. When emitting values with bit 31 set, the result is
// zero-extended to avoid LUI sign-extension on RV64.
func MovImmediate(rd asm.Variable, value int64) asm.Fragment {
	return &loadImmediate{rd: rd, value: value}
}

type loadImmediate struct {
	rd    asm.Variable
	value int64
}

// Slli shifts rd left by shamt bits.
func Slli(rd asm.Variable, shamt uint32) asm.Fragment {
	return shiftImmediate{rd: rd, shamt: shamt, f3: 1, op: 0x13}
}

// Srli shifts rd right logically by shamt bits.
func Srli(rd asm.Variable, shamt uint32) asm.Fragment {
	return shiftImmediate{rd: rd, shamt: shamt, f3: 5, op: 0x13}
}

type store64 struct {
	rs2  asm.Variable
	rs1  asm.Variable
	imm  int32
	f3   uint32
	op   uint32
	sign bool
}

// MovToMemory writes rs2 to [rs1+imm] using SD.
func MovToMemory(base asm.Variable, src asm.Variable, imm int32) asm.Fragment {
	return store64{rs1: base, rs2: src, imm: imm, f3: 3, op: 0x23}
}

type load64 struct {
	rd   asm.Variable
	rs1  asm.Variable
	imm  int32
	f3   uint32
	op   uint32
	sign bool
}

// MovFromMemory loads [rs1+imm] into rd using LD.
func MovFromMemory(rd asm.Variable, base asm.Variable, imm int32) asm.Fragment {
	return load64{rd: rd, rs1: base, imm: imm, f3: 3, op: 0x03}
}

type halt struct{}

// Halt terminates execution by storing zero to address zero, which triggers the
// stop-on-zero check in the emulator wrapper.
func Halt() asm.Fragment { return halt{} }

func (l addImmediate) Emit(ctx asm.Context) error {
	insn, err := encodeI(l.imm, uint32(l.rs1), 0, uint32(l.rd), 0x13)
	if err != nil {
		return err
	}
	emitInsn(ctx, insn)
	return nil
}

func (l *loadImmediate) Emit(ctx asm.Context) error {
	// If the value fits in a 12-bit signed immediate, a single ADDI is enough.
	if l.value >= -2048 && l.value <= 2047 {
		insn, err := encodeI(int32(l.value), uint32(X0), 0, uint32(l.rd), 0x13)
		if err != nil {
			return err
		}
		emitInsn(ctx, insn)
		return nil
	}

	zeroExtend := l.value >= 0 && l.value <= math.MaxUint32 && l.value > math.MaxInt32

	hi := (l.value + (1 << 11)) >> 12
	lo := l.value - (hi << 12)

	lui, err := encodeU(int32(hi), uint32(l.rd), 0x37)
	if err != nil {
		return err
	}
	addi, err := encodeI(int32(lo), uint32(l.rd), 0, uint32(l.rd), 0x13)
	if err != nil {
		return err
	}

	emitInsn(ctx, lui)
	emitInsn(ctx, addi)
	if zeroExtend {
		emitInsn(ctx, mustEncodeShift(l.rd, 32, 1, 0x13))
		emitInsn(ctx, mustEncodeShift(l.rd, 32, 5, 0x13))
	}
	return nil
}

func (s store64) Emit(ctx asm.Context) error {
	insn, err := encodeS(s.imm, uint32(s.rs1), uint32(s.rs2), s.f3, s.op)
	if err != nil {
		return err
	}
	emitInsn(ctx, insn)
	return nil
}

func (l load64) Emit(ctx asm.Context) error {
	insn, err := encodeI(l.imm, uint32(l.rs1), l.f3, uint32(l.rd), l.op)
	if err != nil {
		return err
	}
	emitInsn(ctx, insn)
	return nil
}

func (halt) Emit(ctx asm.Context) error {
	insn, err := encodeS(0, uint32(X0), uint32(X0), 2, 0x23)
	if err != nil {
		return err
	}
	emitInsn(ctx, insn)
	return nil
}

func (s shiftImmediate) Emit(ctx asm.Context) error {
	insn, err := encodeI(int32(s.shamt), uint32(s.rd), s.f3, uint32(s.rd), s.op)
	if err != nil {
		return err
	}
	emitInsn(ctx, insn)
	return nil
}

func emitInsn(ctx asm.Context, insn uint32) {
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], insn)
	ctx.EmitBytes(buf[:])
}

func encodeI(imm int32, rs1 uint32, funct3 uint32, rd uint32, opcode uint32) (uint32, error) {
	if imm < -2048 || imm > 2047 {
		return 0, fmt.Errorf("riscv: immediate %d out of range for I-type", imm)
	}
	uimm := uint32(imm) & 0xfff
	return (uimm << 20) | (rs1 << 15) | (funct3 << 12) | (rd << 7) | opcode, nil
}

func encodeS(imm int32, rs1 uint32, rs2 uint32, funct3 uint32, opcode uint32) (uint32, error) {
	if imm < -2048 || imm > 2047 {
		return 0, fmt.Errorf("riscv: immediate %d out of range for S-type", imm)
	}
	uimm := uint32(imm) & 0xfff
	immHi := (uimm >> 5) & 0x7f
	immLo := uimm & 0x1f

	return (immHi << 25) | (rs2 << 20) | (rs1 << 15) | (funct3 << 12) | (immLo << 7) | opcode, nil
}

func encodeU(imm int32, rd uint32, opcode uint32) (uint32, error) {
	uimm := uint32(imm) & 0xfffff
	return (uimm << 12) | (rd << 7) | opcode, nil
}

func mustEncodeShift(rd asm.Variable, shamt uint32, f3 uint32, op uint32) uint32 {
	insn, err := encodeI(int32(shamt), uint32(rd), f3, uint32(rd), op)
	if err != nil {
		panic(err)
	}
	return insn
}
