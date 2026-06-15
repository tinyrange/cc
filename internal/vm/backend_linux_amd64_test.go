//go:build linux && amd64

package vm

import (
	"testing"

	"j5.nz/cc/internal/fsimage"
)

func TestLinuxRootFSImageTypeEnv(t *testing.T) {
	t.Setenv("CCX3_ROOTFS_IMAGE_TYPE", "ext4")
	typ, err := linuxRootFSImageType()
	if err != nil {
		t.Fatal(err)
	}
	if typ != fsimage.TypeExt4 {
		t.Fatalf("rootfs image type = %q, want %q", typ, fsimage.TypeExt4)
	}
}

func TestLinuxRootFSImageTypeLegacyExt4Env(t *testing.T) {
	t.Setenv("CCX3_ROOTFS_EXT4", "1")
	typ, err := linuxRootFSImageType()
	if err != nil {
		t.Fatal(err)
	}
	if typ != fsimage.TypeExt4 {
		t.Fatalf("rootfs image type = %q, want %q", typ, fsimage.TypeExt4)
	}
}

func TestLinuxRootFSImageTypeAcceptsPlannedType(t *testing.T) {
	t.Setenv("CCX3_ROOTFS_IMAGE_TYPE", "vfat")
	typ, err := linuxRootFSImageType()
	if err != nil {
		t.Fatal(err)
	}
	if typ != fsimage.TypeVFAT {
		t.Fatalf("rootfs image type = %q, want %q", typ, fsimage.TypeVFAT)
	}
}

func TestLinuxRootFSImageTypeRejectsUnknown(t *testing.T) {
	t.Setenv("CCX3_ROOTFS_IMAGE_TYPE", "definitely-not-a-filesystem")
	if _, err := linuxRootFSImageType(); err == nil {
		t.Fatal("linuxRootFSImageType accepted an unknown type")
	}
}

func TestLinuxRootFSImageConfigVars(t *testing.T) {
	got := linuxRootFSImageConfigVars(fsimage.TypeExt4)
	if len(got) != 1 || got[0] != "CONFIG_EXT4_FS" {
		t.Fatalf("ext4 config vars = %#v, want CONFIG_EXT4_FS", got)
	}
}
