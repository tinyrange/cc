package initx

import (
	"context"

	"github.com/tinyrange/cc/internal/ir"
)

var bootProgram = &ir.Program{
	Entrypoint: "main",
	Methods: map[string]ir.Method{
		"main": {ir.Return(ir.Int64(0))},
	},
}

// Boot runs a minimal program to ensure the initx loop is running and devices are ready.
// This should be called before running the real payload program.
func (vm *VirtualMachine) Boot(ctx context.Context) error {
	return vm.Run(ctx, bootProgram)
}
