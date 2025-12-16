package serial

import (
	"fmt"
	"io"

	"github.com/tinyrange/cc/internal/chipset"
	am64serial "github.com/tinyrange/cc/internal/devices/amd64/serial"
	"github.com/tinyrange/cc/internal/hv"
)

const (
	// Serial16550MMIOSize is the default MMIO region size for Serial16550.
	// This reserves space for the 8 registers with up to 4-byte stride.
	Serial16550MMIOSize = 0x1000
)

// Serial16550MMIO wraps a Serial16550 device and exposes it via MMIO instead of PIO.
// It supports configurable register stride (1, 2, or 4 bytes) to match different
// hardware implementations.
type Serial16550MMIO struct {
	serial *am64serial.Serial16550
	base   uint64
	size   uint64
	stride uint64 // Register stride: 1, 2, or 4 bytes
}

// NewSerial16550MMIO creates a new Serial16550MMIO wrapper.
// base is the MMIO base address, regShift controls register spacing (stride = 1 << regShift),
// irqLine is the interrupt line, and out/in are the output/input streams.
func NewSerial16550MMIO(base uint64, regShift uint32, irqLine chipset.LineInterrupt, out io.Writer, in io.Reader) *Serial16550MMIO {
	stride := uint64(1) << regShift
	if stride == 0 {
		stride = 1
	}
	if stride > 4 {
		stride = 4
	}

	// Create underlying Serial16550 with base=0 since we'll handle address translation
	serial := am64serial.NewSerial16550(0, irqLine, out, in)

	return &Serial16550MMIO{
		serial: serial,
		base:   base,
		size:   Serial16550MMIOSize,
		stride: stride,
	}
}

// Init implements hv.Device.
func (s *Serial16550MMIO) Init(vm hv.VirtualMachine) error {
	return s.serial.Init(vm)
}

// Start implements chipset.ChangeDeviceState.
func (s *Serial16550MMIO) Start() error {
	return s.serial.Start()
}

// Stop implements chipset.ChangeDeviceState.
func (s *Serial16550MMIO) Stop() error {
	return s.serial.Stop()
}

// Reset implements chipset.ChangeDeviceState.
func (s *Serial16550MMIO) Reset() error {
	return s.serial.Reset()
}

// SupportsPortIO implements chipset.ChipsetDevice.
// Returns nil since this device only supports MMIO.
func (s *Serial16550MMIO) SupportsPortIO() *chipset.PortIOIntercept {
	return nil
}

// SupportsMmio implements chipset.ChipsetDevice.
func (s *Serial16550MMIO) SupportsMmio() *chipset.MmioIntercept {
	return &chipset.MmioIntercept{
		Regions: []hv.MMIORegion{
			{
				Address: s.base,
				Size:    s.size,
			},
		},
		Handler: s,
	}
}

// SupportsPollDevice implements chipset.ChipsetDevice.
func (s *Serial16550MMIO) SupportsPollDevice() *chipset.PollDevice {
	return s.serial.SupportsPollDevice()
}

// ReadMMIO implements chipset.MmioHandler.
func (s *Serial16550MMIO) ReadMMIO(addr uint64, data []byte) error {
	for i := range data {
		val, err := s.readByte(addr + uint64(i))
		if err != nil {
			return err
		}
		data[i] = val
	}
	return nil
}

// WriteMMIO implements chipset.MmioHandler.
func (s *Serial16550MMIO) WriteMMIO(addr uint64, data []byte) error {
	for i := range data {
		if err := s.writeByte(addr+uint64(i), data[i]); err != nil {
			return err
		}
	}
	return nil
}

// readByte reads a single byte from the MMIO address, mapping it to a register offset.
func (s *Serial16550MMIO) readByte(addr uint64) (byte, error) {
	if addr < s.base || addr >= s.base+s.size {
		return 0, fmt.Errorf("serial16550-mmio: address 0x%x out of bounds", addr)
	}

	offset := addr - s.base

	// For multi-byte strides, only aligned accesses are valid
	if offset%s.stride != 0 {
		// Unaligned access - return 0 (some hardware allows this)
		return 0, nil
	}

	// Map MMIO offset to register index
	regIndex := offset / s.stride
	if regIndex >= 8 { // serialRegisterCount
		return 0, nil
	}

	// Read from underlying Serial16550 using register index as port offset
	port := uint16(regIndex)
	var result [1]byte
	if err := s.serial.ReadIOPort(port, result[:]); err != nil {
		return 0, err
	}
	return result[0], nil
}

// writeByte writes a single byte to the MMIO address, mapping it to a register offset.
func (s *Serial16550MMIO) writeByte(addr uint64, value byte) error {
	if addr < s.base || addr >= s.base+s.size {
		return fmt.Errorf("serial16550-mmio: address 0x%x out of bounds", addr)
	}

	offset := addr - s.base

	// For multi-byte strides, only aligned accesses are valid
	if offset%s.stride != 0 {
		// Unaligned access - ignore (some hardware allows this)
		return nil
	}

	// Map MMIO offset to register index
	regIndex := offset / s.stride
	if regIndex >= 8 { // serialRegisterCount
		return nil
	}

	// Write to underlying Serial16550 using register index as port offset
	port := uint16(regIndex)
	return s.serial.WriteIOPort(port, []byte{value})
}

// SetIRQLine configures the LineInterrupt used for IRQ delivery.
func (s *Serial16550MMIO) SetIRQLine(line chipset.LineInterrupt) {
	s.serial.SetIRQLine(line)
}

var (
	_ hv.Device                 = (*Serial16550MMIO)(nil)
	_ chipset.ChipsetDevice     = (*Serial16550MMIO)(nil)
	_ chipset.MmioHandler       = (*Serial16550MMIO)(nil)
	_ chipset.ChangeDeviceState = (*Serial16550MMIO)(nil)
)
