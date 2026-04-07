package serial

import (
	"encoding/binary"
	"fmt"
	"io"
)

const (
	UART8250Size         = 0x1000
	UART8250DefaultClock = 1843200

	uartRegisterCount = 8

	uartLCRDLAB = 1 << 7
	uartMCRLoop = 1 << 4

	uartLSRDataReady = 1 << 0
	uartLSRTHRE      = 1 << 5
	uartLSRTEMT      = 1 << 6
)

type UART8250 struct {
	base   uint64
	size   uint64
	stride uint64
	out    io.Writer

	dll        byte
	dlm        byte
	ier        byte
	fcr        byte
	lcr        byte
	mcr        byte
	lsr        byte
	scr        byte
	rbr        byte
	pendingIIR byte
	skipLF     bool
}

func NewUART8250(base uint64, regShift uint32, out io.Writer) *UART8250 {
	stride := uint64(1) << regShift
	if stride == 0 {
		stride = 1
	}
	return &UART8250{
		base:       base,
		size:       UART8250Size,
		stride:     stride,
		out:        out,
		lsr:        uartLSRTHRE | uartLSRTEMT,
		pendingIIR: 0x01,
	}
}

func (u *UART8250) Contains(addr uint64, size int) bool {
	if size <= 0 {
		return false
	}
	end := addr + uint64(size)
	return addr >= u.base && end > addr && end <= u.base+u.size
}

func (u *UART8250) Write(addr uint64, data []byte) error {
	for i, value := range data {
		if err := u.writeByte(addr+uint64(i), value); err != nil {
			return err
		}
	}
	return nil
}

func (u *UART8250) Read(addr uint64, data []byte) error {
	for i := range data {
		value, err := u.readByte(addr + uint64(i))
		if err != nil {
			return err
		}
		data[i] = value
	}
	return nil
}

func (u *UART8250) WriteValue(addr uint64, size int, value uint64) error {
	if size <= 0 || size > 8 {
		return fmt.Errorf("invalid write size %d", size)
	}
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], value)
	return u.Write(addr, buf[:size])
}

func (u *UART8250) ReadValue(addr uint64, size int) (uint64, error) {
	if size <= 0 || size > 8 {
		return 0, fmt.Errorf("invalid read size %d", size)
	}
	var buf [8]byte
	if err := u.Read(addr, buf[:size]); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint64(buf[:]), nil
}

func (u *UART8250) writeByte(addr uint64, value byte) error {
	reg, err := u.registerAt(addr)
	if err != nil {
		return err
	}
	u.writeRegister(reg, value)
	return nil
}

func (u *UART8250) readByte(addr uint64) (byte, error) {
	reg, err := u.registerAt(addr)
	if err != nil {
		return 0, err
	}
	return u.readRegister(reg), nil
}

func (u *UART8250) registerAt(addr uint64) (uint16, error) {
	if addr < u.base || addr >= u.base+u.size {
		return 0, fmt.Errorf("uart address %#x outside range [%#x,%#x)", addr, u.base, u.base+u.size)
	}
	offset := addr - u.base
	if offset%u.stride != 0 {
		return 0, fmt.Errorf("uart address %#x is not register aligned", addr)
	}
	reg := offset / u.stride
	if reg >= uartRegisterCount {
		return 0, fmt.Errorf("uart register index %d out of range", reg)
	}
	return uint16(reg), nil
}

func (u *UART8250) writeRegister(reg uint16, value byte) {
	switch reg {
	case 0:
		if u.lcr&uartLCRDLAB != 0 {
			u.dll = value
			return
		}
		u.lsr &^= uartLSRTHRE
		u.transmit(value)
	case 1:
		if u.lcr&uartLCRDLAB != 0 {
			u.dlm = value
			return
		}
		u.ier = value & 0x0f
		u.updateInterrupts()
	case 2:
		u.fcr = value
	case 3:
		u.lcr = value
	case 4:
		u.mcr = value
	case 7:
		u.scr = value
	}
}

func (u *UART8250) readRegister(reg uint16) byte {
	switch reg {
	case 0:
		if u.lcr&uartLCRDLAB != 0 {
			return u.dll
		}
		value := u.rbr
		u.rbr = 0
		u.lsr &^= uartLSRDataReady
		u.updateInterrupts()
		return value
	case 1:
		if u.lcr&uartLCRDLAB != 0 {
			return u.dlm
		}
		return u.ier
	case 2:
		return u.pendingIIR
	case 3:
		return u.lcr
	case 4:
		return u.mcr
	case 5:
		return u.lsr
	case 6:
		return 0xb0
	case 7:
		return u.scr
	default:
		return 0
	}
}

func (u *UART8250) updateInterrupts() {
	u.pendingIIR = 0x01
	switch {
	case u.ier&0x01 != 0 && u.lsr&uartLSRDataReady != 0:
		u.pendingIIR = 0x04
	case u.ier&0x02 != 0 && u.lsr&uartLSRTHRE != 0:
		u.pendingIIR = 0x02
	}
}

func (u *UART8250) transmit(value byte) {
	if u.mcr&uartMCRLoop != 0 {
		u.rbr = value
		u.lsr |= uartLSRDataReady
		u.updateInterrupts()
		return
	}
	if u.out != nil {
		switch value {
		case '\r':
			_, _ = u.out.Write([]byte{'\n'})
			u.skipLF = true
		case '\n':
			if u.skipLF {
				u.skipLF = false
			} else {
				_, _ = u.out.Write([]byte{'\n'})
			}
		default:
			u.skipLF = false
			_, _ = u.out.Write([]byte{value})
		}
	}
	u.lsr |= uartLSRTHRE | uartLSRTEMT
	u.updateInterrupts()
}
