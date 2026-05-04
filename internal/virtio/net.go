package virtio

import (
	"encoding/binary"
	"fmt"
	"net"
	"sync"

	"j5.nz/cc/internal/fdt"
)

const (
	mmioDeviceIDNet = 1

	netQueueRX = 0
	netQueueTX = 1

	netQueueSize = 256

	netFeatureMAC           = uint64(1) << 5
	netFeatureStatus        = uint64(1) << 16
	netFeatureMergeRX       = uint64(1) << 15
	netHdrFlagNeedsChecksum = 1

	netStatusLinkUp = 1
	netHeaderLen    = 12
)

type NetBackend interface {
	HandleTxPacket(packet []byte, release func()) error
}

type Net struct {
	Base uint64
	Size uint64
	IRQ  uint32
	MAC  net.HardwareAddr

	mu               sync.Mutex
	mem              GuestMemory
	irq              IRQController
	backend          NetBackend
	deviceFeatureSel uint32
	driverFeatureSel uint32
	driverFeatures   uint64
	queueSel         uint32
	status           uint32
	interruptStatus  uint32
	irqHigh          bool
	configGeneration uint32
	queues           [2]queue
	pendingRx        [][]byte
}

func NewNet(base, size uint64, irq uint32, mac net.HardwareAddr, backend NetBackend) *Net {
	n := &Net{
		Base:    base,
		Size:    size,
		IRQ:     irq,
		MAC:     append(net.HardwareAddr(nil), mac...),
		backend: backend,
	}
	if len(n.MAC) != 6 {
		n.MAC = net.HardwareAddr{0x02, 0x42, 0x0a, 0x2a, 0x00, 0x02}
	}
	n.resetLocked()
	return n
}

func (n *Net) Attach(mem GuestMemory, irq IRQController) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.mem = mem
	n.irq = irq
}

func (n *Net) Contains(addr uint64, size int) bool {
	return addr >= n.Base && addr+uint64(size) <= n.Base+n.Size
}

func (n *Net) DeviceTreeNode() fdt.Node {
	return fdt.Node{
		Name: fmt.Sprintf("virtio@%x", n.Base),
		Properties: map[string]fdt.Property{
			"compatible": {Strings: []string{"virtio,mmio"}},
			"reg":        {U64: []uint64{n.Base, n.Size}},
			"interrupts": {U32: []uint32{0, n.IRQ, 4}},
			"status":     {Strings: []string{"okay"}},
		},
	}
}

func (n *Net) Read(addr uint64, size int) (uint64, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	offset := addr - n.Base
	switch offset {
	case regMagicValue:
		return truncateValue(mmioMagicValue, size), nil
	case regVersion:
		return truncateValue(mmioVersion, size), nil
	case regDeviceID:
		return truncateValue(mmioDeviceIDNet, size), nil
	case regVendorID:
		return truncateValue(mmioVendorID, size), nil
	case regDeviceFeatures:
		features := featureVersion1 | netFeatureMAC | netFeatureStatus | netFeatureMergeRX
		if n.deviceFeatureSel == 0 {
			return truncateValue(features, size), nil
		}
		if n.deviceFeatureSel == 1 {
			return truncateValue(features>>32, size), nil
		}
		return 0, nil
	case regQueueNumMax:
		if n.queueSel < uint32(len(n.queues)) {
			return truncateValue(netQueueSize, size), nil
		}
		return 0, nil
	case regQueueNum:
		if q := n.selectedQueueLocked(); q != nil {
			return truncateValue(uint64(q.size), size), nil
		}
		return 0, nil
	case regQueueReady:
		if q := n.selectedQueueLocked(); q != nil && q.ready {
			return truncateValue(1, size), nil
		}
		return 0, nil
	case regInterruptStatus:
		return truncateValue(uint64(n.interruptStatus), size), nil
	case regStatus:
		return truncateValue(uint64(n.status), size), nil
	case regConfigGen:
		return truncateValue(uint64(n.configGeneration), size), nil
	}

	if offset >= regConfig && offset+uint64(size) <= regConfig+8 {
		cfg := n.configBytesLocked()
		return truncateValue(readConfigValue(cfg[offset-regConfig:], size), size), nil
	}
	return 0, nil
}

func (n *Net) Write(addr uint64, size int, value uint64) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	offset := addr - n.Base
	switch offset {
	case regDeviceFeatSel:
		n.deviceFeatureSel = uint32(value)
	case regDriverFeatSel:
		n.driverFeatureSel = uint32(value)
	case regDriverFeatures:
		if n.driverFeatureSel == 0 {
			n.driverFeatures = (n.driverFeatures &^ 0xffffffff) | uint64(uint32(value))
		} else if n.driverFeatureSel == 1 {
			n.driverFeatures = (n.driverFeatures & 0xffffffff) | (uint64(uint32(value)) << 32)
		}
	case regQueueSel:
		n.queueSel = uint32(value)
	case regQueueNum:
		if q := n.selectedQueueLocked(); q != nil {
			q.size = uint16(value)
		}
	case regQueueReady:
		if q := n.selectedQueueLocked(); q != nil {
			q.ready = value != 0
			if value == 0 {
				q.lastAvailIdx = 0
				q.usedIdx = 0
			}
		}
	case regQueueDescLow:
		if q := n.selectedQueueLocked(); q != nil {
			n.setQueueAddr(&q.descAddr, uint32(value), true)
		}
	case regQueueDescHigh:
		if q := n.selectedQueueLocked(); q != nil {
			n.setQueueAddr(&q.descAddr, uint32(value), false)
		}
	case regQueueAvailLow:
		if q := n.selectedQueueLocked(); q != nil {
			n.setQueueAddr(&q.availAddr, uint32(value), true)
		}
	case regQueueAvailHigh:
		if q := n.selectedQueueLocked(); q != nil {
			n.setQueueAddr(&q.availAddr, uint32(value), false)
		}
	case regQueueUsedLow:
		if q := n.selectedQueueLocked(); q != nil {
			n.setQueueAddr(&q.usedAddr, uint32(value), true)
		}
	case regQueueUsedHigh:
		if q := n.selectedQueueLocked(); q != nil {
			n.setQueueAddr(&q.usedAddr, uint32(value), false)
		}
	case regInterruptAck:
		n.interruptStatus &^= uint32(value)
		return n.updateIRQLocked()
	case regStatus:
		n.status = uint32(value)
		if n.status == 0 {
			n.resetLocked()
		}
	case regQueueNotify:
		switch value {
		case netQueueTX:
			return n.processTXLocked()
		case netQueueRX:
			return n.processRXLocked()
		}
	}
	return nil
}

func (n *Net) EnqueueRxPacket(packet []byte) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.pendingRx = append(n.pendingRx, append([]byte(nil), packet...))
	return n.processRXLocked()
}

func (n *Net) processTXLocked() error {
	q := &n.queues[netQueueTX]
	if !q.ready || q.size == 0 || n.mem == nil {
		return nil
	}
	header, err := n.mem.ReadIPA(q.availAddr, 4)
	if err != nil {
		return err
	}
	availIdx := binary.LittleEndian.Uint16(header[2:4])
	for q.lastAvailIdx != availIdx {
		slot := q.lastAvailIdx % q.size
		entry, err := n.mem.ReadIPA(q.availAddr+4+uint64(slot)*2, 2)
		if err != nil {
			return err
		}
		head := binary.LittleEndian.Uint16(entry)
		data, err := n.readChainLocked(q, head, false)
		if err != nil {
			return err
		}
		if len(data) >= netHeaderLen {
			if err := fixTXChecksum(data); err != nil {
				return err
			}
			packet := data[netHeaderLen:]
			if n.backend != nil {
				if err := n.backend.HandleTxPacket(packet, nil); err != nil {
					return err
				}
			}
		}
		if err := n.writeUsedLocked(q, head, 0); err != nil {
			return err
		}
		q.lastAvailIdx++
	}
	n.interruptStatus |= intVring
	return n.updateIRQLocked()
}

func (n *Net) processRXLocked() error {
	q := &n.queues[netQueueRX]
	if !q.ready || q.size == 0 || n.mem == nil || len(n.pendingRx) == 0 {
		return nil
	}
	header, err := n.mem.ReadIPA(q.availAddr, 4)
	if err != nil {
		return err
	}
	availIdx := binary.LittleEndian.Uint16(header[2:4])
	processed := false
	for q.lastAvailIdx != availIdx && len(n.pendingRx) > 0 {
		slot := q.lastAvailIdx % q.size
		entry, err := n.mem.ReadIPA(q.availAddr+4+uint64(slot)*2, 2)
		if err != nil {
			return err
		}
		head := binary.LittleEndian.Uint16(entry)
		packet := n.pendingRx[0]
		written, err := n.writeRXPacketLocked(q, head, packet)
		if err != nil {
			return err
		}
		if err := n.writeUsedLocked(q, head, written); err != nil {
			return err
		}
		n.pendingRx = n.pendingRx[1:]
		q.lastAvailIdx++
		processed = true
	}
	if processed {
		n.interruptStatus |= intVring
		return n.updateIRQLocked()
	}
	return nil
}

func (n *Net) readChainLocked(q *queue, head uint16, writable bool) ([]byte, error) {
	var out []byte
	index := head
	for i := uint16(0); i < q.size; i++ {
		desc, err := n.readDescriptorLocked(q, index)
		if err != nil {
			return nil, err
		}
		isWrite := desc.flags&descFWrite != 0
		if isWrite == writable && desc.length > 0 {
			chunk, err := n.mem.ReadIPA(desc.addr, int(desc.length))
			if err != nil {
				return nil, err
			}
			out = append(out, chunk...)
		}
		if desc.flags&descFNext == 0 {
			return out, nil
		}
		index = desc.next
	}
	return nil, fmt.Errorf("virtio-net descriptor chain loop")
}

func (n *Net) writeRXPacketLocked(q *queue, head uint16, packet []byte) (uint32, error) {
	var hdr [netHeaderLen]byte
	binary.LittleEndian.PutUint16(hdr[10:12], 1)
	totalLen := netHeaderLen + len(packet)
	offset := 0
	index := head
	for i := uint16(0); i < q.size; i++ {
		desc, err := n.readDescriptorLocked(q, index)
		if err != nil {
			return uint32(offset), err
		}
		if desc.flags&descFWrite != 0 && desc.length > 0 && offset < totalLen {
			written, err := n.writeRXChunk(desc.addr, int(desc.length), hdr[:], packet, offset)
			if err != nil {
				return uint32(offset), err
			}
			offset += written
		}
		if desc.flags&descFNext == 0 {
			if offset < totalLen {
				return uint32(offset), fmt.Errorf("virtio-net RX chain too small: wrote %d of %d bytes", offset, totalLen)
			}
			return uint32(offset), nil
		}
		index = desc.next
	}
	return uint32(offset), fmt.Errorf("virtio-net RX descriptor chain loop")
}

func (n *Net) writeRXChunk(addr uint64, size int, hdr, packet []byte, offset int) (int, error) {
	buf := make([]byte, 0, size)
	if offset < len(hdr) {
		h := hdr[offset:]
		if len(h) > size {
			h = h[:size]
		}
		buf = append(buf, h...)
	}
	if len(buf) < size {
		packetOffset := offset - len(hdr)
		if packetOffset < 0 {
			packetOffset = 0
		}
		if packetOffset < len(packet) {
			p := packet[packetOffset:]
			if len(p) > size-len(buf) {
				p = p[:size-len(buf)]
			}
			buf = append(buf, p...)
		}
	}
	if len(buf) == 0 {
		return 0, nil
	}
	return len(buf), n.mem.WriteIPA(addr, buf)
}

func (n *Net) readDescriptorLocked(q *queue, index uint16) (descriptor, error) {
	if index >= q.size {
		return descriptor{}, fmt.Errorf("descriptor index %d out of range", index)
	}
	buf, err := n.mem.ReadIPA(q.descAddr+uint64(index)*16, 16)
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

func (n *Net) writeUsedLocked(q *queue, head uint16, usedLen uint32) error {
	slot := q.usedIdx % q.size
	var elem [8]byte
	binary.LittleEndian.PutUint32(elem[0:4], uint32(head))
	binary.LittleEndian.PutUint32(elem[4:8], usedLen)
	if err := n.mem.WriteIPA(q.usedAddr+4+uint64(slot)*8, elem[:]); err != nil {
		return err
	}
	q.usedIdx++
	var idx [2]byte
	binary.LittleEndian.PutUint16(idx[:], q.usedIdx)
	return n.mem.WriteIPA(q.usedAddr+2, idx[:])
}

func (n *Net) selectedQueueLocked() *queue {
	if n.queueSel >= uint32(len(n.queues)) {
		return nil
	}
	return &n.queues[n.queueSel]
}

func (n *Net) setQueueAddr(target *uint64, value uint32, low bool) {
	if low {
		*target = (*target &^ 0xffffffff) | uint64(value)
	} else {
		*target = (*target & 0xffffffff) | (uint64(value) << 32)
	}
}

func (n *Net) updateIRQLocked() error {
	if n.irq == nil {
		return nil
	}
	level := n.interruptStatus != 0
	if n.irqHigh == level {
		return nil
	}
	n.irqHigh = level
	return n.irq.SetIRQ(n.IRQ, level)
}

func (n *Net) configBytesLocked() []byte {
	var cfg [8]byte
	copy(cfg[0:6], n.MAC)
	binary.LittleEndian.PutUint16(cfg[6:8], netStatusLinkUp)
	return cfg[:]
}

func (n *Net) resetLocked() {
	n.deviceFeatureSel = 0
	n.driverFeatureSel = 0
	n.driverFeatures = 0
	n.queueSel = 0
	n.status = 0
	n.interruptStatus = 0
	n.irqHigh = false
	n.configGeneration++
	n.queues = [2]queue{}
	n.pendingRx = nil
}

func fixTXChecksum(frame []byte) error {
	if len(frame) < netHeaderLen {
		return nil
	}
	flags := frame[0]
	if flags&netHdrFlagNeedsChecksum == 0 {
		return nil
	}
	csumStart := int(binary.LittleEndian.Uint16(frame[6:8]))
	csumOffset := int(binary.LittleEndian.Uint16(frame[8:10]))
	packet := frame[netHeaderLen:]
	if csumStart < 0 || csumStart >= len(packet) || csumStart+csumOffset+2 > len(packet) {
		return fmt.Errorf("virtio-net checksum range out of packet: start=%d offset=%d len=%d", csumStart, csumOffset, len(packet))
	}
	binary.LittleEndian.PutUint16(packet[csumStart+csumOffset:csumStart+csumOffset+2], 0)
	sum := internetChecksum(packet[csumStart:])
	binary.BigEndian.PutUint16(packet[csumStart+csumOffset:csumStart+csumOffset+2], sum)
	return nil
}

func internetChecksum(data []byte) uint16 {
	var sum uint32
	for i := 0; i+1 < len(data); i += 2 {
		sum += uint32(binary.BigEndian.Uint16(data[i : i+2]))
	}
	if len(data)%2 == 1 {
		sum += uint32(data[len(data)-1]) << 8
	}
	for sum > 0xffff {
		sum = (sum >> 16) + (sum & 0xffff)
	}
	return ^uint16(sum)
}
