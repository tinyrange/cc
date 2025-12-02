package arm64

import (
	"fmt"

	"github.com/tinyrange/cc/internal/asm"
	"github.com/tinyrange/cc/internal/hv"
)

// EmitProgram lowers a fragment into machine code for AArch64.
func EmitProgram(fragment asm.Fragment) (asm.Program, error) {
	if fragment == nil {
		return asm.Program{}, fmt.Errorf("arm64 asm: fragment is nil")
	}

	ctx := newContext(hv.ArchitectureARM64)
	if err := fragment.Emit(ctx); err != nil {
		return asm.Program{}, err
	}
	return ctx.finalize()
}

// EmitBytes is a convenience helper returning the raw instruction stream for a fragment.
func EmitBytes(fragment asm.Fragment) ([]byte, error) {
	prog, err := EmitProgram(fragment)
	if err != nil {
		return nil, err
	}
	return prog.Bytes(), nil
}
