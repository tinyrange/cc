package initx

import (
	_ "embed"
	"fmt"

	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/ir"
	"github.com/tinyrange/cc/internal/linux/defs"
	linux "github.com/tinyrange/cc/internal/linux/defs/amd64"
	"github.com/tinyrange/cc/internal/linux/kernel"
	"github.com/tinyrange/cc/internal/rtg"
)

//go:embed init_source.go
var rtgInitSource string

type Module struct {
	Name string
	Data []byte
}

type BuilderConfig struct {
	Arch hv.CpuArchitecture

	PreloadModules []kernel.Module

	// TimesliceMMIOPhysAddr is the physical address of the timeslice MMIO region.
	// Guest code can write to this address to record timeslice markers.
	// If 0, uses the default value 0xf0001000.
	TimesliceMMIOPhysAddr uint64

	// VsockPort is the vsock port to use for program loading.
	// If 0, uses the default port (9998).
	VsockPort uint32
}

const (
	snapshotRequestValue   = 0xdeadbeef
	snapshotSignalPhysAddr = 0xf0000000
	mailboxMapSize         = 0x1000
)

const (
	mailboxRunResultDetailOffset   = 8
	mailboxRunResultStageOffset    = 12
	mailboxStartResultDetailOffset = 16
	mailboxStartResultStageOffset  = 20

	// Config region time fields (set by host for guest clock initialization)
	configTimeSecField  = 24
	configTimeNsecField = 32
)

func rebootOnError(arch hv.CpuArchitecture, val ir.Var, msg string) ir.Block {
	return ir.Block{
		ir.If(ir.IsNegative(val), ir.Block{
			ir.Printf(msg, ir.Op(ir.OpSub, ir.Int64(0), val)),
			func() ir.Fragment {
				switch arch {
				case hv.ArchitectureX86_64:
					return ir.Syscall(defs.SYS_REBOOT,
						linux.LINUX_REBOOT_MAGIC1,
						linux.LINUX_REBOOT_MAGIC2,
						linux.LINUX_REBOOT_CMD_RESTART,
						ir.Int64(0),
					)
				case hv.ArchitectureARM64:
					return ir.Syscall(defs.SYS_REBOOT,
						linux.LINUX_REBOOT_MAGIC1,
						linux.LINUX_REBOOT_MAGIC2,
						linux.LINUX_REBOOT_CMD_POWER_OFF,
						ir.Int64(0),
					)
				default:
					panic(fmt.Sprintf("unsupported architecture for reboot: %s", arch))
				}
			}(),
		}),
	}
}

// Build builds the init program using the RTG compiler.
// This is the main entry point for building init programs.
func Build(cfg BuilderConfig) (*ir.Program, error) {
	return BuildFromRTG(cfg)
}

// BuildFromRTG builds the init program from RTG source code.
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

	// Set up compile options with vsock configuration (vsock is always enabled)
	port := cfg.VsockPort
	if port == 0 {
		port = 9998 // Default vsock port
	}

	// Timeslice MMIO allows guest code to record timing markers that are captured
	// by the host hypervisor. The default address 0xf0001000 is handled by MMIO
	// handlers in HVF/KVM/WHP.
	timesliceAddr := cfg.TimesliceMMIOPhysAddr
	if timesliceAddr == 0 {
		timesliceAddr = 0xf0001000 // Default timeslice MMIO address
	}

	compileOpts := rtg.CompileOptions{
		GOARCH: goarch,
		Flags: map[string]bool{
			"USE_VSOCK": true, // Vsock is always enabled (MMIO paths have been removed)
		},
		Config: map[string]any{
			"VSOCK_PORT":               int64(port),
			"TIMESLICE_MMIO_PHYS_ADDR": int64(timesliceAddr), // Enable timeslice recording
		},
	}

	// Compile the RTG source with architecture information
	prog, err := rtg.CompileProgramWithOptions(rtgInitSource, compileOpts)
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
