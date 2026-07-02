package virtio

import (
	"encoding/binary"
	"fmt"
	"sort"
	"sync"

	"j5.nz/cc/internal/fdt"
)

const (
	mmioDeviceIDBalloon = 5

	balloonQueueInflate = 0
	balloonQueueDeflate = 1
	balloonPageSize     = 4096
)

type PageReclaimer interface {
	ReclaimGuestPage(ipa uint64) error
	ReuseGuestPage(ipa uint64) error
}

type Balloon struct {
	Base uint64
	Size uint64
	IRQ  uint32

	mu               sync.Mutex
	mem              GuestMemory
	reclaimer        PageReclaimer
	irq              IRQController
	deviceFeatureSel uint32
	driverFeatureSel uint32
	driverFeatures   uint64
	queueSel         uint32
	status           uint32
	interruptStatus  uint32
	irqHigh          bool
	configGeneration uint32
	numPages         uint32
	actualPages      uint32
	inflated         map[uint64]struct{}
	queues           [2]queue
}

func NewBalloon(base, size uint64, irq uint32) *Balloon {
	b := &Balloon{
		Base: base,
		Size: size,
		IRQ:  irq,
	}
	b.resetLocked()
	return b
}

func (b *Balloon) Attach(mem GuestMemory, irq IRQController) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.mem = mem
	if reclaimer, ok := mem.(PageReclaimer); ok {
		b.reclaimer = reclaimer
	}
	b.irq = irq
}

func (b *Balloon) Contains(addr uint64, size int) bool {
	return addr >= b.Base && addr+uint64(size) <= b.Base+b.Size
}

func (b *Balloon) DeviceTreeNode() fdt.Node {
	return fdt.Node{
		Name: fmt.Sprintf("virtio@%x", b.Base),
		Properties: map[string]fdt.Property{
			"compatible":   {Strings: []string{"virtio,mmio"}},
			"dma-coherent": {Flag: true},
			"reg":          {U64: []uint64{b.Base, b.Size}},
			"interrupts":   {U32: []uint32{0, b.IRQ, 4}},
			"status":       {Strings: []string{"okay"}},
		},
	}
}

func (b *Balloon) SetTargetPages(pages uint32) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.numPages == pages {
		return nil
	}
	b.numPages = pages
	b.configGeneration++
	b.interruptStatus |= intConfig
	return b.updateIRQLocked()
}

func (b *Balloon) Read(addr uint64, size int) (uint64, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	offset := addr - b.Base
	switch offset {
	case regMagicValue:
		return truncateValue(mmioMagicValue, size), nil
	case regVersion:
		return truncateValue(mmioVersion, size), nil
	case regDeviceID:
		return truncateValue(mmioDeviceIDBalloon, size), nil
	case regVendorID:
		return truncateValue(mmioVendorID, size), nil
	case regDeviceFeatures:
		if b.deviceFeatureSel == 1 {
			return truncateValue(1, size), nil
		}
		return 0, nil
	case regQueueNumMax:
		if b.selectedQueueLocked() != nil {
			return truncateValue(256, size), nil
		}
		return 0, nil
	case regQueueNum:
		if q := b.selectedQueueLocked(); q != nil {
			return truncateValue(uint64(q.size), size), nil
		}
		return 0, nil
	case regQueueReady:
		if q := b.selectedQueueLocked(); q != nil && q.ready {
			return truncateValue(1, size), nil
		}
		return 0, nil
	case regInterruptStatus:
		return truncateValue(uint64(b.interruptStatus), size), nil
	case regStatus:
		return truncateValue(uint64(b.status), size), nil
	case regConfigGen:
		return truncateValue(uint64(b.configGeneration), size), nil
	default:
		if offset >= regConfig && offset+uint64(size) <= regConfig+8 {
			cfg := b.configBytesLocked()
			return truncateValue(readConfigValue(cfg[offset-regConfig:], size), size), nil
		}
		return 0, nil
	}
}

func (b *Balloon) Write(addr uint64, size int, value uint64) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	offset := addr - b.Base
	switch offset {
	case regDeviceFeatSel:
		b.deviceFeatureSel = uint32(value)
	case regDriverFeatSel:
		b.driverFeatureSel = uint32(value)
	case regDriverFeatures:
		if b.driverFeatureSel == 0 {
			b.driverFeatures = (b.driverFeatures &^ 0xffffffff) | uint64(uint32(value))
		} else if b.driverFeatureSel == 1 {
			b.driverFeatures = (b.driverFeatures & 0xffffffff) | (uint64(uint32(value)) << 32)
		}
	case regQueueSel:
		b.queueSel = uint32(value)
	case regQueueNum:
		if q := b.selectedQueueLocked(); q != nil {
			q.size = uint16(value)
		}
	case regQueueReady:
		if q := b.selectedQueueLocked(); q != nil {
			q.ready = value != 0
			if value == 0 {
				q.lastAvailIdx = 0
				q.usedIdx = 0
			}
		}
	case regGuestPageSize, regQueueAlign:
		return nil
	case regQueueDescLow:
		if q := b.selectedQueueLocked(); q != nil {
			setQueueAddr(q, uint32(value), queueAddrDesc, true)
		}
	case regQueueDescHigh:
		if q := b.selectedQueueLocked(); q != nil {
			setQueueAddr(q, uint32(value), queueAddrDesc, false)
		}
	case regQueueAvailLow:
		if q := b.selectedQueueLocked(); q != nil {
			setQueueAddr(q, uint32(value), queueAddrAvail, true)
		}
	case regQueueAvailHigh:
		if q := b.selectedQueueLocked(); q != nil {
			setQueueAddr(q, uint32(value), queueAddrAvail, false)
		}
	case regQueueUsedLow:
		if q := b.selectedQueueLocked(); q != nil {
			setQueueAddr(q, uint32(value), queueAddrUsed, true)
		}
	case regQueueUsedHigh:
		if q := b.selectedQueueLocked(); q != nil {
			setQueueAddr(q, uint32(value), queueAddrUsed, false)
		}
	case regInterruptAck:
		b.interruptStatus &^= uint32(value)
		return b.updateIRQLocked()
	case regStatus:
		b.status = uint32(value)
		if b.status == 0 {
			b.resetLocked()
		}
	case regQueueNotify:
		if int(value) == balloonQueueInflate || int(value) == balloonQueueDeflate {
			if err := b.processQueueLocked(int(value)); err != nil {
				return err
			}
		}
	default:
		if offset >= regConfig+4 && offset+uint64(size) <= regConfig+8 {
			b.writeActualLocked(offset-regConfig, size, value)
		}
	}
	return nil
}

func (b *Balloon) processQueueLocked(qidx int) error {
	q := &b.queues[qidx]
	if !q.ready || q.size == 0 || b.mem == nil {
		return nil
	}
	header, err := b.mem.ReadIPA(q.availAddr, 4)
	if err != nil {
		return err
	}
	availIdx := binary.LittleEndian.Uint16(header[2:4])
	for q.lastAvailIdx != availIdx {
		slot := q.lastAvailIdx % q.size
		entry, err := b.mem.ReadIPA(q.availAddr+4+uint64(slot)*2, 2)
		if err != nil {
			return err
		}
		head := binary.LittleEndian.Uint16(entry)
		if err := b.processChainLocked(q, qidx, head); err != nil {
			return err
		}
		if err := b.writeUsedLocked(q, head, 0); err != nil {
			return err
		}
		q.lastAvailIdx++
	}
	b.interruptStatus |= intVring
	return b.updateIRQLocked()
}

func (b *Balloon) processChainLocked(q *queue, qidx int, head uint16) error {
	index := head
	for i := uint16(0); i < q.size; i++ {
		desc, err := b.readDescriptorLocked(q, index)
		if err != nil {
			return err
		}
		if desc.flags&descFWrite == 0 && desc.length > 0 {
			if err := b.processPFNsLocked(qidx, desc.addr, desc.length); err != nil {
				return err
			}
		}
		if desc.flags&descFNext == 0 {
			return nil
		}
		index = desc.next
	}
	return fmt.Errorf("virtio-balloon descriptor chain loop")
}

func (b *Balloon) processPFNsLocked(qidx int, addr uint64, length uint32) error {
	if length%4 != 0 {
		return fmt.Errorf("virtio-balloon pfn buffer length %d is not 4-byte aligned", length)
	}
	buf, err := b.mem.ReadIPA(addr, int(length))
	if err != nil {
		return err
	}
	for off := 0; off < len(buf); off += 4 {
		pfn := uint64(binary.LittleEndian.Uint32(buf[off : off+4]))
		ipa := pfn * balloonPageSize
		switch qidx {
		case balloonQueueInflate:
			b.inflated[pfn] = struct{}{}
			if b.reclaimer != nil {
				if err := b.reclaimer.ReclaimGuestPage(ipa); err != nil {
					return err
				}
			}
		case balloonQueueDeflate:
			delete(b.inflated, pfn)
			if b.reclaimer != nil {
				if err := b.reclaimer.ReuseGuestPage(ipa); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (b *Balloon) readDescriptorLocked(q *queue, index uint16) (descriptor, error) {
	if index >= q.size {
		return descriptor{}, fmt.Errorf("descriptor index %d out of range", index)
	}
	buf, err := b.mem.ReadIPA(q.descAddr+uint64(index)*16, 16)
	if err != nil {
		return descriptor{}, err
	}
	return descriptor{
		addr:   binary.LittleEndian.Uint64(buf[0:8]),
		length: binary.LittleEndian.Uint32(buf[8:12]),
		flags:  binary.LittleEndian.Uint16(buf[12:14]),
		next:   binary.LittleEndian.Uint16(buf[14:16]),
	}, nil
}

func (b *Balloon) writeUsedLocked(q *queue, head uint16, usedLen uint32) error {
	slot := q.usedIdx % q.size
	elem := make([]byte, 8)
	binary.LittleEndian.PutUint32(elem[0:4], uint32(head))
	binary.LittleEndian.PutUint32(elem[4:8], usedLen)
	if err := b.mem.WriteIPA(q.usedAddr+4+uint64(slot)*8, elem); err != nil {
		return err
	}
	q.usedIdx++
	idx := make([]byte, 2)
	binary.LittleEndian.PutUint16(idx, q.usedIdx)
	return b.mem.WriteIPA(q.usedAddr+2, idx)
}

func (b *Balloon) updateIRQLocked() error {
	if b.irq == nil {
		return nil
	}
	level := b.interruptStatus != 0
	if b.irqHigh == level {
		return nil
	}
	b.irqHigh = level
	return b.irq.SetIRQ(b.IRQ, level)
}

func (b *Balloon) resetLocked() {
	b.deviceFeatureSel = 0
	b.driverFeatureSel = 0
	b.driverFeatures = 0
	b.queueSel = 0
	b.status = 0
	b.interruptStatus = 0
	b.irqHigh = false
	b.configGeneration++
	b.actualPages = 0
	b.inflated = make(map[uint64]struct{})
	b.queues = [2]queue{}
}

func (b *Balloon) selectedQueueLocked() *queue {
	if b.queueSel >= uint32(len(b.queues)) {
		return nil
	}
	return &b.queues[b.queueSel]
}

func (b *Balloon) configBytesLocked() []byte {
	var cfg [8]byte
	binary.LittleEndian.PutUint32(cfg[0:4], b.numPages)
	binary.LittleEndian.PutUint32(cfg[4:8], b.actualPages)
	return cfg[:]
}

func (b *Balloon) writeActualLocked(offset uint64, size int, value uint64) {
	var cfg [8]byte
	binary.LittleEndian.PutUint32(cfg[4:8], b.actualPages)
	for i := 0; i < size; i++ {
		cfg[offset+uint64(i)] = byte(value >> (8 * i))
	}
	b.actualPages = binary.LittleEndian.Uint32(cfg[4:8])
}

func (b *Balloon) inflatedPagesLocked() []uint64 {
	pages := make([]uint64, 0, len(b.inflated))
	for pfn := range b.inflated {
		pages = append(pages, pfn)
	}
	sort.Slice(pages, func(i, j int) bool { return pages[i] < pages[j] })
	return pages
}

type queueAddrField int

const (
	queueAddrDesc queueAddrField = iota
	queueAddrAvail
	queueAddrUsed
)

func setQueueAddr(q *queue, value uint32, field queueAddrField, low bool) {
	var target *uint64
	switch field {
	case queueAddrDesc:
		target = &q.descAddr
	case queueAddrAvail:
		target = &q.availAddr
	case queueAddrUsed:
		target = &q.usedAddr
	}
	if low {
		*target = (*target &^ 0xffffffff) | uint64(value)
	} else {
		*target = (*target & 0xffffffff) | (uint64(value) << 32)
	}
}
