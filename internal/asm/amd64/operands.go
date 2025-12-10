package amd64

import (
	"fmt"

	"github.com/tinyrange/cc/internal/asm"
)

type operandSize uint8

const (
	size8  operandSize = 1
	size16 operandSize = 2
	size32 operandSize = 4
	size64 operandSize = 8
)

// Reg represents a general-purpose register with an explicit operand size.
type Reg struct {
	id   asm.Variable
	size operandSize
}

func (r Reg) checkWidth(expected operandSize) error {
	if r.size != expected {
		return fmt.Errorf("expected %d-bit register, got %d-bit width", expected*8, r.size*8)
	}
	return nil
}

// Reg64 constructs a 64-bit register operand backed by the provided register id.
func Reg64(id asm.Variable) Reg { return Reg{id: id, size: size64} }

// Reg32 constructs a 32-bit register operand backed by the provided register id.
func Reg32(id asm.Variable) Reg { return Reg{id: id, size: size32} }

// Reg16 constructs a 16-bit register operand backed by the provided register id.
func Reg16(id asm.Variable) Reg { return Reg{id: id, size: size16} }

// Reg8 constructs an 8-bit register operand backed by the provided register id.
func Reg8(id asm.Variable) Reg { return Reg{id: id, size: size8} }

// Memory describes an effective address used by memory operands.
type Memory struct {
	base     Reg
	index    Reg
	disp     int32
	scale    uint8
	hasBase  bool
	hasIndex bool
}

// ControlReg identifies an x86_64 control register (e.g. CR0, CR3, CR4).
type ControlReg uint8

const (
	CR0 ControlReg = 0
	CR2 ControlReg = 2
	CR3 ControlReg = 3
	CR4 ControlReg = 4
	CR8 ControlReg = 8
)

// Mem constructs a memory operand referencing [base].
func Mem(base Reg) Memory {
	return Memory{
		base:    base,
		scale:   1,
		hasBase: true,
	}
}

// MemIndex constructs a memory operand referencing [base + index*scale].
func MemIndex(base Reg, index Reg, scale uint8) Memory {
	if scale == 0 {
		scale = 1
	}
	return Memory{
		base:     base,
		index:    index,
		scale:    scale,
		hasBase:  true,
		hasIndex: true,
	}
}

// WithDisp returns a copy of the memory operand with the supplied displacement added.
func (m Memory) WithDisp(disp int32) Memory {
	m.disp = disp
	return m
}

func (m Memory) validate() error {
	if !m.hasBase {
		return fmt.Errorf("memory operand requires base register")
	}
	if m.base.size != size64 {
		return fmt.Errorf("base register must be 64-bit")
	}
	if m.hasIndex {
		if m.index.size != size64 {
			return fmt.Errorf("index register must be 64-bit")
		}
		switch m.scale {
		case 1, 2, 4, 8:
		default:
			return fmt.Errorf("invalid index scale %d", m.scale)
		}
	}
	return nil
}

type fragmentFunc func(asm.Context) error

func (f fragmentFunc) Emit(ctx asm.Context) error { return f(ctx) }
