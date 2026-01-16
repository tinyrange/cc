package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/schollz/progressbar/v3"
	"github.com/tinyrange/cc/internal/bundle"
	"github.com/tinyrange/cc/internal/devices/virtio"
	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/hv/factory"
	"github.com/tinyrange/cc/internal/initx"
	"github.com/tinyrange/cc/internal/ir"
	"github.com/tinyrange/cc/internal/linux/kernel"
	"github.com/tinyrange/cc/internal/oci"
	"github.com/tinyrange/cc/internal/timeslice"
	"github.com/tinyrange/cc/internal/vfs"
)

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

type benchmark struct {
	snapshot    hv.Snapshot
	commandArgs []string // The command to execute (path + args)
	commandEnv  []string // Environment variables for the command
}

var (
	tsLoadMetadata              = timeslice.RegisterKind("benchmark::load_metadata", 0)
	tsOciLoadFromDir            = timeslice.RegisterKind("benchmark::oci_load_from_dir", 0)
	tsNewContainerFS            = timeslice.RegisterKind("benchmark::new_container_fs", 0)
	tsNewVirtioFsBackend        = timeslice.RegisterKind("benchmark::new_virtiofs_backend", 0)
	tsFactoryOpen               = timeslice.RegisterKind("benchmark::factory_open", 0)
	tsKernelLoad                = timeslice.RegisterKind("benchmark::kernel_load", 0)
	tsNewVirtualMachine         = timeslice.RegisterKind("benchmark::new_virtual_machine", 0)
	tsBuildContainerInitProgram = timeslice.RegisterKind("benchmark::build_container_init_program", 0)
	tsStartSession              = timeslice.RegisterKind("benchmark::start_session", 0)
	tsWaitForSession            = timeslice.RegisterKind("benchmark::wait_for_session", 0)
	tsCleanup                   = timeslice.RegisterKind("benchmark::cleanup", 0)
	tsOnBootComplete            = timeslice.RegisterKind("benchmark::on_boot_complete", 0)
	tsCaptureSnapshot           = timeslice.RegisterKind("benchmark::capture_snapshot", 0)
	tsRestoreSnapshot           = timeslice.RegisterKind("benchmark::restore_snapshot", 0)
	tsOverallTime               = timeslice.RegisterKind("benchmark::overall", 0)
)

func (b *benchmark) runCommand(
	tsRecord *timeslice.Recorder,
	bundleDir string,
	bundleName string,
	command string,
) error {
	bundlePath := filepath.Join(bundleDir, bundleName)

	meta, err := bundle.LoadMetadata(bundlePath)
	if err != nil {
		return fmt.Errorf("load metadata: %w", err)
	}

	tsRecord.Record(tsLoadMetadata)

	// Determine image directory
	imageDir := filepath.Join(bundlePath, meta.Boot.ImageDir)
	if meta.Boot.ImageDir == "" {
		imageDir = filepath.Join(bundlePath, "image")
	}

	// Determine architecture early so we can validate the image
	hvArch, err := parseArchitecture(runtime.GOARCH)
	if err != nil {
		return fmt.Errorf("determine architecture: %w", err)
	}

	// Load OCI image with architecture validation
	img, err := oci.LoadFromDirForArch(imageDir, hvArch)
	if err != nil {
		return fmt.Errorf("load image: %w", err)
	}

	tsRecord.Record(tsOciLoadFromDir)

	containerFS, err := oci.NewContainerFS(img)
	if err != nil {
		return fmt.Errorf("create container filesystem: %w", err)
	}
	defer containerFS.Close()

	tsRecord.Record(tsNewContainerFS)

	// Parse command - first element is path, rest are args
	commandArgs := []string{command}

	fsBackend := vfs.NewVirtioFsBackendWithAbstract()
	if err := fsBackend.SetAbstractRoot(containerFS); err != nil {
		return fmt.Errorf("set container filesystem as root: %w", err)
	}

	tsRecord.Record(tsNewVirtioFsBackend)

	h, err := factory.OpenWithArchitecture(hvArch)
	if err != nil {
		return fmt.Errorf("create hypervisor: %w", err)
	}
	defer h.Close()

	tsRecord.Record(tsFactoryOpen)

	// Only load kernel when no snapshot exists (first boot)
	// When restoring from snapshot, the kernel is already in guest memory
	var kernelLoader kernel.Kernel
	if b.snapshot == nil {
		var err error
		kernelLoader, err = kernel.LoadForArchitecture(hvArch)
		if err != nil {
			return fmt.Errorf("load kernel: %w", err)
		}
		tsRecord.Record(tsKernelLoad)
	}

	buf := new(bytes.Buffer)

	opts := []initx.Option{
		initx.WithDeviceTemplate(virtio.FSTemplate{
			Tag:     "rootfs",
			Backend: fsBackend,
			Arch:    hvArch,
		}),
		initx.WithConsoleOutput(buf),
	}

	// If we have a snapshot, use WithSnapshot to skip kernel loading and restore automatically
	if b.snapshot != nil {
		opts = append(opts, initx.WithSnapshot(b.snapshot))
	}

	vm, err := initx.NewVirtualMachine(h, 1, 1024, kernelLoader, opts...)
	if err != nil {
		return fmt.Errorf("create VM: %w", err)
	}
	defer vm.Close()

	tsRecord.Record(tsNewVirtualMachine)

	if b.snapshot == nil {
		// First boot - use CommandLoop mode and capture snapshot after container setup
		// Build init program with CommandLoop=true
		prog, err := initx.BuildContainerInitProgram(initx.ContainerInitConfig{
			Arch:                  hvArch,
			CommandLoop:           true, // Enable command loop mode for late snapshot
			Env:                   img.Config.Env,
			WorkDir:               img.Config.WorkingDir,
			UID:                   img.Config.UID,
			GID:                   img.Config.GID,
			MailboxPhysAddr:       vm.MailboxPhysAddr(),
			TimesliceMMIOPhysAddr: vm.TimesliceMMIOPhysAddr(),
			ConfigRegionPhysAddr:  vm.ConfigRegionPhysAddr(),
		})
		if err != nil {
			return fmt.Errorf("build init program: %w", err)
		}

		tsRecord.Record(tsBuildContainerInitProgram)

		// Store command info for later use
		b.commandArgs = commandArgs
		b.commandEnv = img.Config.Env

		// First boot: Run the container init program directly (skip Boot() since we don't need it).
		// The container init will signal snapshot ready (0xdeadbeef) when setup is complete,
		// which causes vm.Run() to return (ErrYield is converted to nil when runResultDetail is cleared).
		// We then capture the snapshot and write the command.

		// Run container init until it signals snapshot ready
		if err := vm.Run(context.Background(), prog); err != nil {
			return fmt.Errorf("failed to run container init: %w", err)
		}

		tsRecord.Record(tsOnBootComplete)

		// Capture snapshot while guest is in command loop
		snap, err := vm.CaptureSnapshot()
		if err != nil {
			return fmt.Errorf("failed to capture snapshot: %w", err)
		}
		b.snapshot = snap

		tsRecord.Record(tsCaptureSnapshot)

		// Write the first command for guest to execute
		if err := vm.WriteExecCommand(commandArgs[0], commandArgs, b.commandEnv); err != nil {
			return fmt.Errorf("failed to write exec command: %w", err)
		}

		tsRecord.Record(tsStartSession)

		// Resume VM to execute the command
		// The guest is waiting in the command loop, will read the command and execute it
		if err := vm.Run(context.Background(), prog); err != nil {
			return fmt.Errorf("failed to run command: %w", err)
		}

		tsRecord.Record(tsWaitForSession)
	} else {
		// Subsequent runs - restore snapshot and execute command directly
		// The snapshot was restored when the VM was created (via WithSnapshot option)
		tsRecord.Record(tsRestoreSnapshot)

		// Write command to config region
		if err := vm.WriteExecCommand(b.commandArgs[0], b.commandArgs, b.commandEnv); err != nil {
			return fmt.Errorf("failed to write exec command: %w", err)
		}

		// We don't need to build a new program - the guest is already in the command loop
		// from the restored snapshot. We just need to resume execution.
		// However, vm.Run() requires a program to write to config region.
		// Use a minimal no-op program since the guest doesn't need it.
		prog := &ir.Program{
			Entrypoint: "main",
			Methods: map[string]ir.Method{
				"main": {ir.Return(ir.Int64(0))},
			},
		}

		tsRecord.Record(tsBuildContainerInitProgram)
		tsRecord.Record(tsStartSession)

		// Resume VM - guest will read command from config region and execute it
		if err := vm.Run(context.Background(), prog); err != nil {
			return fmt.Errorf("failed to run command: %w", err)
		}

		tsRecord.Record(tsWaitForSession)
	}

	return nil
}

func (b *benchmark) run() error {
	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)

	n := fs.Int("n", 100, "the number of VM runs to execute")
	bundleDir := fs.String("bundleDir", "", "the directory to load bundles from")
	bundleName := fs.String("bundle", "alpine", "the oci image name to run inside the virtual machine")
	testCommand := fs.String("cmd", "/usr/bin/whoami", "the command to execute inside the virtual machine")

	tsFile := fs.String("tsfile", "", "record a timeslice file for later analysis")

	if err := fs.Parse(os.Args[1:]); err != nil {
		return fmt.Errorf("failed to parse args: %w", err)
	}

	if *tsFile != "" {
		f, err := os.Create(*tsFile)
		if err != nil {
			return fmt.Errorf("failed to create tsfile: %w", err)
		}
		defer f.Close()

		closer, err := timeslice.StartRecording(f)
		if err != nil {
			return fmt.Errorf("failed to start recording timeslices: %w", err)
		}
		defer closer.Close()
	}

	if *bundleDir == "" {
		userDir, err := os.UserConfigDir()
		if err != nil {
			return fmt.Errorf("failed to get user config dir: %w", err)
		}

		*bundleDir = filepath.Join(userDir, "ccapp", "bundles")
	}

	start := time.Now()

	// execute a first boot to check and capture the snapshot.
	if err := b.runCommand(timeslice.NewState(), *bundleDir, *bundleName, *testCommand); err != nil {
		return fmt.Errorf("failed to perform first boot: %w", err)
	}

	timeslice.Record(tsOverallTime, time.Since(start))

	// enter the main loop
	pb := progressbar.Default(int64(*n))
	defer pb.Close()

	for range *n {
		start := time.Now()

		state := timeslice.NewState()

		if err := b.runCommand(state, *bundleDir, *bundleName, *testCommand); err != nil {
			return fmt.Errorf("failed to run command: %w", err)
		}

		state.Record(tsCleanup)

		timeslice.Record(tsOverallTime, time.Since(start))

		pb.Add(1)
	}

	return nil
}

func main() {
	b := benchmark{}

	if err := b.run(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to run benchmark: %v\n", err)
		os.Exit(1)
	}
}
