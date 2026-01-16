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
	snapshot hv.Snapshot
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
		return fmt.Errorf("")
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

	var sessionCfg initx.SessionConfig
	if b.snapshot == nil {
		// First boot - capture snapshot after boot completes
		sessionCfg.OnBootComplete = func() error {
			tsRecord.Record(tsOnBootComplete)

			snap, err := vm.CaptureSnapshot()
			if err != nil {
				return fmt.Errorf("failed to capture snapshot: %w", err)
			}

			b.snapshot = snap

			tsRecord.Record(tsCaptureSnapshot)

			return nil
		}
	} else {
		// Snapshot was restored in NewVirtualMachine via WithSnapshot option
		sessionCfg.SkipBoot = true

		tsRecord.Record(tsRestoreSnapshot)
	}

	// Build init program
	prog, err := initx.BuildContainerInitProgram(initx.ContainerInitConfig{
		Arch:    hvArch,
		Cmd:     commandArgs,
		Env:     img.Config.Env,
		WorkDir: img.Config.WorkingDir,
		Exec:    meta.Boot.Exec,
		UID:     img.Config.UID,
		GID:     img.Config.GID,
	})
	if err != nil {
		return fmt.Errorf("build init program: %w", err)
	}

	tsRecord.Record(tsBuildContainerInitProgram)

	session := initx.StartSession(context.Background(), vm, prog, sessionCfg)

	tsRecord.Record(tsStartSession)

	if err := session.Wait(); err != nil {
		return fmt.Errorf("failed to wait for session: %w", err)
	}

	tsRecord.Record(tsWaitForSession)

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
