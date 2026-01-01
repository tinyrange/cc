package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strconv"
	"strings"
	"time"

	"github.com/tinyrange/cc/internal/bundle"
	"github.com/tinyrange/cc/internal/debug"
	"github.com/tinyrange/cc/internal/devices/virtio"
	"github.com/tinyrange/cc/internal/gowin/window"
	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/hv/factory"
	"github.com/tinyrange/cc/internal/initx"
	"github.com/tinyrange/cc/internal/ir"
	"github.com/tinyrange/cc/internal/linux/defs"
	linux "github.com/tinyrange/cc/internal/linux/defs/amd64"
	"github.com/tinyrange/cc/internal/linux/kernel"
	"github.com/tinyrange/cc/internal/netstack"
	"github.com/tinyrange/cc/internal/oci"
	termwin "github.com/tinyrange/cc/internal/term"
	"github.com/tinyrange/cc/internal/vfs"
	"golang.org/x/term"
)

func main() {
	// Cocoa requires UI objects (e.g. NSWindow) to be created on the process main
	// thread. The Go scheduler can migrate the main goroutine across OS threads,
	// so we pin it early on darwin to keep later window creation safe.
	if runtime.GOOS == "darwin" {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
	}

	if err := run(); err != nil {
		var exitErr *initx.ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.Code)
		}
		fmt.Fprintf(os.Stderr, "cc: %v\n", err)
		os.Exit(1)
	}
}

type fixCrlf struct {
	w io.Writer
}

func (f *fixCrlf) Write(p []byte) (n int, err error) {
	return f.w.Write(bytes.ReplaceAll(p, []byte{'\n'}, []byte{'\r', '\n'}))
}

type intFlag struct {
	v   int
	set bool
}

func (f *intFlag) String() string { return strconv.Itoa(f.v) }

func (f *intFlag) Set(s string) error {
	v, err := strconv.Atoi(s)
	if err != nil {
		return err
	}
	f.v = v
	f.set = true
	return nil
}

type uint64Flag struct {
	v   uint64
	set bool
}

func (f *uint64Flag) String() string { return strconv.FormatUint(f.v, 10) }

func (f *uint64Flag) Set(s string) error {
	v, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return err
	}
	f.v = v
	f.set = true
	return nil
}

type boolFlag struct {
	v   bool
	set bool
}

func (f *boolFlag) String() string {
	if f.v {
		return "true"
	}
	return "false"
}

func (f *boolFlag) Set(s string) error {
	v, err := strconv.ParseBool(s)
	if err != nil {
		return err
	}
	f.v = v
	f.set = true
	return nil
}

func (f *boolFlag) IsBoolFlag() bool { return true }

func run() error {
	cacheDir := flag.String("cache-dir", "", "Cache directory (default: ~/.config/cc/)")
	buildOut := flag.String("build", "", "Build a prebaked bundle folder at this path, then exit")
	var cpusFlag intFlag
	cpusFlag.v = 1
	flag.Var(&cpusFlag, "cpus", "Number of vCPUs")
	var memoryFlag uint64Flag
	memoryFlag.v = 1024
	flag.Var(&memoryFlag, "memory", "Memory in MB")
	dbg := flag.Bool("debug", false, "Enable debug logging")
	debugFile := flag.String("debug-file", "", "Write debug stream to file")
	cpuprofile := flag.String("cpuprofile", "", "Write CPU profile to file")
	memprofile := flag.String("memprofile", "", "Write memory profile to file")
	var dmesgFlag boolFlag
	flag.Var(&dmesgFlag, "dmesg", "Print kernel dmesg during boot and runtime")
	var networkFlag boolFlag
	flag.Var(&networkFlag, "network", "Enable networking")
	timeout := flag.Duration("timeout", 0, "Timeout for the container")
	packetdump := flag.String("packetdump", "", "Write packet capture (pcap) to file (requires -network)")
	var execFlag boolFlag
	flag.Var(&execFlag, "exec", "Execute the entrypoint as PID 1 taking over init")
	gpu := flag.Bool("gpu", false, "Enable GPU and create a window")
	termWin := flag.Bool("term", false, "Open a terminal window and connect it to the VM console")
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

	if *debugFile != "" {
		if err := debug.OpenFile(*debugFile); err != nil {
			return fmt.Errorf("open debug file: %w", err)
		}
		defer debug.Close()

		debug.Writef("cc debug logging enabled", "filename=%s", *debugFile)
	}

	if *dbg {
		slog.SetDefault(slog.New(slog.NewTextHandler(
			&fixCrlf{w: os.Stderr},
			&slog.HandlerOptions{Level: slog.LevelDebug},
		)))
	} else {
		slog.SetDefault(slog.New(slog.NewTextHandler(
			&fixCrlf{w: os.Stderr},
			&slog.HandlerOptions{Level: slog.LevelInfo},
		)))
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

	if *packetdump != "" && !networkFlag.v {
		return fmt.Errorf("-packetdump requires -network")
	}
	if *termWin && *gpu {
		return fmt.Errorf("-term and -gpu are mutually exclusive")
	}
	if *termWin && *dbg {
		return fmt.Errorf("-term and -debug are mutually exclusive")
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

	if *buildOut != "" {
		img, err := client.PullForArch(imageRef, hvArch)
		if err != nil {
			return fmt.Errorf("pull image: %w", err)
		}

		imageDir := filepath.Join(*buildOut, bundle.DefaultImageDir)
		if err := oci.ExportToDir(img, imageDir); err != nil {
			return fmt.Errorf("export prebaked image: %w", err)
		}

		meta := bundle.Metadata{
			Version:     1,
			Name:        "{{name}}",
			Description: "{{description}}",
			Boot: bundle.BootConfig{
				ImageDir: bundle.DefaultImageDir,
				Command:  img.Command(cmd),
				CPUs:     cpusFlag.v,
				MemoryMB: memoryFlag.v,
				Network:  networkFlag.v,
				Exec:     execFlag.v,
				Dmesg:    dmesgFlag.v,
			},
		}
		if err := bundle.WriteTemplate(*buildOut, meta); err != nil {
			return fmt.Errorf("write bundle metadata: %w", err)
		}

		slog.Info("bundle built", "dir", *buildOut)
		return nil
	}

	slog.Debug("Loading image", "ref", imageRef, "arch", hvArch)
	debug.Writef("cc.run load image", "loading image %s for architecture %s", imageRef, hvArch)

	var meta bundle.Metadata
	var img *oci.Image

	switch {
	case bundle.IsBundleDir(imageRef):
		m, loaded, err := bundle.Load(imageRef)
		if err != nil {
			return err
		}
		meta, img = m, loaded

		// Apply bundle boot defaults iff the user did not override via flags.
		if !cpusFlag.set && meta.Boot.CPUs != 0 {
			cpusFlag.v = meta.Boot.CPUs
		}
		if !memoryFlag.set && meta.Boot.MemoryMB != 0 {
			memoryFlag.v = meta.Boot.MemoryMB
		}
		if !networkFlag.set {
			networkFlag.v = meta.Boot.Network
		}
		if !execFlag.set {
			execFlag.v = meta.Boot.Exec
		}
		if !dmesgFlag.set {
			dmesgFlag.v = meta.Boot.Dmesg
		}
	case func() bool {
		_, err := os.Stat(filepath.Join(imageRef, "config.json"))
		return err == nil
	}():
		loaded, err := oci.LoadFromDir(imageRef)
		if err != nil {
			return fmt.Errorf("load prebaked image: %w", err)
		}
		img = loaded
	default:
		loaded, err := client.PullForArch(imageRef, hvArch)
		if err != nil {
			return fmt.Errorf("pull image: %w", err)
		}
		img = loaded
	}

	slog.Debug("Image pulled", "layers", len(img.Layers))
	debug.Writef("cc.run image pulled", "image pulled with %d layers", len(img.Layers))

	// Determine command to run
	var execCmd []string
	if len(cmd) > 0 {
		execCmd = img.Command(cmd)
	} else if meta.Version != 0 && len(meta.Boot.Command) > 0 {
		execCmd = meta.Boot.Command
	} else {
		execCmd = img.Command(nil)
	}
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

	debug.Writef("cc.run container filesystem created", "container filesystem created")

	execCmd, err = resolveCommandPath(containerFS, execCmd, pathEnv, workDir)
	if err != nil {
		return fmt.Errorf("resolve command: %w", err)
	}

	slog.Debug("Running command", "cmd", execCmd)
	debug.Writef("cc.run running command", "running command %v", execCmd)

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

	debug.Writef("cc.run hypervisor created", "hypervisor created")

	// Load kernel
	kernelLoader, err := kernel.LoadForArchitecture(hvArch)
	if err != nil {
		return fmt.Errorf("load kernel: %w", err)
	}

	debug.Writef("cc.run kernel loaded", "kernel loaded for architecture %s", hvArch)

	// Create VM with VirtioFS
	opts := []initx.Option{
		initx.WithDeviceTemplate(virtio.FSTemplate{
			Tag:     "rootfs",
			Backend: fsBackend,
			Arch:    hvArch,
		}),
		initx.WithDebugLogging(*dbg),
		initx.WithDmesgLogging(dmesgFlag.v),
	}

	var termWindow *termwin.Terminal
	if *termWin {
		scale := window.GetDisplayScale()
		physWidth := int(float32(1024) * scale)
		physHeight := int(float32(768) * scale)

		tw, err := termwin.New("cc", physWidth, physHeight)
		if err != nil {
			return fmt.Errorf("create terminal window: %w", err)
		}
		termWindow = tw
		defer termWindow.Close()

		opts = append(opts,
			initx.WithStdin(termWindow),
			initx.WithConsoleOutput(termWindow),
		)
	} else {
		// Wrap stdin with a filter to strip CPR responses on Windows.
		opts = append(opts, initx.WithStdin(wrapStdinForVT(os.Stdin)))
	}

	// Add network device if enabled
	if networkFlag.v {
		backend := netstack.New(slog.Default())
		var packetDumpFile *os.File
		defer func() {
			_ = backend.Close()
			if packetDumpFile != nil {
				_ = packetDumpFile.Close()
			}
		}()

		if *packetdump != "" {
			dir := filepath.Dir(*packetdump)
			if dir != "." {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return fmt.Errorf("create packet dump directory: %w", err)
				}
			}
			f, err := os.Create(*packetdump)
			if err != nil {
				return fmt.Errorf("create packet dump file: %w", err)
			}
			packetDumpFile = f
			if err := backend.OpenPacketCapture(packetDumpFile); err != nil {
				return fmt.Errorf("enable packet capture: %w", err)
			}
		}

		if err := backend.StartDNSServer(); err != nil {
			return fmt.Errorf("start DNS server: %w", err)
		}

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

		debug.Writef("cc.run networking enabled", "networking enabled")
	}

	if *gpu {
		opts = append(opts, initx.WithGPUEnabled(true))
	}

	vm, err := initx.NewVirtualMachine(
		h,
		cpusFlag.v,
		memoryFlag.v,
		kernelLoader,
		opts...,
	)
	if err != nil {
		return fmt.Errorf("create VM: %w", err)
	}
	defer vm.Close()

	// Build and run the container init program
	prog := buildContainerInit(hvArch, img, execCmd, networkFlag.v, execFlag.v)

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

		debug.Writef("cc.run booting VM", "booting VM")

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

		debug.Writef("cc.run VM booted successfully", "VM booted successfully")

		return nil
	}(); err != nil {
		return err
	}

	// Start stdin forwarding now that VM is booted and user command is about to run.
	// This ensures stdin data goes to the user command, not the init process.
	vm.StartStdinForwarding()

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

	debug.Writef("cc.run running command", "running command %v", execCmd)

	// If GPU is enabled, set up the display manager and run VM in background
	if *gpu && vm.GPU() != nil {
		// Get display scale factor and calculate physical window dimensions
		scale := window.GetDisplayScale()
		physWidth := int(float32(1024) * scale)
		physHeight := int(float32(768) * scale)

		// Create window for display with scaled dimensions
		win, err := window.New("cc", physWidth, physHeight, true)
		if err != nil {
			return fmt.Errorf("failed to create window: %w", err)
		} else {
			defer win.Close()

			// Create display manager and connect to GPU/Input devices
			displayMgr := virtio.NewDisplayManager(vm.GPU(), vm.Keyboard(), vm.Tablet())
			displayMgr.SetWindow(win)

			// Run VM in a goroutine
			vmDone := make(chan error, 1)
			go func() {
				vmDone <- vm.Run(ctx, prog)
			}()

			// Run display loop on main thread
			ticker := time.NewTicker(16 * time.Millisecond) // ~60 FPS
			defer ticker.Stop()

		displayLoop:
			for {
				select {
				case err := <-vmDone:
					if err != nil {
						return fmt.Errorf("run executable in initx virtual machine: %w", err)
					}
					break displayLoop
				case <-ctx.Done():
					return fmt.Errorf("context cancelled: %w", ctx.Err())
				case <-ticker.C:
					// Poll window events
					if !displayMgr.Poll() {
						// Window was closed
						return fmt.Errorf("window closed by user")
					}
					// Render and swap
					displayMgr.Render()
					displayMgr.Swap()
				}
			}

			slog.Info("cc: command exited")
			return nil
		}
	} else if *termWin && termWindow != nil {
		// If terminal window is enabled, run VM in background and drive the window loop
		// on the main thread.

		termCtx, cancel := context.WithCancel(ctx)
		defer cancel()

		vmDone := make(chan error, 1)
		go func() {
			vmDone <- vm.Run(termCtx, prog)
		}()

		var lastResize struct {
			cols int
			rows int
		}

		err := termWindow.Run(termCtx, termwin.Hooks{
			OnResize: func(cols, rows int) {
				if cols == lastResize.cols && rows == lastResize.rows {
					return
				}
				lastResize.cols, lastResize.rows = cols, rows
				vm.SetConsoleSize(cols, rows)
			},
			OnFrame: func() error {
				select {
				case err := <-vmDone:
					if err != nil {
						var exitErr *initx.ExitError
						if errors.As(err, &exitErr) {
							return exitErr
						}
						return fmt.Errorf("run VM: %w", err)
					}
					return io.EOF // stop loop cleanly
				default:
					return nil
				}
			},
		})
		if err != nil {
			if errors.Is(err, termwin.ErrWindowClosed) {
				// Best-effort: stop the VM when the user closes the window.
				cancel()
				select {
				case <-vmDone:
				case <-time.After(2 * time.Second):
				}
				return fmt.Errorf("window closed by user")
			}
			if errors.Is(err, io.EOF) {
				// VM finished successfully and asked us to stop the loop.
				slog.Info("cc: command exited")
				return nil
			}
			var exitErr *initx.ExitError
			if errors.As(err, &exitErr) {
				return exitErr
			}
			return err
		}

		// If the loop returned nil, treat it as window closed.
		return fmt.Errorf("window closed by user")
	} else {
		if term.IsTerminal(int(os.Stdin.Fd())) {
			// On Windows, enable VT processing on stdout so ANSI sequences are interpreted.
			restoreVT, err := enableVTProcessing()
			if err != nil {
				return fmt.Errorf("enable VT processing: %w", err)
			}
			defer restoreVT()

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

		debug.Writef("cc.run command exited", "command exited")

		return nil
	}
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

func ipToUint32(addr string) uint32 {
	ip := net.ParseIP(addr)
	if ip == nil {
		return 0
	}
	return binary.BigEndian.Uint32(ip.To4())
}

func buildContainerInit(arch hv.CpuArchitecture, img *oci.Image, cmd []string, enableNetwork bool, exec bool) *ir.Program {
	errLabel := ir.Label("__cc_error")
	errVar := ir.Var("__cc_errno")
	pivotResult := ir.Var("__cc_pivot_result")

	workDir := containerWorkDir(img)

	main := ir.Method{
		initx.LogKmsg("cc: running container init program\n"),

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

		// Mount /dev/shm (wlroots/xkbcommon use it for shm-backed buffers like keymaps).
		ir.Syscall(defs.SYS_MKDIRAT, ir.Int64(linux.AT_FDCWD), "/mnt/dev/shm", ir.Int64(0o1777)),
		ir.Syscall(
			defs.SYS_MOUNT,
			"tmpfs",
			"/mnt/dev/shm",
			"tmpfs",
			ir.Int64(0),
			"mode=1777",
		),

		initx.LogKmsg("cc: mounted filesystems\n"),

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

		initx.LogKmsg("cc: changed root to container\n"),

		// Always cleanup oldroot
		ir.Assign(errVar, ir.Syscall(defs.SYS_UNLINKAT, ir.Int64(linux.AT_FDCWD), "/oldroot", ir.Int64(linux.AT_REMOVEDIR))),
		ir.If(ir.IsNegative(errVar), ir.Block{
			ir.Printf("cc: failed to remove oldroot: errno=0x%x\n", ir.Op(ir.OpSub, ir.Int64(0), errVar)),
			ir.Goto(errLabel),
		}),

		// Change to working directory
		ir.Syscall(defs.SYS_CHDIR, workDir),

		// mkdir /dev/pts
		ir.Syscall(defs.SYS_MKDIRAT, ir.Int64(linux.AT_FDCWD), "/dev/pts", ir.Int64(0o755)),
		ir.If(ir.IsNegative(errVar), ir.Block{
			ir.Printf("cc: failed to create /dev/pts: errno=0x%x\n", ir.Op(ir.OpSub, ir.Int64(0), errVar)),
			ir.Goto(errLabel),
		}),

		// Mount devpts
		ir.Syscall(
			defs.SYS_MOUNT,
			"devpts",
			"/dev/pts",
			"devpts",
			ir.Int64(0),
			"",
		),
		ir.If(ir.IsNegative(errVar), ir.Block{
			ir.Printf("cc: failed to mount devpts: errno=0x%x\n", ir.Op(ir.OpSub, ir.Int64(0), errVar)),
			ir.Goto(errLabel),
		}),

		initx.LogKmsg("cc: mounted devpts\n"),

		// Set hostname to container name
		initx.SetHostname("tinyrange", errLabel, errVar),

		initx.LogKmsg("cc: set hostname to container name\n"),
	}

	// Configure network interface if networking is enabled
	if enableNetwork {
		// Configure eth0 with IP 10.42.0.2/24
		// IP: 10.42.0.2
		ip := ipToUint32("10.42.0.2")
		// Gateway: 10.42.0.1
		gatewayIp := "10.42.0.1"
		gateway := ipToUint32(gatewayIp)
		// Mask: 255.255.255.0
		mask := ipToUint32("255.255.255.0")
		main = append(main,
			initx.ConfigureInterface("eth0", ip, mask, errLabel, errVar),

			// Add default route via gateway on eth0
			initx.AddDefaultRoute("eth0", gateway, errLabel, errVar),

			// Set /etc/resolv.conf to use DNS server
			initx.SetResolvConf(gatewayIp, errLabel, errVar),

			initx.LogKmsg("cc: configured network interface\n"),
		)
	}

	if exec {
		main = append(main, ir.Block{
			initx.LogKmsg(fmt.Sprintf("cc: executing command %s\n", cmd[0])),
			initx.Exec(cmd[0], cmd[1:], img.Config.Env, errLabel, errVar),
		})
	} else {
		main = append(main,
			initx.ForkExecWait(cmd[0], cmd[1:], img.Config.Env, errLabel, errVar),
		)
	}

	// Fork and exec using initx helper
	main = append(main,
		// Return child exit code to host
		ir.Return(errVar),

		// Error handler
		ir.DeclareLabel(errLabel, ir.Block{
			ir.Printf(
				"cc: fatal error during boot: errno=0x%x\n",
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
