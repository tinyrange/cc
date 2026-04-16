package vm

import (
	"context"
	"fmt"
	"runtime"

	"j5.nz/cc/internal/imagefs"
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

func prepareImageForAMD64Emulation(ctx context.Context, image *oci.Image, readPackageFile packageFileReader) (*oci.Image, error) {
	if !needsAMD64Emulation(image) {
		return image, nil
	}
	if readPackageFile == nil {
		return nil, fmt.Errorf("package file reader is nil")
	}
	qemu, err := readPackageFile(ctx, "community", "qemu-x86_64", "usr/bin/qemu-x86_64")
	if err != nil {
		return nil, fmt.Errorf("read qemu-x86_64 package file: %w", err)
	}
	overlay := imagefs.NewOverlay(image.RootFS)
	if err := overlay.AddFile("/usr/bin/qemu-x86_64-static", 0o755, qemu); err != nil {
		return nil, fmt.Errorf("overlay qemu-x86_64-static: %w", err)
	}
	cloned := *image
	cloned.RootFS = overlay.Root()
	return &cloned, nil
}
