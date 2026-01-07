package virtio

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/timeslice"
)

// -----------------------------------------------------------------------------
// Test infrastructure (similar to fs_bench_test.go)
// -----------------------------------------------------------------------------

// netTestVM implements a minimal hv.VirtualMachine for net testing
type netTestVM struct {
	memory []byte
	irqs   map[uint32]bool
	mu     sync.Mutex
}

func newNetTestVM(memorySize int) *netTestVM {
	return &netTestVM{
		memory: make([]byte, memorySize),
		irqs:   make(map[uint32]bool),
	}
}

func (vm *netTestVM) ReadAt(p []byte, off int64) (int, error) {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	if off < 0 || int(off)+len(p) > len(vm.memory) {
		return 0, fmt.Errorf("read out of bounds: offset=%d len=%d memsize=%d", off, len(p), len(vm.memory))
	}
	copy(p, vm.memory[off:off+int64(len(p))])
	return len(p), nil
}

func (vm *netTestVM) WriteAt(p []byte, off int64) (int, error) {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	if off < 0 || int(off)+len(p) > len(vm.memory) {
		return 0, fmt.Errorf("write out of bounds: offset=%d len=%d memsize=%d", off, len(p), len(vm.memory))
	}
	copy(vm.memory[off:], p)
	return len(p), nil
}

func (vm *netTestVM) SetIRQ(line uint32, level bool) error {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	vm.irqs[line] = level
	return nil
}

func (vm *netTestVM) GetIRQ(line uint32) bool {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	return vm.irqs[line]
}

// Implement other required hv.VirtualMachine methods as no-ops
func (vm *netTestVM) Close() error                                              { return nil }
func (vm *netTestVM) Hypervisor() hv.Hypervisor                                 { return nil }
func (vm *netTestVM) MemorySize() uint64                                        { return uint64(len(vm.memory)) }
func (vm *netTestVM) MemoryBase() uint64                                        { return 0 }
func (vm *netTestVM) Run(ctx context.Context, cfg hv.RunConfig) error           { return nil }
func (vm *netTestVM) VirtualCPUCall(id int, f func(vcpu hv.VirtualCPU) error) error { return nil }
func (vm *netTestVM) AddDevice(dev hv.Device) error                             { return nil }
func (vm *netTestVM) AddDeviceFromTemplate(template hv.DeviceTemplate) error    { return nil }
func (vm *netTestVM) AllocateMemory(physAddr, size uint64) (hv.MemoryRegion, error) {
	return nil, fmt.Errorf("not implemented")
}
func (vm *netTestVM) CaptureSnapshot() (hv.Snapshot, error) { return nil, nil }
func (vm *netTestVM) RestoreSnapshot(snap hv.Snapshot) error { return nil }

var _ hv.VirtualMachine = (*netTestVM)(nil)

// netTestExitContext implements hv.ExitContext for testing
type netTestExitContext struct{}

func (m netTestExitContext) SetExitTimeslice(kind timeslice.TimesliceID) {}

var _ hv.ExitContext = netTestExitContext{}

// Memory layout constants for tests
const (
	netTestMemorySize = 16 * 1024 * 1024 // 16 MB

	// RX queue (queue 0)
	netTestRxDescTableAddr = 0x100000
	netTestRxAvailRingAddr = 0x101000
	netTestRxUsedRingAddr  = 0x102000

	// TX queue (queue 1)
	netTestTxDescTableAddr = 0x110000
	netTestTxAvailRingAddr = 0x111000
	netTestTxUsedRingAddr  = 0x112000

	// Packet buffers
	netTestBufferAddr = 0x200000 // 2 MB onwards

	// Queue size
	netTestQueueSize = 128

	// Virtio-net header size
	netTestHeaderSize = 12
)

// netMMIOHelper provides convenience methods for MMIO operations
type netMMIOHelper struct {
	net *Net
	ctx hv.ExitContext
}

func newNetMMIOHelper(net *Net) *netMMIOHelper {
	return &netMMIOHelper{
		net: net,
		ctx: netTestExitContext{},
	}
}

func (h *netMMIOHelper) readReg(offset uint64) uint32 {
	data := make([]byte, 4)
	if err := h.net.ReadMMIO(h.ctx, NetDefaultMMIOBase+offset, data); err != nil {
		panic(fmt.Sprintf("readReg failed: %v", err))
	}
	return binary.LittleEndian.Uint32(data)
}

func (h *netMMIOHelper) writeReg(offset uint64, value uint32) {
	data := make([]byte, 4)
	binary.LittleEndian.PutUint32(data, value)
	if err := h.net.WriteMMIO(h.ctx, NetDefaultMMIOBase+offset, data); err != nil {
		panic(fmt.Sprintf("writeReg failed: %v", err))
	}
}

// netVirtqueueSetup sets up a virtqueue in guest memory
type netVirtqueueSetup struct {
	vm            *netTestVM
	descTableAddr uint64
	availRingAddr uint64
	usedRingAddr  uint64
	queueSize     uint16
	nextDescIdx   uint16
	availIdx      uint16
}

func newNetVirtqueueSetup(vm *netTestVM, descTable, availRing, usedRing uint64, size uint16) *netVirtqueueSetup {
	return &netVirtqueueSetup{
		vm:            vm,
		descTableAddr: descTable,
		availRingAddr: availRing,
		usedRingAddr:  usedRing,
		queueSize:     size,
	}
}

func (vq *netVirtqueueSetup) writeUint16(addr uint64, val uint16) {
	var buf [2]byte
	binary.LittleEndian.PutUint16(buf[:], val)
	vq.vm.WriteAt(buf[:], int64(addr))
}

func (vq *netVirtqueueSetup) writeUint32(addr uint64, val uint32) {
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], val)
	vq.vm.WriteAt(buf[:], int64(addr))
}

func (vq *netVirtqueueSetup) writeUint64(addr uint64, val uint64) {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], val)
	vq.vm.WriteAt(buf[:], int64(addr))
}

func (vq *netVirtqueueSetup) readUint16(addr uint64) uint16 {
	var buf [2]byte
	vq.vm.ReadAt(buf[:], int64(addr))
	return binary.LittleEndian.Uint16(buf[:])
}

func (vq *netVirtqueueSetup) readUint32(addr uint64) uint32 {
	var buf [4]byte
	vq.vm.ReadAt(buf[:], int64(addr))
	return binary.LittleEndian.Uint32(buf[:])
}

// initRings initializes the available and used rings
func (vq *netVirtqueueSetup) initRings() {
	// Initialize available ring header: flags=0, idx=0
	vq.writeUint16(vq.availRingAddr+0, 0)
	vq.writeUint16(vq.availRingAddr+2, 0)

	// Initialize used ring header: flags=0, idx=0
	vq.writeUint16(vq.usedRingAddr+0, 0)
	vq.writeUint16(vq.usedRingAddr+2, 0)
}

// writeDescriptor writes a descriptor to the descriptor table
func (vq *netVirtqueueSetup) writeDescriptor(idx uint16, addr uint64, length uint32, flags uint16, next uint16) {
	base := vq.descTableAddr + uint64(idx)*16
	vq.writeUint64(base+0, addr)
	vq.writeUint32(base+8, length)
	vq.writeUint16(base+12, flags)
	vq.writeUint16(base+14, next)
}

// addAvailableBuffer adds a buffer to the available ring
func (vq *netVirtqueueSetup) addAvailableBuffer(descIdx uint16) {
	ringIdx := vq.availIdx % vq.queueSize
	vq.writeUint16(vq.availRingAddr+4+uint64(ringIdx)*2, descIdx)
	vq.availIdx++
	vq.writeUint16(vq.availRingAddr+2, vq.availIdx)
}

// getUsedEntry reads a used ring entry
func (vq *netVirtqueueSetup) getUsedEntry(idx uint16) (head uint32, length uint32) {
	base := vq.usedRingAddr + 4 + uint64(idx)*8
	return vq.readUint32(base), vq.readUint32(base + 4)
}

// getUsedIdx reads the used ring index
func (vq *netVirtqueueSetup) getUsedIdx() uint16 {
	return vq.readUint16(vq.usedRingAddr + 2)
}

// writeBuffer writes data to a buffer in guest memory
func (vq *netVirtqueueSetup) writeBuffer(addr uint64, data []byte) {
	vq.vm.WriteAt(data, int64(addr))
}

// readBuffer reads data from a buffer in guest memory
func (vq *netVirtqueueSetup) readBuffer(addr uint64, length uint32) []byte {
	buf := make([]byte, length)
	vq.vm.ReadAt(buf, int64(addr))
	return buf
}

// Descriptor flags
const (
	netVirtqDescFNext  uint16 = 1
	netVirtqDescFWrite uint16 = 2
)

// -----------------------------------------------------------------------------
// Test Backends
// -----------------------------------------------------------------------------

// countingNetBackend counts TX packets (for pure TX benchmarks)
type countingNetBackend struct {
	txCount uint64
	txBytes uint64
}

func (b *countingNetBackend) HandleTx(packet []byte, release func()) error {
	atomic.AddUint64(&b.txCount, 1)
	atomic.AddUint64(&b.txBytes, uint64(len(packet)))
	if release != nil {
		release()
	}
	return nil
}

func (b *countingNetBackend) getTxCount() uint64 {
	return atomic.LoadUint64(&b.txCount)
}

func (b *countingNetBackend) getTxBytes() uint64 {
	return atomic.LoadUint64(&b.txBytes)
}

// loopbackNetBackend echoes TX packets back as RX packets
type loopbackNetBackend struct {
	net     *Net
	txCount uint64
}

func (b *loopbackNetBackend) HandleTx(packet []byte, release func()) error {
	atomic.AddUint64(&b.txCount, 1)
	// Echo packet back asynchronously to avoid deadlock
	// (HandleTx is called from the worker loop, so we can't synchronously enqueue)
	if b.net != nil {
		pkt := append([]byte(nil), packet...)
		go b.net.EnqueueRxPacket(pkt)
	}
	if release != nil {
		release()
	}
	return nil
}

func (b *loopbackNetBackend) BindNetDevice(net *Net) {
	b.net = net
}

// -----------------------------------------------------------------------------
// Test Network Client
// -----------------------------------------------------------------------------

type testNetClient struct {
	net     *Net
	mmio    *netMMIOHelper
	rxQueue *netVirtqueueSetup
	txQueue *netVirtqueueSetup
	vm      *netTestVM
	bufAddr uint64
	unique  uint64
}

func createNetTestSetup(tb testing.TB, backend NetBackend) (*Net, *testNetClient) {
	tb.Helper()

	vm := newNetTestVM(netTestMemorySize)
	mac, _ := net.ParseMAC("02:00:00:00:00:01")
	netDev := NewNet(vm, NetDefaultMMIOBase, NetDefaultMMIOSize, NetDefaultIRQLine, mac, backend)

	mmio := newNetMMIOHelper(netDev)

	// Set up RX queue (queue 0)
	rxQueue := newNetVirtqueueSetup(vm, netTestRxDescTableAddr, netTestRxAvailRingAddr, netTestRxUsedRingAddr, netTestQueueSize)
	rxQueue.initRings()

	// Set up TX queue (queue 1)
	txQueue := newNetVirtqueueSetup(vm, netTestTxDescTableAddr, netTestTxAvailRingAddr, netTestTxUsedRingAddr, netTestQueueSize)
	txQueue.initRings()

	client := &testNetClient{
		net:     netDev,
		mmio:    mmio,
		rxQueue: rxQueue,
		txQueue: txQueue,
		vm:      vm,
		bufAddr: netTestBufferAddr,
	}

	// Initialize device
	if err := client.initDevice(); err != nil {
		tb.Fatalf("initDevice failed: %v", err)
	}

	return netDev, client
}

func (c *testNetClient) initDevice() error {
	// Verify magic
	magic := c.mmio.readReg(VIRTIO_MMIO_MAGIC_VALUE)
	if magic != 0x74726976 {
		return fmt.Errorf("bad magic: 0x%x", magic)
	}

	// Read device features
	c.mmio.writeReg(VIRTIO_MMIO_DEVICE_FEATURES_SEL, 0)
	_ = c.mmio.readReg(VIRTIO_MMIO_DEVICE_FEATURES)

	// Acknowledge device
	c.mmio.writeReg(VIRTIO_MMIO_STATUS, 1) // ACKNOWLEDGE
	c.mmio.writeReg(VIRTIO_MMIO_STATUS, 3) // ACKNOWLEDGE | DRIVER

	// Select features (version 1, event idx, mac, merged rx buffers)
	c.mmio.writeReg(VIRTIO_MMIO_DRIVER_FEATURES_SEL, 0)
	c.mmio.writeReg(VIRTIO_MMIO_DRIVER_FEATURES, (1<<virtioNetFeatureMacBit)|(1<<virtioNetFeatureMrgRxBuf))
	c.mmio.writeReg(VIRTIO_MMIO_DRIVER_FEATURES_SEL, 1)
	c.mmio.writeReg(VIRTIO_MMIO_DRIVER_FEATURES, 1) // VIRTIO_F_VERSION_1

	c.mmio.writeReg(VIRTIO_MMIO_STATUS, 11) // ACKNOWLEDGE | DRIVER | FEATURES_OK

	// Configure RX queue (queue 0)
	c.mmio.writeReg(VIRTIO_MMIO_QUEUE_SEL, 0)
	c.mmio.writeReg(VIRTIO_MMIO_QUEUE_NUM, uint32(c.rxQueue.queueSize))
	c.mmio.writeReg(VIRTIO_MMIO_QUEUE_DESC_LOW, uint32(c.rxQueue.descTableAddr))
	c.mmio.writeReg(VIRTIO_MMIO_QUEUE_DESC_HIGH, uint32(c.rxQueue.descTableAddr>>32))
	c.mmio.writeReg(VIRTIO_MMIO_QUEUE_AVAIL_LOW, uint32(c.rxQueue.availRingAddr))
	c.mmio.writeReg(VIRTIO_MMIO_QUEUE_AVAIL_HIGH, uint32(c.rxQueue.availRingAddr>>32))
	c.mmio.writeReg(VIRTIO_MMIO_QUEUE_USED_LOW, uint32(c.rxQueue.usedRingAddr))
	c.mmio.writeReg(VIRTIO_MMIO_QUEUE_USED_HIGH, uint32(c.rxQueue.usedRingAddr>>32))
	c.mmio.writeReg(VIRTIO_MMIO_QUEUE_READY, 1)

	// Configure TX queue (queue 1)
	c.mmio.writeReg(VIRTIO_MMIO_QUEUE_SEL, 1)
	c.mmio.writeReg(VIRTIO_MMIO_QUEUE_NUM, uint32(c.txQueue.queueSize))
	c.mmio.writeReg(VIRTIO_MMIO_QUEUE_DESC_LOW, uint32(c.txQueue.descTableAddr))
	c.mmio.writeReg(VIRTIO_MMIO_QUEUE_DESC_HIGH, uint32(c.txQueue.descTableAddr>>32))
	c.mmio.writeReg(VIRTIO_MMIO_QUEUE_AVAIL_LOW, uint32(c.txQueue.availRingAddr))
	c.mmio.writeReg(VIRTIO_MMIO_QUEUE_AVAIL_HIGH, uint32(c.txQueue.availRingAddr>>32))
	c.mmio.writeReg(VIRTIO_MMIO_QUEUE_USED_LOW, uint32(c.txQueue.usedRingAddr))
	c.mmio.writeReg(VIRTIO_MMIO_QUEUE_USED_HIGH, uint32(c.txQueue.usedRingAddr>>32))
	c.mmio.writeReg(VIRTIO_MMIO_QUEUE_READY, 1)

	// Set DRIVER_OK status
	c.mmio.writeReg(VIRTIO_MMIO_STATUS, 15) // ACKNOWLEDGE | DRIVER | FEATURES_OK | DRIVER_OK

	return nil
}

// makeVirtioNetHeader creates a virtio-net header
func makeVirtioNetHeader() []byte {
	hdr := make([]byte, netTestHeaderSize)
	// All fields zero (no checksum offload, no GSO)
	return hdr
}

// sendPacket sends a packet through the TX queue
func (c *testNetClient) sendPacket(data []byte) error {
	// Calculate buffer slot using modulo to wrap around
	slot := c.unique % 64
	c.unique++
	bufAddr := c.bufAddr + slot*0x10000

	// Write virtio-net header + packet data
	hdr := makeVirtioNetHeader()
	fullPacket := append(hdr, data...)
	c.txQueue.writeBuffer(bufAddr, fullPacket)

	// Track expected used index before this request
	expectedUsedIdx := c.txQueue.availIdx

	// Allocate descriptor with wrapping
	descIdx := c.txQueue.nextDescIdx % c.txQueue.queueSize
	c.txQueue.writeDescriptor(descIdx, bufAddr, uint32(len(fullPacket)), 0, 0)
	c.txQueue.nextDescIdx++

	// Add to available ring
	c.txQueue.addAvailableBuffer(descIdx)

	// Notify device
	c.mmio.writeReg(VIRTIO_MMIO_QUEUE_NOTIFY, 1) // TX queue

	// Wait for completion
	usedIdx := c.txQueue.getUsedIdx()
	if int16(usedIdx-expectedUsedIdx) <= 0 {
		return fmt.Errorf("no TX completion (usedIdx=%d, expected>%d)", usedIdx, expectedUsedIdx)
	}

	return nil
}

// provideRxBuffer adds a buffer to the RX queue for receiving packets
func (c *testNetClient) provideRxBuffer(bufSize uint32) (bufAddr uint64, descIdx uint16) {
	// Calculate buffer slot
	slot := c.unique % 64
	c.unique++
	bufAddr = c.bufAddr + slot*0x10000 + 0x8000 // Use second half of slot for RX

	// Allocate descriptor with wrapping (writable for device to fill)
	descIdx = c.rxQueue.nextDescIdx % c.rxQueue.queueSize
	c.rxQueue.writeDescriptor(descIdx, bufAddr, bufSize, netVirtqDescFWrite, 0)
	c.rxQueue.nextDescIdx++

	// Add to available ring
	c.rxQueue.addAvailableBuffer(descIdx)

	return bufAddr, descIdx
}

// notifyRx notifies the device of new RX buffers
func (c *testNetClient) notifyRx() {
	c.mmio.writeReg(VIRTIO_MMIO_QUEUE_NOTIFY, 0) // RX queue
}

// receivePacket receives a packet from the RX queue, polling with timeout
func (c *testNetClient) receivePacket(expectedUsedIdx uint16) ([]byte, error) {
	// Poll for packet with timeout (for async loopback)
	for i := 0; i < 1000; i++ {
		usedIdx := c.rxQueue.getUsedIdx()
		if int16(usedIdx-expectedUsedIdx) > 0 {
			// Get used entry
			_, usedLen := c.rxQueue.getUsedEntry(expectedUsedIdx % c.rxQueue.queueSize)

			// Read buffer (we need to find the buffer address from the descriptor)
			// For simplicity, calculate based on the slot pattern
			slot := (uint64(expectedUsedIdx) % 64)
			bufAddr := c.bufAddr + slot*0x10000 + 0x8000

			data := c.rxQueue.readBuffer(bufAddr, usedLen)

			// Skip virtio-net header
			if len(data) < netTestHeaderSize {
				return nil, fmt.Errorf("packet too short: %d", len(data))
			}

			return data[netTestHeaderSize:], nil
		}
		// Small sleep for async operations
		time.Sleep(100 * time.Microsecond)
	}
	return nil, fmt.Errorf("timeout waiting for RX packet (usedIdx=%d, expected>%d)", c.rxQueue.getUsedIdx(), expectedUsedIdx)
}

// -----------------------------------------------------------------------------
// Unit Tests
// -----------------------------------------------------------------------------

func TestNetInitDevice(t *testing.T) {
	backend := &countingNetBackend{}
	_, client := createNetTestSetup(t, backend)

	// Verify device is initialized
	status := client.mmio.readReg(VIRTIO_MMIO_STATUS)
	if status != 15 {
		t.Fatalf("expected status 15 (DRIVER_OK), got %d", status)
	}
}

func TestNetTxSinglePacket(t *testing.T) {
	backend := &countingNetBackend{}
	_, client := createNetTestSetup(t, backend)

	// Create test packet (Ethernet frame)
	packet := make([]byte, 64)
	// Destination MAC
	copy(packet[0:6], []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff})
	// Source MAC
	copy(packet[6:12], []byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x01})
	// EtherType (IPv4)
	binary.BigEndian.PutUint16(packet[12:14], 0x0800)
	// Payload
	copy(packet[14:], []byte("Hello, World!"))

	err := client.sendPacket(packet)
	if err != nil {
		t.Fatalf("sendPacket failed: %v", err)
	}

	// Verify backend received packet
	if backend.getTxCount() != 1 {
		t.Fatalf("expected 1 TX packet, got %d", backend.getTxCount())
	}
	if backend.getTxBytes() != uint64(len(packet)) {
		t.Fatalf("expected %d TX bytes, got %d", len(packet), backend.getTxBytes())
	}
}

func TestNetTxMultiplePackets(t *testing.T) {
	backend := &countingNetBackend{}
	_, client := createNetTestSetup(t, backend)

	numPackets := 100
	packetSize := 64

	for i := 0; i < numPackets; i++ {
		packet := make([]byte, packetSize)
		// Simple Ethernet frame
		copy(packet[0:6], []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff})
		copy(packet[6:12], []byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x01})
		binary.BigEndian.PutUint16(packet[12:14], 0x0800)
		packet[14] = byte(i)

		err := client.sendPacket(packet)
		if err != nil {
			t.Fatalf("sendPacket %d failed: %v", i, err)
		}
	}

	if backend.getTxCount() != uint64(numPackets) {
		t.Fatalf("expected %d TX packets, got %d", numPackets, backend.getTxCount())
	}
}

func TestNetRxSinglePacket(t *testing.T) {
	backend := &countingNetBackend{}
	netDev, client := createNetTestSetup(t, backend)

	// Provide RX buffer
	expectedUsedIdx := client.rxQueue.availIdx
	client.provideRxBuffer(2048)
	client.notifyRx()

	// Inject packet from backend
	packet := make([]byte, 64)
	copy(packet[0:6], []byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x01}) // Dst MAC
	copy(packet[6:12], []byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x02}) // Src MAC
	binary.BigEndian.PutUint16(packet[12:14], 0x0800)
	copy(packet[14:], []byte("Test packet"))

	netDev.EnqueueRxPacket(packet)

	// Receive packet
	rxPacket, err := client.receivePacket(expectedUsedIdx)
	if err != nil {
		t.Fatalf("receivePacket failed: %v", err)
	}

	if !bytes.Equal(rxPacket, packet) {
		t.Fatalf("received packet mismatch: got %v, want %v", rxPacket, packet)
	}
}

func TestNetLoopback(t *testing.T) {
	backend := &loopbackNetBackend{}
	_, client := createNetTestSetup(t, backend)

	// Provide RX buffer first
	expectedUsedIdx := client.rxQueue.availIdx
	client.provideRxBuffer(2048)
	client.notifyRx()

	// Send packet (will be echoed back)
	packet := make([]byte, 64)
	copy(packet[0:6], []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff})
	copy(packet[6:12], []byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x01})
	binary.BigEndian.PutUint16(packet[12:14], 0x0800)
	copy(packet[14:], []byte("Loopback test"))

	err := client.sendPacket(packet)
	if err != nil {
		t.Fatalf("sendPacket failed: %v", err)
	}

	// Receive echoed packet
	rxPacket, err := client.receivePacket(expectedUsedIdx)
	if err != nil {
		t.Fatalf("receivePacket failed: %v", err)
	}

	if !bytes.Equal(rxPacket, packet) {
		t.Fatalf("loopback packet mismatch: got %v, want %v", rxPacket, packet)
	}
}

// -----------------------------------------------------------------------------
// Benchmarks
// -----------------------------------------------------------------------------

func benchmarkNetTxThroughput(b *testing.B, packetSize int) {
	backend := &countingNetBackend{}
	_, client := createNetTestSetup(b, backend)

	// Create test packet
	packet := make([]byte, packetSize)
	copy(packet[0:6], []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff})
	copy(packet[6:12], []byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x01})
	binary.BigEndian.PutUint16(packet[12:14], 0x0800)

	b.SetBytes(int64(packetSize))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if err := client.sendPacket(packet); err != nil {
			b.Fatalf("sendPacket failed: %v", err)
		}
	}

	b.StopTimer()
}

func BenchmarkNetTxThroughput64B(b *testing.B) {
	benchmarkNetTxThroughput(b, 64)
}

func BenchmarkNetTxThroughput512B(b *testing.B) {
	benchmarkNetTxThroughput(b, 512)
}

func BenchmarkNetTxThroughput1500B(b *testing.B) {
	benchmarkNetTxThroughput(b, 1500)
}

func BenchmarkNetTxThroughput9000B(b *testing.B) {
	benchmarkNetTxThroughput(b, 9000)
}

func benchmarkNetRxThroughput(b *testing.B, packetSize int) {
	backend := &countingNetBackend{}
	netDev, client := createNetTestSetup(b, backend)

	// Create test packet
	packet := make([]byte, packetSize)
	copy(packet[0:6], []byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x01})
	copy(packet[6:12], []byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x02})
	binary.BigEndian.PutUint16(packet[12:14], 0x0800)

	b.SetBytes(int64(packetSize))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// Provide RX buffer
		expectedUsedIdx := client.rxQueue.availIdx
		client.provideRxBuffer(uint32(packetSize + netTestHeaderSize + 100))
		client.notifyRx()

		// Inject packet
		netDev.EnqueueRxPacket(packet)

		// Receive packet
		_, err := client.receivePacket(expectedUsedIdx)
		if err != nil {
			b.Fatalf("receivePacket failed: %v", err)
		}
	}

	b.StopTimer()
}

func BenchmarkNetRxThroughput64B(b *testing.B) {
	benchmarkNetRxThroughput(b, 64)
}

func BenchmarkNetRxThroughput512B(b *testing.B) {
	benchmarkNetRxThroughput(b, 512)
}

func BenchmarkNetRxThroughput1500B(b *testing.B) {
	benchmarkNetRxThroughput(b, 1500)
}

func BenchmarkNetRxThroughput9000B(b *testing.B) {
	benchmarkNetRxThroughput(b, 9000)
}

func BenchmarkNetTxPPS(b *testing.B) {
	backend := &countingNetBackend{}
	_, client := createNetTestSetup(b, backend)

	// Minimum Ethernet frame size
	packet := make([]byte, 64)
	copy(packet[0:6], []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff})
	copy(packet[6:12], []byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x01})
	binary.BigEndian.PutUint16(packet[12:14], 0x0800)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if err := client.sendPacket(packet); err != nil {
			b.Fatalf("sendPacket failed: %v", err)
		}
	}

	b.StopTimer()
	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "pps")
}

func BenchmarkNetRxPPS(b *testing.B) {
	backend := &countingNetBackend{}
	netDev, client := createNetTestSetup(b, backend)

	packet := make([]byte, 64)
	copy(packet[0:6], []byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x01})
	copy(packet[6:12], []byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x02})
	binary.BigEndian.PutUint16(packet[12:14], 0x0800)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		expectedUsedIdx := client.rxQueue.availIdx
		client.provideRxBuffer(2048)
		client.notifyRx()

		netDev.EnqueueRxPacket(packet)

		_, err := client.receivePacket(expectedUsedIdx)
		if err != nil {
			b.Fatalf("receivePacket failed: %v", err)
		}
	}

	b.StopTimer()
	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "pps")
}

func benchmarkNetLoopback(b *testing.B, packetSize int) {
	backend := &loopbackNetBackend{}
	_, client := createNetTestSetup(b, backend)

	packet := make([]byte, packetSize)
	copy(packet[0:6], []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff})
	copy(packet[6:12], []byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x01})
	binary.BigEndian.PutUint16(packet[12:14], 0x0800)

	b.SetBytes(int64(packetSize) * 2) // TX + RX
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// Provide RX buffer first
		expectedUsedIdx := client.rxQueue.availIdx
		client.provideRxBuffer(uint32(packetSize + netTestHeaderSize + 100))
		client.notifyRx()

		// Send packet (will be echoed back)
		if err := client.sendPacket(packet); err != nil {
			b.Fatalf("sendPacket failed: %v", err)
		}

		// Receive echoed packet
		_, err := client.receivePacket(expectedUsedIdx)
		if err != nil {
			b.Fatalf("receivePacket failed: %v", err)
		}
	}

	b.StopTimer()
}

func BenchmarkNetLoopback64B(b *testing.B) {
	benchmarkNetLoopback(b, 64)
}

func BenchmarkNetLoopback1500B(b *testing.B) {
	benchmarkNetLoopback(b, 1500)
}

func BenchmarkNetTxBatch(b *testing.B) {
	backend := &countingNetBackend{}
	_, client := createNetTestSetup(b, backend)

	batchSize := 16
	packetSize := 64

	packets := make([][]byte, batchSize)
	for i := 0; i < batchSize; i++ {
		packets[i] = make([]byte, packetSize)
		copy(packets[i][0:6], []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff})
		copy(packets[i][6:12], []byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x01})
		binary.BigEndian.PutUint16(packets[i][12:14], 0x0800)
		packets[i][14] = byte(i)
	}

	b.SetBytes(int64(packetSize * batchSize))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		for _, pkt := range packets {
			if err := client.sendPacket(pkt); err != nil {
				b.Fatalf("sendPacket failed: %v", err)
			}
		}
	}

	b.StopTimer()
}
