//go:build linux

package vm

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"

	"j5.nz/cc/internal/fsimage"
	"j5.nz/cc/internal/imagefs"
	"j5.nz/cc/internal/oci"
	"j5.nz/cc/internal/virtio"
)

func linuxRuntimeImageFS(image *oci.Image) virtio.FSBackend {
	if image == nil {
		return nil
	}
	return virtio.NewImageFS(image.RootFS, image.RootFSDir)
}

func linuxRootFSImageEnabled() bool {
	return strings.TrimSpace(os.Getenv("CCX3_ROOTFS_IMAGE_TYPE")) != "" || strings.TrimSpace(os.Getenv("CCX3_ROOTFS_EXT4")) != ""
}

func linuxRootFSImageType() (fsimage.Type, error) {
	if value := strings.TrimSpace(os.Getenv("CCX3_ROOTFS_IMAGE_TYPE")); value != "" {
		return fsimage.ParseType(value)
	}
	if strings.TrimSpace(os.Getenv("CCX3_ROOTFS_EXT4")) != "" {
		return fsimage.TypeExt4, nil
	}
	return "", fmt.Errorf("rootfs image type is not configured")
}

func buildLinuxRootFSImage(ctx context.Context, root imagefs.Directory, typ fsimage.Type) ([]byte, error) {
	var buf bytes.Buffer
	if err := fsimage.Write(ctx, &buf, root, fsimage.Options{Type: typ}); err != nil {
		return nil, fmt.Errorf("build %s rootfs image: %w", typ, err)
	}
	return buf.Bytes(), nil
}

func linuxRootFSImageConfigVars(typ fsimage.Type) []string {
	switch typ {
	case fsimage.TypeExt4:
		return []string{"CONFIG_EXT4_FS"}
	case fsimage.TypeVFAT:
		return []string{"CONFIG_FAT_FS", "CONFIG_VFAT_FS", "CONFIG_NLS_CODEPAGE_437", "CONFIG_NLS_ISO8859_1"}
	default:
		return nil
	}
}
