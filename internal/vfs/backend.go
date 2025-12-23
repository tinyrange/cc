package vfs

import (
	"encoding/binary"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/tinyrange/cc/internal/devices/virtio"
	linux "github.com/tinyrange/cc/internal/linux/defs/amd64"
)

// AbstractFile represents a file with custom read/write operations.
// Implement this interface to provide files backed by host files, network resources, etc.
type AbstractFile interface {
	// Stat returns the file size and mode.
	Stat() (size uint64, mode fs.FileMode)
	// ModTime returns the file's modified time.
	ModTime() time.Time
	// ReadAt reads up to size bytes starting at offset off.
	ReadAt(off uint64, size uint32) ([]byte, error)
	// WriteAt writes data at offset off. Returns error if not writable.
	WriteAt(off uint64, data []byte) error
	// Truncate sets the file size. Returns error if not supported.
	Truncate(size uint64) error
}

// AbstractDir represents a directory with custom listing and lookup.
// Implement this interface to provide directories backed by host directories, etc.
type AbstractDir interface {
	// Stat returns the directory mode.
	Stat() (mode fs.FileMode)
	// ModTime returns the directory's modified time.
	ModTime() time.Time
	// ReadDir returns all entries in the directory.
	ReadDir() ([]AbstractDirEntry, error)
	// Lookup returns the entry for the given name, or nil if not found.
	Lookup(name string) (AbstractEntry, error)
}

// AbstractDirEntry describes an entry returned by AbstractDir.ReadDir.
type AbstractDirEntry struct {
	Name  string
	IsDir bool
	Mode  fs.FileMode
	Size  uint64
}

// AbstractEntry is returned by AbstractDir.Lookup.
// Exactly one of File or Dir should be non-nil.
type AbstractEntry struct {
	File AbstractFile
	Dir  AbstractDir
}

type virtioFsBackend struct {
	mu      sync.Mutex
	nodes   map[uint64]*fsNode
	handles map[uint64]uint64
	nextID  uint64
	nextFH  uint64
}

const (
	virtioFsRootNodeID = 1
)

type fsExtent struct {
	off  uint64
	data []byte
}

type fsNode struct {
	id      uint64
	name    string
	parent  uint64
	mode    fs.FileMode
	size    uint64
	extents []fsExtent
	entries map[string]uint64
	xattr   map[string][]byte
	modTime time.Time

	symlinkTarget string

	// Abstract backing - if set, delegates to these instead of in-memory storage.
	abstractFile AbstractFile
	abstractDir  AbstractDir
}

func newDirNode(id uint64, name string, parent uint64, perm fs.FileMode) *fsNode {
	return &fsNode{
		id:      id,
		name:    name,
		parent:  parent,
		mode:    fs.ModeDir | perm,
		entries: make(map[string]uint64),
		xattr:   make(map[string][]byte),
		modTime: time.Now(),
	}
}

func newFileNode(id uint64, name string, parent uint64, perm fs.FileMode) *fsNode {
	return &fsNode{
		id:      id,
		name:    name,
		parent:  parent,
		mode:    perm,
		xattr:   make(map[string][]byte),
		modTime: time.Now(),
	}
}

func (n *fsNode) isDir() bool {
	if n.abstractDir != nil {
		return true
	}
	return n.mode.IsDir()
}

func (n *fsNode) isSymlink() bool {
	return n.mode&fs.ModeSymlink != 0
}

func (n *fsNode) blockUsage() uint64 {
	if n.abstractFile != nil {
		size, _ := n.abstractFile.Stat()
		if size == 0 {
			return 0
		}
		return (size + 511) / 512
	}
	var used uint64
	for _, e := range n.extents {
		used += uint64(len(e.data))
	}
	if used == 0 && n.size > 0 {
		return 1
	}
	return (used + 511) / 512
}

func (n *fsNode) attr() virtio.FuseAttr {
	var perm fs.FileMode
	var size uint64
	modTime := n.modTime

	if n.abstractFile != nil {
		size, perm = n.abstractFile.Stat()
		if mt := n.abstractFile.ModTime(); !mt.IsZero() {
			modTime = mt
		}
	} else if n.abstractDir != nil {
		perm = n.abstractDir.Stat()
		if mt := n.abstractDir.ModTime(); !mt.IsZero() {
			modTime = mt
		}
	} else if n.isSymlink() {
		perm = n.mode.Perm()
		size = uint64(len(n.symlinkTarget))
	} else {
		perm = n.mode.Perm()
		size = n.size
	}

	if modTime.IsZero() {
		modTime = time.Unix(0, 0)
	}

	mode := uint32(perm)
	switch {
	case n.isDir():
		mode |= linux.S_IFDIR
	case n.isSymlink():
		mode |= linux.S_IFLNK
	default:
		mode |= linux.S_IFREG
	}

	nlink := uint32(1)
	if n.isDir() {
		entryCount := len(n.entries)
		if n.abstractDir != nil {
			if entries, err := n.abstractDir.ReadDir(); err == nil {
				entryCount = len(entries)
			}
		}
		nlink = 2 + uint32(entryCount)
	}

	sec := uint64(modTime.Unix())
	nsec := uint32(modTime.Nanosecond())

	return virtio.FuseAttr{
		Ino:       n.id,
		Mode:      mode,
		Size:      size,
		NLink:     nlink,
		UID:       0,
		GID:       0,
		Blocks:    n.blockUsage(),
		BlkSize:   4096,
		ATimeSec:  sec,
		ATimeNsec: nsec,
		MTimeSec:  sec,
		MTimeNsec: nsec,
		CTimeSec:  sec,
		CTimeNsec: nsec,
	}
}

func (v *virtioFsBackend) ensureRoot() {
	if v.nodes != nil {
		return
	}
	v.nodes = make(map[uint64]*fsNode)
	root := newDirNode(virtioFsRootNodeID, "", 0, 0o755)
	v.nodes[root.id] = root
	v.handles = make(map[uint64]uint64)
	v.nextID = virtioFsRootNodeID + 1
	v.nextFH = 1
}

func (v *virtioFsBackend) node(id uint64) (*fsNode, int32) {
	n, ok := v.nodes[id]
	if !ok {
		return nil, -int32(linux.ENOENT)
	}
	return n, 0
}

func cleanName(name string) string {
	name = strings.TrimPrefix(path.Clean(name), "/")
	if name == "." {
		return ""
	}
	return name
}

func nameErr(name string) int32 {
	if len(name) > 255 {
		return -errNameTooLong
	}
	return 0
}

func (v *virtioFsBackend) child(parent *fsNode, name string) (*fsNode, int32) {
	if !parent.isDir() {
		return nil, -int32(linux.ENOTDIR)
	}
	if name == "" {
		return parent, 0
	}
	if name == ".." {
		if parent.parent == 0 {
			return parent, 0
		}
		return v.nodes[parent.parent], 0
	}
	if e := nameErr(name); e != 0 {
		return nil, e
	}
	// First check in-memory entries
	id, ok := parent.entries[name]
	if ok {
		return v.nodes[id], 0
	}
	// If parent has an abstract directory, try looking up there
	if parent.abstractDir != nil {
		entry, err := parent.abstractDir.Lookup(name)
		if err != nil {
			return nil, -int32(linux.ENOENT)
		}
		// Create a new node backed by the abstract entry
		return v.createAbstractNode(parent, name, entry)
	}
	return nil, -int32(linux.ENOENT)
}

// createAbstractNode creates an fsNode backed by an AbstractEntry.
func (v *virtioFsBackend) createAbstractNode(parent *fsNode, name string, entry AbstractEntry) (*fsNode, int32) {
	id := v.nextID
	v.nextID++

	modTime := time.Now()
	node := &fsNode{
		id:     id,
		name:   name,
		parent: parent.id,
		xattr:  make(map[string][]byte),
	}

	if entry.Dir != nil {
		node.abstractDir = entry.Dir
		node.mode = fs.ModeDir | entry.Dir.Stat()
		node.entries = make(map[string]uint64)
		if mt := entry.Dir.ModTime(); !mt.IsZero() {
			modTime = mt
		}
	} else if entry.File != nil {
		node.abstractFile = entry.File
		size, mode := entry.File.Stat()
		node.mode = mode
		node.size = size
		if mt := entry.File.ModTime(); !mt.IsZero() {
			modTime = mt
		}
	} else {
		return nil, -int32(linux.ENOENT)
	}

	node.modTime = modTime
	v.nodes[id] = node
	// Cache in parent's entries for future lookups
	if parent.entries == nil {
		parent.entries = make(map[string]uint64)
	}
	parent.entries[name] = id
	return node, 0
}

func (n *fsNode) read(off uint64, size uint32) ([]byte, error) {
	if n.abstractFile != nil {
		return n.abstractFile.ReadAt(off, size)
	}
	buf := make([]byte, size)
	end := off + uint64(size)
	for _, e := range n.extents {
		eEnd := e.off + uint64(len(e.data))
		if eEnd <= off || e.off >= end {
			continue
		}
		start := max64(off, e.off)
		stop := min64(end, eEnd)
		copy(buf[start-off:stop-off], e.data[start-e.off:stop-e.off])
	}
	return buf, nil
}

func mergeExtents(extents []fsExtent) []fsExtent {
	if len(extents) == 0 {
		return extents
	}
	sort.Slice(extents, func(i, j int) bool { return extents[i].off < extents[j].off })
	out := []fsExtent{extents[0]}
	for i := 1; i < len(extents); i++ {
		last := &out[len(out)-1]
		cur := extents[i]
		lastEnd := last.off + uint64(len(last.data))
		if cur.off <= lastEnd {
			overlap := int(lastEnd - cur.off)
			if overlap < len(cur.data) {
				last.data = append(last.data, cur.data[overlap:]...)
			}
			continue
		}
		out = append(out, cur)
	}
	return out
}

func (n *fsNode) write(off uint64, data []byte) error {
	if n.abstractFile != nil {
		return n.abstractFile.WriteAt(off, data)
	}
	if len(data) == 0 {
		return nil
	}
	n.extents = mergeExtents(append(n.extents, fsExtent{off: off, data: append([]byte(nil), data...)}))
	if off+uint64(len(data)) > n.size {
		n.size = off + uint64(len(data))
	}
	n.modTime = time.Now()
	return nil
}

func (n *fsNode) truncate(size uint64) error {
	if n.abstractFile != nil {
		return n.abstractFile.Truncate(size)
	}
	if size >= n.size {
		n.size = size
		n.modTime = time.Now()
		return nil
	}
	var kept []fsExtent
	for _, e := range n.extents {
		if e.off >= size {
			continue
		}
		if e.off+uint64(len(e.data)) > size {
			e.data = e.data[:size-e.off]
		}
		kept = append(kept, e)
	}
	n.extents = kept
	n.size = size
	n.modTime = time.Now()
	return nil
}

func errno(code int32) int32 { return -code }

func max64(a, b uint64) uint64 {
	if a > b {
		return a
	}
	return b
}

func min64(a, b uint64) uint64 {
	if a < b {
		return a
	}
	return b
}

const (
	errNameTooLong = int32(36)
	errNoData      = int32(61)
	errNotEmpty    = int32(39)
)

var loggedLseek bool

// GetAttr implements virtio.FsBackend.
func (v *virtioFsBackend) GetAttr(nodeID uint64) (attr virtio.FuseAttr, errno int32) {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.ensureRoot()
	n, err := v.node(nodeID)
	if err != 0 {
		return virtio.FuseAttr{}, err
	}
	return n.attr(), 0
}

// Init implements virtio.FsBackend.
func (v *virtioFsBackend) Init() (maxWrite uint32, flags uint32) {
	v.ensureRoot()
	return 128 * 1024, 0
}

// Lookup implements virtio.FsBackend.
func (v *virtioFsBackend) Lookup(parent uint64, name string) (nodeID uint64, attr virtio.FuseAttr, errno int32) {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.ensureRoot()
	parentNode, err := v.node(parent)
	if err != 0 {
		return 0, virtio.FuseAttr{}, err
	}
	child, errno := v.child(parentNode, cleanName(name))
	if errno != 0 {
		return 0, virtio.FuseAttr{}, errno
	}
	return child.id, child.attr(), 0
}

// Open implements virtio.FsBackend.
func (v *virtioFsBackend) Open(nodeID uint64, flags uint32) (fh uint64, errno int32) {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.ensureRoot()
	n, err := v.node(nodeID)
	if err != 0 {
		return 0, err
	}
	if n.isDir() {
		return 0, -int32(linux.EISDIR)
	}
	if flags&uint32(os.O_TRUNC) != 0 {
		if err := n.truncate(0); err != nil {
			return 0, -int32(linux.EIO)
		}
	}
	fh = v.nextFH
	v.nextFH++
	v.handles[fh] = n.id
	return fh, 0
}

// Read implements virtio.FsBackend.
func (v *virtioFsBackend) Read(nodeID uint64, fh uint64, off uint64, size uint32) ([]byte, int32) {
	v.mu.Lock()
	defer v.mu.Unlock()

	nid, ok := v.handles[fh]
	if !ok || nid != nodeID {
		return nil, -int32(linux.EBADF)
	}
	n, err := v.node(nid)
	if err != 0 {
		return nil, err
	}
	// Get actual size (from abstract file or in-memory)
	nodeSize := n.size
	if n.abstractFile != nil {
		nodeSize, _ = n.abstractFile.Stat()
	}
	if off >= nodeSize {
		return []byte{}, 0
	}
	if off+uint64(size) > nodeSize {
		size = uint32(nodeSize - off)
	}
	data, readErr := n.read(off, size)
	if readErr != nil {
		return nil, -int32(linux.EIO)
	}
	return data, 0
}

// ReadDir implements virtio.FsBackend.
func (v *virtioFsBackend) ReadDir(nodeID uint64, off uint64, maxBytes uint32) ([]byte, int32) {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.ensureRoot()
	dirNode, err := v.node(nodeID)
	if err != 0 {
		return nil, err
	}
	if !dirNode.isDir() {
		return nil, -int32(linux.ENOTDIR)
	}

	// Collect all entry names, avoiding duplicates
	nameSet := make(map[string]bool)
	for name := range dirNode.entries {
		nameSet[name] = true
	}
	// Also include entries from abstract directory if present
	if dirNode.abstractDir != nil {
		if abstractEntries, err := dirNode.abstractDir.ReadDir(); err == nil {
			for _, entry := range abstractEntries {
				nameSet[entry.Name] = true
			}
		}
	}

	names := make([]string, 0, len(nameSet)+2)
	if off == 0 {
		names = append(names, ".", "..")
	}
	for name := range nameSet {
		names = append(names, name)
	}
	sort.Strings(names)

	var buf []byte
	for idx, name := range names {
		if uint64(idx) < off {
			continue
		}
		typ := linux.DT_DIR
		id := dirNode.id
		if name == "." {
			id = dirNode.id
		} else if name == ".." {
			if dirNode.parent != 0 {
				id = dirNode.parent
			}
		} else {
			// Try to get from cached entries first
			if cachedID, ok := dirNode.entries[name]; ok {
				id = cachedID
				child, _ := v.node(id)
			if child != nil {
				if child.isDir() {
					typ = linux.DT_DIR
				} else if child.isSymlink() {
					typ = linux.DT_LNK
				} else {
					typ = linux.DT_REG
				}
				}
			} else if dirNode.abstractDir != nil {
				// Look up in abstract directory
				if entry, lookupErr := dirNode.abstractDir.Lookup(name); lookupErr == nil {
					// Determine type from abstract entry
					if entry.File != nil {
						typ = linux.DT_REG
					}
					// Create node to get an ID
					node, _ := v.createAbstractNode(dirNode, name, entry)
					if node != nil {
						id = node.id
					}
				}
			}
		}
		dirent := buildFuseDirent(id, name, uint32(typ), uint64(len(buf))+1)
		buf = append(buf, dirent...)
		if uint32(len(buf)) >= maxBytes {
			break
		}
	}

	return buf, 0
}

// Release implements virtio.FsBackend.
func (v *virtioFsBackend) Release(nodeID uint64, fh uint64) {
	v.mu.Lock()
	defer v.mu.Unlock()

	delete(v.handles, fh)
}

// StatFS implements virtio.FsBackend.
func (v *virtioFsBackend) StatFS(nodeID uint64) (blocks uint64, bfree uint64, bavail uint64, files uint64, ffree uint64, bsize uint64, frsize uint64, namelen uint64, errno int32) {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.ensureRoot()
	_ = nodeID
	total := uint64(1024)
	free := uint64(900)
	return total, free, free, total, free, 4096, 4096, 255, 0
}

var (
	_ virtio.FsBackend = &virtioFsBackend{}
)

// Create implements FUSE_CREATE semantics.
func (v *virtioFsBackend) Create(parent uint64, name string, mode uint32, flags uint32, umask uint32) (nodeID uint64, fh uint64, attr virtio.FuseAttr, errno int32) {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.ensureRoot()
	parentNode, err := v.node(parent)
	if err != 0 {
		return 0, 0, virtio.FuseAttr{}, err
	}
	if !parentNode.isDir() {
		return 0, 0, virtio.FuseAttr{}, -int32(linux.ENOTDIR)
	}
	clean := cleanName(name)
	if clean == "" {
		return 0, 0, virtio.FuseAttr{}, -int32(linux.EINVAL)
	}
	if e := nameErr(clean); e != 0 {
		return 0, 0, virtio.FuseAttr{}, e
	}
	if _, exists := parentNode.entries[clean]; exists && flags&uint32(os.O_EXCL) != 0 {
		return 0, 0, virtio.FuseAttr{}, -int32(linux.EEXIST)
	}
	if existingID, ok := parentNode.entries[clean]; ok {
		existing := v.nodes[existingID]
		if flags&uint32(os.O_TRUNC) != 0 {
			if err := existing.truncate(0); err != nil {
				return 0, 0, virtio.FuseAttr{}, -int32(linux.EIO)
			}
		}
		fh = v.nextFH
		v.nextFH++
		v.handles[fh] = existing.id
		return existing.id, fh, existing.attr(), 0
	}

	id := v.nextID
	v.nextID++
	perm := fs.FileMode(mode&^umask) & 0777
	node := newFileNode(id, clean, parentNode.id, perm)
	parentNode.entries[clean] = id
	if parentNode.abstractDir == nil {
		parentNode.modTime = time.Now()
	}
	v.nodes[id] = node

	fh = v.nextFH
	v.nextFH++
	v.handles[fh] = id
	return id, fh, node.attr(), 0
}

func (v *virtioFsBackend) Mkdir(parent uint64, name string, mode uint32, umask uint32) (nodeID uint64, attr virtio.FuseAttr, errno int32) {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.ensureRoot()
	parentNode, err := v.node(parent)
	if err != 0 {
		return 0, virtio.FuseAttr{}, err
	}
	if !parentNode.isDir() {
		return 0, virtio.FuseAttr{}, -int32(linux.ENOTDIR)
	}
	clean := cleanName(name)
	if clean == "" {
		return 0, virtio.FuseAttr{}, -int32(linux.EINVAL)
	}
	if e := nameErr(clean); e != 0 {
		return 0, virtio.FuseAttr{}, e
	}
	if _, exists := parentNode.entries[clean]; exists {
		return 0, virtio.FuseAttr{}, -int32(linux.EEXIST)
	}
	id := v.nextID
	v.nextID++
	perm := fs.FileMode(mode&^umask) & 0777
	node := newDirNode(id, clean, parentNode.id, perm)
	parentNode.entries[clean] = id
	if parentNode.abstractDir == nil {
		parentNode.modTime = time.Now()
	}
	v.nodes[id] = node
	return id, node.attr(), 0
}

func (v *virtioFsBackend) Mknod(parent uint64, name string, mode uint32, _ uint32, _ uint32) (nodeID uint64, attr virtio.FuseAttr, errno int32) {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.ensureRoot()
	parentNode, err := v.node(parent)
	if err != 0 {
		return 0, virtio.FuseAttr{}, err
	}
	if !parentNode.isDir() {
		return 0, virtio.FuseAttr{}, -int32(linux.ENOTDIR)
	}
	clean := cleanName(name)
	if clean == "" {
		return 0, virtio.FuseAttr{}, -int32(linux.EINVAL)
	}
	if e := nameErr(clean); e != 0 {
		return 0, virtio.FuseAttr{}, e
	}
	if _, exists := parentNode.entries[clean]; exists {
		return 0, virtio.FuseAttr{}, -int32(linux.EEXIST)
	}
	id := v.nextID
	v.nextID++
	perm := fs.FileMode(mode & 0777)
	node := newFileNode(id, clean, parentNode.id, perm)
	parentNode.entries[clean] = id
	if parentNode.abstractDir == nil {
		parentNode.modTime = time.Now()
	}
	v.nodes[id] = node
	return id, node.attr(), 0
}

func (v *virtioFsBackend) Write(nodeID uint64, fh uint64, off uint64, data []byte) (uint32, int32) {
	v.mu.Lock()
	defer v.mu.Unlock()

	nid, ok := v.handles[fh]
	if !ok || nid != nodeID {
		return 0, -int32(linux.EBADF)
	}
	n, err := v.node(nid)
	if err != 0 {
		return 0, err
	}
	if n.isDir() {
		return 0, -int32(linux.EISDIR)
	}
	if writeErr := n.write(off, data); writeErr != nil {
		return 0, -int32(linux.EIO)
	}
	return uint32(len(data)), 0
}

func (v *virtioFsBackend) Lseek(nodeID uint64, fh uint64, offset uint64, whence uint32) (uint64, int32) {
	v.mu.Lock()
	defer v.mu.Unlock()

	nid, ok := v.handles[fh]
	if !ok || nid != nodeID {
		return 0, -int32(linux.EBADF)
	}
	n, err := v.node(nid)
	if err != 0 {
		return 0, err
	}
	if n.isDir() {
		return 0, -int32(linux.EISDIR)
	}
	if !loggedLseek {
		slog.Info("virtiofs lseek", "node", nid, "offset", offset, "whence", whence)
		loggedLseek = true
	}
	ext := n.extents
	switch whence {
	case uint32(linux.SEEK_DATA):
		if offset >= n.size || len(ext) == 0 {
			return 0, -int32(linux.ENXIO)
		}
		for _, e := range ext {
			eEnd := e.off + uint64(len(e.data))
			if offset < e.off {
				return e.off, 0
			}
			if offset >= e.off && offset < eEnd {
				return offset, 0
			}
		}
		return 0, -int32(linux.ENXIO)
	case uint32(linux.SEEK_HOLE):
		if offset >= n.size {
			return offset, 0
		}
		for _, e := range ext {
			eEnd := e.off + uint64(len(e.data))
			if offset < e.off {
				return offset, 0
			}
			if offset >= e.off && offset < eEnd {
				return eEnd, 0
			}
		}
		return n.size, 0
	default:
		return 0, -int32(linux.EINVAL)
	}
}

func (v *virtioFsBackend) SetXattr(nodeID uint64, name string, value []byte, flags uint32) int32 {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.ensureRoot()
	n, err := v.node(nodeID)
	if err != 0 {
		return err
	}
	if n.xattr == nil {
		n.xattr = make(map[string][]byte)
	}
	if flags&uint32(linux.XATTR_CREATE) != 0 {
		if _, exists := n.xattr[name]; exists {
			return -int32(linux.EEXIST)
		}
	}
	if flags&uint32(linux.XATTR_REPLACE) != 0 {
		if _, exists := n.xattr[name]; !exists {
			return -errNoData
		}
	}
	n.xattr[name] = append([]byte(nil), value...)
	return 0
}

func (v *virtioFsBackend) GetXattr(nodeID uint64, name string) ([]byte, int32) {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.ensureRoot()
	n, err := v.node(nodeID)
	if err != 0 {
		return nil, err
	}
	val, ok := n.xattr[name]
	if !ok {
		return nil, -errNoData
	}
	return append([]byte(nil), val...), 0
}

func (v *virtioFsBackend) Rename(oldParent uint64, oldName string, newParent uint64, newName string, flags uint32) int32 {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.ensureRoot()
	srcParent, err := v.node(oldParent)
	if err != 0 {
		return err
	}
	dstParent, err2 := v.node(newParent)
	if err2 != 0 {
		return err2
	}
	srcName := cleanName(oldName)
	dstName := cleanName(newName)
	if e := nameErr(dstName); e != 0 {
		return e
	}
	if !srcParent.isDir() || !dstParent.isDir() {
		return -int32(linux.ENOTDIR)
	}
	srcID, ok := srcParent.entries[srcName]
	if !ok {
		slog.Warn("virtiofs rename missing source", "parent", oldParent, "name", srcName)
		return -int32(linux.ENOENT)
	}
	// Prevent overwriting non-empty dir
	if dstID, exists := dstParent.entries[dstName]; exists {
		dstNode := v.nodes[dstID]
		if dstNode.isDir() && len(dstNode.entries) > 0 {
			return -errNotEmpty
		}
	}
	dstParent.entries[dstName] = srcID
	delete(srcParent.entries, srcName)
	node := v.nodes[srcID]
	node.parent = dstParent.id
	node.name = dstName
	now := time.Now()
	if srcParent.abstractDir == nil {
		srcParent.modTime = now
	}
	if dstParent.abstractDir == nil {
		dstParent.modTime = now
	}
	if node.abstractFile == nil && node.abstractDir == nil {
		node.modTime = now
	}
	return 0
}

func (v *virtioFsBackend) Unlink(parent uint64, name string) int32 {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.ensureRoot()
	parentNode, err := v.node(parent)
	if err != 0 {
		return err
	}
	if !parentNode.isDir() {
		return -int32(linux.ENOTDIR)
	}
	clean := cleanName(name)
	if e := nameErr(clean); e != 0 {
		return e
	}
	id, ok := parentNode.entries[clean]
	if !ok {
		return -int32(linux.ENOENT)
	}
	node := v.nodes[id]
	if node.isDir() {
		return -int32(linux.EISDIR)
	}
	delete(parentNode.entries, clean)
	if parentNode.abstractDir == nil {
		parentNode.modTime = time.Now()
	}
	delete(v.nodes, id)
	return 0
}

func (v *virtioFsBackend) Rmdir(parent uint64, name string) int32 {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.ensureRoot()
	parentNode, err := v.node(parent)
	if err != 0 {
		return err
	}
	if !parentNode.isDir() {
		return -int32(linux.ENOTDIR)
	}
	clean := cleanName(name)
	if e := nameErr(clean); e != 0 {
		return e
	}
	id, ok := parentNode.entries[clean]
	if !ok {
		return -int32(linux.ENOENT)
	}
	node := v.nodes[id]
	if !node.isDir() {
		return -int32(linux.ENOTDIR)
	}
	if len(node.entries) > 0 {
		return -errNotEmpty
	}
	delete(parentNode.entries, clean)
	if parentNode.abstractDir == nil {
		parentNode.modTime = time.Now()
	}
	delete(v.nodes, id)
	return 0
}

func (v *virtioFsBackend) SetAttr(nodeID uint64, size *uint64, mode *uint32) int32 {
	v.mu.Lock()
	defer v.mu.Unlock()

	if size == nil && mode == nil {
		return 0
	}
	v.ensureRoot()
	n, err := v.node(nodeID)
	if err != 0 {
		return err
	}
	if size != nil {
		if truncErr := n.truncate(*size); truncErr != nil {
			return -int32(linux.EIO)
		}
	}
	if mode != nil {
		// Preserve file type bits (directory, symlink, etc.) and only update permission bits
		oldType := n.mode &^ 0777
		newPerm := fs.FileMode(*mode) & 0777
		n.mode = oldType | newPerm
		n.modTime = time.Now()
	}
	return 0
}

func (v *virtioFsBackend) Symlink(parent uint64, name string, target string, _ uint32) (nodeID uint64, attr virtio.FuseAttr, errno int32) {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.ensureRoot()
	parentNode, err := v.node(parent)
	if err != 0 {
		return 0, virtio.FuseAttr{}, err
	}
	if !parentNode.isDir() {
		return 0, virtio.FuseAttr{}, -int32(linux.ENOTDIR)
	}
	clean := cleanName(name)
	if clean == "" {
		return 0, virtio.FuseAttr{}, -int32(linux.EINVAL)
	}
	if e := nameErr(clean); e != 0 {
		return 0, virtio.FuseAttr{}, e
	}
	if _, exists := parentNode.entries[clean]; exists {
		return 0, virtio.FuseAttr{}, -int32(linux.EEXIST)
	}

	id := v.nextID
	v.nextID++
	node := &fsNode{
		id:            id,
		name:          clean,
		parent:        parentNode.id,
		mode:          fs.ModeSymlink | 0o777,
		size:          uint64(len(target)),
		xattr:         make(map[string][]byte),
		modTime:       time.Now(),
		symlinkTarget: target,
	}
	parentNode.entries[clean] = id
	if parentNode.abstractDir == nil {
		parentNode.modTime = time.Now()
	}
	v.nodes[id] = node
	return id, node.attr(), 0
}

func (v *virtioFsBackend) Readlink(nodeID uint64) (target string, errno int32) {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.ensureRoot()
	n, err := v.node(nodeID)
	if err != 0 {
		return "", err
	}
	if !n.isSymlink() {
		return "", -int32(linux.EINVAL)
	}
	return n.symlinkTarget, 0
}

func buildFuseDirent(ino uint64, name string, typ uint32, nextOffset uint64) []byte {
	// struct fuse_dirent { uint64 ino; uint64 off; uint32 namelen; uint32 type; char name[]; }
	const headerSize = 8 + 8 + 4 + 4
	namelen := len(name)
	recordLen := headerSize + namelen
	alignedLen := (recordLen + 7) &^ 7

	buf := make([]byte, alignedLen)
	binary.LittleEndian.PutUint64(buf[0:8], ino)
	binary.LittleEndian.PutUint64(buf[8:16], nextOffset)
	binary.LittleEndian.PutUint32(buf[16:20], uint32(namelen))
	binary.LittleEndian.PutUint32(buf[20:24], typ)
	copy(buf[24:], []byte(name))
	return buf
}

func NewVirtioFsBackend() virtio.FsBackend {
	return &virtioFsBackend{}
}

// VirtioFsBackend provides additional methods beyond the FsBackend interface.
type VirtioFsBackend interface {
	virtio.FsBackend
	AddAbstractFile(filePath string, file AbstractFile) error
	AddAbstractDir(dirPath string, dir AbstractDir) error
	SetAbstractRoot(dir AbstractDir) error
}

// AddAbstractFile adds an abstract file at the specified path.
// Parent directories are created as needed.
func (v *virtioFsBackend) AddAbstractFile(filePath string, file AbstractFile) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.ensureRoot()

	filePath = cleanName(filePath)
	if filePath == "" {
		return errors.New("invalid file path")
	}

	parent, name, err := v.resolveParent(filePath)
	if err != nil {
		return err
	}

	if _, exists := parent.entries[name]; exists {
		return errors.New("file already exists")
	}

	id := v.nextID
	v.nextID++

	size, mode := file.Stat()
	modTime := file.ModTime()
	if modTime.IsZero() {
		modTime = time.Now()
	}
	node := &fsNode{
		id:           id,
		name:         name,
		parent:       parent.id,
		mode:         mode,
		size:         size,
		xattr:        make(map[string][]byte),
		abstractFile: file,
		modTime:      modTime,
	}

	parent.entries[name] = id
	if parent.abstractDir == nil {
		parent.modTime = time.Now()
	}
	v.nodes[id] = node
	return nil
}

// AddAbstractDir adds an abstract directory at the specified path.
// Parent directories are created as needed.
func (v *virtioFsBackend) AddAbstractDir(dirPath string, dir AbstractDir) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.ensureRoot()

	dirPath = cleanName(dirPath)
	if dirPath == "" {
		return errors.New("invalid directory path")
	}

	parent, name, err := v.resolveParent(dirPath)
	if err != nil {
		return err
	}

	if _, exists := parent.entries[name]; exists {
		return errors.New("directory already exists")
	}

	id := v.nextID
	v.nextID++

	mode := dir.Stat()
	modTime := dir.ModTime()
	if modTime.IsZero() {
		modTime = time.Now()
	}
	node := &fsNode{
		id:          id,
		name:        name,
		parent:      parent.id,
		mode:        fs.ModeDir | mode,
		xattr:       make(map[string][]byte),
		entries:     make(map[string]uint64),
		abstractDir: dir,
		modTime:     modTime,
	}

	parent.entries[name] = id
	if parent.abstractDir == nil {
		parent.modTime = time.Now()
	}
	v.nodes[id] = node
	return nil
}

// SetAbstractRoot sets the root directory to be an abstract directory.
// This replaces the root node with one backed by the provided AbstractDir.
func (v *virtioFsBackend) SetAbstractRoot(dir AbstractDir) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.ensureRoot()

	// Update the root node to be backed by the abstract directory
	root := v.nodes[virtioFsRootNodeID]
	root.abstractDir = dir
	root.mode = fs.ModeDir | dir.Stat()
	if mt := dir.ModTime(); !mt.IsZero() {
		root.modTime = mt
	} else {
		root.modTime = time.Now()
	}

	return nil
}

// resolveParent walks the path and returns the parent directory and the final name.
// Creates intermediate directories as needed.
func (v *virtioFsBackend) resolveParent(filePath string) (*fsNode, string, error) {
	parts := strings.Split(filePath, "/")
	if len(parts) == 0 {
		return nil, "", errors.New("empty path")
	}

	parent := v.nodes[virtioFsRootNodeID]
	for i := 0; i < len(parts)-1; i++ {
		partName := parts[i]
		if partName == "" {
			continue
		}

		if parent.entries == nil {
			parent.entries = make(map[string]uint64)
		}

		childID, exists := parent.entries[partName]
		if exists {
			child := v.nodes[childID]
			if !child.isDir() {
				return nil, "", errors.New("path component is not a directory: " + partName)
			}
			parent = child
		} else {
			// Create intermediate directory
			id := v.nextID
			v.nextID++
			child := newDirNode(id, partName, parent.id, 0o755)
			parent.entries[partName] = id
			v.nodes[id] = child
			parent = child
		}
	}

	name := parts[len(parts)-1]
	if name == "" {
		return nil, "", errors.New("empty file name")
	}

	return parent, name, nil
}

// NewVirtioFsBackendWithAbstract returns a VirtioFsBackend that supports adding abstract files/dirs.
func NewVirtioFsBackendWithAbstract() VirtioFsBackend {
	return &virtioFsBackend{}
}
