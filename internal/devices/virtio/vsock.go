package virtio

import (
	"fmt"
	"io"
	"sync"

	"github.com/tinyrange/cc/internal/debug"
	"github.com/tinyrange/cc/internal/hv"
)

const (
	vsockDefaultMMIOBase   = 0xd0006000
	vsockDefaultMMIOSize   = 0x200
	vsockDefaultIRQLine    = 11
	vsockArmDefaultIRQLine = 43

	vsockQueueCount  = 3
	vsockQueueNumMax = 128
	vsockVendorID    = 0x554d4551 // "QEMU"
	vsockVersion     = 2
	vsockDeviceID    = 19 // VIRTIO_ID_VSOCK

	vsockQueueRX    = 0
	vsockQueueTX    = 1
	vsockQueueEvent = 2

	vsockInterruptBit = 0x1

	// Default buffer size for flow control
	vsockDefaultBufAlloc = 64 * 1024
)

var vsockDeviceConfig = &MMIODeviceConfig{
	DefaultMMIOBase:   vsockDefaultMMIOBase,
	DefaultMMIOSize:   vsockDefaultMMIOSize,
	DefaultIRQLine:    vsockDefaultIRQLine,
	ArmDefaultIRQLine: vsockArmDefaultIRQLine,
	DeviceID:          vsockDeviceID,
	VendorID:          vsockVendorID,
	Version:           vsockVersion,
	QueueCount:        vsockQueueCount,
	QueueMaxSize:      vsockQueueNumMax,
	FeatureBits:       []uint64{virtioFeatureVersion1},
	DeviceName:        "virtio-vsock",
}

// VsockDeviceConfig returns the shared configuration for vsock devices.
func VsockDeviceConfig() *MMIODeviceConfig {
	return vsockDeviceConfig
}

// VsockBackend is the host-side backend for vsock connections.
type VsockBackend interface {
	// Listen starts listening on a port for guest connections.
	Listen(port uint32) (VsockListener, error)
	// Connect connects to a guest port (not commonly used).
	Connect(port uint32) (VsockConn, error)
}

// VsockListener accepts connections from the guest.
type VsockListener interface {
	Accept() (VsockConn, error)
	Close() error
	Port() uint32
}

// VsockConn represents a single vsock connection.
type VsockConn interface {
	io.ReadWriter
	io.Closer
	LocalPort() uint32
	RemotePort() uint32
}

// vsockConnKey uniquely identifies a connection.
type vsockConnKey struct {
	localPort  uint32
	remotePort uint32
}

// vsockConnection represents a connection state.
type vsockConnection struct {
	key       vsockConnKey
	state     int
	peerAlloc uint32 // buf_alloc from peer
	peerCnt   uint32 // fwd_cnt from peer
	txCnt     uint32 // bytes we've sent
	rxCnt     uint32 // bytes we've received
	rxBuf     []byte // buffered data from guest
	backend   VsockConn
}

const (
	vsockConnStateIdle = iota
	vsockConnStateConnecting
	vsockConnStateConnected
	vsockConnStateClosing
	vsockConnStateClosed
)

// VsockTemplate is the device template for creating vsock devices.
type VsockTemplate struct {
	MMIODeviceTemplateBase
	GuestCID uint64
	Backend  VsockBackend
}

// NewVsockTemplate creates a VsockTemplate with proper configuration.
func NewVsockTemplate(guestCID uint64, backend VsockBackend) VsockTemplate {
	return VsockTemplate{
		MMIODeviceTemplateBase: MMIODeviceTemplateBase{Config: vsockDeviceConfig},
		GuestCID:               guestCID,
		Backend:                backend,
	}
}

func (t VsockTemplate) Create(vm hv.VirtualMachine) (hv.Device, error) {
	config := t.Config
	if config == nil {
		config = vsockDeviceConfig
	}

	arch := t.ArchOrDefault(vm)
	irqLine := t.IRQLineForArch(arch)
	encodedLine := EncodeIRQLineForArch(arch, irqLine)

	mmioBase := config.DefaultMMIOBase
	if vm != nil {
		alloc, err := vm.AllocateMMIO(hv.MMIOAllocationRequest{
			Name:      config.DeviceName,
			Size:      config.DefaultMMIOSize,
			Alignment: 0x1000,
		})
		if err != nil {
			return nil, fmt.Errorf("virtio-vsock: allocate MMIO: %w", err)
		}
		mmioBase = alloc.Base
	}

	vsock := &Vsock{
		MMIODeviceBase: NewMMIODeviceBase(
			mmioBase,
			config.DefaultMMIOSize,
			encodedLine,
			config,
		),
		guestCID:    t.GuestCID,
		backend:     t.Backend,
		connections: make(map[vsockConnKey]*vsockConnection),
	}
	if err := vsock.Init(vm); err != nil {
		return nil, fmt.Errorf("virtio-vsock: initialize device: %w", err)
	}
	return vsock, nil
}

var (
	_ hv.DeviceTemplate = VsockTemplate{}
	_ VirtioMMIODevice  = VsockTemplate{}
)

// Vsock is the virtio-vsock device.
type Vsock struct {
	MMIODeviceBase
	guestCID    uint64
	backend     VsockBackend
	mu          sync.Mutex
	connections map[vsockConnKey]*vsockConnection
	pendingRx   [][]byte // packets to deliver to guest
}

// Init implements hv.MemoryMappedIODevice.
func (v *Vsock) Init(vm hv.VirtualMachine) error {
	if v.Device() == nil {
		if err := v.InitBase(vm, v); err != nil {
			return err
		}
		return nil
	}
	if mmio, ok := v.Device().(*mmioDevice); ok && vm != nil {
		mmio.vm = vm
	}
	return nil
}

// OnReset implements deviceHandler.
func (v *Vsock) OnReset(dev device) {
	v.mu.Lock()
	defer v.mu.Unlock()
	// Close all connections
	for key, conn := range v.connections {
		if conn.backend != nil {
			conn.backend.Close()
		}
		delete(v.connections, key)
	}
	v.pendingRx = nil
}

// OnQueueNotify implements deviceHandler.
func (v *Vsock) OnQueueNotify(ctx hv.ExitContext, dev device, queue int) error {
	debug.Writef("virtio-vsock.OnQueueNotify", "queue=%d", queue)
	switch queue {
	case vsockQueueTX:
		return v.processTxQueue(dev, dev.queue(queue))
	case vsockQueueRX:
		return v.processRxQueue(dev, dev.queue(queue))
	case vsockQueueEvent:
		// Event queue is rarely used; just acknowledge
		return nil
	}
	return nil
}

// ReadConfig implements deviceHandler.
func (v *Vsock) ReadConfig(ctx hv.ExitContext, dev device, offset uint64) (uint32, bool, error) {
	// Config space is 8 bytes: guest_cid (u64)
	relOffset := offset - VIRTIO_MMIO_CONFIG
	if relOffset >= 8 {
		return 0, false, nil
	}
	// Return guest_cid as little-endian bytes
	switch relOffset {
	case 0:
		return uint32(v.guestCID), true, nil
	case 4:
		return uint32(v.guestCID >> 32), true, nil
	default:
		return 0, false, nil
	}
}

// WriteConfig implements deviceHandler.
func (v *Vsock) WriteConfig(ctx hv.ExitContext, dev device, offset uint64, value uint32) (bool, error) {
	// Config is read-only
	return false, nil
}

// processTxQueue handles packets from the guest.
func (v *Vsock) processTxQueue(dev device, q *queue) error {
	processed, err := ProcessQueueNotifications(dev, q, v.handleTxPacket)
	if err != nil {
		return err
	}
	if ShouldRaiseInterrupt(dev, q, processed) {
		dev.raiseInterrupt(vsockInterruptBit)
	}
	return nil
}

// handleTxPacket processes a single TX packet from the guest.
func (v *Vsock) handleTxPacket(dev device, q *queue, head uint16) (uint32, error) {
	data, err := ReadDescriptorChain(dev, q, head)
	if err != nil {
		return 0, err
	}
	if len(data) < vsockHdrSize {
		debug.Writef("virtio-vsock.handleTxPacket", "packet too short: %d", len(data))
		return uint32(len(data)), nil
	}

	hdr, err := parseVsockHeader(data)
	if err != nil {
		return uint32(len(data)), err
	}

	payload := data[vsockHdrSize:]
	if uint32(len(payload)) < hdr.Len {
		debug.Writef("virtio-vsock.handleTxPacket", "truncated payload: have %d, want %d", len(payload), hdr.Len)
	}

	debug.Writef("virtio-vsock.handleTxPacket",
		"src=%d:%d dst=%d:%d op=%s len=%d",
		hdr.SrcCID, hdr.SrcPort, hdr.DstCID, hdr.DstPort, opString(hdr.Op), hdr.Len)

	v.mu.Lock()
	defer v.mu.Unlock()

	switch hdr.Op {
	case VIRTIO_VSOCK_OP_REQUEST:
		v.handleConnect(dev, hdr)
	case VIRTIO_VSOCK_OP_RESPONSE:
		v.handleResponse(dev, hdr)
	case VIRTIO_VSOCK_OP_RST:
		v.handleReset(dev, hdr)
	case VIRTIO_VSOCK_OP_SHUTDOWN:
		v.handleShutdown(dev, hdr)
	case VIRTIO_VSOCK_OP_RW:
		v.handleData(dev, hdr, payload[:hdr.Len])
	case VIRTIO_VSOCK_OP_CREDIT_UPDATE:
		v.handleCreditUpdate(dev, hdr)
	case VIRTIO_VSOCK_OP_CREDIT_REQUEST:
		v.handleCreditRequest(dev, hdr)
	}

	return uint32(len(data)), nil
}

// handleConnect handles a connection request from the guest.
func (v *Vsock) handleConnect(dev device, hdr vsockHeader) {
	key := vsockConnKey{
		localPort:  hdr.DstPort,
		remotePort: hdr.SrcPort,
	}

	// Check if the backend is listening on this port
	if v.backend == nil {
		v.sendReset(dev, hdr)
		return
	}

	// Try to connect via backend
	conn, err := v.backend.Connect(hdr.DstPort)
	if err != nil {
		debug.Writef("virtio-vsock.handleConnect", "backend connect failed: %v", err)
		v.sendReset(dev, hdr)
		return
	}

	// Create connection state
	vsConn := &vsockConnection{
		key:       key,
		state:     vsockConnStateConnected,
		peerAlloc: hdr.BufAlloc,
		peerCnt:   hdr.FwdCnt,
		backend:   conn,
	}
	v.connections[key] = vsConn

	// Send response
	v.sendResponse(dev, hdr)

	// Start reading from backend
	go v.readFromBackend(dev, vsConn)
}

// handleResponse handles a connection response (for host-initiated connections).
func (v *Vsock) handleResponse(dev device, hdr vsockHeader) {
	key := vsockConnKey{
		localPort:  hdr.DstPort,
		remotePort: hdr.SrcPort,
	}
	conn, ok := v.connections[key]
	if !ok || conn.state != vsockConnStateConnecting {
		v.sendReset(dev, hdr)
		return
	}
	conn.state = vsockConnStateConnected
	conn.peerAlloc = hdr.BufAlloc
	conn.peerCnt = hdr.FwdCnt
}

// handleReset handles a reset from the guest.
func (v *Vsock) handleReset(dev device, hdr vsockHeader) {
	key := vsockConnKey{
		localPort:  hdr.DstPort,
		remotePort: hdr.SrcPort,
	}
	if conn, ok := v.connections[key]; ok {
		if conn.backend != nil {
			conn.backend.Close()
		}
		delete(v.connections, key)
	}
}

// handleShutdown handles a shutdown from the guest.
func (v *Vsock) handleShutdown(dev device, hdr vsockHeader) {
	key := vsockConnKey{
		localPort:  hdr.DstPort,
		remotePort: hdr.SrcPort,
	}
	conn, ok := v.connections[key]
	if !ok {
		return
	}
	conn.state = vsockConnStateClosing
	if conn.backend != nil {
		conn.backend.Close()
	}
	// Send RST back
	v.sendReset(dev, hdr)
	delete(v.connections, key)
}

// handleData handles data from the guest.
func (v *Vsock) handleData(dev device, hdr vsockHeader, payload []byte) {
	key := vsockConnKey{
		localPort:  hdr.DstPort,
		remotePort: hdr.SrcPort,
	}
	conn, ok := v.connections[key]
	if !ok || conn.state != vsockConnStateConnected {
		v.sendReset(dev, hdr)
		return
	}

	// Update peer credit info
	conn.peerAlloc = hdr.BufAlloc
	conn.peerCnt = hdr.FwdCnt

	// Write data to backend
	if conn.backend != nil && len(payload) > 0 {
		conn.rxCnt += uint32(len(payload))
		_, err := conn.backend.Write(payload)
		if err != nil {
			debug.Writef("virtio-vsock.handleData", "backend write failed: %v", err)
			v.sendReset(dev, hdr)
			if conn.backend != nil {
				conn.backend.Close()
			}
			delete(v.connections, key)
			return
		}
	}

	// Send credit update
	v.sendCreditUpdate(dev, conn)
}

// handleCreditUpdate updates the credit info from the guest.
func (v *Vsock) handleCreditUpdate(dev device, hdr vsockHeader) {
	key := vsockConnKey{
		localPort:  hdr.DstPort,
		remotePort: hdr.SrcPort,
	}
	conn, ok := v.connections[key]
	if !ok {
		return
	}
	conn.peerAlloc = hdr.BufAlloc
	conn.peerCnt = hdr.FwdCnt
}

// handleCreditRequest handles a credit request from the guest.
func (v *Vsock) handleCreditRequest(dev device, hdr vsockHeader) {
	key := vsockConnKey{
		localPort:  hdr.DstPort,
		remotePort: hdr.SrcPort,
	}
	conn, ok := v.connections[key]
	if !ok {
		return
	}
	v.sendCreditUpdate(dev, conn)
}

// sendResponse sends a connection response to the guest.
func (v *Vsock) sendResponse(dev device, hdr vsockHeader) {
	resp := vsockHeader{
		SrcCID:   VSOCK_CID_HOST,
		DstCID:   v.guestCID,
		SrcPort:  hdr.DstPort,
		DstPort:  hdr.SrcPort,
		Type:     VIRTIO_VSOCK_TYPE_STREAM,
		Op:       VIRTIO_VSOCK_OP_RESPONSE,
		BufAlloc: vsockDefaultBufAlloc,
		FwdCnt:   0,
	}
	v.queueRxPacket(encodeVsockHeader(resp))
	v.tryDeliverRx(dev)
}

// sendReset sends a reset to the guest.
func (v *Vsock) sendReset(dev device, hdr vsockHeader) {
	rst := vsockHeader{
		SrcCID:  VSOCK_CID_HOST,
		DstCID:  v.guestCID,
		SrcPort: hdr.DstPort,
		DstPort: hdr.SrcPort,
		Type:    VIRTIO_VSOCK_TYPE_STREAM,
		Op:      VIRTIO_VSOCK_OP_RST,
	}
	v.queueRxPacket(encodeVsockHeader(rst))
	v.tryDeliverRx(dev)
}

// sendCreditUpdate sends a credit update to the guest.
func (v *Vsock) sendCreditUpdate(dev device, conn *vsockConnection) {
	update := vsockHeader{
		SrcCID:   VSOCK_CID_HOST,
		DstCID:   v.guestCID,
		SrcPort:  conn.key.localPort,
		DstPort:  conn.key.remotePort,
		Type:     VIRTIO_VSOCK_TYPE_STREAM,
		Op:       VIRTIO_VSOCK_OP_CREDIT_UPDATE,
		BufAlloc: vsockDefaultBufAlloc,
		FwdCnt:   conn.rxCnt,
	}
	v.queueRxPacket(encodeVsockHeader(update))
	v.tryDeliverRx(dev)
}

// sendData sends data to the guest.
func (v *Vsock) sendData(dev device, conn *vsockConnection, data []byte) {
	pkt := vsockHeader{
		SrcCID:   VSOCK_CID_HOST,
		DstCID:   v.guestCID,
		SrcPort:  conn.key.localPort,
		DstPort:  conn.key.remotePort,
		Len:      uint32(len(data)),
		Type:     VIRTIO_VSOCK_TYPE_STREAM,
		Op:       VIRTIO_VSOCK_OP_RW,
		BufAlloc: vsockDefaultBufAlloc,
		FwdCnt:   conn.rxCnt,
	}
	hdrBytes := encodeVsockHeader(pkt)
	packet := append(hdrBytes, data...)
	conn.txCnt += uint32(len(data))
	v.queueRxPacket(packet)
	v.tryDeliverRx(dev)
}

// readFromBackend reads data from the backend and sends to guest.
func (v *Vsock) readFromBackend(dev device, conn *vsockConnection) {
	buf := make([]byte, 4096)
	for {
		n, err := conn.backend.Read(buf)
		if err != nil {
			v.mu.Lock()
			if conn.state == vsockConnStateConnected {
				// Send shutdown
				shutdown := vsockHeader{
					SrcCID:  VSOCK_CID_HOST,
					DstCID:  v.guestCID,
					SrcPort: conn.key.localPort,
					DstPort: conn.key.remotePort,
					Type:    VIRTIO_VSOCK_TYPE_STREAM,
					Op:      VIRTIO_VSOCK_OP_SHUTDOWN,
					Flags:   VIRTIO_VSOCK_SHUTDOWN_RCV | VIRTIO_VSOCK_SHUTDOWN_SEND,
				}
				v.queueRxPacket(encodeVsockHeader(shutdown))
				v.tryDeliverRx(dev)
				conn.state = vsockConnStateClosing
			}
			v.mu.Unlock()
			return
		}
		if n > 0 {
			v.mu.Lock()
			if conn.state == vsockConnStateConnected {
				v.sendData(dev, conn, buf[:n])
			}
			v.mu.Unlock()
		}
	}
}

// queueRxPacket queues a packet for delivery to the guest.
func (v *Vsock) queueRxPacket(packet []byte) {
	v.pendingRx = append(v.pendingRx, packet)
}

// processRxQueue delivers queued packets to the guest.
func (v *Vsock) processRxQueue(dev device, q *queue) error {
	if !QueueReady(q) {
		return nil
	}

	v.mu.Lock()
	defer v.mu.Unlock()

	if len(v.pendingRx) == 0 {
		return nil
	}

	_, availIdx, err := dev.readAvailState(q)
	if err != nil {
		return err
	}

	var anyProcessed bool
	for q.lastAvailIdx != availIdx && len(v.pendingRx) > 0 {
		ringIndex := q.lastAvailIdx % q.size
		head, err := dev.readAvailEntry(q, ringIndex)
		if err != nil {
			return err
		}

		packet := v.pendingRx[0]
		written, _, err := FillDescriptorChain(dev, q, head, packet)
		if err != nil {
			return err
		}

		if err := dev.recordUsedElement(q, head, written); err != nil {
			return err
		}

		v.pendingRx = v.pendingRx[1:]
		q.lastAvailIdx++
		anyProcessed = true
	}

	if anyProcessed {
		dev.raiseInterrupt(vsockInterruptBit)
	}

	return nil
}

// tryDeliverRx attempts to deliver pending RX packets.
func (v *Vsock) tryDeliverRx(dev device) {
	q := dev.queue(vsockQueueRX)
	if q != nil {
		// Note: we already hold v.mu, so we call the inner logic directly
		if !QueueReady(q) {
			return
		}

		_, availIdx, err := dev.readAvailState(q)
		if err != nil {
			return
		}

		var anyProcessed bool
		for q.lastAvailIdx != availIdx && len(v.pendingRx) > 0 {
			ringIndex := q.lastAvailIdx % q.size
			head, err := dev.readAvailEntry(q, ringIndex)
			if err != nil {
				return
			}

			packet := v.pendingRx[0]
			written, _, err := FillDescriptorChain(dev, q, head, packet)
			if err != nil {
				return
			}

			if err := dev.recordUsedElement(q, head, written); err != nil {
				return
			}

			v.pendingRx = v.pendingRx[1:]
			q.lastAvailIdx++
			anyProcessed = true
		}

		if anyProcessed {
			dev.raiseInterrupt(vsockInterruptBit)
		}
	}
}

var (
	_ hv.MemoryMappedIODevice = (*Vsock)(nil)
	_ deviceHandler           = (*Vsock)(nil)
)

// SimpleVsockBackend is a simple in-memory vsock backend for testing.
type SimpleVsockBackend struct {
	mu        sync.Mutex
	listeners map[uint32]*simpleVsockListener
}

// NewSimpleVsockBackend creates a new simple vsock backend.
func NewSimpleVsockBackend() *SimpleVsockBackend {
	return &SimpleVsockBackend{
		listeners: make(map[uint32]*simpleVsockListener),
	}
}

func (b *SimpleVsockBackend) Listen(port uint32) (VsockListener, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, exists := b.listeners[port]; exists {
		return nil, fmt.Errorf("port %d already in use", port)
	}

	l := &simpleVsockListener{
		port:   port,
		conns:  make(chan *simpleVsockConn, 16),
		closed: make(chan struct{}),
	}
	b.listeners[port] = l
	return l, nil
}

func (b *SimpleVsockBackend) Connect(port uint32) (VsockConn, error) {
	b.mu.Lock()
	l, ok := b.listeners[port]
	b.mu.Unlock()

	if !ok {
		return nil, fmt.Errorf("no listener on port %d", port)
	}

	// Create a connected pair
	clientSide := &simpleVsockConn{
		localPort:  0, // assigned by listener
		remotePort: port,
		readCh:     make(chan []byte, 64),
		closed:     make(chan struct{}),
	}
	serverSide := &simpleVsockConn{
		localPort:  port,
		remotePort: 0,
		readCh:     make(chan []byte, 64),
		closed:     make(chan struct{}),
	}
	clientSide.peer = serverSide
	serverSide.peer = clientSide

	select {
	case l.conns <- serverSide:
		return clientSide, nil
	case <-l.closed:
		return nil, fmt.Errorf("listener closed")
	}
}

var _ VsockBackend = (*SimpleVsockBackend)(nil)

type simpleVsockListener struct {
	port   uint32
	conns  chan *simpleVsockConn
	closed chan struct{}
}

func (l *simpleVsockListener) Accept() (VsockConn, error) {
	select {
	case conn := <-l.conns:
		return conn, nil
	case <-l.closed:
		return nil, fmt.Errorf("listener closed")
	}
}

func (l *simpleVsockListener) Close() error {
	select {
	case <-l.closed:
	default:
		close(l.closed)
	}
	return nil
}

func (l *simpleVsockListener) Port() uint32 {
	return l.port
}

var _ VsockListener = (*simpleVsockListener)(nil)

type simpleVsockConn struct {
	localPort  uint32
	remotePort uint32
	peer       *simpleVsockConn
	readCh     chan []byte
	closed     chan struct{}
	readBuf    []byte
}

func (c *simpleVsockConn) Read(b []byte) (int, error) {
	// First drain any buffered data
	if len(c.readBuf) > 0 {
		n := copy(b, c.readBuf)
		c.readBuf = c.readBuf[n:]
		return n, nil
	}

	select {
	case data := <-c.readCh:
		n := copy(b, data)
		if n < len(data) {
			c.readBuf = data[n:]
		}
		return n, nil
	case <-c.closed:
		return 0, io.EOF
	}
}

func (c *simpleVsockConn) Write(b []byte) (int, error) {
	if c.peer == nil {
		return 0, fmt.Errorf("no peer")
	}
	data := make([]byte, len(b))
	copy(data, b)
	select {
	case c.peer.readCh <- data:
		return len(b), nil
	case <-c.peer.closed:
		return 0, fmt.Errorf("peer closed")
	case <-c.closed:
		return 0, fmt.Errorf("connection closed")
	}
}

func (c *simpleVsockConn) Close() error {
	select {
	case <-c.closed:
	default:
		close(c.closed)
	}
	return nil
}

func (c *simpleVsockConn) LocalPort() uint32 {
	return c.localPort
}

func (c *simpleVsockConn) RemotePort() uint32 {
	return c.remotePort
}

var _ VsockConn = (*simpleVsockConn)(nil)
