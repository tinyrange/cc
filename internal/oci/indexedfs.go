package oci

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
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

	intcvmfs "j5.nz/cc/internal/cvmfs"
	"j5.nz/cc/internal/fsmeta"
	"j5.nz/cc/internal/imagefs"
	"j5.nz/cc/internal/linuxabi"
)

var debugRootFSReads = strings.TrimSpace(os.Getenv("CCX3_DEBUG_ROOTFS_READS")) != ""

type indexedKind string

const (
	indexedKindDir     indexedKind = "dir"
	indexedKindFile    indexedKind = "file"
	indexedKindSymlink indexedKind = "symlink"
)

type indexedNode struct {
	Path         string      `json:"path"`
	Kind         indexedKind `json:"kind"`
	Mode         uint32      `json:"mode"`
	UID          uint32      `json:"uid,omitempty"`
	GID          uint32      `json:"gid,omitempty"`
	RDev         uint32      `json:"rdev,omitempty"`
	Size         uint64      `json:"size,omitempty"`
	ModTimeNS    int64       `json:"mod_time_ns,omitempty"`
	LinkTarget   string      `json:"link_target,omitempty"`
	TarPath      string      `json:"tar_path,omitempty"`
	TarOffset    uint64      `json:"tar_offset,omitempty"`
	CVMFSTarget  string      `json:"cvmfs_target,omitempty"`
	Packed       bool        `json:"packed,omitempty"`
	PackedOffset uint64      `json:"packed_offset,omitempty"`
}

func encodeFSIndex(nodes map[string]*indexedNode) ([]byte, error) {
	out := make([]indexedNode, 0, len(nodes))
	for _, node := range nodes {
		out = append(out, *node)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return json.MarshalIndent(out, "", "  ")
}

func encodeIndexedNodes(nodes []indexedNode) ([]byte, error) {
	out := append([]indexedNode(nil), nodes...)
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return json.MarshalIndent(out, "", "  ")
}

func decodeFSIndex(data []byte) ([]indexedNode, error) {
	var out []indexedNode
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func buildIndexedRootFS(baseDir string, nodes []indexedNode) (imagefs.Directory, error) {
	root := newIndexedDir("/", fs.ModeDir|0o755, 0, 0, 0, time.Unix(0, 0))
	byPath := map[string]*indexedDir{"/": root}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Path < nodes[j].Path })
	for _, node := range nodes {
		if node.Path == "/" {
			root.mode = imagefsMode(node.Mode, fs.ModeDir|0o755)
			root.uid = node.UID
			root.gid = node.GID
			root.rdev = node.RDev
			root.modTime = time.Unix(0, node.ModTimeNS)
			continue
		}
		parentPath := path.Dir(node.Path)
		if parentPath == "." {
			parentPath = "/"
		}
		parent, ok := byPath[parentPath]
		if !ok {
			return nil, fmt.Errorf("missing parent %q for %q", parentPath, node.Path)
		}
		name := path.Base(node.Path)
		modTime := time.Unix(0, node.ModTimeNS)
		switch node.Kind {
		case indexedKindDir:
			dir := newIndexedDir(node.Path, imagefsMode(node.Mode, fs.ModeDir|0o755), node.UID, node.GID, node.RDev, modTime)
			parent.entries[name] = imagefs.Entry{Dir: dir}
			byPath[node.Path] = dir
		case indexedKindFile:
			file, err := buildIndexedFile(baseDir, "", node, modTime, nil)
			if err != nil {
				return nil, err
			}
			parent.entries[name] = imagefs.Entry{File: file}
		case indexedKindSymlink:
			parent.entries[name] = imagefs.Entry{Symlink: &indexedSymlink{
				mode:    imagefsMode(node.Mode, fs.ModeSymlink|0o777),
				uid:     node.UID,
				gid:     node.GID,
				rdev:    node.RDev,
				target:  node.LinkTarget,
				modTime: modTime,
			}}
		default:
			return nil, fmt.Errorf("unknown node kind %q", node.Kind)
		}
	}
	return root, nil
}

func buildCVMFSIndexedRootFS(client *intcvmfs.Client, packedPath string, nodes []indexedNode) (imagefs.Directory, error) {
	root := newIndexedDir("/", fs.ModeDir|0o755, 0, 0, 0, time.Unix(0, 0))
	byPath := map[string]*indexedDir{"/": root}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Path < nodes[j].Path })
	for _, node := range nodes {
		if node.Path == "/" {
			root.mode = imagefsMode(node.Mode, fs.ModeDir|0o755)
			root.uid = node.UID
			root.gid = node.GID
			root.rdev = node.RDev
			root.modTime = time.Unix(0, node.ModTimeNS)
			continue
		}
		parentPath := path.Dir(node.Path)
		if parentPath == "." {
			parentPath = "/"
		}
		parent, ok := byPath[parentPath]
		if !ok {
			return nil, fmt.Errorf("missing parent %q for %q", parentPath, node.Path)
		}
		name := path.Base(node.Path)
		modTime := time.Unix(0, node.ModTimeNS)
		switch node.Kind {
		case indexedKindDir:
			dir := newIndexedDir(node.Path, imagefsMode(node.Mode, fs.ModeDir|0o755), node.UID, node.GID, node.RDev, modTime)
			parent.entries[name] = imagefs.Entry{Dir: dir}
			byPath[node.Path] = dir
		case indexedKindFile:
			file, err := buildIndexedFile("", packedPath, node, modTime, client)
			if err != nil {
				return nil, err
			}
			parent.entries[name] = imagefs.Entry{File: file}
		case indexedKindSymlink:
			parent.entries[name] = imagefs.Entry{Symlink: &indexedSymlink{
				mode:    imagefsMode(node.Mode, fs.ModeSymlink|0o777),
				uid:     node.UID,
				gid:     node.GID,
				rdev:    node.RDev,
				target:  node.LinkTarget,
				modTime: modTime,
			}}
		default:
			return nil, fmt.Errorf("unknown node kind %q", node.Kind)
		}
	}
	return root, nil
}

func buildIndexedFile(baseDir string, packedPath string, node indexedNode, modTime time.Time, cvmfsClient *intcvmfs.Client) (imagefs.File, error) {
	if node.Packed {
		if packedPath == "" {
			return nil, fmt.Errorf("packed path is required for %q", node.Path)
		}
		return &indexedPackedFile{
			mode:         imagefsMode(node.Mode, 0o644),
			uid:          node.UID,
			gid:          node.GID,
			rdev:         node.RDev,
			size:         node.Size,
			modTime:      modTime,
			contentsPath: packedPath,
			offset:       node.PackedOffset,
		}, nil
	}
	if node.CVMFSTarget != "" {
		if cvmfsClient == nil {
			return nil, fmt.Errorf("cvmfs client is required for %q", node.Path)
		}
		return &indexedCVMFSFile{
			mode:    imagefsMode(node.Mode, 0o644),
			uid:     node.UID,
			gid:     node.GID,
			rdev:    node.RDev,
			size:    node.Size,
			modTime: modTime,
			target:  node.CVMFSTarget,
			client:  cvmfsClient,
		}, nil
	}
	return &indexedFile{
		mode:      imagefsMode(node.Mode, 0o644),
		uid:       node.UID,
		gid:       node.GID,
		rdev:      node.RDev,
		size:      node.Size,
		modTime:   modTime,
		tarPath:   filepath.Join(baseDir, node.TarPath),
		tarOffset: node.TarOffset,
	}, nil
}

type indexedDir struct {
	path    string
	mode    fs.FileMode
	uid     uint32
	gid     uint32
	rdev    uint32
	modTime time.Time
	entries map[string]imagefs.Entry
}

func newIndexedDir(path string, mode fs.FileMode, uid, gid, rdev uint32, modTime time.Time) *indexedDir {
	if modTime.IsZero() {
		modTime = time.Unix(0, 0)
	}
	return &indexedDir{
		path:    path,
		mode:    mode,
		uid:     uid,
		gid:     gid,
		rdev:    rdev,
		modTime: modTime,
		entries: map[string]imagefs.Entry{},
	}
}

func (d *indexedDir) Stat() fs.FileMode       { return d.mode & fs.ModePerm }
func (d *indexedDir) ModTime() time.Time      { return d.modTime }
func (d *indexedDir) Owner() (uint32, uint32) { return d.uid, d.gid }
func (d *indexedDir) RDev() uint32            { return d.rdev }
func (d *indexedDir) ReadDir() ([]imagefs.DirEnt, error) {
	out := make([]imagefs.DirEnt, 0, len(d.entries))
	for name, entry := range d.entries {
		switch {
		case entry.Dir != nil:
			out = append(out, imagefs.DirEnt{Name: name, Mode: fs.ModeDir | entry.Dir.Stat()})
		case entry.Symlink != nil:
			out = append(out, imagefs.DirEnt{Name: name, Mode: fs.ModeSymlink | entry.Symlink.Stat()})
		case entry.File != nil:
			_, mode := entry.File.Stat()
			out = append(out, imagefs.DirEnt{Name: name, Mode: mode})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (d *indexedDir) Lookup(name string) (imagefs.Entry, error) {
	entry, ok := d.entries[name]
	if !ok {
		return imagefs.Entry{}, os.ErrNotExist
	}
	return entry, nil
}

type indexedFile struct {
	mode      fs.FileMode
	uid       uint32
	gid       uint32
	rdev      uint32
	size      uint64
	modTime   time.Time
	tarPath   string
	tarOffset uint64
}

type indexedPackedFile struct {
	mode         fs.FileMode
	uid          uint32
	gid          uint32
	rdev         uint32
	size         uint64
	modTime      time.Time
	contentsPath string
	offset       uint64
}

type indexedCVMFSFile struct {
	mode    fs.FileMode
	uid     uint32
	gid     uint32
	rdev    uint32
	size    uint64
	modTime time.Time
	target  string
	client  *intcvmfs.Client

	cacheMu   sync.Mutex
	cacheData []byte
	cacheErr  error
	cacheInit bool
}

func (f *indexedFile) Stat() (uint64, fs.FileMode) { return f.size, f.mode }
func (f *indexedFile) ModTime() time.Time          { return f.modTime }
func (f *indexedFile) Owner() (uint32, uint32)     { return f.uid, f.gid }
func (f *indexedFile) RDev() uint32                { return f.rdev }
func (f *indexedFile) OpenReader() (io.ReaderAt, io.Closer, error) {
	file, err := os.Open(f.tarPath)
	if err != nil {
		return nil, nil, err
	}
	return &offsetReaderAt{reader: file, base: f.tarOffset}, file, nil
}
func (f *indexedFile) ReadAt(off uint64, size uint32) ([]byte, error) {
	if off >= f.size || size == 0 {
		return nil, nil
	}
	remaining := f.size - off
	if remaining > uint64(size) {
		remaining = uint64(size)
	}
	file, err := os.Open(f.tarPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	buf := make([]byte, remaining)
	n, err := file.ReadAt(buf, int64(f.tarOffset+off))
	if err != nil && err != io.EOF {
		return nil, err
	}
	return buf[:n], nil
}

func (f *indexedPackedFile) Stat() (uint64, fs.FileMode) { return f.size, f.mode }
func (f *indexedPackedFile) ModTime() time.Time          { return f.modTime }
func (f *indexedPackedFile) Owner() (uint32, uint32)     { return f.uid, f.gid }
func (f *indexedPackedFile) RDev() uint32                { return f.rdev }
func (f *indexedPackedFile) OpenReader() (io.ReaderAt, io.Closer, error) {
	file, err := os.Open(f.contentsPath)
	if err != nil {
		return nil, nil, err
	}
	return &offsetReaderAt{reader: file, base: f.offset}, file, nil
}
func (f *indexedPackedFile) ReadAt(off uint64, size uint32) ([]byte, error) {
	if off >= f.size || size == 0 {
		return nil, nil
	}
	remaining := f.size - off
	if remaining > uint64(size) {
		remaining = uint64(size)
	}
	file, err := os.Open(f.contentsPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	buf := make([]byte, remaining)
	n, err := file.ReadAt(buf, int64(f.offset+off))
	if err != nil && err != io.EOF {
		return nil, err
	}
	return buf[:n], nil
}

type offsetReaderAt struct {
	reader io.ReaderAt
	base   uint64
}

func (r *offsetReaderAt) ReadAt(p []byte, off int64) (int, error) {
	return r.reader.ReadAt(p, int64(r.base)+off)
}

func (f *indexedCVMFSFile) Stat() (uint64, fs.FileMode) { return f.size, f.mode }
func (f *indexedCVMFSFile) ModTime() time.Time          { return f.modTime }
func (f *indexedCVMFSFile) Owner() (uint32, uint32)     { return f.uid, f.gid }
func (f *indexedCVMFSFile) RDev() uint32                { return f.rdev }
func (f *indexedCVMFSFile) ReadAt(off uint64, size uint32) ([]byte, error) {
	start := time.Now()
	data, err := f.readAll()
	if err != nil {
		if debugRootFSReads {
			fmt.Fprintf(os.Stderr, "ccx3 rootfs: target=%q off=%d size=%d err=%v took=%s\n", f.target, off, size, err, time.Since(start))
		}
		return nil, err
	}
	if off >= uint64(len(data)) || size == 0 {
		return nil, nil
	}
	end := uint64(len(data))
	if remaining := off + uint64(size); remaining < end {
		end = remaining
	}
	chunk := append([]byte(nil), data[off:end]...)
	if debugRootFSReads {
		fmt.Fprintf(os.Stderr, "ccx3 rootfs: target=%q off=%d size=%d got=%d cached=%t took=%s\n", f.target, off, size, len(chunk), true, time.Since(start))
	}
	return chunk, nil
}

func (f *indexedCVMFSFile) readAll() ([]byte, error) {
	f.cacheMu.Lock()
	if f.cacheInit {
		data := f.cacheData
		err := f.cacheErr
		f.cacheMu.Unlock()
		return data, err
	}
	f.cacheMu.Unlock()

	start := time.Now()
	data, err := f.client.ReadFile(f.target)

	f.cacheMu.Lock()
	defer f.cacheMu.Unlock()
	if !f.cacheInit {
		if err == nil {
			f.cacheData = data
		}
		f.cacheErr = err
		f.cacheInit = true
		if debugRootFSReads {
			if err != nil {
				fmt.Fprintf(os.Stderr, "ccx3 rootfs: cache-fill target=%q err=%v took=%s\n", f.target, err, time.Since(start))
			} else {
				fmt.Fprintf(os.Stderr, "ccx3 rootfs: cache-fill target=%q bytes=%d took=%s\n", f.target, len(data), time.Since(start))
			}
		}
	}
	return f.cacheData, f.cacheErr
}

type indexedSymlink struct {
	mode    fs.FileMode
	uid     uint32
	gid     uint32
	rdev    uint32
	target  string
	modTime time.Time
}

func (l *indexedSymlink) Stat() fs.FileMode       { return l.mode & fs.ModePerm }
func (l *indexedSymlink) ModTime() time.Time      { return l.modTime }
func (l *indexedSymlink) Target() string          { return l.target }
func (l *indexedSymlink) Owner() (uint32, uint32) { return l.uid, l.gid }
func (l *indexedSymlink) RDev() uint32            { return l.rdev }

func writeLayerTar(dstPath, mediaType string, blob []byte) error {
	if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
		return err
	}
	dst, err := os.OpenFile(dstPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer dst.Close()
	var src io.Reader = bytes.NewReader(blob)
	if isGzipMediaType(mediaType, blob) {
		gzr, err := gzip.NewReader(src)
		if err != nil {
			return fmt.Errorf("open gzip layer: %w", err)
		}
		defer gzr.Close()
		src = gzr
	}
	if _, err := io.Copy(dst, src); err != nil {
		return err
	}
	return dst.Close()
}

type countingReader struct {
	r io.Reader
	n uint64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += uint64(n)
	return n, err
}

func applyIndexedLayer(tarPath string, tarRef string, merged map[string]*indexedNode, entries map[string]fsmeta.Entry) error {
	file, err := os.Open(tarPath)
	if err != nil {
		return err
	}
	defer file.Close()
	cr := &countingReader{r: file}
	tr := tar.NewReader(cr)
	layerEntries := map[string]*indexedNode{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read layer tar: %w", err)
		}
		name, err := sanitizeArchivePath(hdr.Name)
		if err != nil {
			return err
		}
		base := path.Base(name)
		dir := path.Dir(name)
		dataOffset := cr.n
		if base == ".wh..wh..opq" {
			opaquePrefix := fsmeta.Normalize(dir)
			for key := range merged {
				if key != opaquePrefix && strings.HasPrefix(key, opaquePrefix+"/") {
					delete(merged, key)
					delete(entries, key)
				}
			}
			continue
		}
		if strings.HasPrefix(base, ".wh.") {
			deletedName := path.Join(dir, strings.TrimPrefix(base, ".wh."))
			removeMergedPath(merged, entries, fsmeta.Normalize(deletedName))
			continue
		}

		mode := fsmeta.LinuxModeFromTarHeader(hdr)
		node := &indexedNode{
			Path:      fsmeta.Normalize(name),
			UID:       uint32(hdr.Uid),
			GID:       uint32(hdr.Gid),
			Mode:      mode,
			Size:      uint64(hdr.Size),
			ModTimeNS: hdr.ModTime.UnixNano(),
			TarPath:   tarRef,
			TarOffset: dataOffset,
			RDev:      uint32(hdr.Devmajor<<8 | hdr.Devminor),
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			node.Kind = indexedKindDir
			node.Size = 0
			node.TarPath = ""
			node.TarOffset = 0
		case tar.TypeReg, tar.TypeRegA:
			node.Kind = indexedKindFile
		case tar.TypeSymlink:
			node.Kind = indexedKindSymlink
			node.LinkTarget = hdr.Linkname
			node.Size = uint64(len(hdr.Linkname))
			node.TarPath = ""
			node.TarOffset = 0
		case tar.TypeLink:
			target, err := sanitizeArchivePath(hdr.Linkname)
			if err != nil {
				return err
			}
			target = fsmeta.Normalize(target)
			targetNode := layerEntries[target]
			if targetNode == nil {
				targetNode = merged[target]
			}
			if targetNode == nil || targetNode.Kind != indexedKindFile {
				return fmt.Errorf("hardlink target %q for %q not found", hdr.Linkname, hdr.Name)
			}
			node.Kind = indexedKindFile
			node.Size = targetNode.Size
			node.TarPath = targetNode.TarPath
			node.TarOffset = targetNode.TarOffset
		case tar.TypeXGlobalHeader:
			continue
		default:
			return fmt.Errorf("unsupported layer entry type %d for %s", hdr.Typeflag, name)
		}
		merged[node.Path] = node
		layerEntries[node.Path] = node
		entries[node.Path] = fsmeta.Entry{
			UID:  node.UID,
			GID:  node.GID,
			Mode: node.Mode,
			RDev: node.RDev,
		}
	}
}

func ensureIndexedParents(merged map[string]*indexedNode, entries map[string]fsmeta.Entry) {
	paths := make([]string, 0, len(merged))
	for path := range merged {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, current := range paths {
		parent := path.Dir(current)
		for parent != "." && parent != "/" {
			parent = fsmeta.Normalize(parent)
			if _, ok := merged[parent]; ok {
				break
			}
			merged[parent] = &indexedNode{
				Path: parent,
				Kind: indexedKindDir,
				Mode: fsmeta.LinuxModeFromFileMode(fs.ModeDir | 0o755),
				UID:  0,
				GID:  0,
				RDev: 0,
			}
			entries[parent] = fsmeta.Entry{
				UID:  0,
				GID:  0,
				Mode: fsmeta.LinuxModeFromFileMode(fs.ModeDir | 0o755),
			}
			parent = path.Dir(parent)
		}
	}
	if _, ok := merged["/"]; !ok {
		merged["/"] = &indexedNode{
			Path: "/",
			Kind: indexedKindDir,
			Mode: fsmeta.LinuxModeFromFileMode(fs.ModeDir | 0o755),
		}
	}
}

func removeMergedPath(merged map[string]*indexedNode, entries map[string]fsmeta.Entry, target string) {
	for key := range merged {
		if key == target || strings.HasPrefix(key, target+"/") {
			delete(merged, key)
			delete(entries, key)
		}
	}
}

func imagefsMode(mode uint32, fallback fs.FileMode) fs.FileMode {
	if mode == 0 {
		return fallback
	}
	return ociLinuxModeToGo(mode)
}

const (
	ociLinuxSIFMT    = linuxabi.SIFMT
	ociLinuxSIFSOCK  = linuxabi.SIFSOCK
	ociLinuxSIFLNK   = linuxabi.SIFLNK
	ociLinuxSIFREG   = linuxabi.SIFREG
	ociLinuxSIFBLK   = linuxabi.SIFBLK
	ociLinuxSIFDIR   = linuxabi.SIFDIR
	ociLinuxSIFCHR   = linuxabi.SIFCHR
	ociLinuxSIFIFO   = linuxabi.SIFIFO
	ociLinuxPermMask = linuxabi.PermMask
)

func ociLinuxModeToGo(mode uint32) fs.FileMode {
	perm := fs.FileMode(mode & ociLinuxPermMask)
	switch mode & ociLinuxSIFMT {
	case ociLinuxSIFDIR:
		perm |= fs.ModeDir
	case ociLinuxSIFLNK:
		perm |= fs.ModeSymlink
	case ociLinuxSIFIFO:
		perm |= fs.ModeNamedPipe
	case ociLinuxSIFCHR:
		perm |= fs.ModeDevice | fs.ModeCharDevice
	case ociLinuxSIFBLK:
		perm |= fs.ModeDevice
	case ociLinuxSIFSOCK:
		perm |= fs.ModeSocket
	case ociLinuxSIFREG:
	}
	return perm
}
