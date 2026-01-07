package virtio

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/timeslice"
)

// consoleTestVM implements a minimal hv.VirtualMachine for console testing
type consoleTestVM struct {
	memory []byte
	irqs   map[uint32]bool
	mu     sync.Mutex
}

func newConsoleTestVM(memorySize int) *consoleTestVM {
	return &consoleTestVM{
		memory: make([]byte, memorySize),
		irqs:   make(map[uint32]bool),
	}
}

func (vm *consoleTestVM) ReadAt(p []byte, off int64) (int, error) {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	if off < 0 || int(off)+len(p) > len(vm.memory) {
		return 0, fmt.Errorf("read out of bounds: offset=%d len=%d memsize=%d", off, len(p), len(vm.memory))
	}
	copy(p, vm.memory[off:off+int64(len(p))])
	return len(p), nil
}

func (vm *consoleTestVM) WriteAt(p []byte, off int64) (int, error) {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	if off < 0 || int(off)+len(p) > len(vm.memory) {
		return 0, fmt.Errorf("write out of bounds: offset=%d len=%d memsize=%d", off, len(p), len(vm.memory))
	}
	copy(vm.memory[off:], p)
	return len(p), nil
}

func (vm *consoleTestVM) SetIRQ(line uint32, level bool) error {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	vm.irqs[line] = level
	return nil
}

func (vm *consoleTestVM) GetIRQ(line uint32) bool {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	return vm.irqs[line]
}

// Implement other required hv.VirtualMachine methods as no-ops
func (vm *consoleTestVM) Close() error                                   { return nil }
func (vm *consoleTestVM) Hypervisor() hv.Hypervisor                      { return nil }
func (vm *consoleTestVM) MemorySize() uint64                             { return uint64(len(vm.memory)) }
func (vm *consoleTestVM) MemoryBase() uint64                             { return 0 }
func (vm *consoleTestVM) Run(ctx context.Context, cfg hv.RunConfig) error { return nil }
func (vm *consoleTestVM) VirtualCPUCall(id int, f func(vcpu hv.VirtualCPU) error) error {
	return nil
}
func (vm *consoleTestVM) AddDevice(dev hv.Device) error { return nil }
func (vm *consoleTestVM) AddDeviceFromTemplate(template hv.DeviceTemplate) error {
	return nil
}
func (vm *consoleTestVM) AllocateMemory(physAddr, size uint64) (hv.MemoryRegion, error) {
	return nil, fmt.Errorf("not implemented")
}
func (vm *consoleTestVM) CaptureSnapshot() (hv.Snapshot, error)         { return nil, nil }
func (vm *consoleTestVM) RestoreSnapshot(snap hv.Snapshot) error        { return nil }

var _ hv.VirtualMachine = (*consoleTestVM)(nil)

// consoleTestExitContext implements hv.ExitContext for testing
type consoleTestExitContext struct{}

func (m consoleTestExitContext) SetExitTimeslice(kind timeslice.TimesliceID) {}

var _ hv.ExitContext = consoleTestExitContext{}

// Memory layout constants for tests
const (
	testMemorySize    = 16 * 1024 * 1024 // 16 MB
	testDescTableAddr = 0x100000         // 1 MB
	testAvailRingAddr = 0x101000         // Offset for available ring
	testUsedRingAddr  = 0x102000         // Offset for used ring
	testBufferAddr    = 0x200000         // 2 MB - data buffers
)

// consoleMMIOHelper provides convenience methods for MMIO operations
type consoleMMIOHelper struct {
	console *Console
	ctx     hv.ExitContext
}

func newConsoleMMIOHelper(console *Console) *consoleMMIOHelper {
	return &consoleMMIOHelper{
		console: console,
		ctx:     consoleTestExitContext{},
	}
}

func (h *consoleMMIOHelper) readReg(offset uint64) uint32 {
	data := make([]byte, 4)
	if err := h.console.ReadMMIO(h.ctx, ConsoleDefaultMMIOBase+offset, data); err != nil {
		panic(fmt.Sprintf("readReg failed: %v", err))
	}
	return binary.LittleEndian.Uint32(data)
}

func (h *consoleMMIOHelper) writeReg(offset uint64, value uint32) {
	data := make([]byte, 4)
	binary.LittleEndian.PutUint32(data, value)
	if err := h.console.WriteMMIO(h.ctx, ConsoleDefaultMMIOBase+offset, data); err != nil {
		panic(fmt.Sprintf("writeReg failed: %v", err))
	}
}

// consoleVirtqueueSetup sets up a virtqueue in guest memory
type consoleVirtqueueSetup struct {
	vm            *consoleTestVM
	descTableAddr uint64
	availRingAddr uint64
	usedRingAddr  uint64
	queueSize     uint16
	nextDescIdx   uint16
	availIdx      uint16
}

func newConsoleVirtqueueSetup(vm *consoleTestVM, descTable, availRing, usedRing uint64, size uint16) *consoleVirtqueueSetup {
	return &consoleVirtqueueSetup{
		vm:            vm,
		descTableAddr: descTable,
		availRingAddr: availRing,
		usedRingAddr:  usedRing,
		queueSize:     size,
	}
}

func (vq *consoleVirtqueueSetup) writeUint16(addr uint64, val uint16) {
	var buf [2]byte
	binary.LittleEndian.PutUint16(buf[:], val)
	vq.vm.WriteAt(buf[:], int64(addr))
}

func (vq *consoleVirtqueueSetup) writeUint32(addr uint64, val uint32) {
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], val)
	vq.vm.WriteAt(buf[:], int64(addr))
}

func (vq *consoleVirtqueueSetup) writeUint64(addr uint64, val uint64) {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], val)
	vq.vm.WriteAt(buf[:], int64(addr))
}

func (vq *consoleVirtqueueSetup) readUint16(addr uint64) uint16 {
	var buf [2]byte
	vq.vm.ReadAt(buf[:], int64(addr))
	return binary.LittleEndian.Uint16(buf[:])
}

func (vq *consoleVirtqueueSetup) readUint32(addr uint64) uint32 {
	var buf [4]byte
	vq.vm.ReadAt(buf[:], int64(addr))
	return binary.LittleEndian.Uint32(buf[:])
}

// initRings initializes the available and used rings
func (vq *consoleVirtqueueSetup) initRings() {
	// Initialize available ring header: flags=0, idx=0
	vq.writeUint16(vq.availRingAddr+0, 0)
	vq.writeUint16(vq.availRingAddr+2, 0)

	// Initialize used ring header: flags=0, idx=0
	vq.writeUint16(vq.usedRingAddr+0, 0)
	vq.writeUint16(vq.usedRingAddr+2, 0)
}

// writeDescriptor writes a descriptor to the descriptor table
func (vq *consoleVirtqueueSetup) writeDescriptor(idx uint16, addr uint64, length uint32, flags uint16, next uint16) {
	base := vq.descTableAddr + uint64(idx)*16
	vq.writeUint64(base+0, addr)
	vq.writeUint32(base+8, length)
	vq.writeUint16(base+12, flags)
	vq.writeUint16(base+14, next)
}

// addAvailableBuffer adds a buffer to the available ring
func (vq *consoleVirtqueueSetup) addAvailableBuffer(descIdx uint16) {
	ringIdx := vq.availIdx % vq.queueSize
	vq.writeUint16(vq.availRingAddr+4+uint64(ringIdx)*2, descIdx)
	vq.availIdx++
	vq.writeUint16(vq.availRingAddr+2, vq.availIdx)
}

// getUsedEntry reads a used ring entry
func (vq *consoleVirtqueueSetup) getUsedEntry(idx uint16) (head uint32, length uint32) {
	base := vq.usedRingAddr + 4 + uint64(idx)*8
	return vq.readUint32(base), vq.readUint32(base + 4)
}

// getUsedIdx reads the used ring index
func (vq *consoleVirtqueueSetup) getUsedIdx() uint16 {
	return vq.readUint16(vq.usedRingAddr + 2)
}

// allocDescriptor allocates a descriptor and returns its index
func (vq *consoleVirtqueueSetup) allocDescriptor(addr uint64, length uint32, flags uint16) uint16 {
	idx := vq.nextDescIdx
	vq.writeDescriptor(idx, addr, length, flags, 0)
	vq.nextDescIdx++
	return idx
}

// writeBuffer writes data to a buffer in guest memory
func (vq *consoleVirtqueueSetup) writeBuffer(addr uint64, data []byte) {
	vq.vm.WriteAt(data, int64(addr))
}

// readBuffer reads data from a buffer in guest memory
func (vq *consoleVirtqueueSetup) readBuffer(addr uint64, length uint32) []byte {
	buf := make([]byte, length)
	vq.vm.ReadAt(buf, int64(addr))
	return buf
}

// initializeConsoleDevice performs the standard virtio device initialization sequence
func initializeConsoleDevice(t *testing.T, mmio *consoleMMIOHelper, rxQueue, txQueue *consoleVirtqueueSetup) {
	t.Helper()

	// Step 1: Check magic value
	magic := mmio.readReg(VIRTIO_MMIO_MAGIC_VALUE)
	if magic != 0x74726976 {
		t.Fatalf("invalid magic value: got 0x%x, want 0x74726976", magic)
	}

	// Step 2: Check version
	version := mmio.readReg(VIRTIO_MMIO_VERSION)
	if version != 2 {
		t.Fatalf("invalid version: got %d, want 2", version)
	}

	// Step 3: Check device ID (console = 3)
	deviceID := mmio.readReg(VIRTIO_MMIO_DEVICE_ID)
	if deviceID != 3 {
		t.Fatalf("invalid device ID: got %d, want 3", deviceID)
	}

	// Step 4: Reset device (write 0 to status)
	mmio.writeReg(VIRTIO_MMIO_STATUS, 0)

	// Step 5: Set ACKNOWLEDGE status bit
	mmio.writeReg(VIRTIO_MMIO_STATUS, 1)

	// Step 6: Set DRIVER status bit
	mmio.writeReg(VIRTIO_MMIO_STATUS, 1|2)

	// Step 7: Read device features
	mmio.writeReg(VIRTIO_MMIO_DEVICE_FEATURES_SEL, 0)
	featuresLow := mmio.readReg(VIRTIO_MMIO_DEVICE_FEATURES)
	mmio.writeReg(VIRTIO_MMIO_DEVICE_FEATURES_SEL, 1)
	featuresHigh := mmio.readReg(VIRTIO_MMIO_DEVICE_FEATURES)

	// Step 8: Write accepted features (accept all)
	mmio.writeReg(VIRTIO_MMIO_DRIVER_FEATURES_SEL, 0)
	mmio.writeReg(VIRTIO_MMIO_DRIVER_FEATURES, featuresLow)
	mmio.writeReg(VIRTIO_MMIO_DRIVER_FEATURES_SEL, 1)
	mmio.writeReg(VIRTIO_MMIO_DRIVER_FEATURES, featuresHigh)

	// Step 9: Set FEATURES_OK status bit
	mmio.writeReg(VIRTIO_MMIO_STATUS, 1|2|8)

	// Step 10: Configure queues
	// Configure receive queue (queue 0)
	mmio.writeReg(VIRTIO_MMIO_QUEUE_SEL, 0)
	maxSize := mmio.readReg(VIRTIO_MMIO_QUEUE_NUM_MAX)
	if maxSize == 0 {
		t.Fatal("queue 0 max size is 0")
	}
	mmio.writeReg(VIRTIO_MMIO_QUEUE_NUM, uint32(rxQueue.queueSize))
	mmio.writeReg(VIRTIO_MMIO_QUEUE_DESC_LOW, uint32(rxQueue.descTableAddr))
	mmio.writeReg(VIRTIO_MMIO_QUEUE_DESC_HIGH, uint32(rxQueue.descTableAddr>>32))
	mmio.writeReg(VIRTIO_MMIO_QUEUE_AVAIL_LOW, uint32(rxQueue.availRingAddr))
	mmio.writeReg(VIRTIO_MMIO_QUEUE_AVAIL_HIGH, uint32(rxQueue.availRingAddr>>32))
	mmio.writeReg(VIRTIO_MMIO_QUEUE_USED_LOW, uint32(rxQueue.usedRingAddr))
	mmio.writeReg(VIRTIO_MMIO_QUEUE_USED_HIGH, uint32(rxQueue.usedRingAddr>>32))
	mmio.writeReg(VIRTIO_MMIO_QUEUE_READY, 1)

	// Configure transmit queue (queue 1)
	mmio.writeReg(VIRTIO_MMIO_QUEUE_SEL, 1)
	maxSize = mmio.readReg(VIRTIO_MMIO_QUEUE_NUM_MAX)
	if maxSize == 0 {
		t.Fatal("queue 1 max size is 0")
	}
	mmio.writeReg(VIRTIO_MMIO_QUEUE_NUM, uint32(txQueue.queueSize))
	mmio.writeReg(VIRTIO_MMIO_QUEUE_DESC_LOW, uint32(txQueue.descTableAddr))
	mmio.writeReg(VIRTIO_MMIO_QUEUE_DESC_HIGH, uint32(txQueue.descTableAddr>>32))
	mmio.writeReg(VIRTIO_MMIO_QUEUE_AVAIL_LOW, uint32(txQueue.availRingAddr))
	mmio.writeReg(VIRTIO_MMIO_QUEUE_AVAIL_HIGH, uint32(txQueue.availRingAddr>>32))
	mmio.writeReg(VIRTIO_MMIO_QUEUE_USED_LOW, uint32(txQueue.usedRingAddr))
	mmio.writeReg(VIRTIO_MMIO_QUEUE_USED_HIGH, uint32(txQueue.usedRingAddr>>32))
	mmio.writeReg(VIRTIO_MMIO_QUEUE_READY, 1)

	// Step 11: Set DRIVER_OK status bit
	mmio.writeReg(VIRTIO_MMIO_STATUS, 1|2|4|8)
}

// TestConsoleMMIODeviceIdentification tests that MMIO device identification works
func TestConsoleMMIODeviceIdentification(t *testing.T) {
	vm := newConsoleTestVM(testMemorySize)
	output := &bytes.Buffer{}
	console := NewConsole(vm, ConsoleDefaultMMIOBase, ConsoleDefaultMMIOSize, ConsoleDefaultIRQLine, output, nil)
	mmio := newConsoleMMIOHelper(console)

	// Test magic value
	magic := mmio.readReg(VIRTIO_MMIO_MAGIC_VALUE)
	if magic != 0x74726976 { // "virt" in little endian
		t.Errorf("magic value: got 0x%x, want 0x74726976", magic)
	}

	// Test version (modern virtio = 2)
	version := mmio.readReg(VIRTIO_MMIO_VERSION)
	if version != 2 {
		t.Errorf("version: got %d, want 2", version)
	}

	// Test device ID (console = 3)
	deviceID := mmio.readReg(VIRTIO_MMIO_DEVICE_ID)
	if deviceID != 3 {
		t.Errorf("device ID: got %d, want 3", deviceID)
	}

	// Test vendor ID
	vendorID := mmio.readReg(VIRTIO_MMIO_VENDOR_ID)
	if vendorID != 0x554d4551 { // "QEMU"
		t.Errorf("vendor ID: got 0x%x, want 0x554d4551", vendorID)
	}
}

// TestConsoleMMIOFeatureNegotiation tests feature negotiation via MMIO
func TestConsoleMMIOFeatureNegotiation(t *testing.T) {
	vm := newConsoleTestVM(testMemorySize)
	output := &bytes.Buffer{}
	console := NewConsole(vm, ConsoleDefaultMMIOBase, ConsoleDefaultMMIOSize, ConsoleDefaultIRQLine, output, nil)
	mmio := newConsoleMMIOHelper(console)

	// Reset device
	mmio.writeReg(VIRTIO_MMIO_STATUS, 0)

	// Read device features (low word)
	mmio.writeReg(VIRTIO_MMIO_DEVICE_FEATURES_SEL, 0)
	featuresLow := mmio.readReg(VIRTIO_MMIO_DEVICE_FEATURES)

	// Check console size feature (bit 0)
	if featuresLow&consoleFeatureSize == 0 {
		t.Error("CONSOLE_F_SIZE feature not set in device features")
	}

	// Read device features (high word - should have VIRTIO_F_VERSION_1)
	mmio.writeReg(VIRTIO_MMIO_DEVICE_FEATURES_SEL, 1)
	featuresHigh := mmio.readReg(VIRTIO_MMIO_DEVICE_FEATURES)

	if featuresHigh&1 == 0 { // VIRTIO_F_VERSION_1 is bit 32, which is bit 0 of high word
		t.Error("VIRTIO_F_VERSION_1 feature not set in device features")
	}

	// Negotiate features - accept all
	mmio.writeReg(VIRTIO_MMIO_DRIVER_FEATURES_SEL, 0)
	mmio.writeReg(VIRTIO_MMIO_DRIVER_FEATURES, featuresLow)
	mmio.writeReg(VIRTIO_MMIO_DRIVER_FEATURES_SEL, 1)
	mmio.writeReg(VIRTIO_MMIO_DRIVER_FEATURES, featuresHigh)

	// Read back negotiated features
	mmio.writeReg(VIRTIO_MMIO_DRIVER_FEATURES_SEL, 0)
	driverFeaturesLow := mmio.readReg(VIRTIO_MMIO_DRIVER_FEATURES)
	if driverFeaturesLow != featuresLow {
		t.Errorf("driver features low: got 0x%x, want 0x%x", driverFeaturesLow, featuresLow)
	}
}

// TestConsoleMMIOQueueConfiguration tests queue configuration via MMIO
func TestConsoleMMIOQueueConfiguration(t *testing.T) {
	vm := newConsoleTestVM(testMemorySize)
	output := &bytes.Buffer{}
	console := NewConsole(vm, ConsoleDefaultMMIOBase, ConsoleDefaultMMIOSize, ConsoleDefaultIRQLine, output, nil)
	mmio := newConsoleMMIOHelper(console)

	// Reset device
	mmio.writeReg(VIRTIO_MMIO_STATUS, 0)

	// Select queue 0 (receive queue)
	mmio.writeReg(VIRTIO_MMIO_QUEUE_SEL, 0)

	// Check max queue size
	maxSize := mmio.readReg(VIRTIO_MMIO_QUEUE_NUM_MAX)
	if maxSize != consoleQueueNumMax {
		t.Errorf("queue 0 max size: got %d, want %d", maxSize, consoleQueueNumMax)
	}

	// Select queue 1 (transmit queue)
	mmio.writeReg(VIRTIO_MMIO_QUEUE_SEL, 1)
	maxSize = mmio.readReg(VIRTIO_MMIO_QUEUE_NUM_MAX)
	if maxSize != consoleQueueNumMax {
		t.Errorf("queue 1 max size: got %d, want %d", maxSize, consoleQueueNumMax)
	}

	// Test queue address configuration
	testDescAddr := uint64(0x1000)
	testAvailAddr := uint64(0x2000)
	testUsedAddr := uint64(0x3000)

	mmio.writeReg(VIRTIO_MMIO_QUEUE_SEL, 0)
	mmio.writeReg(VIRTIO_MMIO_QUEUE_NUM, 64)
	mmio.writeReg(VIRTIO_MMIO_QUEUE_DESC_LOW, uint32(testDescAddr))
	mmio.writeReg(VIRTIO_MMIO_QUEUE_DESC_HIGH, uint32(testDescAddr>>32))
	mmio.writeReg(VIRTIO_MMIO_QUEUE_AVAIL_LOW, uint32(testAvailAddr))
	mmio.writeReg(VIRTIO_MMIO_QUEUE_AVAIL_HIGH, uint32(testAvailAddr>>32))
	mmio.writeReg(VIRTIO_MMIO_QUEUE_USED_LOW, uint32(testUsedAddr))
	mmio.writeReg(VIRTIO_MMIO_QUEUE_USED_HIGH, uint32(testUsedAddr>>32))

	// Read back configured values
	gotDescLow := mmio.readReg(VIRTIO_MMIO_QUEUE_DESC_LOW)
	gotDescHigh := mmio.readReg(VIRTIO_MMIO_QUEUE_DESC_HIGH)
	gotDesc := uint64(gotDescLow) | (uint64(gotDescHigh) << 32)
	if gotDesc != testDescAddr {
		t.Errorf("queue desc addr: got 0x%x, want 0x%x", gotDesc, testDescAddr)
	}

	gotSize := mmio.readReg(VIRTIO_MMIO_QUEUE_NUM)
	if gotSize != 64 {
		t.Errorf("queue size: got %d, want 64", gotSize)
	}
}

// TestConsoleTransmitQueue tests sending data from guest to host via transmit queue
func TestConsoleTransmitQueue(t *testing.T) {
	vm := newConsoleTestVM(testMemorySize)
	output := &bytes.Buffer{}
	console := NewConsole(vm, ConsoleDefaultMMIOBase, ConsoleDefaultMMIOSize, ConsoleDefaultIRQLine, output, nil)
	mmio := newConsoleMMIOHelper(console)

	// Set up queue memory regions
	rxDescTable := uint64(testDescTableAddr)
	rxAvailRing := uint64(testAvailRingAddr)
	rxUsedRing := uint64(testUsedRingAddr)
	txDescTable := uint64(testDescTableAddr + 0x10000)
	txAvailRing := uint64(testAvailRingAddr + 0x10000)
	txUsedRing := uint64(testUsedRingAddr + 0x10000)

	rxQueue := newConsoleVirtqueueSetup(vm, rxDescTable, rxAvailRing, rxUsedRing, 64)
	txQueue := newConsoleVirtqueueSetup(vm, txDescTable, txAvailRing, txUsedRing, 64)

	rxQueue.initRings()
	txQueue.initRings()

	// Initialize the device
	initializeConsoleDevice(t, mmio, rxQueue, txQueue)

	// Prepare test data
	testData := []byte("Hello, Virtio Console!")
	bufferAddr := uint64(testBufferAddr)
	txQueue.writeBuffer(bufferAddr, testData)

	// Create a descriptor for the transmit data (read-only from device perspective)
	descIdx := txQueue.allocDescriptor(bufferAddr, uint32(len(testData)), 0) // flags=0 means read-only

	// Add to available ring
	txQueue.addAvailableBuffer(descIdx)

	// Notify the device (write to queue notify register)
	mmio.writeReg(VIRTIO_MMIO_QUEUE_NOTIFY, 1) // Queue 1 is transmit

	// Check that data was received by the host
	if output.String() != string(testData) {
		t.Errorf("transmitted data: got %q, want %q", output.String(), string(testData))
	}

	// Verify used ring was updated
	usedIdx := txQueue.getUsedIdx()
	if usedIdx != 1 {
		t.Errorf("used index: got %d, want 1", usedIdx)
	}

	head, length := txQueue.getUsedEntry(0)
	if head != uint32(descIdx) {
		t.Errorf("used entry head: got %d, want %d", head, descIdx)
	}
	if length != uint32(len(testData)) {
		t.Errorf("used entry length: got %d, want %d", length, len(testData))
	}
}

// TestConsoleTransmitChainedDescriptors tests transmit with chained descriptors
func TestConsoleTransmitChainedDescriptors(t *testing.T) {
	vm := newConsoleTestVM(testMemorySize)
	output := &bytes.Buffer{}
	console := NewConsole(vm, ConsoleDefaultMMIOBase, ConsoleDefaultMMIOSize, ConsoleDefaultIRQLine, output, nil)
	mmio := newConsoleMMIOHelper(console)

	// Set up queue memory regions
	rxDescTable := uint64(testDescTableAddr)
	rxAvailRing := uint64(testAvailRingAddr)
	rxUsedRing := uint64(testUsedRingAddr)
	txDescTable := uint64(testDescTableAddr + 0x10000)
	txAvailRing := uint64(testAvailRingAddr + 0x10000)
	txUsedRing := uint64(testUsedRingAddr + 0x10000)

	rxQueue := newConsoleVirtqueueSetup(vm, rxDescTable, rxAvailRing, rxUsedRing, 64)
	txQueue := newConsoleVirtqueueSetup(vm, txDescTable, txAvailRing, txUsedRing, 64)

	rxQueue.initRings()
	txQueue.initRings()

	initializeConsoleDevice(t, mmio, rxQueue, txQueue)

	// Prepare test data in multiple buffers
	data1 := []byte("First ")
	data2 := []byte("Second ")
	data3 := []byte("Third")

	buf1Addr := uint64(testBufferAddr)
	buf2Addr := uint64(testBufferAddr + 0x1000)
	buf3Addr := uint64(testBufferAddr + 0x2000)

	txQueue.writeBuffer(buf1Addr, data1)
	txQueue.writeBuffer(buf2Addr, data2)
	txQueue.writeBuffer(buf3Addr, data3)

	// Create chained descriptors: 0 -> 1 -> 2
	txQueue.writeDescriptor(0, buf1Addr, uint32(len(data1)), testVirtqDescFNext, 1) // next=1
	txQueue.writeDescriptor(1, buf2Addr, uint32(len(data2)), testVirtqDescFNext, 2) // next=2
	txQueue.writeDescriptor(2, buf3Addr, uint32(len(data3)), 0, 0)                  // last

	// Add chain head to available ring
	txQueue.addAvailableBuffer(0)

	// Notify transmit queue
	mmio.writeReg(VIRTIO_MMIO_QUEUE_NOTIFY, 1)

	// Verify all data was transmitted in order
	expected := "First Second Third"
	if output.String() != expected {
		t.Errorf("chained transmit: got %q, want %q", output.String(), expected)
	}

	// Verify used ring - should have one entry for the chain
	usedIdx := txQueue.getUsedIdx()
	if usedIdx != 1 {
		t.Errorf("used index: got %d, want 1", usedIdx)
	}

	head, length := txQueue.getUsedEntry(0)
	if head != 0 {
		t.Errorf("used entry head: got %d, want 0", head)
	}
	expectedLen := uint32(len(data1) + len(data2) + len(data3))
	if length != expectedLen {
		t.Errorf("used entry length: got %d, want %d", length, expectedLen)
	}
}

// TestConsoleReceiveQueue tests receiving data from host to guest via receive queue
func TestConsoleReceiveQueue(t *testing.T) {
	vm := newConsoleTestVM(testMemorySize)
	output := &bytes.Buffer{}

	// Create a pipe for input
	inputReader, inputWriter := io.Pipe()
	defer inputWriter.Close()

	console := NewConsole(vm, ConsoleDefaultMMIOBase, ConsoleDefaultMMIOSize, ConsoleDefaultIRQLine, output, inputReader)
	mmio := newConsoleMMIOHelper(console)

	// Set up queue memory regions
	rxDescTable := uint64(testDescTableAddr)
	rxAvailRing := uint64(testAvailRingAddr)
	rxUsedRing := uint64(testUsedRingAddr)
	txDescTable := uint64(testDescTableAddr + 0x10000)
	txAvailRing := uint64(testAvailRingAddr + 0x10000)
	txUsedRing := uint64(testUsedRingAddr + 0x10000)

	rxQueue := newConsoleVirtqueueSetup(vm, rxDescTable, rxAvailRing, rxUsedRing, 64)
	txQueue := newConsoleVirtqueueSetup(vm, txDescTable, txAvailRing, txUsedRing, 64)

	rxQueue.initRings()
	txQueue.initRings()

	initializeConsoleDevice(t, mmio, rxQueue, txQueue)

	// Prepare receive buffers (writable descriptors)
	bufferAddr := uint64(testBufferAddr)
	bufferSize := uint32(256)

	// Create a write-only descriptor for receiving data
	descIdx := rxQueue.allocDescriptor(bufferAddr, bufferSize, testVirtqDescFWrite)
	rxQueue.addAvailableBuffer(descIdx)

	// Notify receive queue that buffers are available
	mmio.writeReg(VIRTIO_MMIO_QUEUE_NOTIFY, 0) // Queue 0 is receive

	// Write data to input (simulating host sending data)
	testInput := []byte("Input from host")
	go func() {
		inputWriter.Write(testInput)
	}()

	// Wait for data to be processed
	time.Sleep(200 * time.Millisecond)

	// Trigger receive queue processing by notifying
	mmio.writeReg(VIRTIO_MMIO_QUEUE_NOTIFY, 0)

	// Allow time for processing
	time.Sleep(100 * time.Millisecond)

	// Check if data was written to guest buffer
	receivedData := rxQueue.readBuffer(bufferAddr, uint32(len(testInput)))
	if !bytes.Equal(receivedData, testInput) {
		t.Errorf("received data: got %q, want %q", receivedData, testInput)
	}
}

// TestConsoleReceiveWithPendingData tests that pending input is delivered when buffers become available
func TestConsoleReceiveWithPendingData(t *testing.T) {
	vm := newConsoleTestVM(testMemorySize)
	output := &bytes.Buffer{}

	console := NewConsole(vm, ConsoleDefaultMMIOBase, ConsoleDefaultMMIOSize, ConsoleDefaultIRQLine, output, nil)
	mmio := newConsoleMMIOHelper(console)

	// Enqueue input data before device is fully initialized
	testInput := []byte("Pending input data")
	console.enqueueInput(testInput)

	// Set up queue memory regions
	rxDescTable := uint64(testDescTableAddr)
	rxAvailRing := uint64(testAvailRingAddr)
	rxUsedRing := uint64(testUsedRingAddr)
	txDescTable := uint64(testDescTableAddr + 0x10000)
	txAvailRing := uint64(testAvailRingAddr + 0x10000)
	txUsedRing := uint64(testUsedRingAddr + 0x10000)

	rxQueue := newConsoleVirtqueueSetup(vm, rxDescTable, rxAvailRing, rxUsedRing, 64)
	txQueue := newConsoleVirtqueueSetup(vm, txDescTable, txAvailRing, txUsedRing, 64)

	rxQueue.initRings()
	txQueue.initRings()

	initializeConsoleDevice(t, mmio, rxQueue, txQueue)

	// Now provide receive buffers
	bufferAddr := uint64(testBufferAddr)
	bufferSize := uint32(256)
	descIdx := rxQueue.allocDescriptor(bufferAddr, bufferSize, testVirtqDescFWrite)
	rxQueue.addAvailableBuffer(descIdx)

	// Notify receive queue - this should deliver pending data
	mmio.writeReg(VIRTIO_MMIO_QUEUE_NOTIFY, 0)

	// Check used ring
	usedIdx := rxQueue.getUsedIdx()
	if usedIdx != 1 {
		t.Errorf("used index: got %d, want 1", usedIdx)
	}

	// Verify data in guest buffer
	receivedData := rxQueue.readBuffer(bufferAddr, uint32(len(testInput)))
	if !bytes.Equal(receivedData, testInput) {
		t.Errorf("received data: got %q, want %q", receivedData, testInput)
	}

	head, length := rxQueue.getUsedEntry(0)
	if head != uint32(descIdx) {
		t.Errorf("used entry head: got %d, want %d", head, descIdx)
	}
	if length != uint32(len(testInput)) {
		t.Errorf("used entry length: got %d, want %d", length, len(testInput))
	}
}

// TestConsoleConfigSpace tests reading the console configuration space
func TestConsoleConfigSpace(t *testing.T) {
	vm := newConsoleTestVM(testMemorySize)
	output := &bytes.Buffer{}
	console := NewConsole(vm, ConsoleDefaultMMIOBase, ConsoleDefaultMMIOSize, ConsoleDefaultIRQLine, output, nil)
	mmio := newConsoleMMIOHelper(console)

	// Read cols (offset 0x100)
	cols := mmio.readReg(VIRTIO_MMIO_CONFIG + 0)
	if cols&0xFFFF != 80 { // default cols
		t.Errorf("cols: got %d, want 80", cols&0xFFFF)
	}

	// Read rows (should be in the same word)
	rows := (cols >> 16) & 0xFFFF
	if rows != 25 { // default rows
		t.Errorf("rows: got %d, want 25", rows)
	}

	// Test SetSize
	console.SetSize(120, 40)

	cols = mmio.readReg(VIRTIO_MMIO_CONFIG + 0)
	if cols&0xFFFF != 120 {
		t.Errorf("cols after SetSize: got %d, want 120", cols&0xFFFF)
	}
	rows = (cols >> 16) & 0xFFFF
	if rows != 40 {
		t.Errorf("rows after SetSize: got %d, want 40", rows)
	}
}

// TestConsoleInterrupts tests interrupt generation
func TestConsoleInterrupts(t *testing.T) {
	vm := newConsoleTestVM(testMemorySize)
	output := &bytes.Buffer{}
	console := NewConsole(vm, ConsoleDefaultMMIOBase, ConsoleDefaultMMIOSize, ConsoleDefaultIRQLine, output, nil)
	mmio := newConsoleMMIOHelper(console)

	// Set up queue memory regions
	rxDescTable := uint64(testDescTableAddr)
	rxAvailRing := uint64(testAvailRingAddr)
	rxUsedRing := uint64(testUsedRingAddr)
	txDescTable := uint64(testDescTableAddr + 0x10000)
	txAvailRing := uint64(testAvailRingAddr + 0x10000)
	txUsedRing := uint64(testUsedRingAddr + 0x10000)

	rxQueue := newConsoleVirtqueueSetup(vm, rxDescTable, rxAvailRing, rxUsedRing, 64)
	txQueue := newConsoleVirtqueueSetup(vm, txDescTable, txAvailRing, txUsedRing, 64)

	rxQueue.initRings()
	txQueue.initRings()

	initializeConsoleDevice(t, mmio, rxQueue, txQueue)

	// Clear any pending interrupts
	mmio.writeReg(VIRTIO_MMIO_INTERRUPT_ACK, 0xFFFFFFFF)

	// Check initial interrupt status
	intStatus := mmio.readReg(VIRTIO_MMIO_INTERRUPT_STATUS)
	if intStatus != 0 {
		t.Errorf("initial interrupt status: got 0x%x, want 0", intStatus)
	}

	// Transmit data to trigger interrupt
	testData := []byte("Test")
	bufferAddr := uint64(testBufferAddr)
	txQueue.writeBuffer(bufferAddr, testData)
	descIdx := txQueue.allocDescriptor(bufferAddr, uint32(len(testData)), 0)
	txQueue.addAvailableBuffer(descIdx)
	mmio.writeReg(VIRTIO_MMIO_QUEUE_NOTIFY, 1)

	// Check interrupt status (VIRTIO_MMIO_INT_VRING should be set)
	intStatus = mmio.readReg(VIRTIO_MMIO_INTERRUPT_STATUS)
	if intStatus&VIRTIO_MMIO_INT_VRING == 0 {
		t.Errorf("interrupt status after transmit: got 0x%x, expected VRING bit set", intStatus)
	}

	// Check IRQ line
	if !vm.GetIRQ(ConsoleDefaultIRQLine) {
		t.Error("IRQ line not asserted after transmit")
	}

	// Acknowledge interrupt
	mmio.writeReg(VIRTIO_MMIO_INTERRUPT_ACK, VIRTIO_MMIO_INT_VRING)

	// Check interrupt cleared
	intStatus = mmio.readReg(VIRTIO_MMIO_INTERRUPT_STATUS)
	if intStatus&VIRTIO_MMIO_INT_VRING != 0 {
		t.Errorf("interrupt status after ACK: got 0x%x, expected VRING bit cleared", intStatus)
	}
}

// TestConsoleMultipleTransmits tests multiple transmit operations
func TestConsoleMultipleTransmits(t *testing.T) {
	vm := newConsoleTestVM(testMemorySize)
	output := &bytes.Buffer{}
	console := NewConsole(vm, ConsoleDefaultMMIOBase, ConsoleDefaultMMIOSize, ConsoleDefaultIRQLine, output, nil)
	mmio := newConsoleMMIOHelper(console)

	// Set up queue memory regions
	rxDescTable := uint64(testDescTableAddr)
	rxAvailRing := uint64(testAvailRingAddr)
	rxUsedRing := uint64(testUsedRingAddr)
	txDescTable := uint64(testDescTableAddr + 0x10000)
	txAvailRing := uint64(testAvailRingAddr + 0x10000)
	txUsedRing := uint64(testUsedRingAddr + 0x10000)

	rxQueue := newConsoleVirtqueueSetup(vm, rxDescTable, rxAvailRing, rxUsedRing, 64)
	txQueue := newConsoleVirtqueueSetup(vm, txDescTable, txAvailRing, txUsedRing, 64)

	rxQueue.initRings()
	txQueue.initRings()

	initializeConsoleDevice(t, mmio, rxQueue, txQueue)

	// Prepare multiple buffers
	messages := []string{"Message 1\n", "Message 2\n", "Message 3\n"}
	baseAddr := uint64(testBufferAddr)

	for i, msg := range messages {
		bufAddr := baseAddr + uint64(i)*0x100
		txQueue.writeBuffer(bufAddr, []byte(msg))
		descIdx := txQueue.allocDescriptor(bufAddr, uint32(len(msg)), 0)
		txQueue.addAvailableBuffer(descIdx)
	}

	// Single notify should process all available buffers
	mmio.writeReg(VIRTIO_MMIO_QUEUE_NOTIFY, 1)

	// Verify all messages were transmitted
	expected := "Message 1\nMessage 2\nMessage 3\n"
	if output.String() != expected {
		t.Errorf("multiple transmits: got %q, want %q", output.String(), expected)
	}

	// Verify used ring
	usedIdx := txQueue.getUsedIdx()
	if usedIdx != 3 {
		t.Errorf("used index: got %d, want 3", usedIdx)
	}
}

// TestConsoleDeviceReset tests that device reset works correctly
func TestConsoleDeviceReset(t *testing.T) {
	vm := newConsoleTestVM(testMemorySize)
	output := &bytes.Buffer{}
	console := NewConsole(vm, ConsoleDefaultMMIOBase, ConsoleDefaultMMIOSize, ConsoleDefaultIRQLine, output, nil)
	mmio := newConsoleMMIOHelper(console)

	// Set up and initialize device
	rxDescTable := uint64(testDescTableAddr)
	rxAvailRing := uint64(testAvailRingAddr)
	rxUsedRing := uint64(testUsedRingAddr)
	txDescTable := uint64(testDescTableAddr + 0x10000)
	txAvailRing := uint64(testAvailRingAddr + 0x10000)
	txUsedRing := uint64(testUsedRingAddr + 0x10000)

	rxQueue := newConsoleVirtqueueSetup(vm, rxDescTable, rxAvailRing, rxUsedRing, 64)
	txQueue := newConsoleVirtqueueSetup(vm, txDescTable, txAvailRing, txUsedRing, 64)

	rxQueue.initRings()
	txQueue.initRings()

	initializeConsoleDevice(t, mmio, rxQueue, txQueue)

	// Do some I/O
	testData := []byte("Test data")
	bufferAddr := uint64(testBufferAddr)
	txQueue.writeBuffer(bufferAddr, testData)
	descIdx := txQueue.allocDescriptor(bufferAddr, uint32(len(testData)), 0)
	txQueue.addAvailableBuffer(descIdx)
	mmio.writeReg(VIRTIO_MMIO_QUEUE_NOTIFY, 1)

	// Reset device
	mmio.writeReg(VIRTIO_MMIO_STATUS, 0)

	// Verify status is 0
	status := mmio.readReg(VIRTIO_MMIO_STATUS)
	if status != 0 {
		t.Errorf("status after reset: got 0x%x, want 0", status)
	}

	// Verify interrupt status cleared
	intStatus := mmio.readReg(VIRTIO_MMIO_INTERRUPT_STATUS)
	if intStatus != 0 {
		t.Errorf("interrupt status after reset: got 0x%x, want 0", intStatus)
	}

	// Verify queue is not ready
	mmio.writeReg(VIRTIO_MMIO_QUEUE_SEL, 0)
	queueReady := mmio.readReg(VIRTIO_MMIO_QUEUE_READY)
	if queueReady != 0 {
		t.Errorf("queue ready after reset: got %d, want 0", queueReady)
	}
}

// BenchmarkConsoleTransmitThroughput benchmarks transmit throughput
func BenchmarkConsoleTransmitThroughput(b *testing.B) {
	vm := newConsoleTestVM(testMemorySize)
	output := io.Discard // Discard output for benchmark
	discardWriter := consoleDiscardWriter{output}
	console := NewConsole(vm, ConsoleDefaultMMIOBase, ConsoleDefaultMMIOSize, ConsoleDefaultIRQLine, discardWriter, nil)
	mmio := newConsoleMMIOHelper(console)

	// Set up queue memory regions
	rxDescTable := uint64(testDescTableAddr)
	rxAvailRing := uint64(testAvailRingAddr)
	rxUsedRing := uint64(testUsedRingAddr)
	txDescTable := uint64(testDescTableAddr + 0x10000)
	txAvailRing := uint64(testAvailRingAddr + 0x10000)
	txUsedRing := uint64(testUsedRingAddr + 0x10000)

	rxQueue := newConsoleVirtqueueSetup(vm, txDescTable, txAvailRing, txUsedRing, 256) // Note: typo fix - use rx addrs
	txQueue := newConsoleVirtqueueSetup(vm, txDescTable, txAvailRing, txUsedRing, 256)

	// Use correct addresses
	rxQueue = newConsoleVirtqueueSetup(vm, rxDescTable, rxAvailRing, rxUsedRing, 256)

	rxQueue.initRings()
	txQueue.initRings()

	// Initialize device (don't use t.Helper here)
	initializeConsoleDeviceBench(b, mmio, rxQueue, txQueue)

	// Prepare a 4KB buffer
	bufferSize := 4096
	testData := make([]byte, bufferSize)
	for i := range testData {
		testData[i] = byte(i % 256)
	}

	bufferAddr := uint64(testBufferAddr)
	vm.WriteAt(testData, int64(bufferAddr))

	b.SetBytes(int64(bufferSize))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// Reset queue state for each iteration
		txQueue.nextDescIdx = 0
		txQueue.availIdx = 0
		txQueue.initRings()

		// Create descriptor and submit
		descIdx := txQueue.allocDescriptor(bufferAddr, uint32(bufferSize), 0)
		txQueue.addAvailableBuffer(descIdx)

		// Notify
		mmio.writeReg(VIRTIO_MMIO_QUEUE_NOTIFY, 1)
	}

	b.StopTimer()
}

// BenchmarkConsoleTransmitBatch benchmarks batch transmit operations
func BenchmarkConsoleTransmitBatch(b *testing.B) {
	vm := newConsoleTestVM(testMemorySize)
	discardWriter := consoleDiscardWriter{io.Discard}
	console := NewConsole(vm, ConsoleDefaultMMIOBase, ConsoleDefaultMMIOSize, ConsoleDefaultIRQLine, discardWriter, nil)
	mmio := newConsoleMMIOHelper(console)

	// Set up queue memory regions
	rxDescTable := uint64(testDescTableAddr)
	rxAvailRing := uint64(testAvailRingAddr)
	rxUsedRing := uint64(testUsedRingAddr)
	txDescTable := uint64(testDescTableAddr + 0x10000)
	txAvailRing := uint64(testAvailRingAddr + 0x10000)
	txUsedRing := uint64(testUsedRingAddr + 0x10000)

	rxQueue := newConsoleVirtqueueSetup(vm, rxDescTable, rxAvailRing, rxUsedRing, 256)
	txQueue := newConsoleVirtqueueSetup(vm, txDescTable, txAvailRing, txUsedRing, 256)

	rxQueue.initRings()
	txQueue.initRings()

	initializeConsoleDeviceBench(b, mmio, rxQueue, txQueue)

	// Prepare multiple 1KB buffers
	batchSize := 32
	bufferSize := 1024
	totalBytes := batchSize * bufferSize

	// Pre-allocate buffers in guest memory
	for i := 0; i < batchSize; i++ {
		bufAddr := uint64(testBufferAddr) + uint64(i)*uint64(bufferSize)
		data := make([]byte, bufferSize)
		for j := range data {
			data[j] = byte((i + j) % 256)
		}
		vm.WriteAt(data, int64(bufAddr))
	}

	b.SetBytes(int64(totalBytes))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// Reset queue state
		txQueue.nextDescIdx = 0
		txQueue.availIdx = 0
		txQueue.initRings()

		// Submit all buffers
		for j := 0; j < batchSize; j++ {
			bufAddr := uint64(testBufferAddr) + uint64(j)*uint64(bufferSize)
			descIdx := txQueue.allocDescriptor(bufAddr, uint32(bufferSize), 0)
			txQueue.addAvailableBuffer(descIdx)
		}

		// Single notify for all buffers
		mmio.writeReg(VIRTIO_MMIO_QUEUE_NOTIFY, 1)
	}

	b.StopTimer()
}

// BenchmarkConsoleReceiveThroughput benchmarks receive throughput
func BenchmarkConsoleReceiveThroughput(b *testing.B) {
	vm := newConsoleTestVM(testMemorySize)
	discardWriter := consoleDiscardWriter{io.Discard}
	console := NewConsole(vm, ConsoleDefaultMMIOBase, ConsoleDefaultMMIOSize, ConsoleDefaultIRQLine, discardWriter, nil)
	mmio := newConsoleMMIOHelper(console)

	// Set up queue memory regions
	rxDescTable := uint64(testDescTableAddr)
	rxAvailRing := uint64(testAvailRingAddr)
	rxUsedRing := uint64(testUsedRingAddr)
	txDescTable := uint64(testDescTableAddr + 0x10000)
	txAvailRing := uint64(testAvailRingAddr + 0x10000)
	txUsedRing := uint64(testUsedRingAddr + 0x10000)

	rxQueue := newConsoleVirtqueueSetup(vm, rxDescTable, rxAvailRing, rxUsedRing, 256)
	txQueue := newConsoleVirtqueueSetup(vm, txDescTable, txAvailRing, txUsedRing, 256)

	rxQueue.initRings()
	txQueue.initRings()

	initializeConsoleDeviceBench(b, mmio, rxQueue, txQueue)

	// Prepare input data
	bufferSize := 4096
	testData := make([]byte, bufferSize)
	for i := range testData {
		testData[i] = byte(i % 256)
	}

	bufferAddr := uint64(testBufferAddr)

	b.SetBytes(int64(bufferSize))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// Reset queue state
		rxQueue.nextDescIdx = 0
		rxQueue.availIdx = 0
		rxQueue.initRings()

		// Enqueue input data
		console.enqueueInput(testData)

		// Provide receive buffer
		descIdx := rxQueue.allocDescriptor(bufferAddr, uint32(bufferSize), testVirtqDescFWrite)
		rxQueue.addAvailableBuffer(descIdx)

		// Notify to trigger receive processing
		mmio.writeReg(VIRTIO_MMIO_QUEUE_NOTIFY, 0)
	}

	b.StopTimer()
}

// BenchmarkConsoleRoundTrip benchmarks TX followed by RX operations (simulated round-trip)
func BenchmarkConsoleRoundTrip(b *testing.B) {
	vm := newConsoleTestVM(testMemorySize)
	discardWriter := consoleDiscardWriter{io.Discard}
	console := NewConsole(vm, ConsoleDefaultMMIOBase, ConsoleDefaultMMIOSize, ConsoleDefaultIRQLine, discardWriter, nil)
	mmio := newConsoleMMIOHelper(console)

	// Set up queue memory regions
	rxDescTable := uint64(testDescTableAddr)
	rxAvailRing := uint64(testAvailRingAddr)
	rxUsedRing := uint64(testUsedRingAddr)
	txDescTable := uint64(testDescTableAddr + 0x10000)
	txAvailRing := uint64(testAvailRingAddr + 0x10000)
	txUsedRing := uint64(testUsedRingAddr + 0x10000)

	rxQueue := newConsoleVirtqueueSetup(vm, rxDescTable, rxAvailRing, rxUsedRing, 256)
	txQueue := newConsoleVirtqueueSetup(vm, txDescTable, txAvailRing, txUsedRing, 256)

	rxQueue.initRings()
	txQueue.initRings()

	initializeConsoleDeviceBench(b, mmio, rxQueue, txQueue)

	// Prepare data
	bufferSize := 1024
	testData := make([]byte, bufferSize)
	for i := range testData {
		testData[i] = byte(i % 256)
	}

	txBufAddr := uint64(testBufferAddr)
	rxBufAddr := uint64(testBufferAddr + 0x10000)
	vm.WriteAt(testData, int64(txBufAddr))

	b.SetBytes(int64(bufferSize * 2)) // Count both directions
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// Reset queue states
		txQueue.nextDescIdx = 0
		txQueue.availIdx = 0
		txQueue.initRings()
		rxQueue.nextDescIdx = 0
		rxQueue.availIdx = 0
		rxQueue.initRings()

		// Transmit phase
		txDescIdx := txQueue.allocDescriptor(txBufAddr, uint32(bufferSize), 0)
		txQueue.addAvailableBuffer(txDescIdx)
		mmio.writeReg(VIRTIO_MMIO_QUEUE_NOTIFY, 1)

		// Simulate response by enqueueing data (without loopback to avoid recursion)
		console.enqueueInput(testData)

		// Receive phase - provide buffer and process
		rxDescIdx := rxQueue.allocDescriptor(rxBufAddr, uint32(bufferSize), testVirtqDescFWrite)
		rxQueue.addAvailableBuffer(rxDescIdx)
		mmio.writeReg(VIRTIO_MMIO_QUEUE_NOTIFY, 0)
	}

	b.StopTimer()
}

// consoleDiscardWriter wraps io.Discard to satisfy io.Writer interface
type consoleDiscardWriter struct {
	io.Writer
}

// initializeConsoleDeviceBench is like initializeConsoleDevice but for benchmarks
func initializeConsoleDeviceBench(b *testing.B, mmio *consoleMMIOHelper, rxQueue, txQueue *consoleVirtqueueSetup) {
	b.Helper()

	// Check magic value
	magic := mmio.readReg(VIRTIO_MMIO_MAGIC_VALUE)
	if magic != 0x74726976 {
		b.Fatalf("invalid magic value: got 0x%x, want 0x74726976", magic)
	}

	// Reset device
	mmio.writeReg(VIRTIO_MMIO_STATUS, 0)

	// Set status bits
	mmio.writeReg(VIRTIO_MMIO_STATUS, 1)   // ACKNOWLEDGE
	mmio.writeReg(VIRTIO_MMIO_STATUS, 1|2) // DRIVER

	// Feature negotiation
	mmio.writeReg(VIRTIO_MMIO_DEVICE_FEATURES_SEL, 0)
	featuresLow := mmio.readReg(VIRTIO_MMIO_DEVICE_FEATURES)
	mmio.writeReg(VIRTIO_MMIO_DEVICE_FEATURES_SEL, 1)
	featuresHigh := mmio.readReg(VIRTIO_MMIO_DEVICE_FEATURES)

	mmio.writeReg(VIRTIO_MMIO_DRIVER_FEATURES_SEL, 0)
	mmio.writeReg(VIRTIO_MMIO_DRIVER_FEATURES, featuresLow)
	mmio.writeReg(VIRTIO_MMIO_DRIVER_FEATURES_SEL, 1)
	mmio.writeReg(VIRTIO_MMIO_DRIVER_FEATURES, featuresHigh)

	mmio.writeReg(VIRTIO_MMIO_STATUS, 1|2|8) // FEATURES_OK

	// Configure receive queue
	mmio.writeReg(VIRTIO_MMIO_QUEUE_SEL, 0)
	mmio.writeReg(VIRTIO_MMIO_QUEUE_NUM, uint32(rxQueue.queueSize))
	mmio.writeReg(VIRTIO_MMIO_QUEUE_DESC_LOW, uint32(rxQueue.descTableAddr))
	mmio.writeReg(VIRTIO_MMIO_QUEUE_DESC_HIGH, uint32(rxQueue.descTableAddr>>32))
	mmio.writeReg(VIRTIO_MMIO_QUEUE_AVAIL_LOW, uint32(rxQueue.availRingAddr))
	mmio.writeReg(VIRTIO_MMIO_QUEUE_AVAIL_HIGH, uint32(rxQueue.availRingAddr>>32))
	mmio.writeReg(VIRTIO_MMIO_QUEUE_USED_LOW, uint32(rxQueue.usedRingAddr))
	mmio.writeReg(VIRTIO_MMIO_QUEUE_USED_HIGH, uint32(rxQueue.usedRingAddr>>32))
	mmio.writeReg(VIRTIO_MMIO_QUEUE_READY, 1)

	// Configure transmit queue
	mmio.writeReg(VIRTIO_MMIO_QUEUE_SEL, 1)
	mmio.writeReg(VIRTIO_MMIO_QUEUE_NUM, uint32(txQueue.queueSize))
	mmio.writeReg(VIRTIO_MMIO_QUEUE_DESC_LOW, uint32(txQueue.descTableAddr))
	mmio.writeReg(VIRTIO_MMIO_QUEUE_DESC_HIGH, uint32(txQueue.descTableAddr>>32))
	mmio.writeReg(VIRTIO_MMIO_QUEUE_AVAIL_LOW, uint32(txQueue.availRingAddr))
	mmio.writeReg(VIRTIO_MMIO_QUEUE_AVAIL_HIGH, uint32(txQueue.availRingAddr>>32))
	mmio.writeReg(VIRTIO_MMIO_QUEUE_USED_LOW, uint32(txQueue.usedRingAddr))
	mmio.writeReg(VIRTIO_MMIO_QUEUE_USED_HIGH, uint32(txQueue.usedRingAddr>>32))
	mmio.writeReg(VIRTIO_MMIO_QUEUE_READY, 1)

	// Driver OK
	mmio.writeReg(VIRTIO_MMIO_STATUS, 1|2|4|8)
}
