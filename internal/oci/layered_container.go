package oci

import (
	"fmt"
	"io/fs"
	"os"
	"strings"
	"time"

	"github.com/tinyrange/cc/internal/archive"
	"github.com/tinyrange/cc/internal/vfs"
)

// LayeredContainerFS wraps a ContainerFS and adds additional layers on top.
// It implements vfs.AbstractDir to provide a unified view of all layers.
type LayeredContainerFS struct {
	base   *ContainerFS
	layers []*snapshotLayerReader
}

// snapshotLayerReader reads from a filesystem snapshot layer.
type snapshotLayerReader struct {
	entries  map[string]archive.Entry
	contents *os.File
}

// NewLayeredContainerFS creates a new layered container filesystem.
func NewLayeredContainerFS(base *ContainerFS, layers []ImageLayer) (*LayeredContainerFS, error) {
	lcfs := &LayeredContainerFS{
		base:   base,
		layers: make([]*snapshotLayerReader, len(layers)),
	}

	for i, layer := range layers {
		lr, err := openSnapshotLayer(layer)
		if err != nil {
			lcfs.Close()
			return nil, fmt.Errorf("open snapshot layer %s: %w", layer.Hash, err)
		}
		lcfs.layers[i] = lr
	}

	return lcfs, nil
}

func openSnapshotLayer(layer ImageLayer) (*snapshotLayerReader, error) {
	idxFile, err := os.Open(layer.IndexPath)
	if err != nil {
		return nil, fmt.Errorf("open index %s: %w", layer.IndexPath, err)
	}
	defer idxFile.Close()

	entries, err := archive.ReadAllEntries(idxFile)
	if err != nil {
		return nil, fmt.Errorf("read entries: %w", err)
	}

	contentsFile, err := os.Open(layer.ContentsPath)
	if err != nil {
		return nil, fmt.Errorf("open contents %s: %w", layer.ContentsPath, err)
	}

	lr := &snapshotLayerReader{
		entries:  make(map[string]archive.Entry),
		contents: contentsFile,
	}

	for _, ent := range entries {
		// Normalize path - remove leading slash
		name := strings.TrimPrefix(ent.Name, "/")
		name = strings.TrimSuffix(name, "/")
		if name == "" {
			name = "."
		}
		lr.entries[name] = ent
	}

	return lr, nil
}

// Close releases resources held by the layered container filesystem.
func (lcfs *LayeredContainerFS) Close() error {
	for _, lr := range lcfs.layers {
		if lr != nil && lr.contents != nil {
			lr.contents.Close()
		}
	}
	// Don't close base - it's owned by the caller
	return nil
}

// Base returns the underlying ContainerFS.
func (lcfs *LayeredContainerFS) Base() *ContainerFS {
	return lcfs.base
}

// Stat implements vfs.AbstractDir.
func (lcfs *LayeredContainerFS) Stat() fs.FileMode {
	return 0o755
}

// ModTime implements vfs.AbstractDir.
func (lcfs *LayeredContainerFS) ModTime() time.Time {
	return lcfs.base.ModTime()
}

// ReadDir implements vfs.AbstractDir.
func (lcfs *LayeredContainerFS) ReadDir() ([]vfs.AbstractDirEntry, error) {
	return lcfs.readDirPath("")
}

func (lcfs *LayeredContainerFS) readDirPath(dirPath string) ([]vfs.AbstractDirEntry, error) {
	seen := make(map[string]bool)
	deleted := make(map[string]bool)
	var result []vfs.AbstractDirEntry

	// First check snapshot layers (top to bottom)
	for i := len(lcfs.layers) - 1; i >= 0; i-- {
		lr := lcfs.layers[i]
		for name, ent := range lr.entries {
			// Check if this entry is in the target directory
			var entryName string
			if dirPath == "" {
				// Root directory
				if !strings.Contains(name, "/") && name != "." {
					entryName = name
				} else if idx := strings.Index(name, "/"); idx > 0 {
					entryName = name[:idx]
				}
			} else {
				// Subdirectory
				prefix := dirPath + "/"
				if strings.HasPrefix(name, prefix) {
					rest := strings.TrimPrefix(name, prefix)
					if !strings.Contains(rest, "/") {
						entryName = rest
					} else if idx := strings.Index(rest, "/"); idx > 0 {
						entryName = rest[:idx]
					}
				}
			}

			if entryName == "" || entryName == "." {
				continue
			}

			fullPath := entryName
			if dirPath != "" {
				fullPath = dirPath + "/" + entryName
			}

			if deleted[fullPath] {
				continue
			}

			if ent.Kind == archive.EntryKindDeleted {
				deleted[fullPath] = true
				continue
			}

			if seen[entryName] {
				continue
			}
			seen[entryName] = true

			result = append(result, vfs.AbstractDirEntry{
				Name:  entryName,
				IsDir: ent.Kind == archive.EntryKindDirectory,
				Mode:  ent.Mode,
				Size:  uint64(ent.Size),
			})
		}
	}

	// Then get entries from base that weren't overridden or deleted
	baseEntries, err := lcfs.base.readDirPath(dirPath)
	if err != nil {
		return result, nil // Ignore base errors, return what we have
	}

	for _, entry := range baseEntries {
		fullPath := entry.Name
		if dirPath != "" {
			fullPath = dirPath + "/" + entry.Name
		}

		if seen[entry.Name] || deleted[fullPath] {
			continue
		}
		seen[entry.Name] = true
		result = append(result, entry)
	}

	return result, nil
}

// Lookup implements vfs.AbstractDir.
func (lcfs *LayeredContainerFS) Lookup(name string) (vfs.AbstractEntry, error) {
	return lcfs.lookupPath(name)
}

func (lcfs *LayeredContainerFS) lookupPath(filePath string) (vfs.AbstractEntry, error) {
	filePath = strings.TrimPrefix(filePath, "/")
	if filePath == "" {
		filePath = "."
	}

	// Check snapshot layers first (top to bottom)
	for i := len(lcfs.layers) - 1; i >= 0; i-- {
		lr := lcfs.layers[i]

		// Exact match
		if ent, ok := lr.entries[filePath]; ok {
			if ent.Kind == archive.EntryKindDeleted {
				return vfs.AbstractEntry{}, fmt.Errorf("file not found: %s", filePath)
			}
			return lcfs.entryToAbstract(filePath, ent, lr)
		}

		// Check for directory (entries starting with this path)
		dirPrefix := filePath + "/"
		for name := range lr.entries {
			if strings.HasPrefix(name, dirPrefix) {
				return vfs.AbstractEntry{
					Dir: &layeredContainerDir{
						lcfs: lcfs,
						path: filePath,
					},
				}, nil
			}
		}
	}

	// Fall back to base
	return lcfs.base.lookupPath(filePath)
}

func (lcfs *LayeredContainerFS) entryToAbstract(filePath string, ent archive.Entry, lr *snapshotLayerReader) (vfs.AbstractEntry, error) {
	switch ent.Kind {
	case archive.EntryKindDirectory:
		return vfs.AbstractEntry{
			Dir: &layeredContainerDir{
				lcfs:    lcfs,
				path:    filePath,
				modTime: ent.ModTime,
				uid:     uint32(ent.UID),
				gid:     uint32(ent.GID),
			},
		}, nil
	case archive.EntryKindRegular:
		return vfs.AbstractEntry{
			File: &snapshotFile{
				entry:    ent,
				contents: lr.contents,
			},
		}, nil
	case archive.EntryKindSymlink:
		return vfs.AbstractEntry{
			Symlink: &snapshotSymlink{
				target:  ent.Linkname,
				mode:    ent.Mode,
				modTime: ent.ModTime,
				uid:     uint32(ent.UID),
				gid:     uint32(ent.GID),
			},
		}, nil
	default:
		return vfs.AbstractEntry{}, fmt.Errorf("unsupported entry kind: %v", ent.Kind)
	}
}

// layeredContainerDir implements vfs.AbstractDir for a subdirectory in LayeredContainerFS.
type layeredContainerDir struct {
	lcfs    *LayeredContainerFS
	path    string
	modTime time.Time
	uid     uint32
	gid     uint32
}

func (d *layeredContainerDir) Stat() fs.FileMode { return 0o755 }
func (d *layeredContainerDir) Owner() (uint32, uint32) {
	return d.uid, d.gid
}
func (d *layeredContainerDir) ModTime() time.Time {
	if !d.modTime.IsZero() {
		return d.modTime
	}
	return time.Unix(0, 0)
}
func (d *layeredContainerDir) ReadDir() ([]vfs.AbstractDirEntry, error) {
	return d.lcfs.readDirPath(d.path)
}
func (d *layeredContainerDir) Lookup(name string) (vfs.AbstractEntry, error) {
	fullPath := d.path + "/" + name
	return d.lcfs.lookupPath(fullPath)
}

// snapshotFile implements vfs.AbstractFile for a file in a snapshot layer.
type snapshotFile struct {
	entry    archive.Entry
	contents *os.File
}

func (f *snapshotFile) Stat() (uint64, fs.FileMode) {
	return uint64(f.entry.Size), f.entry.Mode
}

func (f *snapshotFile) Owner() (uint32, uint32) {
	return uint32(f.entry.UID), uint32(f.entry.GID)
}

func (f *snapshotFile) ModTime() time.Time {
	return f.entry.ModTime
}

func (f *snapshotFile) ReadAt(off uint64, size uint32) ([]byte, error) {
	fileSize := uint64(f.entry.Size)
	if off >= fileSize {
		return []byte{}, nil
	}
	remaining := fileSize - off
	if uint64(size) > remaining {
		size = uint32(remaining)
	}
	if size == 0 {
		return []byte{}, nil
	}

	r, err := f.entry.Open(f.contents)
	if err != nil {
		return nil, fmt.Errorf("open entry: %w", err)
	}

	buf := make([]byte, size)
	n, err := r.ReadAt(buf, int64(off))
	if err != nil && n == 0 {
		return nil, fmt.Errorf("read at offset %d: %w", off, err)
	}
	return buf[:n], nil
}

func (f *snapshotFile) WriteAt(off uint64, data []byte) error {
	return fmt.Errorf("snapshot filesystem is read-only")
}

func (f *snapshotFile) Truncate(size uint64) error {
	return fmt.Errorf("snapshot filesystem is read-only")
}

// snapshotSymlink implements vfs.AbstractSymlink for a symlink in a snapshot layer.
type snapshotSymlink struct {
	target  string
	mode    fs.FileMode
	modTime time.Time
	uid     uint32
	gid     uint32
}

func (s *snapshotSymlink) Stat() fs.FileMode       { return s.mode }
func (s *snapshotSymlink) ModTime() time.Time      { return s.modTime }
func (s *snapshotSymlink) Target() string          { return s.target }
func (s *snapshotSymlink) Owner() (uint32, uint32) { return s.uid, s.gid }

var (
	_ vfs.AbstractDir     = (*LayeredContainerFS)(nil)
	_ vfs.AbstractDir     = (*layeredContainerDir)(nil)
	_ vfs.AbstractOwner   = (*layeredContainerDir)(nil)
	_ vfs.AbstractFile    = (*snapshotFile)(nil)
	_ vfs.AbstractOwner   = (*snapshotFile)(nil)
	_ vfs.AbstractSymlink = (*snapshotSymlink)(nil)
	_ vfs.AbstractOwner   = (*snapshotSymlink)(nil)
)
