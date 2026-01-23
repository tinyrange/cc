package api

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"time"

	"github.com/tinyrange/cc/internal/vfs"
)

// instanceFS implements FS with context support.
type instanceFS struct {
	inst *instance
	ctx  context.Context
}

// WithContext returns an FS that uses the given context for all operations.
func (f *instanceFS) WithContext(ctx context.Context) FS {
	return &instanceFS{
		inst: f.inst,
		ctx:  ctx,
	}
}

// Open opens a file for reading.
func (f *instanceFS) Open(name string) (File, error) {
	return f.OpenFile(name, os.O_RDONLY, 0)
}

// Create creates or truncates a file for writing.
func (f *instanceFS) Create(name string) (File, error) {
	return f.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0644)
}

// OpenFile is the generalized open call.
func (f *instanceFS) OpenFile(name string, flag int, perm fs.FileMode) (File, error) {
	// For read-only operations, we can use the ContainerFS directly
	if flag == os.O_RDONLY {
		return &instanceFile{
			inst: f.inst,
			ctx:  f.ctx,
			path: name,
			flag: flag,
			perm: perm,
		}, nil
	}

	// For write operations, we need to track the file differently
	return &instanceFile{
		inst: f.inst,
		ctx:  f.ctx,
		path: name,
		flag: flag,
		perm: perm,
	}, nil
}

// ReadFile reads the entire contents of a file.
func (f *instanceFS) ReadFile(name string) ([]byte, error) {
	// Use the ContainerFS directly for efficient reads
	cfs := f.inst.src.cfs

	// Resolve symlinks to get the actual file path
	resolvedPath, err := cfs.ResolvePath(name)
	if err != nil {
		return nil, &Error{Op: "readfile", Path: name, Err: err}
	}

	// Lookup the resolved file in the container filesystem
	entry, err := cfs.Lookup(resolvedPath)
	if err != nil {
		return nil, &Error{Op: "readfile", Path: name, Err: err}
	}

	if entry.File == nil {
		return nil, &Error{Op: "readfile", Path: name, Err: fmt.Errorf("not a regular file")}
	}

	// Get file size
	size, _ := entry.File.Stat()

	// Read the entire file
	data, err := entry.File.ReadAt(0, uint32(size))
	if err != nil {
		return nil, &Error{Op: "readfile", Path: name, Err: err}
	}

	return data, nil
}

// WriteFile writes data to a file, creating it if necessary.
func (f *instanceFS) WriteFile(name string, data []byte, perm fs.FileMode) error {
	if f.inst.vm == nil {
		return &Error{Op: "writefile", Path: name, Err: ErrNotRunning}
	}

	// Use the VM's WriteFile method
	reader := bytes.NewReader(data)
	if err := f.inst.vm.WriteFile(f.ctx, reader, int64(len(data)), name); err != nil {
		return &Error{Op: "writefile", Path: name, Err: err}
	}

	return nil
}

// Stat returns file info for the named file.
func (f *instanceFS) Stat(name string) (fs.FileInfo, error) {
	cfs := f.inst.src.cfs

	entry, err := cfs.Lookup(name)
	if err != nil {
		return nil, &Error{Op: "stat", Path: name, Err: err}
	}

	return entryToFileInfo(name, entry), nil
}

// Lstat returns file info without following symlinks.
func (f *instanceFS) Lstat(name string) (fs.FileInfo, error) {
	// For now, same as Stat - ContainerFS handles symlinks internally
	return f.Stat(name)
}

// Remove removes a file or empty directory.
func (f *instanceFS) Remove(name string) error {
	// Execute rm command in guest
	cmd := f.inst.CommandContext(f.ctx, "rm", name)
	if err := cmd.Run(); err != nil {
		return &Error{Op: "remove", Path: name, Err: err}
	}
	return nil
}

// RemoveAll removes a path and any children it contains.
func (f *instanceFS) RemoveAll(name string) error {
	// Execute rm -rf command in guest
	cmd := f.inst.CommandContext(f.ctx, "rm", "-rf", name)
	if err := cmd.Run(); err != nil {
		return &Error{Op: "removeall", Path: name, Err: err}
	}
	return nil
}

// Mkdir creates a directory.
func (f *instanceFS) Mkdir(name string, perm fs.FileMode) error {
	// Execute mkdir command in guest
	cmd := f.inst.CommandContext(f.ctx, "mkdir", name)
	if err := cmd.Run(); err != nil {
		return &Error{Op: "mkdir", Path: name, Err: err}
	}
	return nil
}

// MkdirAll creates a directory and any necessary parents.
func (f *instanceFS) MkdirAll(name string, perm fs.FileMode) error {
	// Execute mkdir -p command in guest
	cmd := f.inst.CommandContext(f.ctx, "mkdir", "-p", name)
	if err := cmd.Run(); err != nil {
		return &Error{Op: "mkdirall", Path: name, Err: err}
	}
	return nil
}

// Rename renames (moves) a file.
func (f *instanceFS) Rename(oldpath, newpath string) error {
	// Execute mv command in guest
	cmd := f.inst.CommandContext(f.ctx, "mv", oldpath, newpath)
	if err := cmd.Run(); err != nil {
		return &Error{Op: "rename", Path: oldpath, Err: err}
	}
	return nil
}

// Symlink creates a symbolic link.
func (f *instanceFS) Symlink(oldname, newname string) error {
	// Execute ln -s command in guest
	cmd := f.inst.CommandContext(f.ctx, "ln", "-s", oldname, newname)
	if err := cmd.Run(); err != nil {
		return &Error{Op: "symlink", Path: newname, Err: err}
	}
	return nil
}

// Readlink returns the destination of a symbolic link.
func (f *instanceFS) Readlink(name string) (string, error) {
	cfs := f.inst.src.cfs

	entry, err := cfs.Lookup(name)
	if err != nil {
		return "", &Error{Op: "readlink", Path: name, Err: err}
	}

	if entry.Symlink == nil {
		return "", &Error{Op: "readlink", Path: name, Err: fmt.Errorf("not a symlink")}
	}

	return entry.Symlink.Target(), nil
}

// ReadDir reads the named directory and returns its entries.
func (f *instanceFS) ReadDir(name string) ([]fs.DirEntry, error) {
	cfs := f.inst.src.cfs

	// Handle root directory
	var entries []vfs.AbstractDirEntry
	var err error

	if name == "/" || name == "" || name == "." {
		entries, err = cfs.ReadDir()
	} else {
		entry, lookupErr := cfs.Lookup(name)
		if lookupErr != nil {
			return nil, &Error{Op: "readdir", Path: name, Err: lookupErr}
		}
		if entry.Dir == nil {
			return nil, &Error{Op: "readdir", Path: name, Err: fmt.Errorf("not a directory")}
		}
		entries, err = entry.Dir.ReadDir()
	}

	if err != nil {
		return nil, &Error{Op: "readdir", Path: name, Err: err}
	}

	result := make([]fs.DirEntry, len(entries))
	for i, e := range entries {
		result[i] = &abstractDirEntry{e: e}
	}

	return result, nil
}

// Chmod changes the mode of the named file.
func (f *instanceFS) Chmod(name string, mode fs.FileMode) error {
	// Execute chmod command in guest
	modeStr := fmt.Sprintf("%04o", mode.Perm())
	cmd := f.inst.CommandContext(f.ctx, "chmod", modeStr, name)
	if err := cmd.Run(); err != nil {
		return &Error{Op: "chmod", Path: name, Err: err}
	}
	return nil
}

// Chown changes the numeric uid and gid of the named file.
func (f *instanceFS) Chown(name string, uid, gid int) error {
	// Execute chown command in guest
	ownership := fmt.Sprintf("%d:%d", uid, gid)
	cmd := f.inst.CommandContext(f.ctx, "chown", ownership, name)
	if err := cmd.Run(); err != nil {
		return &Error{Op: "chown", Path: name, Err: err}
	}
	return nil
}

// Chtimes changes the access and modification times of the named file.
func (f *instanceFS) Chtimes(name string, atime, mtime time.Time) error {
	// Execute touch with timestamp
	mtimeStr := mtime.Format("200601021504.05")
	cmd := f.inst.CommandContext(f.ctx, "touch", "-t", mtimeStr, name)
	if err := cmd.Run(); err != nil {
		return &Error{Op: "chtimes", Path: name, Err: err}
	}
	return nil
}

// instanceFile implements File.
type instanceFile struct {
	inst   *instance
	ctx    context.Context
	path   string
	flag   int
	perm   fs.FileMode
	offset int64
	closed bool
}

func (f *instanceFile) Read(p []byte) (int, error) {
	if f.closed {
		return 0, &Error{Op: "read", Path: f.path, Err: fs.ErrClosed}
	}

	cfs := f.inst.src.cfs
	entry, err := cfs.Lookup(f.path)
	if err != nil {
		return 0, &Error{Op: "read", Path: f.path, Err: err}
	}

	if entry.File == nil {
		return 0, &Error{Op: "read", Path: f.path, Err: fmt.Errorf("not a regular file")}
	}

	size, _ := entry.File.Stat()
	if f.offset >= int64(size) {
		return 0, io.EOF
	}

	toRead := len(p)
	remaining := int64(size) - f.offset
	if int64(toRead) > remaining {
		toRead = int(remaining)
	}

	data, err := entry.File.ReadAt(uint64(f.offset), uint32(toRead))
	if err != nil {
		return 0, &Error{Op: "read", Path: f.path, Err: err}
	}

	copy(p, data)
	f.offset += int64(len(data))

	if f.offset >= int64(size) {
		return len(data), io.EOF
	}

	return len(data), nil
}

func (f *instanceFile) Write(p []byte) (int, error) {
	// Writing to files requires using VM.WriteFile which writes the entire file
	// For streaming writes, we would need to buffer and write on Close
	return 0, &Error{Op: "write", Path: f.path, Err: fmt.Errorf("streaming write not yet implemented")}
}

func (f *instanceFile) Close() error {
	f.closed = true
	return nil
}

func (f *instanceFile) Seek(offset int64, whence int) (int64, error) {
	cfs := f.inst.src.cfs
	entry, err := cfs.Lookup(f.path)
	if err != nil {
		return 0, &Error{Op: "seek", Path: f.path, Err: err}
	}

	size := int64(0)
	if entry.File != nil {
		s, _ := entry.File.Stat()
		size = int64(s)
	}

	switch whence {
	case io.SeekStart:
		f.offset = offset
	case io.SeekCurrent:
		f.offset += offset
	case io.SeekEnd:
		f.offset = size + offset
	}

	if f.offset < 0 {
		f.offset = 0
	}

	return f.offset, nil
}

func (f *instanceFile) ReadAt(p []byte, off int64) (int, error) {
	cfs := f.inst.src.cfs
	entry, err := cfs.Lookup(f.path)
	if err != nil {
		return 0, &Error{Op: "readat", Path: f.path, Err: err}
	}

	if entry.File == nil {
		return 0, &Error{Op: "readat", Path: f.path, Err: fmt.Errorf("not a regular file")}
	}

	data, err := entry.File.ReadAt(uint64(off), uint32(len(p)))
	if err != nil {
		return 0, &Error{Op: "readat", Path: f.path, Err: err}
	}

	copy(p, data)
	return len(data), nil
}

func (f *instanceFile) WriteAt(p []byte, off int64) (int, error) {
	return 0, &Error{Op: "writeat", Path: f.path, Err: fmt.Errorf("not yet implemented")}
}

func (f *instanceFile) Stat() (fs.FileInfo, error) {
	cfs := f.inst.src.cfs
	entry, err := cfs.Lookup(f.path)
	if err != nil {
		return nil, &Error{Op: "stat", Path: f.path, Err: err}
	}

	return entryToFileInfo(f.path, entry), nil
}

func (f *instanceFile) Sync() error {
	// No-op for now
	return nil
}

func (f *instanceFile) Truncate(size int64) error {
	return &Error{Op: "truncate", Path: f.path, Err: fmt.Errorf("not yet implemented")}
}

func (f *instanceFile) Name() string {
	return f.path
}

// entryToFileInfo converts a vfs.AbstractEntry to fs.FileInfo.
func entryToFileInfo(name string, entry vfs.AbstractEntry) fs.FileInfo {
	return &fileInfo{
		name:  path.Base(name),
		entry: entry,
	}
}

type fileInfo struct {
	name  string
	entry vfs.AbstractEntry
}

func (fi *fileInfo) Name() string {
	return fi.name
}

func (fi *fileInfo) Size() int64 {
	if fi.entry.File != nil {
		size, _ := fi.entry.File.Stat()
		return int64(size)
	}
	return 0
}

func (fi *fileInfo) Mode() fs.FileMode {
	if fi.entry.File != nil {
		_, mode := fi.entry.File.Stat()
		return mode
	}
	if fi.entry.Dir != nil {
		return fs.ModeDir | fi.entry.Dir.Stat()
	}
	if fi.entry.Symlink != nil {
		return fs.ModeSymlink | fi.entry.Symlink.Stat()
	}
	return 0
}

func (fi *fileInfo) ModTime() time.Time {
	if fi.entry.File != nil {
		return fi.entry.File.ModTime()
	}
	if fi.entry.Dir != nil {
		return fi.entry.Dir.ModTime()
	}
	if fi.entry.Symlink != nil {
		return fi.entry.Symlink.ModTime()
	}
	return time.Time{}
}

func (fi *fileInfo) IsDir() bool {
	return fi.entry.Dir != nil
}

func (fi *fileInfo) Sys() any {
	return nil
}

// abstractDirEntry wraps vfs.AbstractDirEntry to implement fs.DirEntry.
type abstractDirEntry struct {
	e vfs.AbstractDirEntry
}

func (d *abstractDirEntry) Name() string {
	return d.e.Name
}

func (d *abstractDirEntry) IsDir() bool {
	return d.e.IsDir
}

func (d *abstractDirEntry) Type() fs.FileMode {
	if d.e.IsDir {
		return fs.ModeDir
	}
	return 0
}

func (d *abstractDirEntry) Info() (fs.FileInfo, error) {
	return &dirEntryInfo{e: d.e}, nil
}

type dirEntryInfo struct {
	e vfs.AbstractDirEntry
}

func (di *dirEntryInfo) Name() string       { return di.e.Name }
func (di *dirEntryInfo) Size() int64        { return int64(di.e.Size) }
func (di *dirEntryInfo) Mode() fs.FileMode  { return di.e.Mode }
func (di *dirEntryInfo) ModTime() time.Time { return time.Time{} }
func (di *dirEntryInfo) IsDir() bool        { return di.e.IsDir }
func (di *dirEntryInfo) Sys() any           { return nil }

var (
	_ FS   = (*instanceFS)(nil)
	_ File = (*instanceFile)(nil)
)
