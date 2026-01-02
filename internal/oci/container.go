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

// ResolvePath resolves a path within the container filesystem, following symlinks
// (including in intermediate components). The returned path is absolute (starts with "/").
//
// This is useful for exec'ing symlink entrypoints as their real targets so the dynamic
// loader sees the correct executable location (e.g. for $ORIGIN-based RUNPATH).
func (cfs *ContainerFS) ResolvePath(p string) (string, error) {
	resolved, _, _, isImplicitDir, err := cfs.resolvePath(p, 40)
	if err != nil {
		return "", err
	}
	if isImplicitDir {
		return "", fmt.Errorf("path resolves to directory: %s", p)
	}
	if resolved == "." {
		return "/", nil
	}
	return "/" + resolved, nil
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
		name = strings.TrimSuffix(name, "/") // Strip trailing slash from directories
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

func isOpaqueMarker(p string) bool {
	return path.Base(p) == ".wh..wh..opq"
}

func opaqueMarkerPath(dirPath string) string {
	dirPath = normalizePath(dirPath)
	if dirPath == "" {
		return ".wh..wh..opq"
	}
	return dirPath + "/.wh..wh..opq"
}

func (cfs *ContainerFS) layerHasOpaqueDir(lr *layerReader, dirPath string) bool {
	marker := opaqueMarkerPath(dirPath)
	ent, ok := lr.entries[marker]
	if !ok {
		return false
	}
	// If the marker itself was deleted, treat it as absent.
	return ent.Kind != archive.EntryKindDeleted
}

// findPath returns an entry if there is an exact match, otherwise it may return
// a synthetic directory if any layer contains entries with the given path as a
// prefix (e.g. "etc" exists because "etc/passwd" exists).
//
// The returned *layerReader is only valid when implicitDir == false.
func (cfs *ContainerFS) findPath(filePath string) (ent archive.Entry, lr *layerReader, implicitDir bool, err error) {
	filePath = normalizePath(filePath)
	if filePath == "" {
		filePath = "."
	}

	for i := len(cfs.layers) - 1; i >= 0; i-- {
		layer := cfs.layers[i]

		// Exact match first.
		if e, ok := layer.entries[filePath]; ok {
			if e.Kind == archive.EntryKindDeleted {
				return archive.Entry{}, nil, false, fmt.Errorf("file not found: %s", filePath)
			}
			return e, layer, false, nil
		}

		// Synthetic directory (implicit) if any entry has this as a prefix.
		prefix := filePath + "/"
		for name, e := range layer.entries {
			if e.Kind == archive.EntryKindDeleted {
				continue
			}
			if strings.HasPrefix(name, prefix) {
				return archive.Entry{}, nil, true, nil
			}
		}
	}

	return archive.Entry{}, nil, false, fmt.Errorf("file not found: %s", filePath)
}

// resolvePath resolves a possibly-symlinked path by walking it component-by-component,
// following symlinks and hardlinks along the way (including when symlinks appear in
// intermediate components like /lib on merged-/usr systems).
//
// It returns the resolved path (normalized, no leading "/") and the final entry.
// If the final target is an implicit directory (no explicit archive entry), implicitDir is true.
func (cfs *ContainerFS) resolvePath(filePath string, maxLinks int) (resolved string, ent archive.Entry, lr *layerReader, implicitDir bool, err error) {
	filePath = normalizePath(filePath)
	if filePath == "" {
		// Root
		return ".", archive.Entry{}, nil, true, nil
	}

	linksFollowed := 0
	p := path.Clean(filePath)

	for {
		parts := strings.Split(p, "/")
		cur := ""
		lastImplicitDir := false

		for i := 0; i < len(parts); i++ {
			comp := parts[i]
			if comp == "" || comp == "." {
				continue
			}

			cand := comp
			if cur != "" {
				cand = cur + "/" + comp
			}

			e, layer, isImplicitDir, findErr := cfs.findPath(cand)
			if findErr != nil {
				return "", archive.Entry{}, nil, false, findErr
			}

			// Directory synthesized from child entries; continue walking.
			if isImplicitDir {
				cur = cand
				ent = archive.Entry{}
				lr = nil
				lastImplicitDir = true
				continue
			}

			// Follow links if present. This is critical for cases like:
			//   /sbin/init -> /lib/systemd/systemd
			// where /lib itself is a symlink (merged-/usr), meaning the literal
			// path "lib/systemd/systemd" may not exist in the layer index.
			if e.Kind == archive.EntryKindSymlink || e.Kind == archive.EntryKindHardlink {
				if linksFollowed >= maxLinks {
					return "", archive.Entry{}, nil, false, fmt.Errorf("too many symlink/hardlink traversals resolving %q", filePath)
				}
				linksFollowed++

				target := e.Linkname
				if !path.IsAbs(target) {
					target = path.Join(path.Dir(cand), target)
				}
				target = normalizePath(target)

				rest := strings.Join(parts[i+1:], "/")
				if rest != "" {
					p = path.Join(target, rest)
				} else {
					p = target
				}

				// Restart the walk with the new path.
				goto restart
			}

			// Regular file/dir; continue walking.
			cur = cand
			ent = e
			lr = layer
			implicitDir = false
			lastImplicitDir = false
		}

		// If we ended on a synthesized directory, return it.
		if lastImplicitDir {
			return cur, archive.Entry{}, nil, true, nil
		}

		return cur, ent, lr, false, nil

	restart:
		continue
	}
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
		opaqueHere := cfs.layerHasOpaqueDir(lr, dirPath)
		for name, ent := range lr.entries {
			// Do not expose overlayfs whiteout markers themselves.
			if isOpaqueMarker(name) {
				continue
			}

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
		// Opaque directory: do not inherit entries from lower layers.
		if opaqueHere {
			break
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
	if isOpaqueMarker(filePath) {
		return vfs.AbstractEntry{}, fmt.Errorf("file not found: %s", filePath)
	}
	parentDir := path.Dir(filePath)
	if parentDir == "." {
		parentDir = ""
	}

	// Search layers from top to bottom
	for i := len(cfs.layers) - 1; i >= 0; i-- {
		lr := cfs.layers[i]
		opaqueParentHere := parentDir != "" && cfs.layerHasOpaqueDir(lr, parentDir)

		// Check for exact match
		if ent, ok := lr.entries[filePath]; ok {
			if ent.Kind == archive.EntryKindDeleted {
				// Deleted by this layer
				return vfs.AbstractEntry{}, fmt.Errorf("file not found: %s", filePath)
			}
			// Never surface the marker file itself as a real entry.
			if isOpaqueMarker(ent.Name) {
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

		// If the parent directory is opaque in this layer, we must not consult lower layers
		// for children under it (including this filePath).
		if opaqueParentHere {
			break
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
				uid:     uint32(ent.UID),
				gid:     uint32(ent.GID),
			},
		}, nil
	case archive.EntryKindRegular:
		return vfs.AbstractEntry{
			File: &containerFile{
				entry:    ent,
				contents: lr.contents,
			},
		}, nil
	case archive.EntryKindSymlink:
		// Preserve symlinks as symlinks. Many userspace components (notably systemd)
		// expect enablement links under *.wants/ to be actual symlinks.
		return vfs.AbstractEntry{
			Symlink: &containerSymlink{
				target:  ent.Linkname,
				mode:    ent.Mode,
				modTime: ent.ModTime,
				uid:     uint32(ent.UID),
				gid:     uint32(ent.GID),
			},
		}, nil
	case archive.EntryKindHardlink:
		// Hardlinks are presented as their target contents (regular file or dir).
		// This is a read-only filesystem view, so inode identity isn't observable.
		target := ent.Linkname
		if !path.IsAbs(target) {
			target = path.Join(path.Dir(filePath), target)
		}
		resolvedPath, resolvedEnt, resolvedLayer, isImplicitDir, err := cfs.resolvePath(target, 40)
		if err != nil {
			return vfs.AbstractEntry{}, err
		}
		if isImplicitDir {
			return vfs.AbstractEntry{
				Dir: &containerDir{
					cfs:  cfs,
					path: resolvedPath,
				},
			}, nil
		}
		return cfs.entryToAbstract(resolvedPath, resolvedEnt, resolvedLayer)
	default:
		return vfs.AbstractEntry{}, fmt.Errorf("unsupported entry kind: %v", ent.Kind)
	}
}

// containerDir implements vfs.AbstractDir for a subdirectory.
type containerDir struct {
	cfs  *ContainerFS
	path string

	modTime time.Time
	uid     uint32
	gid     uint32
}

func (d *containerDir) Stat() fs.FileMode {
	return 0o755
}

func (d *containerDir) Owner() (uint32, uint32) {
	return d.uid, d.gid
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

func (f *containerFile) Owner() (uint32, uint32) {
	return uint32(f.entry.UID), uint32(f.entry.GID)
}

func (f *containerFile) ModTime() time.Time {
	return f.entry.ModTime
}

func (f *containerFile) ReadAt(off uint64, size uint32) ([]byte, error) {
	// Clamp size to the actual file size to avoid reading beyond the end
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

	// Use ReadAt directly since Handle implements io.ReaderAt
	buf := make([]byte, size)
	// Zero the buffer to avoid garbage data
	clear(buf)
	n, err := r.ReadAt(buf, int64(off))
	if err == io.EOF || err == io.ErrUnexpectedEOF {
		// Return exactly the bytes read, even if less than requested
		if n == 0 {
			return []byte{}, nil
		}
		return buf[:n], nil
	}
	if err != nil {
		return nil, fmt.Errorf("read at offset %d: %w", off, err)
	}
	// Always return exactly the bytes read, even if less than requested
	if n == 0 {
		return []byte{}, nil
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
	_ vfs.AbstractDir   = (*ContainerFS)(nil)
	_ vfs.AbstractDir   = (*containerDir)(nil)
	_ vfs.AbstractOwner = (*containerDir)(nil)
	_ vfs.AbstractFile  = (*containerFile)(nil)
	_ vfs.AbstractOwner = (*containerFile)(nil)
)

// containerSymlink implements vfs.AbstractSymlink for a symlink entry in the container.
type containerSymlink struct {
	target  string
	mode    fs.FileMode
	modTime time.Time
	uid     uint32
	gid     uint32
}

func (s *containerSymlink) Stat() fs.FileMode { return s.mode }
func (s *containerSymlink) ModTime() time.Time {
	if !s.modTime.IsZero() {
		return s.modTime
	}
	return time.Unix(0, 0)
}
func (s *containerSymlink) Target() string { return s.target }
func (s *containerSymlink) Owner() (uint32, uint32) {
	return s.uid, s.gid
}

var (
	_ vfs.AbstractSymlink = (*containerSymlink)(nil)
	_ vfs.AbstractOwner   = (*containerSymlink)(nil)
)
