// Package fsimage exposes portable in-memory filesystem construction.
// It does not mount images, run host tools, or require host paths.
package fsimage

import (
	"context"
	"io"
	"time"

	"j5.nz/cc/internal/fsimage/ffs"
	"j5.nz/cc/internal/imagefs"
)

type Directory = imagefs.Directory
type File = imagefs.File
type Entry = imagefs.Entry
type DirEnt = imagefs.DirEnt

type Region interface {
	io.ReaderAt
	io.WriterAt
	Size() int64
}

type FFSOptions struct {
	// SizeBytes is filesystem capacity, excluding the OpenBSD disk prefix.
	// Zero selects automatically estimated capacity.
	SizeBytes, ExtraBytes int64
	Timestamp             time.Time
	// Raw omits the MBR and OpenBSD disklabel (FreeBSD and NetBSD roots).
	Raw bool
}

// BuildFFS returns a sparse UFS1 image, borrowing immutable file payloads.
// Keep all sources alive until the returned region is no longer used.
func BuildFFS(ctx context.Context, root Directory, options FFSOptions) (Region, error) {
	layout := ffs.LayoutOpenBSDDisk
	if options.Raw {
		layout = ffs.LayoutRaw
	}
	return ffs.Build(ctx, root, ffs.Options{SizeBytes: options.SizeBytes, ExtraBytes: options.ExtraBytes, DeterministicTime: options.Timestamp, Layout: layout})
}
