package virtio

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// Constants matching the unexported ones in mmio.go
const (
	testVirtqDescFNext  = 1
	testVirtqDescFWrite = 2
)

// mockGuestMemory implements GuestMemory for testing
type mockGuestMemory struct {
	data map[uint64][]byte
}

func newMockGuestMemory() *mockGuestMemory {
	return &mockGuestMemory{
		data: make(map[uint64][]byte),
	}
}

func (m *mockGuestMemory) ReadAt(p []byte, off int64) (int, error) {
	addr := uint64(off)
	for i := 0; i < len(p); i++ {
		if data, ok := m.data[addr+uint64(i)]; ok && len(data) > 0 {
			p[i] = data[0]
		} else {
			p[i] = 0
		}
	}
	return len(p), nil
}

func (m *mockGuestMemory) WriteAt(p []byte, off int64) (int, error) {
	addr := uint64(off)
	for i := 0; i < len(p); i++ {
		m.data[addr+uint64(i)] = []byte{p[i]}
	}
	return len(p), nil
}

// writeUint64 writes a uint64 value at the given address
func (m *mockGuestMemory) writeUint64(addr uint64, val uint64) {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], val)
	for i := 0; i < 8; i++ {
		m.data[addr+uint64(i)] = []byte{buf[i]}
	}
}

// writeUint32 writes a uint32 value at the given address
func (m *mockGuestMemory) writeUint32(addr uint64, val uint32) {
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], val)
	for i := 0; i < 4; i++ {
		m.data[addr+uint64(i)] = []byte{buf[i]}
	}
}

// writeUint16 writes a uint16 value at the given address
func (m *mockGuestMemory) writeUint16(addr uint64, val uint16) {
	var buf [2]byte
	binary.LittleEndian.PutUint16(buf[:], val)
	for i := 0; i < 2; i++ {
		m.data[addr+uint64(i)] = []byte{buf[i]}
	}
}

// readUint64 reads a uint64 value from the given address
func (m *mockGuestMemory) readUint64(addr uint64) uint64 {
	var buf [8]byte
	m.ReadAt(buf[:], int64(addr))
	return binary.LittleEndian.Uint64(buf[:])
}

// readUint32 reads a uint32 value from the given address
func (m *mockGuestMemory) readUint32(addr uint64) uint32 {
	var buf [4]byte
	m.ReadAt(buf[:], int64(addr))
	return binary.LittleEndian.Uint32(buf[:])
}

// readUint16 reads a uint16 value from the given address
func (m *mockGuestMemory) readUint16(addr uint64) uint16 {
	var buf [2]byte
	m.ReadAt(buf[:], int64(addr))
	return binary.LittleEndian.Uint16(buf[:])
}

// writeDescriptor writes a descriptor to the descriptor table
func (m *mockGuestMemory) writeDescriptor(descTableAddr uint64, idx uint16, desc VirtQueueDescriptor) {
	base := descTableAddr + uint64(idx)*16
	m.writeUint64(base+0, desc.Addr)
	m.writeUint32(base+8, desc.Length)
	m.writeUint16(base+12, desc.Flags)
	m.writeUint16(base+14, desc.Next)
}

// TestQueueDescriptorChainWalking tests reading descriptor chains
func TestQueueDescriptorChainWalking(t *testing.T) {
	mem := newMockGuestMemory()
	queue := NewVirtQueue(mem, 256)

	// Set up queue addresses
	descTableAddr := uint64(0x1000)
	availRingAddr := uint64(0x2000)
	usedRingAddr := uint64(0x3000)

	queue.SetAddresses(descTableAddr, availRingAddr, usedRingAddr)
	if err := queue.SetSize(4); err != nil {
		t.Fatalf("SetSize failed: %v", err)
	}
	queue.SetReady(true)

	// Test 1: Single descriptor chain
	t.Run("SingleDescriptor", func(t *testing.T) {
		queue.Reset()
		queue.SetAddresses(descTableAddr, availRingAddr, usedRingAddr)
		queue.SetSize(4)
		queue.SetReady(true)

		desc := VirtQueueDescriptor{
			Addr:   0x4000,
			Length: 100,
			Flags:  0, // Read-only, no next
			Next:   0,
		}
		mem.writeDescriptor(descTableAddr, 0, desc)

		payloads, err := queue.ReadDescriptorChain(0)
		if err != nil {
			t.Fatalf("ReadDescriptorChain failed: %v", err)
		}
		if len(payloads) != 1 {
			t.Fatalf("expected 1 payload, got %d", len(payloads))
		}
		if payloads[0].Addr != 0x4000 || payloads[0].Length != 100 {
			t.Fatalf("unexpected payload: %+v", payloads[0])
		}
		if payloads[0].IsWrite {
			t.Fatalf("expected read-only descriptor")
		}
	})

	// Test 2: Multi-descriptor chain
	t.Run("MultiDescriptorChain", func(t *testing.T) {
		queue.Reset()
		queue.SetAddresses(descTableAddr, availRingAddr, usedRingAddr)
		queue.SetSize(4)
		queue.SetReady(true)

		// Chain: 0 -> 1 -> 2
		desc0 := VirtQueueDescriptor{
			Addr:   0x4000,
			Length: 50,
			Flags:  testVirtqDescFNext, // Has next
			Next:   1,
		}
		desc1 := VirtQueueDescriptor{
			Addr:   0x5000,
			Length: 75,
			Flags:  testVirtqDescFNext | testVirtqDescFWrite, // Has next, write-only
			Next:   2,
		}
		desc2 := VirtQueueDescriptor{
			Addr:   0x6000,
			Length: 25,
			Flags:  0, // Last descriptor
			Next:   0,
		}

		mem.writeDescriptor(descTableAddr, 0, desc0)
		mem.writeDescriptor(descTableAddr, 1, desc1)
		mem.writeDescriptor(descTableAddr, 2, desc2)

		payloads, err := queue.ReadDescriptorChain(0)
		if err != nil {
			t.Fatalf("ReadDescriptorChain failed: %v", err)
		}
		if len(payloads) != 3 {
			t.Fatalf("expected 3 payloads, got %d", len(payloads))
		}

		// Check first descriptor (read-only)
		if payloads[0].Addr != 0x4000 || payloads[0].Length != 50 || payloads[0].IsWrite {
			t.Fatalf("unexpected payload[0]: %+v", payloads[0])
		}

		// Check second descriptor (write-only)
		if payloads[1].Addr != 0x5000 || payloads[1].Length != 75 || !payloads[1].IsWrite {
			t.Fatalf("unexpected payload[1]: %+v", payloads[1])
		}

		// Check third descriptor (read-only)
		if payloads[2].Addr != 0x6000 || payloads[2].Length != 25 || payloads[2].IsWrite {
			t.Fatalf("unexpected payload[2]: %+v", payloads[2])
		}
	})

	// Test 3: Circular chain detection (should stop at queue size)
	t.Run("CircularChainProtection", func(t *testing.T) {
		queue.Reset()
		queue.SetAddresses(descTableAddr, availRingAddr, usedRingAddr)
		queue.SetSize(2) // Small queue size
		queue.SetReady(true)

		// Create a chain that would loop: 0 -> 1 -> 0
		desc0 := VirtQueueDescriptor{
			Addr:   0x4000,
			Length: 50,
			Flags:  testVirtqDescFNext,
			Next:   1,
		}
		desc1 := VirtQueueDescriptor{
			Addr:   0x5000,
			Length: 75,
			Flags:  testVirtqDescFNext,
			Next:   0, // Points back to 0
		}

		mem.writeDescriptor(descTableAddr, 0, desc0)
		mem.writeDescriptor(descTableAddr, 1, desc1)

		payloads, err := queue.ReadDescriptorChain(0)
		if err != nil {
			t.Fatalf("ReadDescriptorChain failed: %v", err)
		}
		// Should stop at queue size (2 descriptors)
		if len(payloads) != 2 {
			t.Fatalf("expected 2 payloads (stopped at queue size), got %d", len(payloads))
		}
	})

	// Test 4: Out of bounds descriptor index
	t.Run("OutOfBoundsDescriptor", func(t *testing.T) {
		queue.Reset()
		queue.SetAddresses(descTableAddr, availRingAddr, usedRingAddr)
		queue.SetSize(4)
		queue.SetReady(true)

		_, err := queue.ReadDescriptor(4) // Index 4 is out of bounds for size 4
		if err == nil {
			t.Fatal("expected error for out-of-bounds descriptor index")
		}
	})
}

// TestUsedRingUpdates tests writing used buffer entries
func TestUsedRingUpdates(t *testing.T) {
	mem := newMockGuestMemory()
	queue := NewVirtQueue(mem, 256)

	descTableAddr := uint64(0x1000)
	availRingAddr := uint64(0x2000)
	usedRingAddr := uint64(0x3000)

	queue.SetAddresses(descTableAddr, availRingAddr, usedRingAddr)
	if err := queue.SetSize(4); err != nil {
		t.Fatalf("SetSize failed: %v", err)
	}
	queue.SetReady(true)

	// Initialize used ring header (flags + idx)
	mem.writeUint16(usedRingAddr+0, 0) // flags
	mem.writeUint16(usedRingAddr+2, 0) // idx (initial)

	// Test 1: Basic used buffer write
	t.Run("BasicUsedBufferWrite", func(t *testing.T) {
		queue.Reset()
		queue.SetAddresses(descTableAddr, availRingAddr, usedRingAddr)
		queue.SetSize(4)
		queue.SetReady(true)
		mem.writeUint16(usedRingAddr+0, 0)
		mem.writeUint16(usedRingAddr+2, 0)

		head := uint16(0)
		length := uint32(100)

		if err := queue.PutUsedBuffer(head, length); err != nil {
			t.Fatalf("PutUsedBuffer failed: %v", err)
		}

		// Check used element was written
		usedElemBase := usedRingAddr + 4 + uint64(0)*8
		gotHead := mem.readUint32(usedElemBase)
		gotLength := mem.readUint32(usedElemBase + 4)

		if gotHead != uint32(head) {
			t.Fatalf("expected head %d, got %d", head, gotHead)
		}
		if gotLength != length {
			t.Fatalf("expected length %d, got %d", length, gotLength)
		}

		// Check used index was updated
		gotIdx := mem.readUint16(usedRingAddr + 2)
		if gotIdx != 1 {
			t.Fatalf("expected used idx 1, got %d", gotIdx)
		}
	})

	// Test 2: Multiple used buffer writes
	t.Run("MultipleUsedBuffers", func(t *testing.T) {
		queue.Reset()
		queue.SetAddresses(descTableAddr, availRingAddr, usedRingAddr)
		queue.SetSize(4)
		queue.SetReady(true)
		mem.writeUint16(usedRingAddr+0, 0)
		mem.writeUint16(usedRingAddr+2, 0)

		// Write 3 used buffers
		for i := uint16(0); i < 3; i++ {
			if err := queue.PutUsedBuffer(i, uint32(i*10)); err != nil {
				t.Fatalf("PutUsedBuffer[%d] failed: %v", i, err)
			}
		}

		// Check all entries
		for i := uint16(0); i < 3; i++ {
			usedElemBase := usedRingAddr + 4 + uint64(i)*8
			gotHead := mem.readUint32(usedElemBase)
			gotLength := mem.readUint32(usedElemBase + 4)

			if gotHead != uint32(i) {
				t.Fatalf("entry[%d]: expected head %d, got %d", i, i, gotHead)
			}
			if gotLength != uint32(i*10) {
				t.Fatalf("entry[%d]: expected length %d, got %d", i, i*10, gotLength)
			}
		}

		// Check final used index
		gotIdx := mem.readUint16(usedRingAddr + 2)
		if gotIdx != 3 {
			t.Fatalf("expected used idx 3, got %d", gotIdx)
		}
	})

	// Test 3: Used ring wrapping
	t.Run("UsedRingWrapping", func(t *testing.T) {
		queue.Reset()
		queue.SetAddresses(descTableAddr, availRingAddr, usedRingAddr)
		queue.SetSize(2) // Small queue to test wrapping
		queue.SetReady(true)
		mem.writeUint16(usedRingAddr+0, 0)
		mem.writeUint16(usedRingAddr+2, 0)

		// Write 3 buffers (should wrap: 0, 1, 0)
		for i := uint16(0); i < 3; i++ {
			if err := queue.PutUsedBuffer(i, uint32(i*10)); err != nil {
				t.Fatalf("PutUsedBuffer[%d] failed: %v", i, err)
			}
		}

		// Third write should wrap to index 0
		usedElemBase := usedRingAddr + 4 + uint64(0)*8
		gotHead := mem.readUint32(usedElemBase)
		if gotHead != 2 {
			t.Fatalf("expected wrapped entry to have head 2, got %d", gotHead)
		}
	})

	// Test 4: Used buffer with interrupt suppression flag
	t.Run("UsedBufferWithInterruptSuppression", func(t *testing.T) {
		queue.Reset()
		queue.SetAddresses(descTableAddr, availRingAddr, usedRingAddr)
		queue.SetSize(4)
		queue.SetReady(true)
		mem.writeUint16(usedRingAddr+0, 0)
		mem.writeUint16(usedRingAddr+2, 0)

		// Write with interrupt suppression
		if err := queue.PutUsedBufferWithFlags(0, 100, true); err != nil {
			t.Fatalf("PutUsedBufferWithFlags failed: %v", err)
		}

		// Check flag was set
		flags := mem.readUint16(usedRingAddr + 0)
		const virtqUsedFNoNotify = 1
		if (flags & virtqUsedFNoNotify) == 0 {
			t.Fatalf("expected NO_NOTIFY flag to be set, flags=0x%x", flags)
		}

		// Clear interrupt suppression
		if err := queue.PutUsedBufferWithFlags(1, 200, false); err != nil {
			t.Fatalf("PutUsedBufferWithFlags(clear) failed: %v", err)
		}

		// Check flag was cleared
		flags = mem.readUint16(usedRingAddr + 0)
		if (flags & virtqUsedFNoNotify) != 0 {
			t.Fatalf("expected NO_NOTIFY flag to be cleared, flags=0x%x", flags)
		}
	})
}

// TestGetAvailableBuffer tests reading from the available ring
func TestGetAvailableBuffer(t *testing.T) {
	mem := newMockGuestMemory()
	queue := NewVirtQueue(mem, 256)

	descTableAddr := uint64(0x1000)
	availRingAddr := uint64(0x2000)
	usedRingAddr := uint64(0x3000)

	queue.SetAddresses(descTableAddr, availRingAddr, usedRingAddr)
	if err := queue.SetSize(4); err != nil {
		t.Fatalf("SetSize failed: %v", err)
	}
	queue.SetReady(true)

	// Test 1: Empty available ring
	t.Run("EmptyAvailableRing", func(t *testing.T) {
		queue.Reset()
		queue.SetAddresses(descTableAddr, availRingAddr, usedRingAddr)
		queue.SetSize(4)
		queue.SetReady(true)

		// Initialize available ring (idx = 0, no entries)
		mem.writeUint16(availRingAddr+0, 0) // flags
		mem.writeUint16(availRingAddr+2, 0) // idx

		head, hasBuffer, err := queue.GetAvailableBuffer()
		if err != nil {
			t.Fatalf("GetAvailableBuffer failed: %v", err)
		}
		if hasBuffer {
			t.Fatalf("expected no buffer available")
		}
		_ = head // unused when hasBuffer is false
	})

	// Test 2: Single available buffer
	t.Run("SingleAvailableBuffer", func(t *testing.T) {
		queue.Reset()
		queue.SetAddresses(descTableAddr, availRingAddr, usedRingAddr)
		queue.SetSize(4)
		queue.SetReady(true)

		// Set up available ring with one entry
		mem.writeUint16(availRingAddr+0, 0) // flags
		mem.writeUint16(availRingAddr+2, 1) // idx = 1 (one entry)
		mem.writeUint16(availRingAddr+4, 2) // ring[0] = descriptor index 2

		head, hasBuffer, err := queue.GetAvailableBuffer()
		if err != nil {
			t.Fatalf("GetAvailableBuffer failed: %v", err)
		}
		if !hasBuffer {
			t.Fatalf("expected buffer available")
		}
		if head != 2 {
			t.Fatalf("expected head 2, got %d", head)
		}

		// Second call should return no buffer (already consumed)
		head, hasBuffer, err = queue.GetAvailableBuffer()
		if err != nil {
			t.Fatalf("GetAvailableBuffer failed: %v", err)
		}
		if hasBuffer {
			t.Fatalf("expected no buffer available after consumption")
		}
	})

	// Test 3: Multiple available buffers
	t.Run("MultipleAvailableBuffers", func(t *testing.T) {
		queue.Reset()
		queue.SetAddresses(descTableAddr, availRingAddr, usedRingAddr)
		queue.SetSize(4)
		queue.SetReady(true)

		// Set up available ring with 3 entries
		mem.writeUint16(availRingAddr+0, 0) // flags
		mem.writeUint16(availRingAddr+2, 3) // idx = 3
		mem.writeUint16(availRingAddr+4, 0) // ring[0] = 0
		mem.writeUint16(availRingAddr+6, 1) // ring[1] = 1
		mem.writeUint16(availRingAddr+8, 2) // ring[2] = 2

		// Read all buffers
		expectedHeads := []uint16{0, 1, 2}
		for i, expectedHead := range expectedHeads {
			head, hasBuffer, err := queue.GetAvailableBuffer()
			if err != nil {
				t.Fatalf("GetAvailableBuffer[%d] failed: %v", i, err)
			}
			if !hasBuffer {
				t.Fatalf("expected buffer[%d] available", i)
			}
			if head != expectedHead {
				t.Fatalf("buffer[%d]: expected head %d, got %d", i, expectedHead, head)
			}
		}

		// Should be empty now
		_, hasBuffer, err := queue.GetAvailableBuffer()
		if err != nil {
			t.Fatalf("GetAvailableBuffer failed: %v", err)
		}
		if hasBuffer {
			t.Fatalf("expected no more buffers")
		}
	})

	// Test 4: Available ring wrapping
	t.Run("AvailableRingWrapping", func(t *testing.T) {
		queue.Reset()
		queue.SetAddresses(descTableAddr, availRingAddr, usedRingAddr)
		queue.SetSize(2) // Small queue to test wrapping
		queue.SetReady(true)

		// Simulate: lastAvailIdx = 2, availIdx = 4 (wrapped)
		// This means entries at ring indices 0 and 1 are available
		mem.writeUint16(availRingAddr+0, 0) // flags
		mem.writeUint16(availRingAddr+2, 4) // idx = 4
		mem.writeUint16(availRingAddr+4, 5) // ring[0] = 5
		mem.writeUint16(availRingAddr+6, 6) // ring[1] = 6

		// Manually set lastAvailIdx to 2
		queue.Reset()
		queue.SetAddresses(descTableAddr, availRingAddr, usedRingAddr)
		queue.SetSize(2)
		queue.SetReady(true)
		// We need to access the private field, so we'll use reflection or test differently
		// For now, let's test the wrapping by reading multiple times
		// First, consume some buffers to get lastAvailIdx to 2
		mem.writeUint16(availRingAddr+2, 2)
		mem.writeUint16(availRingAddr+4, 0)
		mem.writeUint16(availRingAddr+6, 1)
		_, _, _ = queue.GetAvailableBuffer() // consume first
		_, _, _ = queue.GetAvailableBuffer() // consume second, lastAvailIdx should be 2

		// Now update availIdx to 4 (wrapped)
		mem.writeUint16(availRingAddr+2, 4)
		mem.writeUint16(availRingAddr+4, 5) // ring[0] = 5
		mem.writeUint16(availRingAddr+6, 6) // ring[1] = 6

		// Should read ring[0] (5) and ring[1] (6)
		head, hasBuffer, err := queue.GetAvailableBuffer()
		if err != nil {
			t.Fatalf("GetAvailableBuffer failed: %v", err)
		}
		if !hasBuffer {
			t.Fatalf("expected buffer available after wrap")
		}
		if head != 5 {
			t.Fatalf("expected head 5 after wrap, got %d", head)
		}
	})
}

// TestFeatureNegotiation tests feature negotiation (via VirtioDevice interface)
// This is a simplified test that verifies the interface contract
func TestFeatureNegotiation(t *testing.T) {
	// Create a mock device that implements VirtioDevice
	mockDevice := &mockVirtioDevice{
		deviceID:    3, // Console device
		features:    0x1234567890ABCDEF,
		maxQueues:   2,
		configSpace: make(map[uint16]uint32),
	}

	// Test 1: Device ID
	if mockDevice.DeviceID() != 3 {
		t.Fatalf("expected device ID 3, got %d", mockDevice.DeviceID())
	}

	// Test 2: Device features
	features := mockDevice.DeviceFeatures()
	if features != 0x1234567890ABCDEF {
		t.Fatalf("expected features 0x%x, got 0x%x", uint64(0x1234567890ABCDEF), features)
	}

	// Test 3: Max queues
	if mockDevice.MaxQueues() != 2 {
		t.Fatalf("expected max queues 2, got %d", mockDevice.MaxQueues())
	}

	// Test 4: Config space read/write
	mockDevice.WriteConfig(0, 0xDEADBEEF)
	val := mockDevice.ReadConfig(0)
	if val != 0xDEADBEEF {
		t.Fatalf("expected config[0] = 0x%x, got 0x%x", uint32(0xDEADBEEF), val)
	}

	// Test 5: Enable/Disable
	mem := newMockGuestMemory()
	queue1 := NewVirtQueue(mem, 256)
	queue2 := NewVirtQueue(mem, 256)
	queues := []*VirtQueue{queue1, queue2}

	negotiatedFeatures := uint64(0x1234567890ABCDEF & 0xFFFFFFFF) // Lower 32 bits
	mockDevice.Enable(negotiatedFeatures, queues)

	if !mockDevice.enabled {
		t.Fatal("expected device to be enabled")
	}
	if mockDevice.enabledFeatures != negotiatedFeatures {
		t.Fatalf("expected enabled features 0x%x, got 0x%x", negotiatedFeatures, mockDevice.enabledFeatures)
	}
	if len(mockDevice.enabledQueues) != 2 {
		t.Fatalf("expected 2 enabled queues, got %d", len(mockDevice.enabledQueues))
	}

	// Test 6: Disable
	mockDevice.Disable()
	if mockDevice.enabled {
		t.Fatal("expected device to be disabled")
	}
	if len(mockDevice.enabledQueues) != 0 {
		t.Fatalf("expected 0 enabled queues after disable, got %d", len(mockDevice.enabledQueues))
	}
}

// mockVirtioDevice implements VirtioDevice for testing
type mockVirtioDevice struct {
	deviceID        uint16
	features        uint64
	maxQueues       uint16
	configSpace     map[uint16]uint32
	enabled         bool
	enabledFeatures uint64
	enabledQueues   []*VirtQueue
}

func (m *mockVirtioDevice) DeviceID() uint16 {
	return m.deviceID
}

func (m *mockVirtioDevice) DeviceFeatures() uint64 {
	return m.features
}

func (m *mockVirtioDevice) MaxQueues() uint16 {
	return m.maxQueues
}

func (m *mockVirtioDevice) ReadConfig(offset uint16) uint32 {
	return m.configSpace[offset]
}

func (m *mockVirtioDevice) WriteConfig(offset uint16, val uint32) {
	m.configSpace[offset] = val
}

func (m *mockVirtioDevice) Enable(features uint64, queues []*VirtQueue) {
	m.enabled = true
	m.enabledFeatures = features
	m.enabledQueues = make([]*VirtQueue, len(queues))
	copy(m.enabledQueues, queues)
}

func (m *mockVirtioDevice) Disable() {
	m.enabled = false
	m.enabledFeatures = 0
	m.enabledQueues = nil
}

// TestGuestMemoryAccess tests ReadGuest and WriteGuest helpers
func TestGuestMemoryAccess(t *testing.T) {
	mem := newMockGuestMemory()
	queue := NewVirtQueue(mem, 256)

	descTableAddr := uint64(0x1000)
	availRingAddr := uint64(0x2000)
	usedRingAddr := uint64(0x3000)

	queue.SetAddresses(descTableAddr, availRingAddr, usedRingAddr)
	if err := queue.SetSize(4); err != nil {
		t.Fatalf("SetSize failed: %v", err)
	}
	queue.SetReady(true)

	// Test ReadGuest
	testData := []byte{0x01, 0x02, 0x03, 0x04, 0x05}
	testAddr := uint64(0x5000)

	// Write test data directly to memory
	for i, b := range testData {
		mem.data[testAddr+uint64(i)] = []byte{b}
	}

	// Read via ReadGuest
	readData, err := queue.ReadGuest(testAddr, uint32(len(testData)))
	if err != nil {
		t.Fatalf("ReadGuest failed: %v", err)
	}
	if !bytes.Equal(readData, testData) {
		t.Fatalf("expected %v, got %v", testData, readData)
	}

	// Test WriteGuest
	writeData := []byte{0xAA, 0xBB, 0xCC}
	writeAddr := uint64(0x6000)

	if err := queue.WriteGuest(writeAddr, writeData); err != nil {
		t.Fatalf("WriteGuest failed: %v", err)
	}

	// Verify written data
	for i, b := range writeData {
		if mem.data[writeAddr+uint64(i)][0] != b {
			t.Fatalf("writeData[%d]: expected 0x%x, got 0x%x", i, b, mem.data[writeAddr+uint64(i)][0])
		}
	}
}

// TestQueueStateManagement tests queue state transitions
func TestQueueStateManagement(t *testing.T) {
	mem := newMockGuestMemory()
	queue := NewVirtQueue(mem, 256)

	// Test initial state
	if queue.Enabled {
		t.Fatal("expected queue to be disabled initially")
	}
	if queue.Ready {
		t.Fatal("expected queue to be not ready initially")
	}
	if queue.Size != 0 {
		t.Fatalf("expected size 0, got %d", queue.Size)
	}

	// Test SetSize
	if err := queue.SetSize(128); err != nil {
		t.Fatalf("SetSize failed: %v", err)
	}
	if queue.Size != 128 {
		t.Fatalf("expected size 128, got %d", queue.Size)
	}

	// Test SetSize with invalid size
	if err := queue.SetSize(0); err == nil {
		t.Fatal("expected error for size 0")
	}
	if err := queue.SetSize(257); err == nil { // > MaxSize
		t.Fatal("expected error for size > MaxSize")
	}

	// Test SetReady
	queue.SetReady(true)
	if !queue.Ready {
		t.Fatal("expected queue to be ready")
	}

	// Test Reset
	queue.Reset()
	if queue.Ready {
		t.Fatal("expected queue to be not ready after reset")
	}
	if queue.Size != 0 {
		t.Fatalf("expected size 0 after reset, got %d", queue.Size)
	}
	if queue.DescTableAddr != 0 {
		t.Fatalf("expected desc table addr 0 after reset, got 0x%x", queue.DescTableAddr)
	}
}
