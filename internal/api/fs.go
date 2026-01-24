package api

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
	"syscall"
	"time"

	"github.com/tinyrange/cc/internal/devices/virtio"
	"github.com/tinyrange/cc/internal/vfs"
)

// errnoToError converts a FUSE errno to a Go error.
func errnoToError(errno int32) error {
	if errno == 0 {
		return nil
	}
	// Convert to positive value for syscall.Errno
	if errno < 0 {
		errno = -errno
	}
	return syscall.Errno(errno)
}

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

// resolvePath walks the path and returns the parent nodeID, base name, and nodeID of the target.
// Returns errno if any part of the path cannot be resolved (except the last component).
func (f *instanceFS) resolvePath(p string) (parentID uint64, name string, nodeID uint64, errno int32) {
	p = path.Clean(p)
	if p == "/" || p == "" {
		return 1, "", 1, 0 // root
	}

	// Remove leading slash and split
	p = strings.TrimPrefix(p, "/")
	parts := strings.Split(p, "/")

	currentID := uint64(1) // root node ID

	for i, part := range parts {
		if part == "" {
			continue
		}
		childID, _, err := f.inst.fsBackend.Lookup(currentID, part)
		if err != 0 {
			if i == len(parts)-1 {
				// Last component doesn't exist - return parent and name
				return currentID, part, 0, err
			}
			return 0, "", 0, err
		}
		if i == len(parts)-1 {
			return currentID, part, childID, 0
		}
		currentID = childID
	}
	return currentID, "", currentID, 0
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
	if f.inst.fsBackend == nil {
		return &Error{Op: "writefile", Path: name, Err: ErrNotRunning}
	}

	// Resolve path to get parent
	parentID, baseName, nodeID, errno := f.resolvePath(name)
	if errno != 0 && parentID == 0 {
		return &Error{Op: "writefile", Path: name, Err: errnoToError(errno)}
	}

	// Type assert to get the Create and Write interfaces
	createBackend, hasCreate := f.inst.fsBackend.(interface {
		Create(parent uint64, name string, mode uint32, flags uint32, umask uint32, uid uint32, gid uint32) (nodeID uint64, fh uint64, attr virtio.FuseAttr, errno int32)
	})
	writeBackend, hasWrite := f.inst.fsBackend.(interface {
		Write(nodeID uint64, fh uint64, off uint64, data []byte) (uint32, int32)
	})

	if !hasCreate || !hasWrite {
		return &Error{Op: "writefile", Path: name, Err: fmt.Errorf("backend does not support file creation")}
	}

	var fh uint64

	if nodeID != 0 {
		// File exists, open it for writing (truncate)
		openBackend, hasOpen := f.inst.fsBackend.(interface {
			Open(nodeID uint64, flags uint32) (fh uint64, errno int32)
		})
		if !hasOpen {
			return &Error{Op: "writefile", Path: name, Err: fmt.Errorf("backend does not support file open")}
		}
		var err int32
		fh, err = openBackend.Open(nodeID, uint32(syscall.O_WRONLY|syscall.O_TRUNC))
		if err != 0 {
			return &Error{Op: "writefile", Path: name, Err: errnoToError(err)}
		}
		// Truncate via setattr
		setattrBackend, hasSetattr := f.inst.fsBackend.(interface {
			SetAttr(nodeID uint64, size *uint64, mode *uint32, uid *uint32, gid *uint32, atime *time.Time, mtime *time.Time, reqUID uint32, reqGID uint32) int32
		})
		if hasSetattr {
			zero := uint64(0)
			setattrBackend.SetAttr(nodeID, &zero, nil, nil, nil, nil, nil, 0, 0)
		}
	} else {
		// File doesn't exist, create it
		// Convert fs.FileMode to unix mode
		mode := uint32(perm.Perm()) | 0100000 // S_IFREG
		var err int32
		nodeID, fh, _, err = createBackend.Create(parentID, baseName, mode, uint32(syscall.O_WRONLY|syscall.O_CREAT|syscall.O_TRUNC), 0022, 0, 0)
		if err != 0 {
			return &Error{Op: "writefile", Path: name, Err: errnoToError(err)}
		}
	}

	// Write data
	if len(data) > 0 {
		_, err := writeBackend.Write(nodeID, fh, 0, data)
		if err != 0 {
			f.inst.fsBackend.Release(nodeID, fh)
			return &Error{Op: "writefile", Path: name, Err: errnoToError(err)}
		}
	}

	// Release file handle
	f.inst.fsBackend.Release(nodeID, fh)

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
	if f.inst.fsBackend == nil {
		return &Error{Op: "remove", Path: name, Err: ErrNotRunning}
	}

	// Resolve path
	parentID, baseName, nodeID, errno := f.resolvePath(name)
	if errno != 0 || nodeID == 0 {
		return &Error{Op: "remove", Path: name, Err: errnoToError(errno)}
	}

	// Check if it's a directory
	attr, err := f.inst.fsBackend.GetAttr(nodeID)
	if err != 0 {
		return &Error{Op: "remove", Path: name, Err: errnoToError(err)}
	}

	removeBackend, hasRemove := f.inst.fsBackend.(interface {
		Unlink(parent uint64, name string) int32
		Rmdir(parent uint64, name string) int32
	})
	if !hasRemove {
		return &Error{Op: "remove", Path: name, Err: fmt.Errorf("backend does not support removal")}
	}

	// Check if directory (mode & S_IFMT == S_IFDIR)
	isDir := (attr.Mode & 0170000) == 0040000
	if isDir {
		errno = removeBackend.Rmdir(parentID, baseName)
	} else {
		errno = removeBackend.Unlink(parentID, baseName)
	}

	if errno != 0 {
		return &Error{Op: "remove", Path: name, Err: errnoToError(errno)}
	}
	return nil
}

// RemoveAll removes a path and any children it contains.
func (f *instanceFS) RemoveAll(name string) error {
	if f.inst.fsBackend == nil {
		return &Error{Op: "removeall", Path: name, Err: ErrNotRunning}
	}

	// Resolve path
	parentID, baseName, nodeID, errno := f.resolvePath(name)
	if errno != 0 || nodeID == 0 {
		// If it doesn't exist, that's OK for RemoveAll
		return nil
	}

	removeBackend, hasRemove := f.inst.fsBackend.(interface {
		Unlink(parent uint64, name string) int32
		Rmdir(parent uint64, name string) int32
	})
	if !hasRemove {
		return &Error{Op: "removeall", Path: name, Err: fmt.Errorf("backend does not support removal")}
	}

	// Check if it's a directory
	attr, err := f.inst.fsBackend.GetAttr(nodeID)
	if err != 0 {
		return &Error{Op: "removeall", Path: name, Err: errnoToError(err)}
	}

	isDir := (attr.Mode & 0170000) == 0040000
	if !isDir {
		// It's a file, just unlink it
		errno = removeBackend.Unlink(parentID, baseName)
		if errno != 0 {
			return &Error{Op: "removeall", Path: name, Err: errnoToError(errno)}
		}
		return nil
	}

	// It's a directory, remove contents recursively
	if err := f.removeAllRecursive(nodeID, name, removeBackend); err != nil {
		return err
	}

	// Remove the directory itself
	errno = removeBackend.Rmdir(parentID, baseName)
	if errno != 0 {
		return &Error{Op: "removeall", Path: name, Err: errnoToError(errno)}
	}

	return nil
}

// removeAllRecursive recursively removes directory contents.
func (f *instanceFS) removeAllRecursive(nodeID uint64, dirPath string, removeBackend interface {
	Unlink(parent uint64, name string) int32
	Rmdir(parent uint64, name string) int32
}) error {
	// Read directory entries
	entries, errno := f.inst.fsBackend.ReadDir(nodeID, 0, 65536)
	if errno != 0 {
		return &Error{Op: "removeall", Path: dirPath, Err: errnoToError(errno)}
	}

	// Parse directory entries (they're in fuse dirent format)
	// Each entry: ino(8) + off(8) + namelen(4) + type(4) + name(namelen) + padding
	offset := 0
	for offset < len(entries) {
		if offset+24 > len(entries) {
			break
		}
		// Skip ino (8 bytes) and off (8 bytes)
		nameLen := int(entries[offset+16]) | int(entries[offset+17])<<8 | int(entries[offset+18])<<16 | int(entries[offset+19])<<24
		entryType := entries[offset+20]
		if offset+24+nameLen > len(entries) {
			break
		}
		entryName := string(entries[offset+24 : offset+24+nameLen])

		// Skip . and ..
		if entryName == "." || entryName == ".." {
			// Move to next entry (align to 8 bytes)
			entrySize := 24 + nameLen
			entrySize = (entrySize + 7) &^ 7
			offset += entrySize
			continue
		}

		// Get child node ID
		childID, _, err := f.inst.fsBackend.Lookup(nodeID, entryName)
		if err != 0 {
			// Move to next entry
			entrySize := 24 + nameLen
			entrySize = (entrySize + 7) &^ 7
			offset += entrySize
			continue
		}

		childPath := path.Join(dirPath, entryName)

		// Check if directory (DT_DIR = 4)
		if entryType == 4 {
			// Recursively remove directory contents
			if err := f.removeAllRecursive(childID, childPath, removeBackend); err != nil {
				return err
			}
			if errno := removeBackend.Rmdir(nodeID, entryName); errno != 0 {
				return &Error{Op: "removeall", Path: childPath, Err: errnoToError(errno)}
			}
		} else {
			// Remove file
			if errno := removeBackend.Unlink(nodeID, entryName); errno != 0 {
				return &Error{Op: "removeall", Path: childPath, Err: errnoToError(errno)}
			}
		}

		// Move to next entry (align to 8 bytes)
		entrySize := 24 + nameLen
		entrySize = (entrySize + 7) &^ 7
		offset += entrySize
	}

	return nil
}

// Mkdir creates a directory.
func (f *instanceFS) Mkdir(name string, perm fs.FileMode) error {
	if f.inst.fsBackend == nil {
		return &Error{Op: "mkdir", Path: name, Err: ErrNotRunning}
	}

	// Resolve path
	parentID, baseName, _, errno := f.resolvePath(name)
	if errno == 0 {
		// Path already exists
		return &Error{Op: "mkdir", Path: name, Err: fs.ErrExist}
	}
	if parentID == 0 {
		// Parent doesn't exist
		return &Error{Op: "mkdir", Path: name, Err: errnoToError(errno)}
	}

	mkdirBackend, hasMkdir := f.inst.fsBackend.(interface {
		Mkdir(parent uint64, name string, mode uint32, umask uint32, uid uint32, gid uint32) (nodeID uint64, attr virtio.FuseAttr, errno int32)
	})
	if !hasMkdir {
		return &Error{Op: "mkdir", Path: name, Err: fmt.Errorf("backend does not support mkdir")}
	}

	mode := uint32(perm.Perm()) | 0040000 // S_IFDIR
	_, _, errno = mkdirBackend.Mkdir(parentID, baseName, mode, 0022, 0, 0)
	if errno != 0 {
		return &Error{Op: "mkdir", Path: name, Err: errnoToError(errno)}
	}

	return nil
}

// MkdirAll creates a directory and any necessary parents.
func (f *instanceFS) MkdirAll(name string, perm fs.FileMode) error {
	if f.inst.fsBackend == nil {
		return &Error{Op: "mkdirall", Path: name, Err: ErrNotRunning}
	}

	mkdirBackend, hasMkdir := f.inst.fsBackend.(interface {
		Mkdir(parent uint64, name string, mode uint32, umask uint32, uid uint32, gid uint32) (nodeID uint64, attr virtio.FuseAttr, errno int32)
	})
	if !hasMkdir {
		return &Error{Op: "mkdirall", Path: name, Err: fmt.Errorf("backend does not support mkdir")}
	}

	name = path.Clean(name)
	if name == "/" || name == "" {
		return nil
	}

	// Split path and create directories one by one
	name = strings.TrimPrefix(name, "/")
	parts := strings.Split(name, "/")

	currentID := uint64(1) // root
	mode := uint32(perm.Perm()) | 0040000 // S_IFDIR

	for _, part := range parts {
		if part == "" {
			continue
		}

		// Try to lookup the directory
		childID, _, errno := f.inst.fsBackend.Lookup(currentID, part)
		if errno == 0 {
			// Directory exists, continue
			currentID = childID
			continue
		}

		// Directory doesn't exist, create it
		childID, _, errno = mkdirBackend.Mkdir(currentID, part, mode, 0022, 0, 0)
		if errno != 0 {
			return &Error{Op: "mkdirall", Path: name, Err: errnoToError(errno)}
		}
		currentID = childID
	}

	return nil
}

// Rename renames (moves) a file.
func (f *instanceFS) Rename(oldpath, newpath string) error {
	if f.inst.fsBackend == nil {
		return &Error{Op: "rename", Path: oldpath, Err: ErrNotRunning}
	}

	renameBackend, hasRename := f.inst.fsBackend.(interface {
		Rename(oldParent uint64, oldName string, newParent uint64, newName string, flags uint32) int32
	})
	if !hasRename {
		return &Error{Op: "rename", Path: oldpath, Err: fmt.Errorf("backend does not support rename")}
	}

	// Resolve old path
	oldParentID, oldBaseName, oldNodeID, errno := f.resolvePath(oldpath)
	if errno != 0 || oldNodeID == 0 {
		return &Error{Op: "rename", Path: oldpath, Err: errnoToError(errno)}
	}

	// Resolve new path (parent must exist, target may not)
	newParentID, newBaseName, _, errno := f.resolvePath(newpath)
	if newParentID == 0 {
		return &Error{Op: "rename", Path: newpath, Err: errnoToError(errno)}
	}

	errno = renameBackend.Rename(oldParentID, oldBaseName, newParentID, newBaseName, 0)
	if errno != 0 {
		return &Error{Op: "rename", Path: oldpath, Err: errnoToError(errno)}
	}

	return nil
}

// Symlink creates a symbolic link.
func (f *instanceFS) Symlink(oldname, newname string) error {
	if f.inst.fsBackend == nil {
		return &Error{Op: "symlink", Path: newname, Err: ErrNotRunning}
	}

	symlinkBackend, hasSymlink := f.inst.fsBackend.(interface {
		Symlink(parent uint64, name string, target string, umask uint32, uid uint32, gid uint32) (nodeID uint64, attr virtio.FuseAttr, errno int32)
	})
	if !hasSymlink {
		return &Error{Op: "symlink", Path: newname, Err: fmt.Errorf("backend does not support symlink")}
	}

	// Resolve new path (parent must exist, link name must not exist)
	parentID, baseName, nodeID, errno := f.resolvePath(newname)
	if parentID == 0 {
		return &Error{Op: "symlink", Path: newname, Err: errnoToError(errno)}
	}
	if nodeID != 0 {
		return &Error{Op: "symlink", Path: newname, Err: fs.ErrExist}
	}

	_, _, errno = symlinkBackend.Symlink(parentID, baseName, oldname, 0022, 0, 0)
	if errno != 0 {
		return &Error{Op: "symlink", Path: newname, Err: errnoToError(errno)}
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
	if f.inst.fsBackend == nil {
		return &Error{Op: "chmod", Path: name, Err: ErrNotRunning}
	}

	setattrBackend, hasSetattr := f.inst.fsBackend.(interface {
		SetAttr(nodeID uint64, size *uint64, mode *uint32, uid *uint32, gid *uint32, atime *time.Time, mtime *time.Time, reqUID uint32, reqGID uint32) int32
	})
	if !hasSetattr {
		return &Error{Op: "chmod", Path: name, Err: fmt.Errorf("backend does not support setattr")}
	}

	// Resolve path
	_, _, nodeID, errno := f.resolvePath(name)
	if errno != 0 || nodeID == 0 {
		return &Error{Op: "chmod", Path: name, Err: errnoToError(errno)}
	}

	// Get current attr to preserve file type bits
	attr, err := f.inst.fsBackend.GetAttr(nodeID)
	if err != 0 {
		return &Error{Op: "chmod", Path: name, Err: errnoToError(err)}
	}

	// Preserve file type, update permission bits
	newMode := (attr.Mode & 0170000) | uint32(mode.Perm())
	errno = setattrBackend.SetAttr(nodeID, nil, &newMode, nil, nil, nil, nil, 0, 0)
	if errno != 0 {
		return &Error{Op: "chmod", Path: name, Err: errnoToError(errno)}
	}

	return nil
}

// Chown changes the numeric uid and gid of the named file.
func (f *instanceFS) Chown(name string, uid, gid int) error {
	if f.inst.fsBackend == nil {
		return &Error{Op: "chown", Path: name, Err: ErrNotRunning}
	}

	setattrBackend, hasSetattr := f.inst.fsBackend.(interface {
		SetAttr(nodeID uint64, size *uint64, mode *uint32, uid *uint32, gid *uint32, atime *time.Time, mtime *time.Time, reqUID uint32, reqGID uint32) int32
	})
	if !hasSetattr {
		return &Error{Op: "chown", Path: name, Err: fmt.Errorf("backend does not support setattr")}
	}

	// Resolve path
	_, _, nodeID, errno := f.resolvePath(name)
	if errno != 0 || nodeID == 0 {
		return &Error{Op: "chown", Path: name, Err: errnoToError(errno)}
	}

	uidVal := uint32(uid)
	gidVal := uint32(gid)
	errno = setattrBackend.SetAttr(nodeID, nil, nil, &uidVal, &gidVal, nil, nil, 0, 0)
	if errno != 0 {
		return &Error{Op: "chown", Path: name, Err: errnoToError(errno)}
	}

	return nil
}

// Chtimes changes the access and modification times of the named file.
func (f *instanceFS) Chtimes(name string, atime, mtime time.Time) error {
	if f.inst.fsBackend == nil {
		return &Error{Op: "chtimes", Path: name, Err: ErrNotRunning}
	}

	setattrBackend, hasSetattr := f.inst.fsBackend.(interface {
		SetAttr(nodeID uint64, size *uint64, mode *uint32, uid *uint32, gid *uint32, atime *time.Time, mtime *time.Time, reqUID uint32, reqGID uint32) int32
	})
	if !hasSetattr {
		return &Error{Op: "chtimes", Path: name, Err: fmt.Errorf("backend does not support setattr")}
	}

	// Resolve path
	_, _, nodeID, errno := f.resolvePath(name)
	if errno != 0 || nodeID == 0 {
		return &Error{Op: "chtimes", Path: name, Err: errnoToError(errno)}
	}

	errno = setattrBackend.SetAttr(nodeID, nil, nil, nil, nil, &atime, &mtime, 0, 0)
	if errno != 0 {
		return &Error{Op: "chtimes", Path: name, Err: errnoToError(errno)}
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
