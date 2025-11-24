package arm64

import (
	"github.com/tinyrange/cc/internal/asm"
	"github.com/tinyrange/cc/internal/ir"
)

type backend struct{}

func init() {
	ir.RegisterBackend(ir.ArchitectureARM64, backend{})
}

func (backend) BuildStandaloneProgram(p *ir.Program) (asm.Program, error) {
	return BuildStandaloneProgram(p)
}
