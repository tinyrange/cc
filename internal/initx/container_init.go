package initx

import (
	_ "embed"
	"encoding/binary"
	"fmt"
	"net"

	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/ir"
	"github.com/tinyrange/cc/internal/linux/defs"
	linux "github.com/tinyrange/cc/internal/linux/defs/amd64"
	"github.com/tinyrange/cc/internal/rtg"
)

//go:embed container_init_source.go
var rtgContainerInitSource string

// BuildContainerInitProgram builds the container init program using the RTG compiler.
// The source code is embedded from container_init_source.go with most helpers
// implemented as pure RTG code. Complex helpers (addDefaultRoute, execCommand,
// forkExecWait) are injected at IR level.

type ContainerInitConfig struct {
	Arch          hv.CpuArchitecture
	Cmd           []string
	Env           []string
	WorkDir       string
	EnableNetwork bool
	Exec          bool

	Hostname    string // default: tinyrange
	DNS         string // default: 10.42.0.1
	GuestIP     string // default: 10.42.0.2
	GuestMask   string // default: 255.255.255.0
	GuestIFName string // default: eth0
}

func (c *ContainerInitConfig) applyDefaults() {
	if c.Hostname == "" {
		c.Hostname = "tinyrange"
	}
	if c.DNS == "" {
		c.DNS = "10.42.0.1"
	}
	if c.GuestIP == "" {
		c.GuestIP = "10.42.0.2"
	}
	if c.GuestMask == "" {
		c.GuestMask = "255.255.255.0"
	}
	if c.GuestIFName == "" {
		c.GuestIFName = "eth0"
	}
	if c.WorkDir == "" {
		c.WorkDir = "/"
	}
}

func ipToUint32(addr string) uint32 {
	ip := net.ParseIP(addr)
	if ip == nil {
		return 0
	}
	ip4 := ip.To4()
	if ip4 == nil {
		return 0
	}
	return binary.BigEndian.Uint32(ip4)
}

// BuildContainerInitProgram builds the container init program using RTG source code.
// The source code is embedded from container_init_source.go which is a valid Go file
// for IDE completion support. Most helpers are pure RTG, with complex ones injected at IR level.
func BuildContainerInitProgram(cfg ContainerInitConfig) (*ir.Program, error) {
	cfg.applyDefaults()

	if cfg.Arch == "" || cfg.Arch == hv.ArchitectureInvalid {
		return nil, fmt.Errorf("initx: container init requires valid architecture")
	}
	if len(cfg.Cmd) == 0 || cfg.Cmd[0] == "" {
		return nil, fmt.Errorf("initx: container init requires non-empty command")
	}

	// Determine target architecture for runtime.GOARCH
	var goarch string
	switch cfg.Arch {
	case hv.ArchitectureX86_64:
		goarch = "amd64"
	case hv.ArchitectureARM64:
		goarch = "arm64"
	default:
		return nil, fmt.Errorf("unsupported architecture for RTG container init: %s", cfg.Arch)
	}

	// Build compile-time flags for Ifdef
	flags := map[string]bool{
		"network": cfg.EnableNetwork,
		"exec":    cfg.Exec,
	}

	// Build config values for pure RTG helpers
	config := map[string]any{
		"hostname": cfg.Hostname,
		"workdir":  cfg.WorkDir,
	}

	// Build hosts content
	hostsContent := "127.0.0.1\tlocalhost\n::1\t\tlocalhost ip6-localhost ip6-loopback\n"
	if cfg.Hostname != "" && cfg.Hostname != "localhost" {
		hostsContent += "127.0.0.1\t" + cfg.Hostname + "\n"
	}
	config["hosts_content"] = hostsContent

	// Network config (if enabled)
	// Note: resolv_content must always be set because the setResolvConf function
	// is compiled unconditionally (even if only called via Ifdef). The RTG compiler
	// resolves EmbedConfigString at compile time for all functions.
	if cfg.EnableNetwork {
		config["interface_name"] = cfg.GuestIFName
		config["interface_ip_nbo"] = int64(ipToNetworkByteOrder(cfg.GuestIP))
		config["interface_mask_nbo"] = int64(ipToNetworkByteOrder(cfg.GuestMask))
		config["gateway_nbo"] = int64(ipToNetworkByteOrder(cfg.DNS))
		config["resolv_content"] = "nameserver " + cfg.DNS + "\n"
	} else {
		// Provide empty default for resolv_content to allow compilation
		config["resolv_content"] = ""
	}

	// Compile the RTG source with architecture, flags, and config
	prog, err := rtg.CompileProgramWithOptions(rtgContainerInitSource, rtg.CompileOptions{
		GOARCH: goarch,
		Flags:  flags,
		Config: config,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to compile RTG container init source: %w", err)
	}

	// Replace placeholder method bodies for complex helpers that aren't pure RTG yet
	if err := injectContainerInitHelpers(prog, cfg); err != nil {
		return nil, fmt.Errorf("failed to inject container init helpers: %w", err)
	}

	return prog, nil
}

// ipToNetworkByteOrder converts an IP address string to network byte order uint32.
func ipToNetworkByteOrder(addr string) uint32 {
	ip := net.ParseIP(addr)
	if ip == nil {
		return 0
	}
	ip4 := ip.To4()
	if ip4 == nil {
		return 0
	}
	// Network byte order (big-endian) stored in little-endian memory
	// For 10.42.0.2: 0x0a2a0002 big-endian -> 0x02002a0a when read as little-endian
	return uint32(ip4[3])<<24 | uint32(ip4[2])<<16 | uint32(ip4[1])<<8 | uint32(ip4[0])
}

// injectContainerInitHelpers replaces placeholder method bodies with actual implementations.
// Most helpers are now pure RTG and compiled directly. This function only injects
// the complex helpers that aren't pure RTG yet (addDefaultRoute, execCommand, forkExecWait).
func injectContainerInitHelpers(prog *ir.Program, cfg ContainerInitConfig) error {
	errLabel := ir.Label("__cc_container_error")
	errVar := ir.Var("__cc_container_errno")

	// Complex network helpers (require dynamic data structures)
	if cfg.EnableNetwork {
		ip := ipToUint32(cfg.GuestIP)
		gateway := ipToUint32(cfg.DNS)
		mask := ipToUint32(cfg.GuestMask)

		// Each helper needs its own error label since methods are compiled separately
		configIfErrLabel := ir.Label("__cc_configif_error")
		addRouteErrLabel := ir.Label("__cc_addroute_error")

		prog.Methods["configureInterface"] = ir.Method{
			ConfigureInterface(cfg.GuestIFName, ip, mask, configIfErrLabel, errVar),
			ir.Return(ir.Int64(0)),
			ir.DeclareLabel(configIfErrLabel, ir.Block{
				ir.Printf("cc: configureInterface error: errno=0x%x\n", errVar),
				rebootFragment(cfg.Arch),
			}),
		}

		prog.Methods["addDefaultRoute"] = ir.Method{
			AddDefaultRoute(cfg.GuestIFName, gateway, addRouteErrLabel, errVar),
			ir.Return(ir.Int64(0)),
			ir.DeclareLabel(addRouteErrLabel, ir.Block{
				ir.Printf("cc: addDefaultRoute error: errno=0x%x\n", errVar),
				rebootFragment(cfg.Arch),
			}),
		}
	}

	// Command execution helpers (require dynamic argv/envp construction)
	execErrLabel := ir.Label("__cc_exec_error")
	forkErrLabel := ir.Label("__cc_fork_error")

	prog.Methods["execCommand"] = ir.Method{
		Exec(cfg.Cmd[0], cfg.Cmd[1:], cfg.Env, execErrLabel, errVar),
		ir.Return(ir.Int64(0)),
		ir.DeclareLabel(execErrLabel, ir.Block{
			ir.Printf("cc: execCommand error: errno=0x%x\n", errVar),
			rebootFragment(cfg.Arch),
		}),
	}

	prog.Methods["forkExecWait"] = ir.Method{
		ForkExecWait(cfg.Cmd[0], cfg.Cmd[1:], cfg.Env, forkErrLabel, errVar),
		ir.Return(errVar),
		ir.DeclareLabel(forkErrLabel, ir.Block{
			ir.Printf("cc: forkExecWait error: errno=0x%x\n", errVar),
			rebootFragment(cfg.Arch),
		}),
	}

	// Add error handler to main method
	main, ok := prog.Methods["main"]
	if !ok {
		return fmt.Errorf("main method not found")
	}

	// Append error handler label to main
	main = append(main,
		ir.DeclareLabel(errLabel, ir.Block{
			ir.Printf("cc: fatal error during boot: errno=0x%x\n", errVar),
			rebootFragment(cfg.Arch),
		}),
	)
	prog.Methods["main"] = main

	return nil
}

// rebootFragment returns the architecture-appropriate reboot syscall fragment.
func rebootFragment(arch hv.CpuArchitecture) ir.Fragment {
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
}
