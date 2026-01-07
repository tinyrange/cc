package rv64

import (
	"sync/atomic"
	"time"
)

// CLINT register offsets
const (
	CLINTMsip     = 0x0000 // Machine Software Interrupt Pending (per hart)
	CLINTMtimecmp = 0x4000 // Machine Timer Compare (per hart)
	CLINTMtime    = 0xbff8 // Machine Time
)

// CLINT implements the Core Local Interruptor
type CLINT struct {
	cpu *CPU

	// Machine software interrupt pending
	msip uint32

	// Machine timer compare value
	mtimecmp uint64

	// Start time for mtime calculation
	startTime time.Time

	// Time scale (nanoseconds per tick)
	nsPerTick uint64
}

// NewCLINT creates a new CLINT
func NewCLINT(cpu *CPU) *CLINT {
	return &CLINT{
		cpu:       cpu,
		startTime: time.Now(),
		nsPerTick: 100, // 10 MHz timer
		mtimecmp:  ^uint64(0), // Max value - no interrupt initially
	}
}

// Size implements Device
func (c *CLINT) Size() uint64 {
	return CLINTSize
}

// getMtime returns the current mtime value
func (c *CLINT) getMtime() uint64 {
	elapsed := time.Since(c.startTime).Nanoseconds()
	return uint64(elapsed) / c.nsPerTick
}

// Read implements Device
func (c *CLINT) Read(offset uint64, size int) (uint64, error) {
	switch {
	case offset >= CLINTMsip && offset < CLINTMsip+4:
		return uint64(atomic.LoadUint32(&c.msip)), nil

	case offset >= CLINTMtimecmp && offset < CLINTMtimecmp+8:
		return c.mtimecmp, nil

	case offset >= CLINTMtime && offset < CLINTMtime+8:
		return c.getMtime(), nil
	}

	return 0, nil
}

// Write implements Device
func (c *CLINT) Write(offset uint64, size int, value uint64) error {
	switch {
	case offset >= CLINTMsip && offset < CLINTMsip+4:
		if value&1 != 0 {
			atomic.StoreUint32(&c.msip, 1)
			c.cpu.Mip |= MipMSIP
		} else {
			atomic.StoreUint32(&c.msip, 0)
			c.cpu.Mip &^= MipMSIP
		}

	case offset >= CLINTMtimecmp && offset < CLINTMtimecmp+8:
		if size == 4 {
			if offset == CLINTMtimecmp {
				c.mtimecmp = (c.mtimecmp &^ 0xffffffff) | (value & 0xffffffff)
			} else {
				c.mtimecmp = (c.mtimecmp &^ 0xffffffff00000000) | ((value & 0xffffffff) << 32)
			}
		} else {
			c.mtimecmp = value
		}
		// Clear timer interrupt if new compare > current time
		if c.mtimecmp > c.getMtime() {
			c.cpu.Mip &^= MipMTIP
		}
	}

	return nil
}

// Tick updates the timer interrupt pending bit
func (c *CLINT) Tick() {
	mtime := c.getMtime()
	if mtime >= c.mtimecmp {
		c.cpu.Mip |= MipMTIP
	}
}

var _ Device = (*CLINT)(nil)
