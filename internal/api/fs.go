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
// This does NOT follow symlinks for the final component.
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

// resolvePathFollowSymlinks is like resolvePath but follows symlinks for all components including the final one.
// It returns the final nodeID after following all symlinks.
func (f *instanceFS) resolvePathFollowSymlinks(p string) (nodeID uint64, errno int32) {
	const maxSymlinkDepth = 40 // prevent infinite loops

	p = path.Clean(p)
	if p == "/" || p == "" {
		return 1, 0 // root
	}

	readlinkBackend, hasReadlink := f.inst.fsBackend.(interface {
		Readlink(nodeID uint64) (target string, errno int32)
	})
	if !hasReadlink {
		// No symlink support, fall back to regular resolution
		_, _, id, err := f.resolvePath(p)
		return id, err
	}

	// Start from root
	currentID := uint64(1)
	p = strings.TrimPrefix(p, "/")
	parts := strings.Split(p, "/")

	symlinkDepth := 0

	for i := 0; i < len(parts); i++ {
		part := parts[i]
		if part == "" {
			continue
		}

		childID, attr, err := f.inst.fsBackend.Lookup(currentID, part)
		if err != 0 {
			return 0, err
		}

		// Check if this is a symlink
		if (attr.Mode & 0170000) == 0120000 { // S_IFLNK
			symlinkDepth++
			if symlinkDepth > maxSymlinkDepth {
				return 0, -int32(syscall.ELOOP)
			}

			// Read symlink target
			target, err := readlinkBackend.Readlink(childID)
			if err != 0 {
				return 0, err
			}

			// Construct new path
			var newPath string
			if strings.HasPrefix(target, "/") {
				// Absolute symlink
				newPath = target
			} else {
				// Relative symlink - resolve from parent directory
				currentParts := parts[:i]
				newPath = "/" + strings.Join(currentParts, "/") + "/" + target
			}

			// Append remaining parts
			if i+1 < len(parts) {
				newPath = newPath + "/" + strings.Join(parts[i+1:], "/")
			}

			// Restart resolution with new path
			newPath = path.Clean(newPath)
			currentID = 1
			parts = strings.Split(strings.TrimPrefix(newPath, "/"), "/")
			i = -1 // Will be incremented to 0
			continue
		}

		currentID = childID
	}

	return currentID, 0
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
	if f.inst.fsBackend == nil {
		return nil, &Error{Op: "open", Path: name, Err: ErrNotRunning}
	}

	// Resolve path through fsBackend, following symlinks
	nodeID, errno := f.resolvePathFollowSymlinks(name)
	if errno != 0 || nodeID == 0 {
		return nil, &Error{Op: "open", Path: name, Err: errnoToError(errno)}
	}

	// Check if Open is supported
	openBackend, hasOpen := f.inst.fsBackend.(interface {
		Open(nodeID uint64, flags uint32) (fh uint64, errno int32)
	})
	if !hasOpen {
		return nil, &Error{Op: "open", Path: name, Err: fmt.Errorf("backend does not support open")}
	}

	// Convert Go flags to POSIX flags
	var flags uint32
	switch flag & (os.O_RDONLY | os.O_WRONLY | os.O_RDWR) {
	case os.O_RDONLY:
		flags = uint32(syscall.O_RDONLY)
	case os.O_WRONLY:
		flags = uint32(syscall.O_WRONLY)
	case os.O_RDWR:
		flags = uint32(syscall.O_RDWR)
	}

	fh, errno := openBackend.Open(nodeID, flags)
	if errno != 0 {
		return nil, &Error{Op: "open", Path: name, Err: errnoToError(errno)}
	}

	return &instanceFile{
		inst:   f.inst,
		ctx:    f.ctx,
		path:   name,
		flag:   flag,
		perm:   perm,
		nodeID: nodeID,
		fh:     fh,
	}, nil
}

// ReadFile reads the entire contents of a file.
func (f *instanceFS) ReadFile(name string) ([]byte, error) {
	if f.inst.fsBackend == nil {
		return nil, &Error{Op: "readfile", Path: name, Err: ErrNotRunning}
	}

	// Resolve path through the fsBackend, following symlinks
	nodeID, errno := f.resolvePathFollowSymlinks(name)
	if errno != 0 || nodeID == 0 {
		return nil, &Error{Op: "readfile", Path: name, Err: errnoToError(errno)}
	}

	// Get file attributes to check type and size
	attr, errno := f.inst.fsBackend.GetAttr(nodeID)
	if errno != 0 {
		return nil, &Error{Op: "readfile", Path: name, Err: errnoToError(errno)}
	}

	// Check if it's a regular file (S_IFREG = 0100000)
	if (attr.Mode & 0170000) != 0100000 {
		return nil, &Error{Op: "readfile", Path: name, Err: fmt.Errorf("not a regular file")}
	}

	// Open the file
	openBackend, hasOpen := f.inst.fsBackend.(interface {
		Open(nodeID uint64, flags uint32) (fh uint64, errno int32)
	})
	if !hasOpen {
		return nil, &Error{Op: "readfile", Path: name, Err: fmt.Errorf("backend does not support open")}
	}

	fh, errno := openBackend.Open(nodeID, uint32(syscall.O_RDONLY))
	if errno != 0 {
		return nil, &Error{Op: "readfile", Path: name, Err: errnoToError(errno)}
	}
	defer f.inst.fsBackend.Release(nodeID, fh)

	// Read the entire file
	data, errno := f.inst.fsBackend.Read(nodeID, fh, 0, uint32(attr.Size))
	if errno != 0 {
		return nil, &Error{Op: "readfile", Path: name, Err: errnoToError(errno)}
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

// Stat returns file info for the named file, following symlinks.
func (f *instanceFS) Stat(name string) (fs.FileInfo, error) {
	if f.inst.fsBackend == nil {
		return nil, &Error{Op: "stat", Path: name, Err: ErrNotRunning}
	}

	// Resolve path through the fsBackend, following symlinks
	nodeID, errno := f.resolvePathFollowSymlinks(name)
	if errno != 0 || nodeID == 0 {
		return nil, &Error{Op: "stat", Path: name, Err: errnoToError(errno)}
	}

	attr, errno := f.inst.fsBackend.GetAttr(nodeID)
	if errno != 0 {
		return nil, &Error{Op: "stat", Path: name, Err: errnoToError(errno)}
	}

	return fuseAttrToFileInfo(name, attr), nil
}

// Lstat returns file info without following symlinks.
func (f *instanceFS) Lstat(name string) (fs.FileInfo, error) {
	if f.inst.fsBackend == nil {
		return nil, &Error{Op: "lstat", Path: name, Err: ErrNotRunning}
	}

	// resolvePath doesn't follow symlinks for the final component
	_, _, nodeID, errno := f.resolvePath(name)
	if errno != 0 || nodeID == 0 {
		return nil, &Error{Op: "lstat", Path: name, Err: errnoToError(errno)}
	}

	attr, errno := f.inst.fsBackend.GetAttr(nodeID)
	if errno != 0 {
		return nil, &Error{Op: "lstat", Path: name, Err: errnoToError(errno)}
	}

	return fuseAttrToFileInfo(name, attr), nil
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

	currentID := uint64(1)                // root
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
	if f.inst.fsBackend == nil {
		return "", &Error{Op: "readlink", Path: name, Err: ErrNotRunning}
	}

	// Resolve path to get node ID
	_, _, nodeID, errno := f.resolvePath(name)
	if errno != 0 || nodeID == 0 {
		return "", &Error{Op: "readlink", Path: name, Err: errnoToError(errno)}
	}

	// Check if Readlink is supported
	readlinkBackend, hasReadlink := f.inst.fsBackend.(interface {
		Readlink(nodeID uint64) (target string, errno int32)
	})
	if !hasReadlink {
		return "", &Error{Op: "readlink", Path: name, Err: fmt.Errorf("backend does not support readlink")}
	}

	target, errno := readlinkBackend.Readlink(nodeID)
	if errno != 0 {
		return "", &Error{Op: "readlink", Path: name, Err: errnoToError(errno)}
	}

	return target, nil
}

// ReadDir reads the named directory and returns its entries.
func (f *instanceFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if f.inst.fsBackend == nil {
		return nil, &Error{Op: "readdir", Path: name, Err: ErrNotRunning}
	}

	// Resolve directory path
	var nodeID uint64
	if name == "/" || name == "" || name == "." {
		nodeID = 1 // root
	} else {
		_, _, id, errno := f.resolvePath(name)
		if errno != 0 || id == 0 {
			return nil, &Error{Op: "readdir", Path: name, Err: errnoToError(errno)}
		}
		nodeID = id
	}

	// Verify it's a directory
	attr, errno := f.inst.fsBackend.GetAttr(nodeID)
	if errno != 0 {
		return nil, &Error{Op: "readdir", Path: name, Err: errnoToError(errno)}
	}
	if (attr.Mode & 0170000) != 0040000 {
		return nil, &Error{Op: "readdir", Path: name, Err: fmt.Errorf("not a directory")}
	}

	// Read directory entries from fsBackend (FUSE dirent format)
	data, errno := f.inst.fsBackend.ReadDir(nodeID, 0, 1<<20) // 1MB max
	if errno != 0 {
		return nil, &Error{Op: "readdir", Path: name, Err: errnoToError(errno)}
	}

	// Parse FUSE dirent entries
	var result []fs.DirEntry
	offset := 0
	for offset < len(data) {
		if offset+24 > len(data) {
			break
		}
		// struct fuse_dirent: ino(8) + off(8) + namelen(4) + type(4) + name(namelen)
		nameLen := int(data[offset+16]) | int(data[offset+17])<<8 | int(data[offset+18])<<16 | int(data[offset+19])<<24
		entryType := data[offset+20]
		if offset+24+nameLen > len(data) {
			break
		}
		entryName := string(data[offset+24 : offset+24+nameLen])

		// Skip . and ..
		if entryName != "." && entryName != ".." {
			result = append(result, &fuseDirEntry{
				name:      entryName,
				entryType: entryType,
			})
		}

		// Move to next entry (align to 8 bytes)
		entrySize := 24 + nameLen
		entrySize = (entrySize + 7) &^ 7
		offset += entrySize
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
	nodeID uint64 // fsBackend node ID
	fh     uint64 // fsBackend file handle
}

func (f *instanceFile) Read(p []byte) (int, error) {
	if f.closed {
		return 0, &Error{Op: "read", Path: f.path, Err: fs.ErrClosed}
	}

	// Get current file size from fsBackend
	attr, errno := f.inst.fsBackend.GetAttr(f.nodeID)
	if errno != 0 {
		return 0, &Error{Op: "read", Path: f.path, Err: errnoToError(errno)}
	}

	size := int64(attr.Size)
	if f.offset >= size {
		return 0, io.EOF
	}

	toRead := len(p)
	remaining := size - f.offset
	if int64(toRead) > remaining {
		toRead = int(remaining)
	}

	data, errno := f.inst.fsBackend.Read(f.nodeID, f.fh, uint64(f.offset), uint32(toRead))
	if errno != 0 {
		return 0, &Error{Op: "read", Path: f.path, Err: errnoToError(errno)}
	}

	copy(p, data)
	f.offset += int64(len(data))

	if f.offset >= size {
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
	if f.closed {
		return nil
	}
	f.closed = true
	// Release the file handle
	f.inst.fsBackend.Release(f.nodeID, f.fh)
	return nil
}

func (f *instanceFile) Seek(offset int64, whence int) (int64, error) {
	if f.closed {
		return 0, &Error{Op: "seek", Path: f.path, Err: fs.ErrClosed}
	}

	var size int64
	if whence == io.SeekEnd {
		// Only need to get size for SeekEnd
		attr, errno := f.inst.fsBackend.GetAttr(f.nodeID)
		if errno != 0 {
			return 0, &Error{Op: "seek", Path: f.path, Err: errnoToError(errno)}
		}
		size = int64(attr.Size)
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
	if f.closed {
		return 0, &Error{Op: "readat", Path: f.path, Err: fs.ErrClosed}
	}

	data, errno := f.inst.fsBackend.Read(f.nodeID, f.fh, uint64(off), uint32(len(p)))
	if errno != 0 {
		return 0, &Error{Op: "readat", Path: f.path, Err: errnoToError(errno)}
	}

	copy(p, data)
	return len(data), nil
}

func (f *instanceFile) WriteAt(p []byte, off int64) (int, error) {
	return 0, &Error{Op: "writeat", Path: f.path, Err: fmt.Errorf("not yet implemented")}
}

func (f *instanceFile) Stat() (fs.FileInfo, error) {
	if f.closed {
		return nil, &Error{Op: "stat", Path: f.path, Err: fs.ErrClosed}
	}

	attr, errno := f.inst.fsBackend.GetAttr(f.nodeID)
	if errno != 0 {
		return nil, &Error{Op: "stat", Path: f.path, Err: errnoToError(errno)}
	}

	return fuseAttrToFileInfo(f.path, attr), nil
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

// fuseAttrToFileInfo converts a virtio.FuseAttr to fs.FileInfo.
func fuseAttrToFileInfo(name string, attr virtio.FuseAttr) fs.FileInfo {
	return &fuseFileInfo{
		name: path.Base(name),
		attr: attr,
	}
}

type fuseFileInfo struct {
	name string
	attr virtio.FuseAttr
}

func (fi *fuseFileInfo) Name() string {
	return fi.name
}

func (fi *fuseFileInfo) Size() int64 {
	return int64(fi.attr.Size)
}

func (fi *fuseFileInfo) Mode() fs.FileMode {
	// Convert Unix mode to Go FileMode
	mode := fs.FileMode(fi.attr.Mode & 0777)
	switch fi.attr.Mode & 0170000 {
	case 0040000: // S_IFDIR
		mode |= fs.ModeDir
	case 0120000: // S_IFLNK
		mode |= fs.ModeSymlink
	case 0060000: // S_IFBLK
		mode |= fs.ModeDevice
	case 0020000: // S_IFCHR
		mode |= fs.ModeDevice | fs.ModeCharDevice
	case 0010000: // S_IFIFO
		mode |= fs.ModeNamedPipe
	case 0140000: // S_IFSOCK
		mode |= fs.ModeSocket
	}
	return mode
}

func (fi *fuseFileInfo) ModTime() time.Time {
	return time.Unix(int64(fi.attr.MTimeSec), int64(fi.attr.MTimeNsec))
}

func (fi *fuseFileInfo) IsDir() bool {
	return (fi.attr.Mode & 0170000) == 0040000
}

func (fi *fuseFileInfo) Sys() any {
	return &fi.attr
}

// fuseDirEntry wraps FUSE directory entry data to implement fs.DirEntry.
type fuseDirEntry struct {
	name      string
	entryType uint8 // DT_* type from FUSE dirent
}

func (d *fuseDirEntry) Name() string {
	return d.name
}

func (d *fuseDirEntry) IsDir() bool {
	return d.entryType == 4 // DT_DIR
}

func (d *fuseDirEntry) Type() fs.FileMode {
	switch d.entryType {
	case 4: // DT_DIR
		return fs.ModeDir
	case 10: // DT_LNK
		return fs.ModeSymlink
	case 2: // DT_CHR
		return fs.ModeDevice | fs.ModeCharDevice
	case 6: // DT_BLK
		return fs.ModeDevice
	case 1: // DT_FIFO
		return fs.ModeNamedPipe
	case 12: // DT_SOCK
		return fs.ModeSocket
	default: // DT_REG (8) or unknown
		return 0
	}
}

func (d *fuseDirEntry) Info() (fs.FileInfo, error) {
	// Return minimal info; caller can use Stat for full details
	return &fuseDirEntryInfo{name: d.name, entryType: d.entryType}, nil
}

type fuseDirEntryInfo struct {
	name      string
	entryType uint8
}

func (di *fuseDirEntryInfo) Name() string { return di.name }
func (di *fuseDirEntryInfo) Size() int64  { return 0 }
func (di *fuseDirEntryInfo) Mode() fs.FileMode {
	return (&fuseDirEntry{entryType: di.entryType}).Type()
}
func (di *fuseDirEntryInfo) ModTime() time.Time { return time.Time{} }
func (di *fuseDirEntryInfo) IsDir() bool        { return di.entryType == 4 }
func (di *fuseDirEntryInfo) Sys() any           { return nil }

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
