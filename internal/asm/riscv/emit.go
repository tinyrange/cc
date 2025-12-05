package riscv

import (
	"fmt"

	"github.com/tinyrange/cc/internal/asm"
)

type emitter struct {
	code   []byte
	labels map[asm.Label]int
}

// AddZeroConstant implements asm.Context.
func (e *emitter) AddZeroConstant(target asm.Variable, size int) {
	e.AddConstant(target, make([]byte, size))
}

// AddConstant implements asm.Context.
func (e *emitter) AddConstant(target asm.Variable, data []byte) {
	e.EmitBytes(data)
}

// EmitBytes implements asm.Context.
func (e *emitter) EmitBytes(data []byte) {
	e.code = append(e.code, data...)
}

// GetLabel implements asm.Context.
func (e *emitter) GetLabel(label asm.Label) (int, bool) {
	if e.labels == nil {
		return 0, false
	}
	offset, ok := e.labels[label]
	return offset, ok
}

// SetLabel implements asm.Context.
func (e *emitter) SetLabel(label asm.Label) {
	if e.labels == nil {
		e.labels = make(map[asm.Label]int)
	}
	e.labels[label] = len(e.code)
}

// EmitProgram lowers the provided fragment into an asm.Program.
func EmitProgram(frag asm.Fragment) (asm.Program, error) {
	if frag == nil {
		return asm.Program{}, fmt.Errorf("riscv: fragment must be non-nil")
	}

	em := &emitter{
		code:   make([]byte, 0, 64),
		labels: make(map[asm.Label]int),
	}

	if err := frag.Emit(em); err != nil {
		return asm.Program{}, err
	}

	return asm.NewProgram(em.code, nil, 0), nil
}
