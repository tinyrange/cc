//go:build darwin && arm64

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/guestinit"
	"j5.nz/cc/internal/hv/hvf"
	"j5.nz/cc/internal/kernel/alpine"
	"j5.nz/cc/internal/oci"
	"j5.nz/cc/internal/simg"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "tinyboot:", err)
		os.Exit(1)
	}
}

func run() error {
	var cacheDir string
	var simgPath string
	var timeout time.Duration
	var memoryMB uint64
	var cpus int
	var dmesg bool
	var runCommand string
	var stdin string
	var runs int
	var warmup int
	var snapshotDir string
	var restoreSnapshot string

	flag.StringVar(&cacheDir, "cache-dir", "", "cache directory")
	flag.StringVar(&simgPath, "simg", "./fixtures/alpine.simg", "SIMG root filesystem")
	flag.DurationVar(&timeout, "timeout", 30*time.Second, "boot timeout")
	flag.Uint64Var(&memoryMB, "memory-mb", 512, "guest memory in MiB")
	flag.IntVar(&cpus, "cpus", 1, "guest CPUs")
	flag.BoolVar(&dmesg, "dmesg", false, "enable serial console and dmesg capture")
	flag.StringVar(&runCommand, "exec", "", "optional command to run after boot, split on spaces")
	flag.StringVar(&stdin, "stdin", "", "stdin for the optional command")
	flag.IntVar(&runs, "runs", 10, "number of boot runs to average")
	flag.IntVar(&warmup, "warmup", 1, "number of boot runs to exclude from averages")
	flag.StringVar(&snapshotDir, "snapshot-dir", "", "write checkpoint files under this directory when the guest reaches the snapshot trigger")
	flag.StringVar(&restoreSnapshot, "restore-snapshot", "", "restore from this snapshot directory instead of booting from kernel entry")
	flag.Parse()
	if flag.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(flag.Args(), " "))
	}
	if runs <= 0 {
		return fmt.Errorf("runs must be positive")
	}
	if warmup < 0 {
		return fmt.Errorf("warmup must be non-negative")
	}
	if cacheDir == "" {
		dir, err := os.MkdirTemp("", "cc-tinyboot-*")
		if err != nil {
			return fmt.Errorf("create cache dir: %w", err)
		}
		cacheDir = dir
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var kernel []byte
	var modules []alpine.Module
	var initBin []byte
	if restoreSnapshot == "" {
		kernelManager := alpine.NewManager(filepath.Join(cacheDir, "kernel"))
		if err := kernelManager.Ensure(ctx); err != nil {
			return fmt.Errorf("ensure kernel: %w", err)
		}
		var err error
		kernel, err = kernelManager.ReadKernel()
		if err != nil {
			return fmt.Errorf("read kernel: %w", err)
		}
		modules, err = kernelManager.PlanModuleLoad(alpineConfigVars(), alpineModuleMap())
		if err != nil {
			return fmt.Errorf("plan modules: %w", err)
		}
		initBin, err = guestinit.Build(ctx, filepath.Join(cacheDir, "guestinit"))
		if err != nil {
			return fmt.Errorf("build guest init: %w", err)
		}
	}
	rootFS, _, arch, err := simg.BuildImageFS(simgPath)
	if err != nil {
		return fmt.Errorf("open simg rootfs: %w", err)
	}
	image := &oci.Image{
		Name:         "tinyboot",
		Source:       simgPath,
		SourceKind:   oci.SourceKindSIMG,
		Architecture: arch,
		RootFS:       rootFS,
		Config: oci.RuntimeConfig{
			WorkingDir: "/",
		},
	}

	var totalBoot time.Duration
	var totalExec time.Duration
	var measuredBoots int
	var measuredExecs int
	totalRuns := warmup + runs
	for i := 1; i <= totalRuns; i++ {
		measured := i > warmup
		runCtx, runCancel := context.WithTimeout(context.Background(), timeout)
		start := time.Now()
		req := hvf.ContainerRunRequest{
			Kernel:      kernel,
			Init:        initBin,
			Modules:     modules,
			Image:       image,
			MemoryMB:    memoryMB,
			CPUs:        cpus,
			Dmesg:       dmesg,
			Persistent:  true,
			SnapshotDir: snapshotDir,
			UnixTime:    time.Now().Unix(),
		}
		onEvent := func(event client.BootEvent) error {
			if event.Kind == "status" && event.Message != "" {
				fmt.Fprintf(os.Stderr, "run %d boot: %s\n", i, event.Message)
			}
			return nil
		}
		var session *hvf.ContainerSession
		var err error
		if restoreSnapshot != "" {
			session, err = hvf.StartContainerFromSnapshot(runCtx, req, restoreSnapshot, onEvent)
		} else {
			session, err = hvf.StartContainerStream(runCtx, req, onEvent)
		}
		if err != nil {
			runCancel()
			return fmt.Errorf("boot VM run %d: %w", i, err)
		}
		bootDuration := time.Since(start)
		if measured {
			totalBoot += bootDuration
			measuredBoots++
		}
		fmt.Fprintf(os.Stderr, "run %d booted in %s%s\n", i, bootDuration.Round(time.Millisecond), warmupSuffix(measured))

		if strings.TrimSpace(runCommand) != "" {
			execStart := time.Now()
			resp, err := session.Exec(runCtx, client.ExecRequest{
				Command: strings.Fields(runCommand),
				Stdin:   []byte(stdin),
			})
			if err != nil {
				_ = session.Close()
				runCancel()
				return fmt.Errorf("exec command run %d: %w", i, err)
			}
			execDuration := time.Since(execStart)
			if measured {
				totalExec += execDuration
				measuredExecs++
			}
			fmt.Print(resp.Output)
			fmt.Fprintf(os.Stderr, "run %d exec exit=%d duration=%s%s\n", i, resp.ExitCode, execDuration.Round(time.Millisecond), warmupSuffix(measured))
		}
		if err := session.Close(); err != nil {
			runCancel()
			return fmt.Errorf("close VM run %d: %w", i, err)
		}
		runCancel()
	}
	fmt.Fprintf(os.Stderr, "average boot over %d measured runs", measuredBoots)
	if warmup > 0 {
		fmt.Fprintf(os.Stderr, " after %d warmup", warmup)
	}
	fmt.Fprintf(os.Stderr, ": %s\n", (totalBoot / time.Duration(measuredBoots)).Round(time.Millisecond))
	if measuredExecs > 0 {
		fmt.Fprintf(os.Stderr, "average exec over %d measured runs: %s\n", measuredExecs, (totalExec / time.Duration(measuredExecs)).Round(time.Millisecond))
	}
	return nil
}

func warmupSuffix(measured bool) string {
	if measured {
		return ""
	}
	return " (warmup)"
}

func alpineConfigVars() []string {
	return []string{
		"CONFIG_VIRTIO_MMIO",
		"CONFIG_FUSE_FS",
		"CONFIG_VIRTIO_FS",
		"CONFIG_VSOCKETS",
		"CONFIG_VIRTIO_VSOCKETS",
		"CONFIG_HW_RANDOM",
		"CONFIG_HW_RANDOM_VIRTIO",
	}
}

func alpineModuleMap() map[string]string {
	return map[string]string{
		"CONFIG_VIRTIO_MMIO":      "kernel/drivers/virtio/virtio_mmio.ko.gz",
		"CONFIG_FUSE_FS":          "kernel/fs/fuse/fuse.ko.gz",
		"CONFIG_VIRTIO_FS":        "kernel/fs/fuse/virtiofs.ko.gz",
		"CONFIG_VSOCKETS":         "kernel/net/vmw_vsock/vsock.ko.gz",
		"CONFIG_VIRTIO_VSOCKETS":  "kernel/net/vmw_vsock/vmw_vsock_virtio_transport.ko.gz",
		"CONFIG_HW_RANDOM":        "kernel/drivers/char/hw_random/rng-core.ko.gz",
		"CONFIG_HW_RANDOM_VIRTIO": "kernel/drivers/char/hw_random/virtio-rng.ko.gz",
	}
}
