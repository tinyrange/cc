package serial

import (
	"fmt"
	"io"

	"github.com/tinyrange/cc/internal/hv"
)

const (
	// UART8250DefaultClock is the reference clock used by the guest UART.
	UART8250DefaultClock = 1843200
	// UART8250MMIOSize reserves a 4 KiB region for the UART registers.
	UART8250MMIOSize = 0x1000

	uartRegisterCount = 8

	uartLCRDLAB = 1 << 7
	uartMCRLoop = 1 << 4

	uartLSRDataReady = 1 << 0
	uartLSRTHRE      = 1 << 5
	uartLSRTEMT      = 1 << 6
)

// UART8250MMIO implements a minimal 16550-compatible UART exposed via MMIO.
type UART8250MMIO struct {
	vm      hv.VirtualMachine
	base    uint64
	stride  uint64
	irqLine uint32
	out     io.Writer

	dll       byte
	dlm       byte
	ier       byte
	fcr       byte
	lcr       byte
	mcr       byte
	lsr       byte
	msrStatus byte
	msrDelta  byte
	scr       byte
	rbr       byte

	pendingIIR  byte
	fifoEnabled bool
	skipLF      bool
}

// NewUART8250MMIO builds a UART with the supplied base address. regShift controls
// the spacing of successive registers (stride = 1 << regShift).
func NewUART8250MMIO(base uint64, regShift uint32, irqLine uint32, out io.Writer) *UART8250MMIO {
	stride := uint64(1) << regShift
	if stride == 0 {
		stride = 1
	}
	return &UART8250MMIO{
		base:       base,
		stride:     stride,
		irqLine:    irqLine,
		out:        out,
		lsr:        uartLSRTHRE | uartLSRTEMT,
		pendingIIR: 0x01,
	}
}

// Init implements hv.Device.
func (s *UART8250MMIO) Init(vm hv.VirtualMachine) error {
	if vm == nil {
		return fmt.Errorf("uart8250-mmio: virtual machine is nil")
	}
	s.vm = vm
	s.updateModemStatus()
	s.updateInterrupts()
	return nil
}

// MMIORegions implements hv.MemoryMappedIODevice.
func (s *UART8250MMIO) MMIORegions() []hv.MMIORegion {
	return []hv.MMIORegion{{
		Address: s.base,
		Size:    UART8250MMIOSize,
	}}
}

// ReadMMIO implements hv.MemoryMappedIODevice.
func (s *UART8250MMIO) ReadMMIO(ctx hv.ExitContext, addr uint64, data []byte) error {
	for i := range data {
		val, _ := s.readByte(ctx, addr+uint64(i))
		data[i] = val
	}
	return nil
}

// WriteMMIO implements hv.MemoryMappedIODevice.
func (s *UART8250MMIO) WriteMMIO(ctx hv.ExitContext, addr uint64, data []byte) error {
	for i := range data {
		s.writeByte(ctx, addr+uint64(i), data[i])
	}
	return nil
}

func (s *UART8250MMIO) readByte(ctx hv.ExitContext, addr uint64) (byte, bool) {
	if addr < s.base || addr >= s.base+UART8250MMIOSize {
		return 0, false
	}
	offset := addr - s.base
	if offset%s.stride != 0 {
		return 0, true
	}
	reg := offset / s.stride
	if reg >= uartRegisterCount {
		return 0, false
	}
	return s.readRegister(uint16(reg)), true
}

func (s *UART8250MMIO) writeByte(ctx hv.ExitContext, addr uint64, value byte) {
	if addr < s.base || addr >= s.base+UART8250MMIOSize {
		return
	}
	offset := addr - s.base
	if offset%s.stride != 0 {
		return
	}
	reg := offset / s.stride
	if reg >= uartRegisterCount {
		return
	}
	s.writeRegister(uint16(reg), value)
}

func (s *UART8250MMIO) writeRegister(offset uint16, value byte) {
	switch offset {
	case 0:
		if s.lcr&uartLCRDLAB != 0 {
			s.dll = value
		} else {
			s.lsr &^= uartLSRTHRE
			s.updateInterrupts()
			s.transmit(value)
		}
	case 1:
		if s.lcr&uartLCRDLAB != 0 {
			s.dlm = value
		} else {
			s.setIER(value)
		}
	case 2:
		s.setFCR(value)
	case 3:
		s.lcr = value
	case 4:
		s.setMCR(value)
	case 5:
	case 6:
	case 7:
		s.scr = value
	}
}

type irqSetter interface {
	SetIRQ(line uint32, level bool) error
}

func (s *UART8250MMIO) setIER(value byte) {
	s.ier = value & 0x0F
	s.updateInterrupts()
}

func (s *UART8250MMIO) updateInterrupts() {
	interrupt := byte(0x01)

	switch {
	case s.ier&0x04 != 0 && (s.lsr&0x1E) != 0:
		interrupt = 0x06
	case s.ier&0x01 != 0 && s.lsr&uartLSRDataReady != 0:
		interrupt = 0x04
	case s.ier&0x02 != 0 && s.lsr&uartLSRTHRE != 0:
		interrupt = 0x02
	case s.ier&0x08 != 0 && s.msrDelta != 0:
		interrupt = 0x00
	}

	s.pendingIIR = interrupt

	if s.vm == nil || s.irqLine == 0 {
		return
	}
	if setter, ok := s.vm.(irqSetter); ok {
		_ = setter.SetIRQ(s.irqLine, interrupt != 0x01)
	}
}

func (s *UART8250MMIO) readRegister(offset uint16) byte {
	switch offset {
	case 0:
		if s.lcr&uartLCRDLAB != 0 {
			return s.dll
		}
		value := s.rbr
		s.rbr = 0
		s.lsr &^= uartLSRDataReady
		return value
	case 1:
		if s.lcr&uartLCRDLAB != 0 {
			return s.dlm
		}
		return s.ier
	case 2:
		return s.interruptIdentification()
	case 3:
		return s.lcr
	case 4:
		return s.mcr
	case 5:
		return s.lsr
	case 6:
		return s.modemStatus()
	case 7:
		return s.scr
	default:
		return 0
	}
}

func (s *UART8250MMIO) interruptIdentification() byte {
	return s.pendingIIR
}

func (s *UART8250MMIO) transmit(value byte) {
	if s.mcr&uartMCRLoop != 0 {
		s.rbr = value
		s.lsr |= uartLSRDataReady
	} else if s.out != nil {
		switch value {
		case '\r':
			_, _ = s.out.Write([]byte{'\n'})
			s.skipLF = true
		case '\n':
			if s.skipLF {
				s.skipLF = false
				break
			}
			_, _ = s.out.Write([]byte{'\n'})
		default:
			s.skipLF = false
			_, _ = s.out.Write([]byte{value})
		}
	}
	s.lsr |= uartLSRTHRE | uartLSRTEMT
	s.updateInterrupts()
}

func (s *UART8250MMIO) clearRX() {
	s.rbr = 0
	s.lsr &^= uartLSRDataReady
	s.updateInterrupts()
}

func (s *UART8250MMIO) setFCR(value byte) {
	if value&0x02 != 0 {
		s.clearRX()
	}
	s.fcr = value
	s.fifoEnabled = value&0x01 != 0
}

func (s *UART8250MMIO) setMCR(value byte) {
	prev := s.mcr
	s.mcr = value & 0x1F

	if prev&uartMCRLoop != 0 && s.mcr&uartMCRLoop == 0 {
		s.clearRX()
	}

	s.updateModemStatus()
	s.updateInterrupts()
}

func (s *UART8250MMIO) modemStatus() byte {
	value := s.msrStatus | s.msrDelta
	s.msrDelta = 0
	return value
}

func (s *UART8250MMIO) updateModemStatus() {
	const (
		bitCTS = 1 << 4
		bitDSR = 1 << 5
		bitRI  = 1 << 6
		bitDCD = 1 << 7
	)
	s.msrStatus = bitCTS | bitDSR | bitDCD
	if s.mcr&0x04 != 0 {
		s.msrStatus |= bitRI
	}
}

// UART8250Template is an hv.DeviceTemplate for the MMIO UART.
type UART8250Template struct {
	Base     uint64
	RegShift uint32
	IRQLine  uint32
	Out      io.Writer
}

// Create implements hv.DeviceTemplate.
func (t UART8250Template) Create(vm hv.VirtualMachine) (hv.Device, error) {
	dev := NewUART8250MMIO(t.Base, t.RegShift, t.IRQLine, t.Out)
	if err := dev.Init(vm); err != nil {
		return nil, err
	}
	return dev, nil
}

var (
	_ hv.MemoryMappedIODevice = (*UART8250MMIO)(nil)
	_ hv.DeviceTemplate       = UART8250Template{}
)
