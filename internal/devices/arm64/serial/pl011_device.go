package boot

import (
	"encoding/binary"
	"fmt"
	"io"
	"sync"

	"github.com/tinyrange/cc/internal/hv"
)

const (
	pl011RegDR   = 0x00
	pl011RegRSR  = 0x04
	pl011RegFR   = 0x18
	pl011RegILPR = 0x20
	pl011RegIBRD = 0x24
	pl011RegFBRD = 0x28
	pl011RegLCRH = 0x2c
	pl011RegCR   = 0x30
	pl011RegIFLS = 0x34
	pl011RegIMSC = 0x38
	pl011RegRIS  = 0x3c
	pl011RegMIS  = 0x40
	pl011RegICR  = 0x44
	pl011RegDMAC = 0x48

	pl011FlagTxEmpty = 1 << 7
	pl011FlagRxEmpty = 1 << 4
)

type pl011Device struct {
	base uint64
	size uint64

	out io.Writer

	mu    sync.Mutex
	cr    uint32
	lcrh  uint32
	ibrd  uint32
	fbrd  uint32
	ifls  uint32
	imsc  uint32
	dmacr uint32

	outByte [1]byte
}

func NewPL011Device(base, size uint64, out io.Writer) *pl011Device {
	return &pl011Device{
		base: base,
		size: size,
		out:  out,
	}
}

func (p *pl011Device) Init(vm hv.VirtualMachine) error {
	if p.out == nil {
		p.out = io.Discard
	}
	return nil
}

func (p *pl011Device) MMIORegions() []hv.MMIORegion {
	return []hv.MMIORegion{
		{Address: p.base, Size: p.size},
	}
}

func (p *pl011Device) ReadMMIO(ctx hv.ExitContext, addr uint64, data []byte) error {
	if err := p.checkBounds(addr, len(data)); err != nil {
		return err
	}
	if len(data) == 0 || len(data) > 4 {
		return fmt.Errorf("pl011: unsupported read size %d", len(data))
	}

	offset := addr - p.base

	p.mu.Lock()
	value := p.readRegister(offset)
	p.mu.Unlock()

	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], value)
	copy(data, buf[:len(data)])
	return nil
}

func (p *pl011Device) WriteMMIO(ctx hv.ExitContext, addr uint64, data []byte) error {
	if err := p.checkBounds(addr, len(data)); err != nil {
		return err
	}
	if len(data) == 0 || len(data) > 4 {
		return fmt.Errorf("pl011: unsupported write size %d", len(data))
	}

	offset := addr - p.base
	var value uint32
	for i := 0; i < len(data); i++ {
		value |= uint32(data[i]) << (8 * i)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	return p.writeRegister(offset, value)
}

func (p *pl011Device) checkBounds(addr uint64, size int) error {
	if addr < p.base || addr+uint64(size) > p.base+p.size {
		return fmt.Errorf("pl011: access out of range (addr=0x%x size=%d)", addr, size)
	}
	return nil
}

func (p *pl011Device) readRegister(offset uint64) uint32 {
	switch offset {
	case pl011RegDR:
		return 0
	case pl011RegRSR:
		return 0
	case pl011RegFR:
		return pl011FlagTxEmpty | pl011FlagRxEmpty
	case pl011RegILPR:
		return 0
	case pl011RegIBRD:
		return p.ibrd
	case pl011RegFBRD:
		return p.fbrd
	case pl011RegLCRH:
		return p.lcrh
	case pl011RegCR:
		return p.cr
	case pl011RegIFLS:
		return p.ifls
	case pl011RegIMSC:
		return p.imsc
	case pl011RegRIS, pl011RegMIS:
		return 0
	case pl011RegICR:
		return 0
	case pl011RegDMAC:
		return p.dmacr
	default:
		return 0
	}
}

func (p *pl011Device) writeRegister(offset uint64, value uint32) error {
	switch offset {
	case pl011RegDR:
		p.outByte[0] = byte(value & 0xff)
		fmt.Printf("pl011 write DR: %#x\n", p.outByte[0])
		if _, err := p.out.Write(p.outByte[:]); err != nil {
			return fmt.Errorf("pl011: write output: %w", err)
		}
	case pl011RegRSR:
		// writes clear errors, ignore
	case pl011RegILPR:
		// IrDA low-power not supported
	case pl011RegIBRD:
		p.ibrd = value
	case pl011RegFBRD:
		p.fbrd = value
	case pl011RegLCRH:
		p.lcrh = value
	case pl011RegCR:
		p.cr = value
	case pl011RegIFLS:
		p.ifls = value
	case pl011RegIMSC:
		p.imsc = value
	case pl011RegICR:
		p.imsc = 0
	case pl011RegDMAC:
		p.dmacr = value
	default:
		// silently ignore unimplemented registers
	}
	return nil
}

var (
	_ hv.MemoryMappedIODevice = (*pl011Device)(nil)
)
