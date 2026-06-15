package virtio

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"

	"j5.nz/cc/internal/fdt"
)

const (
	mmioDeviceIDBlock = 2

	blockQueue     = 0
	blockQueueSize = 128
	blockSector    = 512

	blockFeatureSizeMax = uint64(1) << 1
	blockFeatureSegMax  = uint64(1) << 2
	blockFeatureFlush   = uint64(1) << 9

	blockReqIn    = 0
	blockReqOut   = 1
	blockReqFlush = 4
	blockReqGetID = 8

	blockStatusOK     = 0
	blockStatusIOErr  = 1
	blockStatusUnsupp = 2
)

type BlockBackend interface {
	io.ReaderAt
	io.WriterAt
	Size() int64
}

type Block struct {
	Base uint64
	Size uint64
	IRQ  uint32

	mu               sync.Mutex
	mem              GuestMemory
	irq              IRQController
	backend          BlockBackend
	deviceFeatureSel uint32
	driverFeatureSel uint32
	driverFeatures   uint64
	queueSel         uint32
	status           uint32
	interruptStatus  uint32
	irqHigh          bool
	configGeneration uint32
	queue            queue
	legacy           bool
}

func NewBlock(base, size uint64, irq uint32, backend BlockBackend) *Block {
	b := &Block{
		Base:    base,
		Size:    size,
		IRQ:     irq,
		backend: backend,
	}
	b.resetLocked()
	return b
}

func (b *Block) Attach(mem GuestMemory, irq IRQController) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.mem = mem
	b.irq = irq
}

func (b *Block) Contains(addr uint64, size int) bool {
	return addr >= b.Base && addr+uint64(size) <= b.Base+b.Size
}

func (b *Block) DeviceTreeNode() fdt.Node {
	return fdt.Node{
		Name: fmt.Sprintf("virtio@%x", b.Base),
		Properties: map[string]fdt.Property{
			"compatible": {Strings: []string{"virtio,mmio"}},
			"reg":        {U64: []uint64{b.Base, b.Size}},
			"interrupts": {U32: []uint32{0, b.IRQ, 4}},
			"status":     {Strings: []string{"okay"}},
		},
	}
}

func (b *Block) Read(addr uint64, size int) (uint64, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	offset := addr - b.Base
	switch offset {
	case regMagicValue:
		return truncateValue(mmioMagicValue, size), nil
	case regVersion:
		return truncateValue(mmioVersion, size), nil
	case regDeviceID:
		return truncateValue(mmioDeviceIDBlock, size), nil
	case regVendorID:
		return truncateValue(mmioVendorID, size), nil
	case regDeviceFeatures:
		features := featureVersion1 | blockFeatureSizeMax | blockFeatureSegMax | blockFeatureFlush
		if b.deviceFeatureSel == 0 {
			return truncateValue(features, size), nil
		}
		if b.deviceFeatureSel == 1 {
			return truncateValue(features>>32, size), nil
		}
		return 0, nil
	case regQueueNumMax:
		if b.queueSel == blockQueue {
			return truncateValue(blockQueueSize, size), nil
		}
		return 0, nil
	case regQueueNum:
		if b.queueSel == blockQueue {
			return truncateValue(uint64(b.queue.size), size), nil
		}
		return 0, nil
	case regQueueReady:
		if b.queueSel == blockQueue && b.queue.ready {
			return truncateValue(1, size), nil
		}
		return 0, nil
	case regInterruptStatus:
		return truncateValue(uint64(b.interruptStatus), size), nil
	case regStatus:
		return truncateValue(uint64(b.status), size), nil
	case regConfigGen:
		return truncateValue(uint64(b.configGeneration), size), nil
	}

	if offset >= regConfig && offset+uint64(size) <= regConfig+16 {
		cfg := b.configBytesLocked()
		return truncateValue(readConfigValue(cfg[offset-regConfig:], size), size), nil
	}
	return 0, nil
}

func (b *Block) Write(addr uint64, size int, value uint64) error {
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
		if b.queueSel == blockQueue {
			b.queue.size = uint16(value)
		}
	case regQueueReady:
		if b.queueSel == blockQueue {
			b.queue.ready = value != 0
			if value == 0 {
				b.queue.lastAvailIdx = 0
				b.queue.usedIdx = 0
			}
		}
	case regQueueDescLow:
		if b.queueSel == blockQueue {
			b.setQueueAddr(&b.queue.descAddr, uint32(value), true)
		}
	case regQueueDescHigh:
		if b.queueSel == blockQueue {
			b.setQueueAddr(&b.queue.descAddr, uint32(value), false)
		}
	case regQueueAvailLow:
		if b.queueSel == blockQueue {
			b.setQueueAddr(&b.queue.availAddr, uint32(value), true)
		}
	case regQueueAvailHigh:
		if b.queueSel == blockQueue {
			b.setQueueAddr(&b.queue.availAddr, uint32(value), false)
		}
	case regQueueUsedLow:
		if b.queueSel == blockQueue {
			b.setQueueAddr(&b.queue.usedAddr, uint32(value), true)
		}
	case regQueueUsedHigh:
		if b.queueSel == blockQueue {
			b.setQueueAddr(&b.queue.usedAddr, uint32(value), false)
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
		if int(value) == blockQueue {
			if err := b.processQueueLocked(); err != nil {
				return err
			}
		}
	}
	return nil
}

func (b *Block) ReadLegacy(offset uint16, size int) (uint64, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.legacy = true

	switch offset {
	case 0:
		return truncateValue(b.legacyFeaturesLocked(), size), nil
	case 8:
		if b.queueSel == blockQueue && b.queue.ready {
			return truncateValue(uint64(b.queue.descAddr/4096), size), nil
		}
		return 0, nil
	case 12:
		if b.queueSel == blockQueue {
			return truncateValue(blockQueueSize, size), nil
		}
		return 0, nil
	case 14:
		return truncateValue(uint64(b.queueSel), size), nil
	case 18:
		return truncateValue(uint64(b.status), size), nil
	case 19:
		isr := b.interruptStatus
		b.interruptStatus = 0
		if err := b.updateIRQLocked(); err != nil {
			return 0, err
		}
		return truncateValue(uint64(isr), size), nil
	}

	if offset >= 20 && int(offset)+size <= 36 {
		cfg := b.configBytesLocked()
		return truncateValue(readConfigValue(cfg[offset-20:], size), size), nil
	}
	return 0, nil
}

func (b *Block) WriteLegacy(offset uint16, size int, value uint64) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.legacy = true

	switch offset {
	case 4:
		b.driverFeatures = uint64(uint32(value))
	case 8:
		if b.queueSel == blockQueue {
			if value == 0 {
				b.queue.ready = false
				b.queue.descAddr = 0
				b.queue.availAddr = 0
				b.queue.usedAddr = 0
				return nil
			}
			b.configureLegacyQueueLocked(uint32(value))
		}
	case 14:
		b.queueSel = uint32(value)
	case 16:
		if int(value) == blockQueue {
			return b.processQueueLocked()
		}
	case 18:
		b.status = uint32(uint8(value))
		if b.status == 0 {
			b.resetLocked()
			b.legacy = true
		}
	}
	return nil
}

func (b *Block) processQueueLocked() error {
	q := &b.queue
	if !q.ready || q.size == 0 || b.mem == nil {
		return nil
	}
	if b.backend == nil {
		return errors.New("virtio-block backend is nil")
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
		written, err := b.processChainLocked(q, head)
		if err != nil {
			return err
		}
		if err := b.writeUsedLocked(q, head, written); err != nil {
			return err
		}
		q.lastAvailIdx++
	}

	b.interruptStatus |= intVring
	return b.updateIRQLocked()
}

func (b *Block) processChainLocked(q *queue, head uint16) (uint32, error) {
	descs, err := b.descriptorChainLocked(q, head)
	if err != nil {
		return 0, err
	}
	if len(descs) < 2 {
		return 0, fmt.Errorf("virtio-block request chain too short")
	}

	headerDesc := descs[0]
	if headerDesc.flags&descFWrite != 0 || headerDesc.length < 16 {
		return 0, fmt.Errorf("virtio-block invalid request header descriptor")
	}
	header, err := b.mem.ReadIPA(headerDesc.addr, 16)
	if err != nil {
		return 0, err
	}
	reqType := binary.LittleEndian.Uint32(header[0:4])
	sector := binary.LittleEndian.Uint64(header[8:16])

	statusDesc := descs[len(descs)-1]
	if statusDesc.flags&descFWrite == 0 || statusDesc.length < 1 {
		return 0, fmt.Errorf("virtio-block missing writable status descriptor")
	}
	dataDescs := descs[1 : len(descs)-1]
	status := byte(blockStatusOK)
	var used uint32 = 1

	switch reqType {
	case blockReqIn:
		offset := int64(sector * blockSector)
		for _, desc := range dataDescs {
			if desc.flags&descFWrite == 0 {
				status = blockStatusIOErr
				break
			}
			buf := make([]byte, desc.length)
			n, readErr := b.backend.ReadAt(buf, offset)
			if readErr != nil && !errors.Is(readErr, io.EOF) {
				status = blockStatusIOErr
				break
			}
			if n < len(buf) {
				clear(buf[n:])
			}
			if err := b.mem.WriteIPA(desc.addr, buf); err != nil {
				return used, err
			}
			offset += int64(desc.length)
			used += desc.length
		}
	case blockReqOut:
		offset := int64(sector * blockSector)
		for _, desc := range dataDescs {
			if desc.flags&descFWrite != 0 {
				status = blockStatusIOErr
				break
			}
			buf, err := b.mem.ReadIPA(desc.addr, int(desc.length))
			if err != nil {
				return used, err
			}
			n, writeErr := b.backend.WriteAt(buf, offset)
			if writeErr != nil || n != len(buf) {
				status = blockStatusIOErr
				break
			}
			offset += int64(desc.length)
		}
	case blockReqFlush:
		// No host-side flush hook is required for in-memory regions.
	case blockReqGetID:
		id := []byte("cc-virtio-block")
		for _, desc := range dataDescs {
			if desc.flags&descFWrite == 0 {
				status = blockStatusIOErr
				break
			}
			buf := make([]byte, desc.length)
			copy(buf, id)
			if err := b.mem.WriteIPA(desc.addr, buf); err != nil {
				return used, err
			}
			used += desc.length
			break
		}
	default:
		status = blockStatusUnsupp
	}

	if err := b.mem.WriteIPA(statusDesc.addr, []byte{status}); err != nil {
		return used, err
	}
	return used, nil
}

func (b *Block) descriptorChainLocked(q *queue, head uint16) ([]descriptor, error) {
	index := head
	descs := make([]descriptor, 0, 4)
	for i := uint16(0); i < q.size; i++ {
		desc, err := b.readDescriptorLocked(q, index)
		if err != nil {
			return nil, err
		}
		descs = append(descs, desc)
		if desc.flags&descFNext == 0 {
			return descs, nil
		}
		index = desc.next
	}
	return nil, fmt.Errorf("virtio-block descriptor chain loop")
}

func (b *Block) readDescriptorLocked(q *queue, index uint16) (descriptor, error) {
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

func (b *Block) writeUsedLocked(q *queue, head uint16, usedLen uint32) error {
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

func (b *Block) configureLegacyQueueLocked(pfn uint32) {
	q := &b.queue
	q.size = blockQueueSize
	q.ready = true
	q.descAddr = uint64(pfn) * 4096
	q.availAddr = q.descAddr + 16*uint64(q.size)
	used := q.availAddr + 4 + 2*uint64(q.size)
	q.usedAddr = alignVirtio(used, 4096)
	q.lastAvailIdx = 0
	q.usedIdx = 0
}

func (b *Block) configBytesLocked() []byte {
	var cfg [16]byte
	if b.backend != nil && b.backend.Size() > 0 {
		binary.LittleEndian.PutUint64(cfg[0:8], uint64(b.backend.Size()/blockSector))
	}
	binary.LittleEndian.PutUint32(cfg[8:12], 128*1024)
	binary.LittleEndian.PutUint32(cfg[12:16], 64)
	return cfg[:]
}

func (b *Block) legacyFeaturesLocked() uint64 {
	return blockFeatureSizeMax | blockFeatureSegMax | blockFeatureFlush
}

func (b *Block) updateIRQLocked() error {
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

func (b *Block) setQueueAddr(target *uint64, value uint32, low bool) {
	if b.queueSel != blockQueue {
		return
	}
	if low {
		*target = (*target &^ 0xffffffff) | uint64(value)
	} else {
		*target = (*target & 0xffffffff) | (uint64(value) << 32)
	}
}

func (b *Block) resetLocked() {
	b.deviceFeatureSel = 0
	b.driverFeatureSel = 0
	b.driverFeatures = 0
	b.queueSel = 0
	b.status = 0
	b.interruptStatus = 0
	b.irqHigh = false
	b.configGeneration++
	b.queue = queue{}
}

func alignVirtio(value, alignment uint64) uint64 {
	return (value + alignment - 1) &^ (alignment - 1)
}
