package vm

import (
	"context"
	"fmt"
	"runtime"

	"j5.nz/cc/internal/oci"
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
	if !NeedsAMD64Emulation(image) {
		return "", nil
	}
	if extractPackageFile == nil {
		return "", fmt.Errorf("package file extractor is nil")
	}
	qemu, err := extractPackageFile(ctx, "community", "qemu-x86_64", "usr/bin/qemu-x86_64")
	if err != nil {
		return "", fmt.Errorf("extract qemu-x86_64 package file: %w", err)
	}
	return qemu, nil
}
