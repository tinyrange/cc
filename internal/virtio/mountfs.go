package virtio

import (
	"fmt"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"j5.nz/cc/internal/imagefs"
	"j5.nz/cc/internal/linuxabi"
)

const (
	linuxEBUSY = linuxabi.EBUSY
	linuxEXDEV = linuxabi.EXDEV
	linuxEROFS = linuxabi.EROFS
)

type ShareMount struct {
	GuestPath string
	Backend   FSBackend
	Writable  bool
}

type mountedFS struct {
	root   FSBackend
	mounts []shareMount

	mu         sync.RWMutex
	nextNodeID uint64
	nextHandle uint64
	nodes      map[uint64]*mountedNode
	pathToNode map[string]uint64
	handles    map[uint64]*mountedHandle
}

type shareMount struct {
	path     string
	backend  FSBackend
	writable bool
}

type mountedNode struct {
	id              uint64
	path            string
	backend         FSBackend
	backendNodeID   uint64
	backendResolved bool
}

type mountedHandle struct {
	backend FSBackend
	nodeID  uint64
	fh      uint64
	dir     bool
}

func NewMountedFS(root FSBackend, shares []ShareMount) FSBackend {
	if root == nil {
		root = NewImageFS(imagefs.NewHostFS("", nil), "")
	}
	mounts := make([]shareMount, 0, len(shares))
	for _, share := range shares {
		mountPath := cleanMountPath(share.GuestPath)
		if mountPath == "/" || share.Backend == nil {
			continue
		}
		mounts = append(mounts, shareMount{
			path:     mountPath,
			backend:  share.Backend,
			writable: share.Writable,
		})
	}
	sort.Slice(mounts, func(i, j int) bool {
		if len(mounts[i].path) == len(mounts[j].path) {
			return mounts[i].path < mounts[j].path
		}
		return len(mounts[i].path) < len(mounts[j].path)
	})
	return &mountedFS{
		root:       root,
		mounts:     mounts,
		nextNodeID: 2,
		nextHandle: 1,
		nodes: map[uint64]*mountedNode{
			1: {id: 1, path: "/"},
		},
		pathToNode: map[string]uint64{"/": 1},
		handles:    map[uint64]*mountedHandle{},
	}
}

type ShareMounter interface {
	AddShare(ShareMount) error
}

func (m *mountedFS) AddShare(share ShareMount) error {
	mountPath := cleanMountPath(share.GuestPath)
	if mountPath == "/" {
		return nil
	}
	if share.Backend == nil {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, existing := range m.mounts {
		if existing.path != mountPath {
			continue
		}
		if existing.writable != share.Writable || existing.backend != share.Backend {
			return fmt.Errorf("mount path %q is already in use", mountPath)
		}
		return nil
	}

	m.mounts = append(m.mounts, shareMount{
		path:     mountPath,
		backend:  share.Backend,
		writable: share.Writable,
	})
	sort.Slice(m.mounts, func(i, j int) bool {
		if len(m.mounts[i].path) == len(m.mounts[j].path) {
			return m.mounts[i].path < m.mounts[j].path
		}
		return len(m.mounts[i].path) < len(m.mounts[j].path)
	})
	return nil
}

func cleanMountPath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "/"
	}
	return path.Clean("/" + strings.TrimPrefix(value, "/"))
}

func (m *mountedFS) Init() (uint32, uint32) {
	return m.root.Init()
}

func (m *mountedFS) GetAttr(nodeID uint64) (FuseAttr, int32) {
	node := m.node(nodeID)
	if node == nil {
		return FuseAttr{}, -linuxENOENT
	}
	attr, errno := m.resolveAttr(node)
	if errno != 0 {
		return FuseAttr{}, errno
	}
	attr.Ino = nodeID
	return attr, 0
}

func (m *mountedFS) Lookup(parent uint64, name string) (uint64, FuseAttr, int32) {
	parentNode := m.node(parent)
	if parentNode == nil {
		return 0, FuseAttr{}, -linuxENOENT
	}
	name = path.Base(path.Clean("/" + name))
	switch name {
	case ".", "/":
		attr, errno := m.GetAttr(parent)
		return parent, attr, errno
	case "..":
		if parentNode.path == "/" {
			attr, errno := m.GetAttr(1)
			return 1, attr, errno
		}
		parentPath := path.Dir(parentNode.path)
		id := m.ensureNode(parentPath)
		attr, errno := m.GetAttr(id)
		return id, attr, errno
	}

	childPath := path.Join(parentNode.path, name)
	if m.isSyntheticPath(childPath) {
		attr := syntheticDirAttr()
		id := m.ensureNode(childPath)
		attr.Ino = id
		return id, attr, 0
	}
	backend, backendNodeID, _, errno := m.resolveBackendNode(childPath)
	if errno != 0 {
		return 0, FuseAttr{}, errno
	}
	attr, errno := backend.GetAttr(backendNodeID)
	if errno != 0 {
		return 0, FuseAttr{}, errno
	}
	id := m.ensureResolvedNode(childPath, backend, backendNodeID)
	attr.Ino = id
	return id, attr, 0
}

func (m *mountedFS) Open(nodeID uint64, flags uint32) (uint64, int32) {
	node := m.node(nodeID)
	if node == nil {
		return 0, -linuxENOENT
	}
	backend, backendNodeID, errno := m.resolveBackendNodeCached(node.path)
	if errno != 0 {
		return 0, errno
	}
	fh, errno := backend.Open(backendNodeID, flags)
	if errno != 0 {
		return 0, errno
	}
	return m.storeHandle(backend, backendNodeID, fh, false), 0
}

func (m *mountedFS) Release(_ uint64, fh uint64) {
	handle := m.takeHandle(fh, false)
	if handle == nil {
		return
	}
	handle.backend.Release(handle.nodeID, handle.fh)
}

func (m *mountedFS) Read(nodeID uint64, fh uint64, off uint64, size uint32) ([]byte, int32) {
	handle := m.handle(fh, false)
	if handle == nil {
		return nil, -linuxEBADF
	}
	return handle.backend.Read(handle.nodeID, handle.fh, off, size)
}

func (m *mountedFS) GetXattr(nodeID uint64, name string) ([]byte, int32) {
	node := m.node(nodeID)
	if node == nil {
		return nil, -linuxENOENT
	}
	backend, backendNodeID, errno := m.resolveBackendNodeCached(node.path)
	if errno != 0 {
		return nil, errno
	}
	xattrBackend, ok := backend.(fsXattrBackend)
	if !ok {
		return nil, -linuxENODATA
	}
	return xattrBackend.GetXattr(backendNodeID, name)
}

func (m *mountedFS) ListXattr(nodeID uint64) ([]byte, int32) {
	node := m.node(nodeID)
	if node == nil {
		return nil, -linuxENOENT
	}
	backend, backendNodeID, errno := m.resolveBackendNodeCached(node.path)
	if errno != 0 {
		return nil, errno
	}
	xattrBackend, ok := backend.(fsXattrBackend)
	if !ok {
		return nil, 0
	}
	return xattrBackend.ListXattr(backendNodeID)
}

func (m *mountedFS) OpenDir(nodeID uint64, flags uint32) (uint64, int32) {
	node := m.node(nodeID)
	if node == nil {
		return 0, -linuxENOENT
	}
	backend, backendNodeID, errno := m.resolveBackendNodeCached(node.path)
	if errno == 0 {
		fh, errno := backend.OpenDir(backendNodeID, flags)
		if errno != 0 {
			return 0, errno
		}
		return m.storeHandle(backend, backendNodeID, fh, true), 0
	}
	if errno != -linuxENOENT || !m.isSyntheticPath(node.path) {
		return 0, errno
	}
	return m.storeHandle(nil, 0, 0, true), 0
}

func (m *mountedFS) ReadDir(nodeID uint64, fh uint64, off uint64, maxBytes uint32) ([]byte, int32) {
	node := m.node(nodeID)
	if node == nil {
		return nil, -linuxENOENT
	}
	handle := m.handle(fh, true)
	if handle == nil {
		return nil, -linuxEBADF
	}

	entries := []dirEntry{
		{name: ".", typ: dirTypeDir, ino: nodeID},
		{name: "..", typ: dirTypeDir, ino: m.ensureNode(parentPath(node.path))},
	}

	names := map[string]dirEntry{}
	if handle.backend != nil {
		children, errno := m.readBackendDir(handle.backend, handle.nodeID, handle.fh)
		if errno != 0 {
			return nil, errno
		}
		for _, child := range children {
			if child.name == "." || child.name == ".." {
				continue
			}
			childPath := path.Join(node.path, child.name)
			childID := m.ensureResolvedNode(childPath, handle.backend, child.ino)
			child.ino = childID
			names[child.name] = child
		}
	}
	for _, mountChild := range m.mountChildren(node.path) {
		childID := m.ensureNode(path.Join(node.path, mountChild))
		names[mountChild] = dirEntry{name: mountChild, typ: dirTypeDir, ino: childID}
	}
	sorted := make([]string, 0, len(names))
	for name := range names {
		sorted = append(sorted, name)
	}
	sort.Strings(sorted)
	for _, name := range sorted {
		entries = append(entries, names[name])
	}
	return encodeDirEntries(entries, off, maxBytes), 0
}

func (m *mountedFS) ReleaseDir(_ uint64, fh uint64) {
	handle := m.takeHandle(fh, true)
	if handle == nil || handle.backend == nil {
		return
	}
	handle.backend.ReleaseDir(handle.nodeID, handle.fh)
}

func (m *mountedFS) Readlink(nodeID uint64) (string, int32) {
	node := m.node(nodeID)
	if node == nil {
		return "", -linuxENOENT
	}
	backend, backendNodeID, errno := m.resolveBackendNodeCached(node.path)
	if errno != 0 {
		return "", errno
	}
	return backend.Readlink(backendNodeID)
}

func (m *mountedFS) StatFS(nodeID uint64) (uint64, uint64, uint64, uint64, uint64, uint64, uint64, uint64, int32) {
	node := m.node(nodeID)
	if node == nil {
		return 0, 0, 0, 0, 0, 0, 0, 0, -linuxENOENT
	}
	backend, backendNodeID, errno := m.resolveBackendNodeCached(node.path)
	if errno != 0 {
		return m.root.StatFS(1)
	}
	return backend.StatFS(backendNodeID)
}

func (m *mountedFS) Mkdir(parent uint64, name string, mode uint32) (uint64, FuseAttr, int32) {
	parentNode := m.node(parent)
	if parentNode == nil {
		return 0, FuseAttr{}, -linuxENOENT
	}
	childPath := path.Join(parentNode.path, path.Base(path.Clean("/"+name)))
	backend, backendParent, mount, errno := m.resolveBackendNode(parentNode.path)
	if errno != 0 {
		return 0, FuseAttr{}, errno
	}
	if mount == nil || !mount.writable {
		return 0, FuseAttr{}, -linuxEROFS
	}
	mkdirBackend, ok := backend.(fsMkdirBackend)
	if !ok {
		return 0, FuseAttr{}, -linuxEROFS
	}
	nodeID, attr, errno := mkdirBackend.Mkdir(backendParent, name, mode)
	if errno != 0 {
		return 0, FuseAttr{}, errno
	}
	id := m.ensureResolvedNode(childPath, backend, nodeID)
	attr.Ino = id
	return id, attr, 0
}

func (m *mountedFS) RmDir(parent uint64, name string) int32 {
	parentNode := m.node(parent)
	if parentNode == nil {
		return -linuxENOENT
	}
	childPath := path.Join(parentNode.path, path.Base(path.Clean("/"+name)))
	if m.isMountPath(childPath) || m.isSyntheticPath(childPath) {
		return -linuxEBUSY
	}
	backend, backendParent, mount, errno := m.resolveBackendNode(parentNode.path)
	if errno != 0 {
		return errno
	}
	if mount == nil || !mount.writable {
		return -linuxEROFS
	}
	rmBackend, ok := backend.(fsRmDirBackend)
	if !ok {
		return -linuxEROFS
	}
	if errno := rmBackend.RmDir(backendParent, name); errno != 0 {
		return errno
	}
	m.removeNode(childPath)
	return 0
}

func (m *mountedFS) Create(parent uint64, name string, flags uint32, mode uint32) (uint64, uint64, FuseAttr, int32) {
	parentNode := m.node(parent)
	if parentNode == nil {
		return 0, 0, FuseAttr{}, -linuxENOENT
	}
	childName, ok := cleanChildName(name)
	if !ok {
		return 0, 0, FuseAttr{}, -linuxEINVAL
	}
	childPath := path.Join(parentNode.path, path.Base(childName))
	if m.isMountPath(childPath) || m.isSyntheticPath(childPath) {
		return 0, 0, FuseAttr{}, -linuxEEXIST
	}
	backend, backendParent, mount, errno := m.resolveBackendNode(parentNode.path)
	if errno != 0 {
		return 0, 0, FuseAttr{}, errno
	}
	if mount == nil || !mount.writable {
		return 0, 0, FuseAttr{}, -linuxEROFS
	}
	createBackend, ok := backend.(fsCreateBackend)
	if !ok {
		return 0, 0, FuseAttr{}, -linuxEROFS
	}
	backendNodeID, fh, attr, errno := createBackend.Create(backendParent, childName, flags, mode)
	if errno != 0 {
		return 0, 0, FuseAttr{}, errno
	}
	id := m.ensureResolvedNode(childPath, backend, backendNodeID)
	attr.Ino = id
	return id, m.storeHandle(backend, backendNodeID, fh, false), attr, 0
}

func (m *mountedFS) Write(nodeID uint64, fh uint64, off uint64, data []byte, flags uint32) (uint32, int32) {
	handle := m.handle(fh, false)
	if handle == nil {
		return 0, -linuxEBADF
	}
	writeBackend, ok := handle.backend.(fsWriteBackend)
	if !ok {
		return 0, -linuxEROFS
	}
	return writeBackend.Write(handle.nodeID, handle.fh, off, data, flags)
}

func (m *mountedFS) SetAttr(nodeID uint64, valid uint32, fh uint64, size uint64, mode uint32, uid uint32, gid uint32, atime time.Time, mtime time.Time) (FuseAttr, int32) {
	node := m.node(nodeID)
	if node == nil {
		return FuseAttr{}, -linuxENOENT
	}
	backend, backendNodeID, mount, errno := m.resolveBackendNode(node.path)
	if errno != 0 {
		return FuseAttr{}, errno
	}
	if mount == nil || !mount.writable {
		return FuseAttr{}, -linuxEROFS
	}
	setAttrBackend, ok := backend.(fsSetAttrBackend)
	if !ok {
		return FuseAttr{}, -linuxEROFS
	}
	var backendFH uint64
	if valid&fattrFH != 0 {
		handle := m.handle(fh, false)
		if handle == nil {
			return FuseAttr{}, -linuxEBADF
		}
		backendFH = handle.fh
	}
	attr, errno := setAttrBackend.SetAttr(backendNodeID, valid, backendFH, size, mode, uid, gid, atime, mtime)
	if errno != 0 {
		return FuseAttr{}, errno
	}
	attr.Ino = nodeID
	return attr, 0
}

func (m *mountedFS) Unlink(parent uint64, name string) int32 {
	parentNode := m.node(parent)
	if parentNode == nil {
		return -linuxENOENT
	}
	childPath := path.Join(parentNode.path, path.Base(path.Clean("/"+name)))
	if m.isMountPath(childPath) || m.isSyntheticPath(childPath) {
		return -linuxEBUSY
	}
	backend, backendParent, mount, errno := m.resolveBackendNode(parentNode.path)
	if errno != 0 {
		return errno
	}
	if mount == nil || !mount.writable {
		return -linuxEROFS
	}
	unlinkBackend, ok := backend.(fsUnlinkBackend)
	if !ok {
		return -linuxEROFS
	}
	if errno := unlinkBackend.Unlink(backendParent, name); errno != 0 {
		return errno
	}
	m.removeNode(childPath)
	return 0
}

func (m *mountedFS) Rename(parent uint64, name string, newParent uint64, newName string, flags uint32) int32 {
	oldParentNode := m.node(parent)
	newParentNode := m.node(newParent)
	if oldParentNode == nil || newParentNode == nil {
		return -linuxENOENT
	}
	oldPath := path.Join(oldParentNode.path, path.Base(path.Clean("/"+name)))
	newPath := path.Join(newParentNode.path, path.Base(path.Clean("/"+newName)))
	if m.isMountPath(oldPath) || m.isSyntheticPath(oldPath) || m.isMountPath(newPath) || m.isSyntheticPath(newPath) {
		return -linuxEBUSY
	}
	oldBackend, oldParentID, oldMount, errno := m.resolveBackendNode(oldParentNode.path)
	if errno != 0 {
		return errno
	}
	newBackend, newParentID, newMount, errno := m.resolveBackendNode(newParentNode.path)
	if errno != 0 {
		return errno
	}
	if oldMount == nil || !oldMount.writable || newMount == nil || !newMount.writable || oldMount.path != newMount.path || oldBackend != newBackend {
		return -linuxEXDEV
	}
	renameBackend, ok := oldBackend.(fsRenameBackend)
	if !ok {
		return -linuxEROFS
	}
	if errno := renameBackend.Rename(oldParentID, name, newParentID, newName, flags); errno != 0 {
		return errno
	}
	m.renameNode(oldPath, newPath)
	return 0
}

func (m *mountedFS) Flush(nodeID uint64, fh uint64, lockOwner uint64) int32 {
	handle := m.handle(fh, false)
	if handle == nil {
		return -linuxEBADF
	}
	flushBackend, ok := handle.backend.(fsFlushBackend)
	if !ok {
		return 0
	}
	return flushBackend.Flush(handle.nodeID, handle.fh, lockOwner)
}

func (m *mountedFS) Fsync(nodeID uint64, fh uint64, flags uint32) int32 {
	handle := m.handle(fh, false)
	if handle == nil {
		return -linuxEBADF
	}
	fsyncBackend, ok := handle.backend.(fsFsyncBackend)
	if !ok {
		return 0
	}
	return fsyncBackend.Fsync(handle.nodeID, handle.fh, flags)
}

func (m *mountedFS) FsyncDir(nodeID uint64, fh uint64, flags uint32) int32 {
	handle := m.handle(fh, true)
	if handle == nil {
		return -linuxEBADF
	}
	fsyncBackend, ok := handle.backend.(fsFsyncDirBackend)
	if !ok {
		return 0
	}
	return fsyncBackend.FsyncDir(handle.nodeID, handle.fh, flags)
}

func (m *mountedFS) Lseek(nodeID uint64, fh uint64, offset uint64, whence uint32) (uint64, int32) {
	handle := m.handle(fh, false)
	if handle == nil {
		return 0, -linuxEBADF
	}
	lseekBackend, ok := handle.backend.(fsLseekBackend)
	if !ok {
		return 0, -linuxEINVAL
	}
	return lseekBackend.Lseek(handle.nodeID, handle.fh, offset, whence)
}

func (m *mountedFS) resolveAttr(node *mountedNode) (FuseAttr, int32) {
	if node.path == "/" {
		return m.root.GetAttr(1)
	}
	if m.isSyntheticPath(node.path) {
		return syntheticDirAttr(), 0
	}
	backend, nodeID, errno := m.resolveBackendNodeCached(node.path)
	if errno != 0 {
		return FuseAttr{}, errno
	}
	return backend.GetAttr(nodeID)
}

func (m *mountedFS) resolveBackendNodeCached(guestPath string) (FSBackend, uint64, int32) {
	guestPath = cleanMountPath(guestPath)
	if guestPath == "/" {
		return m.root, 1, 0
	}
	if node := m.nodeForPath(guestPath); node != nil && node.backendResolved {
		return node.backend, node.backendNodeID, 0
	}
	backend, nodeID, _, errno := m.resolveBackendNode(guestPath)
	return backend, nodeID, errno
}

func (m *mountedFS) resolveBackendNode(guestPath string) (FSBackend, uint64, *shareMount, int32) {
	guestPath = cleanMountPath(guestPath)
	if guestPath == "/" {
		return m.root, 1, nil, 0
	}
	mount := m.mountForPath(guestPath)
	if node := m.nodeForPath(guestPath); node != nil && node.backendResolved {
		return node.backend, node.backendNodeID, mount, 0
	}
	if mount == nil {
		nodeID, _, errno := backendLookupPath(m.root, guestPath)
		return m.root, nodeID, nil, errno
	}
	rel := strings.TrimPrefix(strings.TrimPrefix(guestPath, mount.path), "/")
	if rel == "" {
		return mount.backend, 1, mount, 0
	}
	nodeID, _, errno := backendLookupPath(mount.backend, "/"+rel)
	return mount.backend, nodeID, mount, errno
}

func (m *mountedFS) mountForPath(guestPath string) *shareMount {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var best *shareMount
	for i := range m.mounts {
		mount := &m.mounts[i]
		if guestPath == mount.path || strings.HasPrefix(guestPath, mount.path+"/") {
			if best == nil || len(mount.path) > len(best.path) {
				best = mount
			}
		}
	}
	if best == nil {
		return nil
	}
	copy := *best
	return &copy
}

func (m *mountedFS) isMountPath(guestPath string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for i := range m.mounts {
		if m.mounts[i].path == guestPath {
			return true
		}
	}
	return false
}

func (m *mountedFS) isSyntheticPath(guestPath string) bool {
	if guestPath == "/" {
		return false
	}
	if m.isMountPath(guestPath) {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for i := range m.mounts {
		mount := m.mounts[i].path
		if strings.HasPrefix(mount, guestPath+"/") {
			return true
		}
	}
	return false
}

func (m *mountedFS) mountChildren(parent string) []string {
	parent = cleanMountPath(parent)
	m.mu.RLock()
	defer m.mu.RUnlock()
	set := map[string]struct{}{}
	for _, mount := range m.mounts {
		if parent == "/" {
			trimmed := strings.TrimPrefix(mount.path, "/")
			if trimmed == "" {
				continue
			}
			name := strings.Split(trimmed, "/")[0]
			set[name] = struct{}{}
			continue
		}
		if !strings.HasPrefix(mount.path, parent+"/") {
			continue
		}
		rel := strings.TrimPrefix(mount.path, parent+"/")
		if rel == "" {
			continue
		}
		name := strings.Split(rel, "/")[0]
		set[name] = struct{}{}
	}
	names := make([]string, 0, len(set))
	for name := range set {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (m *mountedFS) readBackendDir(backend FSBackend, nodeID uint64, fh uint64) ([]dirEntry, int32) {
	data, errno := backend.ReadDir(nodeID, fh, 0, 1<<20)
	if errno != 0 {
		return nil, errno
	}
	return decodeDirEntries(data), 0
}

func encodeDirEntries(entries []dirEntry, off uint64, maxBytes uint32) []byte {
	var out []byte
	for i := int(off); i < len(entries); i++ {
		entry := entries[i]
		nameBytes := []byte(entry.name)
		reclen := align8(fuseDirentBaseSize + len(nameBytes))
		if len(out)+reclen > int(maxBytes) {
			break
		}
		start := len(out)
		out = append(out, make([]byte, reclen)...)
		putLE64(out[start:start+8], entry.ino)
		putLE64(out[start+8:start+16], uint64(i+1))
		putLE32(out[start+16:start+20], uint32(len(nameBytes)))
		putLE32(out[start+20:start+24], entry.typ)
		copy(out[start+24:start+24+len(nameBytes)], nameBytes)
	}
	return out
}

func decodeDirEntries(data []byte) []dirEntry {
	var entries []dirEntry
	for len(data) >= fuseDirentBaseSize {
		ino := readLE64(data[0:8])
		nameLen := int(readLE32(data[16:20]))
		typ := readLE32(data[20:24])
		reclen := align8(fuseDirentBaseSize + nameLen)
		if nameLen < 0 || len(data) < reclen {
			break
		}
		name := string(data[24 : 24+nameLen])
		entries = append(entries, dirEntry{name: name, typ: typ, ino: ino})
		data = data[reclen:]
	}
	return entries
}

func backendLookupPath(backend FSBackend, guestPath string) (uint64, FuseAttr, int32) {
	guestPath = cleanMountPath(guestPath)
	if guestPath == "/" {
		attr, errno := backend.GetAttr(1)
		return 1, attr, errno
	}
	nodeID := uint64(1)
	var attr FuseAttr
	for _, part := range strings.Split(strings.TrimPrefix(guestPath, "/"), "/") {
		var errno int32
		nodeID, attr, errno = backend.Lookup(nodeID, part)
		if errno != 0 {
			return 0, FuseAttr{}, errno
		}
	}
	return nodeID, attr, 0
}

func syntheticDirAttr() FuseAttr {
	modTime := time.Unix(0, 0)
	return FuseAttr{
		ATimeSec:  uint64(modTime.Unix()),
		MTimeSec:  uint64(modTime.Unix()),
		CTimeSec:  uint64(modTime.Unix()),
		ATimeNsec: uint32(modTime.Nanosecond()),
		MTimeNsec: uint32(modTime.Nanosecond()),
		CTimeNsec: uint32(modTime.Nanosecond()),
		Mode:      linuxSIFDIR | 0o755,
		NLink:     2,
		UID:       0,
		GID:       0,
		BlkSize:   4096,
	}
}

func (m *mountedFS) node(id uint64) *mountedNode {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.nodes[id]
}

func (m *mountedFS) ensureNode(guestPath string) uint64 {
	return m.ensureResolvedNode(guestPath, nil, 0)
}

func (m *mountedFS) ensureResolvedNode(guestPath string, backend FSBackend, backendNodeID uint64) uint64 {
	guestPath = cleanMountPath(guestPath)
	m.mu.Lock()
	defer m.mu.Unlock()
	if id, ok := m.pathToNode[guestPath]; ok {
		if node := m.nodes[id]; node != nil && backend != nil {
			node.backend = backend
			node.backendNodeID = backendNodeID
			node.backendResolved = true
		}
		return id
	}
	id := m.nextNodeID
	m.nextNodeID++
	m.nodes[id] = &mountedNode{
		id:              id,
		path:            guestPath,
		backend:         backend,
		backendNodeID:   backendNodeID,
		backendResolved: backend != nil,
	}
	m.pathToNode[guestPath] = id
	return id
}

func (m *mountedFS) nodeForPath(guestPath string) *mountedNode {
	guestPath = cleanMountPath(guestPath)
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.nodes[m.pathToNode[guestPath]]
}

func (m *mountedFS) removeNode(guestPath string) {
	guestPath = cleanMountPath(guestPath)
	m.mu.Lock()
	defer m.mu.Unlock()
	id, ok := m.pathToNode[guestPath]
	if !ok {
		return
	}
	delete(m.pathToNode, guestPath)
	delete(m.nodes, id)
}

func (m *mountedFS) renameNode(oldPath, newPath string) {
	oldPath = cleanMountPath(oldPath)
	newPath = cleanMountPath(newPath)
	m.mu.Lock()
	defer m.mu.Unlock()
	id, ok := m.pathToNode[oldPath]
	if !ok {
		return
	}
	delete(m.pathToNode, oldPath)
	m.pathToNode[newPath] = id
	if node := m.nodes[id]; node != nil {
		node.path = newPath
	}
}

func (m *mountedFS) storeHandle(backend FSBackend, nodeID uint64, fh uint64, dir bool) uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := m.nextHandle
	m.nextHandle++
	m.handles[id] = &mountedHandle{backend: backend, nodeID: nodeID, fh: fh, dir: dir}
	return id
}

func (m *mountedFS) handle(id uint64, dir bool) *mountedHandle {
	m.mu.RLock()
	defer m.mu.RUnlock()
	handle := m.handles[id]
	if handle == nil || handle.dir != dir {
		return nil
	}
	return handle
}

func (m *mountedFS) takeHandle(id uint64, dir bool) *mountedHandle {
	m.mu.Lock()
	defer m.mu.Unlock()
	handle := m.handles[id]
	if handle == nil || handle.dir != dir {
		return nil
	}
	delete(m.handles, id)
	return handle
}

func parentPath(guestPath string) string {
	guestPath = cleanMountPath(guestPath)
	if guestPath == "/" {
		return "/"
	}
	return path.Dir(guestPath)
}

func putLE32(dst []byte, v uint32) {
	dst[0] = byte(v)
	dst[1] = byte(v >> 8)
	dst[2] = byte(v >> 16)
	dst[3] = byte(v >> 24)
}

func putLE64(dst []byte, v uint64) {
	putLE32(dst[0:4], uint32(v))
	putLE32(dst[4:8], uint32(v>>32))
}

func readLE32(src []byte) uint32 {
	return uint32(src[0]) | uint32(src[1])<<8 | uint32(src[2])<<16 | uint32(src[3])<<24
}

func readLE64(src []byte) uint64 {
	return uint64(readLE32(src[0:4])) | uint64(readLE32(src[4:8]))<<32
}

var _ FSBackend = (*mountedFS)(nil)
var _ ShareMounter = (*mountedFS)(nil)
var _ fsMkdirBackend = (*mountedFS)(nil)
var _ fsRmDirBackend = (*mountedFS)(nil)
var _ fsCreateBackend = (*mountedFS)(nil)
var _ fsWriteBackend = (*mountedFS)(nil)
var _ fsSetAttrBackend = (*mountedFS)(nil)
var _ fsUnlinkBackend = (*mountedFS)(nil)
var _ fsRenameBackend = (*mountedFS)(nil)
var _ fsFlushBackend = (*mountedFS)(nil)
var _ fsFsyncBackend = (*mountedFS)(nil)
var _ fsFsyncDirBackend = (*mountedFS)(nil)
var _ fsLseekBackend = (*mountedFS)(nil)
