package arm64

import (
	"fmt"

	"github.com/tinyrange/cc/internal/asm"
)

// Register identifiers exposed to callers. They intentionally mirror the AMD64
// package style so the IR backend can stay architecture-agnostic at the call
// site.
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
	SP
)

type operandSize uint8

const (
	size8  operandSize = 8
	size16 operandSize = 16
	size32 operandSize = 32
	size64 operandSize = 64
)

// Reg stores the logical register plus the width used by the instruction.
type Reg struct {
	id   asm.Variable
	size operandSize
}

func (r Reg) validate() error {
	if r.id < X0 || r.id > SP {
		return fmt.Errorf("arm64: invalid register %d", r.id)
	}
	switch r.size {
	case size8, size16, size32, size64:
		return nil
	default:
		return fmt.Errorf("arm64: unsupported register width %d", r.size)
	}
}

func Reg32(id asm.Variable) Reg { return Reg{id: id, size: size32} }
func Reg64(id asm.Variable) Reg { return Reg{id: id, size: size64} }
func Reg16(id asm.Variable) Reg { return Reg{id: id, size: size16} }
func Reg8(id asm.Variable) Reg  { return Reg{id: id, size: size8} }

// Memory represents [base + imm] style addressing. The initial encoder will
// only support base registers with 32-bit signed displacements; additional
// modes (scaled, register offset) can be added incrementally.
type Memory struct {
	base    Reg
	hasBase bool
	disp    int32
}

func Mem(base Reg) Memory {
	return Memory{base: base, hasBase: true}
}

func (m Memory) WithDisp(disp int32) Memory {
	m.disp = disp
	return m
}

func (m Memory) validate() error {
	if !m.hasBase {
		return fmt.Errorf("arm64 asm: memory reference missing base register")
	}
	if err := m.base.validate(); err != nil {
		return err
	}
	return nil
}

type fragmentFunc func(asm.Context) error

func (f fragmentFunc) Emit(ctx asm.Context) error {
	return f(ctx)
}
