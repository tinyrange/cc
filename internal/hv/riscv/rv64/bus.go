package rv64

import (
	"fmt"
	"io"
)

// Device represents a memory-mapped device
type Device interface {
	// Read reads from the device at the given offset
	Read(offset uint64, size int) (uint64, error)
	// Write writes to the device at the given offset
	Write(offset uint64, size int, value uint64) error
	// Size returns the size of the device's address space
	Size() uint64
}

// MemoryRegion represents a contiguous region of RAM
type MemoryRegion struct {
	Data []byte
}

// NewMemoryRegion creates a new memory region of the given size
func NewMemoryRegion(size uint64) *MemoryRegion {
	return &MemoryRegion{
		Data: make([]byte, size),
	}
}

// Read implements Device
func (m *MemoryRegion) Read(offset uint64, size int) (uint64, error) {
	if offset+uint64(size) > uint64(len(m.Data)) {
		return 0, fmt.Errorf("memory read out of bounds: offset=0x%x size=%d len=%d", offset, size, len(m.Data))
	}

	switch size {
	case 1:
		return uint64(m.Data[offset]), nil
	case 2:
		return uint64(cpuEndian.Uint16(m.Data[offset:])), nil
	case 4:
		return uint64(cpuEndian.Uint32(m.Data[offset:])), nil
	case 8:
		return cpuEndian.Uint64(m.Data[offset:]), nil
	default:
		return 0, fmt.Errorf("invalid read size: %d", size)
	}
}

// Write implements Device
func (m *MemoryRegion) Write(offset uint64, size int, value uint64) error {
	if offset+uint64(size) > uint64(len(m.Data)) {
		return fmt.Errorf("memory write out of bounds: offset=0x%x size=%d len=%d", offset, size, len(m.Data))
	}

	switch size {
	case 1:
		m.Data[offset] = byte(value)
	case 2:
		cpuEndian.PutUint16(m.Data[offset:], uint16(value))
	case 4:
		cpuEndian.PutUint32(m.Data[offset:], uint32(value))
	case 8:
		cpuEndian.PutUint64(m.Data[offset:], value)
	default:
		return fmt.Errorf("invalid write size: %d", size)
	}
	return nil
}

// Size implements Device
func (m *MemoryRegion) Size() uint64 {
	return uint64(len(m.Data))
}

// ReadAt implements io.ReaderAt for loading data
func (m *MemoryRegion) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 || off >= int64(len(m.Data)) {
		return 0, io.EOF
	}
	n := copy(p, m.Data[off:])
	return n, nil
}

// WriteAt implements io.WriterAt for loading data
func (m *MemoryRegion) WriteAt(p []byte, off int64) (int, error) {
	if off < 0 || off > int64(len(m.Data)) {
		return 0, fmt.Errorf("write offset out of bounds")
	}
	n := copy(m.Data[off:], p)
	return n, nil
}

// Slice returns a slice of the memory region
func (m *MemoryRegion) Slice(offset, length uint64) []byte {
	if offset+length > uint64(len(m.Data)) {
		return nil
	}
	return m.Data[offset : offset+length]
}

// DeviceMapping maps a device to an address range
type DeviceMapping struct {
	Base   uint64
	Size   uint64
	Device Device
}

// BusInterface defines the interface for memory bus operations
type BusInterface interface {
	Read(addr uint64, size int) (uint64, error)
	Write(addr uint64, size int, value uint64) error
	Read8(addr uint64) (uint8, error)
	Read16(addr uint64) (uint16, error)
	Read32(addr uint64) (uint32, error)
	Read64(addr uint64) (uint64, error)
	Write8(addr uint64, value uint8) error
	Write16(addr uint64, value uint16) error
	Write32(addr uint64, value uint32) error
	Write64(addr uint64, value uint64) error
}

// Bus connects the CPU to memory and devices
type Bus struct {
	RAM        *MemoryRegion
	RAMBase    uint64
	Devices    []DeviceMapping

	// Output for UART (for debugging)
	UARTOutput io.Writer
}

// NewBus creates a new bus with the given RAM size
func NewBus(ramSize uint64) *Bus {
	return &Bus{
		RAM:     NewMemoryRegion(ramSize),
		RAMBase: RAMBase,
	}
}

// AddDevice adds a device mapping to the bus
func (bus *Bus) AddDevice(base uint64, dev Device) {
	bus.Devices = append(bus.Devices, DeviceMapping{
		Base:   base,
		Size:   dev.Size(),
		Device: dev,
	})
}

// findDevice finds a device at the given address
func (bus *Bus) findDevice(addr uint64) (Device, uint64, error) {
	// Fast path for RAM
	if addr >= bus.RAMBase && addr < bus.RAMBase+bus.RAM.Size() {
		return bus.RAM, addr - bus.RAMBase, nil
	}

	// Check devices
	for _, mapping := range bus.Devices {
		if addr >= mapping.Base && addr < mapping.Base+mapping.Size {
			return mapping.Device, addr - mapping.Base, nil
		}
	}

	return nil, 0, fmt.Errorf("no device at address 0x%x", addr)
}

// Read reads from the bus
func (bus *Bus) Read(addr uint64, size int) (uint64, error) {
	dev, offset, err := bus.findDevice(addr)
	if err != nil {
		return 0, err
	}
	return dev.Read(offset, size)
}

// Write writes to the bus
func (bus *Bus) Write(addr uint64, size int, value uint64) error {
	dev, offset, err := bus.findDevice(addr)
	if err != nil {
		return err
	}
	return dev.Write(offset, size, value)
}

// Read8 reads a byte from the bus
func (bus *Bus) Read8(addr uint64) (uint8, error) {
	val, err := bus.Read(addr, 1)
	return uint8(val), err
}

// Read16 reads a halfword from the bus
func (bus *Bus) Read16(addr uint64) (uint16, error) {
	val, err := bus.Read(addr, 2)
	return uint16(val), err
}

// Read32 reads a word from the bus
func (bus *Bus) Read32(addr uint64) (uint32, error) {
	val, err := bus.Read(addr, 4)
	return uint32(val), err
}

// Read64 reads a doubleword from the bus
func (bus *Bus) Read64(addr uint64) (uint64, error) {
	return bus.Read(addr, 8)
}

// Write8 writes a byte to the bus
func (bus *Bus) Write8(addr uint64, value uint8) error {
	return bus.Write(addr, 1, uint64(value))
}

// Write16 writes a halfword to the bus
func (bus *Bus) Write16(addr uint64, value uint16) error {
	return bus.Write(addr, 2, uint64(value))
}

// Write32 writes a word to the bus
func (bus *Bus) Write32(addr uint64, value uint32) error {
	return bus.Write(addr, 4, uint64(value))
}

// Write64 writes a doubleword to the bus
func (bus *Bus) Write64(addr uint64, value uint64) error {
	return bus.Write(addr, 8, value)
}

// LoadBytes loads bytes into the bus at the given address
func (bus *Bus) LoadBytes(addr uint64, data []byte) error {
	// Fast path for RAM
	if addr >= bus.RAMBase && addr+uint64(len(data)) <= bus.RAMBase+bus.RAM.Size() {
		copy(bus.RAM.Data[addr-bus.RAMBase:], data)
		return nil
	}

	// Slow path - write byte by byte
	for i, b := range data {
		if err := bus.Write8(addr+uint64(i), b); err != nil {
			return err
		}
	}
	return nil
}

// Fetch fetches an instruction (up to 4 bytes) from memory
func (bus *Bus) Fetch(addr uint64) (uint32, error) {
	// First, read 2 bytes to check if it's a compressed instruction
	lo, err := bus.Read16(addr)
	if err != nil {
		return 0, err
	}

	// Check if it's a compressed instruction (16-bit)
	if lo&0x3 != 0x3 {
		return uint32(lo), nil
	}

	// It's a 32-bit instruction, read the upper half
	hi, err := bus.Read16(addr + 2)
	if err != nil {
		return 0, err
	}

	return uint32(lo) | (uint32(hi) << 16), nil
}
