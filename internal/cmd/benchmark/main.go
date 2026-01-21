package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strings"
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
	// Persistent state for VM reuse
	snapshot      hv.Snapshot
	vm            *initx.VirtualMachine
	h             hv.Hypervisor
	containerFS   *oci.ContainerFS
	fsBackend     vfs.VirtioFsBackend
	commandArgs   []string // The command to execute (path + args)
	commandEnv    []string // Environment variables for the command
	consoleBuffer *bytes.Buffer // Shared buffer for console output
}

// Close releases resources held by the benchmark, including snapshot files.
func (b *benchmark) Close() error {
	if b.vm != nil {
		b.vm.Close()
		b.vm = nil
	}
	if b.h != nil {
		b.h.Close()
		b.h = nil
	}
	if b.containerFS != nil {
		b.containerFS.Close()
		b.containerFS = nil
	}
	if b.snapshot != nil {
		b.snapshot.Close()
		b.snapshot = nil
	}
	// fsBackend doesn't need explicit close - it's managed by the VM
	return nil
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
	tsWriteCommand              = timeslice.RegisterKind("benchmark::write_command", 0)
)

// firstBoot initializes the VM, boots it, and captures a snapshot.
// After this call, the VM is ready for repeated command execution.
func (b *benchmark) firstBoot(
	consoleOutput io.Writer,
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

	b.containerFS, err = oci.NewContainerFS(img)
	if err != nil {
		return fmt.Errorf("create container filesystem: %w", err)
	}

	tsRecord.Record(tsNewContainerFS)

	// Parse command - first element is path, rest are args
	commandArgs := []string{command}

	b.fsBackend = vfs.NewVirtioFsBackendWithAbstract()
	if err := b.fsBackend.SetAbstractRoot(b.containerFS); err != nil {
		return fmt.Errorf("set container filesystem as root: %w", err)
	}

	tsRecord.Record(tsNewVirtioFsBackend)

	b.h, err = factory.OpenWithArchitecture(hvArch)
	if err != nil {
		return fmt.Errorf("create hypervisor: %w", err)
	}

	tsRecord.Record(tsFactoryOpen)

	// Load kernel for first boot
	kernelLoader, err := kernel.LoadForArchitecture(hvArch)
	if err != nil {
		return fmt.Errorf("load kernel: %w", err)
	}
	tsRecord.Record(tsKernelLoad)

	// Initialize shared console buffer for output validation
	b.consoleBuffer = new(bytes.Buffer)

	opts := []initx.Option{
		initx.WithDeviceTemplate(virtio.FSTemplate{
			Tag:     "rootfs",
			Backend: b.fsBackend,
			Arch:    hvArch,
		}),
		initx.WithConsoleOutput(b.consoleBuffer),
	}

	b.vm, err = initx.NewVirtualMachine(b.h, 1, 1024, kernelLoader, opts...)
	if err != nil {
		return fmt.Errorf("create VM: %w", err)
	}

	tsRecord.Record(tsNewVirtualMachine)

	// First boot - use CommandLoop mode and capture snapshot after container setup
	prog, err := initx.BuildContainerInitProgram(initx.ContainerInitConfig{
		Arch:                  hvArch,
		CommandLoop:           true, // Enable command loop mode for late snapshot
		Env:                   img.Config.Env,
		WorkDir:               img.Config.WorkingDir,
		UID:                   img.Config.UID,
		GID:                   img.Config.GID,
		MailboxPhysAddr:       b.vm.MailboxPhysAddr(),
		TimesliceMMIOPhysAddr: b.vm.TimesliceMMIOPhysAddr(),
		ConfigRegionPhysAddr:  b.vm.ConfigRegionPhysAddr(),
	})
	if err != nil {
		return fmt.Errorf("build init program: %w", err)
	}

	tsRecord.Record(tsBuildContainerInitProgram)

	// Store command info for later use
	b.commandArgs = commandArgs
	b.commandEnv = img.Config.Env

	// First boot: Run the container init program directly
	timeoutCtx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	if err := b.vm.Run(timeoutCtx, prog); err != nil {
		return fmt.Errorf("failed to run container init: %w", err)
	}

	tsRecord.Record(tsOnBootComplete)

	// Capture snapshot while guest is in command loop
	b.snapshot, err = b.vm.CaptureSnapshot()
	if err != nil {
		return fmt.Errorf("failed to capture snapshot: %w", err)
	}

	tsRecord.Record(tsCaptureSnapshot)

	// Write the first command for guest to execute - whoami with no args
	if err := b.vm.WriteExecCommand(commandArgs[0], nil, nil); err != nil {
		return fmt.Errorf("failed to write exec command: %w", err)
	}

	tsRecord.Record(tsStartSession)

	// Resume VM to execute the command
	timeoutCtx2, cancel2 := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel2()

	if err := b.vm.Run(timeoutCtx2, prog); err != nil {
		return fmt.Errorf("failed to run command: %w", err)
	}

	tsRecord.Record(tsWaitForSession)

	return nil
}

// runIteration restores the snapshot and runs a command with the given iteration number.
// It validates that the output matches the expected result.
// NOTE: On ARM64 Linux, commands with arguments fail because the config region is
// MMIO-only (no backing memory), and execve's copy_from_user doesn't work with MMIO.
// For now, we use whoami (no args) which works correctly.
// This reuses the existing VM instead of creating a new one.
func (b *benchmark) runIteration(
	tsRecord *timeslice.Recorder,
	iteration int,
) error {
	// Reset console buffer before running
	b.consoleBuffer.Reset()

	// Restore snapshot on the existing VM
	if err := b.vm.RestoreSnapshot(b.snapshot); err != nil {
		return fmt.Errorf("failed to restore snapshot: %w", err)
	}

	tsRecord.Record(tsRestoreSnapshot)

	// Use whoami with no arguments - commands with args don't work on ARM64 Linux
	// due to MMIO config region (execve can't copy strings from MMIO memory)
	expectedOutput := "root"

	if err := b.vm.WriteExecCommand("/usr/bin/whoami", nil, nil); err != nil {
		return fmt.Errorf("failed to write exec command: %w", err)
	}

	// Minimal no-op program since guest is already in command loop
	prog := &ir.Program{
		Entrypoint: "main",
		Methods: map[string]ir.Method{
			"main": {ir.Return(ir.Int64(0))},
		},
	}

	tsRecord.Record(tsBuildContainerInitProgram)
	tsRecord.Record(tsStartSession)

	// Resume VM - guest will read command from config region and execute it
	if err := b.vm.Run(context.Background(), prog); err != nil {
		return fmt.Errorf("failed to run command: %w", err)
	}

	tsRecord.Record(tsWaitForSession)

	// Validate output contains expected string
	output := b.consoleBuffer.String()
	if !strings.Contains(output, expectedOutput) {
		return fmt.Errorf("iteration %d: expected output to contain %q, got %q", iteration, expectedOutput, output)
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
	memprofile := fs.String("memprofile", "", "write memory profile to file")

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

	if *memprofile != "" {
		defer func() {
			f, err := os.Create(*memprofile)
			if err != nil {
				fmt.Fprintf(os.Stderr, "failed to create memprofile: %v\n", err)
				return
			}
			defer f.Close()
			if err := pprof.Lookup("heap").WriteTo(f, 0); err != nil {
				fmt.Fprintf(os.Stderr, "failed to write memory profile: %v\n", err)
				return
			}
		}()
	}

	if *bundleDir == "" {
		userDir, err := os.UserConfigDir()
		if err != nil {
			return fmt.Errorf("failed to get user config dir: %w", err)
		}

		*bundleDir = filepath.Join(userDir, "ccapp", "bundles")
	}

	// First boot: create VM, boot, capture snapshot
	totalStart := time.Now()
	buf := new(bytes.Buffer)

	if err := b.firstBoot(buf, timeslice.NewState(), *bundleDir, *bundleName, *testCommand); err != nil {
		fmt.Fprintf(os.Stderr, "failed to perform first boot: %v\n%s\n", err, buf.String())
		return fmt.Errorf("failed to perform first boot: %w", err)
	}

	timeslice.Record(tsOverallTime, time.Since(totalStart))

	// Main loop: reuse VM, just restore snapshot and run with different commands
	pb := progressbar.Default(int64(*n))
	defer pb.Close()

	totalStart = time.Now()

	for i := range *n {
		start := time.Now()
		state := timeslice.NewState()

		if err := b.runIteration(state, i); err != nil {
			fmt.Fprintf(os.Stderr, "failed to run command: %v\nconsole output: %s\n", err, b.consoleBuffer.String())
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
		b.Close()
		os.Exit(1)
	}

	b.Close()
}
