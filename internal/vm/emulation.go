package vm

import (
	"context"
	"fmt"
	"runtime"
	"time"

	"j5.nz/cc/internal/oci"
	"j5.nz/cc/internal/timing"
)

type packageFileExtractor func(ctx context.Context, repo, packageName, innerPath string) (string, error)

func NeedsAMD64Emulation(image *oci.Image) bool {
	_ = image
	if runtime.GOARCH != "arm64" {
		return false
	}
	return true
}

func PrepareAMD64Emulator(ctx context.Context, image *oci.Image, extractPackageFile packageFileExtractor) (string, error) {
	start := time.Now()
	if !NeedsAMD64Emulation(image) {
		timing.Since(ctx, "backend.prepare_amd64_emulator.needs_check", start)
		return "", nil
	}
	timing.Since(ctx, "backend.prepare_amd64_emulator.needs_check", start)
	if extractPackageFile == nil {
		return "", fmt.Errorf("package file extractor is nil")
	}
	start = time.Now()
	qemu, err := extractPackageFile(ctx, "community", "qemu-x86_64", "usr/bin/qemu-x86_64")
	timing.Since(ctx, "backend.prepare_amd64_emulator.extract_package_file", start)
	if err != nil {
		return "", fmt.Errorf("extract qemu-x86_64 package file: %w", err)
	}
	return qemu, nil
}
