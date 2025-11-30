package serial

import (
	"fmt"
	"io"

	"github.com/tinyrange/cc/internal/hv"
)

const (
	serialRegisterCount = 8

	serialLCRDLAB = 1 << 7
	serialMCRLoop = 1 << 4

	serialLSRDataReady = 1 << 0
	serialLSRTHRE      = 1 << 5
	serialLSRTEMT      = 1 << 6
)

type Serial16550 struct {
	vm      hv.VirtualMachine
	base    uint16
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

// IOPorts implements hv.X86IOPortDevice.
func (s *Serial16550) IOPorts() []uint16 {
	ports := make([]uint16, serialRegisterCount)
	for i := range uint16(serialRegisterCount) {
		ports[i] = s.base + i
	}
	return ports
}

// ReadIOPort implements hv.X86IOPortDevice.
func (s *Serial16550) ReadIOPort(port uint16, data []byte) error {
	for i := range data {
		data[i] = s.readRegister(port)
	}
	return nil
}

// WriteIOPort implements hv.X86IOPortDevice.
func (s *Serial16550) WriteIOPort(port uint16, data []byte) error {
	for _, value := range data {
		s.writeRegister(port, value)
	}
	return nil
}

// Init implements hv.Device.
func (s *Serial16550) Init(vm hv.VirtualMachine) error {
	if _, ok := vm.(hv.VirtualMachineAmd64); !ok {
		return fmt.Errorf("serial16550: vm does not implement hv.VirtualMachineAmd64")
	}

	s.vm = vm
	s.updateModemStatus()
	return nil
}

func NewSerial16550(base uint16, irqLine uint32, out io.Writer) *Serial16550 {
	return &Serial16550{
		base:       base,
		irqLine:    irqLine,
		out:        out,
		lsr:        serialLSRTHRE | serialLSRTEMT,
		pendingIIR: 0x01,
	}
}

func (s *Serial16550) writeRegister(port uint16, value byte) {
	if port < s.base || port >= s.base+serialRegisterCount {
		return
	}

	offset := port - s.base
	switch offset {
	case 0:
		if s.lcr&serialLCRDLAB != 0 {
			s.dll = value
		} else {
			// FIX: Simulate the buffer becoming full before it becomes empty again.
			// 1. Clear THRE (Buffer Full)
			s.lsr &^= serialLSRTHRE
			// 2. Drop the IRQ Line (Update Interrupts sees THRE is gone)
			s.updateInterrupts()

			// 3. Perform Transmission (Instantly empties buffer)
			s.transmit(value)
		}
	case 1:
		if s.lcr&serialLCRDLAB != 0 {
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
		// Factory Test (Write to LSR) - usually ignored or resets LSR
	case 6:
		// MSR is read-only
	case 7:
		s.scr = value
	}
}

func (s *Serial16550) setIER(value byte) {
	s.ier = value & 0x0F
	s.updateInterrupts()
}

func (s *Serial16550) readRegister(port uint16) byte {
	if port < s.base || port >= s.base+serialRegisterCount {
		return 0
	}

	offset := port - s.base
	switch offset {
	case 0:
		if s.lcr&serialLCRDLAB != 0 {
			return s.dll
		}
		value := s.rbr
		s.rbr = 0
		s.lsr &^= serialLSRDataReady
		s.updateInterrupts()
		return value
	case 1:
		if s.lcr&serialLCRDLAB != 0 {
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

func (s *Serial16550) updateInterrupts() {
	interrupt := byte(0x01)

	switch {
	case s.ier&0x04 != 0 && (s.lsr&0x1E) != 0:
		interrupt = 0x06
	case s.ier&0x01 != 0 && s.lsr&serialLSRDataReady != 0:
		interrupt = 0x04
	case s.ier&0x02 != 0 && s.lsr&serialLSRTHRE != 0:
		interrupt = 0x02
	case s.ier&0x08 != 0 && s.msrDelta != 0:
		interrupt = 0x00
	}

	s.pendingIIR = interrupt

	if s.vm != nil && s.irqLine != 0 {
		isAsserted := (interrupt != 0x01)

		if vm, ok := s.vm.(hv.VirtualMachineAmd64); ok {
			_ = vm.SetIRQ(s.irqLine, isAsserted)
		}
	}
}

func (s *Serial16550) transmit(value byte) {
	if s.mcr&serialMCRLoop != 0 {
		s.rbr = value
		s.lsr |= serialLSRDataReady
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

	// 4. Set THRE (Buffer Empty)
	s.lsr |= serialLSRTHRE | serialLSRTEMT

	// 5. Raise the IRQ Line (Edge Trigger!)
	s.updateInterrupts()
}

func (s *Serial16550) clearRX() {
	s.rbr = 0
	s.lsr &^= serialLSRDataReady
	s.updateInterrupts()
}

func (s *Serial16550) setFCR(value byte) {
	if value&0x02 != 0 {
		s.clearRX()
	}
	s.fcr = value
	s.fifoEnabled = value&0x01 != 0
	s.updateInterrupts()
}

func (s *Serial16550) setMCR(value byte) {
	prev := s.mcr
	s.mcr = value & 0x1F

	if prev&serialMCRLoop != 0 && s.mcr&serialMCRLoop == 0 {
		s.clearRX()
	}

	s.updateModemStatus()
}

func (s *Serial16550) modemStatus() byte {
	value := s.msrStatus | s.msrDelta
	s.msrDelta = 0
	s.updateInterrupts()
	return value
}

func (s *Serial16550) updateModemStatus() {
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

func (s *Serial16550) interruptIdentification() byte {
	return s.pendingIIR
}

var (
	_ hv.Device          = &Serial16550{}
	_ hv.X86IOPortDevice = &Serial16550{}
)
