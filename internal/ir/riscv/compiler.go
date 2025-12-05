package riscv

import (
	"fmt"

	"github.com/tinyrange/cc/internal/asm"
	riscvasm "github.com/tinyrange/cc/internal/asm/riscv"
	"github.com/tinyrange/cc/internal/ir"
)

// Compile flattens an IR method into a single asm.Fragment sequence. Only
// architecture-specific asm fragments are supported for now.
func Compile(method ir.Method) (asm.Fragment, error) {
	seq := make([]asm.Fragment, 0, len(method))

	for _, frag := range method {
		switch v := frag.(type) {
		case asm.Fragment:
			seq = append(seq, v)
		case ir.Block:
			sub, err := Compile(ir.Method(v))
			if err != nil {
				return nil, err
			}
			seq = append(seq, sub)
		default:
			return nil, fmt.Errorf("riscv: unsupported IR fragment %T", frag)
		}
	}

	return asm.Group(seq), nil
}

// BuildStandaloneProgram builds a standalone program containing only the
// entrypoint method. Globals and cross-method calls are not yet supported.
func BuildStandaloneProgram(p *ir.Program) (asm.Program, error) {
	if p == nil {
		return asm.Program{}, fmt.Errorf("riscv: program must be non-nil")
	}
	if p.Entrypoint == "" {
		return asm.Program{}, fmt.Errorf("riscv: entrypoint must be specified")
	}

	entry, ok := p.Methods[p.Entrypoint]
	if !ok {
		return asm.Program{}, fmt.Errorf("riscv: entrypoint method %q not found", p.Entrypoint)
	}

	if len(p.Globals) > 0 {
		return asm.Program{}, fmt.Errorf("riscv: globals are not supported yet")
	}

	frag, err := Compile(entry)
	if err != nil {
		return asm.Program{}, fmt.Errorf("riscv: compile %q: %w", p.Entrypoint, err)
	}

	return riscvasm.EmitProgram(frag)
}
