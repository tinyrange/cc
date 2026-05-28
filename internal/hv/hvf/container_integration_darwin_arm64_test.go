//go:build darwin && arm64

package hvf

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"j5.nz/cc/internal/guestinit"
	"j5.nz/cc/internal/imagefs"
	"j5.nz/cc/internal/kernel/alpine"
	"j5.nz/cc/internal/oci"
	"j5.nz/cc/internal/virtio"
)

type memFS struct {
	mu         sync.Mutex
	nextNodeID uint64
	nextHandle uint64
	nodes      map[uint64]*memNode
	handles    map[uint64]uint64
	dirHandles map[uint64][]memDirEnt
}

type memNode struct {
	id      uint64
	parent  uint64
	name    string
	mode    uint32
	data    []byte
	target  string
	entries map[string]uint64
	xattrs  map[string][]byte
}

type memDirEnt struct {
	name string
	typ  uint32
	ino  uint64
}

const (
	testLinuxENOENT    int32 = 2
	testLinuxENXIO     int32 = 6
	testLinuxEBADF     int32 = 9
	testLinuxEEXIST    int32 = 17
	testLinuxENOTDIR   int32 = 20
	testLinuxEISDIR    int32 = 21
	testLinuxEINVAL    int32 = 22
	testLinuxENOTEMPTY int32 = 39
	testLinuxENODATA   int32 = 61
)

func newMemFS() *memFS {
	fs := &memFS{
		nextNodeID: 2,
		nextHandle: 1,
		nodes:      map[uint64]*memNode{},
		handles:    map[uint64]uint64{},
		dirHandles: map[uint64][]memDirEnt{},
	}
	fs.nodes[1] = &memNode{id: 1, parent: 1, name: "/", mode: 0o040755, entries: map[string]uint64{}}
	return fs
}

func (m *memFS) addDir(parent uint64, name string, perm uint32) uint64 {
	id := m.nextNodeID
	m.nextNodeID++
	m.nodes[id] = &memNode{id: id, parent: parent, name: name, mode: 0o040000 | (perm & 0o7777), entries: map[string]uint64{}}
	m.nodes[parent].entries[name] = id
	return id
}

func (m *memFS) addFile(parent uint64, name string, perm uint32, data []byte) uint64 {
	id := m.nextNodeID
	m.nextNodeID++
	m.nodes[id] = &memNode{
		id:     id,
		parent: parent,
		name:   name,
		mode:   0o100000 | (perm & 0o7777),
		data:   append([]byte(nil), data...),
		xattrs: map[string][]byte{},
	}
	m.nodes[parent].entries[name] = id
	return id
}

func (m *memFS) setXattr(nodeID uint64, name string, value []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := m.nodes[nodeID]
	if n == nil {
		return
	}
	if n.xattrs == nil {
		n.xattrs = map[string][]byte{}
	}
	n.xattrs[name] = append([]byte(nil), value...)
}

func (m *memFS) Init() (uint32, uint32) { return 128 << 10, 0 }

func (m *memFS) GetAttr(nodeID uint64) (virtio.FuseAttr, int32) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := m.nodes[nodeID]
	if n == nil {
		return virtio.FuseAttr{}, -testLinuxENOENT
	}
	return m.attr(n), 0
}

func (m *memFS) Lookup(parent uint64, name string) (uint64, virtio.FuseAttr, int32) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.nodes[parent]
	if p == nil || p.entries == nil {
		return 0, virtio.FuseAttr{}, -testLinuxENOENT
	}
	name = pathBase(name)
	if name == "." {
		return p.id, m.attr(p), 0
	}
	id, ok := p.entries[name]
	if !ok {
		return 0, virtio.FuseAttr{}, -testLinuxENOENT
	}
	return id, m.attr(m.nodes[id]), 0
}

func (m *memFS) Open(nodeID uint64, _ uint32) (uint64, int32) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := m.nodes[nodeID]
	if n == nil {
		return 0, -testLinuxENOENT
	}
	if n.mode&0o170000 == 0o040000 {
		return 0, -testLinuxEISDIR
	}
	fh := m.nextHandle
	m.nextHandle++
	m.handles[fh] = nodeID
	return fh, 0
}

func (m *memFS) Release(_ uint64, fh uint64)              { m.mu.Lock(); delete(m.handles, fh); m.mu.Unlock() }
func (m *memFS) Flush(_ uint64, _ uint64, _ uint64) int32 { return 0 }

func (m *memFS) Read(nodeID uint64, fh uint64, off uint64, size uint32) ([]byte, int32) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.handles[fh] != nodeID {
		return nil, -testLinuxEBADF
	}
	n := m.nodes[nodeID]
	if n == nil {
		return nil, -testLinuxENOENT
	}
	if off >= uint64(len(n.data)) {
		return []byte{}, 0
	}
	end := off + uint64(size)
	if end > uint64(len(n.data)) {
		end = uint64(len(n.data))
	}
	return append([]byte(nil), n.data[off:end]...), 0
}

func (m *memFS) OpenDir(nodeID uint64, _ uint32) (uint64, int32) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := m.nodes[nodeID]
	if n == nil {
		return 0, -testLinuxENOENT
	}
	if n.entries == nil {
		return 0, -testLinuxENOTDIR
	}
	entries := []memDirEnt{{name: ".", typ: 4, ino: n.id}, {name: "..", typ: 4, ino: n.parent}}
	names := make([]string, 0, len(n.entries))
	for name := range n.entries {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		child := m.nodes[n.entries[name]]
		typ := uint32(8)
		if child.mode&0o170000 == 0o040000 {
			typ = 4
		}
		entries = append(entries, memDirEnt{name: name, typ: typ, ino: child.id})
	}
	fh := m.nextHandle
	m.nextHandle++
	m.dirHandles[fh] = entries
	return fh, 0
}

func (m *memFS) ReadDir(_ uint64, fh uint64, off uint64, maxBytes uint32) ([]byte, int32) {
	m.mu.Lock()
	entries := append([]memDirEnt(nil), m.dirHandles[fh]...)
	m.mu.Unlock()
	if entries == nil {
		return nil, -testLinuxEBADF
	}
	var out []byte
	for i := int(off); i < len(entries); i++ {
		nameBytes := []byte(entries[i].name)
		reclen := align8ForTest(24 + len(nameBytes))
		if len(out)+reclen > int(maxBytes) {
			break
		}
		start := len(out)
		out = append(out, make([]byte, reclen)...)
		putLE64(out[start:start+8], entries[i].ino)
		putLE64(out[start+8:start+16], uint64(i+1))
		putLE32(out[start+16:start+20], uint32(len(nameBytes)))
		putLE32(out[start+20:start+24], entries[i].typ)
		copy(out[start+24:start+24+len(nameBytes)], nameBytes)
	}
	return out, 0
}

func (m *memFS) ReleaseDir(_ uint64, fh uint64)    { m.mu.Lock(); delete(m.dirHandles, fh); m.mu.Unlock() }
func (m *memFS) Readlink(_ uint64) (string, int32) { return "", -testLinuxEINVAL }
func (m *memFS) StatFS(_ uint64) (uint64, uint64, uint64, uint64, uint64, uint64, uint64, uint64, int32) {
	return 1024, 1024, 1024, 16, 16, 4096, 4096, 255, 0
}
func (m *memFS) GetXattr(nodeID uint64, name string) ([]byte, int32) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := m.nodes[nodeID]
	if n == nil {
		return nil, -testLinuxENOENT
	}
	val, ok := n.xattrs[name]
	if !ok {
		return nil, -testLinuxENODATA
	}
	return append([]byte(nil), val...), 0
}

func (m *memFS) ListXattr(nodeID uint64) ([]byte, int32) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := m.nodes[nodeID]
	if n == nil {
		return nil, -testLinuxENOENT
	}
	if len(n.xattrs) == 0 {
		return []byte{}, 0
	}
	names := make([]string, 0, len(n.xattrs))
	for name := range n.xattrs {
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
func (m *memFS) Lseek(nodeID uint64, fh uint64, offset uint64, whence uint32) (uint64, int32) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.handles[fh] != nodeID {
		return 0, -testLinuxEBADF
	}
	n := m.nodes[nodeID]
	if n == nil {
		return 0, -testLinuxENOENT
	}
	switch whence {
	case 3:
		if offset >= uint64(len(n.data)) {
			return 0, -testLinuxENXIO
		}
		return offset, 0
	case 4:
		if offset >= uint64(len(n.data)) {
			return offset, 0
		}
		return uint64(len(n.data)), 0
	default:
		return 0, -testLinuxEINVAL
	}
}
func (m *memFS) Mkdir(parent uint64, name string, mode uint32, _ uint32, _ uint32) (uint64, virtio.FuseAttr, int32) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.nodes[parent]
	if p == nil || p.entries == nil {
		return 0, virtio.FuseAttr{}, -testLinuxENOENT
	}
	name = pathBase(name)
	if _, ok := p.entries[name]; ok {
		return 0, virtio.FuseAttr{}, -testLinuxEEXIST
	}
	id := m.nextNodeID
	m.nextNodeID++
	n := &memNode{id: id, parent: parent, name: name, mode: 0o040000 | (mode & 0o7777), entries: map[string]uint64{}}
	m.nodes[id] = n
	p.entries[name] = id
	return id, m.attr(n), 0
}
func (m *memFS) RmDir(parent uint64, name string) int32 {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.nodes[parent]
	if p == nil || p.entries == nil {
		return -testLinuxENOENT
	}
	name = pathBase(name)
	id, ok := p.entries[name]
	if !ok {
		return -testLinuxENOENT
	}
	n := m.nodes[id]
	if n == nil || len(n.entries) != 0 {
		return -testLinuxENOTEMPTY
	}
	delete(p.entries, name)
	delete(m.nodes, id)
	return 0
}

func (m *memFS) attr(n *memNode) virtio.FuseAttr {
	size := uint64(len(n.data))
	if n.entries != nil {
		size = 0
	}
	nlink := uint32(1)
	if n.entries != nil {
		nlink = 2 + uint32(len(n.entries))
	}
	return virtio.FuseAttr{
		Ino:     n.id,
		Size:    size,
		Blocks:  (size + 511) / 512,
		Mode:    n.mode,
		NLink:   nlink,
		UID:     0,
		GID:     0,
		BlkSize: 4096,
	}
}

func pathBase(name string) string {
	name = filepath.ToSlash(name)
	name = strings.TrimPrefix(name, "/")
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		return name[i+1:]
	}
	return name
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

func align8ForTest(n int) int { return (n + 7) &^ 7 }

func TestRunAlpineUname(t *testing.T) {
	if os.Getenv("CCX3_RUN_ALPINE") == "" {
		t.Skip("set CCX3_RUN_ALPINE=1 to run the live alpine container boot test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), liveTestTimeout())
	defer cancel()

	root := t.TempDir()
	kernel := alpine.NewManager(filepath.Join(root, "kernel"))
	if err := kernel.Ensure(ctx); err != nil {
		t.Fatalf("kernel.Ensure() error = %v", err)
	}
	kernelBytes, err := kernel.ReadKernel()
	if err != nil {
		t.Fatalf("kernel.ReadKernel() error = %v", err)
	}
	modules, err := kernel.PlanModuleLoad(
		[]string{"CONFIG_VIRTIO_MMIO", "CONFIG_FUSE_FS", "CONFIG_VIRTIO_FS"},
		map[string]string{
			"CONFIG_VIRTIO_MMIO": "kernel/drivers/virtio/virtio_mmio.ko.gz",
			"CONFIG_FUSE_FS":     "kernel/fs/fuse/fuse.ko.gz",
			"CONFIG_VIRTIO_FS":   "kernel/fs/fuse/virtiofs.ko.gz",
		},
	)
	if err != nil {
		t.Fatalf("kernel.PlanModuleLoad() error = %v", err)
	}
	store := oci.NewStore(filepath.Join(root, "images"))
	if _, err := store.Pull(ctx, "alpine", "alpine:latest"); err != nil {
		t.Fatalf("store.Pull() error = %v", err)
	}
	image, err := store.Open("alpine")
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	initBin, err := guestinit.Build(ctx, filepath.Join(root, "guestinit"))
	if err != nil {
		t.Fatalf("guestinit.Build() error = %v", err)
	}

	result, err := RunContainer(ctx, ContainerRunRequest{
		Kernel:  kernelBytes,
		Init:    initBin,
		Modules: modules,
		Image:   image,
		Command: []string{"uname", "-a"},
		Dmesg:   true,
	})
	if err != nil {
		t.Fatalf("RunContainer() error = %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("RunContainer().ExitCode = %d, want 0\ntranscript:\n%s", result.ExitCode, result.Transcript)
	}
	if !strings.Contains(result.Transcript, "Linux") {
		t.Fatalf("transcript did not contain uname output\ntranscript:\n%s", result.Transcript)
	}
}

func TestRunAlpineSMPNproc(t *testing.T) {
	if os.Getenv("CCX3_RUN_ALPINE") == "" {
		t.Skip("set CCX3_RUN_ALPINE=1 to run the live alpine container boot test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), liveTestTimeout())
	defer cancel()

	root := t.TempDir()
	kernel := alpine.NewManager(filepath.Join(root, "kernel"))
	if err := kernel.Ensure(ctx); err != nil {
		t.Fatalf("kernel.Ensure() error = %v", err)
	}
	kernelBytes, err := kernel.ReadKernel()
	if err != nil {
		t.Fatalf("kernel.ReadKernel() error = %v", err)
	}
	modules, err := kernel.PlanModuleLoad(
		[]string{"CONFIG_VIRTIO_MMIO", "CONFIG_FUSE_FS", "CONFIG_VIRTIO_FS"},
		map[string]string{
			"CONFIG_VIRTIO_MMIO": "kernel/drivers/virtio/virtio_mmio.ko.gz",
			"CONFIG_FUSE_FS":     "kernel/fs/fuse/fuse.ko.gz",
			"CONFIG_VIRTIO_FS":   "kernel/fs/fuse/virtiofs.ko.gz",
		},
	)
	if err != nil {
		t.Fatalf("kernel.PlanModuleLoad() error = %v", err)
	}
	store := oci.NewStore(filepath.Join(root, "images"))
	if _, err := store.Pull(ctx, "alpine", "alpine:latest"); err != nil {
		t.Fatalf("store.Pull() error = %v", err)
	}
	image, err := store.Open("alpine")
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	initBin, err := guestinit.Build(ctx, filepath.Join(root, "guestinit"))
	if err != nil {
		t.Fatalf("guestinit.Build() error = %v", err)
	}

	for _, cpus := range []int{2, 4, 8} {
		t.Run(fmt.Sprintf("%dcpus", cpus), func(t *testing.T) {
			online := fmt.Sprintf("0-%d", cpus-1)
			result, err := RunContainer(ctx, ContainerRunRequest{
				Kernel:  kernelBytes,
				Init:    initBin,
				Modules: modules,
				Image:   image,
				Command: []string{"sh", "-c", "nproc && cat /sys/devices/system/cpu/online"},
				CPUs:    cpus,
				Dmesg:   true,
			})
			if err != nil {
				t.Fatalf("RunContainer() error = %v\ntranscript:\n%s", err, result.Transcript)
			}
			if result.ExitCode != 0 {
				t.Fatalf("RunContainer().ExitCode = %d, want 0\ntranscript:\n%s", result.ExitCode, result.Transcript)
			}
			if !strings.Contains(result.Output, fmt.Sprintf("%d", cpus)) || !strings.Contains(result.Output, online) {
				t.Fatalf("guest did not report %d CPUs\noutput:\n%s\ntranscript:\n%s", cpus, result.Output, result.Transcript)
			}
		})
	}
}

func TestRunAlpineShowsLoadedVirtioFSModules(t *testing.T) {
	if os.Getenv("CCX3_RUN_ALPINE") == "" {
		t.Skip("set CCX3_RUN_ALPINE=1 to run the live alpine container boot test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), liveTestTimeout())
	defer cancel()

	root := t.TempDir()
	kernel := alpine.NewManager(filepath.Join(root, "kernel"))
	if err := kernel.Ensure(ctx); err != nil {
		t.Fatalf("kernel.Ensure() error = %v", err)
	}
	kernelBytes, err := kernel.ReadKernel()
	if err != nil {
		t.Fatalf("kernel.ReadKernel() error = %v", err)
	}
	modules, err := kernel.PlanModuleLoad(
		[]string{"CONFIG_VIRTIO_MMIO", "CONFIG_FUSE_FS", "CONFIG_VIRTIO_FS"},
		map[string]string{
			"CONFIG_VIRTIO_MMIO": "kernel/drivers/virtio/virtio_mmio.ko.gz",
			"CONFIG_FUSE_FS":     "kernel/fs/fuse/fuse.ko.gz",
			"CONFIG_VIRTIO_FS":   "kernel/fs/fuse/virtiofs.ko.gz",
		},
	)
	if err != nil {
		t.Fatalf("kernel.PlanModuleLoad() error = %v", err)
	}

	store := oci.NewStore(filepath.Join(root, "images"))
	if _, err := store.Pull(ctx, "alpine", "alpine:latest"); err != nil {
		t.Fatalf("store.Pull() error = %v", err)
	}
	image, err := store.Open("alpine")
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	initBin, err := guestinit.Build(ctx, filepath.Join(root, "guestinit"))
	if err != nil {
		t.Fatalf("guestinit.Build() error = %v", err)
	}

	result, err := RunContainer(ctx, ContainerRunRequest{
		Kernel:  kernelBytes,
		Init:    initBin,
		Modules: modules,
		Image:   image,
		Command: []string{"/bin/cat", "/proc/modules"},
		Dmesg:   false,
	})
	if err != nil {
		t.Fatalf("RunContainer() error = %v", err)
	}
	for _, want := range []string{"virtio_mmio", "fuse", "virtiofs"} {
		if !strings.Contains(result.Output, want) {
			t.Fatalf("module output missing %q\noutput:\n%s", want, result.Output)
		}
	}
}

func TestRunAlpineSeesVirtioConsole(t *testing.T) {
	if os.Getenv("CCX3_RUN_ALPINE") == "" {
		t.Skip("set CCX3_RUN_ALPINE=1 to run the live alpine container boot test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), liveTestTimeout())
	defer cancel()

	root := t.TempDir()
	kernel := alpine.NewManager(filepath.Join(root, "kernel"))
	if err := kernel.Ensure(ctx); err != nil {
		t.Fatalf("kernel.Ensure() error = %v", err)
	}
	kernelBytes, err := kernel.ReadKernel()
	if err != nil {
		t.Fatalf("kernel.ReadKernel() error = %v", err)
	}
	modules, err := kernel.PlanModuleLoad(
		[]string{"CONFIG_VIRTIO_MMIO", "CONFIG_FUSE_FS", "CONFIG_VIRTIO_FS"},
		map[string]string{
			"CONFIG_VIRTIO_MMIO": "kernel/drivers/virtio/virtio_mmio.ko.gz",
			"CONFIG_FUSE_FS":     "kernel/fs/fuse/fuse.ko.gz",
			"CONFIG_VIRTIO_FS":   "kernel/fs/fuse/virtiofs.ko.gz",
		},
	)
	if err != nil {
		t.Fatalf("kernel.PlanModuleLoad() error = %v", err)
	}

	store := oci.NewStore(filepath.Join(root, "images"))
	if _, err := store.Pull(ctx, "alpine", "alpine:latest"); err != nil {
		t.Fatalf("store.Pull() error = %v", err)
	}
	image, err := store.Open("alpine")
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	initBin, err := guestinit.Build(ctx, filepath.Join(root, "guestinit"))
	if err != nil {
		t.Fatalf("guestinit.Build() error = %v", err)
	}

	result, err := RunContainer(ctx, ContainerRunRequest{
		Kernel:  kernelBytes,
		Init:    initBin,
		Modules: modules,
		Image:   image,
		Command: []string{
			"/bin/sh", "-lc",
			"test -e /sys/class/tty/hvc0/dev && cat /sys/class/tty/hvc0/dev",
		},
		Dmesg: false,
	})
	if err != nil {
		t.Fatalf("RunContainer() error = %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("RunContainer().ExitCode = %d, want 0\ntranscript:\n%s", result.ExitCode, result.Transcript)
	}
	if !strings.Contains(strings.TrimSpace(result.Output), ":") {
		t.Fatalf("virtio console output = %q, want tty major:minor", result.Output)
	}
}

func TestRunStaticProbeFromVirtioFS(t *testing.T) {
	if os.Getenv("CCX3_RUN_ALPINE") == "" {
		t.Skip("set CCX3_RUN_ALPINE=1 to run the live alpine container boot test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), liveTestTimeout())
	defer cancel()

	root := t.TempDir()
	kernel := alpine.NewManager(filepath.Join(root, "kernel"))
	if err := kernel.Ensure(ctx); err != nil {
		t.Fatalf("kernel.Ensure() error = %v", err)
	}
	kernelBytes, err := kernel.ReadKernel()
	if err != nil {
		t.Fatalf("kernel.ReadKernel() error = %v", err)
	}
	modules, err := kernel.PlanModuleLoad(
		[]string{"CONFIG_VIRTIO_MMIO", "CONFIG_FUSE_FS", "CONFIG_VIRTIO_FS"},
		map[string]string{
			"CONFIG_VIRTIO_MMIO": "kernel/drivers/virtio/virtio_mmio.ko.gz",
			"CONFIG_FUSE_FS":     "kernel/fs/fuse/fuse.ko.gz",
			"CONFIG_VIRTIO_FS":   "kernel/fs/fuse/virtiofs.ko.gz",
		},
	)
	if err != nil {
		t.Fatalf("kernel.PlanModuleLoad() error = %v", err)
	}

	store := oci.NewStore(filepath.Join(root, "images"))
	if _, err := store.Pull(ctx, "alpine", "alpine:latest"); err != nil {
		t.Fatalf("store.Pull() error = %v", err)
	}
	image, err := store.Open("alpine")
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	staticProbe, err := buildStaticProbeBytes(ctx, root)
	if err != nil {
		t.Fatalf("buildStaticProbeBytes() error = %v", err)
	}
	image = overlayImageWithFile(t, image, "/bin/ccx3-static-probe", 0o755, staticProbe)

	initBin, err := guestinit.Build(ctx, filepath.Join(root, "guestinit"))
	if err != nil {
		t.Fatalf("guestinit.Build() error = %v", err)
	}

	result, err := RunContainer(ctx, ContainerRunRequest{
		Kernel:  kernelBytes,
		Init:    initBin,
		Modules: modules,
		Image:   image,
		Command: []string{"/bin/ccx3-static-probe"},
		Dmesg:   false,
	})
	if err != nil {
		t.Fatalf("RunContainer() error = %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("RunContainer().ExitCode = %d, want 0\ntranscript:\n%s", result.ExitCode, result.Transcript)
	}
	if !strings.Contains(result.Output, "hello-static") {
		t.Fatalf("static probe output missing\noutput:\n%s", result.Output)
	}
}

func TestRunMinimalELFFromVirtioFS(t *testing.T) {
	if os.Getenv("CCX3_RUN_ALPINE") == "" {
		t.Skip("set CCX3_RUN_ALPINE=1 to run the live alpine container boot test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), liveTestTimeout())
	defer cancel()

	root := t.TempDir()
	kernel := alpine.NewManager(filepath.Join(root, "kernel"))
	if err := kernel.Ensure(ctx); err != nil {
		t.Fatalf("kernel.Ensure() error = %v", err)
	}
	kernelBytes, err := kernel.ReadKernel()
	if err != nil {
		t.Fatalf("kernel.ReadKernel() error = %v", err)
	}
	modules, err := kernel.PlanModuleLoad(
		[]string{"CONFIG_VIRTIO_MMIO", "CONFIG_FUSE_FS", "CONFIG_VIRTIO_FS"},
		map[string]string{
			"CONFIG_VIRTIO_MMIO": "kernel/drivers/virtio/virtio_mmio.ko.gz",
			"CONFIG_FUSE_FS":     "kernel/fs/fuse/fuse.ko.gz",
			"CONFIG_VIRTIO_FS":   "kernel/fs/fuse/virtiofs.ko.gz",
		},
	)
	if err != nil {
		t.Fatalf("kernel.PlanModuleLoad() error = %v", err)
	}

	store := oci.NewStore(filepath.Join(root, "images"))
	if _, err := store.Pull(ctx, "alpine", "alpine:latest"); err != nil {
		t.Fatalf("store.Pull() error = %v", err)
	}
	image, err := store.Open("alpine")
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	minElf, err := buildMinimalELFBytes(ctx, root)
	if err != nil {
		t.Fatalf("buildMinimalELFBytes() error = %v", err)
	}
	image = overlayImageWithFile(t, image, "/bin/ccx3-min-elf", 0o755, minElf)

	initBin, err := guestinit.Build(ctx, filepath.Join(root, "guestinit"))
	if err != nil {
		t.Fatalf("guestinit.Build() error = %v", err)
	}

	result, err := RunContainer(ctx, ContainerRunRequest{
		Kernel:  kernelBytes,
		Init:    initBin,
		Modules: modules,
		Image:   image,
		Command: []string{"/bin/ccx3-min-elf"},
		Dmesg:   true,
	})
	if err != nil {
		t.Fatalf("RunContainer() error = %v\ntranscript:\n%s", err, result.Transcript)
	}
	if result.ExitCode != 42 {
		t.Fatalf("minimal ELF exit code = %d, want 42\ntranscript:\n%s", result.ExitCode, result.Transcript)
	}
}

func TestRunMinimalELFFromInMemoryVirtioFS(t *testing.T) {
	if os.Getenv("CCX3_RUN_ALPINE") == "" {
		t.Skip("set CCX3_RUN_ALPINE=1 to run the live alpine container boot test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), liveTestTimeout())
	defer cancel()

	root := t.TempDir()
	kernel := alpine.NewManager(filepath.Join(root, "kernel"))
	if err := kernel.Ensure(ctx); err != nil {
		t.Fatalf("kernel.Ensure() error = %v", err)
	}
	kernelBytes, err := kernel.ReadKernel()
	if err != nil {
		t.Fatalf("kernel.ReadKernel() error = %v", err)
	}
	modules, err := kernel.PlanModuleLoad(
		[]string{"CONFIG_VIRTIO_MMIO", "CONFIG_FUSE_FS", "CONFIG_VIRTIO_FS"},
		map[string]string{
			"CONFIG_VIRTIO_MMIO": "kernel/drivers/virtio/virtio_mmio.ko.gz",
			"CONFIG_FUSE_FS":     "kernel/fs/fuse/fuse.ko.gz",
			"CONFIG_VIRTIO_FS":   "kernel/fs/fuse/virtiofs.ko.gz",
		},
	)
	if err != nil {
		t.Fatalf("kernel.PlanModuleLoad() error = %v", err)
	}
	minElf, err := buildMinimalELFBytes(ctx, root)
	if err != nil {
		t.Fatalf("buildMinimalELFBytes() error = %v", err)
	}
	initBin, err := guestinit.Build(ctx, filepath.Join(root, "guestinit"))
	if err != nil {
		t.Fatalf("guestinit.Build() error = %v", err)
	}

	memfs := newMemFS()
	binID := memfs.addDir(1, "bin", 0o755)
	memfs.addFile(binID, "ccx3-min-elf", 0o755, minElf)

	result, err := RunContainer(ctx, ContainerRunRequest{
		Kernel:  kernelBytes,
		Init:    initBin,
		Modules: modules,
		RootFS:  memfs,
		Command: []string{"/bin/ccx3-min-elf"},
		Dmesg:   true,
	})
	if err != nil {
		t.Fatalf("RunContainer() error = %v\ntranscript:\n%s", err, result.Transcript)
	}
	if result.ExitCode != 42 {
		t.Fatalf("minimal in-memory ELF exit code = %d, want 42\ntranscript:\n%s", result.ExitCode, result.Transcript)
	}
}

func liveTestTimeout() time.Duration {
	if raw := strings.TrimSpace(os.Getenv("CCX3_TEST_TIMEOUT_SEC")); raw != "" {
		if secs, err := strconv.Atoi(raw); err == nil && secs > 0 {
			return time.Duration(secs) * time.Second
		}
	}
	return 90 * time.Second
}

func buildStaticProbe(ctx context.Context, outPath string) error {
	dir := filepath.Dir(outPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir probe dir: %w", err)
	}
	tmp := filepath.Join(dir, "ccx3_static_probe_src.go")
	src := "package main\nimport \"fmt\"\nfunc main(){fmt.Println(\"hello-static\")}\n"
	if err := os.WriteFile(tmp, []byte(src), 0o644); err != nil {
		return fmt.Errorf("write probe source: %w", err)
	}
	defer os.Remove(tmp)
	cmd := exec.CommandContext(ctx, "go", "build", "-o", outPath, tmp)
	cmd.Env = append(os.Environ(), "GOOS=linux", "GOARCH=arm64", "CGO_ENABLED=0")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("build probe: %w\n%s", err, string(output))
	}
	return nil
}

func buildStaticProbeBytes(ctx context.Context, dir string) ([]byte, error) {
	outPath := filepath.Join(dir, "ccx3-static-probe")
	if err := buildStaticProbe(ctx, outPath); err != nil {
		return nil, err
	}
	return os.ReadFile(outPath)
}

func buildMinimalELF(ctx context.Context, outPath string) error {
	dir := filepath.Dir(outPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir minimal elf dir: %w", err)
	}
	src := filepath.Join(dir, "ccx3_min_elf.S")
	asm := ".text\n.globl _start\n_start:\n    mov x0, #42\n    mov x8, #93\n    svc #0\n"
	if err := os.WriteFile(src, []byte(asm), 0o644); err != nil {
		return fmt.Errorf("write minimal elf source: %w", err)
	}
	defer os.Remove(src)

	toolchain, err := clangAArch64LinuxToolchain()
	if err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx,
		toolchain.clang,
		"-target", "aarch64-linux-gnu",
		"-nostdlib",
		"-static",
		"-fuse-ld=lld",
		"-Wl,-e,_start",
		"-o", outPath,
		src,
	)
	cmd.Env = toolchain.env
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("build minimal elf: %w\n%s", err, string(output))
	}
	return nil
}

func buildMinimalELFBytes(ctx context.Context, dir string) ([]byte, error) {
	outPath := filepath.Join(dir, "ccx3-min-elf")
	if err := buildMinimalELF(ctx, outPath); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func overlayImageWithFile(t *testing.T, image *oci.Image, guestPath string, mode os.FileMode, data []byte) *oci.Image {
	t.Helper()
	overlay := imagefs.NewOverlay(image.RootFS)
	if err := overlay.AddFile(guestPath, mode, data); err != nil {
		t.Fatalf("overlay.AddFile(%q) error = %v", guestPath, err)
	}
	cloned := *image
	cloned.RootFS = overlay.Root()
	return &cloned
}
