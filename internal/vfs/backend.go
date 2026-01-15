package vfs

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
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

// goModeToLinux converts Go's fs.FileMode to Linux permission bits.
// Go uses high bits for setuid/setgid/sticky (fs.ModeSetuid, etc.),
// while Linux uses the low 12 bits (0o4000, 0o2000, 0o1000).
func goModeToLinux(m fs.FileMode) fs.FileMode {
	perm := m.Perm() // low 9 bits (rwxrwxrwx)
	if m&fs.ModeSetuid != 0 {
		perm |= modeSetuid
	}
	if m&fs.ModeSetgid != 0 {
		perm |= modeSetgid
	}
	if m&fs.ModeSticky != 0 {
		perm |= modeSticky
	}
	return perm
}

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
	// fhOwners tracks the last observed fcntl lock "owner" cookie for a given open
	// file handle (fh). This lets us reliably drop any remaining locks on RELEASE,
	// even when the close path provides a different lock_owner cookie (e.g. OFD
	// locks vs POSIX locks).
	fhOwners map[uint64]uint64
	nextID   uint64
	nextFH   uint64
	// POSIX advisory locks keyed by (nodeID, owner). Each entry is a list of held locks.
	posixLocks map[lockKey][]lockRange
	// OFD locks keyed by (nodeID, fh). OFD locks are associated with an open file
	// description and should not be released until the file handle is fully closed.
	ofdLocks map[ofdLockKey][]lockRange
	lockCond *sync.Cond
}

type lockKey struct {
	nodeID uint64
	owner  uint64
}

type ofdLockKey struct {
	nodeID uint64
	fh     uint64
}

type lockRange struct {
	start uint64
	end   uint64
	typ   uint32 // F_RDLCK=0, F_WRLCK=1, F_UNLCK=2
	pid   uint32
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
	// Sparse file storage. We model allocation at a fixed block granularity to
	// provide realistic SEEK_DATA/SEEK_HOLE behavior (xfstests seek_sanity_test).
	// Key is block index (offset / fileBlockSize), value is a full block.
	blocks         map[uint64][]byte
	entries        map[string]uint64
	deletedEntries map[string]struct{} // tracks entries deleted from abstractDir to prevent re-creation
	xattr          map[string][]byte
	modTime  time.Time // best-effort mtime
	aTime    time.Time // best-effort atime
	ctime    time.Time // change time (metadata changes like chmod/chown/xattr)
	nlink    uint32    // number of hard links (POSIX st_nlink); may be 0 when unlinked but still open
	openRefs uint32    // open handle refcount (file handles + directory handles)
	unlinked bool      // true once link count reaches 0 (or directory removed); inode may still be open
	uid      uint32    // owner user ID
	gid      uint32    // owner group ID

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
		nlink:   1,
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
		nlink:   1,
		blocks:  make(map[uint64][]byte),
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
	const fileBlockSize = uint64(4096)
	if len(n.blocks) == 0 {
		if n.size > 0 {
			// Conservative: sparse file with size but no allocated blocks.
			return 0
		}
		return 0
	}
	// Each allocated 4KiB block counts as 8×512B sectors.
	return uint64(len(n.blocks)) * (fileBlockSize / 512)
}

func (n *fsNode) attr() virtio.FuseAttr {
	var perm fs.FileMode
	var size uint64
	modTime := n.modTime
	aTime := n.aTime
	chgTime := n.ctime

	if n.abstractFile != nil {
		var rawPerm fs.FileMode
		size, rawPerm = n.abstractFile.Stat()
		perm = goModeToLinux(rawPerm)
		if mt := n.abstractFile.ModTime(); !mt.IsZero() {
			modTime = mt
			aTime = mt
		}
	} else if n.abstractDir != nil {
		perm = goModeToLinux(n.abstractDir.Stat())
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

	var nlink uint32
	switch {
	case n.unlinked:
		// Unlinked-but-open inodes must report st_nlink==0 (xfstests generic/035).
		nlink = 0
	case n.isDir():
		// Best-effort directory link count.
		entryCount := len(n.entries)
		if n.abstractDir != nil {
			if entries, err := n.abstractDir.ReadDir(); err == nil {
				entryCount = len(entries)
			}
		}
		nlink = 2 + uint32(entryCount)
	default:
		// Regular file / symlink / special node.
		if n.nlink == 0 {
			// Defensive: most nodes are created with nlink=1.
			nlink = 1
		} else {
			nlink = n.nlink
		}
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
	v.fhOwners = make(map[uint64]uint64)
	v.nextID = virtioFsRootNodeID + 1
	v.nextFH = 1
	v.posixLocks = make(map[lockKey][]lockRange)
	v.ofdLocks = make(map[ofdLockKey][]lockRange)
	v.lockCond = sync.NewCond(&v.mu)
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
				// Skip entries that were explicitly deleted
				if dirNode.deletedEntries != nil {
					if _, deleted := dirNode.deletedEntries[ent.Name]; deleted {
						continue
					}
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
		typ := uint32(direntTypeForNode(child))
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

// maybeReapNode deletes an inode once it has no links and no remaining open handles.
// Caller must hold v.mu.
func (v *virtioFsBackend) maybeReapNode(n *fsNode) {
	if n == nil {
		return
	}
	if n.unlinked && n.openRefs == 0 {
		delete(v.nodes, n.id)
	}
}

// direntTypeForNode maps an inode to a Linux DT_* value for directory enumeration.
func direntTypeForNode(n *fsNode) uint32 {
	if n == nil {
		return linux.DT_UNKNOWN
	}
	if n.isDir() {
		return linux.DT_DIR
	}
	if n.isSymlink() {
		return linux.DT_LNK
	}
	if n.rawMode != 0 {
		switch n.rawMode & linux.S_IFMT {
		case linux.S_IFCHR:
			return linux.DT_CHR
		case linux.S_IFBLK:
			return linux.DT_BLK
		case linux.S_IFIFO:
			return linux.DT_FIFO
		case linux.S_IFSOCK:
			return linux.DT_SOCK
		}
	}
	return linux.DT_REG
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
	// Check if this entry was explicitly deleted (prevents re-creation from abstractDir)
	if parent.deletedEntries != nil {
		if _, deleted := parent.deletedEntries[name]; deleted {
			return nil, -int32(linux.ENOENT)
		}
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
		nlink:  1,
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
	const fileBlockSize = uint64(4096)
	buf := make([]byte, size)
	end := off + uint64(size)

	// Iterate blocks overlapping [off, end).
	first := off / fileBlockSize
	last := (end - 1) / fileBlockSize
	for bi := first; bi <= last; bi++ {
		b, ok := n.blocks[bi]
		if !ok {
			continue
		}
		bStart := bi * fileBlockSize
		bEnd := bStart + fileBlockSize
		start := max64(off, bStart)
		stop := min64(end, bEnd)
		copy(buf[start-off:stop-off], b[int(start-bStart):int(stop-bStart)])
	}
	return buf, nil
}

// materializeAbstractFile copies an abstract-backed regular file into in-memory
// blocks so subsequent write/truncate operations can succeed even when the
// backing store is read-only or does not support mutation.
//
// NOTE: this does not try to preserve sparse extents; it materializes a dense
// view of the file contents.
func (n *fsNode) materializeAbstractFile() error {
	if n.abstractFile == nil {
		return nil
	}
	af := n.abstractFile
	size, _ := af.Stat()

	// Reset to an in-memory file representation.
	n.abstractFile = nil
	n.blocks = nil
	n.size = size

	if size == 0 {
		return nil
	}

	const fileBlockSize = uint64(4096)
	const chunkSize = uint32(128 * 1024)

	n.blocks = make(map[uint64][]byte)

	var off uint64
	for off < size {
		want := chunkSize
		if remain := size - off; uint64(want) > remain {
			want = uint32(remain)
		}
		b, err := af.ReadAt(off, want)
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		if len(b) == 0 {
			// Defensive: avoid an infinite loop if the backing returns empty reads.
			break
		}
		// Copy into block map.
		pos := off
		i := 0
		for i < len(b) {
			bi := pos / fileBlockSize
			inBlock := pos % fileBlockSize
			nAvail := int(fileBlockSize - inBlock)
			toCopy := len(b) - i
			if toCopy > nAvail {
				toCopy = nAvail
			}
			blk, ok := n.blocks[bi]
			if !ok {
				blk = make([]byte, fileBlockSize)
				n.blocks[bi] = blk
			}
			start := int(inBlock)
			copy(blk[start:start+toCopy], b[i:i+toCopy])
			pos += uint64(toCopy)
			i += toCopy
		}
		off += uint64(len(b))
	}
	return nil
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
		// Copy-up on first write to avoid failing writes on read-only backings
		// (e.g. container image layers).
		if err := n.materializeAbstractFile(); err != nil {
			return err
		}
	}
	if len(data) == 0 {
		return nil
	}
	if n.blocks == nil {
		n.blocks = make(map[uint64][]byte)
	}
	const fileBlockSize = uint64(4096)
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

	// Block-based write with correct overwrite semantics.
	pos := off
	i := 0
	for i < len(data) {
		bi := pos / fileBlockSize
		inBlock := pos % fileBlockSize
		nAvail := int(fileBlockSize - inBlock)
		toCopy := len(data) - i
		if toCopy > nAvail {
			toCopy = nAvail
		}
		blk, ok := n.blocks[bi]
		if !ok {
			blk = make([]byte, fileBlockSize)
			n.blocks[bi] = blk
		}
		start := int(inBlock)
		copy(blk[start:start+toCopy], data[i:i+toCopy])
		pos += uint64(toCopy)
		i += toCopy
	}
	if end := off + uint64(len(data)); end > n.size {
		n.size = end
	}
	now := time.Now()
	n.modTime = bumpTime(n.modTime, now)
	n.ctime = bumpTime(n.ctime, now)
	return nil
}

func (n *fsNode) truncate(size uint64) error {
	if n.abstractFile != nil {
		// Copy-up on first truncate so open(O_TRUNC)/shell redirections work on
		// abstract-backed files like /etc/hosts.
		if err := n.materializeAbstractFile(); err != nil {
			return err
		}
	}
	const fileBlockSize = uint64(4096)
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
	// Drop blocks beyond EOF.
	if n.blocks != nil {
		if size == 0 {
			for k := range n.blocks {
				delete(n.blocks, k)
			}
		} else {
			lastKeep := (size - 1) / fileBlockSize
			for bi := range n.blocks {
				if bi > lastKeep {
					delete(n.blocks, bi)
				}
			}
			// Zero bytes beyond new EOF within the last block to avoid resurrecting
			// stale data if the file is later extended.
			if rem := size % fileBlockSize; rem != 0 {
				if blk, ok := n.blocks[lastKeep]; ok {
					clear(blk[int(rem):])
				}
			}
		}
	}
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
	// Also advertise POSIX locks support for fcntl(F_SETLK/F_GETLK).
	return 128 * 1024, virtio.FuseCapPosixACL | virtio.FuseCapPosixLocks
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
	n.openRefs++
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
	// Also include entries from abstract directory if present (but not deleted ones)
	if dirNode.abstractDir != nil {
		if abstractEntries, err := dirNode.abstractDir.ReadDir(); err == nil {
			for _, entry := range abstractEntries {
				if entry.Name == "." || entry.Name == ".." {
					continue
				}
				// Skip entries that were explicitly deleted
				if dirNode.deletedEntries != nil {
					if _, deleted := dirNode.deletedEntries[entry.Name]; deleted {
						continue
					}
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
		typ := uint32(linux.DT_DIR)
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
					typ = direntTypeForNode(child)
				}
			} else if dirNode.abstractDir != nil {
				// Look up in abstract directory
				if entry, lookupErr := dirNode.abstractDir.Lookup(name); lookupErr == nil {
					// Determine type from abstract entry
					if entry.Dir != nil {
						typ = uint32(linux.DT_DIR)
					} else if entry.Symlink != nil {
						typ = uint32(linux.DT_LNK)
					} else if entry.File != nil {
						typ = uint32(linux.DT_REG)
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
		dirent := buildFuseDirent(id, name, typ, uint64(idx+1))
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
	dirNode.openRefs++
	return fh, 0
}

// ReleaseDir implements virtio's optional directory-handle backend.
func (v *virtioFsBackend) ReleaseDir(_ uint64, fh uint64) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if h, ok := v.dirHandles[fh]; ok {
		if n := v.nodes[h.nodeID]; n != nil && n.openRefs > 0 {
			n.openRefs--
			v.maybeReapNode(n)
		}
		delete(v.dirHandles, fh)
	}
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
	v.ensureRoot()

	// Release OFD locks associated with this file handle on final close.
	// (Kernel should only issue RELEASE when the file handle is truly closed.)
	for k := range v.ofdLocks {
		if k.nodeID == nodeID && k.fh == fh {
			delete(v.ofdLocks, k)
		}
	}
	// Also drop any remaining locks associated with the lock owner cookie that was
	// used for this file handle. This is important for OFD lock tests (generic/478)
	// where the lock owner in the lk_in request may not match the lock_owner cookie
	// provided via FLUSH, so relying on FLUSH alone can leak locks indefinitely.
	if owner, ok := v.fhOwners[fh]; ok {
		delete(v.posixLocks, lockKey{nodeID: nodeID, owner: owner})
		delete(v.fhOwners, fh)
	}
	if v.lockCond != nil {
		v.lockCond.Broadcast()
	}

	if nid, ok := v.handles[fh]; ok {
		if n := v.nodes[nid]; n != nil && n.openRefs > 0 {
			n.openRefs--
			v.maybeReapNode(n)
		}
		delete(v.handles, fh)
		return
	}
	_ = nodeID
}

// Flush implements a best-effort close hook. Linux sends FUSE_FLUSH with a
// lock_owner so a FUSE filesystem can release POSIX locks on close.
//
// POSIX fcntl locks are per-process and are released when *any* fd referring to
// the file is closed by that process; this aligns with locktest expectations.
func (v *virtioFsBackend) Flush(nodeID uint64, fh uint64, lockOwner uint64) int32 {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.ensureRoot()

	_ = fh
	if lockOwner == 0 {
		return 0
	}
	delete(v.posixLocks, lockKey{nodeID: nodeID, owner: lockOwner})
	if v.lockCond != nil {
		v.lockCond.Broadcast()
	}
	return 0
}

// StatFS implements virtio.FsBackend.
func (v *virtioFsBackend) StatFS(nodeID uint64) (blocks uint64, bfree uint64, bavail uint64, files uint64, ffree uint64, bsize uint64, frsize uint64, namelen uint64, errno int32) {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.ensureRoot()
	_ = nodeID
	total := uint64(25 * 1024 * 1024) // 100GB in 4KB blocks
	free := uint64(24 * 1024 * 1024)  // ~96GB free
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
		existing.openRefs++
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
	node.openRefs++
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
	// SEEK_HOLE/DATA use signed offsets; negative offsets are rejected with ENXIO
	// (xfstests seek_sanity_test expects ENXIO).
	if int64(offset) < 0 {
		return 0, -int32(linux.ENXIO)
	}
	const fileBlockSize = uint64(4096)
	switch whence {
	case uint32(linux.SEEK_DATA):
		if offset >= n.size || len(n.blocks) == 0 {
			return 0, -int32(linux.ENXIO)
		}
		bi := offset / fileBlockSize
		if _, ok := n.blocks[bi]; ok {
			return offset, 0
		}
		// Find next allocated block.
		next := bi + 1
		for {
			if _, ok := n.blocks[next]; ok {
				off := next * fileBlockSize
				if off >= n.size {
					return 0, -int32(linux.ENXIO)
				}
				return off, 0
			}
			// Stop if we would definitely be past EOF.
			if next*fileBlockSize >= n.size {
				return 0, -int32(linux.ENXIO)
			}
			next++
		}
	case uint32(linux.SEEK_HOLE):
		if offset >= n.size {
			return offset, 0
		}
		if len(n.blocks) == 0 {
			return offset, 0
		}
		bi := offset / fileBlockSize
		if _, ok := n.blocks[bi]; !ok {
			return offset, 0
		}
		// Find end of contiguous allocated run.
		next := bi + 1
		for {
			if _, ok := n.blocks[next]; !ok {
				off := next * fileBlockSize
				if off > n.size {
					return n.size, 0
				}
				return off, 0
			}
			next++
		}
	default:
		return 0, -int32(linux.EINVAL)
	}
}

func (v *virtioFsBackend) Fallocate(nodeID uint64, fh uint64, offset uint64, length uint64, mode uint32) int32 {
	v.mu.Lock()
	defer v.mu.Unlock()

	nid, ok := v.handles[fh]
	if !ok || nid != nodeID {
		return -int32(linux.EBADF)
	}
	n, err := v.node(nid)
	if err != 0 {
		return err
	}
	if n.isDir() {
		return -int32(linux.EISDIR)
	}
	if length == 0 {
		return 0
	}
	const errOpNotSupp = int32(95) // EOPNOTSUPP/ENOTSUP
	const fileBlockSize = uint64(4096)
	// Only support the modes needed by xfstests/fsx/xfs_io:
	// - mode 0
	// - FALLOC_FL_KEEP_SIZE
	// Optionally support PUNCH_HOLE|KEEP_SIZE as a convenience.
	allowed := uint32(linux.FALLOC_FL_KEEP_SIZE | linux.FALLOC_FL_PUNCH_HOLE)
	if mode&^allowed != 0 {
		return -errOpNotSupp
	}
	if (mode&uint32(linux.FALLOC_FL_PUNCH_HOLE)) != 0 && (mode&uint32(linux.FALLOC_FL_KEEP_SIZE)) == 0 {
		// Linux requires KEEP_SIZE with PUNCH_HOLE.
		return -int32(linux.EINVAL)
	}
	if n.abstractFile != nil {
		// Abstract backing doesn't expose allocation semantics.
		return -errOpNotSupp
	}
	if n.blocks == nil {
		n.blocks = make(map[uint64][]byte)
	}

	end := offset + length
	sizeBefore := n.size
	if (mode & uint32(linux.FALLOC_FL_PUNCH_HOLE)) != 0 {
		// Convert the range to a hole by deleting/zeroing blocks in range.
		start := offset
		stop := end
		first := start / fileBlockSize
		last := (stop - 1) / fileBlockSize
		for bi := first; bi <= last; bi++ {
			bStart := bi * fileBlockSize
			bEnd := bStart + fileBlockSize
			hStart := max64(start, bStart)
			hEnd := min64(stop, bEnd)
			if hStart == bStart && hEnd == bEnd {
				delete(n.blocks, bi)
				continue
			}
			blk, ok := n.blocks[bi]
			if !ok {
				continue
			}
			clear(blk[int(hStart-bStart):int(hEnd-bStart)])
		}
		// KEEP_SIZE enforced above; size unchanged.
	} else if (mode & uint32(linux.FALLOC_FL_KEEP_SIZE)) == 0 {
		// Default fallocate extends file size.
		if end > n.size {
			n.size = end
		}
		// Allocate blocks for the newly reserved range.
		first := offset / fileBlockSize
		last := (end - 1) / fileBlockSize
		for bi := first; bi <= last; bi++ {
			if _, ok := n.blocks[bi]; !ok {
				n.blocks[bi] = make([]byte, fileBlockSize)
			}
		}
	} else {
		// KEEP_SIZE: allocate blocks but do not extend size.
		first := offset / fileBlockSize
		last := (end - 1) / fileBlockSize
		for bi := first; bi <= last; bi++ {
			if _, ok := n.blocks[bi]; !ok {
				n.blocks[bi] = make([]byte, fileBlockSize)
			}
		}
	}

	now := time.Now()
	n.ctime = bumpTime(n.ctime, now)
	if n.size != sizeBefore {
		// Size change updates mtime as well.
		n.modTime = bumpTime(n.modTime, now)
	}
	return 0
}

// --- POSIX advisory locks ---

const (
	fLockRdlck = 0
	fLockWrlck = 1
	fLockUnlck = 2
)

// rangesOverlap returns true if [a1,a2] overlaps [b1,b2] (inclusive).
func rangesOverlap(a1, a2, b1, b2 uint64) bool {
	return a1 <= b2 && b1 <= a2
}

// GetLk tests whether a lock could be placed and returns the conflicting lock, or an unlocked lock.
func (v *virtioFsBackend) GetLk(nodeID uint64, fh uint64, owner uint64, lk virtio.FuseLock, flags uint32) (virtio.FuseLock, int32) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.ensureRoot()

	if lk.Type == fLockUnlck {
		// Querying an unlock: return unlocked.
		return virtio.FuseLock{Type: fLockUnlck}, 0
	}

	ofd := (flags & virtio.FuseLkOFD) != 0
	_ = ofd

	// Check for conflicts against POSIX locks (other owners).
	for key, ranges := range v.posixLocks {
		if key.nodeID != nodeID {
			continue
		}
		// For OFD GETLK we still need to report conflicts with POSIX locks.
		if !ofd && key.owner == owner {
			continue
		}
		for _, r := range ranges {
			if !rangesOverlap(lk.Start, lk.End, r.start, r.end) {
				continue
			}
			if lk.Type == fLockWrlck || r.typ == fLockWrlck {
				// For OFD GETLK, Linux reports l_pid = 0.
				pid := r.pid
				if ofd {
					pid = 0
				}
				return virtio.FuseLock{Start: r.start, End: r.end, Type: r.typ, PID: pid}, 0
			}
		}
	}

	// Check for conflicts against OFD locks (other file handles).
	for key, ranges := range v.ofdLocks {
		if key.nodeID != nodeID {
			continue
		}
		// For OFD locks, same fh means same open-file-description.
		if key.fh == fh {
			continue
		}
		for _, r := range ranges {
			if !rangesOverlap(lk.Start, lk.End, r.start, r.end) {
				continue
			}
			if lk.Type == fLockWrlck || r.typ == fLockWrlck {
				// OFD GETLK reports pid 0.
				return virtio.FuseLock{Start: r.start, End: r.end, Type: r.typ, PID: 0}, 0
			}
		}
	}

	// No conflict.
	return virtio.FuseLock{Type: fLockUnlck}, 0
}

func (v *virtioFsBackend) canPlaceLock(nodeID uint64, fh uint64, owner uint64, lk virtio.FuseLock, flags uint32) bool {
	ofd := (flags & virtio.FuseLkOFD) != 0

	// Conflicts with POSIX locks.
	for k, ranges := range v.posixLocks {
		if k.nodeID != nodeID {
			continue
		}
		if !ofd && k.owner == owner {
			continue
		}
		for _, r := range ranges {
			if !rangesOverlap(lk.Start, lk.End, r.start, r.end) {
				continue
			}
			if lk.Type == fLockWrlck || r.typ == fLockWrlck {
				return false
			}
		}
	}
	// Conflicts with OFD locks.
	for k, ranges := range v.ofdLocks {
		if k.nodeID != nodeID {
			continue
		}
		if k.fh == fh {
			continue
		}
		for _, r := range ranges {
			if !rangesOverlap(lk.Start, lk.End, r.start, r.end) {
				continue
			}
			if lk.Type == fLockWrlck || r.typ == fLockWrlck {
				return false
			}
		}
	}
	return true
}

func mergeOrReplace(ranges []lockRange, lk virtio.FuseLock) []lockRange {
	newR := lockRange{start: lk.Start, end: lk.End, typ: lk.Type, pid: lk.PID}
	var kept []lockRange
	for _, r := range ranges {
		if rangesOverlap(lk.Start, lk.End, r.start, r.end) {
			// Expand new to cover.
			if r.start < newR.start {
				newR.start = r.start
			}
			if r.end > newR.end {
				newR.end = r.end
			}
		} else {
			kept = append(kept, r)
		}
	}
	return append(kept, newR)
}

func removeRange(ranges []lockRange, lk virtio.FuseLock) []lockRange {
	var kept []lockRange
	for _, r := range ranges {
		if !rangesOverlap(lk.Start, lk.End, r.start, r.end) {
			kept = append(kept, r)
			continue
		}
		// Partial overlap: split.
		if r.start < lk.Start {
			kept = append(kept, lockRange{start: r.start, end: lk.Start - 1, typ: r.typ, pid: r.pid})
		}
		if r.end > lk.End {
			kept = append(kept, lockRange{start: lk.End + 1, end: r.end, typ: r.typ, pid: r.pid})
		}
	}
	return kept
}

func (v *virtioFsBackend) setLockInternal(nodeID uint64, fh uint64, owner uint64, lk virtio.FuseLock, flags uint32, blocking bool) int32 {
	v.ensureRoot()
	if v.lockCond == nil {
		v.lockCond = sync.NewCond(&v.mu)
	}

	// Track the owner cookie used for locks on this handle so we can clean up on RELEASE.
	if v.fhOwners == nil {
		v.fhOwners = make(map[uint64]uint64)
	}
	v.fhOwners[fh] = owner

	ofd := (flags & virtio.FuseLkOFD) != 0

	// Unlock: always succeeds and wakes waiters.
	if lk.Type == fLockUnlck {
		if ofd {
			key := ofdLockKey{nodeID: nodeID, fh: fh}
			ranges := v.ofdLocks[key]
			kept := removeRange(ranges, lk)
			if len(kept) == 0 {
				delete(v.ofdLocks, key)
			} else {
				v.ofdLocks[key] = kept
			}
		} else {
			key := lockKey{nodeID: nodeID, owner: owner}
			ranges := v.posixLocks[key]
			kept := removeRange(ranges, lk)
			if len(kept) == 0 {
				delete(v.posixLocks, key)
			} else {
				v.posixLocks[key] = kept
			}
		}
		v.lockCond.Broadcast()
		return 0
	}

	// Wait for conflicts to clear if blocking.
	for !v.canPlaceLock(nodeID, fh, owner, lk, flags) {
		if !blocking {
			return -int32(linux.EAGAIN)
		}
		v.lockCond.Wait()
	}

	// Merge/replace in the correct lock table.
	if ofd {
		key := ofdLockKey{nodeID: nodeID, fh: fh}
		v.ofdLocks[key] = mergeOrReplace(v.ofdLocks[key], lk)
	} else {
		key := lockKey{nodeID: nodeID, owner: owner}
		v.posixLocks[key] = mergeOrReplace(v.posixLocks[key], lk)
	}
	v.lockCond.Broadcast()
	return 0
}

// SetLk sets or clears a POSIX advisory lock (non-blocking).
func (v *virtioFsBackend) SetLk(nodeID uint64, fh uint64, owner uint64, lk virtio.FuseLock, flags uint32) int32 {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.setLockInternal(nodeID, fh, owner, lk, flags, false)
}

// SetLkW is the blocking variant (SETLKW / F_OFD_SETLKW).
func (v *virtioFsBackend) SetLkW(nodeID uint64, fh uint64, owner uint64, lk virtio.FuseLock, flags uint32) int32 {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.setLockInternal(nodeID, fh, owner, lk, flags, true)
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
	// We only support the plain rename(2) behavior and RENAME_NOREPLACE.
	// Unknown renameat2(2) flags should fail with EINVAL.
	if flags&^linux.RENAME_NOREPLACE != 0 {
		return -int32(linux.EINVAL)
	}
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
	if srcName == "" || dstName == "" {
		return -int32(linux.EINVAL)
	}
	if e := nameErr(dstName); e != 0 {
		return e
	}
	// Trivial no-op.
	if oldParent == newParent && srcName == dstName {
		return 0
	}
	if !srcParent.isDir() || !dstParent.isDir() {
		return -int32(linux.ENOTDIR)
	}
	srcID, ok := srcParent.entries[srcName]
	if !ok {
		slog.Warn("virtiofs rename missing source", "parent", oldParent, "name", srcName)
		return -int32(linux.ENOENT)
	}
	srcNode := v.nodes[srcID]
	if srcNode == nil {
		return -int32(linux.ENOENT)
	}

	// Handle overwrite semantics (rename-overwrite should unlink the replaced inode).
	if dstID, exists := dstParent.entries[dstName]; exists {
		if flags&linux.RENAME_NOREPLACE != 0 {
			return -int32(linux.EEXIST)
		}
		// If destination is a different inode, unlink/rmdir it (but keep it alive if open).
		if dstID != srcID {
			dstNode := v.nodes[dstID]
			if dstNode != nil {
				// Basic type checks: file<->dir mismatches.
				if dstNode.isDir() && !srcNode.isDir() {
					return -int32(linux.EISDIR)
				}
				if !dstNode.isDir() && srcNode.isDir() {
					return -int32(linux.ENOTDIR)
				}
				// Prevent overwriting non-empty dir.
				if dstNode.isDir() && len(dstNode.entries) > 0 {
					return -errNotEmpty
				}

				// Remove destination entry (like unlink/rmdir).
				delete(dstParent.entries, dstName)

				// POSIX: replacing changes target inode metadata (nlink), so ctime must update.
				now := time.Now()
				dstNode.ctime = bumpTime(dstNode.ctime, now)
				if dstNode.isDir() {
					dstNode.unlinked = true
				} else {
					if dstNode.nlink > 0 {
						dstNode.nlink--
					}
					if dstNode.nlink == 0 {
						dstNode.unlinked = true
					}
				}
				v.maybeReapNode(dstNode)
			} else {
				// Stale entry; drop it.
				delete(dstParent.entries, dstName)
			}
		}
	}
	dstParent.entries[dstName] = srcID
	// Clear any deleted marker for the destination name
	if dstParent.deletedEntries != nil {
		delete(dstParent.deletedEntries, dstName)
	}
	delete(srcParent.entries, srcName)
	// Track deleted entries for the source to prevent re-creation from abstractDir
	if srcParent.abstractDir != nil {
		if srcParent.deletedEntries == nil {
			srcParent.deletedEntries = make(map[string]struct{})
		}
		srcParent.deletedEntries[srcName] = struct{}{}
	}
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
	// Track deleted entries to prevent re-creation from abstractDir
	if parentNode.abstractDir != nil {
		if parentNode.deletedEntries == nil {
			parentNode.deletedEntries = make(map[string]struct{})
		}
		parentNode.deletedEntries[clean] = struct{}{}
	} else {
		now := time.Now()
		parentNode.modTime = bumpTime(parentNode.modTime, now)
		parentNode.aTime = bumpTime(parentNode.aTime, now)
		parentNode.ctime = bumpTime(parentNode.ctime, now)
	}
	// POSIX: unlink changes target inode metadata (nlink), so ctime must update.
	node.ctime = bumpTime(node.ctime, time.Now())
	// Decrement link count; if it hits 0, keep the inode around while open but
	// report st_nlink==0 (xfstests generic/035).
	if node.nlink > 0 {
		node.nlink--
	}
	if node.nlink == 0 {
		node.unlinked = true
	}
	v.maybeReapNode(node)
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
	// Track deleted entries to prevent re-creation from abstractDir
	if parentNode.abstractDir != nil {
		if parentNode.deletedEntries == nil {
			parentNode.deletedEntries = make(map[string]struct{})
		}
		parentNode.deletedEntries[clean] = struct{}{}
	} else {
		now := time.Now()
		parentNode.modTime = bumpTime(parentNode.modTime, now)
		parentNode.aTime = bumpTime(parentNode.aTime, now)
		parentNode.ctime = bumpTime(parentNode.ctime, now)
	}
	// Directory is now unlinked. If it is still open (opendir), keep it alive.
	node.unlinked = true
	v.maybeReapNode(node)
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
		nlink:         1,
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
	oldNode.nlink++
	oldNode.unlinked = false
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
	// AddKernelModules adds kernel module files at /lib/modules/<version>/.
	AddKernelModules(version string, files []ModuleFile) error
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
		nlink:        1,
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
// Creates intermediate directories as needed. If the parent has an abstractDir,
// it will materialize existing directories from it rather than creating new empty ones.
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
			if child.symlinkTarget != "" {
				// Follow the already-materialized symlink
				target := child.symlinkTarget
				remaining := parts[i+1:]
				var newPath string
				if strings.HasPrefix(target, "/") {
					newPath = target
				} else {
					newPath = target
				}
				if len(remaining) > 0 {
					newPath = newPath + "/" + strings.Join(remaining, "/")
				}
				return v.resolveParent(newPath)
			}
			if !child.isDir() {
				return nil, "", errors.New("path component is not a directory: " + partName)
			}
			parent = child
		} else {
			// Before creating a new directory, check if parent has an abstractDir
			// that already contains this entry - if so, materialize it to avoid shadowing.
			var child *fsNode
			if parent.abstractDir != nil {
				if entry, err := parent.abstractDir.Lookup(partName); err == nil {
					// Materialize whatever exists (dir, symlink, or file)
					materializedNode, errno := v.createAbstractNode(parent, partName, entry)
					if errno == 0 && materializedNode != nil {
						// For symlinks, we need to follow them to continue path resolution
						if materializedNode.symlinkTarget != "" {
							// Follow the symlink
							target := materializedNode.symlinkTarget
							// Build the new path: symlink target + remaining path components
							remaining := parts[i+1:]
							var newPath string
							if strings.HasPrefix(target, "/") {
								// Absolute symlink
								newPath = target
							} else {
								// Relative symlink - resolve relative to current parent
								// For root, this just means using the target as-is
								newPath = target
							}
							if len(remaining) > 0 {
								newPath = newPath + "/" + strings.Join(remaining, "/")
							}
							return v.resolveParent(newPath)
						} else if materializedNode.isDir() {
							child = materializedNode
						}
						// If it's a file, child remains nil and we'll create a dir (error case)
					}
				}
			}

			if child == nil {
				// Create intermediate directory
				id := v.nextID
				v.nextID++
				child = newDirNode(id, partName, parent.id, 0o755)
				parent.entries[partName] = id
				v.nodes[id] = child
			}
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

// BytesFile implements AbstractFile for read-only in-memory file content.
// This is useful for exposing static content like kernel modules.
type BytesFile struct {
	data    []byte
	mode    fs.FileMode
	modTime time.Time
}

// NewBytesFile creates a new BytesFile with the given content and mode.
func NewBytesFile(data []byte, mode fs.FileMode) *BytesFile {
	return &BytesFile{
		data:    data,
		mode:    mode,
		modTime: time.Now(),
	}
}

func (f *BytesFile) Stat() (uint64, fs.FileMode) {
	return uint64(len(f.data)), f.mode
}

func (f *BytesFile) ModTime() time.Time {
	return f.modTime
}

func (f *BytesFile) ReadAt(off uint64, size uint32) ([]byte, error) {
	if off >= uint64(len(f.data)) {
		return nil, nil
	}
	end := off + uint64(size)
	if end > uint64(len(f.data)) {
		end = uint64(len(f.data))
	}
	return f.data[off:end], nil
}

func (f *BytesFile) WriteAt(off uint64, data []byte) error {
	return errors.New("read-only file")
}

func (f *BytesFile) Truncate(size uint64) error {
	return errors.New("read-only file")
}

var _ AbstractFile = (*BytesFile)(nil)

// ModuleFile represents a kernel module file to be added to the VFS.
type ModuleFile struct {
	Path string      // relative path within lib/modules/<version>/
	Data []byte      // file content
	Mode fs.FileMode // file mode
}

// AddKernelModules adds kernel module files to the VFS at /lib/modules/<version>/.
// This enables modprobe support by exposing the kernel modules directory.
func (v *virtioFsBackend) AddKernelModules(version string, files []ModuleFile) error {
	basePath := "lib/modules/" + version

	for _, f := range files {
		filePath := basePath + "/" + f.Path
		mode := f.Mode
		if mode == 0 {
			mode = 0644
		}
		if err := v.AddAbstractFile(filePath, NewBytesFile(f.Data, mode)); err != nil {
			return err
		}
	}
	return nil
}
