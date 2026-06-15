package imagefs

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"j5.nz/cc/internal/fsmeta"
	"j5.nz/cc/internal/linuxabi"
)

type File interface {
	Stat() (size uint64, mode fs.FileMode)
	ModTime() time.Time
	ReadAt(off uint64, size uint32) ([]byte, error)
	Owner() (uid, gid uint32)
	RDev() uint32
}

type OpenReaderFile interface {
	OpenReader() (io.ReaderAt, io.Closer, error)
}

type HardlinkFile interface {
	HardlinkKey() string
}

type Directory interface {
	Stat() fs.FileMode
	ModTime() time.Time
	ReadDir() ([]DirEnt, error)
	Lookup(name string) (Entry, error)
	Owner() (uid, gid uint32)
	RDev() uint32
}

type Symlink interface {
	Stat() fs.FileMode
	ModTime() time.Time
	Target() string
	Owner() (uid, gid uint32)
	RDev() uint32
}

type Entry struct {
	File    File
	Dir     Directory
	Symlink Symlink
}

type DirEnt struct {
	Name string
	Mode fs.FileMode
}

func NewHostFS(root string, meta map[string]fsmeta.Entry) Directory {
	rootMode := fs.ModeDir | 0o755
	rootUID := uint32(0)
	rootGID := uint32(0)
	rootRDev := uint32(0)
	if entry, ok := meta["/"]; ok {
		rootMode = linuxModeToGo(fsmeta.NormalizeLinuxMode(entry.Mode, fs.ModeDir|0o755))
		rootUID = entry.UID
		rootGID = entry.GID
		rootRDev = entry.RDev
	}
	rootModTime := time.Unix(0, 0)
	if info, err := os.Lstat(root); err == nil {
		rootModTime = info.ModTime()
	}
	return &hostDir{
		rootPath: root,
		hostPath: root,
		mode:     rootMode,
		uid:      rootUID,
		gid:      rootGID,
		rdev:     rootRDev,
		modTime:  rootModTime,
		meta:     meta,
	}
}

func LookupPath(root Directory, guestPath string) (Entry, error) {
	if root == nil {
		return Entry{}, fmt.Errorf("root filesystem is nil")
	}
	clean := path.Clean("/" + strings.TrimPrefix(strings.TrimSpace(guestPath), "/"))
	if clean == "/" {
		return Entry{Dir: root}, nil
	}
	current := root
	parts := strings.Split(strings.TrimPrefix(clean, "/"), "/")
	for i, part := range parts {
		entry, err := current.Lookup(part)
		if err != nil {
			return Entry{}, err
		}
		if i == len(parts)-1 {
			return entry, nil
		}
		if entry.Dir == nil {
			return Entry{}, fmt.Errorf("%q is not a directory", "/"+strings.Join(parts[:i+1], "/"))
		}
		current = entry.Dir
	}
	return Entry{}, fmt.Errorf("empty path")
}

func ResolveCommand(root Directory, command []string, env []string) ([]string, error) {
	if len(command) == 0 {
		return nil, fmt.Errorf("command is empty")
	}
	if strings.Contains(command[0], "/") {
		_, entry, err := ResolvePath(root, command[0])
		if err != nil {
			return nil, fmt.Errorf("resolve command %q: %w", command[0], err)
		}
		if entry.Dir != nil {
			return nil, fmt.Errorf("command %q is a directory", command[0])
		}
		if entry.File != nil {
			_, mode := entry.File.Stat()
			if mode&0o111 == 0 {
				return nil, fmt.Errorf("command %q is not executable", command[0])
			}
		}
		out := append([]string(nil), command...)
		out[0] = command[0]
		return out, nil
	}
	pathEnv := "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
	for _, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			pathEnv = strings.TrimPrefix(kv, "PATH=")
			break
		}
	}
	for _, dir := range strings.Split(pathEnv, ":") {
		if dir == "" {
			continue
		}
		guestPath := path.Join(dir, command[0])
		_, entry, err := ResolvePath(root, guestPath)
		if err != nil || entry.Dir != nil {
			continue
		}
		if entry.File != nil {
			_, mode := entry.File.Stat()
			if mode&0o111 != 0 {
				return append([]string{guestPath}, command[1:]...), nil
			}
		}
	}
	return nil, fmt.Errorf("resolve command %q in PATH", command[0])
}

func ResolvePath(root Directory, guestPath string) (string, Entry, error) {
	clean := path.Clean("/" + strings.TrimPrefix(strings.TrimSpace(guestPath), "/"))
	for depth := 0; depth < 40; depth++ {
		entry, err := LookupPath(root, clean)
		if err != nil {
			return "", Entry{}, err
		}
		if entry.Symlink == nil {
			return clean, entry, nil
		}
		target := fsmeta.NormalizeSymlinkTarget(strings.TrimSpace(entry.Symlink.Target()))
		if target == "" {
			return "", Entry{}, fmt.Errorf("%q symlink target is empty", clean)
		}
		if strings.HasPrefix(target, "/") {
			clean = path.Clean(target)
		} else {
			clean = path.Clean(path.Join(path.Dir(clean), target))
		}
	}
	return "", Entry{}, fmt.Errorf("%q has too many symlink levels", guestPath)
}

type hostFile struct {
	hostPath string
	mode     fs.FileMode
	uid      uint32
	gid      uint32
	rdev     uint32
	size     uint64
	modTime  time.Time
}

type hostDir struct {
	rootPath string
	hostPath string
	mode     fs.FileMode
	uid      uint32
	gid      uint32
	rdev     uint32
	modTime  time.Time
	meta     map[string]fsmeta.Entry
}

type hostSymlink struct {
	hostPath string
	mode     fs.FileMode
	uid      uint32
	gid      uint32
	rdev     uint32
	target   string
	modTime  time.Time
}

func (f *hostFile) Stat() (uint64, fs.FileMode) { return f.size, f.mode }
func (f *hostFile) ModTime() time.Time          { return f.modTime }
func (f *hostFile) Owner() (uint32, uint32)     { return f.uid, f.gid }
func (f *hostFile) RDev() uint32                { return f.rdev }
func (f *hostFile) OpenReader() (io.ReaderAt, io.Closer, error) {
	file, err := os.Open(f.hostPath)
	if err != nil {
		return nil, nil, err
	}
	return file, file, nil
}
func (f *hostFile) ReadAt(off uint64, size uint32) ([]byte, error) {
	file, err := os.Open(f.hostPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	buf := make([]byte, size)
	n, err := file.ReadAt(buf, int64(off))
	if err != nil && err != io.EOF {
		return nil, err
	}
	return buf[:n], nil
}

func (d *hostDir) Stat() fs.FileMode       { return d.mode & linuxPermMask }
func (d *hostDir) ModTime() time.Time      { return d.modTime }
func (d *hostDir) Owner() (uint32, uint32) { return d.uid, d.gid }
func (d *hostDir) RDev() uint32            { return d.rdev }
func (d *hostDir) ReadDir() ([]DirEnt, error) {
	entries, err := os.ReadDir(d.hostPath)
	if err != nil {
		return nil, err
	}
	out := make([]DirEnt, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		out = append(out, DirEnt{Name: entry.Name(), Mode: info.Mode()})
	}
	return out, nil
}

func (d *hostDir) Lookup(name string) (Entry, error) {
	host := filepath.Join(d.hostPath, filepath.FromSlash(name))
	info, err := os.Lstat(host)
	if err != nil {
		return Entry{}, err
	}
	rel, err := filepath.Rel(d.rootPath, host)
	if err != nil {
		return Entry{}, err
	}
	guest := fsmeta.Normalize(filepath.ToSlash(rel))
	meta := d.meta[guest]
	mode := linuxModeToGo(fsmeta.NormalizeLinuxMode(meta.Mode, info.Mode()))
	modTime := info.ModTime()
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(host)
		if err != nil {
			return Entry{}, err
		}
		if meta.LinkTarget != "" {
			target = meta.LinkTarget
		}
		target = fsmeta.NormalizeSymlinkTarget(target)
		return Entry{Symlink: &hostSymlink{hostPath: host, mode: mode, uid: meta.UID, gid: meta.GID, rdev: meta.RDev, target: target, modTime: modTime}}, nil
	case info.IsDir():
		return Entry{Dir: &hostDir{rootPath: d.rootPath, hostPath: host, mode: mode, uid: meta.UID, gid: meta.GID, rdev: meta.RDev, modTime: modTime, meta: d.meta}}, nil
	default:
		return Entry{File: &hostFile{hostPath: host, mode: mode, uid: meta.UID, gid: meta.GID, rdev: meta.RDev, size: uint64(info.Size()), modTime: modTime}}, nil
	}
}

func (l *hostSymlink) Stat() fs.FileMode       { return l.mode & linuxPermMask }
func (l *hostSymlink) ModTime() time.Time      { return l.modTime }
func (l *hostSymlink) Target() string          { return l.target }
func (l *hostSymlink) Owner() (uint32, uint32) { return l.uid, l.gid }
func (l *hostSymlink) RDev() uint32            { return l.rdev }

const (
	linuxSIFMT    = linuxabi.SIFMT
	linuxSIFSOCK  = linuxabi.SIFSOCK
	linuxSIFLNK   = linuxabi.SIFLNK
	linuxSIFREG   = linuxabi.SIFREG
	linuxSIFBLK   = linuxabi.SIFBLK
	linuxSIFDIR   = linuxabi.SIFDIR
	linuxSIFCHR   = linuxabi.SIFCHR
	linuxSIFIFO   = linuxabi.SIFIFO
	linuxPermMask = linuxabi.PermMask
)

func linuxModeToGo(mode uint32) fs.FileMode {
	perm := fs.FileMode(mode & linuxPermMask)
	switch mode & linuxSIFMT {
	case linuxSIFDIR:
		perm |= fs.ModeDir
	case linuxSIFLNK:
		perm |= fs.ModeSymlink
	case linuxSIFIFO:
		perm |= fs.ModeNamedPipe
	case linuxSIFCHR:
		perm |= fs.ModeDevice | fs.ModeCharDevice
	case linuxSIFBLK:
		perm |= fs.ModeDevice
	case linuxSIFSOCK:
		perm |= fs.ModeSocket
	}
	return perm
}
