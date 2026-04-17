package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"j5.nz/cc/internal/fulltest"
	"j5.nz/cc/internal/kernel/alpine"
	"j5.nz/cc/internal/macos"
	"j5.nz/cc/internal/oci"
	"j5.nz/cc/internal/vm"
)

func main() {
	if err := macos.EnsureExecutableIsSigned(); err != nil {
		panic(err)
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fulltest:", err)
		os.Exit(1)
	}
}

func run() error {
	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	cacheDir := fs.String("cache-dir", "", "Cache directory")
	workDir := fs.String("work-dir", "", "Working directory")
	recipe := fs.String("recipe", "local/neurocontainers/recipes/niimath/fulltest.yaml", "Path to fulltest YAML")
	imageSource := fs.String("image-source", "", "Image source (.simg path, /cvmfs path, or cvmfs:// URL)")
	imageName := fs.String("image-name", "", "Cached image name override")
	filter := fs.String("filter", "", "Substring filter for test names")
	keepVM := fs.Bool("keep-vm", false, "Keep VM running after test run")
	if err := fs.Parse(os.Args[1:]); err != nil {
		return err
	}

	rootCache, err := resolveCacheDir(*cacheDir)
	if err != nil {
		return err
	}
	kernel := alpine.NewManager(filepath.Join(rootCache, "kernel"))
	images := oci.NewStore(filepath.Join(rootCache, "images"))
	vms := vm.NewManagerWithBackend(vm.NewRuntimeBackend(kernel, images, filepath.Join(rootCache, "guestinit")))

	runner := &fulltest.Runner{
		Kernel: kernel,
		Images: images,
		VMs:    vms,
	}
	res, err := runner.Run(context.Background(), *recipe, fulltest.Options{
		ImageSource: *imageSource,
		ImageName:   *imageName,
		WorkDir:     *workDir,
		KeepVM:      *keepVM,
		Filter:      *filter,
	})
	if err != nil {
		return err
	}
	passed := 0
	failed := 0
	skipped := 0
	for _, test := range res.Results {
		switch {
		case test.Passed:
			passed++
			fmt.Printf("PASS %s (%s)\n", test.Name, test.Duration.Round(10_000_000))
		case test.Skipped:
			skipped++
			fmt.Printf("SKIP %s: %s\n", test.Name, test.Message)
		default:
			failed++
			fmt.Printf("FAIL %s: %s\n", test.Name, test.Message)
		}
	}
	fmt.Printf("\nSuite: %s\nWork dir: %s\nPassed: %d\nFailed: %d\nSkipped: %d\n", res.Suite, res.WorkDir, passed, failed, skipped)
	if failed > 0 {
		return fmt.Errorf("%d tests failed", failed)
	}
	return nil
}

func resolveCacheDir(arg string) (string, error) {
	if arg != "" {
		return arg, os.MkdirAll(arg, 0o755)
	}
	userCacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolve user cache dir: %w", err)
	}
	dir := filepath.Join(userCacheDir, "ccx3")
	return dir, os.MkdirAll(dir, 0o755)
}
