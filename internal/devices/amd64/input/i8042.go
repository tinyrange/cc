package input

import (
	"fmt"
	"sync"

	"github.com/tinyrange/cc/internal/hv"
)

const (
	i8042DataPort          = 0x60
	i8042SystemControlPort = 0x61 // Port B (Speaker/NMI/Timer)
	i8042CommandPort       = 0x64

	i8042CommandReadCommandByte  = 0x20
	i8042CommandWriteCommandByte = 0x60
	i8042CommandCheckAuxPort     = 0xa8 // Often checked by Linux
	i8042CommandControllerTest   = 0xaa
	i8042CommandTestFirstPort    = 0xab
	i8042CommandDisableFirstPort = 0xad
	i8042CommandEnableFirstPort  = 0xae
	i8042CommandWriteOutputPort  = 0xd1 // Used for A20 Gate
	i8042CommandResetCPU         = 0xfe
)

const (
	i8042StatusOutputFull = 1 << 0
	i8042StatusKeyLock    = 1 << 4
)

const (
	i8042CommandByteSystemFlag      = 1 << 2
	i8042CommandByteDisablePort1Clk = 1 << 4
)

const (
	i8042ResponseSelfTestOK = 0x55
	i8042ResponsePortOK     = 0x00
)

type I8042 struct {
	mu sync.Mutex

	commandByte             byte
	outputBuffer            byte
	outputBufferFull        bool
	expectingCommandByte    bool
	expectingOutputPortByte bool // New: For handling 0xD1 command data

	port61 byte // New: State for System Control Port B
}

// IOPorts implements hv.X86IOPortDevice.
func (c *I8042) IOPorts() []uint16 {
	// Updated: Added i8042SystemControlPort (0x61)
	return []uint16{i8042DataPort, i8042SystemControlPort, i8042CommandPort}
}

// Init implements hv.X86IOPortDevice.
func (c *I8042) Init(vm hv.VirtualMachine) error {
	return nil
}

// ReadIOPort implements hv.X86IOPortDevice.
func (c *I8042) ReadIOPort(port uint16, data []byte) error {
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

// WriteIOPort implements hv.X86IOPortDevice.
func (c *I8042) WriteIOPort(port uint16, data []byte) error {
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

func NewI8042() *I8042 {
	return &I8042{
		commandByte: i8042CommandByteSystemFlag,
		// Initialize port61 with Speaker OFF, Gate 2 OFF.
		port61: 0x00,
	}
}

func (c *I8042) handleCommandLocked(command byte) error {
	// Reset data expectations when a new command arrives
	c.expectingCommandByte = false
	c.expectingOutputPortByte = false

	switch command {
	case i8042CommandReadCommandByte:
		c.queueOutputLocked(c.commandByte)
	case i8042CommandWriteCommandByte:
		c.expectingCommandByte = true
	case i8042CommandControllerTest:
		c.queueOutputLocked(i8042ResponseSelfTestOK)
	case i8042CommandTestFirstPort:
		c.queueOutputLocked(i8042ResponsePortOK)
	case i8042CommandDisableFirstPort:
		c.commandByte |= i8042CommandByteDisablePort1Clk
	case i8042CommandEnableFirstPort:
		c.commandByte &^= i8042CommandByteDisablePort1Clk
	case i8042CommandWriteOutputPort:
		// 0xD1: The next byte written to 0x60 is meant for the Output Port (A20 gate)
		c.expectingOutputPortByte = true
	case i8042CommandCheckAuxPort:
		// Linux checks if a mouse port exists. We don't have one, but we shouldn't crash.
		// Return 0xFF (Test Failed) or similar if we want to say "No Mouse".
		// But usually just ignoring it or returning a dummy value works.
		// For now, let's just ignore it to avoid fallback to default.
	case i8042CommandResetCPU:
		return hv.ErrGuestRequestedReboot
	default:
		// Debug logging can be helpful here
		// fmt.Printf("i8042: unhandled command 0x%x\n", command)
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
		c.expectingOutputPortByte = false
		return nil
	}

	// Normal data write (e.g. keyboard LEDs or scancode ack).
	// We can ignore this for a basic boot.
	return nil
}

func (c *I8042) statusLocked() byte {
	status := byte(i8042StatusKeyLock)
	if c.outputBufferFull {
		status |= i8042StatusOutputFull
	}
	status |= c.commandByte & i8042CommandByteSystemFlag
	return status
}

func (c *I8042) readDataLocked() byte {
	if !c.outputBufferFull {
		return 0x00
	}

	value := c.outputBuffer
	c.outputBufferFull = false
	return value
}

func (c *I8042) queueOutputLocked(value byte) {
	c.outputBuffer = value
	c.outputBufferFull = true
}

var (
	_ hv.X86IOPortDevice = &I8042{}
)
