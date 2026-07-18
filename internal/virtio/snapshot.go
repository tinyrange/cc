package virtio

import (
	"fmt"
	"io/fs"
	"os"
	"sort"
	"time"

	"j5.nz/cc/internal/imagefs"
)

func (p *imageFS) RootSnapshot() (imagefs.Directory, error) {
	if p == nil {
		return nil, fmt.Errorf("image filesystem is nil")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	root := p.nodes[1]
	if root == nil || !root.isDir() {
		return nil, fmt.Errorf("image filesystem root is missing")
	}
	return p.snapshotDirLocked(root)
}

func (p *imageFS) snapshotDirLocked(node *imageNode) (*snapshotDir, error) {
	if node.abstractDir != nil && !node.entriesDone {
		if _, errno := p.materializeDirEntriesLocked(node); errno != 0 {
			return nil, fmt.Errorf("materialize %s: errno %d", p.pathForNode(node.id), errno)
		}
	}
	attr := p.attr(node)
	out := &snapshotDir{
		mode:    linuxModeToGo(attr.Mode),
		uid:     attr.UID,
		gid:     attr.GID,
		rdev:    attr.RDev,
		modTime: unixAttrModTime(attr),
		entries: map[string]imagefs.Entry{},
	}
	names := make([]string, 0, len(node.entries))
	for name := range node.entries {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		child := p.nodes[node.entries[name]]
		if child == nil {
			continue
		}
		entry, err := p.snapshotEntryLocked(child)
		if err != nil {
			return nil, err
		}
		out.entries[name] = entry
	}
	return out, nil
}

func (p *imageFS) snapshotEntryLocked(node *imageNode) (imagefs.Entry, error) {
	attr := p.attr(node)
	switch {
	case node.isDir():
		dir, err := p.snapshotDirLocked(node)
		if err != nil {
			return imagefs.Entry{}, err
		}
		return imagefs.Entry{Dir: dir}, nil
	case node.isSymlink():
		target := node.symlinkTarget
		if node.abstractLink != nil {
			target = node.abstractLink.Target()
		}
		return imagefs.Entry{Symlink: &snapshotSymlink{
			mode:    linuxModeToGo(attr.Mode),
			uid:     attr.UID,
			gid:     attr.GID,
			rdev:    attr.RDev,
			target:  target,
			modTime: unixAttrModTime(attr),
		}}, nil
	default:
		var source imagefs.File
		var data sparseImageData
		if node.abstractFile != nil {
			source = node.abstractFile
		} else {
			data = make(sparseImageData, len(node.data))
			for pageIndex, page := range node.data {
				data[pageIndex] = append([]byte(nil), page...)
			}
		}
		return imagefs.Entry{File: &snapshotFile{
			mode:    linuxModeToGo(attr.Mode),
			uid:     attr.UID,
			gid:     attr.GID,
			rdev:    attr.RDev,
			size:    attr.Size,
			data:    data,
			source:  source,
			modTime: unixAttrModTime(attr),
		}}, nil
	}
}

func unixAttrModTime(attr FuseAttr) time.Time {
	if attr.MTimeSec == 0 && attr.MTimeNsec == 0 {
		return time.Unix(0, 0)
	}
	return time.Unix(int64(attr.MTimeSec), int64(attr.MTimeNsec))
}

type snapshotDir struct {
	mode    fs.FileMode
	uid     uint32
	gid     uint32
	rdev    uint32
	modTime time.Time
	entries map[string]imagefs.Entry
}

func (d *snapshotDir) Stat() fs.FileMode       { return d.mode }
func (d *snapshotDir) ModTime() time.Time      { return d.modTime }
func (d *snapshotDir) Owner() (uint32, uint32) { return d.uid, d.gid }
func (d *snapshotDir) RDev() uint32            { return d.rdev }
func (d *snapshotDir) ReadDir() ([]imagefs.DirEnt, error) {
	names := make([]string, 0, len(d.entries))
	for name := range d.entries {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]imagefs.DirEnt, 0, len(names))
	for _, name := range names {
		entry := d.entries[name]
		var mode fs.FileMode
		switch {
		case entry.Dir != nil:
			mode = fs.ModeDir | entry.Dir.Stat()
		case entry.Symlink != nil:
			mode = fs.ModeSymlink | entry.Symlink.Stat()
		case entry.File != nil:
			_, mode = entry.File.Stat()
		}
		out = append(out, imagefs.DirEnt{Name: name, Mode: mode})
	}
	return out, nil
}
func (d *snapshotDir) Lookup(name string) (imagefs.Entry, error) {
	entry, ok := d.entries[name]
	if !ok {
		return imagefs.Entry{}, os.ErrNotExist
	}
	return entry, nil
}

type snapshotFile struct {
	mode    fs.FileMode
	uid     uint32
	gid     uint32
	rdev    uint32
	size    uint64
	data    sparseImageData
	source  imagefs.File
	modTime time.Time
}

func (f *snapshotFile) Stat() (uint64, fs.FileMode) { return f.size, f.mode }
func (f *snapshotFile) ModTime() time.Time          { return f.modTime }
func (f *snapshotFile) Owner() (uint32, uint32)     { return f.uid, f.gid }
func (f *snapshotFile) RDev() uint32                { return f.rdev }
func (f *snapshotFile) ReadAt(off uint64, size uint32) ([]byte, error) {
	if off >= f.size || size == 0 {
		return nil, nil
	}
	end := off + uint64(size)
	if end > f.size {
		end = f.size
	}
	if f.source != nil {
		return f.source.ReadAt(off, uint32(end-off))
	}
	data := make([]byte, end-off)
	f.data.readAt(data, off)
	return data, nil
}

type snapshotSymlink struct {
	mode    fs.FileMode
	uid     uint32
	gid     uint32
	rdev    uint32
	target  string
	modTime time.Time
}

func (l *snapshotSymlink) Stat() fs.FileMode       { return l.mode }
func (l *snapshotSymlink) ModTime() time.Time      { return l.modTime }
func (l *snapshotSymlink) Target() string          { return l.target }
func (l *snapshotSymlink) Owner() (uint32, uint32) { return l.uid, l.gid }
func (l *snapshotSymlink) RDev() uint32            { return l.rdev }
