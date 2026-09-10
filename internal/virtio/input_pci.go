package virtio

import (
	"encoding/binary"
	"fmt"
)

// InputPCI is the modern virtio PCI transport for an Input device. BAR0 contains
// common configuration (0), notification (0x1000), ISR (0x2000), and device
// configuration (0x3000). It uses INTx; MSI-X is deliberately not advertised.
type InputPCI struct{ *Input }

func (p *InputPCI) PCIConfig(cfg []byte) {
	cfg[6] |= 0x10
	cfg[0x34] = 0x40
	for n, typ := range []byte{1, 2, 3, 4} {
		a := 0x40 + n*20
		cfg[a] = 9
		if n < 3 {
			cfg[a+1] = byte(a + 20)
		}
		cfg[a+2] = 16
		cfg[a+3] = typ
		binary.LittleEndian.PutUint32(cfg[a+8:], uint32(n)*0x1000)
		length := uint32(0x1000)
		if n == 0 {
			length = 56
		}
		if n == 2 {
			length = 1
		}
		if n == 3 {
			length = 136
		}
		binary.LittleEndian.PutUint32(cfg[a+12:], length)
		if typ == 2 {
			cfg[a+2] = 20
			binary.LittleEndian.PutUint32(cfg[a+16:], 4)
		}
	}
}

func (p *InputPCI) ReadMMIO(offset uint64, size int) (uint64, error) {
	i := p.Input
	i.mu.Lock()
	defer i.mu.Unlock()
	if size < 1 || size > 8 || offset+uint64(size) > 0x4000 {
		return 0, fmt.Errorf("invalid input PCI read")
	}
	if offset == 0x2000 {
		value := i.interruptStatus
		i.interruptStatus = 0
		return uint64(value), i.updateIRQLocked()
	}
	if offset >= 0x3000 && offset+uint64(size) <= 0x3000+136 {
		return readConfigValue(i.configBytesLocked()[offset-0x3000:], size), nil
	}
	var c [56]byte
	binary.LittleEndian.PutUint32(c[0:], i.deviceFeatureSel)
	if i.deviceFeatureSel == 1 {
		binary.LittleEndian.PutUint32(c[4:], 1)
	}
	binary.LittleEndian.PutUint32(c[8:], i.driverFeatureSel)
	if i.driverFeatureSel < 2 {
		binary.LittleEndian.PutUint32(c[12:], uint32(i.driverFeatures>>(32*i.driverFeatureSel)))
	}
	binary.LittleEndian.PutUint16(c[16:], 0xffff)
	binary.LittleEndian.PutUint16(c[18:], 2)
	c[20] = byte(i.status)
	c[21] = byte(i.configGeneration)
	binary.LittleEndian.PutUint16(c[22:], uint16(i.queueSel))
	binary.LittleEndian.PutUint16(c[26:], 0xffff)
	if q := i.selectedQueueLocked(); q != nil {
		n := q.size
		if n == 0 {
			n = 64
		}
		binary.LittleEndian.PutUint16(c[24:], n)
		if q.ready {
			binary.LittleEndian.PutUint16(c[28:], 1)
		}
		binary.LittleEndian.PutUint16(c[30:], uint16(i.queueSel))
		binary.LittleEndian.PutUint64(c[32:], q.descAddr)
		binary.LittleEndian.PutUint64(c[40:], q.availAddr)
		binary.LittleEndian.PutUint64(c[48:], q.usedAddr)
	}
	if offset+uint64(size) <= 56 {
		return readConfigValue(c[offset:], size), nil
	}
	return 0, nil
}
func (p *InputPCI) WriteMMIO(offset uint64, size int, value uint64) error {
	i := p.Input
	i.mu.Lock()
	defer i.mu.Unlock()
	if size < 1 || size > 8 || offset+uint64(size) > 0x4000 {
		return fmt.Errorf("invalid input PCI write")
	}
	if offset >= 0x1000 && offset < 0x2000 {
		switch uint16(value) {
		case 0:
			return i.flushEventsLocked()
		case 1:
			return i.processStatusLocked()
		}
		return nil
	}
	if offset >= 0x3000 && offset < 0x3002 {
		if offset == 0x3000 {
			i.configSelect = byte(value)
		}
		if offset+uint64(size) > 0x3001 {
			i.configSubsel = byte(value >> ((0x3001 - offset) * 8))
		}
		return nil
	}
	switch offset {
	case 0:
		i.deviceFeatureSel = uint32(value)
	case 8:
		i.driverFeatureSel = uint32(value)
	case 12:
		if i.driverFeatureSel < 2 {
			s := i.driverFeatureSel * 32
			i.driverFeatures = i.driverFeatures&^(uint64(0xffffffff)<<s) | uint64(uint32(value))<<s
		}
	case 20:
		i.status = uint32(byte(value))
		if i.status == 0 {
			wasHigh := i.irqHigh
			i.resetLocked()
			i.irqHigh = wasHigh
			return i.updateIRQLocked()
		}
	case 22:
		i.queueSel = uint32(uint16(value))
	case 24:
		if q := i.selectedQueueLocked(); q != nil {
			if value == 0 || value > 64 || value&(value-1) != 0 {
				return fmt.Errorf("invalid input queue size %d", value)
			}
			q.size = uint16(value)
		}
	case 28:
		if q := i.selectedQueueLocked(); q != nil {
			q.ready = value != 0
		}
	default:
		if offset >= 32 && offset+uint64(size) <= 56 {
			if q := i.selectedQueueLocked(); q != nil {
				targets := []*uint64{&q.descAddr, &q.availAddr, &q.usedAddr}
				idx := (offset - 32) / 8
				s := (offset - 32) % 8 * 8
				if offset%8+uint64(size) > 8 {
					return fmt.Errorf("crossing input queue address write")
				}
				mask := ^uint64(0)
				if size < 8 {
					mask = (uint64(1) << (size * 8)) - 1
				}
				*targets[idx] = *targets[idx]&^(mask<<s) | (value&mask)<<s
			}
		}
	}
	return nil
}
