package virtio

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"j5.nz/cc/internal/fsmeta"
	"j5.nz/cc/internal/imagefs"
	"j5.nz/cc/internal/linuxabi"
)

type passthroughFS struct {
	root           string
	meta           map[string]fsmeta.Entry
	writebackCache bool
	ownerUID       uint32
	ownerGID       uint32
	mapOwner       bool

	mu         sync.RWMutex
	nextNodeID uint64
	nextHandle uint64
	nodes      map[uint64]string
	pathToNode map[string]uint64
	handles    map[uint64]*passthroughHandle
	dirHandles map[uint64][]dirEntry
}

type passthroughHandle struct {
	nodeID uint64
	file   *os.File
	append bool
}

type imageFS struct {
	root       string
	dataStore  *imageDataStore
	ownerUID   uint32
	ownerGID   uint32
	mapOwner   bool
	debugPaths []string
	debugLog   io.Writer

	mu                    sync.Mutex
	nextNodeID            uint64
	nextHandle            uint64
	nodes                 map[uint64]*imageNode
	handles               map[uint64]imageHandle
	dirHandles            map[uint64][]dirEntry
	dirHandleNodes        map[uint64]uint64
	xattrBytes            uint64
	metadataHighWater     uint64
	retainedNodes         int
	retainedHandles       int
	retainedDirHandles    int
	retainedEntries       int
	retainedWhiteouts     int
	dynamicMetadata       uint64
	materializations      map[uint64]*imageDirMaterialization
	materializationCtx    context.Context
	materializationCancel context.CancelFunc
	materializationWG     sync.WaitGroup
	beginClose            sync.Once
	closeStart            sync.Once
	closeDone             chan struct{}
	closeErr              error
	closed                bool
}

type imageHandle struct {
	nodeID uint64
	reader io.ReaderAt
	closer io.Closer
}

type imageNode struct {
	id                uint64
	inode             uint64
	parent            uint64
	name              string
	mode              fs.FileMode
	rawMode           uint32
	uid               uint32
	gid               uint32
	rdev              uint32
	size              uint64
	nlink             uint32
	data              sparseImageData
	symlinkTarget     string
	entries           map[string]uint64
	whiteouts         map[string]bool
	retainedEntries   int
	retainedWhiteouts int
	accountedMetadata uint64
	entriesDone       bool
	atime             time.Time
	modTime           time.Time
	ctime             time.Time
	xattrs            map[string][]byte
	abstractFile      imagefs.File
	// lowerFile remains immutable after the first writable operation. data is
	// a page overlay, so metadata changes and small writes do not eagerly copy
	// the lower file or expand its sparse holes.
	lowerFile    imagefs.File
	lowerSize    uint64
	abstractDir  imagefs.Directory
	abstractLink imagefs.Symlink
}

const imageDataPageSize = uint64(4096)

const (
	imageXattrEntryOverhead   = 64
	imageMaxXattrBytesPerNode = 256 << 10
	imageMaxXattrBytes        = 16 << 20
)

// sparseImageData stores only runs of pages which have actually been written.
// Both logical page indexes and allocation tokens are sequential for ordinary
// writes, so an extent avoids one heap map entry per 4 KiB page while retaining
// sparse/random-write semantics.
type sparseImageData struct {
	extents []imageDataExtent
}

type imageDataExtent struct {
	page     uint64
	location uint64
	count    uint64
}

func (d sparseImageData) location(page uint64) (uint64, bool) {
	i := sort.Search(len(d.extents), func(i int) bool {
		return d.extents[i].page+d.extents[i].count > page
	})
	if i >= len(d.extents) || page < d.extents[i].page {
		return 0, false
	}
	extent := d.extents[i]
	return extent.location + page - extent.page, true
}

func (d sparseImageData) nextDataPage(page uint64) (uint64, bool) {
	i := sort.Search(len(d.extents), func(i int) bool {
		return d.extents[i].page+d.extents[i].count > page
	})
	if i >= len(d.extents) {
		return 0, false
	}
	if page < d.extents[i].page {
		return d.extents[i].page, true
	}
	return page, true
}

func (d sparseImageData) nextHolePage(page uint64) uint64 {
	i := sort.Search(len(d.extents), func(i int) bool {
		return d.extents[i].page+d.extents[i].count > page
	})
	if i >= len(d.extents) || page < d.extents[i].page {
		return page
	}
	return d.extents[i].page + d.extents[i].count
}

func (d *sparseImageData) insert(page, location uint64) {
	i := sort.Search(len(d.extents), func(i int) bool { return d.extents[i].page >= page })
	if i > 0 {
		previous := &d.extents[i-1]
		if previous.page+previous.count == page && previous.location+previous.count == location {
			previous.count++
			if i < len(d.extents) && page+1 == d.extents[i].page && location+1 == d.extents[i].location {
				previous.count += d.extents[i].count
				d.extents = append(d.extents[:i], d.extents[i+1:]...)
			}
			return
		}
	}
	if i < len(d.extents) && page+1 == d.extents[i].page && location+1 == d.extents[i].location {
		d.extents[i].page = page
		d.extents[i].location = location
		d.extents[i].count++
		return
	}
	d.extents = append(d.extents, imageDataExtent{})
	copy(d.extents[i+1:], d.extents[i:])
	d.extents[i] = imageDataExtent{page: page, location: location, count: 1}
}

func (d *sparseImageData) replace(page, location uint64) {
	for i, extent := range d.extents {
		if page < extent.page || page >= extent.page+extent.count {
			continue
		}
		rebuilt := make([]imageDataExtent, 0, len(d.extents)+1)
		rebuilt = append(rebuilt, d.extents[:i]...)
		if left := page - extent.page; left != 0 {
			rebuilt = append(rebuilt, imageDataExtent{page: extent.page, location: extent.location, count: left})
		}
		if right := extent.page + extent.count - page - 1; right != 0 {
			rebuilt = append(rebuilt, imageDataExtent{page: page + 1, location: extent.location + (page - extent.page) + 1, count: right})
		}
		rebuilt = append(rebuilt, d.extents[i+1:]...)
		d.extents = rebuilt
		d.insert(page, location)
		return
	}
	d.insert(page, location)
}

func (d sparseImageData) readAt(store *imageDataStore, dst []byte, off uint64) error {
	for len(dst) > 0 {
		pageIndex := off / imageDataPageSize
		pageOffset := off % imageDataPageSize
		n := min(len(dst), int(imageDataPageSize-pageOffset))
		if location, ok := d.location(pageIndex); ok {
			var page [imageDataPageSize]byte
			if err := store.readPage(location, page[:]); err != nil {
				return err
			}
			copy(dst[:n], page[pageOffset:pageOffset+uint64(n)])
		}
		dst = dst[n:]
		off += uint64(n)
	}
	return nil
}

func (d *sparseImageData) writeAt(store *imageDataStore, src []byte, off uint64) (int, error) {
	written := 0
	for len(src) > 0 {
		pageIndex := off / imageDataPageSize
		pageOffset := off % imageDataPageSize
		n := min(len(src), int(imageDataPageSize-pageOffset))
		location, ok := d.location(pageIndex)
		if !ok {
			var page [imageDataPageSize]byte
			copy(page[pageOffset:pageOffset+uint64(n)], src[:n])
			var err error
			location, err = store.allocatePage(page[:])
			if err != nil {
				return written, err
			}
			d.insert(pageIndex, location)
		} else {
			newLocation, err := store.writeAtCOW(location, pageOffset, src[:n])
			if err != nil {
				return written, err
			}
			if newLocation != location {
				d.replace(pageIndex, newLocation)
			}
		}
		src = src[n:]
		off += uint64(n)
		written += n
	}
	return written, nil
}

func (d *sparseImageData) truncate(store *imageDataStore, size uint64) error {
	keepPages := size / imageDataPageSize
	if size%imageDataPageSize != 0 {
		keepPages++
	}
	kept := d.extents[:0]
	for _, extent := range d.extents {
		keep := min(extent.count, max(uint64(0), keepPages-min(keepPages, extent.page)))
		if extent.page >= keepPages {
			keep = 0
		}
		for page := keep; page < extent.count; page++ {
			store.releasePage(extent.location + page)
		}
		if keep != 0 {
			extent.count = keep
			kept = append(kept, extent)
		}
	}
	d.extents = kept
	if size == 0 || size%imageDataPageSize == 0 {
		return nil
	}
	pageIndex := size / imageDataPageSize
	if location, ok := d.location(pageIndex); ok {
		var zero [imageDataPageSize]byte
		newLocation, err := store.writeAtCOW(location, size%imageDataPageSize, zero[:imageDataPageSize-size%imageDataPageSize])
		if err != nil {
			return err
		}
		if newLocation != location {
			d.replace(pageIndex, newLocation)
		}
	}
	return nil
}

func (d sparseImageData) release(store *imageDataStore) {
	for _, extent := range d.extents {
		for page := uint64(0); page < extent.count; page++ {
			store.releasePage(extent.location + page)
		}
	}
}

func (d sparseImageData) allocatedBytes(size uint64) uint64 {
	var allocated uint64
	fullPages := size / imageDataPageSize
	partialBytes := size % imageDataPageSize
	for _, extent := range d.extents {
		if extent.page < fullPages {
			pages := min(extent.count, fullPages-extent.page)
			allocated += pages * imageDataPageSize
		}
		if partialBytes != 0 && extent.page <= fullPages && fullPages-extent.page < extent.count {
			allocated += partialBytes
		}
	}
	return allocated
}

type dirEntry struct {
	name string
	typ  uint32
	ino  uint64
}

func NewPassthroughFS(root string, meta map[string]fsmeta.Entry) FSBackend {
	return newPassthroughFS(root, meta, 0, 0, false)
}

func NewPassthroughFSWithOwner(root string, meta map[string]fsmeta.Entry, uid, gid uint32) FSBackend {
	return newPassthroughFS(root, meta, uid, gid, true)
}

func newPassthroughFS(root string, meta map[string]fsmeta.Entry, uid, gid uint32, mapOwner bool) FSBackend {
	fs := &passthroughFS{
		root:       root,
		meta:       meta,
		ownerUID:   uid,
		ownerGID:   gid,
		mapOwner:   mapOwner,
		nextNodeID: 2,
		nextHandle: 1,
		nodes:      map[uint64]string{1: "/"},
		pathToNode: map[string]uint64{"/": 1},
		handles:    map[uint64]*passthroughHandle{},
		dirHandles: map[uint64][]dirEntry{},
	}
	return fs
}

func NewImageFS(root imagefs.Directory, statfsPath string) FSBackend {
	return newImageFS(root, statfsPath, 0, 0, false)
}

func NewImageFSWithOwner(root imagefs.Directory, statfsPath string, uid, gid uint32) FSBackend {
	return newImageFS(root, statfsPath, uid, gid, true)
}

func newImageFS(root imagefs.Directory, statfsPath string, uid, gid uint32, mapOwner bool) FSBackend {
	materializationCtx, materializationCancel := context.WithCancel(context.Background())
	imgFS := &imageFS{
		dataStore:             newImageDataStore(),
		ownerUID:              uid,
		ownerGID:              gid,
		mapOwner:              mapOwner,
		debugPaths:            virtioFSDebugPathsFromEnv(),
		debugLog:              os.Stderr,
		nextNodeID:            2,
		nextHandle:            1,
		nodes:                 map[uint64]*imageNode{},
		handles:               map[uint64]imageHandle{},
		dirHandles:            map[uint64][]dirEntry{},
		dirHandleNodes:        map[uint64]uint64{},
		retainedNodes:         1,
		materializationCtx:    materializationCtx,
		materializationCancel: materializationCancel,
	}
	imgFS.root = imgFS.dataStore.dir
	if root == nil {
		root = imagefs.NewHostFS("", nil)
	}
	rootMode := fs.ModeDir | root.Stat()
	rootUID, rootGID := root.Owner()
	rootRDev := root.RDev()
	rootModTime := root.ModTime()
	if rootModTime.IsZero() {
		rootModTime = time.Unix(0, 0)
	}
	imgFS.nodes[1] = &imageNode{
		id:          1,
		parent:      1,
		name:        "/",
		mode:        rootMode,
		uid:         rootUID,
		gid:         rootGID,
		rdev:        rootRDev,
		entries:     map[string]uint64{},
		modTime:     rootModTime,
		abstractDir: root,
	}
	imgFS.refreshImageNodeMetadataLocked(imgFS.nodes[1])
	return imgFS
}

func (p *passthroughFS) Init() (uint32, uint32) {
	return 128 << 10, 0
}

func (p *passthroughFS) SetWritebackCache(enabled bool) {
	p.mu.Lock()
	p.writebackCache = enabled
	p.mu.Unlock()
}

func (p *passthroughFS) GetAttr(nodeID uint64) (FuseAttr, int32) {
	p.logNode("getattr", nodeID)
	host, errno := p.hostPath(nodeID)
	if errno != 0 {
		return FuseAttr{}, errno
	}
	info, err := os.Lstat(host)
	if err != nil {
		return FuseAttr{}, errnoFromError(err)
	}
	return p.fileAttr(nodeID, host, info), 0
}

func (p *passthroughFS) Lookup(parent uint64, name string) (uint64, FuseAttr, int32) {
	p.logNode("lookup-parent", parent)
	hostParent, guestParent, errno := p.hostAndGuestPath(parent)
	if errno != 0 {
		return 0, FuseAttr{}, errno
	}
	switch name {
	case ".":
		attr, errno := p.GetAttr(parent)
		return parent, attr, errno
	case "..":
		guestPath := path.Dir(guestParent)
		if guestPath == "." {
			guestPath = "/"
		}
		nodeID := p.ensureNode(guestPath)
		attr, errno := p.GetAttr(nodeID)
		return nodeID, attr, errno
	}
	rel, ok := cleanChildName(name)
	if !ok {
		return 0, FuseAttr{}, -linuxEINVAL
	}
	host := filepath.Join(hostParent, filepath.FromSlash(rel))
	info, err := os.Lstat(host)
	if err != nil {
		return 0, FuseAttr{}, errnoFromError(err)
	}
	guestPath := joinGuestChild(guestParent, rel)
	if p.root != "" {
		p.logf("lookup name=%q guest=%q host=%q", name, guestPath, host)
	}
	nodeID := p.ensureNode(guestPath)
	return nodeID, p.fileAttr(nodeID, host, info), 0
}

func (p *passthroughFS) Mkdir(parent uint64, name string, mode uint32, uid uint32, gid uint32) (uint64, FuseAttr, int32) {
	p.logNode("mkdir-parent", parent)
	hostParent, guestParent, errno := p.hostAndGuestPath(parent)
	if errno != 0 {
		return 0, FuseAttr{}, errno
	}
	rel, ok := cleanChildName(name)
	if !ok {
		return 0, FuseAttr{}, -linuxEINVAL
	}
	host := filepath.Join(hostParent, filepath.FromSlash(rel))
	if err := os.Mkdir(host, fs.FileMode(mode&linuxPermMask)); err != nil {
		return 0, FuseAttr{}, errnoFromError(err)
	}
	info, err := os.Lstat(host)
	if err != nil {
		return 0, FuseAttr{}, errnoFromError(err)
	}
	guestPath := joinGuestChild(guestParent, rel)
	if p.meta != nil {
		p.mu.Lock()
		if _, ok := p.meta[guestPath]; !ok {
			p.meta[guestPath] = fsmeta.Entry{
				UID:  uid,
				GID:  gid,
				Mode: uint32(linuxSIFDIR) | (mode & linuxPermMask),
			}
		}
		p.mu.Unlock()
	}
	nodeID := p.ensureNode(guestPath)
	return nodeID, p.fileAttr(nodeID, host, info), 0
}

func (p *passthroughFS) Symlink(parent uint64, name string, target string, uid uint32, gid uint32) (uint64, FuseAttr, int32) {
	hostParent, guestParent, errno := p.hostAndGuestPath(parent)
	if errno != 0 {
		return 0, FuseAttr{}, errno
	}
	rel, ok := cleanChildName(name)
	if !ok {
		return 0, FuseAttr{}, -linuxEINVAL
	}
	host := filepath.Join(hostParent, filepath.FromSlash(rel))
	if err := os.Symlink(target, host); err != nil {
		return 0, FuseAttr{}, errnoFromError(err)
	}
	info, err := os.Lstat(host)
	if err != nil {
		return 0, FuseAttr{}, errnoFromError(err)
	}
	guestPath := joinGuestChild(guestParent, rel)
	if p.meta != nil {
		p.mu.Lock()
		if _, ok := p.meta[guestPath]; !ok {
			p.meta[guestPath] = fsmeta.Entry{
				UID:  uid,
				GID:  gid,
				Mode: uint32(linuxSIFLNK) | 0o777,
			}
		}
		p.mu.Unlock()
	}
	nodeID := p.ensureNode(guestPath)
	return nodeID, p.fileAttr(nodeID, host, info), 0
}

func (p *passthroughFS) Link(nodeID uint64, newParent uint64, newName string) (uint64, FuseAttr, int32) {
	host, errno := p.hostPath(nodeID)
	if errno != 0 {
		return 0, FuseAttr{}, errno
	}
	hostParent, guestParent, errno := p.hostAndGuestPath(newParent)
	if errno != 0 {
		return 0, FuseAttr{}, errno
	}
	rel, ok := cleanChildName(newName)
	if !ok {
		return 0, FuseAttr{}, -linuxEINVAL
	}
	dst := filepath.Join(hostParent, filepath.FromSlash(rel))
	if err := os.Link(host, dst); err != nil {
		return 0, FuseAttr{}, errnoFromError(err)
	}
	info, err := os.Lstat(dst)
	if err != nil {
		return 0, FuseAttr{}, errnoFromError(err)
	}
	guestPath := joinGuestChild(guestParent, rel)
	newNodeID := p.ensureNode(guestPath)
	return newNodeID, p.fileAttr(newNodeID, dst, info), 0
}

func (p *passthroughFS) Create(parent uint64, name string, flags uint32, mode uint32, uid uint32, gid uint32) (uint64, uint64, FuseAttr, int32) {
	p.logNode("create-parent", parent)
	hostParent, guestParent, errno := p.hostAndGuestPath(parent)
	if errno != 0 {
		return 0, 0, FuseAttr{}, errno
	}
	rel, ok := cleanChildName(name)
	if !ok {
		return 0, 0, FuseAttr{}, -linuxEINVAL
	}
	host := filepath.Join(hostParent, filepath.FromSlash(rel))
	file, err := os.OpenFile(host, p.translateOpenFlags(flags)|os.O_CREATE, fs.FileMode(mode&linuxPermMask))
	if err != nil {
		return 0, 0, FuseAttr{}, errnoFromError(err)
	}
	info, err := os.Lstat(host)
	if err != nil {
		_ = file.Close()
		return 0, 0, FuseAttr{}, errnoFromError(err)
	}
	guestPath := joinGuestChild(guestParent, rel)
	nodeID := p.ensureNode(guestPath)
	p.mu.Lock()
	if p.meta != nil {
		if _, ok := p.meta[guestPath]; !ok {
			p.meta[guestPath] = fsmeta.Entry{
				UID:  uid,
				GID:  gid,
				Mode: uint32(linuxSIFREG) | (mode & linuxPermMask),
			}
		}
	}
	handle := p.nextHandle
	p.nextHandle++
	p.handles[handle] = &passthroughHandle{nodeID: nodeID, file: file, append: flags&linuxOAPPEND != 0}
	p.mu.Unlock()
	return nodeID, handle, p.fileAttr(nodeID, host, info), 0
}

func (p *passthroughFS) Open(nodeID uint64, flags uint32) (uint64, int32) {
	p.logNode("open", nodeID)
	host, errno := p.hostPath(nodeID)
	if errno != 0 {
		return 0, errno
	}
	info, err := os.Lstat(host)
	if err != nil {
		return 0, errnoFromError(err)
	}
	if info.IsDir() {
		return 0, -linuxEISDIR
	}
	file, err := os.OpenFile(host, p.translateOpenFlags(flags), 0)
	if err != nil {
		return 0, errnoFromError(err)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	handle := p.nextHandle
	p.nextHandle++
	p.handles[handle] = &passthroughHandle{nodeID: nodeID, file: file, append: flags&linuxOAPPEND != 0}
	return handle, 0
}

func (p *passthroughFS) Release(_ uint64, fh uint64) {
	p.mu.Lock()
	handle := p.handles[fh]
	delete(p.handles, fh)
	p.mu.Unlock()
	if handle != nil && handle.file != nil {
		_ = handle.file.Close()
	}
}

func (p *passthroughFS) Flush(_ uint64, fh uint64, _ uint64) int32 {
	p.mu.RLock()
	handle := p.handles[fh]
	p.mu.RUnlock()
	if handle == nil || handle.file == nil {
		return -linuxEBADF
	}
	return 0
}

func (p *passthroughFS) Fsync(_ uint64, fh uint64, _ uint32) int32 {
	p.mu.RLock()
	handle := p.handles[fh]
	p.mu.RUnlock()
	if handle == nil || handle.file == nil {
		return -linuxEBADF
	}
	if err := handle.file.Sync(); err != nil {
		return errnoFromError(err)
	}
	return 0
}

func (p *passthroughFS) FsyncDir(_ uint64, fh uint64, _ uint32) int32 {
	p.mu.RLock()
	_, ok := p.dirHandles[fh]
	p.mu.RUnlock()
	if !ok {
		return -linuxEBADF
	}
	return 0
}

func (p *passthroughFS) Read(nodeID uint64, fh uint64, off uint64, size uint32) ([]byte, int32) {
	p.logf("read node=%d fh=%d off=%d size=%d", nodeID, fh, off, size)
	p.mu.RLock()
	handle, ok := p.handles[fh]
	p.mu.RUnlock()
	if !ok || handle == nil || handle.nodeID != nodeID || handle.file == nil {
		return nil, -linuxEBADF
	}
	buf := make([]byte, size)
	n, err := handle.file.ReadAt(buf, int64(off))
	if err != nil && err != io.EOF {
		return nil, errnoFromError(err)
	}
	return buf[:n], 0
}

func (p *passthroughFS) Lseek(nodeID uint64, fh uint64, offset uint64, whence uint32) (uint64, int32) {
	p.mu.RLock()
	handle, ok := p.handles[fh]
	p.mu.RUnlock()
	if !ok || handle == nil || handle.nodeID != nodeID {
		return 0, -linuxEBADF
	}
	host, errno := p.hostPath(nodeID)
	if errno != 0 {
		return 0, errno
	}
	info, err := os.Lstat(host)
	if err != nil {
		return 0, errnoFromError(err)
	}
	if info.IsDir() {
		return 0, -linuxEISDIR
	}
	size := uint64(info.Size())
	switch whence {
	case 3: // SEEK_DATA
		if offset >= size {
			return 0, -linuxENXIO
		}
		return offset, 0
	case 4: // SEEK_HOLE
		if offset >= size {
			return offset, 0
		}
		return size, 0
	default:
		return 0, -linuxEINVAL
	}
}

func (p *passthroughFS) OpenDir(nodeID uint64, _ uint32) (uint64, int32) {
	p.logNode("opendir", nodeID)
	host, guest, errno := p.hostAndGuestPath(nodeID)
	if errno != 0 {
		return 0, errno
	}
	entries, err := os.ReadDir(host)
	if err != nil {
		return 0, errnoFromError(err)
	}
	parentID := nodeID
	if guest != "/" {
		parentID = p.ensureNode(path.Dir(guest))
	}
	dirEntries := []dirEntry{
		{name: ".", typ: dirTypeDir, ino: nodeID},
		{name: "..", typ: dirTypeDir, ino: parentID},
	}
	for _, entry := range entries {
		childPath := joinGuestChild(guest, entry.Name())
		childID := p.ensureNode(childPath)
		dirEntries = append(dirEntries, dirEntry{
			name: entry.Name(),
			typ:  dirTypeForMode(entry.Type()),
			ino:  childID,
		})
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	handle := p.nextHandle
	p.nextHandle++
	p.dirHandles[handle] = dirEntries
	return handle, 0
}

func (p *passthroughFS) Write(nodeID uint64, fh uint64, off uint64, data []byte, _ uint32) (uint32, int32) {
	p.mu.RLock()
	handle := p.handles[fh]
	p.mu.RUnlock()
	if handle == nil || handle.nodeID != nodeID || handle.file == nil {
		return 0, -linuxEBADF
	}
	var (
		n   int
		err error
	)
	if handle.append {
		n, err = handle.file.Write(data)
	} else {
		n, err = handle.file.WriteAt(data, int64(off))
	}
	if err != nil {
		return uint32(n), errnoFromError(err)
	}
	return uint32(n), 0
}

func (p *passthroughFS) ReadDir(_ uint64, fh uint64, off uint64, maxBytes uint32) ([]byte, int32) {
	p.mu.Lock()
	entries := append([]dirEntry(nil), p.dirHandles[fh]...)
	p.mu.Unlock()
	if entries == nil {
		return nil, -linuxEBADF
	}
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
		binary.LittleEndian.PutUint64(out[start:start+8], entry.ino)
		binary.LittleEndian.PutUint64(out[start+8:start+16], uint64(i+1))
		binary.LittleEndian.PutUint32(out[start+16:start+20], uint32(len(nameBytes)))
		binary.LittleEndian.PutUint32(out[start+20:start+24], entry.typ)
		copy(out[start+24:start+24+len(nameBytes)], nameBytes)
	}
	return out, 0
}

func (p *passthroughFS) ReleaseDir(_ uint64, fh uint64) {
	p.mu.Lock()
	delete(p.dirHandles, fh)
	p.mu.Unlock()
}

func (p *passthroughFS) Readlink(nodeID uint64) (string, int32) {
	p.logNode("readlink", nodeID)
	host, errno := p.hostPath(nodeID)
	if errno != 0 {
		return "", errno
	}
	target, err := os.Readlink(host)
	if err != nil {
		return "", errnoFromError(err)
	}
	return target, 0
}

func (p *passthroughFS) RmDir(parent uint64, name string) int32 {
	p.logNode("rmdir-parent", parent)
	hostParent, guestParent, errno := p.hostAndGuestPath(parent)
	if errno != 0 {
		return errno
	}
	clean := path.Clean("/" + name)
	if clean == "/" {
		return -linuxEINVAL
	}
	rel := strings.TrimPrefix(clean, "/")
	host := filepath.Join(hostParent, filepath.FromSlash(rel))
	if err := os.Remove(host); err != nil {
		return errnoFromError(err)
	}
	guestPath := joinGuestChild(guestParent, rel)
	p.mu.Lock()
	delete(p.meta, guestPath)
	delete(p.pathToNode, guestPath)
	for id, existing := range p.nodes {
		if existing == guestPath {
			delete(p.nodes, id)
			break
		}
	}
	p.mu.Unlock()
	return 0
}

func (p *passthroughFS) Unlink(parent uint64, name string) int32 {
	hostParent, guestParent, errno := p.hostAndGuestPath(parent)
	if errno != 0 {
		return errno
	}
	clean := path.Clean("/" + name)
	if clean == "/" {
		return -linuxEINVAL
	}
	host := filepath.Join(hostParent, filepath.FromSlash(strings.TrimPrefix(clean, "/")))
	if err := os.Remove(host); err != nil {
		return errnoFromError(err)
	}
	p.removeNodeForGuestPath(joinGuestChild(guestParent, strings.TrimPrefix(clean, "/")))
	return 0
}

func (p *passthroughFS) Rename(parent uint64, name string, newParent uint64, newName string, _ uint32) int32 {
	oldParent, oldGuestParent, errno := p.hostAndGuestPath(parent)
	if errno != 0 {
		return errno
	}
	newParentPath, newGuestParent, errno := p.hostAndGuestPath(newParent)
	if errno != 0 {
		return errno
	}
	oldRel := strings.TrimPrefix(path.Clean("/"+name), "/")
	newRel := strings.TrimPrefix(path.Clean("/"+newName), "/")
	oldHost := filepath.Join(oldParent, filepath.FromSlash(oldRel))
	newHost := filepath.Join(newParentPath, filepath.FromSlash(newRel))
	if err := os.Rename(oldHost, newHost); err != nil {
		return errnoFromError(err)
	}
	p.renameNodeGuestPath(joinGuestChild(oldGuestParent, oldRel), joinGuestChild(newGuestParent, newRel))
	return 0
}

func (p *passthroughFS) SetAttr(nodeID uint64, valid uint32, fh uint64, size uint64, mode uint32, uid uint32, gid uint32, atime time.Time, mtime time.Time) (FuseAttr, int32) {
	host, errno := p.hostPath(nodeID)
	if errno != 0 {
		return FuseAttr{}, errno
	}
	var file *os.File
	if valid&fattrFH != 0 {
		p.mu.Lock()
		handle := p.handles[fh]
		p.mu.Unlock()
		if handle == nil || handle.nodeID != nodeID {
			return FuseAttr{}, -linuxEBADF
		}
		file = handle.file
	}
	if valid&fattrSize != 0 {
		if file != nil {
			if err := file.Truncate(int64(size)); err != nil {
				return FuseAttr{}, errnoFromError(err)
			}
		} else if err := os.Truncate(host, int64(size)); err != nil {
			return FuseAttr{}, errnoFromError(err)
		}
	}
	if valid&fattrMode != 0 {
		if err := os.Chmod(host, fs.FileMode(mode&linuxPermMask)); err != nil {
			return FuseAttr{}, errnoFromError(err)
		}
	}
	if valid&(fattrUID|fattrGID) != 0 {
		if err := os.Chown(host, int(uid), int(gid)); err != nil {
			return FuseAttr{}, errnoFromError(err)
		}
	}
	if valid&(fattrATime|fattrMTime) != 0 {
		current := time.Now()
		if valid&fattrATime == 0 {
			atime = current
		}
		if valid&fattrMTime == 0 {
			mtime = current
		}
		if err := os.Chtimes(host, atime, mtime); err != nil {
			return FuseAttr{}, errnoFromError(err)
		}
	}
	info, err := os.Lstat(host)
	if err != nil {
		return FuseAttr{}, errnoFromError(err)
	}
	return p.fileAttr(nodeID, host, info), 0
}

func (p *passthroughFS) StatFS(_ uint64) (uint64, uint64, uint64, uint64, uint64, uint64, uint64, uint64, int32) {
	if p.root == "" {
		return 0, 0, 0, 0, 0, 4096, 4096, 255, 0
	}
	return hostStatFS(p.root)
}

func (p *passthroughFS) hostPath(nodeID uint64) (string, int32) {
	host, _, errno := p.hostAndGuestPath(nodeID)
	return host, errno
}

func (p *passthroughFS) translateOpenFlags(flags uint32) int {
	p.mu.RLock()
	writebackCache := p.writebackCache
	p.mu.RUnlock()
	return translateLinuxOpenFlags(flags, writebackCache)
}

func (p *passthroughFS) hostAndGuestPath(nodeID uint64) (string, string, int32) {
	p.mu.RLock()
	guest, ok := p.nodes[nodeID]
	p.mu.RUnlock()
	if !ok {
		return "", "", -linuxENOENT
	}
	if p.root == "" {
		return "", "", -linuxENOENT
	}
	if guest == "/" {
		return p.root, guest, 0
	}
	return filepath.Join(p.root, filepath.FromSlash(strings.TrimPrefix(guest, "/"))), guest, 0
}

func joinGuestChild(parentGuest, rel string) string {
	if rel == "" {
		return path.Clean(parentGuest)
	}
	return path.Join(parentGuest, filepath.ToSlash(rel))
}

func (p *passthroughFS) DebugPath(nodeID uint64) string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.nodes[nodeID]
}

func (p *passthroughFS) GetXattr(nodeID uint64, name string) ([]byte, int32) {
	p.logf("getxattr-backend node=%d name=%q", nodeID, name)
	return nil, -linuxENODATA
}

func (p *passthroughFS) ListXattr(nodeID uint64) ([]byte, int32) {
	p.logNode("listxattr-backend", nodeID)
	return nil, 0
}

func (p *passthroughFS) logNode(op string, nodeID uint64) {
	p.logf("%s node=%d", op, nodeID)
}

func (p *passthroughFS) logf(format string, args ...any) {
	_ = format
	_ = args
}

func (p *passthroughFS) ensureNode(guestPath string) uint64 {
	guestPath = path.Clean("/" + strings.TrimPrefix(guestPath, "/"))
	p.mu.Lock()
	defer p.mu.Unlock()
	if id, ok := p.pathToNode[guestPath]; ok {
		return id
	}
	id := p.nextNodeID
	p.nextNodeID++
	p.pathToNode[guestPath] = id
	p.nodes[id] = guestPath
	return id
}

func (p *passthroughFS) removeNodeForGuestPath(guestPath string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.pathToNode, guestPath)
	for id, existing := range p.nodes {
		if existing == guestPath {
			delete(p.nodes, id)
			break
		}
	}
}

func (p *passthroughFS) renameNodeGuestPath(oldPath, newPath string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if id, ok := p.pathToNode[oldPath]; ok {
		delete(p.pathToNode, oldPath)
		p.pathToNode[newPath] = id
		p.nodes[id] = newPath
	}
}

func (p *passthroughFS) fileAttr(nodeID uint64, hostPath string, info os.FileInfo) FuseAttr {
	mode := goModeToLinux(info.Mode())
	if mode&os.ModeType == 0 {
		mode |= 0
	}
	attr := FuseAttr{
		Ino:     nodeID,
		Size:    uint64(info.Size()),
		Blocks:  uint64((info.Size() + 511) / 512),
		Mode:    fsmeta.NormalizeLinuxMode(0, info.Mode()),
		NLink:   1,
		UID:     0,
		GID:     0,
		BlkSize: 4096,
	}
	mod := info.ModTime()
	attr.ATimeSec = uint64(mod.Unix())
	attr.MTimeSec = uint64(mod.Unix())
	attr.CTimeSec = uint64(mod.Unix())
	attr.ATimeNsec = uint32(mod.Nanosecond())
	attr.MTimeNsec = uint32(mod.Nanosecond())
	attr.CTimeNsec = uint32(mod.Nanosecond())
	enrichHostFileAttr(hostPath, info, &attr)
	if p.mapOwner {
		attr.UID = p.ownerUID
		attr.GID = p.ownerGID
	}
	if attr.Blocks == 0 && attr.Size > 0 {
		attr.Blocks = uint64((attr.Size + 511) / 512)
	}
	if attr.BlkSize == 0 {
		attr.BlkSize = 4096
	}
	if p.meta != nil {
		p.mu.RLock()
		guestPath := p.nodes[nodeID]
		meta, ok := p.meta[guestPath]
		p.mu.RUnlock()
		if ok {
			attr.UID = meta.UID
			attr.GID = meta.GID
			if meta.RDev != 0 {
				attr.RDev = meta.RDev
			}
			if meta.Mode != 0 {
				attr.Mode = fsmeta.NormalizeLinuxMode(meta.Mode, info.Mode())
			}
		}
	}
	if info.IsDir() {
		attr.NLink = maxU32(attr.NLink, 2)
	}
	return attr
}

func (p *imageFS) pathForNode(id uint64) string {
	node := p.nodes[id]
	if node == nil {
		return ""
	}
	if id == 1 {
		return "/"
	}
	var parts []string
	for node != nil && node.id != 1 {
		parts = append(parts, node.name)
		node = p.nodes[node.parent]
	}
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	return "/" + strings.Join(parts, "/")
}

func (p *imageFS) DebugPath(nodeID uint64) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.pathForNode(nodeID)
}

func (p *imageFS) SnapshotNodePaths() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	ids := make([]int, 0, len(p.nodes))
	for id := range p.nodes {
		ids = append(ids, int(id))
	}
	sort.Ints(ids)
	paths := make([]string, 0, len(ids))
	for _, id := range ids {
		if path := p.pathForNode(uint64(id)); path != "" {
			paths = append(paths, path)
		}
	}
	return paths
}

func (p *imageFS) RestoreNodePaths(paths []string) error {
	for _, nodePath := range paths {
		nodePath = path.Clean("/" + strings.TrimPrefix(nodePath, "/"))
		if nodePath == "/" {
			continue
		}
		if err := p.restoreNodePath(nodePath); err != nil {
			return err
		}
	}
	return nil
}

func (p *imageFS) restoreNodePath(nodePath string) error {
	parentPath, name := path.Split(nodePath)
	parentPath = path.Clean(parentPath)
	if parentPath == "." {
		parentPath = "/"
	}
	parentID, ok := p.nodeIDForPath(parentPath)
	if !ok {
		if err := p.restoreNodePath(parentPath); err != nil {
			return err
		}
		parentID, ok = p.nodeIDForPath(parentPath)
		if !ok {
			return fmt.Errorf("restore imagefs node %q: parent %q was not created", nodePath, parentPath)
		}
	}
	childID, _, errno := p.Lookup(parentID, name)
	if errno != 0 {
		return fmt.Errorf("restore imagefs node %q: lookup errno %d", nodePath, errno)
	}
	if restoredPath := p.DebugPath(childID); restoredPath != nodePath {
		return fmt.Errorf("restore imagefs node %q: got node %d path %q", nodePath, childID, restoredPath)
	}
	return nil
}

func (p *imageFS) nodeIDForPath(nodePath string) (uint64, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for id := range p.nodes {
		if p.pathForNode(id) == nodePath {
			return id, true
		}
	}
	return 0, false
}

func virtioFSDebugPathsFromEnv() []string {
	raw := strings.TrimSpace(os.Getenv("CCX3_DEBUG_VIRTIOFS_PATHS"))
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv("CCX3_DEBUG_VIRTIOFS_PATH"))
	}
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	paths := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if !strings.HasPrefix(part, "/") {
			part = "/" + part
		}
		paths = append(paths, path.Clean(part))
	}
	return paths
}

func (p *imageFS) debugPathMatchLocked(guestPath string) bool {
	if len(p.debugPaths) == 0 || guestPath == "" {
		return false
	}
	guestPath = path.Clean(guestPath)
	for _, prefix := range p.debugPaths {
		if guestPath == prefix || strings.HasPrefix(guestPath, strings.TrimSuffix(prefix, "/")+"/") {
			return true
		}
	}
	return false
}

func (p *imageFS) debugfLocked(format string, args ...any) {
	if p.debugLog == nil {
		return
	}
	_, _ = fmt.Fprintf(p.debugLog, "virtiofs:image "+format+"\n", args...)
}

func (p *imageFS) debugNodefLocked(op string, nodeID uint64, format string, args ...any) {
	if len(p.debugPaths) == 0 || p.debugLog == nil {
		return
	}
	guestPath := p.pathForNode(nodeID)
	if !p.debugPathMatchLocked(guestPath) {
		return
	}
	msg := fmt.Sprintf(format, args...)
	if msg != "" {
		msg = " " + msg
	}
	p.debugfLocked("%s path=%q node=%d%s", op, guestPath, nodeID, msg)
}

func (p *imageFS) debugChildfLocked(op string, parent uint64, name string, format string, args ...any) {
	if len(p.debugPaths) == 0 || p.debugLog == nil {
		return
	}
	parentPath := p.pathForNode(parent)
	childName, ok := cleanChildName(name)
	if !ok {
		childName = name
	}
	childPath := path.Join(parentPath, childName)
	if !p.debugPathMatchLocked(childPath) && !p.debugPathMatchLocked(parentPath) {
		return
	}
	msg := fmt.Sprintf(format, args...)
	if msg != "" {
		msg = " " + msg
	}
	p.debugfLocked("%s parent=%q name=%q child=%q%s", op, parentPath, name, childPath, msg)
}

func (p *imageFS) Init() (uint32, uint32) {
	return 128 << 10, 0
}

func (p *imageFS) GetAttr(nodeID uint64) (FuseAttr, int32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	node := p.nodes[nodeID]
	if node == nil {
		return FuseAttr{}, -linuxENOENT
	}
	return p.attr(node), 0
}

func (p *imageFS) Lookup(parent uint64, name string) (uint64, FuseAttr, int32) {
	p.mu.Lock()
	parentNode := p.nodes[parent]
	if parentNode == nil {
		p.mu.Unlock()
		return 0, FuseAttr{}, -linuxENOENT
	}
	switch name {
	case ".":
		attr := p.attr(parentNode)
		p.mu.Unlock()
		return parentNode.id, attr, 0
	case "..":
		node := p.nodes[parentNode.parent]
		if node == nil {
			p.mu.Unlock()
			return 0, FuseAttr{}, -linuxENOENT
		}
		attr := p.attr(node)
		p.mu.Unlock()
		return node.id, attr, 0
	}
	var ok bool
	name, ok = cleanChildName(name)
	if !ok {
		p.mu.Unlock()
		return 0, FuseAttr{}, -linuxEINVAL
	}
	p.debugChildfLocked("lookup", parent, name, "")
	childID, ok := parentNode.entries[name]
	if !ok {
		if parentNode.whiteouts[name] {
			p.debugChildfLocked("lookup-whiteout", parent, name, "errno=%d", -linuxENOENT)
			p.mu.Unlock()
			return 0, FuseAttr{}, -linuxENOENT
		}
		if parentNode.abstractDir == nil {
			p.debugChildfLocked("lookup-miss", parent, name, "errno=%d", -linuxENOENT)
			p.mu.Unlock()
			return 0, FuseAttr{}, -linuxENOENT
		}
		lowerDir := parentNode.abstractDir
		lowerParent := parentNode
		p.mu.Unlock()
		entry, err := lowerDir.Lookup(name)
		if err != nil {
			errno := errnoFromError(err)
			p.mu.Lock()
			p.debugChildfLocked("lookup-lower-error", parent, name, "errno=%d", errno)
			p.mu.Unlock()
			return 0, FuseAttr{}, errno
		}
		p.mu.Lock()
		parentNode = p.nodes[parent]
		if parentNode == nil {
			p.mu.Unlock()
			return 0, FuseAttr{}, -linuxENOENT
		}
		if parentNode.whiteouts[name] {
			p.debugChildfLocked("lookup-whiteout-after-lower", parent, name, "errno=%d", -linuxENOENT)
			p.mu.Unlock()
			return 0, FuseAttr{}, -linuxENOENT
		}
		if existingID, exists := parentNode.entries[name]; exists {
			child := p.nodes[existingID]
			if child == nil {
				p.debugChildfLocked("lookup-stale-node", parent, name, "node=%d errno=%d", existingID, -linuxENOENT)
				p.mu.Unlock()
				return 0, FuseAttr{}, -linuxENOENT
			}
			attr := p.attr(child)
			p.debugChildfLocked("lookup-raced-hit", parent, name, "node=%d mode=%#o", child.id, attr.Mode)
			p.mu.Unlock()
			return child.id, attr, 0
		}
		if parentNode != lowerParent {
			p.debugChildfLocked("lookup-lower-miss", parent, name, "errno=%d", -linuxENOENT)
			p.mu.Unlock()
			return 0, FuseAttr{}, -linuxENOENT
		}
		child, errno := p.createAbstractNode(parentNode, name, entry)
		if errno != 0 {
			p.debugChildfLocked("lookup-lower-error", parent, name, "errno=%d", errno)
			p.mu.Unlock()
			return 0, FuseAttr{}, errno
		}
		attr := p.attr(child)
		p.debugChildfLocked("lookup-lower-hit", parent, name, "node=%d mode=%#o", child.id, attr.Mode)
		p.mu.Unlock()
		return child.id, attr, 0
	}
	child := p.nodes[childID]
	if child == nil {
		p.debugChildfLocked("lookup-stale-node", parent, name, "node=%d errno=%d", childID, -linuxENOENT)
		p.mu.Unlock()
		return 0, FuseAttr{}, -linuxENOENT
	}
	attr := p.attr(child)
	p.debugChildfLocked("lookup-hit", parent, name, "node=%d mode=%#o", child.id, attr.Mode)
	p.mu.Unlock()
	return child.id, attr, 0
}

func (p *imageFS) Open(nodeID uint64, flags uint32) (uint64, int32) {
	return p.OpenForCaller(nodeID, flags, 0, 0)
}

func (p *imageFS) OpenForCaller(nodeID uint64, flags uint32, uid uint32, gid uint32) (uint64, int32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	node := p.nodes[nodeID]
	if node == nil {
		return 0, -linuxENOENT
	}
	p.debugNodefLocked("open", nodeID, "flags=%#x", flags)
	if node.isDir() {
		return 0, -linuxEISDIR
	}
	if flags&linuxOACCMODE != linuxORDONLY {
		if errno := p.copyUpFileLocked(node); errno != 0 {
			return 0, errno
		}
		if flags&linuxOTRUNC != 0 {
			node.data.release(p.dataStore)
			node.data = sparseImageData{}
			node.lowerFile = nil
			node.lowerSize = 0
			node.size = 0
			if uid != 0 {
				node.mode &^= fs.FileMode(0o6000)
			}
			now := time.Now()
			node.modTime = now
			node.ctime = now
			p.refreshImageNodeMetadataLocked(node)
		}
	}
	fh := p.nextHandle
	p.nextHandle++
	handle := imageHandle{nodeID: nodeID}
	readerFile := node.abstractFile
	if readerFile == nil {
		readerFile = node.lowerFile
	}
	if openable, ok := readerFile.(imagefs.OpenReaderFile); ok {
		reader, closer, err := openable.OpenReader()
		if err != nil {
			return 0, errnoFromError(err)
		}
		handle.reader = reader
		handle.closer = closer
	}
	p.handles[fh] = handle
	p.noteImageHandleAddedLocked()
	return fh, 0
}

func (p *imageFS) Release(_ uint64, fh uint64) {
	p.mu.Lock()
	handle, ok := p.handles[fh]
	delete(p.handles, fh)
	p.compactImageHandleMapsLocked()
	if ok {
		p.collectImageNodeLocked(handle.nodeID)
	}
	p.mu.Unlock()
	if ok && handle.closer != nil {
		_ = handle.closer.Close()
	}
}

func (p *imageFS) Flush(_ uint64, _ uint64, _ uint64) int32 {
	return 0
}

func (p *imageFS) Fsync(_ uint64, _ uint64, _ uint32) int32 {
	if err := p.dataStore.sync(); err != nil {
		return errnoFromError(err)
	}
	return 0
}

func (p *imageFS) FsyncDir(_ uint64, _ uint64, _ uint32) int32 {
	if err := p.dataStore.sync(); err != nil {
		return errnoFromError(err)
	}
	return 0
}

func (p *imageFS) Read(nodeID uint64, fh uint64, off uint64, size uint32) ([]byte, int32) {
	p.mu.Lock()
	handle, ok := p.handles[fh]
	node := p.nodes[nodeID]
	if !ok || handle.nodeID != nodeID || node == nil {
		p.mu.Unlock()
		return nil, -linuxEBADF
	}
	if node.abstractFile == nil {
		end := off + uint64(size)
		if off >= node.size || size == 0 {
			p.mu.Unlock()
			return []byte{}, 0
		}
		if end > node.size {
			end = node.size
		}
		data := make([]byte, end-off)
		if node.lowerFile != nil && off < node.lowerSize {
			lowerEnd := min(end, node.lowerSize)
			var err error
			if handle.reader != nil {
				var n int
				n, err = handle.reader.ReadAt(data[:lowerEnd-off], int64(off))
				if err != nil && err != io.EOF {
					p.mu.Unlock()
					return nil, errnoFromError(err)
				}
				if uint64(n) != lowerEnd-off {
					p.mu.Unlock()
					return nil, -linuxEIO
				}
			} else {
				var lower []byte
				lower, err = node.lowerFile.ReadAt(off, uint32(lowerEnd-off))
				if err != nil {
					p.mu.Unlock()
					return nil, errnoFromError(err)
				}
				if uint64(len(lower)) != lowerEnd-off {
					p.mu.Unlock()
					return nil, -linuxEIO
				}
				copy(data, lower)
			}
		}
		if err := node.data.readAt(p.dataStore, data, off); err != nil {
			p.mu.Unlock()
			return nil, errnoFromError(err)
		}
		node.atime = time.Now()
		p.mu.Unlock()
		return data, 0
	}
	abstractFile := node.abstractFile
	p.mu.Unlock()
	if handle.reader != nil {
		buf := make([]byte, size)
		n, err := handle.reader.ReadAt(buf, int64(off))
		if err != nil && err != io.EOF {
			return nil, errnoFromError(err)
		}
		p.mu.Lock()
		if current := p.nodes[nodeID]; current != nil {
			current.atime = time.Now()
		}
		p.mu.Unlock()
		return buf[:n], 0
	}
	data, err := abstractFile.ReadAt(off, size)
	if err != nil {
		return nil, errnoFromError(err)
	}
	if data == nil {
		return []byte{}, 0
	}
	p.mu.Lock()
	if current := p.nodes[nodeID]; current != nil {
		current.atime = time.Now()
	}
	p.mu.Unlock()
	return data, 0
}

func (p *imageFS) Lseek(nodeID uint64, fh uint64, offset uint64, whence uint32) (uint64, int32) {
	p.mu.Lock()
	handle, ok := p.handles[fh]
	node := p.nodes[nodeID]
	if !ok || handle.nodeID != nodeID || node == nil {
		p.mu.Unlock()
		return 0, -linuxEBADF
	}
	if offset >= node.size {
		p.mu.Unlock()
		return 0, -linuxENXIO
	}
	if node.abstractFile != nil || node.lowerFile != nil && offset < node.lowerSize {
		size := node.size
		lowerSize := node.lowerSize
		if node.abstractFile != nil {
			lowerSize = size
		}
		if offset < lowerSize {
			p.mu.Unlock()
			if whence == 3 {
				return offset, 0
			}
			if whence == 4 {
				return lowerSize, 0
			}
			return 0, -linuxEINVAL
		}
	}
	switch whence {
	case 3: // SEEK_DATA
		page := offset / imageDataPageSize
		if _, ok := node.data.location(page); ok {
			p.mu.Unlock()
			return offset, 0
		}
		if candidate, ok := node.data.nextDataPage(page + 1); ok && candidate*imageDataPageSize < node.size {
			p.mu.Unlock()
			return candidate * imageDataPageSize, 0
		}
		p.mu.Unlock()
		return 0, -linuxENXIO
	case 4: // SEEK_HOLE
		page := offset / imageDataPageSize
		if _, ok := node.data.location(page); !ok {
			p.mu.Unlock()
			return offset, 0
		}
		if candidate := node.data.nextHolePage(page); candidate*imageDataPageSize < node.size {
			p.mu.Unlock()
			return candidate * imageDataPageSize, 0
		}
		size := node.size
		p.mu.Unlock()
		return size, 0
	default:
		p.mu.Unlock()
		return 0, -linuxEINVAL
	}
}

func (p *imageFS) OpenDir(nodeID uint64, _ uint32) (uint64, int32) {
	if errno := p.materializeDirEntries(nodeID); errno != 0 {
		return 0, errno
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	node := p.nodes[nodeID]
	if node == nil {
		return 0, -linuxENOENT
	}
	if !node.isDir() {
		return 0, -linuxENOTDIR
	}
	entries := []dirEntry{
		{name: ".", typ: dirTypeDir, ino: node.id},
		{name: "..", typ: dirTypeDir, ino: node.parent},
	}
	names := make([]string, 0, len(node.entries))
	for name := range node.entries {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		child := p.nodes[node.entries[name]]
		entries = append(entries, dirEntry{name: name, typ: p.dirType(child), ino: child.id})
	}
	fh := p.nextHandle
	p.nextHandle++
	p.dirHandles[fh] = entries
	p.dirHandleNodes[fh] = nodeID
	p.noteImageDirHandleAddedLocked()
	return fh, 0
}

func (p *imageFS) ReadDir(_ uint64, fh uint64, off uint64, maxBytes uint32) ([]byte, int32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	entries, ok := p.dirHandles[fh]
	if !ok {
		return nil, -linuxEBADF
	}
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
		binary.LittleEndian.PutUint64(out[start:start+8], entry.ino)
		binary.LittleEndian.PutUint64(out[start+8:start+16], uint64(i+1))
		binary.LittleEndian.PutUint32(out[start+16:start+20], uint32(len(nameBytes)))
		binary.LittleEndian.PutUint32(out[start+20:start+24], entry.typ)
		copy(out[start+24:start+24+len(nameBytes)], nameBytes)
	}
	return out, 0
}

func (p *imageFS) ReleaseDir(_ uint64, fh uint64) {
	p.mu.Lock()
	nodeID := p.dirHandleNodes[fh]
	delete(p.dirHandles, fh)
	delete(p.dirHandleNodes, fh)
	p.compactImageDirHandleMapsLocked()
	p.collectImageNodeLocked(nodeID)
	p.mu.Unlock()
}

func (p *imageFS) Readlink(nodeID uint64) (string, int32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	node := p.nodes[nodeID]
	if node == nil {
		return "", -linuxENOENT
	}
	if !node.isSymlink() {
		return "", -linuxEINVAL
	}
	return node.symlinkTarget, 0
}

func (p *imageFS) inheritImageCreateLocked(parent *imageNode, mode uint32, gid uint32, directory bool) (uint32, uint32) {
	if parent == nil || linuxModeBits(parent.mode)&0o2000 == 0 {
		return mode, gid
	}
	gid = p.attr(parent).GID
	if directory {
		mode |= 0o2000
	}
	return mode, gid
}

func (p *imageFS) Mkdir(parent uint64, name string, mode uint32, uid uint32, gid uint32) (uint64, FuseAttr, int32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	parentNode := p.nodes[parent]
	if parentNode == nil {
		return 0, FuseAttr{}, -linuxENOENT
	}
	mode, gid = p.inheritImageCreateLocked(parentNode, mode, gid, true)
	var ok bool
	name, ok = cleanChildName(name)
	if !ok {
		return 0, FuseAttr{}, -linuxEINVAL
	}
	p.debugChildfLocked("mkdir", parent, name, "mode=%#o uid=%d gid=%d", mode, uid, gid)
	if _, exists := parentNode.entries[name]; exists {
		p.debugChildfLocked("mkdir-exists", parent, name, "errno=%d", -linuxEEXIST)
		return 0, FuseAttr{}, -linuxEEXIST
	}
	now := time.Now()
	node := &imageNode{
		id:      p.nextNodeID,
		parent:  parent,
		name:    name,
		mode:    fs.ModeDir | fs.FileMode(mode&linuxPermMask),
		uid:     uid,
		gid:     gid,
		entries: map[string]uint64{},
		atime:   now,
		modTime: now,
		ctime:   now,
	}
	p.nextNodeID++
	p.nodes[node.id] = node
	p.noteImageNodeAddedLocked(node)
	p.registerImageNodeLinkLocked(node)
	parentNode.entries[name] = node.id
	p.noteImageEntryAddedLocked(parentNode)
	p.touchImageDirectoryLocked(parentNode, now)
	return node.id, p.attr(node), 0
}

func (p *imageFS) Mknod(parent uint64, name string, mode uint32, rdev uint32, uid uint32, gid uint32) (uint64, FuseAttr, int32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	parentNode := p.nodes[parent]
	if parentNode == nil {
		return 0, FuseAttr{}, -linuxENOENT
	}
	mode, gid = p.inheritImageCreateLocked(parentNode, mode, gid, false)
	var ok bool
	name, ok = cleanChildName(name)
	if !ok {
		return 0, FuseAttr{}, -linuxEINVAL
	}
	fileType := mode & linuxSIFMT
	p.debugChildfLocked("mknod", parent, name, "mode=%#o type=%#o rdev=%d uid=%d gid=%d", mode, fileType, rdev, uid, gid)
	switch fileType {
	case linuxSIFREG, linuxSIFCHR, linuxSIFBLK, linuxSIFIFO, linuxSIFSOCK:
	default:
		p.debugChildfLocked("mknod-invalid-type", parent, name, "mode=%#o errno=%d", mode, -linuxEINVAL)
		return 0, FuseAttr{}, -linuxEINVAL
	}
	if existingID, exists := parentNode.entries[name]; exists {
		existing := p.nodes[existingID]
		if existing == nil {
			p.debugChildfLocked("mknod-stale-existing", parent, name, "existing=%d errno=%d", existingID, -linuxENOENT)
			return 0, FuseAttr{}, -linuxENOENT
		}
		p.debugChildfLocked("mknod-exists", parent, name, "existing=%d existing_dir=%v errno=%d", existingID, existing.isDir(), -linuxEEXIST)
		return 0, FuseAttr{}, -linuxEEXIST
	}
	if parentNode.whiteouts != nil {
		delete(parentNode.whiteouts, name)
	}
	now := time.Now()
	node := &imageNode{
		id:      p.nextNodeID,
		parent:  parent,
		name:    name,
		mode:    linuxModeToGo(fileType | (mode & linuxPermMask)),
		rawMode: fileType | (mode & linuxPermMask),
		uid:     uid,
		gid:     gid,
		rdev:    rdev,
		atime:   now,
		modTime: now,
		ctime:   now,
	}
	p.nextNodeID++
	p.nodes[node.id] = node
	p.noteImageNodeAddedLocked(node)
	p.registerImageNodeLinkLocked(node)
	parentNode.entries[name] = node.id
	p.noteImageEntryAddedLocked(parentNode)
	p.touchImageDirectoryLocked(parentNode, now)
	return node.id, p.attr(node), 0
}

func (p *imageFS) Symlink(parent uint64, name string, target string, uid uint32, gid uint32) (uint64, FuseAttr, int32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	parentNode := p.nodes[parent]
	if parentNode == nil {
		return 0, FuseAttr{}, -linuxENOENT
	}
	_, gid = p.inheritImageCreateLocked(parentNode, 0, gid, false)
	var ok bool
	name, ok = cleanChildName(name)
	if !ok {
		return 0, FuseAttr{}, -linuxEINVAL
	}
	p.debugChildfLocked("symlink", parent, name, "target=%q uid=%d gid=%d", target, uid, gid)
	if _, exists := parentNode.entries[name]; exists {
		p.debugChildfLocked("symlink-exists", parent, name, "errno=%d", -linuxEEXIST)
		return 0, FuseAttr{}, -linuxEEXIST
	}
	now := time.Now()
	node := &imageNode{
		id:            p.nextNodeID,
		parent:        parent,
		name:          name,
		mode:          fs.ModeSymlink | 0o777,
		uid:           uid,
		gid:           gid,
		size:          uint64(len(target)),
		symlinkTarget: target,
		atime:         now,
		modTime:       now,
		ctime:         now,
	}
	p.nextNodeID++
	p.nodes[node.id] = node
	p.noteImageNodeAddedLocked(node)
	parentNode.entries[name] = node.id
	p.noteImageEntryAddedLocked(parentNode)
	p.touchImageDirectoryLocked(parentNode, now)
	return node.id, p.attr(node), 0
}

func (p *imageFS) Link(nodeID uint64, newParent uint64, newName string) (uint64, FuseAttr, int32) {
	return p.LinkForCaller(nodeID, newParent, newName, 0, 0)
}

func (p *imageFS) LinkForCaller(nodeID uint64, newParent uint64, newName string, uid uint32, gid uint32) (uint64, FuseAttr, int32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	node := p.nodes[nodeID]
	parentNode := p.nodes[newParent]
	if node == nil || parentNode == nil {
		return 0, FuseAttr{}, -linuxENOENT
	}
	if node.isDir() {
		return 0, FuseAttr{}, -linuxEPERM
	}
	name, ok := cleanChildName(newName)
	if !ok {
		return 0, FuseAttr{}, -linuxEINVAL
	}
	p.debugChildfLocked("link", newParent, name, "old_path=%q old_node=%d", p.pathForNode(nodeID), nodeID)
	if _, exists := parentNode.entries[name]; exists {
		p.debugChildfLocked("link-exists", newParent, name, "errno=%d", -linuxEEXIST)
		return 0, FuseAttr{}, -linuxEEXIST
	}
	if parentNode.whiteouts != nil {
		delete(parentNode.whiteouts, name)
	}
	if node.abstractFile != nil {
		if errno := p.copyUpFileLocked(node); errno != 0 {
			return 0, FuseAttr{}, errno
		}
	}
	parentNode.entries[name] = node.id
	p.noteImageEntryAddedLocked(parentNode)
	now := time.Now()
	node.ctime = now
	p.touchImageDirectoryLocked(parentNode, now)
	p.refreshImageNodeLinksLocked(node)
	return node.id, p.attr(node), 0
}

func (p *imageFS) RmDir(parent uint64, name string) int32 {
	return p.RmDirForCaller(parent, name, 0, 0)
}

func (p *imageFS) RmDirForCaller(parent uint64, name string, uid uint32, gid uint32) int32 {
	p.mu.Lock()
	defer p.mu.Unlock()
	parentNode := p.nodes[parent]
	if parentNode == nil {
		return -linuxENOENT
	}
	var ok bool
	name, ok = cleanChildName(name)
	if !ok {
		return -linuxEINVAL
	}
	p.debugChildfLocked("rmdir", parent, name, "")
	childID, ok := parentNode.entries[name]
	if !ok {
		p.debugChildfLocked("rmdir-miss", parent, name, "errno=%d", -linuxENOENT)
		return -linuxENOENT
	}
	child := p.nodes[childID]
	if child == nil {
		return -linuxENOENT
	}
	if len(child.entries) != 0 {
		p.debugChildfLocked("rmdir-not-empty", parent, name, "node=%d entries=%d errno=%d", childID, len(child.entries), -linuxENOTEMPTY)
		return -linuxENOTEMPTY
	}
	delete(parentNode.entries, name)
	if parentNode.abstractDir != nil {
		if parentNode.whiteouts == nil {
			parentNode.whiteouts = map[string]bool{}
		}
		parentNode.whiteouts[name] = true
		p.noteImageWhiteoutAddedLocked(parentNode)
	}
	p.collectImageNodeLocked(childID)
	p.touchImageDirectoryLocked(parentNode, time.Now())
	p.compactImageNodeMapsLocked(parentNode)
	return 0
}

func (p *imageFS) Create(parent uint64, name string, flags uint32, mode uint32, uid uint32, gid uint32) (uint64, uint64, FuseAttr, int32) {
	return p.CreateForCaller(parent, name, flags, mode, uid, gid)
}

func (p *imageFS) CreateForCaller(parent uint64, name string, flags uint32, mode uint32, uid uint32, gid uint32) (uint64, uint64, FuseAttr, int32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	parentNode := p.nodes[parent]
	if parentNode == nil {
		return 0, 0, FuseAttr{}, -linuxENOENT
	}
	var ok bool
	name, ok = cleanChildName(name)
	if !ok {
		return 0, 0, FuseAttr{}, -linuxEINVAL
	}
	p.debugChildfLocked("create", parent, name, "flags=%#x mode=%#o uid=%d gid=%d", flags, mode, uid, gid)
	if existingID, exists := parentNode.entries[name]; exists {
		if flags&linuxOEXCL != 0 {
			p.debugChildfLocked("create-excl-exists", parent, name, "existing=%d errno=%d", existingID, -linuxEEXIST)
			return 0, 0, FuseAttr{}, -linuxEEXIST
		}
		node := p.nodes[existingID]
		if node == nil {
			p.debugChildfLocked("create-stale-existing", parent, name, "existing=%d errno=%d", existingID, -linuxENOENT)
			return 0, 0, FuseAttr{}, -linuxENOENT
		}
		if node.isDir() {
			p.debugChildfLocked("create-existing-dir", parent, name, "existing=%d errno=%d", existingID, -linuxEISDIR)
			return 0, 0, FuseAttr{}, -linuxEISDIR
		}
		p.debugChildfLocked("create-open-existing", parent, name, "existing=%d flags=%#x", existingID, flags)
		if flags&linuxOACCMODE != linuxORDONLY {
			if errno := p.copyUpFileLocked(node); errno != 0 {
				p.debugChildfLocked("create-copyup-error", parent, name, "existing=%d errno=%d", existingID, errno)
				return 0, 0, FuseAttr{}, errno
			}
			if flags&linuxOTRUNC != 0 {
				p.debugChildfLocked("create-truncate-existing", parent, name, "existing=%d", existingID)
				node.data.release(p.dataStore)
				node.data = sparseImageData{}
				node.lowerFile = nil
				node.lowerSize = 0
				node.size = 0
				if uid != 0 {
					node.mode &^= fs.FileMode(0o6000)
				}
				now := time.Now()
				node.modTime = now
				node.ctime = now
				p.refreshImageNodeMetadataLocked(node)
			}
		}
		fh := p.nextHandle
		p.nextHandle++
		p.handles[fh] = imageHandle{nodeID: node.id}
		p.noteImageHandleAddedLocked()
		return node.id, fh, p.attr(node), 0
	}
	mode, gid = p.inheritImageCreateLocked(parentNode, mode, gid, false)
	if parentNode.whiteouts != nil {
		delete(parentNode.whiteouts, name)
	}
	now := time.Now()
	node := &imageNode{
		id:      p.nextNodeID,
		parent:  parent,
		name:    name,
		mode:    fs.FileMode(mode & linuxPermMask),
		uid:     uid,
		gid:     gid,
		atime:   now,
		modTime: now,
		ctime:   now,
	}
	p.nextNodeID++
	p.nodes[node.id] = node
	p.noteImageNodeAddedLocked(node)
	p.registerImageNodeLinkLocked(node)
	parentNode.entries[name] = node.id
	p.noteImageEntryAddedLocked(parentNode)
	p.touchImageDirectoryLocked(parentNode, now)
	fh := p.nextHandle
	p.nextHandle++
	p.handles[fh] = imageHandle{nodeID: node.id}
	p.noteImageHandleAddedLocked()
	return node.id, fh, p.attr(node), 0
}

func (p *imageFS) Write(nodeID uint64, fh uint64, off uint64, data []byte, _ uint32) (uint32, int32) {
	return p.WriteForCaller(nodeID, fh, off, data, 0, 0, 0)
}

func (p *imageFS) WriteForCaller(nodeID uint64, fh uint64, off uint64, data []byte, flags uint32, uid uint32, gid uint32) (uint32, int32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	handle, ok := p.handles[fh]
	node := p.nodes[nodeID]
	if !ok || handle.nodeID != nodeID || node == nil {
		return 0, -linuxEBADF
	}
	if errno := p.copyUpFileLocked(node); errno != 0 {
		return 0, errno
	}
	end := off + uint64(len(data))
	if end < off || end > uint64(^uint(0)>>1) {
		return 0, -linuxEFBIG
	}
	if errno := p.prepareImageOverlayWriteLocked(node, off, uint64(len(data))); errno != 0 {
		return 0, errno
	}
	written, err := node.data.writeAt(p.dataStore, data, off)
	p.refreshImageNodeMetadataLocked(node)
	if err != nil {
		if written > 0 && off+uint64(written) > node.size {
			node.size = off + uint64(written)
		}
		return uint32(written), errnoFromError(err)
	}
	if end > node.size {
		node.size = end
	}
	now := time.Now()
	if uid != 0 && uint32(node.mode)&0o6000 != 0 {
		node.mode &^= fs.FileMode(0o6000)
	}
	node.modTime = now
	node.ctime = now
	return uint32(len(data)), 0
}

func (p *imageFS) SetAttr(nodeID uint64, valid uint32, _ uint64, size uint64, mode uint32, uid uint32, gid uint32, _ time.Time, mtime time.Time) (FuseAttr, int32) {
	return p.SetAttrForCaller(nodeID, valid, 0, size, mode, uid, gid, time.Time{}, mtime, 0, 0)
}

func (p *imageFS) SetAttrForCaller(nodeID uint64, valid uint32, _ uint64, size uint64, mode uint32, uid uint32, gid uint32, atime time.Time, mtime time.Time, callerUID uint32, callerGID uint32) (FuseAttr, int32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	node := p.nodes[nodeID]
	if node == nil {
		return FuseAttr{}, -linuxENOENT
	}
	if !node.isDir() {
		if errno := p.copyUpFileLocked(node); errno != 0 {
			return FuseAttr{}, errno
		}
	}
	if valid&fattrMode != 0 {
		if mode&linuxSIFMT != 0 {
			node.mode = linuxModeToGo(mode)
		} else {
			node.mode = (node.mode &^ fs.FileMode(linuxPermMask)) | fs.FileMode(mode&linuxPermMask)
		}
	}
	if valid&(fattrUID|fattrGID) != 0 && valid&fattrMode == 0 {
		node.mode &^= fs.FileMode(0o6000)
	}
	if valid&fattrUID != 0 {
		node.uid = uid
	}
	if valid&fattrGID != 0 {
		node.gid = gid
	}
	if valid&fattrSize != 0 {
		if err := node.data.truncate(p.dataStore, size); err != nil {
			return FuseAttr{}, errnoFromError(err)
		}
		if size < node.lowerSize {
			node.lowerSize = size
		}
		node.size = size
		p.refreshImageNodeMetadataLocked(node)
		if callerUID != 0 {
			node.mode &^= fs.FileMode(0o6000)
		}
	}
	now := time.Now()
	if valid&fattrSize != 0 {
		node.modTime = now
	}
	if valid&fattrATimeNow != 0 {
		node.atime = now
	} else if valid&fattrATime != 0 && !atime.IsZero() {
		node.atime = atime
	}
	if valid&fattrMTimeNow != 0 {
		node.modTime = now
	} else if valid&fattrMTime != 0 && !mtime.IsZero() {
		node.modTime = mtime
	}
	if valid&(fattrMode|fattrUID|fattrGID|fattrSize|fattrATime|fattrMTime|fattrATimeNow|fattrMTimeNow) != 0 {
		node.ctime = now
	}
	return p.attr(node), 0
}

func (p *imageFS) Unlink(parent uint64, name string) int32 {
	return p.UnlinkForCaller(parent, name, 0, 0)
}

func (p *imageFS) UnlinkForCaller(parent uint64, name string, uid uint32, gid uint32) int32 {
	p.mu.Lock()
	defer p.mu.Unlock()
	parentNode := p.nodes[parent]
	if parentNode == nil {
		return -linuxENOENT
	}
	var ok bool
	name, ok = cleanChildName(name)
	if !ok {
		return -linuxEINVAL
	}
	childID, ok := parentNode.entries[name]
	if !ok {
		p.debugChildfLocked("unlink-miss", parent, name, "errno=%d", -linuxENOENT)
		return -linuxENOENT
	}
	child := p.nodes[childID]
	if child == nil {
		p.debugChildfLocked("unlink-stale-node", parent, name, "node=%d errno=%d", childID, -linuxENOENT)
		return -linuxENOENT
	}
	if child.isDir() {
		p.debugChildfLocked("unlink-dir", parent, name, "node=%d errno=%d", childID, -linuxEISDIR)
		return -linuxEISDIR
	}
	p.debugChildfLocked("unlink", parent, name, "node=%d", childID)
	delete(parentNode.entries, name)
	if parentNode.abstractDir != nil {
		if parentNode.whiteouts == nil {
			parentNode.whiteouts = map[string]bool{}
		}
		parentNode.whiteouts[name] = true
		p.noteImageWhiteoutAddedLocked(parentNode)
	}
	child.ctime = time.Now()
	p.touchImageDirectoryLocked(parentNode, child.ctime)
	if p.imageNodeReferenceCountLocked(childID) == 0 {
		child.nlink = 0
		p.collectImageNodeLocked(childID)
	} else {
		p.refreshImageNodeLinksLocked(child)
	}
	p.compactImageNodeMapsLocked(parentNode)
	return 0
}

func (p *imageFS) Rename(parent uint64, name string, newParent uint64, newName string, flags uint32) int32 {
	return p.RenameForCaller(parent, name, newParent, newName, flags, 0, 0)
}

func (p *imageFS) RenameForCaller(parent uint64, name string, newParent uint64, newName string, flags uint32, uid uint32, gid uint32) int32 {
	p.mu.Lock()
	defer p.mu.Unlock()
	parentNode := p.nodes[parent]
	newParentNode := p.nodes[newParent]
	if parentNode == nil || newParentNode == nil {
		return -linuxENOENT
	}
	var ok bool
	name, ok = cleanChildName(name)
	if !ok {
		return -linuxEINVAL
	}
	newName, ok = cleanChildName(newName)
	if !ok {
		return -linuxEINVAL
	}
	p.debugChildfLocked("rename-old", parent, name, "new_parent=%q new_name=%q flags=%#x", p.pathForNode(newParent), newName, flags)
	p.debugChildfLocked("rename-new", newParent, newName, "old_parent=%q old_name=%q flags=%#x", p.pathForNode(parent), name, flags)
	childID, ok := parentNode.entries[name]
	if !ok {
		p.debugChildfLocked("rename-miss", parent, name, "new_parent=%q new_name=%q errno=%d", p.pathForNode(newParent), newName, -linuxENOENT)
		return -linuxENOENT
	}
	child := p.nodes[childID]
	if child == nil {
		p.debugChildfLocked("rename-stale-node", parent, name, "node=%d errno=%d", childID, -linuxENOENT)
		return -linuxENOENT
	}
	existingID, targetExists := newParentNode.entries[newName]
	if flags&^(linuxRenameNoReplace|linuxRenameExchange) != 0 || flags == linuxRenameNoReplace|linuxRenameExchange {
		return -linuxEINVAL
	}
	if flags&linuxRenameNoReplace != 0 && targetExists {
		return -linuxEEXIST
	}
	if flags&linuxRenameExchange != 0 {
		if !targetExists {
			return -linuxENOENT
		}
		other := p.nodes[existingID]
		if other == nil {
			return -linuxENOENT
		}
		if existingID == childID {
			return 0
		}
		parentNode.entries[name] = existingID
		newParentNode.entries[newName] = childID
		child.parent, child.name = newParent, newName
		other.parent, other.name = parent, name
		p.refreshImageNodeMetadataLocked(child)
		p.refreshImageNodeMetadataLocked(other)
		now := time.Now()
		child.ctime, other.ctime = now, now
		parentNode.modTime, parentNode.ctime = now, now
		newParentNode.modTime, newParentNode.ctime = now, now
		return 0
	}
	// POSIX requires renaming one hard link over another link to the same
	// inode to succeed without removing either directory entry.
	if targetExists && existingID == childID {
		return 0
	}
	var replaced *imageNode
	if existingID, exists := newParentNode.entries[newName]; exists {
		replaced = p.nodes[existingID]
		if replaced != nil && replaced.isDir() && !child.isDir() {
			p.debugChildfLocked("rename-target-dir", newParent, newName, "existing=%d errno=%d", existingID, -linuxEISDIR)
			return -linuxEISDIR
		}
		if replaced != nil && !replaced.isDir() && child.isDir() {
			p.debugChildfLocked("rename-target-not-dir", newParent, newName, "existing=%d errno=%d", existingID, -linuxENOTDIR)
			return -linuxENOTDIR
		}
		p.debugChildfLocked("rename-replace-target", newParent, newName, "existing=%d", existingID)
	}
	delete(parentNode.entries, name)
	if parentNode.abstractDir != nil {
		if parentNode.whiteouts == nil {
			parentNode.whiteouts = map[string]bool{}
		}
		parentNode.whiteouts[name] = true
		p.noteImageWhiteoutAddedLocked(parentNode)
	}
	if newParentNode.whiteouts != nil {
		delete(newParentNode.whiteouts, newName)
	}
	newParentNode.entries[newName] = childID
	p.noteImageEntryAddedLocked(newParentNode)
	child.parent = newParent
	child.name = newName
	p.refreshImageNodeMetadataLocked(child)
	now := time.Now()
	child.ctime = now
	parentNode.modTime, parentNode.ctime = now, now
	newParentNode.modTime, newParentNode.ctime = now, now
	if replaced != nil {
		if p.imageNodeReferenceCountLocked(replaced.id) == 0 {
			replaced.nlink = 0
			p.collectImageNodeLocked(replaced.id)
		} else {
			p.refreshImageNodeLinksLocked(replaced)
		}
	}
	p.refreshImageNodeLinksLocked(child)
	p.compactImageNodeMapsLocked(parentNode)
	if newParentNode != parentNode {
		p.compactImageNodeMapsLocked(newParentNode)
	}
	return 0
}

func (p *imageFS) StatFS(_ uint64) (uint64, uint64, uint64, uint64, uint64, uint64, uint64, uint64, int32) {
	if p.root == "" {
		return 0, 0, 0, 0, 0, 4096, 4096, 255, 0
	}
	return hostStatFS(p.root)
}

func (p *imageFS) GetXattr(nodeID uint64, name string) ([]byte, int32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	node := p.nodes[nodeID]
	if node == nil {
		return nil, -linuxENOENT
	}
	value, ok := node.xattrs[name]
	if !ok {
		return nil, -linuxENODATA
	}
	return append([]byte(nil), value...), 0
}

func (p *imageFS) ListXattr(nodeID uint64) ([]byte, int32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	node := p.nodes[nodeID]
	if node == nil {
		return nil, -linuxENOENT
	}
	names := make([]string, 0, len(node.xattrs))
	for name := range node.xattrs {
		names = append(names, name)
	}
	sort.Strings(names)
	var out []byte
	for _, name := range names {
		out = append(out, name...)
		out = append(out, 0)
	}
	return out, 0
}

func (p *imageFS) SetXattr(nodeID uint64, name string, value []byte, flags uint32) int32 {
	if name == "" || len(name) > 255 || len(value) > 64<<10 || flags&^uint32(3) != 0 || flags == 3 {
		return -linuxEINVAL
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	node := p.nodes[nodeID]
	if node == nil {
		return -linuxENOENT
	}
	_, exists := node.xattrs[name]
	if flags&1 != 0 && exists {
		return -linuxEEXIST
	}
	if flags&2 != 0 && !exists {
		return -linuxENODATA
	}
	oldBytes := 0
	if exists {
		oldBytes = len(name) + len(node.xattrs[name]) + imageXattrEntryOverhead
	}
	newBytes := len(name) + len(value) + imageXattrEntryOverhead
	nodeBytes := imageNodeXattrBytes(node) - oldBytes + newBytes
	if nodeBytes > imageMaxXattrBytesPerNode || int64(p.xattrBytes)-int64(oldBytes)+int64(newBytes) > imageMaxXattrBytes {
		return -linuxENOSPC
	}
	if node.xattrs == nil {
		node.xattrs = make(map[string][]byte)
	}
	node.xattrs[name] = append([]byte(nil), value...)
	p.xattrBytes = uint64(int64(p.xattrBytes) - int64(oldBytes) + int64(newBytes))
	p.refreshImageNodeMetadataLocked(node)
	node.ctime = time.Now()
	return 0
}

func (p *imageFS) RemoveXattr(nodeID uint64, name string) int32 {
	p.mu.Lock()
	defer p.mu.Unlock()
	node := p.nodes[nodeID]
	if node == nil {
		return -linuxENOENT
	}
	if _, exists := node.xattrs[name]; !exists {
		return -linuxENODATA
	}
	p.xattrBytes -= uint64(len(name) + len(node.xattrs[name]) + imageXattrEntryOverhead)
	delete(node.xattrs, name)
	p.refreshImageNodeMetadataLocked(node)
	node.ctime = time.Now()
	return 0
}

func (p *imageFS) attr(node *imageNode) FuseAttr {
	var mode uint32
	size := node.size
	modTime := node.modTime
	nodeMode := node.mode
	switch {
	case node.abstractFile != nil:
		size, nodeMode = node.abstractFile.Stat()
		if mt := node.abstractFile.ModTime(); !mt.IsZero() {
			modTime = mt
		}
	case node.abstractDir != nil:
		nodeMode = fs.ModeDir | node.abstractDir.Stat()
		if mt := node.abstractDir.ModTime(); !mt.IsZero() {
			modTime = mt
		}
	case node.abstractLink != nil:
		nodeMode = fs.ModeSymlink | node.abstractLink.Stat().Perm()
		node.symlinkTarget = node.abstractLink.Target()
		size = uint64(len(node.symlinkTarget))
		if mt := node.abstractLink.ModTime(); !mt.IsZero() {
			modTime = mt
		}
	}
	isDir := node.isDir()
	isSymlink := node.isSymlink()
	switch {
	case isDir:
		mode = linuxSIFDIR | linuxModeBits(nodeMode)
	case isSymlink:
		mode = linuxSIFLNK | linuxModeBits(nodeMode)
	case node.rawMode != 0:
		mode = (node.rawMode &^ linuxPermMask) | linuxModeBits(nodeMode)
	default:
		mode = linuxSIFREG | linuxModeBits(nodeMode)
	}
	nlink := uint32(1)
	if isDir {
		nlink = 2
		for _, childID := range node.entries {
			if child := p.nodes[childID]; child != nil && child.isDir() {
				nlink++
			}
		}
	} else {
		nlink = p.imageNodeLinkCountLocked(node)
	}
	attrUID, attrGID := node.uid, node.gid
	if p.mapOwner {
		attrUID, attrGID = p.ownerUID, p.ownerGID
	}
	atime, ctime := node.atime, node.ctime
	if atime.IsZero() {
		atime = modTime
	}
	if ctime.IsZero() {
		ctime = modTime
	}
	allocatedBytes := node.data.allocatedBytes(size)
	if node.abstractFile != nil || node.lowerFile != nil {
		allocatedBytes = size
	}
	return FuseAttr{
		Ino:       p.imageNodeInodeLocked(node),
		Size:      size,
		Blocks:    (allocatedBytes + 511) / 512,
		ATimeSec:  uint64(atime.Unix()),
		MTimeSec:  uint64(modTime.Unix()),
		CTimeSec:  uint64(ctime.Unix()),
		ATimeNsec: uint32(atime.Nanosecond()),
		MTimeNsec: uint32(modTime.Nanosecond()),
		CTimeNsec: uint32(ctime.Nanosecond()),
		Mode:      mode,
		NLink:     nlink,
		UID:       attrUID,
		GID:       attrGID,
		RDev:      node.rdev,
		BlkSize:   4096,
	}
}

func (p *imageFS) registerImageNodeLinkLocked(node *imageNode) {
	if node == nil || node.isDir() {
		return
	}
	if node.nlink == 0 {
		node.nlink = 1
	}
}

func (p *imageFS) imageNodeLinkCountLocked(node *imageNode) uint32 {
	if node == nil || node.isDir() {
		return 1
	}
	if node.nlink != 0 {
		return node.nlink
	}
	return 1
}

func (p *imageFS) refreshImageNodeLinksLocked(node *imageNode) {
	if node == nil || node.isDir() {
		return
	}
	inode := p.imageNodeInodeLocked(node)
	nlink := uint32(0)
	for id, candidate := range p.nodes {
		if candidate != nil && !candidate.isDir() && p.imageNodeInodeLocked(candidate) == inode {
			nlink += p.imageNodeReferenceCountLocked(id)
		}
	}
	if nlink == 0 {
		nlink = 1
	}
	for _, candidate := range p.nodes {
		if candidate != nil && !candidate.isDir() && p.imageNodeInodeLocked(candidate) == inode {
			candidate.nlink = nlink
		}
	}
}

func (p *imageFS) imageNodeReferenceCountLocked(nodeID uint64) uint32 {
	var count uint32
	for _, candidate := range p.nodes {
		if candidate == nil || !candidate.isDir() {
			continue
		}
		for _, childID := range candidate.entries {
			if childID == nodeID {
				count++
			}
		}
	}
	return count
}

func (p *imageFS) imageNodeHasHandleLocked(nodeID uint64) bool {
	for _, handle := range p.handles {
		if handle.nodeID == nodeID {
			return true
		}
	}
	for _, handleNodeID := range p.dirHandleNodes {
		if handleNodeID == nodeID {
			return true
		}
	}
	return false
}

func (p *imageFS) touchImageDirectoryLocked(node *imageNode, now time.Time) {
	if node == nil || !node.isDir() {
		return
	}
	node.modTime = now
	node.ctime = now
}

func (p *imageFS) collectImageNodeLocked(nodeID uint64) {
	// The root has no parent directory entry, so its reference count is always
	// zero. Releasing an open root directory must never collect it: persistent
	// guest shells routinely open and close / while resolving paths.
	if nodeID == 1 {
		return
	}
	node := p.nodes[nodeID]
	if node == nil {
		return
	}
	// Regular-file link counts are maintained when directory entries change.
	// Most Release calls therefore prove the node is still linked in O(1),
	// instead of rescanning every directory after every close. Directories are
	// not hard-linkable and remain on the conservative reference scan.
	linked := node.nlink != 0
	if node.isDir() {
		linked = p.imageNodeReferenceCountLocked(nodeID) != 0
	}
	if !linked && !p.imageNodeHasHandleLocked(nodeID) {
		if node := p.nodes[nodeID]; node != nil {
			p.xattrBytes -= uint64(imageNodeXattrBytes(node))
			p.retainedEntries -= node.retainedEntries
			p.retainedWhiteouts -= node.retainedWhiteouts
			node.data.release(p.dataStore)
			p.dynamicMetadata -= node.accountedMetadata
		}
		delete(p.nodes, nodeID)
		p.compactImageNodesLocked()
	}
}

func (p *imageFS) Close() error {
	return p.closeWithin(2 * time.Second)
}

func (p *imageFS) BeginClose() {
	if p == nil {
		return
	}
	p.beginClose.Do(func() {
		p.mu.Lock()
		p.closed = true
		p.materializationCancel()
		for _, materialization := range p.materializations {
			materialization.cancel()
		}
		p.mu.Unlock()
	})
}

func (p *imageFS) closeWithin(timeout time.Duration) error {
	if p == nil {
		return nil
	}
	p.BeginClose()
	p.closeStart.Do(func() {
		p.mu.Lock()
		p.closeDone = make(chan struct{})
		done := p.closeDone
		p.mu.Unlock()
		// Legacy lower filesystems cannot always interrupt an in-flight host I/O
		// operation. Keep ownership of both the worker and backing store until it
		// actually returns, but do not make VM teardown wait without a bound.
		go func() {
			p.materializationWG.Wait()
			p.closeErr = p.dataStore.close()
			close(done)
		}()
	})
	p.mu.Lock()
	done := p.closeDone
	p.mu.Unlock()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
		return p.closeErr
	case <-timer.C:
		return &CloseIncompleteError{Resource: "image filesystem lower operation", Timeout: timeout}
	}
}

func (p *imageFS) BackingUsage() (uint64, uint64, uint64, error) {
	if p == nil {
		return 0, 0, 0, nil
	}
	return p.dataStore.usage()
}

func (p *imageFS) BackingCurrent() uint64 {
	if p == nil {
		return 0
	}
	p.dataStore.mu.Lock()
	defer p.dataStore.mu.Unlock()
	return p.dataStore.current
}

func (p *imageFS) BackingMetadataUsage() (uint64, uint64) {
	if p == nil {
		return 0, 0
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	// This is deliberately accounting, not a guest quota. Empty files and page
	// indexes consume host heap even when no backing blocks are allocated; make
	// that pressure visible to the same host recovery telemetry as file data.
	metadata := uint64(p.retainedNodes)*256 + uint64(p.retainedHandles)*64 + uint64(p.retainedDirHandles)*64 + p.xattrBytes
	metadata += uint64(p.retainedEntries+p.retainedWhiteouts) * 64
	metadata += uint64(len(p.materializations)) * 128
	metadata += p.dynamicMetadata
	metadata += p.dataStore.metadataUsage()
	if metadata > p.metadataHighWater {
		p.metadataHighWater = metadata
	}
	return metadata, p.metadataHighWater
}

func (p *imageFS) noteImageNodeAddedLocked(node *imageNode) {
	p.retainedNodes = max(p.retainedNodes, len(p.nodes))
	p.refreshImageNodeMetadataLocked(node)
	p.bumpImageMetadataFloorLocked()
}

func (p *imageFS) refreshImageNodeMetadataLocked(node *imageNode) {
	if node == nil {
		return
	}
	current := uint64(len(node.name)+len(node.symlinkTarget)) + uint64(cap(node.data.extents))*32
	for _, value := range node.xattrs {
		current += uint64(cap(value) - len(value))
	}
	if current >= node.accountedMetadata {
		p.dynamicMetadata += current - node.accountedMetadata
	} else {
		p.dynamicMetadata -= node.accountedMetadata - current
	}
	node.accountedMetadata = current
}

func (p *imageFS) noteImageHandleAddedLocked() {
	p.retainedHandles = max(p.retainedHandles, len(p.handles))
	p.bumpImageMetadataFloorLocked()
}

func (p *imageFS) noteImageDirHandleAddedLocked() {
	p.retainedDirHandles = max(p.retainedDirHandles, len(p.dirHandles))
	p.bumpImageMetadataFloorLocked()
}

func (p *imageFS) noteImageEntryAddedLocked(node *imageNode) {
	if node == nil || len(node.entries) <= node.retainedEntries {
		return
	}
	p.retainedEntries += len(node.entries) - node.retainedEntries
	node.retainedEntries = len(node.entries)
	p.bumpImageMetadataFloorLocked()
}

func (p *imageFS) noteImageWhiteoutAddedLocked(node *imageNode) {
	if node == nil || len(node.whiteouts) <= node.retainedWhiteouts {
		return
	}
	p.retainedWhiteouts += len(node.whiteouts) - node.retainedWhiteouts
	node.retainedWhiteouts = len(node.whiteouts)
	p.bumpImageMetadataFloorLocked()
}

func (p *imageFS) compactImageNodeMapsLocked(node *imageNode) {
	if node == nil {
		return
	}
	if len(node.entries)*4 < node.retainedEntries && (node.retainedEntries >= 64 || len(node.entries) <= 4) {
		rebuilt := make(map[string]uint64, len(node.entries))
		for name, id := range node.entries {
			rebuilt[name] = id
		}
		p.retainedEntries -= node.retainedEntries - len(rebuilt)
		node.retainedEntries = len(rebuilt)
		node.entries = rebuilt
	}
	if len(node.whiteouts)*4 < node.retainedWhiteouts && (node.retainedWhiteouts >= 64 || len(node.whiteouts) <= 4) {
		rebuilt := make(map[string]bool, len(node.whiteouts))
		for name, present := range node.whiteouts {
			rebuilt[name] = present
		}
		p.retainedWhiteouts -= node.retainedWhiteouts - len(rebuilt)
		node.retainedWhiteouts = len(rebuilt)
		node.whiteouts = rebuilt
	}
}

func (p *imageFS) compactImageNodesLocked() {
	if len(p.nodes)*4 >= p.retainedNodes || p.retainedNodes < 64 && len(p.nodes) > 4 {
		return
	}
	rebuilt := make(map[uint64]*imageNode, len(p.nodes))
	for id, node := range p.nodes {
		rebuilt[id] = node
	}
	p.nodes = rebuilt
	p.retainedNodes = len(rebuilt)
}

func (p *imageFS) compactImageHandleMapsLocked() {
	if len(p.handles)*4 >= p.retainedHandles || p.retainedHandles < 64 && !(len(p.handles) == 0 && p.retainedHandles >= 16) {
		return
	}
	rebuilt := make(map[uint64]imageHandle, len(p.handles))
	for id, handle := range p.handles {
		rebuilt[id] = handle
	}
	p.handles = rebuilt
	p.retainedHandles = len(rebuilt)
}

func (p *imageFS) compactImageDirHandleMapsLocked() {
	if len(p.dirHandles)*4 >= p.retainedDirHandles || p.retainedDirHandles < 64 && !(len(p.dirHandles) == 0 && p.retainedDirHandles >= 16) {
		return
	}
	handles := make(map[uint64][]dirEntry, len(p.dirHandles))
	nodes := make(map[uint64]uint64, len(p.dirHandleNodes))
	for id, entries := range p.dirHandles {
		handles[id] = entries
	}
	for id, node := range p.dirHandleNodes {
		nodes[id] = node
	}
	p.dirHandles = handles
	p.dirHandleNodes = nodes
	p.retainedDirHandles = len(handles)
}

func (p *imageFS) bumpImageMetadataFloorLocked() {
	floor := uint64(p.retainedNodes)*256 + uint64(p.retainedHandles+p.retainedDirHandles+p.retainedEntries+p.retainedWhiteouts)*64 + p.xattrBytes
	if floor > p.metadataHighWater {
		p.metadataHighWater = floor
	}
}

func imageNodeXattrBytes(node *imageNode) int {
	if node == nil {
		return 0
	}
	total := 0
	for name, value := range node.xattrs {
		total += len(name) + len(value) + imageXattrEntryOverhead
	}
	return total
}

func (p *imageFS) imageNodeInodeLocked(node *imageNode) uint64 {
	if node == nil {
		return 0
	}
	if node.inode != 0 {
		return node.inode
	}
	return node.id
}

func (p *imageFS) dirType(node *imageNode) uint32 {
	switch {
	case node.isDir():
		return dirTypeDir
	case node.isSymlink():
		return dirTypeLink
	case node.rawMode&linuxSIFMT == linuxSIFCHR:
		return dirTypeChar
	case node.rawMode&linuxSIFMT == linuxSIFBLK:
		return dirTypeBlock
	case node.rawMode&linuxSIFMT == linuxSIFIFO:
		return dirTypeFIFO
	case node.rawMode&linuxSIFMT == linuxSIFSOCK:
		return dirTypeSocket
	default:
		return dirTypeFile
	}
}

func (p *imageFS) createAbstractNode(parent *imageNode, name string, entry imagefs.Entry) (*imageNode, int32) {
	if parent.whiteouts[name] {
		return nil, -linuxENOENT
	}
	node := &imageNode{
		id:      p.nextNodeID,
		parent:  parent.id,
		name:    name,
		entries: map[string]uint64{},
		modTime: time.Unix(0, 0),
	}
	p.nextNodeID++
	switch {
	case entry.Dir != nil:
		node.abstractDir = entry.Dir
		node.mode = fs.ModeDir | entry.Dir.Stat()
		node.modTime = entry.Dir.ModTime()
		node.uid, node.gid = entry.Dir.Owner()
		node.rdev = entry.Dir.RDev()
	case entry.File != nil:
		node.abstractFile = entry.File
		node.size, node.mode = entry.File.Stat()
		node.modTime = entry.File.ModTime()
		node.uid, node.gid = entry.File.Owner()
		node.rdev = entry.File.RDev()
	case entry.Symlink != nil:
		node.abstractLink = entry.Symlink
		node.mode = fs.ModeSymlink | entry.Symlink.Stat().Perm()
		node.symlinkTarget = entry.Symlink.Target()
		node.size = uint64(len(node.symlinkTarget))
		node.modTime = entry.Symlink.ModTime()
		node.uid, node.gid = entry.Symlink.Owner()
		node.rdev = entry.Symlink.RDev()
	default:
		return nil, -linuxENOENT
	}
	if node.modTime.IsZero() {
		node.modTime = time.Unix(0, 0)
	}
	p.nodes[node.id] = node
	p.noteImageNodeAddedLocked(node)
	p.registerImageNodeLinkLocked(node)
	parent.entries[name] = node.id
	p.noteImageEntryAddedLocked(parent)
	return node, 0
}

func (p *imageFS) materializeDirEntries(nodeID uint64) int32 {
	p.mu.Lock()
	node := p.nodes[nodeID]
	if node == nil {
		p.mu.Unlock()
		return -linuxENOENT
	}
	if !node.isDir() {
		p.mu.Unlock()
		return -linuxENOTDIR
	}
	lowerDir := node.abstractDir
	lowerNode := node
	if lowerDir == nil || node.entriesDone {
		p.mu.Unlock()
		return 0
	}
	p.mu.Unlock()

	ents, err := lowerDir.ReadDir()
	if err != nil {
		return errnoFromError(err)
	}
	sort.Slice(ents, func(i, j int) bool { return ents[i].Name < ents[j].Name })
	type lowerEntry struct {
		name  string
		entry imagefs.Entry
	}
	lowerEntries := make([]lowerEntry, 0, len(ents))
	for _, ent := range ents {
		if ent.Name == "." || ent.Name == ".." {
			continue
		}
		entry, err := lowerDir.Lookup(ent.Name)
		if err != nil {
			return errnoFromError(err)
		}
		lowerEntries = append(lowerEntries, lowerEntry{name: ent.Name, entry: entry})
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	node = p.nodes[nodeID]
	if node == nil {
		return -linuxENOENT
	}
	if node != lowerNode {
		return 0
	}
	if node.entriesDone {
		return 0
	}
	for _, ent := range lowerEntries {
		if node.whiteouts[ent.name] {
			continue
		}
		if _, ok := node.entries[ent.name]; ok {
			continue
		}
		if _, errno := p.createAbstractNode(node, ent.name, ent.entry); errno != 0 {
			return errno
		}
	}
	node.entriesDone = true
	return 0
}

type imageLowerEntry struct {
	name  string
	entry imagefs.Entry
}

type imageDirMaterialization struct {
	done   chan struct{}
	cancel context.CancelFunc
	node   *imageNode
	err    error
}

func readImageLowerEntries(ctx context.Context, lowerDir imagefs.Directory) ([]imageLowerEntry, error) {
	ents, err := imagefs.ReadDirContext(ctx, lowerDir)
	if err != nil {
		return nil, err
	}
	sort.Slice(ents, func(i, j int) bool { return ents[i].Name < ents[j].Name })
	entries := make([]imageLowerEntry, 0, len(ents))
	for _, ent := range ents {
		if ent.Name == "." || ent.Name == ".." {
			continue
		}
		entry, err := imagefs.LookupContext(ctx, lowerDir, ent.Name)
		if err != nil {
			return nil, fmt.Errorf("lookup lower entry %q: %w", ent.Name, err)
		}
		entries = append(entries, imageLowerEntry{name: ent.Name, entry: entry})
	}
	return entries, nil
}

// materializeDirEntriesContext keeps potentially blocking lower-filesystem I/O
// outside imageFS.mu. Legacy imagefs implementations do not accept a context,
// so one background read may outlive a canceled caller. The in-flight record
// prevents repeated cancellations from creating unbounded readers for the same
// directory, and the eventual result is committed or discarded under the lock.
func (p *imageFS) materializeDirEntriesContext(ctx context.Context, nodeID uint64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p.mu.Lock()
	node := p.nodes[nodeID]
	if node == nil {
		p.mu.Unlock()
		return os.ErrNotExist
	}
	if !node.isDir() {
		p.mu.Unlock()
		return fmt.Errorf("%s is not a directory", p.pathForNode(nodeID))
	}
	lowerDir := node.abstractDir
	if lowerDir == nil || node.entriesDone {
		p.mu.Unlock()
		return nil
	}
	if p.closed {
		p.mu.Unlock()
		return os.ErrClosed
	}
	materialization := p.materializations[nodeID]
	if materialization == nil {
		if p.materializations == nil {
			p.materializations = make(map[uint64]*imageDirMaterialization)
		}
		materializationCtx, cancel := context.WithCancel(p.materializationCtx)
		materialization = &imageDirMaterialization{done: make(chan struct{}), cancel: cancel, node: node}
		p.materializations[nodeID] = materialization
		p.materializationWG.Add(1)
		go p.finishDirMaterialization(materializationCtx, nodeID, lowerDir, materialization)
	}
	p.mu.Unlock()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-materialization.done:
		return materialization.err
	}
}

func (p *imageFS) finishDirMaterialization(ctx context.Context, nodeID uint64, lowerDir imagefs.Directory, materialization *imageDirMaterialization) {
	defer p.materializationWG.Done()
	defer materialization.cancel()
	entries, err := readImageLowerEntries(ctx, lowerDir)
	p.mu.Lock()
	defer p.mu.Unlock()
	node := p.nodes[nodeID]
	if err == nil && node == materialization.node && !node.entriesDone {
		for _, ent := range entries {
			if node.whiteouts[ent.name] {
				continue
			}
			if _, ok := node.entries[ent.name]; ok {
				continue
			}
			if _, errno := p.createAbstractNode(node, ent.name, ent.entry); errno != 0 {
				err = fmt.Errorf("materialize %s: errno %d", p.pathForNode(nodeID), errno)
				break
			}
		}
		if err == nil {
			node.entriesDone = true
		}
	} else if err == nil && node == nil {
		err = os.ErrNotExist
	}
	materialization.err = err
	if p.materializations[nodeID] == materialization {
		delete(p.materializations, nodeID)
	}
	close(materialization.done)
}

func (p *imageFS) copyUpFileLocked(node *imageNode) int32 {
	if node == nil {
		return -linuxENOENT
	}
	if node.abstractDir != nil {
		return -linuxEISDIR
	}
	if node.abstractLink != nil {
		return -linuxEINVAL
	}
	if node.abstractFile == nil {
		return 0
	}
	size, mode := node.abstractFile.Stat()
	node.size = size
	node.mode = mode
	node.lowerFile = node.abstractFile
	node.lowerSize = size
	node.abstractFile = nil
	if node.modTime.IsZero() {
		node.modTime = time.Now()
	}
	return 0
}

// prepareImageOverlayWriteLocked materializes only partially overwritten lower
// pages. Complete pages can be created directly from the write payload. This is
// the page-level COW boundary that prevents a one-byte change from copying an
// entire lower file (and from expanding sparse lower files).
func (p *imageFS) prepareImageOverlayWriteLocked(node *imageNode, off, length uint64) int32 {
	if node == nil || node.lowerFile == nil || length == 0 {
		return 0
	}
	defer p.refreshImageNodeMetadataLocked(node)
	end := off + length
	if end < off {
		return -linuxEFBIG
	}
	for cursor := off; cursor < end; {
		page := cursor / imageDataPageSize
		pageStart := page * imageDataPageSize
		pageEnd := pageStart + imageDataPageSize
		writeEnd := min(end, pageEnd)
		fullPage := cursor == pageStart && writeEnd == pageEnd
		if !fullPage {
			if _, exists := node.data.location(page); !exists && pageStart < node.lowerSize {
				visible := min(imageDataPageSize, node.lowerSize-pageStart)
				data, err := node.lowerFile.ReadAt(pageStart, uint32(visible))
				if err != nil {
					return errnoFromError(err)
				}
				if uint64(len(data)) != visible {
					return -linuxEIO
				}
				location, err := p.dataStore.allocatePage(data)
				if err != nil {
					return errnoFromError(err)
				}
				node.data.insert(page, location)
			}
		}
		cursor = writeEnd
	}
	return 0
}

func (n *imageNode) isDir() bool {
	return n.abstractDir != nil || n.mode&fs.ModeDir != 0
}

func (n *imageNode) isSymlink() bool {
	return n.abstractLink != nil || n.mode&fs.ModeSymlink != 0
}

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

const (
	linuxEPERM      = linuxabi.EPERM
	linuxENOENT     = linuxabi.ENOENT
	linuxENXIO      = linuxabi.ENXIO
	linuxEIO        = linuxabi.EIO
	linuxEBADF      = linuxabi.EBADF
	linuxEACCES     = linuxabi.EACCES
	linuxEPIPE      = linuxabi.EPIPE
	linuxEEXIST     = linuxabi.EEXIST
	linuxENOTDIR    = linuxabi.ENOTDIR
	linuxEISDIR     = linuxabi.EISDIR
	linuxEINVAL     = linuxabi.EINVAL
	linuxENOTTY     = linuxabi.ENOTTY
	linuxEFBIG      = linuxabi.EFBIG
	linuxENOSPC     = linuxabi.ENOSPC
	linuxERANGE     = linuxabi.ERANGE
	linuxENOSYS     = linuxabi.ENOSYS
	linuxENOTEMPTY  = linuxabi.ENOTEMPTY
	linuxENODATA    = linuxabi.ENODATA
	linuxEOPNOTSUPP = linuxabi.EOPNOTSUPP
	linuxETIMEDOUT  = linuxabi.ETIMEDOUT
)

func goModeToLinux(mode fs.FileMode) fs.FileMode {
	perm := mode.Perm()
	if mode&fs.ModeSetuid != 0 {
		perm |= 0o4000
	}
	if mode&fs.ModeSetgid != 0 {
		perm |= 0o2000
	}
	if mode&fs.ModeSticky != 0 {
		perm |= 0o1000
	}
	switch {
	case mode&fs.ModeDir != 0:
		perm |= fs.FileMode(linuxSIFDIR)
	case mode&fs.ModeSymlink != 0:
		perm |= fs.FileMode(linuxSIFLNK)
	case mode&fs.ModeNamedPipe != 0:
		perm |= fs.FileMode(linuxSIFIFO)
	case mode&fs.ModeDevice != 0 && mode&fs.ModeCharDevice != 0:
		perm |= fs.FileMode(linuxSIFCHR)
	case mode&fs.ModeDevice != 0:
		perm |= fs.FileMode(linuxSIFBLK)
	case mode&fs.ModeSocket != 0:
		perm |= fs.FileMode(linuxSIFSOCK)
	default:
		perm |= fs.FileMode(linuxSIFREG)
	}
	return perm
}

func linuxModeBits(mode fs.FileMode) uint32 {
	return uint32(mode & fs.FileMode(linuxPermMask))
}

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

func maxU32(a, b uint32) uint32 {
	if a > b {
		return a
	}
	return b
}

func dirTypeForMode(mode os.FileMode) uint32 {
	switch {
	case mode&os.ModeDir != 0:
		return dirTypeDir
	case mode&os.ModeSymlink != 0:
		return dirTypeLink
	case mode&os.ModeDevice != 0 && mode&os.ModeCharDevice != 0:
		return dirTypeChar
	case mode&os.ModeDevice != 0:
		return dirTypeBlock
	case mode&os.ModeNamedPipe != 0:
		return dirTypeFIFO
	case mode&os.ModeSocket != 0:
		return dirTypeSocket
	default:
		return dirTypeFile
	}
}

func errnoFromError(err error) int32 {
	var pathErr *os.PathError
	var linkErr *os.LinkError
	if os.IsNotExist(err) {
		return -linuxENOENT
	}
	if os.IsPermission(err) {
		return -linuxEPERM
	}
	if os.IsExist(err) {
		return -linuxEEXIST
	}
	if os.IsTimeout(err) {
		return -linuxETIMEDOUT
	}
	if strings.Contains(err.Error(), "is a directory") {
		return -linuxEISDIR
	}
	if strings.Contains(err.Error(), "not a directory") {
		return -linuxENOTDIR
	}
	if ok := errorAs(err, &pathErr); ok {
		if errno, ok := mapHostError(pathErr.Err); ok {
			return -errno
		}
	}
	if errors.As(err, &linkErr) {
		if errno, ok := mapHostError(linkErr.Err); ok {
			return -errno
		}
	}
	if errno, ok := mapHostError(err); ok {
		return -errno
	}
	return -linuxEIO
}

func errorAs(err error, target any) bool {
	switch t := target.(type) {
	case **os.PathError:
		if v, ok := err.(*os.PathError); ok {
			*t = v
			return true
		}
	}
	return false
}

func translateLinuxOpenFlags(flags uint32, writebackCache bool) int {
	openFlags := 0
	switch flags & 0x3 {
	case linuxOWRONLY:
		if writebackCache {
			openFlags |= os.O_RDWR
		} else {
			openFlags |= os.O_WRONLY
		}
	case linuxORDWR:
		openFlags |= os.O_RDWR
	default:
		openFlags |= os.O_RDONLY
	}
	if flags&linuxOCREAT != 0 {
		openFlags |= os.O_CREATE
	}
	if flags&linuxOEXCL != 0 {
		openFlags |= os.O_EXCL
	}
	if flags&linuxOTRUNC != 0 {
		openFlags |= os.O_TRUNC
	}
	if flags&linuxOAPPEND != 0 {
		openFlags |= os.O_APPEND
	}
	return openFlags
}
