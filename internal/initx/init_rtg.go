package initx

import (
	_ "embed"
	"fmt"

	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/ir"
	"github.com/tinyrange/cc/internal/linux/defs"
	"github.com/tinyrange/cc/internal/rtg"
)

//go:embed init_source.go
var rtgInitSource string

// BuildFromRTG builds the init program from RTG source code.
// This is the new implementation that uses RTG instead of direct IR construction.
// The source code is embedded from init_source.go which is a valid Go file
// for IDE completion support.
func BuildFromRTG(cfg BuilderConfig) (*ir.Program, error) {
	// Determine target architecture for runtime.GOARCH
	var goarch string
	switch cfg.Arch {
	case hv.ArchitectureX86_64:
		goarch = "amd64"
	case hv.ArchitectureARM64:
		goarch = "arm64"
	default:
		return nil, fmt.Errorf("unsupported architecture for RTG init: %s", cfg.Arch)
	}

	// Compile the RTG source with architecture information
	prog, err := rtg.CompileProgramWithOptions(rtgInitSource, rtg.CompileOptions{
		GOARCH: goarch,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to compile RTG init source: %w", err)
	}

	// If there are preload modules, we need to inject them into the main method
	// This is handled separately since RTG doesn't support binary data embedding
	if len(cfg.PreloadModules) > 0 {
		if err := injectModulePreloads(prog, cfg); err != nil {
			return nil, fmt.Errorf("failed to inject module preloads: %w", err)
		}
	}

	return prog, nil
}

// injectModulePreloads adds module loading code to the IR program.
// This is necessary because RTG doesn't support embedding binary data.
func injectModulePreloads(prog *ir.Program, cfg BuilderConfig) error {
	// Get the main method
	main, ok := prog.Methods["main"]
	if !ok {
		return fmt.Errorf("main method not found")
	}

	// Build preload fragments
	var preloads ir.Block
	moduleParamArg := any(ir.Int64(0))

	if len(cfg.PreloadModules) > 0 {
		moduleParamPtr := ir.Var("__initx_module_params_ptr")
		moduleParamArg = moduleParamPtr

		preloads = append(preloads, ir.LoadConstantBytesConfig(ir.ConstantBytesConfig{
			Target:        nextExecVar(),
			Data:          nil,
			ZeroTerminate: true,
			Pointer:       moduleParamPtr,
		}))
	}

	for idx, mod := range cfg.PreloadModules {
		dataPtr := ir.Var(fmt.Sprintf("__initx_module_%d_data_ptr", idx))
		dataLen := ir.Var(fmt.Sprintf("__initx_module_%d_data_len", idx))
		resultVar := ir.Var(fmt.Sprintf("__initx_module_%d_result", idx))

		preloads = append(preloads, ir.Block{
			ir.LoadConstantBytesConfig(ir.ConstantBytesConfig{
				Target:  nextExecVar(),
				Data:    mod.Data,
				Pointer: dataPtr,
				Length:  dataLen,
			}),
			LogKmsg(fmt.Sprintf("initx: loading module %s\n", mod.Name)),
			ir.Assign(resultVar, ir.Syscall(
				defs.SYS_INIT_MODULE,
				dataPtr,
				dataLen,
				moduleParamArg,
			)),
			rebootOnError(cfg.Arch, resultVar, fmt.Sprintf("initx: failed to load module %s (errno=0x%%x)\n", mod.Name)),
			LogKmsg(fmt.Sprintf("initx: module %s ready\n", mod.Name)),
		})
	}

	// Find the marker comment in main and inject preloads
	// For now, insert preloads near the beginning after the filesystem setup
	// We'll look for the console opening phase and insert before it
	var newMain ir.Method
	injected := false

	for _, frag := range main {
		// Look for the console open syscall and inject preloads before it
		if !injected {
			if assign, ok := frag.(ir.AssignFragment); ok {
				if v, ok := assign.Dst.(ir.Var); ok && string(v) == "consoleFd" {
					// Inject preloads here
					for _, p := range preloads {
						newMain = append(newMain, p)
					}
					injected = true
				}
			}
		}
		newMain = append(newMain, frag)
	}

	// If we couldn't find the injection point, append at end (shouldn't happen)
	if !injected && len(preloads) > 0 {
		return fmt.Errorf("could not find injection point for module preloads")
	}

	prog.Methods["main"] = newMain
	return nil
}
