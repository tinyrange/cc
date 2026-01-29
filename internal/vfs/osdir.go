package vfs

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// OSDirBackend implements AbstractDir by wrapping a host directory path.
// This allows exposing host directories to guests via virtio-fs.
type OSDirBackend struct {
	hostPath string
	readOnly bool
}

// NewOSDirBackend creates a new OS directory backend wrapping the given host path.
// If readOnly is true, all write operations will return errors.
func NewOSDirBackend(hostPath string, readOnly bool) (*OSDirBackend, error) {
	// Verify the path exists and is a directory
	info, err := os.Stat(hostPath)
	if err != nil {
		return nil, fmt.Errorf("stat host path: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("host path is not a directory: %s", hostPath)
	}

	absPath, err := filepath.Abs(hostPath)
	if err != nil {
		return nil, fmt.Errorf("absolute path: %w", err)
	}

	return &OSDirBackend{
		hostPath: absPath,
		readOnly: readOnly,
	}, nil
}

// Stat implements AbstractDir.
func (d *OSDirBackend) Stat() fs.FileMode {
	return 0o755
}

// ModTime implements AbstractDir.
func (d *OSDirBackend) ModTime() time.Time {
	info, err := os.Stat(d.hostPath)
	if err != nil {
		return time.Unix(0, 0)
	}
	return info.ModTime()
}

// ReadDir implements AbstractDir.
func (d *OSDirBackend) ReadDir() ([]AbstractDirEntry, error) {
	return d.readDirPath("")
}

func (d *OSDirBackend) readDirPath(relPath string) ([]AbstractDirEntry, error) {
	fullPath := d.hostPath
	if relPath != "" {
		fullPath = filepath.Join(d.hostPath, relPath)
	}

	entries, err := os.ReadDir(fullPath)
	if err != nil {
		return nil, fmt.Errorf("read dir: %w", err)
	}

	result := make([]AbstractDirEntry, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue // Skip entries we can't stat
		}

		result = append(result, AbstractDirEntry{
			Name:  entry.Name(),
			IsDir: entry.IsDir(),
			Mode:  info.Mode(),
			Size:  uint64(info.Size()),
		})
	}

	return result, nil
}

// Lookup implements AbstractDir.
func (d *OSDirBackend) Lookup(name string) (AbstractEntry, error) {
	return d.lookupPath(name)
}

func (d *OSDirBackend) lookupPath(relPath string) (AbstractEntry, error) {
	fullPath := filepath.Join(d.hostPath, relPath)

	// Security check: ensure resolved path is within hostPath
	absPath, err := filepath.Abs(fullPath)
	if err != nil {
		return AbstractEntry{}, fmt.Errorf("resolve path: %w", err)
	}

	// Check that the resolved path is within the host path
	// This prevents directory traversal attacks
	rel, err := filepath.Rel(d.hostPath, absPath)
	if err != nil || len(rel) >= 2 && rel[:2] == ".." {
		return AbstractEntry{}, fmt.Errorf("path escapes mount: %s", relPath)
	}

	info, err := os.Lstat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return AbstractEntry{}, fmt.Errorf("file not found: %s", relPath)
		}
		return AbstractEntry{}, fmt.Errorf("stat: %w", err)
	}

	switch {
	case info.Mode().IsDir():
		return AbstractEntry{
			Dir: &osDirEntry{
				backend: d,
				relPath: relPath,
				info:    info,
			},
		}, nil
	case info.Mode().IsRegular():
		return AbstractEntry{
			File: &osFileEntry{
				backend: d,
				relPath: relPath,
				info:    info,
			},
		}, nil
	case info.Mode()&fs.ModeSymlink != 0:
		target, err := os.Readlink(fullPath)
		if err != nil {
			return AbstractEntry{}, fmt.Errorf("readlink: %w", err)
		}
		return AbstractEntry{
			Symlink: &osSymlinkEntry{
				target: target,
				info:   info,
			},
		}, nil
	default:
		// For other file types (devices, sockets, etc.), treat as regular files
		return AbstractEntry{
			File: &osFileEntry{
				backend: d,
				relPath: relPath,
				info:    info,
			},
		}, nil
	}
}

var _ AbstractDir = (*OSDirBackend)(nil)

// osDirEntry implements AbstractDir for a subdirectory in OSDirBackend.
type osDirEntry struct {
	backend *OSDirBackend
	relPath string
	info    fs.FileInfo
}

func (d *osDirEntry) Stat() fs.FileMode  { return d.info.Mode() }
func (d *osDirEntry) ModTime() time.Time { return d.info.ModTime() }
func (d *osDirEntry) ReadDir() ([]AbstractDirEntry, error) {
	return d.backend.readDirPath(d.relPath)
}
func (d *osDirEntry) Lookup(name string) (AbstractEntry, error) {
	fullRelPath := d.relPath + "/" + name
	if d.relPath == "" {
		fullRelPath = name
	}
	return d.backend.lookupPath(fullRelPath)
}

var _ AbstractDir = (*osDirEntry)(nil)

// osFileEntry implements AbstractFile for a file in OSDirBackend.
type osFileEntry struct {
	backend *OSDirBackend
	relPath string
	info    fs.FileInfo
}

func (f *osFileEntry) Stat() (uint64, fs.FileMode) {
	return uint64(f.info.Size()), f.info.Mode()
}

func (f *osFileEntry) ModTime() time.Time {
	return f.info.ModTime()
}

func (f *osFileEntry) ReadAt(off uint64, size uint32) ([]byte, error) {
	fullPath := filepath.Join(f.backend.hostPath, f.relPath)

	file, err := os.Open(fullPath)
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}
	defer file.Close()

	buf := make([]byte, size)
	n, err := file.ReadAt(buf, int64(off))
	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("read file: %w", err)
	}

	return buf[:n], nil
}

func (f *osFileEntry) WriteAt(off uint64, data []byte) error {
	if f.backend.readOnly {
		return fmt.Errorf("filesystem is read-only")
	}

	fullPath := filepath.Join(f.backend.hostPath, f.relPath)

	file, err := os.OpenFile(fullPath, os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("open file: %w", err)
	}
	defer file.Close()

	_, err = file.WriteAt(data, int64(off))
	if err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	return nil
}

func (f *osFileEntry) Truncate(size uint64) error {
	if f.backend.readOnly {
		return fmt.Errorf("filesystem is read-only")
	}

	fullPath := filepath.Join(f.backend.hostPath, f.relPath)

	if err := os.Truncate(fullPath, int64(size)); err != nil {
		return fmt.Errorf("truncate file: %w", err)
	}

	return nil
}

var _ AbstractFile = (*osFileEntry)(nil)

// osSymlinkEntry implements AbstractSymlink for a symlink in OSDirBackend.
type osSymlinkEntry struct {
	target string
	info   fs.FileInfo
}

func (s *osSymlinkEntry) Stat() fs.FileMode  { return s.info.Mode() }
func (s *osSymlinkEntry) ModTime() time.Time { return s.info.ModTime() }
func (s *osSymlinkEntry) Target() string     { return s.target }

var _ AbstractSymlink = (*osSymlinkEntry)(nil)
