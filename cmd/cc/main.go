package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"runtime"

	"github.com/tinyrange/cc/internal/devices/virtio"
	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/hv/factory"
	"github.com/tinyrange/cc/internal/initx"
	"github.com/tinyrange/cc/internal/ir"
	"github.com/tinyrange/cc/internal/linux/defs"
	linux "github.com/tinyrange/cc/internal/linux/defs/amd64"
	"github.com/tinyrange/cc/internal/linux/kernel"
	"github.com/tinyrange/cc/internal/oci"
	"github.com/tinyrange/cc/internal/vfs"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "cc: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	arch := flag.String("arch", runtime.GOARCH, "Target architecture (amd64, arm64)")
	cacheDir := flag.String("cache-dir", "", "Cache directory (default: ~/.config/cc/)")
	cpus := flag.Int("cpus", 1, "Number of vCPUs")
	memory := flag.Uint64("memory", 256, "Memory in MB")
	debug := flag.Bool("debug", false, "Enable debug logging")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [flags] <image> [command] [args...]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Run a command inside an OCI container image in a virtual machine.\n\n")
		fmt.Fprintf(os.Stderr, "Examples:\n")
		fmt.Fprintf(os.Stderr, "  %s alpine:latest /bin/sh -c 'echo hello'\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s ubuntu:22.04 ls -la\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s --arch arm64 alpine:latest uname -m\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Flags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

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
	hvArch, err := parseArchitecture(*arch)
	if err != nil {
		return err
	}

	// Create OCI client
	client, err := oci.NewClient(*cacheDir)
	if err != nil {
		return fmt.Errorf("create OCI client: %w", err)
	}

	slog.Info("Pulling image", "ref", imageRef, "arch", *arch)

	// Pull image
	img, err := client.PullForArch(imageRef, *arch)
	if err != nil {
		return fmt.Errorf("pull image: %w", err)
	}

	slog.Info("Image pulled", "layers", len(img.Layers))

	// Determine command to run
	execCmd := img.Command(cmd)
	if len(execCmd) == 0 {
		return fmt.Errorf("no command specified and image has no entrypoint/cmd")
	}

	slog.Info("Running command", "cmd", execCmd)

	// Create container filesystem
	containerFS, err := oci.NewContainerFS(img)
	if err != nil {
		return fmt.Errorf("create container filesystem: %w", err)
	}
	defer containerFS.Close()

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
	vm, err := initx.NewVirtualMachine(
		h,
		*cpus,
		*memory,
		kernelLoader,
		initx.WithDeviceTemplate(virtio.FSTemplate{
			Tag:     "rootfs",
			Backend: fsBackend,
			Arch:    hvArch,
		}),
		initx.WithDebugLogging(*debug),
	)
	if err != nil {
		return fmt.Errorf("create VM: %w", err)
	}
	defer vm.Close()

	// Build and run the container init program
	ctx := context.Background()
	prog := buildContainerInit(img, execCmd)

	slog.Info("Starting VM")

	if err := vm.Run(ctx, prog); err != nil {
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

func buildContainerInit(img *oci.Image, cmd []string) *ir.Program {
	errLabel := ir.Label("__cc_error")
	errVar := ir.Var("__cc_errno")

	workDir := img.Config.WorkingDir
	if workDir == "" {
		workDir = "/"
	}

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
		ir.Assign(errVar, ir.Syscall(defs.SYS_PIVOT_ROOT, ".", "oldroot")),
		ir.If(ir.IsNegative(errVar), ir.Block{
			// Fall back to chroot if pivot_root fails
			ir.Assign(errVar, ir.Syscall(defs.SYS_CHROOT, ".")),
			ir.If(ir.IsNegative(errVar), ir.Block{
				ir.Printf("cc: failed to chroot: errno=0x%x\n", ir.Op(ir.OpSub, ir.Int64(0), errVar)),
				ir.Goto(errLabel),
			}),
		}),

		// Change to working directory
		ir.Syscall(defs.SYS_CHDIR, workDir),

		// Fork and exec using initx helper
		// Note: ForkExecWait expects argv to be the arguments AFTER the path,
		// as it puts path at argv[0] itself
		initx.ForkExecWait(cmd[0], cmd[1:], img.Config.Env, errLabel, errVar),

		// Return success
		ir.Return(ir.Int64(0)),

		// Error handler
		ir.DeclareLabel(errLabel, ir.Block{
			ir.Syscall(defs.SYS_REBOOT,
				linux.LINUX_REBOOT_MAGIC1,
				linux.LINUX_REBOOT_MAGIC2,
				linux.LINUX_REBOOT_CMD_RESTART,
				ir.Int64(0),
			),
		}),
	}

	return &ir.Program{
		Methods:    map[string]ir.Method{"main": main},
		Entrypoint: "main",
	}
}
