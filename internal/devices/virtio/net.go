package virtio

import (
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"

	"github.com/tinyrange/cc/internal/devices/pci"
	"github.com/tinyrange/cc/internal/fdt"
	"github.com/tinyrange/cc/internal/hv"
)

const (
	NetDefaultMMIOBase = 0xd0002000
	NetDefaultMMIOSize = 0x200
	NetDefaultIRQLine  = 7

	netQueueCount    = 2
	netQueueNumMax   = 256
	netVendorID      = 0x554d4551 // "QEMU"
	netVersion       = 2
	netDeviceID      = 1
	netInterruptBit  = 0x1
	netQueueReceive  = 0
	netQueueTransmit = 1
	netHeaderSize    = 12

	virtioNetHdrFNeedsCsum = 1 << 0
	virtioNetHdrFDataValid = 1 << 1

	virtioNetHdrGSOnone = 0

	etherTypeIPv4 = 0x0800
	etherTypeIPv6 = 0x86dd

	virtioNetFeatureMacBit    = 5
	virtioNetFeatureStatusBit = 16
	virtioFeatureEventIdx     = uint64(1) << virtioRingFeatureEventIdxBit

	virtioNetStatusLinkUp = 1

	virtqAvailFNoInterrupt = 1

	txBufferPoolMaxSize = 256 << 10
)

type virtioNetHeader struct {
	flags      uint8
	gsoType    uint8
	hdrLen     uint16
	gsoSize    uint16
	csumStart  uint16
	csumOffset uint16
	numBuffers uint16
}

type NetBackend interface {
	HandleTx(packet []byte, release func()) error
}

type netDeviceBinder interface {
	BindNetDevice(*Net)
}

type Net struct {
	device     device
	base       uint64
	size       uint64
	mac        net.HardwareAddr
	backend    NetBackend
	pendingRx  [][]byte
	rxMu       sync.Mutex
	rxDisabled bool
	linkUp     bool
	txBufPool  sync.Pool
	txSegPool  sync.Pool
	txHdrPool  sync.Pool
}

func NewNet(vm hv.VirtualMachine, base uint64, size uint64, irqLine uint32, mac net.HardwareAddr, backend NetBackend) *Net {
	if len(mac) != 6 {
		panic("virtio net requires 6-byte MAC address")
	}
	if backend == nil {
		backend = &discardNetBackend{}
	}
	netdev := &Net{
		device:  nil, // Will be set below
		base:    base,
		size:    size,
		mac:     append(net.HardwareAddr(nil), mac...),
		backend: backend,
		linkUp:  true,
		txBufPool: sync.Pool{
			New: func() any {
				return make([]byte, 0, 4096)
			},
		},
		txSegPool: sync.Pool{
			New: func() any {
				return make([][]byte, 0, 8)
			},
		},
		txHdrPool: sync.Pool{
			New: func() any {
				return make([]byte, 0, netHeaderSize)
			},
		},
	}
	features := []uint64{virtioFeatureVersion1 | (uint64(1) << virtioNetFeatureMacBit) | virtioFeatureEventIdx}
	netdev.device = newMMIODevice(vm, base, size, irqLine, netDeviceID, netVendorID, netVersion, features, netdev)
	if binder, ok := backend.(netDeviceBinder); ok {
		binder.BindNetDevice(netdev)
	}
	return netdev
}

func NewNetPCI(vm hv.VirtualMachine, host *pci.HostBridge, bus, device, function uint8, mac net.HardwareAddr, backend NetBackend) (*Net, error) {
	if len(mac) != 6 {
		return nil, fmt.Errorf("virtio net requires 6-byte MAC address")
	}
	if backend == nil {
		backend = &discardNetBackend{}
	}
	netdev := &Net{
		mac:     append(net.HardwareAddr(nil), mac...),
		backend: backend,
		linkUp:  true,
		txBufPool: sync.Pool{
			New: func() any {
				return make([]byte, 0, 4096)
			},
		},
		txSegPool: sync.Pool{
			New: func() any {
				return make([][]byte, 0, 8)
			},
		},
		txHdrPool: sync.Pool{
			New: func() any {
				return make([]byte, 0, netHeaderSize)
			},
		},
	}
	features := []uint64{virtioFeatureVersion1 | (uint64(1) << virtioNetFeatureMacBit) | virtioFeatureEventIdx}
	pciDev, err := NewVirtioPCIDevice(vm, host, bus, device, function, uint16(netDeviceID), uint16(netDeviceID), features, netdev)
	if err != nil {
		return nil, err
	}
	netdev.device = pciDev
	if binder, ok := backend.(netDeviceBinder); ok {
		binder.BindNetDevice(netdev)
	}
	return netdev, nil
}

// Init implements hv.MemoryMappedIODevice.
func (vn *Net) Init(vm hv.VirtualMachine) error {
	if mmio, ok := vn.device.(*mmioDevice); ok && vm != nil {
		mmio.vm = vm
	}
	return nil
}

// MMIORegions implements hv.MemoryMappedIODevice.
func (vn *Net) MMIORegions() []hv.MMIORegion {
	if vn.size == 0 {
		return nil
	}
	return []hv.MMIORegion{{
		Address: vn.base,
		Size:    vn.size,
	}}
}

// ReadMMIO implements hv.MemoryMappedIODevice.
func (vn *Net) ReadMMIO(addr uint64, data []byte) error {
	if vn.device == nil {
		return fmt.Errorf("virtio-net: device not initialized")
	}
	return vn.device.readMMIO(addr, data)
}

// WriteMMIO implements hv.MemoryMappedIODevice.
func (vn *Net) WriteMMIO(addr uint64, data []byte) error {
	if vn.device == nil {
		return fmt.Errorf("virtio-net: device not initialized")
	}
	return vn.device.writeMMIO(addr, data)
}

func (vn *Net) NumQueues() int {
	return netQueueCount
}

func (vn *Net) QueueMaxSize(int) uint16 {
	return netQueueNumMax
}

func (vn *Net) OnReset(device) {
	vn.rxMu.Lock()
	defer vn.rxMu.Unlock()
	vn.pendingRx = nil
	vn.rxDisabled = false
	vn.linkUp = true
}

func (vn *Net) OnQueueNotify(dev device, queue int) error {
	switch queue {
	case netQueueTransmit:
		return vn.processTransmitQueue(dev, dev.queue(queue))
	case netQueueReceive:
		return vn.processReceiveQueue(dev, dev.queue(queue))
	default:
		return nil
	}
}

func (vn *Net) ReadConfig(_ device, offset uint64) (uint32, bool, error) {
	cfg := offset
	if cfg >= VIRTIO_MMIO_CONFIG {
		cfg -= VIRTIO_MMIO_CONFIG
	}

	// Build config space: 6 bytes MAC + 2 bytes status
	var configSpace [8]byte
	copy(configSpace[0:6], vn.mac)
	if vn.linkUp {
		configSpace[6] = 1 // status low byte
	}
	// configSpace[7] = 0 // status high byte (already zero)

	// Return 4-byte window at requested offset
	idx := int(cfg)
	if idx < 0 || idx >= len(configSpace) {
		return 0, false, nil
	}

	var w [4]byte
	for i := 0; i < 4; i++ {
		if idx+i < len(configSpace) {
			w[i] = configSpace[idx+i]
		}
	}
	return binary.LittleEndian.Uint32(w[:]), true, nil
}

func (vn *Net) WriteConfig(device, uint64, uint32) (bool, error) {
	return false, nil
}

func (vn *Net) EnqueueRxPacket(packet []byte) error {
	vn.rxMu.Lock()
	defer vn.rxMu.Unlock()
	if vn.rxDisabled {
		return io.EOF
	}
	pendingBefore := len(vn.pendingRx)
	vn.pendingRx = append(vn.pendingRx, append([]byte(nil), packet...))
	if vn.device != nil {
		if err := vn.processReceiveQueueLocked(vn.device, vn.device.queue(netQueueReceive)); err != nil {
			return err
		}
		pendingAfter := len(vn.pendingRx)
		delivered := pendingBefore + 1 - pendingAfter
		if delivered > 0 {
			// slog.Info("virtio-net: delivered rx packet", "packet", packet)
		} else {
			// Packet is stuck in pendingRx - log diagnostic info
			q := vn.device.queue(netQueueReceive)
			if q != nil && q.ready {
				_, avail, _, err := vn.device.queuePointers(q)
				if err == nil {
					availIdx := binary.LittleEndian.Uint16(avail[2:4])
					slog.Warn("virtio-net: rx packet queued (no buffers available)",
						"pending", len(vn.pendingRx),
						"lastAvailIdx", q.lastAvailIdx,
						"availIdx", availIdx,
						"queueReady", q.ready,
						"queueSize", q.size)
				} else {
					slog.Warn("virtio-net: rx packet queued (no buffers available)",
						"pending", len(vn.pendingRx),
						"err", err)
				}
			} else {
				slog.Warn("virtio-net: rx packet queued (queue not ready)",
					"pending", len(vn.pendingRx),
					"queueReady", q != nil && q.ready)
			}
		}
	}
	return nil
}

func (vn *Net) processTransmitQueue(dev device, q *queue) error {
	if q == nil || !q.ready || q.size == 0 {
		return nil
	}
	descTable, avail, _, err := dev.queuePointers(q)
	if err != nil {
		return err
	}

	availIdx := binary.LittleEndian.Uint16(avail[2:4])
	suppressInterrupt := binary.LittleEndian.Uint16(avail[0:2])&virtqAvailFNoInterrupt != 0

	oldUsedIdx := q.usedIdx
	var processed uint16

	for q.lastAvailIdx != availIdx {
		ringIndex := q.lastAvailIdx % q.size
		ringOffset := 4 + int(ringIndex)*2
		if ringOffset+2 > len(avail) {
			return fmt.Errorf("net tx avail ring offset %d out of bounds", ringOffset)
		}
		head := binary.LittleEndian.Uint16(avail[ringOffset : ringOffset+2])
		packet, headerBytes, err := vn.collectTxDescriptorChain(dev, descTable, q, head)
		if err != nil {
			return err
		}
		release := vn.makeTxRelease(packet)
		hdr, err := parseVirtioNetHeader(headerBytes)
		vn.putTxHeaderBuffer(headerBytes)
		if err != nil {
			release()
			return err
		}
		// slog.Info("virtio-net: preparing tx packet", "hdr", hdr, "packet", packet)
		if err := vn.prepareTxPacket(hdr, packet); err != nil {
			release()
			return err
		}
		if err := vn.backend.HandleTx(packet, release); err != nil {
			release()
			return err
		}
		if err := dev.recordUsedElement(q, head, 0); err != nil {
			release()
			return err
		}
		q.lastAvailIdx++
		processed++
	}

	if processed == 0 {
		return nil
	}

	if dev.eventIdxEnabled() {
		if err := dev.setAvailEvent(q, q.lastAvailIdx); err != nil {
			return err
		}
	}

	newUsedIdx := q.usedIdx
	if vn.shouldTriggerTxInterrupt(dev, q, avail, oldUsedIdx, newUsedIdx, suppressInterrupt) {
		dev.raiseInterrupt(netInterruptBit)
	}

	return nil
}

func (vn *Net) processReceiveQueue(dev device, q *queue) error {
	vn.rxMu.Lock()
	defer vn.rxMu.Unlock()
	return vn.processReceiveQueueLocked(dev, q)
}

func (vn *Net) processReceiveQueueLocked(dev device, q *queue) error {
	if q == nil || !q.ready || q.size == 0 {
		if len(vn.pendingRx) > 0 {
			queueSize := uint16(0)
			queueReady := false
			if q != nil {
				queueSize = q.size
				queueReady = q.ready
			}
			slog.Debug("virtio-net: rx queue not ready", "pending", len(vn.pendingRx), "ready", queueReady, "size", queueSize)
		}
		return nil
	}
	if len(vn.pendingRx) == 0 {
		return nil
	}

	descTable, avail, _, err := dev.queuePointers(q)
	if err != nil {
		return err
	}

	availIdx := binary.LittleEndian.Uint16(avail[2:4])
	suppressInterrupt := binary.LittleEndian.Uint16(avail[0:2])&virtqAvailFNoInterrupt != 0
	oldUsedIdx := q.usedIdx

	var packetIndex int
	var processed uint16

	// Log diagnostic info if we have pending packets but no available buffers
	if q.lastAvailIdx == availIdx && len(vn.pendingRx) > 0 {
		slog.Debug("virtio-net: rx queue has no available buffers",
			"pending", len(vn.pendingRx),
			"lastAvailIdx", q.lastAvailIdx,
			"availIdx", availIdx)
	}

	for q.lastAvailIdx != availIdx && packetIndex < len(vn.pendingRx) {
		packet := vn.pendingRx[packetIndex]

		ringIndex := q.lastAvailIdx % q.size
		ringOffset := 4 + int(ringIndex)*2
		if ringOffset+2 > len(avail) {
			return fmt.Errorf("net rx avail ring offset %d out of bounds", ringOffset)
		}
		head := binary.LittleEndian.Uint16(avail[ringOffset : ringOffset+2])

		written, consumed, err := vn.fillRxDescriptorChain(dev, descTable, q, head, packet)
		if err != nil {
			return err
		}
		if !consumed {
			break
		}
		if err := dev.recordUsedElement(q, head, written); err != nil {
			return err
		}
		packetIndex++
		q.lastAvailIdx++
		processed++
	}

	if packetIndex > 0 {
		if packetIndex >= len(vn.pendingRx) {
			vn.pendingRx = vn.pendingRx[:0]
		} else {
			vn.pendingRx = vn.pendingRx[packetIndex:]
		}
	}

	if processed == 0 {
		return nil
	}

	if dev.eventIdxEnabled() {
		if err := dev.setAvailEvent(q, q.lastAvailIdx); err != nil {
			return err
		}
	}

	newUsedIdx := q.usedIdx
	if vn.shouldTriggerTxInterrupt(dev, q, avail, oldUsedIdx, newUsedIdx, suppressInterrupt) {
		dev.raiseInterrupt(netInterruptBit)
	}
	return nil
}

func (vn *Net) collectTxDescriptorChain(dev device, descTable []byte, q *queue, head uint16) ([]byte, []byte, error) {
	index := head
	headerRemaining := netHeaderSize
	headerBytes := vn.getTxHeaderBuffer()
	if cap(headerBytes) < netHeaderSize {
		headerBytes = make([]byte, 0, netHeaderSize)
	} else {
		headerBytes = headerBytes[:0]
	}
	segments := vn.getTxSegments()
	defer vn.putTxSegments(segments)
	totalPayload := 0

	for i := uint16(0); i < q.size; i++ {
		offset := int(index) * 16
		if offset+16 > len(descTable) {
			vn.putTxHeaderBuffer(headerBytes)
			return nil, nil, fmt.Errorf("net tx descriptor %d out of bounds", index)
		}
		addr := binary.LittleEndian.Uint64(descTable[offset : offset+8])
		length := binary.LittleEndian.Uint32(descTable[offset+8 : offset+12])
		flags := binary.LittleEndian.Uint16(descTable[offset+12 : offset+14])
		next := binary.LittleEndian.Uint16(descTable[offset+14 : offset+16])

		if flags&virtqDescFWrite != 0 {
			vn.putTxHeaderBuffer(headerBytes)
			return nil, nil, fmt.Errorf("net tx descriptor %d unexpectedly writable", index)
		}

		if length > 0 {
			data, err := dev.memSlice(addr, uint64(length))
			if err != nil {
				vn.putTxHeaderBuffer(headerBytes)
				return nil, nil, err
			}
			consumed := 0
			if headerRemaining > 0 {
				toConsume := headerRemaining
				if toConsume > len(data) {
					toConsume = len(data)
				}
				consumed = toConsume
				headerRemaining -= toConsume
				headerBytes = append(headerBytes, data[:consumed]...)
			}
			if consumed < len(data) {
				payload := data[consumed:]
				segments = append(segments, payload)
				totalPayload += len(payload)
			}
		}

		if flags&virtqDescFNext == 0 {
			if headerRemaining > 0 {
				return nil, nil, fmt.Errorf("net tx header truncated in descriptor %d", index)
			}
			break
		}
		index = next
	}

	if headerRemaining > 0 {
		vn.putTxHeaderBuffer(headerBytes)
		return nil, nil, fmt.Errorf("net tx descriptor chain shorter than header")
	}

	var packet []byte
	if totalPayload == 0 {
		packet = vn.getTxBuffer(0)
	} else {
		buf := vn.getTxBuffer(totalPayload)
		if cap(buf) < totalPayload {
			vn.putTxBuffer(buf)
			buf = make([]byte, totalPayload)
		}
		packet = buf[:totalPayload]
		offset := 0
		for _, seg := range segments {
			offset += copy(packet[offset:], seg)
		}
	}

	return packet, headerBytes, nil
}

func (vn *Net) getTxBuffer(size int) []byte {
	if size <= 0 {
		return nil
	}
	if size > txBufferPoolMaxSize {
		return make([]byte, size)
	}
	if raw := vn.txBufPool.Get(); raw != nil {
		buf := raw.([]byte)
		if cap(buf) >= size {
			return buf[:size]
		}
		vn.txBufPool.Put(buf[:0])
	}
	return make([]byte, size)
}

func (vn *Net) putTxBuffer(buf []byte) {
	if buf == nil {
		return
	}
	if cap(buf) == 0 || cap(buf) > txBufferPoolMaxSize {
		return
	}
	vn.txBufPool.Put(buf[:0])
}

func (vn *Net) getTxHeaderBuffer() []byte {
	if raw := vn.txHdrPool.Get(); raw != nil {
		return raw.([]byte)[:0]
	}
	return make([]byte, 0, netHeaderSize)
}

func (vn *Net) putTxHeaderBuffer(buf []byte) {
	if buf == nil {
		return
	}
	if cap(buf) < netHeaderSize || cap(buf) > 256 {
		return
	}
	vn.txHdrPool.Put(buf[:0])
}

func (vn *Net) getTxSegments() [][]byte {
	if raw := vn.txSegPool.Get(); raw != nil {
		return raw.([][]byte)[:0]
	}
	return make([][]byte, 0, 8)
}

func (vn *Net) putTxSegments(segs [][]byte) {
	for i := range segs {
		segs[i] = nil
	}
	if cap(segs) == 0 || cap(segs) > 32 {
		return
	}
	vn.txSegPool.Put(segs[:0])
}

func (vn *Net) makeTxRelease(buf []byte) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			vn.putTxBuffer(buf)
		})
	}
}

func (vn *Net) shouldTriggerTxInterrupt(dev device, q *queue, avail []byte, oldUsedIdx, newUsedIdx uint16, suppressInterrupt bool) bool {
	if suppressInterrupt {
		return false
	}
	if !dev.eventIdxEnabled() {
		return true
	}
	usedEventOffset := 4 + int(q.size)*2
	if usedEventOffset+2 > len(avail) {
		// Malformed ring, best-effort wakeup.
		return true
	}
	usedEvent := binary.LittleEndian.Uint16(avail[usedEventOffset : usedEventOffset+2])
	return vringNeedEvent(usedEvent, newUsedIdx, oldUsedIdx)
}

func vringNeedEvent(eventIdx, newIdx, oldIdx uint16) bool {
	return uint16(newIdx-eventIdx-1) < uint16(newIdx-oldIdx)
}

func parseVirtioNetHeader(headerBytes []byte) (virtioNetHeader, error) {
	if len(headerBytes) < 10 {
		return virtioNetHeader{}, fmt.Errorf("virtio-net header too short: %d", len(headerBytes))
	}
	hdr := virtioNetHeader{
		flags:      headerBytes[0],
		gsoType:    headerBytes[1],
		hdrLen:     binary.LittleEndian.Uint16(headerBytes[2:4]),
		gsoSize:    binary.LittleEndian.Uint16(headerBytes[4:6]),
		csumStart:  binary.LittleEndian.Uint16(headerBytes[6:8]),
		csumOffset: binary.LittleEndian.Uint16(headerBytes[8:10]),
	}
	if len(headerBytes) >= 12 {
		hdr.numBuffers = binary.LittleEndian.Uint16(headerBytes[10:12])
	}
	return hdr, nil
}

func (vn *Net) prepareTxPacket(hdr virtioNetHeader, packet []byte) error {
	if hdr.gsoType != virtioNetHdrGSOnone {
		return fmt.Errorf("unsupported virtio-net gso type %d", hdr.gsoType)
	}
	if hdr.flags&virtioNetHdrFNeedsCsum != 0 {
		if err := applyChecksum(hdr, packet); err != nil {
			return err
		}
	}
	return nil
}

func applyChecksum(hdr virtioNetHeader, packet []byte) error {
	csStart := int(hdr.csumStart)
	csOffset := int(hdr.csumOffset)
	if csStart < 0 || csStart > len(packet) {
		return fmt.Errorf("virtio-net checksum start %d out of range", csStart)
	}
	checksumPos := csStart + csOffset
	if checksumPos < 0 || checksumPos+2 > len(packet) {
		return fmt.Errorf("virtio-net checksum offset %d out of range", checksumPos)
	}
	packet[checksumPos] = 0
	packet[checksumPos+1] = 0

	if len(packet) < 14 {
		return fmt.Errorf("virtio-net packet too small for ethernet header: %d", len(packet))
	}
	ethType := binary.BigEndian.Uint16(packet[12:14])

	var sum uint32
	switch ethType {
	case etherTypeIPv4:
		if len(packet) < 34 {
			return fmt.Errorf("virtio-net ipv4 packet too small: %d", len(packet))
		}
		ipHeader := packet[14:]
		ihl := int(ipHeader[0]&0x0f) * 4
		if len(ipHeader) < ihl {
			return fmt.Errorf("virtio-net ipv4 header length %d larger than packet %d", ihl, len(ipHeader))
		}
		payload := packet[csStart:]
		var pseudo [12]byte
		copy(pseudo[0:4], ipHeader[12:16])
		copy(pseudo[4:8], ipHeader[16:20])
		pseudo[9] = ipHeader[9]
		binary.BigEndian.PutUint16(pseudo[10:12], uint16(len(payload)))
		sum = checksumAdd(0, pseudo[:])
		sum = checksumAdd(sum, payload)
	case etherTypeIPv6:
		if len(packet) < 54 {
			return fmt.Errorf("virtio-net ipv6 packet too small: %d", len(packet))
		}
		ipHeader := packet[14:]
		payload := packet[csStart:]
		var pseudo [40]byte
		copy(pseudo[0:16], ipHeader[8:24]) // Source
		copy(pseudo[16:32], ipHeader[24:40])
		binary.BigEndian.PutUint32(pseudo[32:36], uint32(len(payload)))
		pseudo[39] = ipHeader[6]
		sum = checksumAdd(0, pseudo[:])
		sum = checksumAdd(sum, payload)
	default:
		sum = checksumAdd(0, packet[csStart:])
	}
	checksum := checksumFinalize(sum)
	if checksum == 0 {
		checksum = 0xffff
	}
	binary.BigEndian.PutUint16(packet[checksumPos:], checksum)
	return nil
}

func checksumAdd(sum uint32, data []byte) uint32 {
	for len(data) >= 2 {
		sum += uint32(binary.BigEndian.Uint16(data[:2]))
		data = data[2:]
	}
	if len(data) == 1 {
		sum += uint32(data[0]) << 8
	}
	return sum
}

func checksumFinalize(sum uint32) uint16 {
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return ^uint16(sum)
}

type rxDescriptor struct {
	addr   uint64
	length uint32
}

func (vn *Net) fillRxDescriptorChain(dev device, descTable []byte, q *queue, head uint16, packet []byte) (uint32, bool, error) {
	index := head
	var descriptors []rxDescriptor

	for i := uint16(0); i < q.size; i++ {
		offset := int(index) * 16
		if offset+16 > len(descTable) {
			return 0, false, fmt.Errorf("net rx descriptor %d out of bounds", index)
		}
		addr := binary.LittleEndian.Uint64(descTable[offset : offset+8])
		length := binary.LittleEndian.Uint32(descTable[offset+8 : offset+12])
		flags := binary.LittleEndian.Uint16(descTable[offset+12 : offset+14])
		next := binary.LittleEndian.Uint16(descTable[offset+14 : offset+16])

		if flags&virtqDescFWrite == 0 {
			return 0, false, fmt.Errorf("net rx descriptor %d not writable", index)
		}

		descriptors = append(descriptors, rxDescriptor{addr: addr, length: length})

		if flags&virtqDescFNext == 0 {
			break
		}
		index = next
	}

	if len(descriptors) == 0 {
		return 0, false, fmt.Errorf("net rx descriptor chain empty")
	}

	if descriptors[0].length < netHeaderSize {
		return 0, false, fmt.Errorf("net rx first descriptor too small for header")
	}

	required := uint32(len(packet)) + netHeaderSize
	var available uint64
	for _, d := range descriptors {
		available += uint64(d.length)
	}
	if available < uint64(required) {
		return 0, false, nil
	}

	bytesRemaining := packet
	buffersUsed := uint16(1)
	for i, desc := range descriptors {
		if desc.length == 0 {
			continue
		}
		data, err := dev.memSlice(desc.addr, uint64(desc.length))
		if err != nil {
			return 0, false, err
		}
		var bytesWritten int
		if i == 0 {
			// First descriptor: zero header, write packet data, set buffersUsed
			for j := 0; j < netHeaderSize && j < len(data); j++ {
				data[j] = 0
			}
			copyLen := copy(data[netHeaderSize:], bytesRemaining)
			bytesRemaining = bytesRemaining[copyLen:]
			if len(data) >= 12 {
				binary.LittleEndian.PutUint16(data[10:12], buffersUsed)
			}
			// Write back at least netHeaderSize bytes (to include buffersUsed field),
			// plus any packet data we copied
			bytesWritten = netHeaderSize + copyLen
			if bytesWritten > len(data) {
				bytesWritten = len(data)
			}
		} else {
			// Subsequent descriptors: write packet data
			copyLen := copy(data, bytesRemaining)
			bytesRemaining = bytesRemaining[copyLen:]
			bytesWritten = copyLen
			if copyLen > 0 {
				buffersUsed++
			}
		}
		// Write the modified data back to guest memory
		if bytesWritten > 0 {
			if err := dev.writeGuest(desc.addr, data[:bytesWritten]); err != nil {
				return 0, false, fmt.Errorf("write guest memory for rx descriptor %d: %w", i, err)
			}
		}
		if len(bytesRemaining) == 0 {
			break
		}
	}

	if len(bytesRemaining) != 0 {
		return 0, false, fmt.Errorf("net rx bytes remaining after copy")
	}

	return required, true, nil
}

type discardNetBackend struct{}

func (d *discardNetBackend) HandleTx(_ []byte, release func()) error {
	if release != nil {
		release()
	}
	return nil
}

// NetTemplate is a template for creating virtio-net devices
type NetTemplate struct {
	Backend NetBackend
	MAC     net.HardwareAddr
	Arch    hv.CpuArchitecture
	IRQLine uint32
}

func (t NetTemplate) archOrDefault(vm hv.VirtualMachine) hv.CpuArchitecture {
	if t.Arch != "" && t.Arch != hv.ArchitectureInvalid {
		return t.Arch
	}
	if vm != nil && vm.Hypervisor() != nil {
		return vm.Hypervisor().Architecture()
	}
	return hv.ArchitectureInvalid
}

func (t NetTemplate) irqLineForArch(arch hv.CpuArchitecture) uint32 {
	if t.IRQLine != 0 {
		return t.IRQLine
	}
	if arch == hv.ArchitectureARM64 {
		return NetDefaultIRQLine + 1 // ARM64 might use different IRQ
	}
	return NetDefaultIRQLine
}

// GetLinuxCommandLineParam implements VirtioMMIODevice.
func (t NetTemplate) GetLinuxCommandLineParam() ([]string, error) {
	irqLine := t.irqLineForArch(t.Arch)
	param := fmt.Sprintf(
		"virtio_mmio.device=4k@0x%x:%d",
		NetDefaultMMIOBase,
		irqLine,
	)
	return []string{param}, nil
}

// DeviceTreeNodes implements VirtioMMIODevice.
func (t NetTemplate) DeviceTreeNodes() ([]fdt.Node, error) {
	irqLine := t.irqLineForArch(t.Arch)
	node := fdt.Node{
		Name: fmt.Sprintf("virtio@%x", NetDefaultMMIOBase),
		Properties: map[string]fdt.Property{
			"compatible": {Strings: []string{"virtio,mmio"}},
			"reg":        {U64: []uint64{NetDefaultMMIOBase, NetDefaultMMIOSize}},
			"interrupts": {U32: []uint32{0, irqLine, 4}},
			"status":     {Strings: []string{"okay"}},
		},
	}
	return []fdt.Node{node}, nil
}

// GetACPIDeviceInfo implements VirtioMMIODevice.
func (t NetTemplate) GetACPIDeviceInfo() ACPIDeviceInfo {
	irqLine := t.irqLineForArch(t.archOrDefault(nil))
	return ACPIDeviceInfo{
		BaseAddr: NetDefaultMMIOBase,
		Size:     NetDefaultMMIOSize,
		GSI:      irqLine,
	}
}

func (t NetTemplate) Create(vm hv.VirtualMachine) (hv.Device, error) {
	arch := t.archOrDefault(vm)
	irqLine := t.irqLineForArch(arch)
	mac := t.MAC
	if mac == nil || len(mac) != 6 {
		// Generate a random MAC if not provided
		mac = net.HardwareAddr{0x02, 0x00, 0x00, 0x00, 0x00, 0x01}
	}
	backend := t.Backend
	if backend == nil {
		backend = &discardNetBackend{}
	}
	netdev := NewNet(vm, NetDefaultMMIOBase, NetDefaultMMIOSize, EncodeIRQLineForArch(arch, irqLine), mac, backend)
	if err := netdev.Init(vm); err != nil {
		return nil, fmt.Errorf("virtio-net: initialize device: %w", err)
	}
	return netdev, nil
}

var (
	_ hv.DeviceTemplate = NetTemplate{}
	_ VirtioMMIODevice  = NetTemplate{}
	_ deviceHandler     = (*Net)(nil)
)
