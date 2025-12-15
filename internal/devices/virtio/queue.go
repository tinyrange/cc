package virtio

import (
	"encoding/binary"
	"fmt"
	"io"
)

// GuestMemory provides access to guest physical memory.
// This interface abstracts the memory access needed for virtio queue operations.
type GuestMemory interface {
	io.ReaderAt
	io.WriterAt
}

// VirtQueueDescriptor represents a single descriptor in a virtio queue.
type VirtQueueDescriptor struct {
	Addr   uint64
	Length uint32
	Flags  uint16
	Next   uint16
}

// VirtQueuePayload represents a single buffer in a descriptor chain.
type VirtQueuePayload struct {
	Addr   uint64
	Length uint32
	IsWrite bool
}

// VirtQueue represents a virtio queue with its rings and state.
type VirtQueue struct {
	DescTableAddr uint64
	AvailRingAddr uint64
	UsedRingAddr  uint64
	Size           uint16
	MaxSize        uint16
	Enabled        bool
	Ready          bool

	// Internal state tracking
	lastAvailIdx uint16
	usedIdx      uint16

	// Guest memory access
	mem GuestMemory

	// NotifyEvent channel for queue notifications (optional)
	NotifyEvent chan struct{}
}

// NewVirtQueue creates a new VirtQueue instance.
func NewVirtQueue(mem GuestMemory, maxSize uint16) *VirtQueue {
	return &VirtQueue{
		MaxSize:     maxSize,
		mem:         mem,
		NotifyEvent: make(chan struct{}, 1),
	}
}

// Reset clears the queue state.
func (q *VirtQueue) Reset() {
	q.Size = 0
	q.Ready = false
	q.DescTableAddr = 0
	q.AvailRingAddr = 0
	q.UsedRingAddr = 0
	q.lastAvailIdx = 0
	q.usedIdx = 0
	q.Enabled = false
}

// SetAddresses configures the queue ring addresses.
func (q *VirtQueue) SetAddresses(descAddr, availAddr, usedAddr uint64) {
	q.DescTableAddr = descAddr
	q.AvailRingAddr = availAddr
	q.UsedRingAddr = usedAddr
}

// SetSize sets the queue size (number of descriptors).
func (q *VirtQueue) SetSize(size uint16) error {
	if size > q.MaxSize {
		return fmt.Errorf("queue size %d exceeds max size %d", size, q.MaxSize)
	}
	if size == 0 {
		return fmt.Errorf("queue size cannot be zero")
	}
	q.Size = size
	return nil
}

// SetReady marks the queue as ready for operation.
func (q *VirtQueue) SetReady(ready bool) {
	q.Ready = ready
	if !ready {
		q.Reset()
	}
}

// ReadDescriptor reads a descriptor from the descriptor table.
func (q *VirtQueue) ReadDescriptor(idx uint16) (VirtQueueDescriptor, error) {
	if err := q.ensureReady(); err != nil {
		return VirtQueueDescriptor{}, err
	}
	if idx >= q.Size {
		return VirtQueueDescriptor{}, fmt.Errorf("descriptor index %d out of bounds (size %d)", idx, q.Size)
	}

	var buf [16]byte
	offset := q.DescTableAddr + uint64(idx)*16
	if err := q.readGuestInto(offset, buf[:]); err != nil {
		return VirtQueueDescriptor{}, err
	}

	return VirtQueueDescriptor{
		Addr:   binary.LittleEndian.Uint64(buf[0:8]),
		Length: binary.LittleEndian.Uint32(buf[8:12]),
		Flags:  binary.LittleEndian.Uint16(buf[12:14]),
		Next:   binary.LittleEndian.Uint16(buf[14:16]),
	}, nil
}

// GetAvailableBuffer reads the next available buffer from the available ring.
// Returns the descriptor head index, whether there was a buffer available, and any error.
func (q *VirtQueue) GetAvailableBuffer() (head uint16, hasBuffer bool, err error) {
	if err := q.ensureReady(); err != nil {
		return 0, false, err
	}

	// Read available ring header (flags + idx)
	var header [4]byte
	if err := q.readGuestInto(q.AvailRingAddr, header[:]); err != nil {
		return 0, false, err
	}
	flags := binary.LittleEndian.Uint16(header[0:2])
	availIdx := binary.LittleEndian.Uint16(header[2:4])

	// Check if there are new buffers available
	if q.lastAvailIdx == availIdx {
		return 0, false, nil
	}

	// Read the descriptor head index from the available ring
	ringIndex := q.lastAvailIdx % q.Size
	var buf [2]byte
	offset := q.AvailRingAddr + 4 + uint64(ringIndex)*2
	if err := q.readGuestInto(offset, buf[:]); err != nil {
		return 0, false, err
	}

	head = binary.LittleEndian.Uint16(buf[:])
	q.lastAvailIdx++

	// Check interrupt suppression flag (VIRTQ_AVAIL_F_NO_INTERRUPT)
	// If set, the driver doesn't want interrupts when buffers are consumed
	_ = flags // TODO: Use flags for interrupt suppression if needed

	return head, true, nil
}

// GetAvailableBuffers reads all available buffers from the available ring.
// Returns a slice of descriptor head indices.
func (q *VirtQueue) GetAvailableBuffers() ([]uint16, error) {
	if err := q.ensureReady(); err != nil {
		return nil, err
	}

	var heads []uint16
	for {
		head, hasBuffer, err := q.GetAvailableBuffer()
		if err != nil {
			return heads, err
		}
		if !hasBuffer {
			break
		}
		heads = append(heads, head)
	}
	return heads, nil
}

// ReadDescriptorChain reads a complete descriptor chain starting from head.
// Returns a slice of payloads representing the buffers in the chain.
func (q *VirtQueue) ReadDescriptorChain(head uint16) ([]VirtQueuePayload, error) {
	if err := q.ensureReady(); err != nil {
		return nil, err
	}

	var payloads []VirtQueuePayload
	index := head

	// Walk the descriptor chain (limit to queue size to prevent infinite loops)
	for i := uint16(0); i < q.Size; i++ {
		desc, err := q.ReadDescriptor(index)
		if err != nil {
			return payloads, err
		}

		isWrite := (desc.Flags & virtqDescFWrite) != 0
		payloads = append(payloads, VirtQueuePayload{
			Addr:    desc.Addr,
			Length:  desc.Length,
			IsWrite: isWrite,
		})

		// Check if this is the last descriptor in the chain
		if (desc.Flags & virtqDescFNext) == 0 {
			break
		}
		index = desc.Next
	}

	return payloads, nil
}

// PutUsedBuffer writes a used buffer entry to the used ring.
// head is the descriptor head index, and length is the total length written.
func (q *VirtQueue) PutUsedBuffer(head uint16, length uint32) error {
	if err := q.ensureReady(); err != nil {
		return err
	}

	usedIdx := q.usedIdx % q.Size
	base := q.UsedRingAddr + 4 + uint64(usedIdx)*8

	// Write used element (head index + length)
	if err := q.writeGuestUint32(base, uint32(head)); err != nil {
		return err
	}
	if err := q.writeGuestUint32(base+4, length); err != nil {
		return err
	}

	// Update used index
	q.usedIdx++
	return q.writeGuestUint16(q.UsedRingAddr+2, q.usedIdx)
}

// PutUsedBufferWithFlags writes a used buffer entry with interrupt suppression flag.
// If suppressInterrupt is true, sets VIRTQ_USED_F_NO_NOTIFY flag.
func (q *VirtQueue) PutUsedBufferWithFlags(head uint16, length uint32, suppressInterrupt bool) error {
	if err := q.PutUsedBuffer(head, length); err != nil {
		return err
	}

	// Read used ring flags
	var flags [2]byte
	if err := q.readGuestInto(q.UsedRingAddr, flags[:]); err != nil {
		return err
	}
	usedFlags := binary.LittleEndian.Uint16(flags[:])

	// Set or clear VIRTQ_USED_F_NO_NOTIFY flag
	const virtqUsedFNoNotify = 1
	if suppressInterrupt {
		usedFlags |= virtqUsedFNoNotify
	} else {
		usedFlags &^= virtqUsedFNoNotify
	}

	return q.writeGuestUint16(q.UsedRingAddr, usedFlags)
}

// ReadGuest reads data from guest memory.
func (q *VirtQueue) ReadGuest(addr uint64, length uint32) ([]byte, error) {
	if length == 0 {
		return nil, nil
	}
	buf := make([]byte, length)
	if err := q.readGuestInto(addr, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

// WriteGuest writes data to guest memory.
func (q *VirtQueue) WriteGuest(addr uint64, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	return q.writeGuestFrom(addr, data)
}

// Helper methods for guest memory access

func (q *VirtQueue) ensureReady() error {
	if !q.Ready || q.Size == 0 {
		return fmt.Errorf("queue not ready")
	}
	if q.mem == nil {
		return fmt.Errorf("guest memory accessor is nil")
	}
	return nil
}

func (q *VirtQueue) readGuestInto(addr uint64, buf []byte) error {
	if len(buf) == 0 {
		return nil
	}
	off, err := guestOffset(addr, len(buf))
	if err != nil {
		return err
	}
	n, err := q.mem.ReadAt(buf, off)
	if err != nil {
		return err
	}
	if n != len(buf) {
		return fmt.Errorf("virtio: short guest memory read (want %d, got %d)", len(buf), n)
	}
	return nil
}

func (q *VirtQueue) writeGuestFrom(addr uint64, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	off, err := guestOffset(addr, len(data))
	if err != nil {
		return err
	}
	n, err := q.mem.WriteAt(data, off)
	if err != nil {
		return err
	}
	if n != len(data) {
		return fmt.Errorf("virtio: short guest memory write (want %d, got %d)", len(data), n)
	}
	return nil
}

func (q *VirtQueue) readGuestUint16(addr uint64) (uint16, error) {
	var buf [2]byte
	if err := q.readGuestInto(addr, buf[:]); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint16(buf[:]), nil
}

func (q *VirtQueue) writeGuestUint16(addr uint64, value uint16) error {
	var buf [2]byte
	binary.LittleEndian.PutUint16(buf[:], value)
	return q.writeGuestFrom(addr, buf[:])
}

func (q *VirtQueue) writeGuestUint32(addr uint64, value uint32) error {
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], value)
	return q.writeGuestFrom(addr, buf[:])
}

