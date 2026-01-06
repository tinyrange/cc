package input

import (
	"fmt"
	"sync"

	"github.com/tinyrange/cc/internal/chipset"
	"github.com/tinyrange/cc/internal/hv"
)

const (
	i8042DataPort          = 0x60
	i8042SystemControlPort = 0x61 // Port B (Speaker/NMI/Timer)
	i8042CommandPort       = 0x64

	// Controller commands
	i8042CommandReadCommandByte  = 0x20
	i8042CommandWriteCommandByte = 0x60
	i8042CommandDisableAux       = 0xa7
	i8042CommandEnableAux        = 0xa8
	i8042CommandTestAux          = 0xa9
	i8042CommandControllerTest   = 0xaa
	i8042CommandTestKeyboard     = 0xab
	i8042CommandDisableKeyboard  = 0xad
	i8042CommandEnableKeyboard   = 0xae
	i8042CommandReadInputPort    = 0xc0
	i8042CommandReadOutputPort   = 0xd0
	i8042CommandWriteOutputPort  = 0xd1 // Used for A20 Gate
	i8042CommandWriteAux         = 0xd4
	i8042CommandPulseReset       = 0xfe
)

const (
	i8042StatusOutputFull  = 1 << 0 // Output buffer full
	i8042StatusInputFull   = 1 << 1 // Input buffer full
	i8042StatusSystemFlag  = 1 << 2 // System flag (0 = POST, 1 = normal)
	i8042StatusCommandData = 1 << 3 // 0 = data, 1 = command
	i8042StatusKeyLock     = 1 << 4 // Keyboard locked
	i8042StatusAuxData     = 1 << 5 // 0 = keyboard, 1 = aux (mouse)
	i8042StatusTimeout     = 1 << 6 // Timeout error
	i8042StatusParityError = 1 << 7 // Parity error
)

const (
	i8042CommandByteInterruptKeyboard = 1 << 0 // Enable keyboard interrupt (IRQ1)
	i8042CommandByteInterruptAux      = 1 << 1 // Enable aux interrupt (IRQ12)
	i8042CommandByteSystemFlag        = 1 << 2 // System flag
	i8042CommandByteOverride          = 1 << 3 // Override keyboard lock
	i8042CommandByteDisableKeyboard   = 1 << 4 // Disable keyboard clock
	i8042CommandByteDisableAux        = 1 << 5 // Disable aux clock
	i8042CommandByteTranslate         = 1 << 6 // Translate scancodes (set 1 -> set 2)
	i8042CommandByteReserved          = 1 << 7 // Reserved
)

const (
	i8042ResponseSelfTestOK = 0x55
	i8042ResponsePortOK     = 0x00
	i8042ResponsePortError  = 0x01
	i8042ResponseAck        = 0xfa
	i8042ResponseResend     = 0xfe
	i8042ResponseError      = 0xff
	i8042ResponseTestPassed = 0x00
	i8042ResponseTestFailed = 0x01
)

// OutputBufferState tracks what device owns the output buffer
type OutputBufferState uint8

const (
	OutputBufferEmpty OutputBufferState = iota
	OutputBufferController
	OutputBufferKeyboard
	OutputBufferMouse
)

// I8042 implements the i8042 keyboard controller.
type I8042 struct {
	mu sync.Mutex

	vm hv.VirtualMachine

	// Controller state
	commandByte             byte
	outputBuffer            byte
	outputBufferState       OutputBufferState
	expectingCommandByte    bool
	expectingOutputPortByte bool
	expectingAuxWrite       bool

	// Internal RAM (32 bytes)
	memory [32]byte

	// Port 0x61 state (System Control Port B)
	port61 byte

	// Interrupt lines
	irq1  chipset.LineInterrupt // Keyboard interrupt
	irq12 chipset.LineInterrupt // Mouse interrupt

	// Keyboard device (will be created)
	keyboard *PS2Keyboard
}

// NewI8042 creates a new i8042 controller.
func NewI8042() *I8042 {
	ctrl := &I8042{
		commandByte:       i8042CommandByteSystemFlag | i8042CommandByteInterruptKeyboard,
		outputBufferState: OutputBufferEmpty,
		irq1:              chipset.LineInterruptDetached(),
		irq12:             chipset.LineInterruptDetached(),
		keyboard:          NewPS2Keyboard(),
	}
	// Connect keyboard to controller
	ctrl.keyboard.SetController(ctrl)
	return ctrl
}

// SetKeyboardIRQ sets the interrupt line for keyboard (IRQ1).
func (c *I8042) SetKeyboardIRQ(line chipset.LineInterrupt) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if line == nil {
		c.irq1 = chipset.LineInterruptDetached()
	} else {
		c.irq1 = line
	}
	c.keyboard.SetIRQ(line)
}

// SetKeyboardIRQFromFunc sets the keyboard IRQ using a function adapter.
func (c *I8042) SetKeyboardIRQFromFunc(fn func(bool)) {
	c.SetKeyboardIRQ(chipset.LineInterruptFromFunc(fn))
}

// SetMouseIRQ sets the interrupt line for mouse (IRQ12).
func (c *I8042) SetMouseIRQ(line chipset.LineInterrupt) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if line == nil {
		c.irq12 = chipset.LineInterruptDetached()
	} else {
		c.irq12 = line
	}
}

// Init implements hv.Device.
func (c *I8042) Init(vm hv.VirtualMachine) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vm = vm
	c.resetLocked()
	return nil
}

// Start implements chipset.ChangeDeviceState.
func (c *I8042) Start() error {
	return nil
}

// Stop implements chipset.ChangeDeviceState.
func (c *I8042) Stop() error {
	return nil
}

// Reset implements chipset.ChangeDeviceState.
func (c *I8042) Reset() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.resetLocked()
	return nil
}

func (c *I8042) resetLocked() {
	c.commandByte = i8042CommandByteSystemFlag | i8042CommandByteInterruptKeyboard
	c.outputBuffer = 0
	c.outputBufferState = OutputBufferEmpty
	c.expectingCommandByte = false
	c.expectingOutputPortByte = false
	c.expectingAuxWrite = false
	c.port61 = 0
	for i := range c.memory {
		c.memory[i] = 0
	}
	c.keyboard.Reset()
}

// SupportsPortIO implements chipset.ChipsetDevice.
func (c *I8042) SupportsPortIO() *chipset.PortIOIntercept {
	// Port 0x61 (i8042SystemControlPort) is handled by chipset.Port61 for PIT integration.
	// We only register ports 0x60 (data) and 0x64 (command).
	return &chipset.PortIOIntercept{
		Ports:   []uint16{i8042DataPort, i8042CommandPort},
		Handler: c,
	}
}

// SupportsMmio implements chipset.ChipsetDevice.
func (c *I8042) SupportsMmio() *chipset.MmioIntercept {
	return nil
}

// SupportsPollDevice implements chipset.ChipsetDevice.
func (c *I8042) SupportsPollDevice() *chipset.PollDevice {
	return nil
}

// IOPorts implements hv.X86IOPortDevice (legacy support).
func (c *I8042) IOPorts() []uint16 {
	// Port 0x61 (i8042SystemControlPort) is handled by chipset.Port61 for PIT integration.
	return []uint16{i8042DataPort, i8042CommandPort}
}

// ReadIOPort implements chipset.PortIOHandler.
func (c *I8042) ReadIOPort(ctx hv.ExitContext, port uint16, data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	for i := range data {
		switch port {
		case i8042CommandPort:
			data[i] = c.statusLocked()
		case i8042DataPort:
			data[i] = c.readDataLocked()
		case i8042SystemControlPort:
			// Linux/Bios often loops reading this bit to check for timer refresh.
			// We toggle Bit 4 (0x10) on every read to simulate a running timer.
			// This prevents "Calibrating Delay Loop" hangs.
			c.port61 ^= 0x10
			data[i] = c.port61
		default:
			return fmt.Errorf("i8042: invalid read port 0x%04x", port)
		}
	}
	return nil
}

// WriteIOPort implements chipset.PortIOHandler.
func (c *I8042) WriteIOPort(ctx hv.ExitContext, port uint16, data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, value := range data {
		switch port {
		case i8042CommandPort:
			if err := c.handleCommandLocked(value); err != nil {
				return err
			}
		case i8042DataPort:
			if err := c.handleDataWriteLocked(value); err != nil {
				return err
			}
		case i8042SystemControlPort:
			// The OS writes here to control the PC Speaker or NMI gates.
			// We just store the value. Bit 4 (Refresh) is read-only on hardware,
			// but overwriting it here doesn't hurt since we toggle it on read anyway.
			c.port61 = value
		default:
			return fmt.Errorf("i8042: invalid write port 0x%04x", port)
		}
	}
	return nil
}

func (c *I8042) handleCommandLocked(command byte) error {
	// Reset data expectations when a new command arrives
	c.expectingCommandByte = false
	c.expectingOutputPortByte = false
	c.expectingAuxWrite = false

	switch command {
	case i8042CommandReadCommandByte:
		c.queueOutputLocked(c.commandByte, OutputBufferController)

	case i8042CommandWriteCommandByte:
		c.expectingCommandByte = true

	case i8042CommandDisableAux:
		c.commandByte |= i8042CommandByteDisableAux

	case i8042CommandEnableAux:
		c.commandByte &^= i8042CommandByteDisableAux

	case i8042CommandTestAux:
		// Test aux (mouse) port - return OK for now
		c.queueOutputLocked(i8042ResponsePortOK, OutputBufferController)

	case i8042CommandControllerTest:
		c.queueOutputLocked(i8042ResponseSelfTestOK, OutputBufferController)

	case i8042CommandTestKeyboard:
		// Test keyboard port
		if c.keyboard != nil {
			c.queueOutputLocked(i8042ResponsePortOK, OutputBufferController)
		} else {
			c.queueOutputLocked(i8042ResponsePortError, OutputBufferController)
		}

	case i8042CommandDisableKeyboard:
		c.commandByte |= i8042CommandByteDisableKeyboard

	case i8042CommandEnableKeyboard:
		c.commandByte &^= i8042CommandByteDisableKeyboard

	case i8042CommandReadInputPort:
		// Read input port (P2) - return 0xFF (no errors)
		c.queueOutputLocked(0xff, OutputBufferController)

	case i8042CommandReadOutputPort:
		// Read output port (P1) - return current port61 state
		c.queueOutputLocked(c.port61, OutputBufferController)

	case i8042CommandWriteOutputPort:
		// 0xD1: The next byte written to 0x60 is meant for the Output Port (A20 gate)
		c.expectingOutputPortByte = true

	case i8042CommandWriteAux:
		// Write to aux device (mouse) - next data byte goes to mouse
		c.expectingAuxWrite = true

	case i8042CommandPulseReset:
		return hv.ErrGuestRequestedReboot

	default:
		// Unknown command - ignore
		return nil
	}

	return nil
}

func (c *I8042) handleDataWriteLocked(value byte) error {
	if c.expectingCommandByte {
		c.commandByte = value
		c.expectingCommandByte = false
		return nil
	}

	if c.expectingOutputPortByte {
		// The OS is writing to the output port (usually to enable A20).
		// Since we are in a VMM, A20 is likely always enabled.
		// We consume the byte so it doesn't get stuck in the buffer.
		// Logic: If (value & 0x02), A20 is enabled.
		c.port61 = value
		c.expectingOutputPortByte = false
		return nil
	}

	if c.expectingAuxWrite {
		// Write to aux device (mouse) - not implemented yet
		c.expectingAuxWrite = false
		return nil
	}

	// Normal data write to keyboard (e.g. keyboard commands, LED control)
	if c.keyboard != nil && (c.commandByte&i8042CommandByteDisableKeyboard) == 0 {
		// Release mutex before calling keyboard to avoid deadlock.
		// The keyboard may call QueueKeyboardData which needs the mutex.
		c.mu.Unlock()
		err := c.keyboard.HandleCommand(value)
		c.mu.Lock()
		return err
	}

	return nil
}

func (c *I8042) statusLocked() byte {
	status := byte(0)

	// Output buffer full
	if c.outputBufferState != OutputBufferEmpty {
		status |= i8042StatusOutputFull
	}

	// Input buffer full (always false for now)
	// status |= i8042StatusInputFull

	// System flag
	if c.commandByte&i8042CommandByteSystemFlag != 0 {
		status |= i8042StatusSystemFlag
	}

	// Command/data flag (always 0 for reads, 1 for writes)
	// This is set by the hardware based on port access, not state

	// Keyboard lock (always set)
	status |= i8042StatusKeyLock

	// Aux data flag
	if c.outputBufferState == OutputBufferMouse {
		status |= i8042StatusAuxData
	}

	return status
}

func (c *I8042) readDataLocked() byte {
	if c.outputBufferState == OutputBufferEmpty {
		return 0x00
	}

	value := c.outputBuffer
	c.outputBufferState = OutputBufferEmpty
	return value
}

func (c *I8042) queueOutputLocked(value byte, source OutputBufferState) {
	c.outputBuffer = value
	c.outputBufferState = source

	// Trigger interrupt if enabled
	if source == OutputBufferKeyboard && (c.commandByte&i8042CommandByteInterruptKeyboard) != 0 {
		c.irq1.PulseInterrupt()
	} else if source == OutputBufferMouse && (c.commandByte&i8042CommandByteInterruptAux) != 0 {
		c.irq12.PulseInterrupt()
	}
}

// QueueKeyboardData queues data from the keyboard to the output buffer.
func (c *I8042) QueueKeyboardData(data byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.queueOutputLocked(data, OutputBufferKeyboard)
}

// QueueKeyboardDataLocked queues data from the keyboard (must be called with mutex held).
func (c *I8042) QueueKeyboardDataLocked(data byte) {
	c.queueOutputLocked(data, OutputBufferKeyboard)
}

// QueueMouseData queues data from the mouse to the output buffer.
func (c *I8042) QueueMouseData(data byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.queueOutputLocked(data, OutputBufferMouse)
}

var (
	_ hv.X86IOPortDevice    = &I8042{}
	_ chipset.ChipsetDevice = &I8042{}
	_ chipset.PortIOHandler = &I8042{}
)
