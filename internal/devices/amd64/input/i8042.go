package input

import (
	"fmt"
	"sync"

	"github.com/tinyrange/cc/internal/hv"
)

const (
	i8042DataPort    = 0x60
	i8042CommandPort = 0x64

	i8042CommandReadCommandByte  = 0x20
	i8042CommandWriteCommandByte = 0x60
	i8042CommandControllerTest   = 0xaa
	i8042CommandTestFirstPort    = 0xab
	i8042CommandDisableFirstPort = 0xad
	i8042CommandEnableFirstPort  = 0xae
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

	commandByte          byte
	outputBuffer         byte
	outputBufferFull     bool
	expectingCommandByte bool
}

// IOPorts implements hv.X86IOPortDevice.
func (c *I8042) IOPorts() []uint16 {
	return []uint16{i8042DataPort, i8042CommandPort}
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
		default:
			return fmt.Errorf("i8042: invalid write port 0x%04x", port)
		}
	}
	return nil
}

func NewI8042() *I8042 {
	return &I8042{
		commandByte: i8042CommandByteSystemFlag,
	}
}

func (c *I8042) handleCommandLocked(command byte) error {
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
	case i8042CommandResetCPU:
		return hv.ErrGuestRequestedReboot
	default:
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
