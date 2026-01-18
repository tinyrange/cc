// Package pl031 implements the ARM PrimeCell PL031 Real Time Clock.
package pl031

import (
	"encoding/binary"
	"fmt"
	"sync"
	"time"

	"github.com/tinyrange/cc/internal/chipset"
	"github.com/tinyrange/cc/internal/hv"
)

// PL031 register offsets
const (
	PL031_DR   = 0x00 // Data Register (RO) - current counter value
	PL031_MR   = 0x04 // Match Register (RW)
	PL031_LR   = 0x08 // Load Register (RW)
	PL031_CR   = 0x0C // Control Register (RW)
	PL031_IMSC = 0x10 // Interrupt Mask Set/Clear (RW)
	PL031_RIS  = 0x14 // Raw Interrupt Status (RO)
	PL031_MIS  = 0x18 // Masked Interrupt Status (RO)
	PL031_ICR  = 0x1C // Interrupt Clear Register (WO)

	// PrimCell identification registers
	PL031_PERIPH_ID0 = 0xFE0
	PL031_PERIPH_ID1 = 0xFE4
	PL031_PERIPH_ID2 = 0xFE8
	PL031_PERIPH_ID3 = 0xFEC
	PL031_PCELL_ID0  = 0xFF0
	PL031_PCELL_ID1  = 0xFF4
	PL031_PCELL_ID2  = 0xFF8
	PL031_PCELL_ID3  = 0xFFC
)

// PL031 Control Register bits
const (
	PL031_CR_EN = 1 << 0 // RTC enable
)

// Default base address and size for PL031
const (
	DefaultBase = 0x09010000
	DefaultSize = 0x1000
)

// PL031 implements the ARM PrimeCell PL031 Real Time Clock.
type PL031 struct {
	mu sync.Mutex

	base uint64
	size uint64

	// RTC state
	loadTime time.Time // Host time when LR was written
	lr       uint32    // Load Register value
	mr       uint32    // Match Register
	cr       uint32    // Control Register
	imsc     uint32    // Interrupt Mask
	ris      uint32    // Raw Interrupt Status

	irqLine chipset.LineInterrupt
}

// New creates a new PL031 RTC at the given base address.
func New(base uint64, irqLine chipset.LineInterrupt) *PL031 {
	if irqLine == nil {
		irqLine = chipset.LineInterruptDetached()
	}
	p := &PL031{
		base:     base,
		size:     DefaultSize,
		loadTime: time.Now(),
		lr:       uint32(time.Now().Unix()),
		cr:       PL031_CR_EN, // RTC enabled by default
		irqLine:  irqLine,
	}
	return p
}

// NewDefault creates a new PL031 RTC at the default base address.
func NewDefault(irqLine chipset.LineInterrupt) *PL031 {
	return New(DefaultBase, irqLine)
}

// Init implements hv.Device.
func (p *PL031) Init(vm hv.VirtualMachine) error {
	return nil
}

// Start implements chipset.ChangeDeviceState.
func (p *PL031) Start() error {
	return nil
}

// Stop implements chipset.ChangeDeviceState.
func (p *PL031) Stop() error {
	return nil
}

// Reset implements chipset.ChangeDeviceState.
func (p *PL031) Reset() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.loadTime = time.Now()
	p.lr = uint32(time.Now().Unix())
	p.mr = 0
	p.cr = PL031_CR_EN
	p.imsc = 0
	p.ris = 0
	p.updateInterrupt()
	return nil
}

// SupportsPortIO implements chipset.ChipsetDevice.
func (p *PL031) SupportsPortIO() *chipset.PortIOIntercept {
	return nil
}

// SupportsMmio implements chipset.ChipsetDevice.
func (p *PL031) SupportsMmio() *chipset.MmioIntercept {
	return &chipset.MmioIntercept{
		Regions: []hv.MMIORegion{
			{
				Address: p.base,
				Size:    p.size,
			},
		},
		Handler: p,
	}
}

// SupportsPollDevice implements chipset.ChipsetDevice.
func (p *PL031) SupportsPollDevice() *chipset.PollDevice {
	return nil
}

// currentTime returns the current RTC counter value based on the load time.
func (p *PL031) currentTime() uint32 {
	if p.cr&PL031_CR_EN == 0 {
		// When disabled, counter doesn't advance
		return p.lr
	}
	elapsed := time.Since(p.loadTime)
	return p.lr + uint32(elapsed.Seconds())
}

// ReadMMIO implements chipset.MmioHandler.
func (p *PL031) ReadMMIO(ctx hv.ExitContext, addr uint64, data []byte) error {
	if addr < p.base || addr+uint64(len(data)) > p.base+p.size {
		return fmt.Errorf("pl031: address 0x%x out of bounds", addr)
	}

	offset := addr - p.base

	// Handle multi-byte reads
	for i := range data {
		val, err := p.readRegister(offset + uint64(i))
		if err != nil {
			return err
		}
		data[i] = val
	}
	return nil
}

// WriteMMIO implements chipset.MmioHandler.
func (p *PL031) WriteMMIO(ctx hv.ExitContext, addr uint64, data []byte) error {
	if addr < p.base || addr+uint64(len(data)) > p.base+p.size {
		return fmt.Errorf("pl031: address 0x%x out of bounds", addr)
	}

	offset := addr - p.base

	// Handle 4-byte aligned writes
	if len(data) == 4 && offset%4 == 0 {
		value := binary.LittleEndian.Uint32(data)
		return p.writeRegister(offset, value)
	}

	// Handle byte-wise writes (less common)
	for i := range data {
		if err := p.writeRegister(offset+uint64(i), uint32(data[i])); err != nil {
			return err
		}
	}
	return nil
}

// readRegister reads a single byte from the given register offset.
func (p *PL031) readRegister(offset uint64) (byte, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Calculate which 32-bit register and byte within it
	regOffset := offset & ^uint64(3)
	byteIndex := offset & 3

	var value uint32
	switch regOffset {
	case PL031_DR:
		value = p.currentTime()
	case PL031_MR:
		value = p.mr
	case PL031_LR:
		value = p.lr
	case PL031_CR:
		value = p.cr
	case PL031_IMSC:
		value = p.imsc
	case PL031_RIS:
		// Check if current time matches match register
		if p.currentTime() >= p.mr && p.mr != 0 {
			value = 1
		}
	case PL031_MIS:
		// Masked interrupt status
		if p.currentTime() >= p.mr && p.mr != 0 && p.imsc&1 != 0 {
			value = 1
		}
	case PL031_ICR:
		value = 0 // Write-only register reads as 0

	// PrimeCell peripheral identification
	case PL031_PERIPH_ID0:
		value = 0x31 // Part number[7:0]
	case PL031_PERIPH_ID1:
		value = 0x10 // Part number[11:8], Designer[3:0]
	case PL031_PERIPH_ID2:
		value = 0x04 // Revision, Designer[7:4]
	case PL031_PERIPH_ID3:
		value = 0x00 // Configuration

	// PrimeCell ID
	case PL031_PCELL_ID0:
		value = 0x0D
	case PL031_PCELL_ID1:
		value = 0xF0
	case PL031_PCELL_ID2:
		value = 0x05
	case PL031_PCELL_ID3:
		value = 0xB1

	default:
		value = 0
	}

	return byte(value >> (byteIndex * 8)), nil
}

// writeRegister writes a 32-bit value to the given register offset.
func (p *PL031) writeRegister(offset uint64, value uint32) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	switch offset {
	case PL031_MR:
		p.mr = value
	case PL031_LR:
		p.lr = value
		p.loadTime = time.Now()
	case PL031_CR:
		p.cr = value
	case PL031_IMSC:
		p.imsc = value
		p.updateInterrupt()
	case PL031_ICR:
		// Clear interrupt
		p.ris &^= value
		p.updateInterrupt()
	}
	return nil
}

// updateInterrupt updates the interrupt line based on current state.
func (p *PL031) updateInterrupt() {
	// Interrupt is asserted when RIS bit is set and mask is enabled
	asserted := p.ris&p.imsc&1 != 0
	p.irqLine.SetLevel(asserted)
}

// SetIRQLine configures the interrupt line.
func (p *PL031) SetIRQLine(line chipset.LineInterrupt) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.irqLine = line
}

// Base returns the MMIO base address.
func (p *PL031) Base() uint64 {
	return p.base
}

// Size returns the MMIO region size.
func (p *PL031) Size() uint64 {
	return p.size
}

var (
	_ hv.Device                 = (*PL031)(nil)
	_ chipset.ChipsetDevice     = (*PL031)(nil)
	_ chipset.MmioHandler       = (*PL031)(nil)
	_ chipset.ChangeDeviceState = (*PL031)(nil)
)
