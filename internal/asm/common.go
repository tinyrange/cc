package asm

import (
	"encoding/binary"
	"fmt"
)

type Value interface {
}

type Immediate int64

var (
	_ Value = Immediate(0)
)

type Variable int

var (
	_ Value = Variable(0)
)

type Register Variable

var (
	_ Value = Register(0)
)

type Context interface {
	AddZeroConstant(target Variable, size int)
	AddConstant(target Variable, data []byte)
	EmitBytes(data []byte)

	GetLabel(label Label) (int, bool)
	SetLabel(label Label)
}

type Fragment interface {
	Emit(ctx Context) error
}

type Group []Fragment

var (
	_ Fragment = Group{}
)

func (g Group) Emit(ctx Context) error {
	for _, frag := range g {
		if err := frag.Emit(ctx); err != nil {
			return err
		}
	}
	return nil
}

type Label string

type labelDef struct {
	label Label
}

func MarkLabel(label Label) Fragment {
	return &labelDef{label: label}
}

func (l *labelDef) Emit(ctx Context) error {
	if _, exists := ctx.GetLabel(l.label); exists {
		return fmt.Errorf("label %q already defined", l.label)
	}
	ctx.SetLabel(l.label)
	return nil
}

type Program struct {
	code        []byte
	relocations []int
	bssSize     int
}

func (p Program) Bytes() []byte {
	return append([]byte(nil), p.code...)
}

func (p Program) Relocations() []int {
	return append([]int(nil), p.relocations...)
}

func (p Program) BSSSize() int {
	return p.bssSize
}

func (p Program) RelocatedCopy(base uintptr) []byte {
	out := append([]byte(nil), p.code...)
	for _, off := range p.relocations {
		if off < 0 || off+8 > len(out) {
			continue
		}
		val := binary.LittleEndian.Uint64(out[off:])
		binary.LittleEndian.PutUint64(out[off:], val+uint64(base))
	}
	return out
}

func (p Program) Clone() Program {
	return Program{
		code:        append([]byte(nil), p.code...),
		relocations: append([]int(nil), p.relocations...),
		bssSize:     p.bssSize,
	}
}

func NewProgram(code []byte, relocations []int, bss int) Program {
	return Program{
		code:        append([]byte(nil), code...),
		relocations: append([]int(nil), relocations...),
		bssSize:     bss,
	}
}

func String(s string) Value {
	return LiteralValue{
		Data:     append([]byte(nil), []byte(s)...),
		ZeroTerm: true,
	}
}

type LiteralValue struct {
	Data     []byte
	ZeroTerm bool
}

var (
	_ Value = LiteralValue{}
)

func LiteralBytes(data []byte) Value {
	return LiteralValue{
		Data:     append([]byte(nil), data...),
		ZeroTerm: false,
	}
}
