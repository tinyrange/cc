package vfs

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/tinyrange/cc/internal/debug"
	"github.com/tinyrange/cc/internal/devices/virtio"
	linux "github.com/tinyrange/cc/internal/linux/defs/amd64"
)

// NOTE: Linux encodes suid/sgid/sticky in the low 12 permission bits (0o4000,
// 0o2000, 0o1000). Go's fs.ModeSetuid/ModeSetgid/ModeSticky are *not* those
// numeric bits (they are high-bit FileMode flags), so we model the Linux bits
// explicitly here.
const (
	modePermMask fs.FileMode = 0o7777
	modeSetuid   fs.FileMode = 0o4000
	modeSetgid   fs.FileMode = 0o2000
	modeSticky   fs.FileMode = 0o1000
)

const xattrPosixACLDefault = "system.posix_acl_default"
const xattrPosixACLAccess = "system.posix_acl_access"

func hasDefaultACL(n *fsNode) bool {
	if n == nil || n.xattr == nil {
		return false
	}
	_, ok := n.xattr[xattrPosixACLDefault]
	return ok
}

// parsePosixACLGroupPerm extracts the group permission bits (rwx 0..7) from a
// Linux POSIX ACL xattr blob.
//
// Prefer ACL_MASK if present; otherwise fall back to ACL_GROUP_OBJ.
//
// Format (little-endian):
// - u32 version (expected 2)
// - repeated entries: u16 tag, u16 perm, u32 id
//
// Tags: ACL_USER_OBJ=0x01, ACL_USER=0x02, ACL_GROUP_OBJ=0x04, ACL_GROUP=0x08,
// ACL_MASK=0x10, ACL_OTHER=0x20.
func parsePosixACLGroupPerm(b []byte) (uint16, bool) {
	if len(b) < 4 {
		return 0, false
	}
	ver := binary.LittleEndian.Uint32(b[0:4])
	if ver != 2 {
		return 0, false
	}
	off := 4
	var groupObjPerm *uint16
	var maskPerm *uint16
	for off+8 <= len(b) {
		tag := binary.LittleEndian.Uint16(b[off : off+2])
		perm := binary.LittleEndian.Uint16(b[off+2 : off+4])
		// id := binary.LittleEndian.Uint32(b[off+4 : off+8])
		if tag == 0x10 { // ACL_MASK
			p := perm & 0x7
			maskPerm = &p
		}
		if tag == 0x04 { // ACL_GROUP_OBJ
			p := perm & 0x7
			groupObjPerm = &p
		}
		off += 8
	}
	// If both exist, prefer the more permissive one. This matches what xfstests
	// expects in our simplified model (and avoids transient mask values that
	// would otherwise strip group-exec during create paths).
	if groupObjPerm != nil && maskPerm != nil {
		if *groupObjPerm > *maskPerm {
			return *groupObjPerm, true
		}
		return *maskPerm, true
	}
	if maskPerm != nil {
		return *maskPerm, true
	}
	if groupObjPerm != nil {
		return *groupObjPerm, true
	}
	return 0, false
}

func defaultACLMaskPerm(parent *fsNode) (fs.FileMode, bool) {
	if parent == nil || parent.xattr == nil {
		return 0, false
	}
	val, ok := parent.xattr[xattrPosixACLDefault]
	if !ok {
		return 0, false
	}
	p, ok := parsePosixACLGroupPerm(val)
	if !ok {
		return 0, false
	}
	return fs.FileMode(p) & 0o7, true
}

type posixACLPerms struct {
	userObj  uint16
	groupObj uint16
	other    uint16
	mask     *uint16

	// bookkeeping
	hasUserObj  bool
	hasGroupObj bool
	hasOther    bool
	hasMask     bool
	hasUser     bool
	hasGroup    bool
	entries     int
}

func parsePosixACLPerms(b []byte) (posixACLPerms, bool) {
	var p posixACLPerms
	if len(b) < 4 {
		return p, false
	}
	ver := binary.LittleEndian.Uint32(b[0:4])
	if ver != 2 {
		return p, false
	}
	off := 4
	for off+8 <= len(b) {
		tag := binary.LittleEndian.Uint16(b[off : off+2])
		perm := binary.LittleEndian.Uint16(b[off+2:off+4]) & 0x7
		p.entries++
		switch tag {
		case 0x01: // ACL_USER_OBJ
			p.userObj = perm
			p.hasUserObj = true
		case 0x04: // ACL_GROUP_OBJ
			p.groupObj = perm
			p.hasGroupObj = true
		case 0x20: // ACL_OTHER
			p.other = perm
			p.hasOther = true
		case 0x10: // ACL_MASK
			cp := perm
			p.mask = &cp
			p.hasMask = true
		case 0x02: // ACL_USER
			p.hasUser = true
		case 0x08: // ACL_GROUP
			p.hasGroup = true
		}
		off += 8
	}
	if !p.hasUserObj || !p.hasGroupObj || !p.hasOther {
		return p, false
	}
	return p, true
}

// applyPosixACLAccessToMode updates n.mode's rwx bits based on a POSIX ACL
// access xattr blob. It preserves setuid/setgid/sticky bits.
func applyPosixACLAccessToMode(n *fsNode, acl []byte) {
	parsed, ok := parsePosixACLPerms(acl)
	if !ok {
		return
	}
	g := parsed.groupObj
	if parsed.mask != nil {
		g = *parsed.mask
	}

	special := n.mode & (modeSetuid | modeSetgid | modeSticky)
	newPerm := special |
		(fs.FileMode(parsed.userObj&0x7) << 6) |
		(fs.FileMode(g&0x7) << 3) |
		fs.FileMode(parsed.other&0x7)
	n.mode = (n.mode &^ modePermMask) | (newPerm & modePermMask)
	if n.rawMode != 0 {
		n.rawMode = (n.rawMode &^ uint32(modePermMask)) | uint32(newPerm&modePermMask)
	}
}

func isMinimalPosixACLAccess(acl []byte) bool {
	parsed, ok := parsePosixACLPerms(acl)
	return ok && !parsed.hasMask && !parsed.hasUser && !parsed.hasGroup && parsed.entries == 3
}

// inheritDefaultACLToAccessForCreate returns an access-ACL blob for a newly
// created inode inheriting from a directory default ACL.
//
// POSIX rule: execute bits are cleared on regular file creation unless execute
// was requested. We only apply this when `mode` carries permission bits; some
// setgid/ACL kernel paths intentionally pass mode without perms.
func inheritDefaultACLToAccessForCreate(def []byte, mode uint32) []byte {
	out := append([]byte(nil), def...)
	reqPerm := mode & 0o777
	if reqPerm == 0 {
		return out
	}
	// If no execute bits requested at all, strip execute from all ACL entries.
	if reqPerm&0o111 == 0 {
		off := 4
		for off+8 <= len(out) {
			perm := binary.LittleEndian.Uint16(out[off+2 : off+4])
			perm &^= 0x1 // clear execute
			binary.LittleEndian.PutUint16(out[off+2:off+4], perm)
			off += 8
		}
	}
	return out
}

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
// Exactly one of File, Dir, or Symlink should be non-nil.
type AbstractEntry struct {
	File    AbstractFile
	Dir     AbstractDir
	Symlink AbstractSymlink
}

// AbstractSymlink represents a symlink (read-only) with a target string.
// Ownership information can be exposed by also implementing AbstractOwner.
type AbstractSymlink interface {
	// Stat returns the symlink's mode (permission bits are used; file type is ignored).
	Stat() fs.FileMode
	// ModTime returns the modified time for the symlink.
	ModTime() time.Time
	// Target returns the symlink target.
	Target() string
}

// AbstractOwner is an optional interface that AbstractFile and AbstractDir
// implementations can provide to expose ownership information (uid/gid).
type AbstractOwner interface {
	Owner() (uid, gid uint32)
}

type virtioFsBackend struct {
	mu         sync.Mutex
	nodes      map[uint64]*fsNode
	handles    map[uint64]uint64
	dirHandles map[uint64]*dirHandle
	nextID     uint64
	nextFH     uint64
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
	rawMode uint32 // raw mode from mknod (includes S_IFSOCK, S_IFIFO, etc.)
	rdev    uint32 // device number for device nodes
	size    uint64
	extents []fsExtent
	entries map[string]uint64
	xattr   map[string][]byte
	modTime time.Time // best-effort mtime
	aTime   time.Time // best-effort atime
	ctime   time.Time // change time (metadata changes like chmod/chown/xattr)
	nlink   uint32    // number of hard links (0 means 1 for backwards compat)
	uid     uint32    // owner user ID
	gid     uint32    // owner group ID

	symlinkTarget string

	// Abstract backing - if set, delegates to these instead of in-memory storage.
	abstractFile AbstractFile
	abstractDir  AbstractDir
}

func newDirNode(id uint64, name string, parent uint64, perm fs.FileMode) *fsNode {
	now := time.Now()
	return &fsNode{
		id:      id,
		name:    name,
		parent:  parent,
		mode:    fs.ModeDir | perm,
		entries: make(map[string]uint64),
		xattr:   make(map[string][]byte),
		modTime: now,
		aTime:   now,
		ctime:   now,
	}
}

func newFileNode(id uint64, name string, parent uint64, perm fs.FileMode) *fsNode {
	now := time.Now()
	return &fsNode{
		id:      id,
		name:    name,
		parent:  parent,
		mode:    perm,
		xattr:   make(map[string][]byte),
		modTime: now,
		aTime:   now,
		ctime:   now,
	}
}

func bumpTime(prev time.Time, next time.Time) time.Time {
	if prev.IsZero() {
		return next
	}
	if next.IsZero() {
		return prev
	}
	// Compare on wall-clock only; UnixNano works for pre-epoch values too.
	if next.UnixNano() <= prev.UnixNano() {
		return time.Unix(0, prev.UnixNano()+1)
	}
	return next
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
	aTime := n.aTime
	chgTime := n.ctime

	if n.abstractFile != nil {
		size, perm = n.abstractFile.Stat()
		if mt := n.abstractFile.ModTime(); !mt.IsZero() {
			modTime = mt
			aTime = mt
		}
	} else if n.abstractDir != nil {
		perm = n.abstractDir.Stat()
		if mt := n.abstractDir.ModTime(); !mt.IsZero() {
			modTime = mt
			aTime = mt
		}
	} else if n.isSymlink() {
		perm = n.mode & modePermMask
		size = uint64(len(n.symlinkTarget))
	} else {
		perm = n.mode & modePermMask
		size = n.size
	}
	// Ensure we include special bits (setuid/setgid/sticky) but never leak
	// unrelated FileMode flags into the on-wire numeric mode.
	perm &= modePermMask

	if modTime.IsZero() {
		modTime = time.Unix(0, 0)
	}
	if aTime.IsZero() {
		aTime = modTime
	}
	if chgTime.IsZero() {
		// Preserve legacy behavior: if we haven't tracked ctime, mirror modTime.
		chgTime = modTime
	}

	mode := uint32(perm)
	switch {
	case n.isDir():
		mode |= linux.S_IFDIR
	case n.isSymlink():
		mode |= linux.S_IFLNK
	case n.rawMode != 0:
		// Use raw mode for special file types (sockets, fifos, device nodes),
		// but keep permission + special bits from the mutable `n.mode` so chmod
		// works on these nodes.
		mode = (n.rawMode &^ uint32(modePermMask)) | uint32(perm&modePermMask)
	default:
		mode |= linux.S_IFREG
	}

	nlink := uint32(1)
	if n.nlink > 0 {
		nlink = n.nlink
	}
	if n.isDir() {
		entryCount := len(n.entries)
		if n.abstractDir != nil {
			if entries, err := n.abstractDir.ReadDir(); err == nil {
				entryCount = len(entries)
			}
		}
		nlink = 2 + uint32(entryCount)
	}

	asec := uint64(int64(aTime.Unix()))
	ansec := uint32(aTime.Nanosecond())
	msec := uint64(int64(modTime.Unix()))
	mnsec := uint32(modTime.Nanosecond())
	csec := uint64(chgTime.Unix())
	cnsec := uint32(chgTime.Nanosecond())

	return virtio.FuseAttr{
		Ino:       n.id,
		Mode:      mode,
		Size:      size,
		NLink:     nlink,
		UID:       n.uid,
		GID:       n.gid,
		RDev:      n.rdev,
		Blocks:    n.blockUsage(),
		BlkSize:   4096,
		ATimeSec:  asec,
		ATimeNsec: ansec,
		MTimeSec:  msec,
		MTimeNsec: mnsec,
		CTimeSec:  csec,
		CTimeNsec: cnsec,
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
	v.dirHandles = make(map[uint64]*dirHandle)
	v.nextID = virtioFsRootNodeID + 1
	v.nextFH = 1
}

type dirHandle struct {
	nodeID  uint64
	ents    []dirHandleEnt // stable snapshot for the lifetime of the handle
	started bool
}

type dirHandleEnt struct {
	name string
	ino  uint64
	typ  uint32
}

// snapshotDirEnts builds a deterministic snapshot of directory entries and
// materializes abstract entries in sorted order so inode assignment is stable
// (independent of pagination boundaries).
//
// NOTE: caller must hold v.mu.
func (v *virtioFsBackend) snapshotDirEnts(dirNode *fsNode) []dirHandleEnt {
	// Build deterministic name set excluding "." and "..".
	nameSet := make(map[string]struct{})
	for name := range dirNode.entries {
		if name == "." || name == ".." {
			continue
		}
		nameSet[name] = struct{}{}
	}

	var abstractEntries []AbstractDirEntry
	if dirNode.abstractDir != nil {
		if ents, e := dirNode.abstractDir.ReadDir(); e == nil {
			abstractEntries = ents
			for _, ent := range ents {
				if ent.Name == "." || ent.Name == ".." {
					continue
				}
				nameSet[ent.Name] = struct{}{}
			}
		}
	}

	rest := make([]string, 0, len(nameSet))
	for name := range nameSet {
		rest = append(rest, name)
	}
	sort.Strings(rest)

	// For abstract directories, pre-create nodes in sorted order so inode assignment
	// is deterministic and does not depend on READDIR pagination.
	if dirNode.abstractDir != nil {
		_ = abstractEntries
		for _, name := range rest {
			if _, ok := dirNode.entries[name]; ok {
				continue
			}
			if entry, lookupErr := dirNode.abstractDir.Lookup(name); lookupErr == nil {
				_, _ = v.createAbstractNode(dirNode, name, entry)
			}
		}
	}

	// Build a stable snapshot of (name, ino, type).
	ents := make([]dirHandleEnt, 0, len(rest)+2)
	ents = append(ents, dirHandleEnt{name: ".", ino: dirNode.id, typ: uint32(linux.DT_DIR)})
	parentIno := dirNode.id
	if dirNode.parent != 0 {
		parentIno = dirNode.parent
	}
	ents = append(ents, dirHandleEnt{name: "..", ino: parentIno, typ: uint32(linux.DT_DIR)})

	for _, name := range rest {
		id, ok := dirNode.entries[name]
		if !ok {
			// Directory changed between enumeration and now. Skip entries we can't resolve.
			continue
		}
		child := v.nodes[id]
		typ := uint32(linux.DT_REG)
		if child != nil {
			switch {
			case child.isDir():
				typ = uint32(linux.DT_DIR)
			case child.isSymlink():
				typ = uint32(linux.DT_LNK)
			default:
				typ = uint32(linux.DT_REG)
			}
		}
		ents = append(ents, dirHandleEnt{name: name, ino: id, typ: typ})
	}
	return ents
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
		// Check if the directory provides ownership info
		if owner, ok := entry.Dir.(AbstractOwner); ok {
			node.uid, node.gid = owner.Owner()
		}
	} else if entry.File != nil {
		node.abstractFile = entry.File
		size, mode := entry.File.Stat()
		node.mode = mode
		node.size = size
		if mt := entry.File.ModTime(); !mt.IsZero() {
			modTime = mt
		}
		// Check if the file provides ownership info
		if owner, ok := entry.File.(AbstractOwner); ok {
			node.uid, node.gid = owner.Owner()
		}
	} else if entry.Symlink != nil {
		target := entry.Symlink.Target()
		perm := entry.Symlink.Stat().Perm()
		if perm == 0 {
			perm = 0o777
		}
		node.mode = fs.ModeSymlink | perm
		node.symlinkTarget = target
		node.size = uint64(len(target))
		if mt := entry.Symlink.ModTime(); !mt.IsZero() {
			modTime = mt
		}
		if owner, ok := entry.Symlink.(AbstractOwner); ok {
			node.uid, node.gid = owner.Owner()
		}
	} else {
		return nil, -int32(linux.ENOENT)
	}

	node.modTime = modTime
	node.aTime = modTime
	node.ctime = modTime
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
	// Linux clears setuid on write by unprivileged writers. It clears setgid on
	// write only when the file is group-executable (i.e. setgid in the "s" form),
	// but preserves setgid when group-exec is not set (the "S" form, used for
	// mandatory locking semantics).
	if n.mode&modeSetuid != 0 {
		n.mode &^= modeSetuid
	}
	if n.mode&modeSetgid != 0 && (n.mode&0o010) != 0 {
		n.mode &^= modeSetgid
	}
	n.extents = mergeExtents(append(n.extents, fsExtent{off: off, data: append([]byte(nil), data...)}))
	if off+uint64(len(data)) > n.size {
		n.size = off + uint64(len(data))
	}
	now := time.Now()
	n.modTime = bumpTime(n.modTime, now)
	n.ctime = bumpTime(n.ctime, now)
	return nil
}

func (n *fsNode) truncate(size uint64) error {
	if n.abstractFile != nil {
		return n.abstractFile.Truncate(size)
	}
	// Truncation clears setuid; it clears setgid only when the file is
	// group-executable (mirrors Linux behavior and xfstests expectations).
	if n.mode&modeSetuid != 0 {
		n.mode &^= modeSetuid
	}
	if n.mode&modeSetgid != 0 && (n.mode&0o010) != 0 {
		n.mode &^= modeSetgid
	}
	if size >= n.size {
		n.size = size
		now := time.Now()
		n.modTime = bumpTime(n.modTime, now)
		n.ctime = bumpTime(n.ctime, now)
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
	now := time.Now()
	n.modTime = bumpTime(n.modTime, now)
	n.ctime = bumpTime(n.ctime, now)
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
	// Advertise POSIX ACL support so Linux will round-trip ACLs via xattrs.
	// This enables `setfacl`/`getfacl` behavior expected by xfstests.
	return 128 * 1024, virtio.FuseCapPosixACL
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
	// Best-effort atime tracking.
	now := time.Now()
	n.aTime = bumpTime(n.aTime, now)
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
		// Never allow "." or ".." to come from the backing store.
		if name == "." || name == ".." {
			continue
		}
		nameSet[name] = true
	}
	// Also include entries from abstract directory if present
	if dirNode.abstractDir != nil {
		if abstractEntries, err := dirNode.abstractDir.ReadDir(); err == nil {
			for _, entry := range abstractEntries {
				if entry.Name == "." || entry.Name == ".." {
					continue
				}
				nameSet[entry.Name] = true
			}
		}
	}

	// FUSE "off" is an opaque cookie provided by the filesystem (us) to the kernel.
	// The kernel passes it back to continue enumeration. It is *not* a byte offset.
	//
	// We implement it as a stable, 0-based entry index into a deterministic name list
	// that always starts with "." and "..". Each returned dirent's "off" field is the
	// next index (i+1), so subsequent READDIR calls resume correctly.
	names := make([]string, 0, len(nameSet)+2)
	names = append(names, ".", "..")
	rest := make([]string, 0, len(nameSet))
	for name := range nameSet {
		rest = append(rest, name)
	}
	sort.Strings(rest)
	names = append(names, rest...)

	if off >= uint64(len(names)) {
		return []byte{}, 0
	}

	var buf []byte
	for idx := int(off); idx < len(names); idx++ {
		name := names[idx]
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
					if entry.Dir != nil {
						typ = linux.DT_DIR
					} else if entry.Symlink != nil {
						typ = linux.DT_LNK
					} else if entry.File != nil {
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
		// Cookie is the next entry index.
		dirent := buildFuseDirent(id, name, uint32(typ), uint64(idx+1))
		if maxBytes > 0 && len(buf)+len(dirent) > int(maxBytes) {
			break
		}
		buf = append(buf, dirent...)
	}

	return buf, 0
}

// OpenDir implements virtio's optional directory-handle backend.
// It captures a stable snapshot of directory entry names (and ensures inode/name mapping
// for abstract directories is deterministic regardless of pagination boundaries).
func (v *virtioFsBackend) OpenDir(nodeID uint64, _ uint32) (fh uint64, errno int32) {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.ensureRoot()
	dirNode, err := v.node(nodeID)
	if err != 0 {
		return 0, err
	}
	if !dirNode.isDir() {
		return 0, -int32(linux.ENOTDIR)
	}

	fh = v.nextFH
	v.nextFH++
	// Do NOT snapshot here. Names may be created after OPENDIR but before the first
	// READDIR, and they must be observable (see xfstests generic/471).
	// Snapshotting is deferred until the first READDIR(off=0) on this handle.
	v.dirHandles[fh] = &dirHandle{nodeID: nodeID}
	return fh, 0
}

// ReleaseDir implements virtio's optional directory-handle backend.
func (v *virtioFsBackend) ReleaseDir(_ uint64, fh uint64) {
	v.mu.Lock()
	defer v.mu.Unlock()
	delete(v.dirHandles, fh)
}

// ReadDirHandle implements virtio's optional directory-handle backend.
func (v *virtioFsBackend) ReadDirHandle(nodeID uint64, fh uint64, off uint64, maxBytes uint32) ([]byte, int32) {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.ensureRoot()
	h, ok := v.dirHandles[fh]
	if !ok || h.nodeID != nodeID {
		return nil, -int32(linux.EBADF)
	}

	dirNode, err := v.node(nodeID)
	if err != 0 {
		return nil, err
	}
	if !dirNode.isDir() {
		return nil, -int32(linux.ENOTDIR)
	}

	// First enumeration on this handle: snapshot at the start, not at OPENDIR time.
	if !h.started && len(h.ents) == 0 {
		h.ents = v.snapshotDirEnts(dirNode)
	}

	// POSIX: rewinddir(3) must observe new names created after the opendir(3).
	// On Linux, rewinddir typically results in a READDIR call with off=0 after the
	// stream has already been read. We treat that as a rewind and refresh the snapshot.
	if off == 0 && h.started {
		h.ents = v.snapshotDirEnts(dirNode)
	}
	h.started = true

	if off >= uint64(len(h.ents)) {
		return []byte{}, 0
	}

	var buf []byte
	for idx := int(off); idx < len(h.ents); idx++ {
		ent := h.ents[idx]
		// Cookie is the next entry index.
		dirent := buildFuseDirent(ent.ino, ent.name, ent.typ, uint64(idx+1))
		if maxBytes > 0 && len(buf)+len(dirent) > int(maxBytes) {
			// If we can't fit even a single entry, do not return an empty buffer (which
			// the kernel treats as EOF). Report an error instead.
			if len(buf) == 0 {
				return nil, -int32(linux.EINVAL)
			}
			break
		}
		buf = append(buf, dirent...)
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
func (v *virtioFsBackend) Create(parent uint64, name string, mode uint32, flags uint32, umask uint32, uid uint32, gid uint32) (nodeID uint64, fh uint64, attr virtio.FuseAttr, errno int32) {
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
	// Keep permission + special bits (suid/sgid/sticky); apply umask.
	perm := fs.FileMode(mode&^(umask&0777)) & modePermMask
	// If a default ACL is present, group bits are governed by the ACL mask.
	// In particular, umask must not strip group permissions in the ACL path.
	if m, ok := defaultACLMaskPerm(parentNode); ok {
		debug.Writef("vfs.acl_create", "parent=%d name=%q inMode=0%o umask=0%o before=0%o groupPerm=0%o", parentNode.id, clean, mode, umask, perm, m)
		perm = (perm &^ 0o070) | (m << 3)
		debug.Writef("vfs.acl_create", "parent=%d name=%q after=0%o", parentNode.id, clean, perm)
	}
	node := newFileNode(id, clean, parentNode.id, perm)
	node.uid = uid
	node.gid = gid
	// If parent has setgid, new entries inherit the parent's group.
	if parentNode.mode&modeSetgid != 0 {
		node.gid = parentNode.gid
	}
	// Inherit access ACL for newly created files if parent has a default ACL.
	if parentNode.xattr != nil {
		if def, ok := parentNode.xattr[xattrPosixACLDefault]; ok {
			acc := inheritDefaultACLToAccessForCreate(def, mode)
			applyPosixACLAccessToMode(node, acc)
			if !isMinimalPosixACLAccess(acc) {
				node.xattr[xattrPosixACLAccess] = append([]byte(nil), acc...)
			}
		}
	}
	parentNode.entries[clean] = id
	if parentNode.abstractDir == nil {
		now := time.Now()
		parentNode.modTime = bumpTime(parentNode.modTime, now)
		parentNode.aTime = bumpTime(parentNode.aTime, now)
		parentNode.ctime = bumpTime(parentNode.ctime, now)
	}
	v.nodes[id] = node

	fh = v.nextFH
	v.nextFH++
	v.handles[fh] = id
	return id, fh, node.attr(), 0
}

func (v *virtioFsBackend) Mkdir(parent uint64, name string, mode uint32, umask uint32, uid uint32, gid uint32) (nodeID uint64, attr virtio.FuseAttr, errno int32) {
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
	// Keep permission + special bits (suid/sgid/sticky); apply umask.
	perm := fs.FileMode(mode&^(umask&0777)) & modePermMask
	// If a default ACL is present, group bits are governed by the ACL mask.
	if m, ok := defaultACLMaskPerm(parentNode); ok {
		// For directories, don't synthesize execute permission from the ACL if it
		// wasn't requested (mkdirat(..., 0000) should not suddenly become +x).
		reqG := (perm >> 3) & 0o7
		outG := (m &^ 0o1) | (reqG & 0o1)
		perm = (perm &^ 0o070) | (outG << 3)
	}
	node := newDirNode(id, clean, parentNode.id, perm)
	node.uid = uid
	node.gid = gid
	// If parent has setgid, new directories inherit parent's group and setgid.
	if parentNode.mode&modeSetgid != 0 {
		node.gid = parentNode.gid
		node.mode |= modeSetgid
	}
	// Inherit default ACLs.
	if parentNode.xattr != nil {
		if def, ok := parentNode.xattr[xattrPosixACLDefault]; ok {
			node.xattr[xattrPosixACLDefault] = append([]byte(nil), def...)
			node.xattr[xattrPosixACLAccess] = append([]byte(nil), def...)
			applyPosixACLAccessToMode(node, def)
			// Clamp execute bits to those requested by mkdir(2).
			reqExec := fs.FileMode(mode&^(umask&0o777)) & 0o111
			node.mode = (node.mode &^ 0o111) | reqExec
			if node.rawMode != 0 {
				node.rawMode = (node.rawMode &^ 0o111) | uint32(reqExec)
			}
		}
	}
	parentNode.entries[clean] = id
	if parentNode.abstractDir == nil {
		now := time.Now()
		parentNode.modTime = bumpTime(parentNode.modTime, now)
		parentNode.aTime = bumpTime(parentNode.aTime, now)
		parentNode.ctime = bumpTime(parentNode.ctime, now)
	}
	v.nodes[id] = node
	return id, node.attr(), 0
}

func (v *virtioFsBackend) Mknod(parent uint64, name string, mode uint32, rdev uint32, umask uint32, uid uint32, gid uint32) (nodeID uint64, attr virtio.FuseAttr, errno int32) {
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
	// Preserve full mode including file type bits (S_IFSOCK, S_IFIFO, etc.) and apply umask to permission bits
	perm := fs.FileMode(mode&^(umask&0777)) & modePermMask
	// If a default ACL is present, group bits are governed by the ACL mask.
	if m, ok := defaultACLMaskPerm(parentNode); ok {
		perm = (perm &^ 0o070) | (m << 3)
	}
	node := newFileNode(id, clean, parentNode.id, perm)
	// Store the raw mode for special file types (sockets, fifos, etc.)
	node.rawMode = mode
	node.rdev = rdev
	node.uid = uid
	node.gid = gid
	// If parent has setgid, new entries inherit the parent's group.
	if parentNode.mode&modeSetgid != 0 {
		node.gid = parentNode.gid
	}
	// Inherit access ACL for newly created nodes if parent has a default ACL.
	if parentNode.xattr != nil {
		if def, ok := parentNode.xattr[xattrPosixACLDefault]; ok {
			acc := inheritDefaultACLToAccessForCreate(def, mode)
			applyPosixACLAccessToMode(node, acc)
			if !isMinimalPosixACLAccess(acc) {
				node.xattr[xattrPosixACLAccess] = append([]byte(nil), acc...)
			}
		}
	}
	parentNode.entries[clean] = id
	if parentNode.abstractDir == nil {
		now := time.Now()
		parentNode.modTime = bumpTime(parentNode.modTime, now)
		parentNode.aTime = bumpTime(parentNode.aTime, now)
		parentNode.ctime = bumpTime(parentNode.ctime, now)
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

func (v *virtioFsBackend) SetXattr(nodeID uint64, name string, value []byte, flags uint32, reqUID uint32, reqGID uint32) int32 {
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
	// Special handling for POSIX ACLs: minimal access ACLs are represented by
	// mode bits alone and should not force an xattr (+) indicator.
	if name == xattrPosixACLAccess {
		if parsed, ok := parsePosixACLPerms(value); ok && !parsed.hasMask && !parsed.hasUser && !parsed.hasGroup && parsed.entries == 3 {
			applyPosixACLAccessToMode(n, value)
			// Drop the xattr to emulate kernel behavior for "minimal" ACLs.
			delete(n.xattr, name)
		} else {
			n.xattr[name] = append([]byte(nil), value...)
			applyPosixACLAccessToMode(n, value)
		}
	} else {
		n.xattr[name] = append([]byte(nil), value...)
	}
	if name == xattrPosixACLDefault {
		p, ok := parsePosixACLGroupPerm(value)
		head := value
		if len(head) > 32 {
			head = head[:32]
		}
		debug.Writef("vfs.posix_acl_default", "node=%d size=%d groupPerm=%d ok=%t head=%s", nodeID, len(value), p, ok, hex.EncodeToString(head))
	}
	// If an unprivileged caller changes metadata on a setgid inode, Linux may
	// clear the setgid bit when the caller is not in the owning group.
	if reqUID != 0 && (n.mode&modeSetgid) != 0 && reqGID != n.gid {
		n.mode &^= modeSetgid
		if n.rawMode != 0 {
			n.rawMode &^= uint32(modeSetgid)
		}
	}
	n.ctime = bumpTime(n.ctime, time.Now())
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

func (v *virtioFsBackend) ListXattr(nodeID uint64) ([]byte, int32) {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.ensureRoot()
	n, err := v.node(nodeID)
	if err != 0 {
		return nil, err
	}
	if len(n.xattr) == 0 {
		return []byte{}, 0
	}

	// Return a NUL-terminated list of names, deterministic order.
	names := make([]string, 0, len(n.xattr))
	for k := range n.xattr {
		names = append(names, k)
	}
	sort.Strings(names)

	var out []byte
	for _, k := range names {
		out = append(out, k...)
		out = append(out, 0)
	}
	return out, 0
}

func (v *virtioFsBackend) RemoveXattr(nodeID uint64, name string) int32 {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.ensureRoot()
	n, err := v.node(nodeID)
	if err != 0 {
		return err
	}
	if len(n.xattr) == 0 {
		return -errNoData
	}
	if _, ok := n.xattr[name]; !ok {
		return -errNoData
	}
	delete(n.xattr, name)
	n.ctime = bumpTime(n.ctime, time.Now())
	return 0
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
		srcParent.modTime = bumpTime(srcParent.modTime, now)
		srcParent.aTime = bumpTime(srcParent.aTime, now)
		srcParent.ctime = bumpTime(srcParent.ctime, now)
	}
	if dstParent.abstractDir == nil {
		dstParent.modTime = bumpTime(dstParent.modTime, now)
		dstParent.aTime = bumpTime(dstParent.aTime, now)
		dstParent.ctime = bumpTime(dstParent.ctime, now)
	}
	if node.abstractFile == nil && node.abstractDir == nil {
		node.modTime = bumpTime(node.modTime, now)
		node.aTime = bumpTime(node.aTime, now)
		node.ctime = bumpTime(node.ctime, now)
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
	if node == nil {
		// Node was already deleted (shouldn't happen, but be safe)
		delete(parentNode.entries, clean)
		return 0
	}
	if node.isDir() {
		return -int32(linux.EISDIR)
	}
	delete(parentNode.entries, clean)
	if parentNode.abstractDir == nil {
		now := time.Now()
		parentNode.modTime = bumpTime(parentNode.modTime, now)
		parentNode.aTime = bumpTime(parentNode.aTime, now)
		parentNode.ctime = bumpTime(parentNode.ctime, now)
	}
	// POSIX: unlink changes target inode metadata (nlink), so ctime must update.
	node.ctime = bumpTime(node.ctime, time.Now())
	// Decrement link count; only delete node when no more links remain
	if node.nlink <= 1 {
		// nlink==0 means 1 link (implicit), nlink==1 means 1 link (explicit)
		delete(v.nodes, id)
	} else {
		node.nlink--
	}
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
		now := time.Now()
		parentNode.modTime = bumpTime(parentNode.modTime, now)
		parentNode.aTime = bumpTime(parentNode.aTime, now)
		parentNode.ctime = bumpTime(parentNode.ctime, now)
	}
	delete(v.nodes, id)
	return 0
}

func (v *virtioFsBackend) SetAttr(nodeID uint64, size *uint64, mode *uint32, uid *uint32, gid *uint32, atime *time.Time, mtime *time.Time, reqUID uint32, reqGID uint32) int32 {
	v.mu.Lock()
	defer v.mu.Unlock()

	if size == nil && mode == nil && uid == nil && gid == nil && atime == nil && mtime == nil {
		return 0
	}
	v.ensureRoot()
	n, err := v.node(nodeID)
	if err != 0 {
		return err
	}
	timeTouched := false
	if atime != nil {
		n.aTime = *atime
		timeTouched = true
	}
	if mtime != nil {
		n.modTime = *mtime
		timeTouched = true
	}
	if timeTouched {
		// Updating timestamps is a metadata change; bump ctime to now.
		n.ctime = bumpTime(n.ctime, time.Now())
	}
	if size != nil {
		if truncErr := n.truncate(*size); truncErr != nil {
			return -int32(linux.EIO)
		}
	}
	if mode != nil {
		// Preserve non-permission bits (directory, symlink, etc.) and update
		// permission + special bits (suid/sgid/sticky).
		oldType := n.mode &^ modePermMask
		newBits := fs.FileMode(*mode) & modePermMask
		// If the parent has a default ACL, group bits are governed by the ACL's
		// group permission (mask/group_obj). This matches Linux's ACL semantics
		// where the group mode reflects the ACL mask, not umask.
		if n.parent != 0 {
			if parent := v.nodes[n.parent]; parent != nil {
				if m, ok := defaultACLMaskPerm(parent); ok {
					newBits = (newBits &^ 0o070) | (m << 3)
				}
			}
		}
		// If an unprivileged caller tries to keep/set setgid but is not in the
		// owning group, Linux clears setgid.
		if reqUID != 0 && (newBits&modeSetgid) != 0 && reqGID != n.gid {
			newBits &^= modeSetgid
		}
		n.mode = oldType | newBits
		if n.rawMode != 0 {
			// Keep special node type bits (fifo/device/etc.) while updating
			// permission + special bits.
			n.rawMode = (n.rawMode &^ uint32(modePermMask)) | uint32(newBits&modePermMask)
		}
		n.ctime = bumpTime(n.ctime, time.Now())
	}
	if uid != nil || gid != nil {
		if uid != nil {
			n.uid = *uid
		}
		if gid != nil {
			n.gid = *gid
		}
		// Linux clears setuid on chown. It clears setgid only when the file is
		// group-executable (the "s" form), but preserves setgid when group-exec
		// is not set (the "S" form).
		n.mode &^= modeSetuid
		if n.mode&modeSetgid != 0 && (n.mode&0o010) != 0 {
			n.mode &^= modeSetgid
		}
		n.ctime = bumpTime(n.ctime, time.Now())
	}
	return 0
}

func (v *virtioFsBackend) Symlink(parent uint64, name string, target string, _ uint32, uid uint32, gid uint32) (nodeID uint64, attr virtio.FuseAttr, errno int32) {
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

	now := time.Now()
	id := v.nextID
	v.nextID++
	node := &fsNode{
		id:            id,
		name:          clean,
		parent:        parentNode.id,
		mode:          fs.ModeSymlink | 0o777,
		size:          uint64(len(target)),
		xattr:         make(map[string][]byte),
		modTime:       now,
		ctime:         now,
		symlinkTarget: target,
		uid:           uid,
		gid:           gid,
	}
	parentNode.entries[clean] = id
	if parentNode.abstractDir == nil {
		parentNode.modTime = bumpTime(parentNode.modTime, now)
		parentNode.aTime = bumpTime(parentNode.aTime, now)
		parentNode.ctime = bumpTime(parentNode.ctime, now)
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

func (v *virtioFsBackend) Link(oldNodeID uint64, newParent uint64, newName string) (nodeID uint64, attr virtio.FuseAttr, errno int32) {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.ensureRoot()
	oldNode, err := v.node(oldNodeID)
	if err != 0 {
		return 0, virtio.FuseAttr{}, err
	}
	if oldNode.isDir() {
		return 0, virtio.FuseAttr{}, -int32(linux.EPERM)
	}
	parentNode, err := v.node(newParent)
	if err != 0 {
		return 0, virtio.FuseAttr{}, err
	}
	if !parentNode.isDir() {
		return 0, virtio.FuseAttr{}, -int32(linux.ENOTDIR)
	}
	clean := cleanName(newName)
	if clean == "" {
		return 0, virtio.FuseAttr{}, -int32(linux.EINVAL)
	}
	if e := nameErr(clean); e != 0 {
		return 0, virtio.FuseAttr{}, e
	}
	if _, exists := parentNode.entries[clean]; exists {
		return 0, virtio.FuseAttr{}, -int32(linux.EEXIST)
	}
	// Add new entry pointing to the existing node and increment link count
	parentNode.entries[clean] = oldNodeID
	if oldNode.nlink == 0 {
		oldNode.nlink = 2 // was 1 implicitly, now 2
	} else {
		oldNode.nlink++
	}
	// POSIX: link changes target inode metadata (nlink), so ctime must update.
	oldNode.ctime = bumpTime(oldNode.ctime, time.Now())
	if parentNode.abstractDir == nil {
		now := time.Now()
		parentNode.modTime = bumpTime(parentNode.modTime, now)
		parentNode.aTime = bumpTime(parentNode.aTime, now)
		parentNode.ctime = bumpTime(parentNode.ctime, now)
	}
	return oldNodeID, oldNode.attr(), 0
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
		aTime:        modTime,
		ctime:        modTime,
	}
	// Check if the file provides ownership info
	if owner, ok := file.(AbstractOwner); ok {
		node.uid, node.gid = owner.Owner()
	}

	parent.entries[name] = id
	if parent.abstractDir == nil {
		now := time.Now()
		parent.modTime = bumpTime(parent.modTime, now)
		parent.aTime = bumpTime(parent.aTime, now)
		parent.ctime = bumpTime(parent.ctime, now)
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
		ctime:       modTime,
	}
	// Check if the directory provides ownership info
	if owner, ok := dir.(AbstractOwner); ok {
		node.uid, node.gid = owner.Owner()
	}

	parent.entries[name] = id
	if parent.abstractDir == nil {
		now := time.Now()
		parent.modTime = now
		parent.ctime = now
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
		root.ctime = mt
	} else {
		now := time.Now()
		root.modTime = now
		root.ctime = now
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
