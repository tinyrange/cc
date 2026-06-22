package nfs

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/linuxabi"
	"j5.nz/cc/internal/virtio"
)

const (
	PortmapPort = 111
	NFSPort     = 2049
	MountPort   = 20048

	nfsOK             = 0
	nfsErrPerm        = 1
	nfsErrNoEnt       = 2
	nfsErrIO          = 5
	nfsErrNXIO        = 6
	nfsErrAcces       = 13
	nfsErrExist       = 17
	nfsErrXDev        = 18
	nfsErrNotDir      = 20
	nfsErrIsDir       = 21
	nfsErrInval       = 22
	nfsErrFBig        = 27
	nfsErrNoSpc       = 28
	nfsErrROFS        = 30
	nfsErrMLink       = 31
	nfsErrNameTooLong = 63
	nfsErrNotEmpty    = 66
	nfsErrDQuot       = 69
	nfsErrStale       = 70
	nfsErrBadHandle   = 10001
	nfsErrNotSupp     = 10004

	nfsFileReg  = 1
	nfsFileDir  = 2
	nfsFileBlk  = 3
	nfsFileChr  = 4
	nfsFileLnk  = 5
	nfsFileSock = 6
	nfsFileFIFO = 7

	fuseDirentBaseSize = 24
)

type Network interface {
	ListenInternal(network, address string) (net.Listener, error)
}

type packetNetwork interface {
	ListenPacketInternal(network, address string) (net.PacketConn, error)
}

type Server struct {
	network Network

	mu        sync.Mutex
	nextID    uint64
	exports   map[string]*Export
	byMount   map[string]*Export
	listeners []net.Listener
	packets   []net.PacketConn
	wg        sync.WaitGroup
}

type Export struct {
	ID       uint64
	Name     string
	Mount    string
	Source   string
	Writable bool
	Backend  virtio.FSBackend
}

func New(network Network) *Server {
	return &Server{
		network: network,
		nextID:  1,
		exports: map[string]*Export{},
		byMount: map[string]*Export{},
	}
}

func (s *Server) Start() error {
	if s == nil || s.network == nil {
		return fmt.Errorf("nfs network is not configured")
	}
	for _, spec := range []struct {
		port int
		fn   rpcHandler
	}{
		{PortmapPort, s.handlePortmap},
		{MountPort, s.handleMount},
		{NFSPort, s.handleNFS},
	} {
		ln, err := s.network.ListenInternal("tcp", fmt.Sprintf(":%d", spec.port))
		if err != nil {
			s.Close()
			return fmt.Errorf("listen nfs tcp/%d: %w", spec.port, err)
		}
		s.listeners = append(s.listeners, ln)
		serveRPCListener(ln, spec.fn, &s.wg)
	}
	if packetNet, ok := s.network.(packetNetwork); ok {
		for _, spec := range []struct {
			port int
			fn   rpcHandler
		}{
			{PortmapPort, s.handlePortmap},
			{MountPort, s.handleMount},
		} {
			pc, err := packetNet.ListenPacketInternal("udp", fmt.Sprintf(":%d", spec.port))
			if err != nil {
				s.Close()
				return fmt.Errorf("listen nfs udp/%d: %w", spec.port, err)
			}
			s.packets = append(s.packets, pc)
			serveRPCPacketConn(pc, spec.fn, &s.wg)
		}
	}
	return nil
}

func (s *Server) Close() error {
	if s == nil {
		return nil
	}
	for _, ln := range s.listeners {
		_ = ln.Close()
	}
	s.listeners = nil
	for _, pc := range s.packets {
		_ = pc.Close()
	}
	s.packets = nil
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
	}
	return nil
}

func (s *Server) AddShare(share client.ShareMount) (*Export, error) {
	if s == nil {
		return nil, fmt.Errorf("nfs server is not configured")
	}
	key := cleanGuestMount(share.Mount)
	if key == "/" {
		return nil, fmt.Errorf("nfs share mount path is required")
	}
	s.mu.Lock()
	if existing := s.byMount[key]; existing != nil {
		s.mu.Unlock()
		if existing.Source == share.Source && existing.Writable == share.Writable {
			return existing, nil
		}
		return nil, fmt.Errorf("share mount %q already exists", key)
	}
	id := s.nextID
	s.nextID++
	s.mu.Unlock()

	backend, err := buildNFSBackend(share)
	if err != nil {
		return nil, err
	}
	exp := &Export{
		ID:       id,
		Name:     fmt.Sprintf("/ccx3/%d", id),
		Mount:    key,
		Source:   share.Source,
		Writable: share.Writable,
		Backend:  backend,
	}
	s.mu.Lock()
	s.exports[exp.Name] = exp
	s.byMount[key] = exp
	s.mu.Unlock()
	return exp, nil
}

func buildNFSBackend(share client.ShareMount) (virtio.FSBackend, error) {
	source := strings.TrimSpace(share.Source)
	if source == "" {
		return nil, fmt.Errorf("share source is required")
	}
	info, err := os.Stat(source)
	if err != nil {
		return nil, fmt.Errorf("stat share source: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("share source %q is not a directory", source)
	}
	if share.MapOwner {
		return virtio.NewPassthroughFSWithOwner(source, nil, share.OwnerUID, share.OwnerGID), nil
	}
	return virtio.NewPassthroughFS(source, nil), nil
}

func (s *Server) ExportForMount(mountPath string) (*Export, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	exp := s.byMount[cleanGuestMount(mountPath)]
	return exp, exp != nil
}

func cleanGuestMount(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "/"
	}
	return path.Clean("/" + strings.TrimPrefix(value, "/"))
}

func (s *Server) exportByName(name string) *Export {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.exports[name]
}

func (s *Server) exportByID(id uint64) *Export {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, exp := range s.exports {
		if exp.ID == id {
			return exp
		}
	}
	return nil
}

func fileHandle(exportID, nodeID uint64) []byte {
	out := make([]byte, 16)
	binary.BigEndian.PutUint64(out[:8], exportID)
	binary.BigEndian.PutUint64(out[8:], nodeID)
	return out
}

func parseFileHandle(r *xdrReader) (uint64, uint64, error) {
	data, err := r.Opaque(64)
	if err != nil {
		return 0, 0, err
	}
	if len(data) != 16 {
		return 0, 0, fmt.Errorf("unsupported nfs file handle length %d", len(data))
	}
	return binary.BigEndian.Uint64(data[:8]), binary.BigEndian.Uint64(data[8:]), nil
}

func (s *Server) handlePortmap(call rpcCall) ([]byte, uint32) {
	if call.prog != progPortmap {
		return nil, rpcProgUnavail
	}
	debugf("portmap proc=%d request=%d", call.proc, len(call.body))
	var w xdrWriter
	if call.version == rpcbindVersion3 || call.version == rpcbindVersion4 {
		switch call.proc {
		case rpcbindProcNull:
			return nil, rpcAcceptSuccess
		case rpcbindProcGetAddr:
			r := newXDRReader(call.body)
			prog, err := r.Uint32()
			if err != nil {
				return nil, rpcGarbageArgs
			}
			vers, err := r.Uint32()
			if err != nil {
				return nil, rpcGarbageArgs
			}
			netid, err := r.String(256)
			if err != nil {
				return nil, rpcGarbageArgs
			}
			_, _ = r.String(512) // address
			_, _ = r.String(256) // owner
			w.String(rpcbindUniversalAddress(prog, vers, netid))
			return w.Bytes(), rpcAcceptSuccess
		default:
			return nil, rpcProcUnavail
		}
	}
	if call.version != portmapVersion {
		return nil, rpcProgUnavail
	}
	switch call.proc {
	case portmapProcNull:
		return nil, rpcAcceptSuccess
	case portmapProcGet:
		r := newXDRReader(call.body)
		prog, err := r.Uint32()
		if err != nil {
			return nil, rpcGarbageArgs
		}
		vers, err := r.Uint32()
		if err != nil {
			return nil, rpcGarbageArgs
		}
		proto, _ := r.Uint32()
		_, _ = r.Uint32() // port
		port := uint32(0)
		switch {
		case prog == progPortmap && vers == portmapVersion:
			port = PortmapPort
		case prog == progMount && vers == mountVersion3 && (proto == ipProtoTCP || proto == ipProtoUDP):
			port = MountPort
		case prog == progNFS && vers == nfsVersion3 && proto == ipProtoTCP:
			port = NFSPort
		}
		w.Uint32(port)
		return w.Bytes(), rpcAcceptSuccess
	default:
		return nil, rpcProcUnavail
	}
}

func rpcbindUniversalAddress(prog, vers uint32, netid string) string {
	var port int
	switch {
	case prog == progPortmap && (vers == portmapVersion || vers == rpcbindVersion3 || vers == rpcbindVersion4):
		port = PortmapPort
	case prog == progMount && vers == mountVersion3:
		port = MountPort
	case prog == progNFS && vers == nfsVersion3:
		if netid == "udp" || netid == "udp4" {
			return ""
		}
		port = NFSPort
	default:
		return ""
	}
	switch netid {
	case "tcp", "tcp4", "udp", "udp4":
		return fmt.Sprintf("10.42.0.1.%d.%d", port/256, port%256)
	default:
		return ""
	}
}

func (s *Server) handleMount(call rpcCall) ([]byte, uint32) {
	if call.prog != progMount || call.version != mountVersion3 {
		return nil, rpcProgUnavail
	}
	debugf("mount proc=%d request=%d", call.proc, len(call.body))
	var w xdrWriter
	switch call.proc {
	case mountProcNull:
		return nil, rpcAcceptSuccess
	case mountProcMnt:
		r := newXDRReader(call.body)
		name, err := r.String(1024)
		if err != nil {
			return nil, rpcGarbageArgs
		}
		exp := s.exportByName(name)
		if exp == nil {
			w.Uint32(nfsErrNoEnt)
			return w.Bytes(), rpcAcceptSuccess
		}
		w.Uint32(nfsOK)
		w.Opaque(fileHandle(exp.ID, 1))
		w.Uint32(1)
		w.Uint32(authUnix)
		return w.Bytes(), rpcAcceptSuccess
	case mountProcDump, mountProcUmnt, mountProcUmntAll:
		return w.Bytes(), rpcAcceptSuccess
	case mountProcExport:
		s.mu.Lock()
		exports := make([]*Export, 0, len(s.exports))
		for _, exp := range s.exports {
			exports = append(exports, exp)
		}
		s.mu.Unlock()
		sort.Slice(exports, func(i, j int) bool { return exports[i].Name < exports[j].Name })
		for _, exp := range exports {
			w.Bool(true)
			w.String(exp.Name)
			w.Bool(false)
		}
		w.Bool(false)
		return w.Bytes(), rpcAcceptSuccess
	default:
		return nil, rpcProcUnavail
	}
}

func (s *Server) handleNFS(call rpcCall) ([]byte, uint32) {
	if call.prog != progNFS || call.version != nfsVersion3 {
		return nil, rpcProgUnavail
	}
	if call.proc == nfsProcNull {
		return nil, rpcAcceptSuccess
	}
	body, err := s.handleNFSProc(call.proc, call.body)
	if err != nil {
		debugf("nfs proc=%d bad args: %v", call.proc, err)
		return nil, rpcGarbageArgs
	}
	debugf("nfs proc=%d request=%d response=%d", call.proc, len(call.body), len(body))
	return body, rpcAcceptSuccess
}

func (s *Server) handleNFSProc(proc uint32, body []byte) ([]byte, error) {
	r := newXDRReader(body)
	var w xdrWriter
	switch proc {
	case nfsProcGetAttr:
		exp, node, err := s.readHandle(r)
		if err != nil {
			w.Uint32(nfsErrBadHandle)
			return w.Bytes(), nil
		}
		attr, errno := exp.Backend.GetAttr(node)
		w.Uint32(nfsStatus(errno))
		if errno == 0 {
			writeFAttr(&w, attr)
		}
	case nfsProcSetAttr:
		exp, node, err := s.readHandle(r)
		if err != nil {
			w.Uint32(nfsErrBadHandle)
			return w.Bytes(), nil
		}
		before, _ := exp.Backend.GetAttr(node)
		valid, size, mode, uid, gid, atime, mtime, err := readSAttr(r)
		if err != nil {
			return nil, err
		}
		errno := int32(-linuxabi.EROFS)
		var attr virtio.FuseAttr
		if exp.Writable {
			if be, ok := exp.Backend.(setAttrBackend); ok {
				attr, errno = be.SetAttr(node, valid, 0, size, mode, uid, gid, atime, mtime)
			} else {
				errno = -linuxabi.ENOSYS
			}
		}
		w.Uint32(nfsStatus(errno))
		writeWcc(&w, before, attr, errno == 0)
	case nfsProcLookup:
		exp, parent, err := s.readHandle(r)
		if err != nil {
			w.Uint32(nfsErrBadHandle)
			return w.Bytes(), nil
		}
		name, err := r.String(255)
		if err != nil {
			return nil, err
		}
		parentAttr, _ := exp.Backend.GetAttr(parent)
		node, attr, errno := exp.Backend.Lookup(parent, name)
		w.Uint32(nfsStatus(errno))
		if errno == 0 {
			w.Opaque(fileHandle(exp.ID, node))
			writePostOpAttr(&w, attr)
		}
		writePostOpAttr(&w, parentAttr)
	case nfsProcAccess:
		exp, node, err := s.readHandle(r)
		if err != nil {
			w.Uint32(nfsErrBadHandle)
			return w.Bytes(), nil
		}
		requested, err := r.Uint32()
		if err != nil {
			return nil, err
		}
		attr, errno := exp.Backend.GetAttr(node)
		w.Uint32(nfsStatus(errno))
		writePostOpAttr(&w, attr)
		if errno == 0 {
			allowed := requested
			if !exp.Writable {
				allowed &^= 0x0004 | 0x0008 | 0x0010
			}
			w.Uint32(allowed)
		}
	case nfsProcReadlink:
		exp, node, err := s.readHandle(r)
		if err != nil {
			w.Uint32(nfsErrBadHandle)
			return w.Bytes(), nil
		}
		attr, _ := exp.Backend.GetAttr(node)
		target, errno := exp.Backend.Readlink(node)
		w.Uint32(nfsStatus(errno))
		writePostOpAttr(&w, attr)
		if errno == 0 {
			w.String(target)
		}
	case nfsProcRead:
		exp, node, err := s.readHandle(r)
		if err != nil {
			w.Uint32(nfsErrBadHandle)
			return w.Bytes(), nil
		}
		off, err := r.Uint64()
		if err != nil {
			return nil, err
		}
		count, err := r.Uint32()
		if err != nil {
			return nil, err
		}
		_, _ = r.Uint32()
		attr, _ := exp.Backend.GetAttr(node)
		fh, errno := exp.Backend.Open(node, 0)
		var data []byte
		if errno == 0 {
			data, errno = exp.Backend.Read(node, fh, off, count)
			exp.Backend.Release(node, fh)
		}
		w.Uint32(nfsStatus(errno))
		writePostOpAttr(&w, attr)
		if errno == 0 {
			w.Uint32(uint32(len(data)))
			w.Bool(uint64(len(data)) < uint64(count))
			w.Opaque(data)
		}
	case nfsProcWrite:
		exp, node, err := s.readHandle(r)
		if err != nil {
			w.Uint32(nfsErrBadHandle)
			return w.Bytes(), nil
		}
		off, err := r.Uint64()
		if err != nil {
			return nil, err
		}
		_, _ = r.Uint32() // count
		_, _ = r.Uint32() // stable
		data, err := r.Opaque(16 << 20)
		if err != nil {
			return nil, err
		}
		before, _ := exp.Backend.GetAttr(node)
		errno := int32(-linuxabi.EROFS)
		count := uint32(0)
		if exp.Writable {
			fh, openErr := exp.Backend.Open(node, 1)
			errno = openErr
			if errno == 0 {
				if be, ok := exp.Backend.(writeBackend); ok {
					count, errno = be.Write(node, fh, off, data, 0)
				} else {
					errno = -linuxabi.ENOSYS
				}
				exp.Backend.Release(node, fh)
			}
		}
		after, _ := exp.Backend.GetAttr(node)
		w.Uint32(nfsStatus(errno))
		writeWcc(&w, before, after, errno == 0)
		if errno == 0 {
			w.Uint32(count)
			w.Uint32(2) // FILE_SYNC
			w.FixedOpaque([]byte("ccx3nfs0"))
		}
	case nfsProcCreate:
		return s.handleCreate(r, &w)
	case nfsProcMkdir:
		return s.handleMkdir(r, &w)
	case nfsProcSymlink:
		return s.handleSymlink(r, &w)
	case nfsProcRemove:
		return s.handleRemove(r, &w, false)
	case nfsProcRmdir:
		return s.handleRemove(r, &w, true)
	case nfsProcRename:
		return s.handleRename(r, &w)
	case nfsProcReaddir, nfsProcReaddirPlus:
		return s.handleReaddir(proc, r, &w)
	case nfsProcFsstat:
		exp, node, err := s.readHandle(r)
		if err != nil {
			w.Uint32(nfsErrBadHandle)
			return w.Bytes(), nil
		}
		attr, _ := exp.Backend.GetAttr(node)
		blocks, bfree, bavail, files, ffree, bsize, _, _, errno := exp.Backend.StatFS(node)
		w.Uint32(nfsStatus(errno))
		writePostOpAttr(&w, attr)
		if errno == 0 {
			total := blocks * bsize
			free := bfree * bsize
			avail := bavail * bsize
			w.Uint64(total)
			w.Uint64(free)
			w.Uint64(avail)
			w.Uint64(files)
			w.Uint64(ffree)
			w.Uint64(ffree)
			w.Uint32(0)
		}
	case nfsProcFsinfo:
		exp, node, err := s.readHandle(r)
		if err != nil {
			w.Uint32(nfsErrBadHandle)
			return w.Bytes(), nil
		}
		attr, _ := exp.Backend.GetAttr(node)
		w.Uint32(nfsOK)
		writePostOpAttr(&w, attr)
		for i := 0; i < 7; i++ {
			w.Uint32(128 << 10)
		}
		w.Uint64(1<<63 - 1)
		w.Uint32(0)
		w.Uint32(1)
		w.Uint32(0x1f)
	case nfsProcPathconf:
		exp, node, err := s.readHandle(r)
		if err != nil {
			w.Uint32(nfsErrBadHandle)
			return w.Bytes(), nil
		}
		attr, _ := exp.Backend.GetAttr(node)
		w.Uint32(nfsOK)
		writePostOpAttr(&w, attr)
		w.Uint32(255)
		w.Uint32(255)
		w.Bool(false)
		w.Bool(false)
		w.Bool(true)
		w.Bool(true)
	case nfsProcCommit:
		exp, node, err := s.readHandle(r)
		if err != nil {
			w.Uint32(nfsErrBadHandle)
			return w.Bytes(), nil
		}
		attr, _ := exp.Backend.GetAttr(node)
		w.Uint32(nfsOK)
		writeWcc(&w, attr, attr, true)
		w.FixedOpaque([]byte("ccx3nfs0"))
	default:
		w.Uint32(nfsErrNotSupp)
	}
	return w.Bytes(), nil
}

func (s *Server) readHandle(r *xdrReader) (*Export, uint64, error) {
	expID, nodeID, err := parseFileHandle(r)
	if err != nil {
		return nil, 0, err
	}
	exp := s.exportByID(expID)
	if exp == nil {
		return nil, 0, fmt.Errorf("stale export %d", expID)
	}
	return exp, nodeID, nil
}

type writeBackend interface {
	Write(nodeID uint64, fh uint64, off uint64, data []byte, flags uint32) (uint32, int32)
}

type createBackend interface {
	Create(parent uint64, name string, flags uint32, mode uint32, uid uint32, gid uint32) (uint64, uint64, virtio.FuseAttr, int32)
}

type mkdirBackend interface {
	Mkdir(parent uint64, name string, mode uint32, uid uint32, gid uint32) (uint64, virtio.FuseAttr, int32)
}

type symlinkBackend interface {
	Symlink(parent uint64, name string, target string, uid uint32, gid uint32) (uint64, virtio.FuseAttr, int32)
}

type unlinkBackend interface {
	Unlink(parent uint64, name string) int32
}

type rmdirBackend interface {
	RmDir(parent uint64, name string) int32
}

type renameBackend interface {
	Rename(parent uint64, name string, newParent uint64, newName string, flags uint32) int32
}

type setAttrBackend interface {
	SetAttr(nodeID uint64, valid uint32, fh uint64, size uint64, mode uint32, uid uint32, gid uint32, atime time.Time, mtime time.Time) (virtio.FuseAttr, int32)
}

func (s *Server) handleCreate(r *xdrReader, w *xdrWriter) ([]byte, error) {
	exp, parent, err := s.readHandle(r)
	if err != nil {
		w.Uint32(nfsErrBadHandle)
		return w.Bytes(), nil
	}
	name, err := r.String(255)
	if err != nil {
		return nil, err
	}
	modeKind, _ := r.Uint32()
	valid, _, mode, uid, gid, _, _, err := readSAttr(r)
	if err != nil {
		return nil, err
	}
	_ = valid
	if modeKind == 2 {
		_, _ = r.Uint64()
	}
	before, _ := exp.Backend.GetAttr(parent)
	errno := int32(-linuxabi.EROFS)
	var node uint64
	var attr virtio.FuseAttr
	if exp.Writable {
		if be, ok := exp.Backend.(createBackend); ok {
			node, _, attr, errno = be.Create(parent, path.Base(name), 0x42, mode, uid, gid)
		} else {
			errno = -linuxabi.ENOSYS
		}
	}
	w.Uint32(nfsStatus(errno))
	if errno == 0 {
		w.Bool(true)
		w.Opaque(fileHandle(exp.ID, node))
		writePostOpAttr(w, attr)
	}
	after, _ := exp.Backend.GetAttr(parent)
	writeWcc(w, before, after, errno == 0)
	return w.Bytes(), nil
}

func (s *Server) handleMkdir(r *xdrReader, w *xdrWriter) ([]byte, error) {
	exp, parent, err := s.readHandle(r)
	if err != nil {
		w.Uint32(nfsErrBadHandle)
		return w.Bytes(), nil
	}
	name, err := r.String(255)
	if err != nil {
		return nil, err
	}
	_, _, mode, uid, gid, _, _, err := readSAttr(r)
	if err != nil {
		return nil, err
	}
	before, _ := exp.Backend.GetAttr(parent)
	errno := int32(-linuxabi.EROFS)
	var node uint64
	var attr virtio.FuseAttr
	if exp.Writable {
		if be, ok := exp.Backend.(mkdirBackend); ok {
			node, attr, errno = be.Mkdir(parent, path.Base(name), mode, uid, gid)
		} else {
			errno = -linuxabi.ENOSYS
		}
	}
	w.Uint32(nfsStatus(errno))
	if errno == 0 {
		w.Bool(true)
		w.Opaque(fileHandle(exp.ID, node))
		writePostOpAttr(w, attr)
	}
	after, _ := exp.Backend.GetAttr(parent)
	writeWcc(w, before, after, errno == 0)
	return w.Bytes(), nil
}

func (s *Server) handleSymlink(r *xdrReader, w *xdrWriter) ([]byte, error) {
	exp, parent, err := s.readHandle(r)
	if err != nil {
		w.Uint32(nfsErrBadHandle)
		return w.Bytes(), nil
	}
	name, err := r.String(255)
	if err != nil {
		return nil, err
	}
	_, _, mode, uid, gid, _, _, err := readSAttr(r)
	if err != nil {
		return nil, err
	}
	target, err := r.String(4096)
	if err != nil {
		return nil, err
	}
	before, _ := exp.Backend.GetAttr(parent)
	errno := int32(-linuxabi.EROFS)
	var node uint64
	var attr virtio.FuseAttr
	if exp.Writable {
		if be, ok := exp.Backend.(symlinkBackend); ok {
			node, attr, errno = be.Symlink(parent, path.Base(name), target, uid, gid)
			_ = mode
		} else {
			errno = -linuxabi.ENOSYS
		}
	}
	w.Uint32(nfsStatus(errno))
	if errno == 0 {
		w.Bool(true)
		w.Opaque(fileHandle(exp.ID, node))
		writePostOpAttr(w, attr)
	}
	after, _ := exp.Backend.GetAttr(parent)
	writeWcc(w, before, after, errno == 0)
	return w.Bytes(), nil
}

func (s *Server) handleRemove(r *xdrReader, w *xdrWriter, dir bool) ([]byte, error) {
	exp, parent, err := s.readHandle(r)
	if err != nil {
		w.Uint32(nfsErrBadHandle)
		return w.Bytes(), nil
	}
	name, err := r.String(255)
	if err != nil {
		return nil, err
	}
	before, _ := exp.Backend.GetAttr(parent)
	errno := int32(-linuxabi.EROFS)
	if exp.Writable {
		if dir {
			if be, ok := exp.Backend.(rmdirBackend); ok {
				errno = be.RmDir(parent, path.Base(name))
			} else {
				errno = -linuxabi.ENOSYS
			}
		} else if be, ok := exp.Backend.(unlinkBackend); ok {
			errno = be.Unlink(parent, path.Base(name))
		} else {
			errno = -linuxabi.ENOSYS
		}
	}
	after, _ := exp.Backend.GetAttr(parent)
	w.Uint32(nfsStatus(errno))
	writeWcc(w, before, after, errno == 0)
	return w.Bytes(), nil
}

func (s *Server) handleRename(r *xdrReader, w *xdrWriter) ([]byte, error) {
	exp, parent, err := s.readHandle(r)
	if err != nil {
		w.Uint32(nfsErrBadHandle)
		return w.Bytes(), nil
	}
	name, err := r.String(255)
	if err != nil {
		return nil, err
	}
	exp2, newParent, err := s.readHandle(r)
	if err != nil || exp2 != exp {
		w.Uint32(nfsErrXDev)
		return w.Bytes(), nil
	}
	newName, err := r.String(255)
	if err != nil {
		return nil, err
	}
	fromBefore, _ := exp.Backend.GetAttr(parent)
	toBefore, _ := exp.Backend.GetAttr(newParent)
	errno := int32(-linuxabi.EROFS)
	if exp.Writable {
		if be, ok := exp.Backend.(renameBackend); ok {
			errno = be.Rename(parent, path.Base(name), newParent, path.Base(newName), 0)
		} else {
			errno = -linuxabi.ENOSYS
		}
	}
	fromAfter, _ := exp.Backend.GetAttr(parent)
	toAfter, _ := exp.Backend.GetAttr(newParent)
	w.Uint32(nfsStatus(errno))
	writeWcc(w, fromBefore, fromAfter, errno == 0)
	writeWcc(w, toBefore, toAfter, errno == 0)
	return w.Bytes(), nil
}

func (s *Server) handleReaddir(proc uint32, r *xdrReader, w *xdrWriter) ([]byte, error) {
	exp, node, err := s.readHandle(r)
	if err != nil {
		w.Uint32(nfsErrBadHandle)
		return w.Bytes(), nil
	}
	cookie, err := r.Uint64()
	if err != nil {
		return nil, err
	}
	_, err = r.Opaque(8)
	if err != nil {
		return nil, err
	}
	count, err := r.Uint32()
	if err != nil {
		return nil, err
	}
	if proc == nfsProcReaddirPlus {
		if _, err := r.Uint32(); err != nil {
			return nil, err
		}
	}
	attr, _ := exp.Backend.GetAttr(node)
	fh, errno := exp.Backend.OpenDir(node, 0)
	var entries []fuseDirent
	if errno == 0 {
		data, dirErr := exp.Backend.ReadDir(node, fh, 0, max(count, 8192))
		exp.Backend.ReleaseDir(node, fh)
		errno = dirErr
		entries = parseFuseDirents(data)
	}
	w.Uint32(nfsStatus(errno))
	writePostOpAttr(w, attr)
	if errno != 0 {
		return w.Bytes(), nil
	}
	w.FixedOpaque([]byte("ccx3nfs0"))
	start := int(cookie)
	if start < 0 {
		start = 0
	}
	for i := start; i < len(entries); i++ {
		ent := entries[i]
		w.Bool(true)
		w.Uint64(ent.ino)
		w.String(ent.name)
		w.Uint64(uint64(i + 1))
		if proc == nfsProcReaddirPlus {
			childID, childAttr, childErr := lookupDirent(exp.Backend, node, ent)
			if childErr == 0 {
				writePostOpAttr(w, childAttr)
				w.Bool(true)
				w.Opaque(fileHandle(exp.ID, childID))
			} else {
				w.Bool(false)
				w.Bool(false)
			}
		}
	}
	w.Bool(false)
	w.Bool(true)
	return w.Bytes(), nil
}

func lookupDirent(backend virtio.FSBackend, parent uint64, ent fuseDirent) (uint64, virtio.FuseAttr, int32) {
	switch ent.name {
	case ".":
		attr, errno := backend.GetAttr(parent)
		return parent, attr, errno
	case "..":
		attr, errno := backend.GetAttr(parent)
		return parent, attr, errno
	default:
		return backend.Lookup(parent, ent.name)
	}
}

type fuseDirent struct {
	ino  uint64
	off  uint64
	typ  uint32
	name string
}

func parseFuseDirents(data []byte) []fuseDirent {
	var out []fuseDirent
	for off := 0; off+fuseDirentBaseSize <= len(data); {
		ino := binary.LittleEndian.Uint64(data[off:])
		next := binary.LittleEndian.Uint64(data[off+8:])
		nameLen := int(binary.LittleEndian.Uint32(data[off+16:]))
		typ := binary.LittleEndian.Uint32(data[off+20:])
		recLen := align8(fuseDirentBaseSize + nameLen)
		if nameLen < 0 || off+recLen > len(data) || off+fuseDirentBaseSize+nameLen > len(data) {
			break
		}
		name := string(data[off+fuseDirentBaseSize : off+fuseDirentBaseSize+nameLen])
		out = append(out, fuseDirent{ino: ino, off: next, typ: typ, name: name})
		off += recLen
	}
	return out
}

func align8(n int) int {
	return (n + 7) &^ 7
}

func writePostOpAttr(w *xdrWriter, attr virtio.FuseAttr) {
	w.Bool(true)
	writeFAttr(w, attr)
}

func writeWcc(w *xdrWriter, before virtio.FuseAttr, after virtio.FuseAttr, haveAfter bool) {
	w.Bool(true)
	writeWccAttr(w, before)
	if haveAfter {
		writePostOpAttr(w, after)
	} else {
		w.Bool(false)
	}
}

func writeWccAttr(w *xdrWriter, attr virtio.FuseAttr) {
	w.Uint64(attr.Size)
	w.Uint32(uint32(attr.MTimeSec))
	w.Uint32(attr.MTimeNsec)
	w.Uint32(uint32(attr.CTimeSec))
	w.Uint32(attr.CTimeNsec)
}

func writeFAttr(w *xdrWriter, attr virtio.FuseAttr) {
	w.Uint32(nfsFileType(attr.Mode))
	w.Uint32(attr.Mode & 0o7777)
	w.Uint32(attr.NLink)
	w.Uint32(attr.UID)
	w.Uint32(attr.GID)
	w.Uint64(attr.Size)
	w.Uint64(attr.Size)
	w.Uint32(0)
	w.Uint32(attr.RDev)
	w.Uint64(attr.Blocks)
	w.Uint64(attr.Ino)
	w.Uint32(uint32(attr.ATimeSec))
	w.Uint32(attr.ATimeNsec)
	w.Uint32(uint32(attr.MTimeSec))
	w.Uint32(attr.MTimeNsec)
	w.Uint32(uint32(attr.CTimeSec))
	w.Uint32(attr.CTimeNsec)
}

func nfsFileType(mode uint32) uint32 {
	switch mode & 0o170000 {
	case 0o040000:
		return nfsFileDir
	case 0o120000:
		return nfsFileLnk
	case 0o060000:
		return nfsFileBlk
	case 0o020000:
		return nfsFileChr
	case 0o010000:
		return nfsFileFIFO
	case 0o140000:
		return nfsFileSock
	default:
		return nfsFileReg
	}
}

func readSAttr(r *xdrReader) (valid uint32, size uint64, mode, uid, gid uint32, atime, mtime time.Time, err error) {
	set, err := r.Uint32()
	if err != nil {
		return 0, 0, 0, 0, 0, time.Time{}, time.Time{}, err
	}
	if set != 0 {
		mode, err = r.Uint32()
		if err != nil {
			return
		}
		valid |= 1
	}
	set, err = r.Uint32()
	if err != nil {
		return 0, 0, 0, 0, 0, time.Time{}, time.Time{}, err
	}
	if set != 0 {
		uid, err = r.Uint32()
		if err != nil {
			return
		}
		valid |= 2
	}
	set, err = r.Uint32()
	if err != nil {
		return 0, 0, 0, 0, 0, time.Time{}, time.Time{}, err
	}
	if set != 0 {
		gid, err = r.Uint32()
		if err != nil {
			return
		}
		valid |= 4
	}
	set, err = r.Uint32()
	if err != nil {
		return 0, 0, 0, 0, 0, time.Time{}, time.Time{}, err
	}
	if set != 0 {
		size, err = r.Uint64()
		if err != nil {
			return
		}
		valid |= 8
	}
	set, err = r.Uint32()
	if err != nil {
		return 0, 0, 0, 0, 0, time.Time{}, time.Time{}, err
	}
	if set == 2 {
		atime, err = readNFSTime(r)
		if err != nil {
			return
		}
		valid |= 16
	}
	set, err = r.Uint32()
	if err != nil {
		return 0, 0, 0, 0, 0, time.Time{}, time.Time{}, err
	}
	if set == 2 {
		mtime, err = readNFSTime(r)
		if err != nil {
			return
		}
		valid |= 32
	}
	return
}

func readNFSTime(r *xdrReader) (time.Time, error) {
	sec, err := r.Uint32()
	if err != nil {
		return time.Time{}, err
	}
	nsec, err := r.Uint32()
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(int64(sec), int64(nsec)), nil
}

func nfsStatus(errno int32) uint32 {
	if errno == 0 {
		return nfsOK
	}
	switch -errno {
	case linuxabi.EPERM:
		return nfsErrPerm
	case linuxabi.ENOENT:
		return nfsErrNoEnt
	case linuxabi.EIO:
		return nfsErrIO
	case linuxabi.ENXIO:
		return nfsErrNXIO
	case linuxabi.EACCES:
		return nfsErrAcces
	case linuxabi.EEXIST:
		return nfsErrExist
	case linuxabi.EXDEV:
		return nfsErrXDev
	case linuxabi.ENOTDIR:
		return nfsErrNotDir
	case linuxabi.EISDIR:
		return nfsErrIsDir
	case linuxabi.EINVAL:
		return nfsErrInval
	case linuxabi.EFBIG:
		return nfsErrFBig
	case linuxabi.EROFS:
		return nfsErrROFS
	case linuxabi.ENOTEMPTY:
		return nfsErrNotEmpty
	case linuxabi.ENOSYS:
		return nfsErrNotSupp
	default:
		return nfsErrIO
	}
}

func max(a, b uint32) uint32 {
	if a > b {
		return a
	}
	return b
}

var _ io.Closer = (*Server)(nil)

func MountCommand(osName, serverAddr, exportName, mountPath string) []string {
	server := strings.TrimSpace(serverAddr)
	if server == "" {
		server = "10.42.0.1"
	}
	target := server + ":" + exportName
	switch strings.ToLower(strings.TrimSpace(osName)) {
	case "openbsd":
		return []string{"/sbin/mount", "-t", "nfs", "-o", fmt.Sprintf("tcp,port=%d", NFSPort), target, mountPath}
	case "freebsd":
		return []string{"/sbin/mount", "-t", "nfs", "-o", fmt.Sprintf("nfsv3,proto=tcp,soft,retrycnt=1,port=%d,mountport=%d,nolockd", NFSPort, MountPort), target, mountPath}
	case "netbsd":
		return []string{"/sbin/mount_nfs", "-3", "-T", "-p", "-R", "1", "-t", "1", target, mountPath}
	default:
		return []string{"/sbin/mount", "-t", "nfs", "-o", fmt.Sprintf("vers=3,tcp,port=%d,mountport=%d,nolock", NFSPort, MountPort), target, mountPath}
	}
}

func MountShare(ctx context.Context, osName string, exec func(context.Context, client.ExecRequest) (client.ExecResponse, error), exp *Export) error {
	if exp == nil {
		return fmt.Errorf("nfs export is not configured")
	}
	mountCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	mkdir := client.ExecRequest{Command: []string{"/bin/mkdir", "-p", exp.Mount}, SkipResolve: true}
	debugf("mount share mkdir %s", exp.Mount)
	if resp, err := exec(mountCtx, mkdir); err != nil {
		return err
	} else if resp.ExitCode != 0 {
		return fmt.Errorf("mkdir %s failed: %s", exp.Mount, resp.Output)
	}
	req := client.ExecRequest{Command: MountCommand(osName, "10.42.0.1", exp.Name, exp.Mount), SkipResolve: true}
	debugf("mount share command %q", strings.Join(req.Command, " "))
	resp, err := exec(mountCtx, req)
	if err != nil {
		return err
	}
	if resp.ExitCode != 0 {
		return fmt.Errorf("mount nfs %s on %s failed: %s", exp.Name, exp.Mount, resp.Output)
	}
	return nil
}

func debugf(format string, args ...any) {
	target := strings.TrimSpace(os.Getenv("CCX3_DEBUG_NFS"))
	if target == "" {
		return
	}
	line := fmt.Sprintf("nfs: "+format+"\n", args...)
	_, _ = os.Stderr.WriteString(line)
	if strings.Contains(target, "/") {
		if f, err := os.OpenFile(target, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
			_, _ = f.WriteString(line)
			_ = f.Close()
		}
	}
}
