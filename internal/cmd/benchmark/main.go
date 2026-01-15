package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/schollz/progressbar/v3"
	"github.com/tinyrange/cc/internal/bundle"
	"github.com/tinyrange/cc/internal/devices/virtio"
	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/hv/factory"
	"github.com/tinyrange/cc/internal/initx"
	"github.com/tinyrange/cc/internal/linux/kernel"
	"github.com/tinyrange/cc/internal/oci"
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
}

func (b *benchmark) runCommand(bundleDir string, bundleName string, command string) error {
	bundlePath := filepath.Join(bundleDir, bundleName)

	meta, err := bundle.LoadMetadata(bundlePath)
	if err != nil {
		return fmt.Errorf("")
	}

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

	containerFS, err := oci.NewContainerFS(img)
	if err != nil {
		return fmt.Errorf("create container filesystem: %w", err)
	}
	defer containerFS.Close()

	commandArgs := []string{"/bin/sh", "-c", command}

	fsBackend := vfs.NewVirtioFsBackendWithAbstract()
	if err := fsBackend.SetAbstractRoot(containerFS); err != nil {
		return fmt.Errorf("set container filesystem as root: %w", err)
	}

	h, err := factory.OpenWithArchitecture(hvArch)
	if err != nil {
		return fmt.Errorf("create hypervisor: %w", err)
	}
	defer h.Close()

	kernelLoader, err := kernel.LoadForArchitecture(hvArch)
	if err != nil {
		return fmt.Errorf("load kernel: %w", err)
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

	vm, err := initx.NewVirtualMachine(h, 1, 1024, kernelLoader, opts...)
	if err != nil {
		return fmt.Errorf("create VM: %w", err)
	}
	defer vm.Close()

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

	session := initx.StartSession(context.Background(), vm, prog, initx.SessionConfig{})

	return session.Wait()
}

func (b *benchmark) run() error {
	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)

	n := fs.Int("n", 10, "the number of VM runs to execute")
	bundleDir := fs.String("bundleDir", "", "the directory to load bundles from")
	bundleName := fs.String("bundle", "alpine", "the oci image name to run inside the virtual machine")
	testCommand := fs.String("cmd", "whoami", "the command to execute inside the virtual machine")

	if err := fs.Parse(os.Args[1:]); err != nil {
		return fmt.Errorf("failed to parse args: %w", err)
	}

	if *bundleDir == "" {
		userDir, err := os.UserConfigDir()
		if err != nil {
			return fmt.Errorf("failed to get user config dir: %w", err)
		}

		*bundleDir = filepath.Join(userDir, "ccapp", "bundles")
	}

	pb := progressbar.Default(int64(*n))
	defer pb.Close()

	for range *n {
		if err := b.runCommand(*bundleDir, *bundleName, *testCommand); err != nil {
			return fmt.Errorf("failed to run command: %w", err)
		}

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
