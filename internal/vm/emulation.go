package vm

import (
	"context"
	"fmt"
	"runtime"

	"j5.nz/cc/internal/oci"
)

type packageFileReader func(ctx context.Context, repo, packageName, innerPath string) ([]byte, error)

func needsAMD64Emulation(image *oci.Image) bool {
	if image == nil {
		return false
	}
	if runtime.GOARCH != "arm64" {
		return false
	}
	switch image.Architecture {
	case "amd64", "x86_64":
		return true
	default:
		return false
	}
}

func loadAMD64Emulator(ctx context.Context, image *oci.Image, readPackageFile packageFileReader) ([]byte, error) {
	if !needsAMD64Emulation(image) {
		return nil, nil
	}
	if readPackageFile == nil {
		return nil, fmt.Errorf("package file reader is nil")
	}
	qemu, err := readPackageFile(ctx, "community", "qemu-x86_64", "usr/bin/qemu-x86_64")
	if err != nil {
		return nil, fmt.Errorf("read qemu-x86_64 package file: %w", err)
	}
	return qemu, nil
}
