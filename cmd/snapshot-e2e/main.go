package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"time"

	"github.com/tinyrange/cc/internal/debug"
	"github.com/tinyrange/cc/internal/devices/virtio"
	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/hv/factory"
	"github.com/tinyrange/cc/internal/initx"
	"github.com/tinyrange/cc/internal/linux/kernel"
	"github.com/tinyrange/cc/internal/oci"
	"github.com/tinyrange/cc/internal/timeslice"
	"github.com/tinyrange/cc/internal/vfs"
)

func main() {
	// Pin to main thread for darwin
	if runtime.GOOS == "darwin" {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
	}

	if err := run(); err != nil {
		var exitErr *initx.ExitError
		if errors.As(err, &exitErr) {
			// Ignore non-zero exit codes from the command itself
			if exitErr.Code != 0 {
				fmt.Fprintf(os.Stderr, "command exited with code %d\n", exitErr.Code)
			}
		} else {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	}
}

func run() error {
	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)

	repeat := fs.Int("repeat", 1, "Number of test iterations")
	cpuprofile := fs.String("cpuprofile", "", "Write CPU profile to file")
	memprofile := fs.String("memprofile", "", "Write memory profile to file")
	timesliceFile := fs.String("timeslice-file", "", "Write timeslice data to file")
	cpus := fs.Int("cpus", 1, "Number of CPUs")
	memory := fs.Int("memory", 1024, "Memory in MB")
	cacheDir := fs.String("cache-dir", "", "Cache directory for OCI images")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [flags]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Benchmark snapshot performance by running alpine whoami.\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(os.Args[1:]); err != nil {
		return err
	}

	// Setup CPU profiling
	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			return fmt.Errorf("create cpu profile: %w", err)
		}
		if err := pprof.StartCPUProfile(f); err != nil {
			f.Close()
			return fmt.Errorf("start cpu profile: %w", err)
		}
		defer func() {
			pprof.StopCPUProfile()
			f.Close()
		}()
	}

	// Setup timeslice recording
	if *timesliceFile != "" {
		f, err := os.Create(*timesliceFile)
		if err != nil {
			return fmt.Errorf("create timeslice file: %w", err)
		}
		w, err := timeslice.StartRecording(f)
		if err != nil {
			f.Close()
			return fmt.Errorf("open timeslice file: %w", err)
		}
		defer w.Close()
	}

	// Setup debug file from environment variable
	if debugFile := os.Getenv("CC_DEBUG_FILE"); debugFile != "" {
		if err := debug.OpenFile(debugFile); err != nil {
			return fmt.Errorf("open debug file: %w", err)
		}
		defer debug.Close()
		debug.Writef("snapshot-e2e", "debug file opened: %s", debugFile)
	}

	// Get snapshot cache directory
	snapshotDir, err := getSnapshotCacheDir(*cacheDir)
	if err != nil {
		return fmt.Errorf("get snapshot cache dir: %w", err)
	}

	snapshotCache := initx.NewSnapshotCache(snapshotDir, initx.GetSnapshotIO())
	if snapshotCache == nil {
		return fmt.Errorf("snapshot caching not available on this platform")
	}

	// Create OCI client
	client, err := oci.NewClient(*cacheDir)
	if err != nil {
		return fmt.Errorf("create OCI client: %w", err)
	}

	// Determine target architecture
	hvArch, err := parseArchitecture(runtime.GOARCH)
	if err != nil {
		return err
	}

	// Pull alpine image once
	fmt.Println("Pulling alpine image...")
	img, err := client.PullForArch("alpine", hvArch)
	if err != nil {
		return fmt.Errorf("pull image: %w", err)
	}

	for i := 0; i < *repeat; i++ {
		if *repeat > 1 {
			fmt.Printf("\n=== Iteration %d/%d ===\n", i+1, *repeat)
		}

		// Clear snapshot cache
		fmt.Println("Clearing snapshot cache...")
		if err := snapshotCache.InvalidateAll(); err != nil {
			return fmt.Errorf("clear snapshot cache: %w", err)
		}

		// Run 1: Cold boot (no snapshot)
		fmt.Println("Run 1 (cold boot)...")
		start := time.Now()
		if err := runVM(img, hvArch, *cpus, *memory, snapshotCache, false); err != nil {
			return fmt.Errorf("cold boot: %w", err)
		}
		coldDuration := time.Since(start)
		fmt.Printf("Run 1 (cold boot): %v\n", coldDuration)

		// Run 2: Warm boot (from snapshot)
		fmt.Println("Run 2 (snapshot)...")
		start = time.Now()
		if err := runVM(img, hvArch, *cpus, *memory, snapshotCache, true); err != nil {
			return fmt.Errorf("warm boot: %w", err)
		}
		warmDuration := time.Since(start)
		fmt.Printf("Run 2 (snapshot): %v\n", warmDuration)

		// Print speedup
		speedup := float64(coldDuration) / float64(warmDuration)
		fmt.Printf("Speedup: %.2fx\n", speedup)
	}

	// Memory profile
	if *memprofile != "" {
		f, err := os.Create(*memprofile)
		if err != nil {
			return fmt.Errorf("create memory profile: %w", err)
		}
		if err := pprof.WriteHeapProfile(f); err != nil {
			f.Close()
			return fmt.Errorf("write memory profile: %w", err)
		}
		f.Close()
	}

	return nil
}

func runVM(img *oci.Image, hvArch hv.CpuArchitecture, cpus, memory int, cache *initx.SnapshotCache, useSnapshot bool) error {
	// Create container filesystem
	containerFS, err := oci.NewContainerFS(img)
	if err != nil {
		return fmt.Errorf("create container filesystem: %w", err)
	}
	defer containerFS.Close()

	// Create VirtioFS backend
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
	}

	vm, err := initx.NewVirtualMachine(h, cpus, uint64(memory), kernelLoader, opts...)
	if err != nil {
		return fmt.Errorf("create VM: %w", err)
	}
	defer vm.Close()

	// Compute config hash
	configHash := hv.ComputeConfigHash(
		hvArch,
		uint64(memory)<<20,
		vm.HVVirtualMachine().MemoryBase(),
		cpus,
		nil,
	)

	// Snapshot handling
	var sessionCfg initx.SessionConfig
	if useSnapshot && cache.HasValidSnapshot(configHash, time.Time{}) {
		snap, err := cache.LoadSnapshot(configHash)
		if err == nil {
			if err := vm.RestoreSnapshot(snap); err == nil {
				sessionCfg.SkipBoot = true
			}
		}
	}

	if !sessionCfg.SkipBoot {
		sessionCfg.OnBootComplete = func() error {
			snap, err := vm.CaptureSnapshot()
			if err != nil {
				return nil
			}
			_ = cache.SaveSnapshot(configHash, snap)
			return nil
		}
	}

	// Resolve command path - whoami is at /usr/bin/whoami on alpine
	execCmd := []string{"/usr/bin/whoami"}
	workDir := "/"
	if img.Config.WorkingDir != "" {
		workDir = img.Config.WorkingDir
	}

	// Build and run the container init program
	prog, err := initx.BuildContainerInitProgram(initx.ContainerInitConfig{
		Arch:    hvArch,
		Cmd:     execCmd,
		Env:     img.Config.Env,
		WorkDir: workDir,
	})
	if err != nil {
		return err
	}

	// Run the VM with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	session := initx.StartSession(ctx, vm, prog, sessionCfg)
	err = session.Wait()
	if ctx.Err() != nil {
		return fmt.Errorf("VM execution timed out")
	}
	return err
}

func getSnapshotCacheDir(cacheDir string) (string, error) {
	if cacheDir == "" {
		cfg, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		cacheDir = filepath.Join(cfg, "cc")
	}
	return filepath.Join(cacheDir, "snapshots"), nil
}

func parseArchitecture(goarch string) (hv.CpuArchitecture, error) {
	switch goarch {
	case "amd64":
		return hv.ArchitectureX86_64, nil
	case "arm64":
		return hv.ArchitectureARM64, nil
	case "riscv64":
		return hv.ArchitectureRISCV64, nil
	default:
		return hv.ArchitectureInvalid, fmt.Errorf("unsupported architecture: %s", goarch)
	}
}
