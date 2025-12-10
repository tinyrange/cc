package oci

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
	"time"

	"github.com/tinyrange/cc/internal/archive"
	"github.com/tinyrange/cc/internal/vfs"
)

// ContainerFS implements vfs.AbstractDir to provide a layered filesystem view
// of an OCI container image.
type ContainerFS struct {
	image  *Image
	layers []*layerReader
}

type layerReader struct {
	entries  map[string]archive.Entry
	contents *os.File
}

// NewContainerFS creates a new container filesystem from an OCI image.
func NewContainerFS(img *Image) (*ContainerFS, error) {
	cfs := &ContainerFS{
		image:  img,
		layers: make([]*layerReader, len(img.Layers)),
	}

	// Load layers in reverse order (bottom to top)
	for i, layer := range img.Layers {
		lr, err := openLayer(layer)
		if err != nil {
			cfs.Close()
			return nil, fmt.Errorf("open layer %s: %w", layer.Hash, err)
		}
		cfs.layers[i] = lr
	}

	return cfs, nil
}

func openLayer(layer ImageLayer) (*layerReader, error) {
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

	lr := &layerReader{
		entries:  make(map[string]archive.Entry),
		contents: contentsFile,
	}

	for _, ent := range entries {
		// Normalize path
		name := strings.TrimPrefix(ent.Name, "./")
		name = strings.TrimPrefix(name, "/")
		if name == "" {
			name = "."
		}
		lr.entries[name] = ent
	}

	return lr, nil
}

// Close releases resources held by the container filesystem.
func (cfs *ContainerFS) Close() error {
	for _, lr := range cfs.layers {
		if lr != nil && lr.contents != nil {
			lr.contents.Close()
		}
	}
	return nil
}

func normalizePath(filePath string) string {
	filePath = strings.TrimPrefix(filePath, "/")
	filePath = strings.TrimPrefix(filePath, "./")
	if filePath == "." {
		return ""
	}
	return filePath
}

func (cfs *ContainerFS) entryForPath(filePath string) (archive.Entry, bool) {
	normalized := normalizePath(filePath)
	if normalized == "" {
		normalized = "."
	}
	for i := len(cfs.layers) - 1; i >= 0; i-- {
		lr := cfs.layers[i]
		ent, ok := lr.entries[normalized]
		if !ok {
			continue
		}
		if ent.Kind == archive.EntryKindDeleted {
			return archive.Entry{}, false
		}
		return ent, true
	}
	return archive.Entry{}, false
}

func (cfs *ContainerFS) entryModTime(filePath string) time.Time {
	if ent, ok := cfs.entryForPath(filePath); ok {
		return ent.ModTime
	}
	return time.Time{}
}

// Stat implements vfs.AbstractDir.
func (cfs *ContainerFS) Stat() fs.FileMode {
	return 0o755
}

// ModTime implements vfs.AbstractDir.
func (cfs *ContainerFS) ModTime() time.Time {
	return cfs.entryModTime(".")
}

// ReadDir implements vfs.AbstractDir.
func (cfs *ContainerFS) ReadDir() ([]vfs.AbstractDirEntry, error) {
	return cfs.readDirPath("")
}

func (cfs *ContainerFS) readDirPath(dirPath string) ([]vfs.AbstractDirEntry, error) {
	dirPath = normalizePath(dirPath)

	seen := make(map[string]bool)
	deleted := make(map[string]bool)
	var result []vfs.AbstractDirEntry

	// Iterate layers from top to bottom (later layers override earlier)
	for i := len(cfs.layers) - 1; i >= 0; i-- {
		lr := cfs.layers[i]
		for name, ent := range lr.entries {
			// Check if this entry is in the target directory
			var entryName string
			if dirPath == "" {
				// Root directory - only include top-level entries
				if !strings.Contains(name, "/") && name != "." {
					entryName = name
				} else if idx := strings.Index(name, "/"); idx > 0 {
					// Directory entry
					entryName = name[:idx]
				}
			} else {
				// Subdirectory - check if entry is direct child
				if strings.HasPrefix(name, dirPath+"/") {
					rest := strings.TrimPrefix(name, dirPath+"/")
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

			// Check if deleted by upper layer
			if deleted[fullPath] {
				continue
			}

			// Mark as deleted if this is a whiteout
			if ent.Kind == archive.EntryKindDeleted {
				deleted[fullPath] = true
				continue
			}

			// Skip if already seen (overridden by upper layer)
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

	return result, nil
}

// Lookup implements vfs.AbstractDir.
func (cfs *ContainerFS) Lookup(name string) (vfs.AbstractEntry, error) {
	return cfs.lookupPath(name)
}

func (cfs *ContainerFS) lookupPath(filePath string) (vfs.AbstractEntry, error) {
	filePath = normalizePath(filePath)

	// Search layers from top to bottom
	for i := len(cfs.layers) - 1; i >= 0; i-- {
		lr := cfs.layers[i]

		// Check for exact match
		if ent, ok := lr.entries[filePath]; ok {
			if ent.Kind == archive.EntryKindDeleted {
				// Deleted by this layer
				return vfs.AbstractEntry{}, fmt.Errorf("file not found: %s", filePath)
			}
			return cfs.entryToAbstract(filePath, ent, lr)
		}

		// Check for directory (entries starting with this path)
		dirPrefix := filePath + "/"
		for name := range lr.entries {
			if strings.HasPrefix(name, dirPrefix) {
				// This is a directory
				return vfs.AbstractEntry{
					Dir: &containerDir{
						cfs:  cfs,
						path: filePath,
					},
				}, nil
			}
		}
	}

	return vfs.AbstractEntry{}, fmt.Errorf("file not found: %s", filePath)
}

func (cfs *ContainerFS) entryToAbstract(filePath string, ent archive.Entry, lr *layerReader) (vfs.AbstractEntry, error) {
	switch ent.Kind {
	case archive.EntryKindDirectory:
		return vfs.AbstractEntry{
			Dir: &containerDir{
				cfs:     cfs,
				path:    filePath,
				modTime: ent.ModTime,
			},
		}, nil
	case archive.EntryKindRegular:
		return vfs.AbstractEntry{
			File: &containerFile{
				entry:    ent,
				contents: lr.contents,
			},
		}, nil
	case archive.EntryKindSymlink, archive.EntryKindHardlink:
		// For symlinks and hardlinks, resolve to the target
		target := ent.Linkname
		if !path.IsAbs(target) {
			target = path.Join(path.Dir(filePath), target)
		}
		return cfs.lookupPath(target)
	default:
		return vfs.AbstractEntry{}, fmt.Errorf("unsupported entry kind: %v", ent.Kind)
	}
}

// containerDir implements vfs.AbstractDir for a subdirectory.
type containerDir struct {
	cfs  *ContainerFS
	path string

	modTime time.Time
}

func (d *containerDir) Stat() fs.FileMode {
	return 0o755
}

func (d *containerDir) ModTime() time.Time {
	if !d.modTime.IsZero() {
		return d.modTime
	}
	return d.cfs.entryModTime(d.path)
}

func (d *containerDir) ReadDir() ([]vfs.AbstractDirEntry, error) {
	return d.cfs.readDirPath(d.path)
}

func (d *containerDir) Lookup(name string) (vfs.AbstractEntry, error) {
	fullPath := d.path + "/" + name
	return d.cfs.lookupPath(fullPath)
}

// containerFile implements vfs.AbstractFile for a file in the container.
type containerFile struct {
	entry    archive.Entry
	contents *os.File
}

func (f *containerFile) Stat() (uint64, fs.FileMode) {
	return uint64(f.entry.Size), f.entry.Mode
}

func (f *containerFile) ModTime() time.Time {
	return f.entry.ModTime
}

func (f *containerFile) ReadAt(off uint64, size uint32) ([]byte, error) {
	r, err := f.entry.Open(f.contents)
	if err != nil {
		return nil, fmt.Errorf("open entry: %w", err)
	}

	// Use ReadAt directly since Handle implements io.ReaderAt
	buf := make([]byte, size)
	n, err := r.ReadAt(buf, int64(off))
	if err == io.EOF || err == io.ErrUnexpectedEOF {
		return buf[:n], nil
	}
	if err != nil {
		return nil, fmt.Errorf("read at offset %d: %w", off, err)
	}
	return buf[:n], nil
}

func (f *containerFile) WriteAt(off uint64, data []byte) error {
	return fmt.Errorf("container filesystem is read-only")
}

func (f *containerFile) Truncate(size uint64) error {
	return fmt.Errorf("container filesystem is read-only")
}

var (
	_ vfs.AbstractDir  = (*ContainerFS)(nil)
	_ vfs.AbstractDir  = (*containerDir)(nil)
	_ vfs.AbstractFile = (*containerFile)(nil)
)
