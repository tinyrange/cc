package virtio

import (
	"encoding/binary"
	"fmt"
	"log"
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
	virtioNetFeatureMrgRxBuf  = 15
	virtioNetFeatureStatusBit = 16
	virtioFeatureEventIdx     = uint64(1) << virtioRingFeatureEventIdxBit

	virtioNetStatusLinkUp = 1

	virtqAvailFNoInterrupt = 1

	txBufferPoolMaxSize = 256 << 10

	// Backpressure limit for packets awaiting guest RX buffers.
	// Without this, a fast host-side producer (e.g. outbound TCP proxy) can queue
	// unbounded RX packets if the guest is slow to replenish descriptors.
	netMaxPendingRxPackets = 256
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

type netWorkKind uint8

const (
	netWorkKick netWorkKind = iota
	netWorkRxFrame
	netWorkReset
)

type netWorkMsg struct {
	kind  netWorkKind
	queue int
	frame []byte
	dev   device
	resp  chan error
}

type Net struct {
	device    device
	base      uint64
	size      uint64
	mac       net.HardwareAddr
	backend   NetBackend
	pendingRx [][]byte
	linkUp    bool
	txBufPool sync.Pool
	txSegPool sync.Pool
	txHdrPool sync.Pool

	workOnce sync.Once
	workCh   chan netWorkMsg
	rxSlots  chan struct{}
	workDev  device
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
	features := []uint64{virtioFeatureVersion1 | (uint64(1) << virtioNetFeatureMacBit) | (uint64(1) << virtioNetFeatureMrgRxBuf)}
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
	features := []uint64{virtioFeatureVersion1 | (uint64(1) << virtioNetFeatureMacBit) | (uint64(1) << virtioNetFeatureMrgRxBuf)}
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

func (vn *Net) ensureWorker(dev device) {
	vn.workOnce.Do(func() {
		if dev == nil {
			dev = vn.device
		}
		vn.workDev = dev
		vn.workCh = make(chan netWorkMsg, netMaxPendingRxPackets+128)
		vn.rxSlots = make(chan struct{}, netMaxPendingRxPackets)
		go vn.workerLoop()
	})
}

func (vn *Net) workerLoop() {
	for msg := range vn.workCh {
		dev := msg.dev
		if dev == nil {
			dev = vn.workDev
		}
		var err error
		switch msg.kind {
		case netWorkKick:
			switch msg.queue {
			case netQueueTransmit:
				err = vn.processTransmitQueue(dev, dev.queue(msg.queue))
			case netQueueReceive:
				err = vn.processReceiveQueueLocked(dev, dev.queue(msg.queue))
			default:
				err = nil
			}
		case netWorkRxFrame:
			// vn.rxSlots is acquired by the sender; we release when the packet is
			// actually removed from vn.pendingRx (after delivery or reset).
			pendingBefore := len(vn.pendingRx)
			vn.pendingRx = append(vn.pendingRx, msg.frame)
			err = vn.processReceiveQueueLocked(dev, dev.queue(netQueueReceive))
			pendingAfter := len(vn.pendingRx)
			// If the packet wasn't consumed, emit diagnostics at key thresholds.
			// This helps debug flaky stalls where host RX builds up waiting on
			// guest-provided RX buffers.
			if err == nil && pendingAfter > pendingBefore {
				if pendingAfter == 1 || pendingAfter == 8 || pendingAfter == 32 ||
					pendingAfter == 64 || pendingAfter == 128 || pendingAfter == netMaxPendingRxPackets {
					q := dev.queue(netQueueReceive)
					queueReady := q != nil && q.ready
					queueSize := uint16(0)
					lastAvail := uint16(0)
					availIdx := uint16(0)
					if q != nil {
						queueSize = q.size
						lastAvail = q.lastAvailIdx
					}
					if q != nil && q.ready {
						_, avail, _, e := dev.queuePointers(q)
						if e == nil && len(avail) >= 4 {
							availIdx = binary.LittleEndian.Uint16(avail[2:4])
						}
					}
					log.Printf(
						"virtio-net: rx pending (waiting on guest buffers) pending=%d queueReady=%t queueSize=%d lastAvailIdx=%d availIdx=%d",
						pendingAfter, queueReady, queueSize, lastAvail, availIdx,
					)
				}
			}
		case netWorkReset:
			// Clear pending RX and release backpressure tokens.
			for range vn.pendingRx {
				select {
				case <-vn.rxSlots:
				default:
				}
			}
			vn.pendingRx = nil
			err = nil
		default:
			err = nil
		}
		if msg.resp != nil {
			msg.resp <- err
		}
	}
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

func (vn *Net) OnReset(dev device) {
	vn.ensureWorker(dev)
	done := make(chan error, 1)
	vn.workCh <- netWorkMsg{kind: netWorkReset, dev: dev, resp: done}
	_ = <-done
	vn.linkUp = true
}

func (vn *Net) OnQueueNotify(dev device, queue int) error {
	vn.ensureWorker(dev)
	done := make(chan error, 1)
	vn.workCh <- netWorkMsg{kind: netWorkKick, queue: queue, dev: dev, resp: done}
	return <-done
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
	vn.ensureWorker(vn.device)

	// Backpressure: block when too many packets are queued awaiting RX buffers.
	select {
	case vn.rxSlots <- struct{}{}:
	default:
		log.Printf("virtio-net: rxSlots full (pending=%d)", len(vn.rxSlots))
		vn.rxSlots <- struct{}{}
	}

	msg := netWorkMsg{
		kind:  netWorkRxFrame,
		frame: append([]byte(nil), packet...),
		dev:   vn.device,
		resp:  make(chan error, 1),
	}
	vn.workCh <- msg
	err := <-msg.resp
	return err
}

func (vn *Net) shouldTriggerInterrupt(dev device, q *queue, oldUsedIdx, newUsedIdx uint16, suppressInterrupt bool) bool {
	// If EVENT_IDX is negotiated, the device must ignore VIRTQ_AVAIL_F_NO_INTERRUPT
	// and instead consult the used_event field to determine whether an interrupt
	// is needed.
	if dev.eventIdxEnabled() {
		usedEventOffset := q.availAddr + 4 + uint64(q.size)*2
		raw, err := dev.readGuest(usedEventOffset, 2)
		if err != nil || len(raw) < 2 {
			// Malformed ring, best-effort wakeup.
			return true
		}
		usedEvent := binary.LittleEndian.Uint16(raw[:2])
		return vringNeedEvent(usedEvent, newUsedIdx, oldUsedIdx)
	}
	if suppressInterrupt {
		return false
	}
	return true
}

func (vn *Net) processTransmitQueue(dev device, q *queue) error {
	if q == nil || !q.ready || q.size == 0 {
		return nil
	}
	flags, availIdx, err := dev.readAvailState(q)
	if err != nil {
		return err
	}
	suppressInterrupt := flags&virtqAvailFNoInterrupt != 0

	oldUsedIdx := q.usedIdx
	var processed uint16

	for q.lastAvailIdx != availIdx {
		ringIndex := q.lastAvailIdx % q.size
		head, err := dev.readAvailEntry(q, ringIndex)
		if err != nil {
			return err
		}
		packet, headerBytes, err := vn.collectTxDescriptorChain(dev, q, head)
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
	if vn.shouldTriggerInterrupt(dev, q, oldUsedIdx, newUsedIdx, suppressInterrupt) {
		dev.raiseInterrupt(netInterruptBit)
	}

	return nil
}

func (vn *Net) processReceiveQueue(dev device, q *queue) error {
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

	flags, availIdx, err := dev.readAvailState(q)
	if err != nil {
		return err
	}
	suppressInterrupt := flags&virtqAvailFNoInterrupt != 0
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
		head, err := dev.readAvailEntry(q, ringIndex)
		if err != nil {
			return err
		}

		written, consumed, err := vn.fillRxDescriptorChain(dev, q, head, packet)
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
		// Release backpressure slots for packets removed from pendingRx.
		for i := 0; i < packetIndex; i++ {
			select {
			case <-vn.rxSlots:
			default:
			}
		}
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
	if vn.shouldTriggerInterrupt(dev, q, oldUsedIdx, newUsedIdx, suppressInterrupt) {
		dev.raiseInterrupt(netInterruptBit)
	}
	return nil
}

func (vn *Net) collectTxDescriptorChain(dev device, q *queue, head uint16) ([]byte, []byte, error) {
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
		desc, err := dev.readDescriptor(q, index)
		if err != nil {
			vn.putTxHeaderBuffer(headerBytes)
			return nil, nil, err
		}
		addr := desc.addr
		length := desc.length
		flags := desc.flags
		next := desc.next

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

func (vn *Net) fillRxDescriptorChain(dev device, q *queue, head uint16, packet []byte) (uint32, bool, error) {
	index := head
	var descriptors []rxDescriptor

	for i := uint16(0); i < q.size; i++ {
		desc, err := dev.readDescriptor(q, index)
		if err != nil {
			return 0, false, err
		}
		addr := desc.addr
		length := desc.length
		flags := desc.flags
		next := desc.next

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
	var (
		firstDescAddr     uint64
		firstDescData     []byte
		firstBytesWritten int
	)
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
			firstDescAddr = desc.addr
			firstDescData = data

			// First descriptor: zero header, write packet data, set buffersUsed
			for j := 0; j < netHeaderSize && j < len(data); j++ {
				data[j] = 0
			}
			copyLen := copy(data[netHeaderSize:], bytesRemaining)
			bytesRemaining = bytesRemaining[copyLen:]
			// Write back at least netHeaderSize bytes (to include buffersUsed field),
			// plus any packet data we copied
			bytesWritten = netHeaderSize + copyLen
			if bytesWritten > len(data) {
				bytesWritten = len(data)
			}
			firstBytesWritten = bytesWritten
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
		//
		// For the first descriptor, defer the write until we've finalized the
		// buffersUsed count so the guest sees a consistent header.
		if bytesWritten > 0 && i != 0 {
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

	if firstDescData == nil {
		return 0, false, fmt.Errorf("net rx missing first descriptor data")
	}
	// Always populate numBuffers for the 12-byte header; if the guest doesn't
	// use it, it will be ignored, but when it does it must be accurate.
	if len(firstDescData) >= 12 {
		binary.LittleEndian.PutUint16(firstDescData[10:12], buffersUsed)
	}
	if firstBytesWritten > 0 {
		if err := dev.writeGuest(firstDescAddr, firstDescData[:firstBytesWritten]); err != nil {
			return 0, false, fmt.Errorf("write guest memory for rx descriptor %d: %w", 0, err)
		}
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
