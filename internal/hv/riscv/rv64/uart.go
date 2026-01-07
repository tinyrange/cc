package rv64

import (
	"io"
)

// UART register offsets (16550 compatible)
const (
	UARTRegRBR = 0 // Receive Buffer Register (read)
	UARTRegTHR = 0 // Transmit Holding Register (write)
	UARTRegIER = 1 // Interrupt Enable Register
	UARTRegIIR = 2 // Interrupt Identification Register (read)
	UARTRegFCR = 2 // FIFO Control Register (write)
	UARTRegLCR = 3 // Line Control Register
	UARTRegMCR = 4 // Modem Control Register
	UARTRegLSR = 5 // Line Status Register
	UARTRegMSR = 6 // Modem Status Register
	UARTRegSCR = 7 // Scratch Register
)

// LSR bits
const (
	UARTLSRDataReady        = 1 << 0 // Data ready
	UARTLSROverrunError     = 1 << 1 // Overrun error
	UARTLSRParityError      = 1 << 2 // Parity error
	UARTLSRFramingError     = 1 << 3 // Framing error
	UARTLSRBreakInterrupt   = 1 << 4 // Break interrupt
	UARTLSRTHREmpty         = 1 << 5 // Transmit holding register empty
	UARTLSRTxEmpty          = 1 << 6 // Transmitter empty
	UARTLSRFIFOError        = 1 << 7 // FIFO error
)

// IIR bits
const (
	UARTIIRNoInterrupt = 1 << 0 // No interrupt pending
)

// UART implements a simple 16550-compatible UART
type UART struct {
	Output io.Writer
	Input  io.Reader

	// Registers
	RBR uint8 // Receive buffer
	IER uint8 // Interrupt enable
	IIR uint8 // Interrupt identification
	FCR uint8 // FIFO control
	LCR uint8 // Line control
	MCR uint8 // Modem control
	LSR uint8 // Line status
	MSR uint8 // Modem status
	SCR uint8 // Scratch

	// DLAB registers
	DLL uint8 // Divisor latch low
	DLH uint8 // Divisor latch high

	// Input buffer
	inputBuffer []byte
	inputPos    int

	// Interrupt pending
	InterruptPending bool

	// Interrupt callback
	OnInterrupt func(pending bool)
}

// NewUART creates a new UART device
func NewUART(output io.Writer, input io.Reader) *UART {
	return &UART{
		Output: output,
		Input:  input,
		LSR:    UARTLSRTHREmpty | UARTLSRTxEmpty, // TX ready
		IIR:    UARTIIRNoInterrupt,               // No interrupt pending
	}
}

// Size implements Device
func (uart *UART) Size() uint64 {
	return UARTSize
}

// Read implements Device
func (uart *UART) Read(offset uint64, size int) (uint64, error) {
	if size != 1 {
		return 0, nil
	}

	// Check DLAB bit
	dlab := (uart.LCR & 0x80) != 0

	switch offset {
	case UARTRegRBR:
		if dlab {
			return uint64(uart.DLL), nil
		}
		// Read from receive buffer
		uart.updateInput()
		data := uart.RBR
		if len(uart.inputBuffer) > 0 && uart.inputPos < len(uart.inputBuffer) {
			data = uart.inputBuffer[uart.inputPos]
			uart.inputPos++
			if uart.inputPos >= len(uart.inputBuffer) {
				uart.inputBuffer = nil
				uart.inputPos = 0
			}
		}
		uart.updateLSR()
		return uint64(data), nil

	case UARTRegIER:
		if dlab {
			return uint64(uart.DLH), nil
		}
		return uint64(uart.IER), nil

	case UARTRegIIR:
		return uint64(uart.IIR), nil

	case UARTRegLCR:
		return uint64(uart.LCR), nil

	case UARTRegMCR:
		return uint64(uart.MCR), nil

	case UARTRegLSR:
		uart.updateInput()
		uart.updateLSR()
		return uint64(uart.LSR), nil

	case UARTRegMSR:
		return uint64(uart.MSR), nil

	case UARTRegSCR:
		return uint64(uart.SCR), nil
	}

	return 0, nil
}

// Write implements Device
func (uart *UART) Write(offset uint64, size int, value uint64) error {
	if size != 1 {
		return nil
	}

	data := uint8(value)

	// Check DLAB bit
	dlab := (uart.LCR & 0x80) != 0

	switch offset {
	case UARTRegTHR:
		if dlab {
			uart.DLL = data
			return nil
		}
		// Write to output
		if uart.Output != nil {
			uart.Output.Write([]byte{data})
		}

	case UARTRegIER:
		if dlab {
			uart.DLH = data
			return nil
		}
		uart.IER = data
		uart.updateInterrupt()

	case UARTRegFCR:
		uart.FCR = data
		if data&0x01 != 0 {
			// FIFO enable - clear FIFOs if requested
			if data&0x02 != 0 {
				uart.inputBuffer = nil
				uart.inputPos = 0
			}
		}

	case UARTRegLCR:
		uart.LCR = data

	case UARTRegMCR:
		uart.MCR = data

	case UARTRegSCR:
		uart.SCR = data
	}

	return nil
}

// updateInput tries to read more input if available
func (uart *UART) updateInput() {
	if uart.Input == nil {
		return
	}
	if len(uart.inputBuffer) > uart.inputPos {
		return // Already have data
	}

	// Note: Non-blocking input would need select/channels
	// For now, input must be pushed externally via PushInput
}

// updateLSR updates the line status register
func (uart *UART) updateLSR() {
	uart.LSR = UARTLSRTHREmpty | UARTLSRTxEmpty // TX always ready

	if len(uart.inputBuffer) > uart.inputPos {
		uart.LSR |= UARTLSRDataReady
	}
}

// updateInterrupt updates the interrupt status
func (uart *UART) updateInterrupt() {
	pending := false

	// Check for receive data available interrupt
	if (uart.IER&0x01) != 0 && len(uart.inputBuffer) > uart.inputPos {
		pending = true
		uart.IIR = 0x04 // Receive data available
	} else if (uart.IER & 0x02) != 0 {
		// THR empty interrupt (always ready)
		pending = true
		uart.IIR = 0x02 // THR empty
	} else {
		uart.IIR = UARTIIRNoInterrupt
	}

	if pending != uart.InterruptPending {
		uart.InterruptPending = pending
		if uart.OnInterrupt != nil {
			uart.OnInterrupt(pending)
		}
	}
}

// EnqueueInput adds input bytes to be read by the guest
func (uart *UART) EnqueueInput(data []byte) {
	uart.inputBuffer = append(uart.inputBuffer, data...)
	uart.updateLSR()
	uart.updateInterrupt()
}

var _ Device = (*UART)(nil)
