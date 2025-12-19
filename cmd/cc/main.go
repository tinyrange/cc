package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path"
	"runtime"
	"runtime/pprof"
	"strings"
	"time"

	"github.com/tinyrange/cc/internal/devices/virtio"
	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/hv/factory"
	"github.com/tinyrange/cc/internal/initx"
	"github.com/tinyrange/cc/internal/ir"
	"github.com/tinyrange/cc/internal/linux/defs"
	linux "github.com/tinyrange/cc/internal/linux/defs/amd64"
	"github.com/tinyrange/cc/internal/linux/kernel"
	"github.com/tinyrange/cc/internal/netstack"
	"github.com/tinyrange/cc/internal/oci"
	"github.com/tinyrange/cc/internal/vfs"
	"golang.org/x/term"
)

func main() {
	if err := run(); err != nil {
		var exitErr *initx.ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.Code)
		}
		fmt.Fprintf(os.Stderr, "cc: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cacheDir := flag.String("cache-dir", "", "Cache directory (default: ~/.config/cc/)")
	cpus := flag.Int("cpus", 1, "Number of vCPUs")
	memory := flag.Uint64("memory", 1024, "Memory in MB")
	debug := flag.Bool("debug", false, "Enable debug logging")
	cpuprofile := flag.String("cpuprofile", "", "Write CPU profile to file")
	memprofile := flag.String("memprofile", "", "Write memory profile to file")
	dmesg := flag.Bool("dmesg", false, "Print kernel dmesg during boot and runtime")
	network := flag.Bool("network", false, "Enable networking")
	timeout := flag.Duration("timeout", 0, "Timeout for the container")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [flags] <image> [command] [args...]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Run a command inside an OCI container image in a virtual machine.\n\n")
		fmt.Fprintf(os.Stderr, "Examples:\n")
		fmt.Fprintf(os.Stderr, "  %s alpine:latest /bin/sh -c 'echo hello'\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s ubuntu:22.04 ls -la\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Flags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *debug {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})))
	}

	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			return fmt.Errorf("create cpu profile file: %w", err)
		}
		defer f.Close()

		if err := pprof.StartCPUProfile(f); err != nil {
			return fmt.Errorf("start cpu profile: %w", err)
		}
		defer pprof.StopCPUProfile()
	}

	if *memprofile != "" {
		defer func() {
			f, err := os.Create(*memprofile)
			if err != nil {
				slog.Error("create memory profile file", "error", err)
				return
			}
			defer f.Close()

			if err := pprof.Lookup("heap").WriteTo(f, 0); err != nil {
				slog.Error("write memory profile", "error", err)
			}
		}()
	}

	args := flag.Args()
	if len(args) < 1 {
		flag.Usage()
		return fmt.Errorf("image reference required")
	}

	imageRef := args[0]
	var cmd []string
	if len(args) > 1 {
		cmd = args[1:]
	}

	// Determine target architecture
	hvArch, err := parseArchitecture(runtime.GOARCH)
	if err != nil {
		return err
	}

	// Create OCI client
	client, err := oci.NewClient(*cacheDir)
	if err != nil {
		return fmt.Errorf("create OCI client: %w", err)
	}

	slog.Debug("Pulling image", "ref", imageRef, "arch", hvArch)

	// Pull image
	img, err := client.PullForArch(imageRef, hvArch)
	if err != nil {
		return fmt.Errorf("pull image: %w", err)
	}

	slog.Debug("Image pulled", "layers", len(img.Layers))

	// Determine command to run
	execCmd := img.Command(cmd)
	if len(execCmd) == 0 {
		return fmt.Errorf("no command specified and image has no entrypoint/cmd")
	}

	pathEnv := extractInitialPath(img.Config.Env)
	workDir := containerWorkDir(img)

	// Create container filesystem
	containerFS, err := oci.NewContainerFS(img)
	if err != nil {
		return fmt.Errorf("create container filesystem: %w", err)
	}
	defer containerFS.Close()

	execCmd, err = resolveCommandPath(containerFS, execCmd, pathEnv, workDir)
	if err != nil {
		return fmt.Errorf("resolve command: %w", err)
	}

	slog.Debug("Running command", "cmd", execCmd)

	// Create VirtioFS backend with container filesystem as root
	fsBackend := vfs.NewVirtioFsBackendWithAbstract()
	if err := fsBackend.SetAbstractRoot(containerFS); err != nil {
		return fmt.Errorf("set container filesystem as root: %w", err)
	}

	// Create hypervisor
	h, err := factory.OpenWithArchitecture(hvArch)
	if err != nil {
		return fmt.Errorf("create hypervisor: %w", err)
	}
	defer h.Close()

	// Load kernel
	kernelLoader, err := kernel.LoadForArchitecture(hvArch)
	if err != nil {
		return fmt.Errorf("load kernel: %w", err)
	}

	// Create VM with VirtioFS
	opts := []initx.Option{
		initx.WithDeviceTemplate(virtio.FSTemplate{
			Tag:     "rootfs",
			Backend: fsBackend,
			Arch:    hvArch,
		}),
		initx.WithDebugLogging(*debug),
		initx.WithDmesgLogging(*dmesg),
		initx.WithStdin(os.Stdin),
	}

	// Add network device if enabled
	if *network {
		backend := netstack.New(slog.Default())

		mac := net.HardwareAddr{0x02, 0x00, 0x00, 0x00, 0x00, 0x01}

		netBackend, err := virtio.NewNetstackBackend(backend, mac)
		if err != nil {
			return fmt.Errorf("create netstack backend: %w", err)
		}

		opts = append(opts, initx.WithDeviceTemplate(virtio.NetTemplate{
			Backend: netBackend,
			MAC:     mac,
			Arch:    hvArch,
		}))
	}

	vm, err := initx.NewVirtualMachine(
		h,
		*cpus,
		*memory,
		kernelLoader,
		opts...,
	)
	if err != nil {
		return fmt.Errorf("create VM: %w", err)
	}
	defer vm.Close()

	// Build and run the container init program
	prog := buildContainerInit(hvArch, img, execCmd, *network)

	slog.Debug("Booting VM")

	// Boot the VM first to set up devices
	if err := func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		vm.VirtualCPUCall(0, func(vcpu hv.VirtualCPU) error {
			debug, ok := vcpu.(hv.VirtualCPUDebug)
			if !ok {
				return nil
			}
			return debug.EnableTrace(64)
		})

		if err := vm.Run(ctx, &ir.Program{
			Entrypoint: "main",
			Methods: map[string]ir.Method{
				"main": {
					ir.Return(ir.Int64(0)),
				},
			},
		}); err != nil {
			var exitErr *initx.ExitError
			if errors.As(err, &exitErr) {
				return exitErr
			}
			fmt.Fprintf(os.Stderr, "cc: VM boot failed: %v\n", err)
			if err := vm.VirtualCPUCall(0, func(vcpu hv.VirtualCPU) error {
				if vm.Architecture() == hv.ArchitectureX86_64 {
					// figure out the current state
					regs := map[hv.Register]hv.RegisterValue{
						hv.RegisterAMD64Rax:    hv.Register64(0),
						hv.RegisterAMD64Rbx:    hv.Register64(0),
						hv.RegisterAMD64Rcx:    hv.Register64(0),
						hv.RegisterAMD64Rdx:    hv.Register64(0),
						hv.RegisterAMD64Rsi:    hv.Register64(0),
						hv.RegisterAMD64Rdi:    hv.Register64(0),
						hv.RegisterAMD64Rsp:    hv.Register64(0),
						hv.RegisterAMD64Rbp:    hv.Register64(0),
						hv.RegisterAMD64Rip:    hv.Register64(0),
						hv.RegisterAMD64Rflags: hv.Register64(0),
					}

					if err := vcpu.GetRegisters(regs); err != nil {
						return fmt.Errorf("get registers: %w", err)
					}

					fmt.Fprintf(os.Stderr, "cc: VM boot failed, vCPU state:\n"+
						"  RAX:    0x%016x\n"+
						"  RBX:    0x%016x\n"+
						"  RCX:    0x%016x\n"+
						"  RDX:    0x%016x\n"+
						"  RSI:    0x%016x\n"+
						"  RDI:    0x%016x\n"+
						"  RSP:    0x%016x\n"+
						"  RBP:    0x%016x\n"+
						"  RIP:    0x%016x\n"+
						"  RFLAGS: 0x%016x\n",
						regs[hv.RegisterAMD64Rax],
						regs[hv.RegisterAMD64Rbx],
						regs[hv.RegisterAMD64Rcx],
						regs[hv.RegisterAMD64Rdx],
						regs[hv.RegisterAMD64Rsi],
						regs[hv.RegisterAMD64Rdi],
						regs[hv.RegisterAMD64Rsp],
						regs[hv.RegisterAMD64Rbp],
						regs[hv.RegisterAMD64Rip],
						regs[hv.RegisterAMD64Rflags],
					)

					pc, err := vm.DumpStackTrace(vcpu)
					if err != nil {
						return fmt.Errorf("dump stack trace: %w", err)
					}

					// Dump a hexdump of RIP to RIP+128 bytes
					mem := make([]byte, 128)
					if _, err := vcpu.VirtualMachine().ReadAt(mem, pc); err != nil {
						return fmt.Errorf("read memory at RIP: %w", err)
					}
					func(mem []byte) {
						const bytesPerLine = 16
						for i := 0; i < len(mem); i += bytesPerLine {
							lineEnd := min(i+bytesPerLine, len(mem))
							line := mem[i:lineEnd]
							fmt.Fprintf(os.Stderr, "  %016x: ", uint64(pc)+uint64(i))
							for j := range bytesPerLine {
								if j < len(line) {
									fmt.Fprintf(os.Stderr, "%02x ", line[j])
								} else {
									fmt.Fprintf(os.Stderr, "   ")
								}
							}
							fmt.Fprintf(os.Stderr, " ")
							for _, b := range line {
								if b >= 32 && b <= 126 {
									fmt.Fprintf(os.Stderr, "%c", b)
								} else {
									fmt.Fprintf(os.Stderr, ".")
								}
							}
							fmt.Fprintf(os.Stderr, "\n")
						}
					}(mem)
				} else if vm.Architecture() == hv.ArchitectureARM64 {
					// figure out the current state
					regs := map[hv.Register]hv.RegisterValue{
						hv.RegisterARM64X0:     hv.Register64(0),
						hv.RegisterARM64X1:     hv.Register64(0),
						hv.RegisterARM64X2:     hv.Register64(0),
						hv.RegisterARM64X3:     hv.Register64(0),
						hv.RegisterARM64X4:     hv.Register64(0),
						hv.RegisterARM64X5:     hv.Register64(0),
						hv.RegisterARM64X6:     hv.Register64(0),
						hv.RegisterARM64X7:     hv.Register64(0),
						hv.RegisterARM64X8:     hv.Register64(0),
						hv.RegisterARM64X9:     hv.Register64(0),
						hv.RegisterARM64X10:    hv.Register64(0),
						hv.RegisterARM64X11:    hv.Register64(0),
						hv.RegisterARM64X12:    hv.Register64(0),
						hv.RegisterARM64X13:    hv.Register64(0),
						hv.RegisterARM64X14:    hv.Register64(0),
						hv.RegisterARM64X15:    hv.Register64(0),
						hv.RegisterARM64X16:    hv.Register64(0),
						hv.RegisterARM64X17:    hv.Register64(0),
						hv.RegisterARM64X18:    hv.Register64(0),
						hv.RegisterARM64X19:    hv.Register64(0),
						hv.RegisterARM64X20:    hv.Register64(0),
						hv.RegisterARM64X21:    hv.Register64(0),
						hv.RegisterARM64X22:    hv.Register64(0),
						hv.RegisterARM64X23:    hv.Register64(0),
						hv.RegisterARM64X24:    hv.Register64(0),
						hv.RegisterARM64X25:    hv.Register64(0),
						hv.RegisterARM64X26:    hv.Register64(0),
						hv.RegisterARM64X27:    hv.Register64(0),
						hv.RegisterARM64X28:    hv.Register64(0),
						hv.RegisterARM64X29:    hv.Register64(0),
						hv.RegisterARM64X30:    hv.Register64(0),
						hv.RegisterARM64Sp:     hv.Register64(0),
						hv.RegisterARM64Pc:     hv.Register64(0),
						hv.RegisterARM64Pstate: hv.Register64(0),
						hv.RegisterARM64Vbar:   hv.Register64(0),
					}

					if err := vcpu.GetRegisters(regs); err != nil {
						return fmt.Errorf("get registers: %w", err)
					}

					fmt.Fprintf(os.Stderr, "cc: VM boot failed, vCPU state:\n"+
						"  X0:    0x%016x\n"+
						"  X1:    0x%016x\n"+
						"  X2:    0x%016x\n"+
						"  X3:    0x%016x\n"+
						"  X4:    0x%016x\n"+
						"  X5:    0x%016x\n"+
						"  X6:    0x%016x\n"+
						"  X7:    0x%016x\n"+
						"  X8:    0x%016x\n"+
						"  X9:    0x%016x\n"+
						"  X10:    0x%016x\n"+
						"  X11:    0x%016x\n"+
						"  X12:    0x%016x\n"+
						"  X13:    0x%016x\n"+
						"  X14:    0x%016x\n"+
						"  X15:    0x%016x\n"+
						"  X16:    0x%016x\n"+
						"  X17:    0x%016x\n"+
						"  X18:    0x%016x\n"+
						"  X19:    0x%016x\n"+
						"  X20:    0x%016x\n"+
						"  X21:    0x%016x\n"+
						"  X22:    0x%016x\n"+
						"  X23:    0x%016x\n"+
						"  X24:    0x%016x\n"+
						"  X25:    0x%016x\n"+
						"  X26:    0x%016x\n"+
						"  X27:    0x%016x\n"+
						"  X28:    0x%016x\n"+
						"  X29:    0x%016x\n"+
						"  X30:    0x%016x\n"+
						"  SP:    0x%016x\n"+
						"  PC:    0x%016x\n"+
						"  PSTATE:    0x%016x\n"+
						"  VBAR:    0x%016x\n",
						regs[hv.RegisterARM64X0],
						regs[hv.RegisterARM64X1],
						regs[hv.RegisterARM64X2],
						regs[hv.RegisterARM64X3],
						regs[hv.RegisterARM64X4],
						regs[hv.RegisterARM64X5],
						regs[hv.RegisterARM64X6],
						regs[hv.RegisterARM64X7],
						regs[hv.RegisterARM64X8],
						regs[hv.RegisterARM64X9],
						regs[hv.RegisterARM64X10],
						regs[hv.RegisterARM64X11],
						regs[hv.RegisterARM64X12],
						regs[hv.RegisterARM64X13],
						regs[hv.RegisterARM64X14],
						regs[hv.RegisterARM64X15],
						regs[hv.RegisterARM64X16],
						regs[hv.RegisterARM64X17],
						regs[hv.RegisterARM64X18],
						regs[hv.RegisterARM64X19],
						regs[hv.RegisterARM64X20],
						regs[hv.RegisterARM64X21],
						regs[hv.RegisterARM64X22],
						regs[hv.RegisterARM64X23],
						regs[hv.RegisterARM64X24],
						regs[hv.RegisterARM64X25],
						regs[hv.RegisterARM64X26],
						regs[hv.RegisterARM64X27],
						regs[hv.RegisterARM64X28],
						regs[hv.RegisterARM64X29],
						regs[hv.RegisterARM64X30],
						regs[hv.RegisterARM64Sp],
						regs[hv.RegisterARM64Pc],
						regs[hv.RegisterARM64Pstate],
						regs[hv.RegisterARM64Vbar],
					)
				}

				// Get the trace buffer
				if debug, ok := vcpu.(hv.VirtualCPUDebug); ok {
					trace, err := debug.GetTraceBuffer()
					if err != nil {
						return fmt.Errorf("get trace buffer: %w", err)
					}

					fmt.Fprintf(os.Stderr, "\ntrace buffer:\n")

					for _, entry := range trace {
						fmt.Fprintf(os.Stderr, "  %s\n", entry)
					}
				}

				return nil
			}); err != nil {
				return fmt.Errorf("post-boot vCPU call: %w", err)
			}

			return fmt.Errorf("boot VM: %w", err)
		}

		return nil
	}(); err != nil {
		return err
	}

	var ctx context.Context
	if *timeout > 0 {
		newCtx, cancel := context.WithTimeout(context.Background(), *timeout)
		defer cancel()
		ctx = newCtx
	} else {
		ctx = context.Background()
	}

	// Put stdin into raw mode so we don't send cooked/echoed characters into the guest.
	// Do this after booting so that any Ctrl+C during boot still works to kill cc itself.
	if term.IsTerminal(int(os.Stdin.Fd())) {
		oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
		if err != nil {
			return fmt.Errorf("enable raw mode: %w", err)
		}
		defer term.Restore(int(os.Stdin.Fd()), oldState)
	}

	if err := vm.Run(ctx, prog); err != nil {
		var exitErr *initx.ExitError
		if errors.As(err, &exitErr) {
			return exitErr
		}
		return fmt.Errorf("run VM: %w", err)
	}

	return nil
}

func parseArchitecture(arch string) (hv.CpuArchitecture, error) {
	switch arch {
	case "amd64", "x86_64":
		return hv.ArchitectureX86_64, nil
	case "arm64", "aarch64":
		return hv.ArchitectureARM64, nil
	default:
		return "", fmt.Errorf("unsupported architecture: %s", arch)
	}
}

const defaultPathEnv = "/bin:/usr/bin"

func extractInitialPath(env []string) string {
	for _, entry := range env {
		if after, ok := strings.CutPrefix(entry, "PATH="); ok {
			return after
		}
	}
	return defaultPathEnv
}

func containerWorkDir(img *oci.Image) string {
	if img.Config.WorkingDir == "" {
		return "/"
	}
	return img.Config.WorkingDir
}

func resolveCommandPath(fs *oci.ContainerFS, cmd []string, pathEnv string, workDir string) ([]string, error) {
	if len(cmd) == 0 {
		return nil, fmt.Errorf("empty command")
	}

	resolved := make([]string, len(cmd))
	copy(resolved, cmd)

	if strings.Contains(resolved[0], "/") {
		return resolved, nil
	}

	resolvedPath, err := lookPath(fs, pathEnv, workDir, resolved[0])
	if err != nil {
		return nil, err
	}
	resolved[0] = resolvedPath
	return resolved, nil
}

func lookPath(fs *oci.ContainerFS, pathEnv string, workDir string, file string) (string, error) {
	if file == "" {
		return "", fmt.Errorf("executable name is empty")
	}
	if pathEnv == "" {
		pathEnv = defaultPathEnv
	}
	if workDir == "" {
		workDir = "/"
	}

	for dir := range strings.SplitSeq(pathEnv, ":") {
		switch {
		case dir == "":
			dir = workDir
		case !path.IsAbs(dir):
			dir = path.Join(workDir, dir)
		}

		candidate := path.Join(dir, file)
		entry, err := fs.Lookup(candidate)
		if err != nil {
			continue
		}

		if entry.File == nil {
			continue
		}
		_, mode := entry.File.Stat()
		if mode.IsDir() || mode&0o111 == 0 {
			continue
		}

		return candidate, nil
	}

	return "", fmt.Errorf("executable %q not found in PATH", file)
}

func buildContainerInit(arch hv.CpuArchitecture, img *oci.Image, cmd []string, enableNetwork bool) *ir.Program {
	errLabel := ir.Label("__cc_error")
	errVar := ir.Var("__cc_errno")
	pivotResult := ir.Var("__cc_pivot_result")

	workDir := containerWorkDir(img)

	main := ir.Method{
		// Create mount points
		ir.Syscall(defs.SYS_MKDIRAT, ir.Int64(linux.AT_FDCWD), "/mnt", ir.Int64(0o755)),
		ir.Syscall(defs.SYS_MKDIRAT, ir.Int64(linux.AT_FDCWD), "/proc", ir.Int64(0o755)),
		ir.Syscall(defs.SYS_MKDIRAT, ir.Int64(linux.AT_FDCWD), "/sys", ir.Int64(0o755)),

		// Mount virtiofs
		ir.Assign(errVar, ir.Syscall(
			defs.SYS_MOUNT,
			"rootfs",
			"/mnt",
			"virtiofs",
			ir.Int64(0),
			"",
		)),
		ir.If(ir.IsNegative(errVar), ir.Block{
			ir.Printf("cc: failed to mount virtiofs: errno=0x%x\n", ir.Op(ir.OpSub, ir.Int64(0), errVar)),
			ir.Goto(errLabel),
		}),

		// Create necessary directories in container
		ir.Syscall(defs.SYS_MKDIRAT, ir.Int64(linux.AT_FDCWD), "/mnt/proc", ir.Int64(0o755)),
		ir.Syscall(defs.SYS_MKDIRAT, ir.Int64(linux.AT_FDCWD), "/mnt/sys", ir.Int64(0o755)),
		ir.Syscall(defs.SYS_MKDIRAT, ir.Int64(linux.AT_FDCWD), "/mnt/dev", ir.Int64(0o755)),
		ir.Syscall(defs.SYS_MKDIRAT, ir.Int64(linux.AT_FDCWD), "/mnt/tmp", ir.Int64(0o1777)),

		// Mount proc
		ir.Syscall(
			defs.SYS_MOUNT,
			"proc",
			"/mnt/proc",
			"proc",
			ir.Int64(0),
			"",
		),

		// Mount sysfs
		ir.Syscall(
			defs.SYS_MOUNT,
			"sysfs",
			"/mnt/sys",
			"sysfs",
			ir.Int64(0),
			"",
		),

		// Mount devtmpfs
		ir.Syscall(
			defs.SYS_MOUNT,
			"devtmpfs",
			"/mnt/dev",
			"devtmpfs",
			ir.Int64(0),
			"",
		),

		// Change root to container using pivot_root
		ir.Assign(errVar, ir.Syscall(defs.SYS_CHDIR, "/mnt")),
		ir.If(ir.IsNegative(errVar), ir.Block{
			ir.Printf("cc: failed to chdir to /mnt: errno=0x%x\n", ir.Op(ir.OpSub, ir.Int64(0), errVar)),
			ir.Goto(errLabel),
		}),

		// pivot_root
		ir.Syscall(defs.SYS_MKDIRAT, ir.Int64(linux.AT_FDCWD), "oldroot", ir.Int64(0o755)),
		ir.Assign(pivotResult, ir.Syscall(defs.SYS_PIVOT_ROOT, ".", "oldroot")),
		ir.Assign(errVar, pivotResult),
		ir.If(ir.IsNegative(pivotResult), ir.Block{
			// Fall back to chroot if pivot_root fails
			ir.Assign(errVar, ir.Syscall(defs.SYS_CHROOT, ".")),
			ir.If(ir.IsNegative(errVar), ir.Block{
				ir.Printf("cc: failed to chroot: errno=0x%x\n", ir.Op(ir.OpSub, ir.Int64(0), errVar)),
				ir.Goto(errLabel),
			}),
		}),
		ir.If(ir.IsGreaterOrEqual(pivotResult, ir.Int64(0)), ir.Block{
			ir.Assign(errVar, ir.Syscall(defs.SYS_CHDIR, "/")),
			ir.If(ir.IsNegative(errVar), ir.Block{
				ir.Printf("cc: failed to chdir to new root: errno=0x%x\n", ir.Op(ir.OpSub, ir.Int64(0), errVar)),
				ir.Goto(errLabel),
			}),
			ir.Assign(errVar, ir.Syscall(defs.SYS_UMOUNT2, "/oldroot", ir.Int64(linux.MNT_DETACH))),
			ir.If(ir.IsNegative(errVar), ir.Block{
				ir.Printf("cc: failed to unmount oldroot: errno=0x%x\n", ir.Op(ir.OpSub, ir.Int64(0), errVar)),
				ir.Goto(errLabel),
			}),
		}),

		// Always cleanup oldroot
		ir.Assign(errVar, ir.Syscall(defs.SYS_UNLINKAT, ir.Int64(linux.AT_FDCWD), "/oldroot", ir.Int64(linux.AT_REMOVEDIR))),
		ir.If(ir.IsNegative(errVar), ir.Block{
			ir.Printf("cc: failed to remove oldroot: errno=0x%x\n", ir.Op(ir.OpSub, ir.Int64(0), errVar)),
			ir.Goto(errLabel),
		}),

		// Change to working directory
		ir.Syscall(defs.SYS_CHDIR, workDir),
	}

	// Configure network interface if networking is enabled
	if enableNetwork {
		// Configure eth0 with IP 10.0.2.15/24 (common QEMU default)
		// IP: 10.0.2.15 = 0x0a00020f
		// Mask: 255.255.255.0 = 0xffffff00
		main = append(main,
			initx.ConfigureInterface("eth0", 0x0a00020f, 0xffffff00, errLabel, errVar),

			// Add default route via 10.0.2.2 (gateway) on eth0
			initx.AddDefaultRoute("eth0", 0x0a000202, errLabel, errVar),

			// Set /etc/resolv.conf to use 10.0.2.2 as DNS server
			initx.SetResolvConf("10.0.2.2", errLabel, errVar),
		)
	}

	// Fork and exec using initx helper
	main = append(main,
		// Note: ForkExecWait expects argv to be the arguments AFTER the path,
		// as it puts path at argv[0] itself
		initx.ForkExecWait(cmd[0], cmd[1:], img.Config.Env, errLabel, errVar),

		// Return child exit code to host
		ir.Return(errVar),

		// Error handler
		ir.DeclareLabel(errLabel, ir.Block{
			ir.Printf(
				"cc: failed to add default route: errno=0x%x\n",
				errVar,
			),
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
	)

	return &ir.Program{
		Methods:    map[string]ir.Method{"main": main},
		Entrypoint: "main",
	}
}
