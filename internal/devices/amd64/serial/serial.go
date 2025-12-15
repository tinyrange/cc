package serial

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/tinyrange/cc/internal/chipset"
	"github.com/tinyrange/cc/internal/hv"
)

const (
	serialRegisterCount = 8

	serialLCRDLAB = 1 << 7
	serialMCRLoop = 1 << 4

	serialLSRDataReady = 1 << 0
	serialLSRTHRE      = 1 << 5
	serialLSRTEMT      = 1 << 6

	// MCR bits
	mcrDTR     = 1 << 0
	mcrRTS     = 1 << 1
	mcrOUT1    = 1 << 2
	mcrOUT2    = 1 << 3 // Interrupt gate
	mcrLoop    = 1 << 4

	// MSR bits (low 4 bits are change flags, high 4 bits are status)
	msrDeltaCTS = 1 << 0
	msrDeltaDSR = 1 << 1
	msrDeltaRI  = 1 << 2 // Trailing edge
	msrDeltaDCD = 1 << 3
	msrCTS      = 1 << 4
	msrDSR      = 1 << 5
	msrRI       = 1 << 6
	msrDCD      = 1 << 7

	// FCR trigger levels (bits 6-7)
	fcrTrigger1  = 0x00
	fcrTrigger4  = 0x40
	fcrTrigger8  = 0x80
	fcrTrigger14 = 0xC0

	// FIFO sizes
	fifoSize = 16
)

type serialStats struct {
	txBytes uint64
	rxBytes uint64
	txIRQs  uint64
	rxIRQs  uint64
}

type Serial16550 struct {
	mu sync.Mutex

	vm      hv.VirtualMachine
	base    uint16
	irqLine chipset.LineInterrupt
	out     io.Writer
	in      io.Reader

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

	// FIFO buffers
	rxFIFO    [fifoSize]byte
	rxFIFOHead int
	rxFIFOTail int
	rxFIFOCount int
	txFIFO    [fifoSize]byte
	txFIFOHead int
	txFIFOTail int
	txFIFOCount int

	pendingIIR  byte
	fifoEnabled bool
	fifoTrigger int // Trigger level: 1, 4, 8, or 14
	skipLF      bool

	stats serialStats
}

// NewSerial16550 creates a new 16550 UART device.
func NewSerial16550(base uint16, irqLine chipset.LineInterrupt, out io.Writer, in io.Reader) *Serial16550 {
	if irqLine == nil {
		irqLine = chipset.LineInterruptDetached()
	}
	return &Serial16550{
		base:        base,
		irqLine:     irqLine,
		out:         out,
		in:          in,
		lsr:         serialLSRTHRE | serialLSRTEMT,
		pendingIIR:  0x01,
		fifoTrigger: 1, // Default trigger level
	}
}

// NewSerial16550WithIRQ creates a new 16550 UART device with legacy IRQ line support.
// This is a convenience function for backward compatibility that creates a LineInterrupt
// wrapper around the VM's SetIRQ method.
func NewSerial16550WithIRQ(base uint16, irqLineNum uint32, out io.Writer) *Serial16550 {
	var irqLine chipset.LineInterrupt
	if irqLineNum != 0 {
		// Create a LineInterrupt that will use VM.SetIRQ when VM is initialized
		irqLine = &vmIRQLine{irqNum: irqLineNum}
	}
	return NewSerial16550(base, irqLine, out, nil)
}

// vmIRQLine is a LineInterrupt implementation that uses VM.SetIRQ.
type vmIRQLine struct {
	mu     sync.Mutex
	vm     hv.VirtualMachine
	irqNum uint32
}

func (v *vmIRQLine) SetLevel(level bool) {
	v.mu.Lock()
	vm := v.vm
	irqNum := v.irqNum
	v.mu.Unlock()
	if vm != nil && irqNum != 0 {
		if vm64, ok := vm.(hv.VirtualMachineAmd64); ok {
			_ = vm64.SetIRQ(irqNum, level)
		}
	}
}

func (v *vmIRQLine) PulseInterrupt() {
	v.SetLevel(true)
	v.SetLevel(false)
}

func (v *vmIRQLine) setVM(vm hv.VirtualMachine) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.vm = vm
}

// Init implements hv.Device.
func (s *Serial16550) Init(vm hv.VirtualMachine) error {
	if _, ok := vm.(hv.VirtualMachineAmd64); !ok {
		return fmt.Errorf("serial16550: vm does not implement hv.VirtualMachineAmd64")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.vm = vm
	
	// If irqLine is a vmIRQLine, set its VM reference
	if vmIRQ, ok := s.irqLine.(*vmIRQLine); ok {
		vmIRQ.setVM(vm)
	}
	
	s.updateModemStatusLocked()
	return nil
}

// Start implements chipset.ChangeDeviceState.
func (s *Serial16550) Start() error {
	return nil
}

// Stop implements chipset.ChangeDeviceState.
func (s *Serial16550) Stop() error {
	return nil
}

// Reset implements chipset.ChangeDeviceState.
func (s *Serial16550) Reset() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.dll = 0
	s.dlm = 0
	s.ier = 0
	s.fcr = 0
	s.lcr = 0
	s.mcr = 0
	s.lsr = serialLSRTHRE | serialLSRTEMT
	s.msrStatus = msrCTS | msrDSR | msrDCD
	s.msrDelta = 0
	s.scr = 0

	s.rxFIFOHead = 0
	s.rxFIFOTail = 0
	s.rxFIFOCount = 0
	s.txFIFOHead = 0
	s.txFIFOTail = 0
	s.txFIFOCount = 0

	s.pendingIIR = 0x01
	s.fifoEnabled = false
	s.fifoTrigger = 1
	s.skipLF = false

	return nil
}

// SupportsPortIO implements chipset.ChipsetDevice.
func (s *Serial16550) SupportsPortIO() *chipset.PortIOIntercept {
	ports := make([]uint16, serialRegisterCount)
	for i := range uint16(serialRegisterCount) {
		ports[i] = s.base + i
	}
	return &chipset.PortIOIntercept{
		Ports:   ports,
		Handler: s,
	}
}

// SupportsMmio implements chipset.ChipsetDevice.
func (s *Serial16550) SupportsMmio() *chipset.MmioIntercept {
	return nil
}

// SupportsPollDevice implements chipset.ChipsetDevice.
func (s *Serial16550) SupportsPollDevice() *chipset.PollDevice {
	return &chipset.PollDevice{
		Handler: s,
	}
}

// Poll implements chipset.PollHandler for async TX/RX.
func (s *Serial16550) Poll(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check for incoming data
	if s.in != nil && s.rxFIFOCount < fifoSize {
		buf := make([]byte, 1)
		n, err := s.in.Read(buf)
		if n > 0 && err == nil {
			s.rxByteLocked(buf[0])
		}
	}

	// Process TX FIFO
	if s.txFIFOCount > 0 {
		s.processTXFIFOLocked()
	}

	return nil
}

// ReadIOPort implements chipset.PortIOHandler.
func (s *Serial16550) ReadIOPort(port uint16, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range data {
		data[i] = s.readRegisterLocked(port)
	}
	return nil
}

// WriteIOPort implements chipset.PortIOHandler.
func (s *Serial16550) WriteIOPort(port uint16, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, value := range data {
		s.writeRegisterLocked(port, value)
	}
	return nil
}

func (s *Serial16550) writeRegisterLocked(port uint16, value byte) {
	if port < s.base || port >= s.base+serialRegisterCount {
		return
	}

	offset := port - s.base
	switch offset {
	case 0:
		if s.lcr&serialLCRDLAB != 0 {
			s.dll = value
		} else {
			s.writeTXByteLocked(value)
		}
	case 1:
		if s.lcr&serialLCRDLAB != 0 {
			s.dlm = value
		} else {
			s.setIERLocked(value)
		}
	case 2:
		s.setFCRLocked(value)
	case 3:
		s.lcr = value
	case 4:
		s.setMCRLocked(value)
	case 5:
		// Factory Test (Write to LSR) - usually ignored or resets LSR
	case 6:
		// MSR is read-only
	case 7:
		s.scr = value
	}
}

func (s *Serial16550) setIERLocked(value byte) {
	s.ier = value & 0x0F
	s.updateInterruptsLocked()
}

func (s *Serial16550) readRegisterLocked(port uint16) byte {
	if port < s.base || port >= s.base+serialRegisterCount {
		return 0
	}

	offset := port - s.base
	switch offset {
	case 0:
		if s.lcr&serialLCRDLAB != 0 {
			return s.dll
		}
		return s.readRXByteLocked()
	case 1:
		if s.lcr&serialLCRDLAB != 0 {
			return s.dlm
		}
		return s.ier
	case 2:
		return s.interruptIdentificationLocked()
	case 3:
		return s.lcr
	case 4:
		return s.mcr
	case 5:
		return s.lsr
	case 6:
		return s.modemStatusLocked()
	case 7:
		return s.scr
	default:
		return 0
	}
}

func (s *Serial16550) updateInterruptsLocked() {
	interrupt := byte(0x01)

	switch {
	case s.ier&0x04 != 0 && (s.lsr&0x1E) != 0:
		// Line status interrupt (priority 1)
		interrupt = 0x06
	case s.ier&0x01 != 0 && s.hasRXDataLocked():
		// RX data available (priority 2)
		interrupt = 0x04
	case s.ier&0x02 != 0 && s.lsr&serialLSRTHRE != 0:
		// TX holding register empty (priority 3)
		interrupt = 0x02
	case s.ier&0x08 != 0 && s.msrDelta != 0:
		// Modem status change (priority 4)
		interrupt = 0x00
	}

	s.pendingIIR = interrupt

	// OUT2 gates interrupts: interrupt only asserted if OUT2 is high
	isAsserted := (interrupt != 0x01) && (s.mcr&mcrOUT2 != 0)
	s.irqLine.SetLevel(isAsserted)
}

func (s *Serial16550) writeTXByteLocked(value byte) {
	if s.fifoEnabled {
		// Add to TX FIFO
		if s.txFIFOCount < fifoSize {
			s.txFIFO[s.txFIFOTail] = value
			s.txFIFOTail = (s.txFIFOTail + 1) % fifoSize
			s.txFIFOCount++
			s.stats.txBytes++
		}
		// Clear THRE if FIFO is full
		if s.txFIFOCount >= fifoSize {
			s.lsr &^= serialLSRTHRE
		} else {
			s.lsr |= serialLSRTHRE
		}
		s.lsr &^= serialLSRTEMT
	} else {
		// Non-FIFO mode: immediate transmission
		s.lsr &^= serialLSRTHRE
		s.transmitByteLocked(value)
		s.lsr |= serialLSRTHRE | serialLSRTEMT
	}
	s.updateInterruptsLocked()
}

func (s *Serial16550) processTXFIFOLocked() {
	for s.txFIFOCount > 0 {
		value := s.txFIFO[s.txFIFOHead]
		s.txFIFOHead = (s.txFIFOHead + 1) % fifoSize
		s.txFIFOCount--
		s.transmitByteLocked(value)
	}
	s.lsr |= serialLSRTHRE | serialLSRTEMT
	s.updateInterruptsLocked()
}

func (s *Serial16550) transmitByteLocked(value byte) {
	if s.mcr&mcrLoop != 0 {
		// Loopback mode: feed back to RX
		s.rxByteLocked(value)
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
		s.stats.txBytes++
	}
}

func (s *Serial16550) rxByteLocked(value byte) {
	if s.fifoEnabled {
		// Add to RX FIFO
		if s.rxFIFOCount < fifoSize {
			s.rxFIFO[s.rxFIFOTail] = value
			s.rxFIFOTail = (s.rxFIFOTail + 1) % fifoSize
			s.rxFIFOCount++
			s.stats.rxBytes++

			// Check trigger level
			if s.rxFIFOCount >= s.fifoTrigger {
				s.lsr |= serialLSRDataReady
				s.updateInterruptsLocked()
			}
		} else {
			// FIFO overflow - set overrun error
			s.lsr |= 1 << 1 // Overrun error bit
		}
	} else {
		// Non-FIFO mode
		if s.lsr&serialLSRDataReady != 0 {
			// Overrun
			s.lsr |= 1 << 1
		} else {
			s.rxFIFO[0] = value
			s.lsr |= serialLSRDataReady
			s.stats.rxBytes++
		}
		s.updateInterruptsLocked()
	}
}

func (s *Serial16550) readRXByteLocked() byte {
	if s.fifoEnabled {
		if s.rxFIFOCount == 0 {
			return 0
		}
		value := s.rxFIFO[s.rxFIFOHead]
		s.rxFIFOHead = (s.rxFIFOHead + 1) % fifoSize
		s.rxFIFOCount--

		// Update LSR based on FIFO state
		if s.rxFIFOCount == 0 {
			s.lsr &^= serialLSRDataReady
		} else if s.rxFIFOCount >= s.fifoTrigger {
			// Still above trigger level
			s.lsr |= serialLSRDataReady
		}

		s.updateInterruptsLocked()
		return value
	} else {
		// Non-FIFO mode
		value := s.rxFIFO[0]
		s.rxFIFO[0] = 0
		s.lsr &^= serialLSRDataReady
		s.updateInterruptsLocked()
		return value
	}
}

func (s *Serial16550) hasRXDataLocked() bool {
	if s.fifoEnabled {
		return s.rxFIFOCount > 0
	}
	return s.lsr&serialLSRDataReady != 0
}

func (s *Serial16550) setFCRLocked(value byte) {
	if value&0x02 != 0 {
		// Clear RX FIFO
		s.rxFIFOHead = 0
		s.rxFIFOTail = 0
		s.rxFIFOCount = 0
		s.lsr &^= serialLSRDataReady
	}
	if value&0x04 != 0 {
		// Clear TX FIFO
		s.txFIFOHead = 0
		s.txFIFOTail = 0
		s.txFIFOCount = 0
		s.lsr |= serialLSRTHRE | serialLSRTEMT
	}

	s.fcr = value
	s.fifoEnabled = value&0x01 != 0

	// Set trigger level
	switch value & 0xC0 {
	case fcrTrigger1:
		s.fifoTrigger = 1
	case fcrTrigger4:
		s.fifoTrigger = 4
	case fcrTrigger8:
		s.fifoTrigger = 8
	case fcrTrigger14:
		s.fifoTrigger = 14
	default:
		s.fifoTrigger = 1
	}

	s.updateInterruptsLocked()
}

func (s *Serial16550) setMCRLocked(value byte) {
	prev := s.mcr
	s.mcr = value & 0x1F

	if prev&mcrLoop != 0 && s.mcr&mcrLoop == 0 {
		// Exiting loopback mode
		s.rxFIFOHead = 0
		s.rxFIFOTail = 0
		s.rxFIFOCount = 0
		s.lsr &^= serialLSRDataReady
	}

	s.updateModemStatusLocked()
	s.updateInterruptsLocked() // OUT2 change affects interrupts
}

func (s *Serial16550) modemStatusLocked() byte {
	value := s.msrStatus | s.msrDelta
	s.msrDelta = 0
	s.updateInterruptsLocked()
	return value
}

func (s *Serial16550) updateModemStatusLocked() {
	// Update status bits based on MCR (in loopback mode) or external signals
	if s.mcr&mcrLoop != 0 {
		// Loopback: status reflects control signals
		if s.mcr&mcrDTR != 0 {
			s.msrStatus |= msrDSR
		} else {
			s.msrStatus &^= msrDSR
		}
		if s.mcr&mcrRTS != 0 {
			s.msrStatus |= msrCTS
		} else {
			s.msrStatus &^= msrCTS
		}
		if s.mcr&mcrOUT1 != 0 {
			s.msrStatus |= msrRI
		} else {
			s.msrStatus &^= msrRI
		}
		if s.mcr&mcrOUT2 != 0 {
			s.msrStatus |= msrDCD
		} else {
			s.msrStatus &^= msrDCD
		}
	} else {
		// Normal mode: assume all signals are asserted (typical for virtual serial)
		s.msrStatus = msrCTS | msrDSR | msrDCD
		if s.mcr&mcrOUT1 != 0 {
			s.msrStatus |= msrRI
		}
	}
}

func (s *Serial16550) interruptIdentificationLocked() byte {
	return s.pendingIIR
}

// SetIRQLine configures the LineInterrupt used for IRQ delivery.
func (s *Serial16550) SetIRQLine(line chipset.LineInterrupt) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if line == nil {
		s.irqLine = chipset.LineInterruptDetached()
		return
	}
	s.irqLine = line
}

// Stats returns current statistics.
func (s *Serial16550) Stats() serialStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stats
}

var (
	_ hv.Device                = &Serial16550{}
	_ chipset.ChipsetDevice    = &Serial16550{}
	_ chipset.PortIOHandler    = &Serial16550{}
	_ chipset.PollHandler      = &Serial16550{}
	_ chipset.ChangeDeviceState = &Serial16550{}
)
