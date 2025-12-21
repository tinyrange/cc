// Package raw implements a tiny, purpose-built, in-VM L2/L3 network stack.
//
// The goals are:
//   - Minimal correctness for ARP, IPv4, ICMP, UDP, and a very small TCP
//     subset sufficient for inbound connections to a handful of services.
//   - Zero external dependencies beyond the project itself and stdlib.
//   - Explicit memory management: packet/frame buffers are drawn from small
//     sync.Pools to reduce allocations.
//
// Notes and limitations:
//   - No IPv6 support.
//   - No IP fragmentation/reassembly.
//   - Very small portion of TCP is implemented (SYN/ACK/FIN, no retransmits,
//     no congestion control, no window scaling, no options beyond header size).
//   - MAC learning is simplistic: records latest observed source MAC.
//   - Certain counters and debug helpers are best effort only.
package netstack

import (
	"context"
	cryptoRand "crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tinyrange/cc/internal/pcap"
)

////////////////////////////////////////////////////////////////////////////////
// Top-level constants and protocol numbers.
////////////////////////////////////////////////////////////////////////////////

// Debug toggle. When true, emits verbose logs from key code paths.
const DEBUG = false

type etherType uint16

// EtherTypes we care about.
const (
	etherTypeIPv4   etherType = 0x0800
	etherTypeIPv6   etherType = 0x86DD
	etherTypeARP    etherType = 0x0806
	etherTypeCustom etherType = 0x1234
)

func (e etherType) String() string {
	switch e {
	case etherTypeIPv4:
		return "ipv4"
	case etherTypeIPv6:
		return "ipv6"
	case etherTypeARP:
		return "arp"
	case etherTypeCustom:
		return "custom"
	}
	return fmt.Sprintf("unknown ether type 0x%04x", uint16(e))
}

type protocolNumber uint8

// Basic protocol numbers for IPv4's Protocol field.
const (
	tcpProtocolNumber protocolNumber = 6
	udpProtocolNumber protocolNumber = 17
	icmpProtocol      protocolNumber = 1
)

func (p protocolNumber) String() string {
	switch p {
	case tcpProtocolNumber:
		return "tcp"
	case udpProtocolNumber:
		return "udp"
	case icmpProtocol:
		return "icmp"
	}
	return fmt.Sprintf("unknown protocol 0x%02x", uint8(p))
}

// ARP constants (Ethernet + IPv4).
const (
	arpHardwareEthernet = 1
	arpProtoIPv4        = 0x0800
)

// Header sizes (bytes).
const (
	tcpHeaderLen      = 20
	udpHeaderLen      = 8
	ipv4HeaderLen     = 20
	ethernetHeaderLen = 14
)

const macMask macAddr = (1 << 48) - 1
const macUnset macAddr = ^macAddr(0)

////////////////////////////////////////////////////////////////////////////////
// Defaults for the synthetic network.
////////////////////////////////////////////////////////////////////////////////

// Default network parameters matching the legacy configuration.
var (
	defaultHostIPv4    = [4]byte{10, 42, 0, 1}   // Address of the "host" (this stack)
	defaultGuestIPv4   = [4]byte{10, 42, 0, 2}   // Address expected for the guest
	defaultServiceIPv4 = [4]byte{10, 42, 0, 100} // Virtual "service" endpoint
)

////////////////////////////////////////////////////////////////////////////////
// Buffer pools for TCP, IPv4, and Ethernet frames.
// This is an allocation optimization to reduce GC churn when IO is heavy.
////////////////////////////////////////////////////////////////////////////////

const (
	defaultPacketCapacity   = 64*1024 + tcpHeaderLen
	maxTCPPacketPoolSize    = 256*1024 + tcpHeaderLen
	maxIPv4PacketPoolSize   = 256*1024 + ipv4HeaderLen
	maxEthernetFramePoolLen = 256*1024 + ethernetHeaderLen
)

var (
	tcpPacketPool = sync.Pool{
		New: func() any {
			return make([]byte, 0, defaultPacketCapacity)
		},
	}
	ipv4PacketPool = sync.Pool{
		New: func() any {
			return make([]byte, 0, defaultPacketCapacity)
		},
	}
	ethernetFramePool = sync.Pool{
		New: func() any {
			return make([]byte, 0, defaultPacketCapacity+ethernetHeaderLen)
		},
	}
)

func getTCPPacketBuffer(payloadLen int) []byte {
	total := tcpHeaderLen + payloadLen
	if total <= 0 {
		total = tcpHeaderLen
	}
	if total > maxTCPPacketPoolSize {
		return make([]byte, total)
	}
	raw := tcpPacketPool.Get().([]byte)
	if cap(raw) < total {
		tcpPacketPool.Put(raw[:0])
		return make([]byte, total)
	}
	return raw[:total]
}

func putTCPPacketBuffer(buf []byte) {
	if buf == nil {
		return
	}
	if cap(buf) > maxTCPPacketPoolSize {
		return
	}
	tcpPacketPool.Put(buf[:0])
}

func getIPv4PacketBuffer(payloadLen int) []byte {
	total := ipv4HeaderLen + payloadLen
	if total <= 0 {
		total = ipv4HeaderLen
	}
	if total > maxIPv4PacketPoolSize {
		return make([]byte, total)
	}
	raw := ipv4PacketPool.Get().([]byte)
	if cap(raw) < total {
		ipv4PacketPool.Put(raw[:0])
		return make([]byte, total)
	}
	return raw[:total]
}

func putIPv4PacketBuffer(buf []byte) {
	if buf == nil {
		return
	}
	if cap(buf) > maxIPv4PacketPoolSize {
		return
	}
	ipv4PacketPool.Put(buf[:0])
}

func getEthernetFrameBuffer(payloadLen int) []byte {
	total := ethernetHeaderLen + payloadLen
	if total <= ethernetHeaderLen {
		total = ethernetHeaderLen
	}
	if total > maxEthernetFramePoolLen {
		return make([]byte, total)
	}
	raw := ethernetFramePool.Get().([]byte)
	if cap(raw) < total {
		ethernetFramePool.Put(raw[:0])
		return make([]byte, total)
	}
	return raw[:total]
}

func putEthernetFrameBuffer(buf []byte) {
	if buf == nil {
		return
	}
	if cap(buf) > maxEthernetFramePoolLen {
		return
	}
	ethernetFramePool.Put(buf[:0])
}

////////////////////////////////////////////////////////////////////////////////
// NetStack: central struct tying together interface, routing and transport.
////////////////////////////////////////////////////////////////////////////////

type udpEndpoint interface {
	Close() error

	enqueue(data []byte, addr net.UDPAddr) error
}

// NetStack implements the ns.NetStack interface for our raw stack.
type NetStack struct {
	log *slog.Logger

	// MAC and addressing state.
	hostMAC          atomic.Uint64 // MAC of this stack ("host")
	guestMAC         atomic.Uint64 // Configured guest MAC (optional)
	observedGuestMAC atomic.Uint64 // Last source MAC seen from the guest

	hostIPv4    [4]byte // Primary address of the stack
	guestIPv4   [4]byte // Expected guest IPv4 address
	serviceIPv4 [4]byte // Special service-ip used for proxying

	serviceProxyEnabled bool // Forward connections destined to serviceIPv4
	allowInternet       bool // Allow DNS fallback et al

	// Wire interface and optional packet capture.
	mu         sync.RWMutex
	iface      *NetworkInterface
	packetDump *pcap.Writer

	// TCP state.
	tcpMu      sync.Mutex
	tcpListen  map[uint16]*tcpListener
	tcpConns   map[tcpFourTuple]*tcpConn
	randSource *rand.Rand

	// UDP state.
	udpSockets sync.Map

	// Embedded DNS server (optional).
	dnsServer *dnsServer

	// Debug HTTP server.
	debugMu       sync.Mutex
	debugSrv      *http.Server
	debugListener net.Listener
	debugWG       sync.WaitGroup
	debugAddr     string

	// Simple counters.
	udpRxPackets atomic.Uint64
	udpTxPackets atomic.Uint64

	closeOnce sync.Once
}

// New constructs a NetStack with defaults.
func New(l *slog.Logger) *NetStack {
	now := time.Now().UnixNano()
	stack := &NetStack{
		log:                 l,
		hostIPv4:            defaultHostIPv4,
		guestIPv4:           defaultGuestIPv4,
		serviceIPv4:         defaultServiceIPv4,
		serviceProxyEnabled: true,
		allowInternet:       true,
		tcpListen:           make(map[uint16]*tcpListener),
		tcpConns:            make(map[tcpFourTuple]*tcpConn),
		randSource:          rand.New(rand.NewSource(now)),
	}
	stack.hostMAC.Store(uint64(macUnset))
	stack.guestMAC.Store(uint64(macUnset))
	stack.observedGuestMAC.Store(uint64(macUnset))
	return stack
}

////////////////////////////////////////////////////////////////////////////////
// Lifecycle and configuration.
////////////////////////////////////////////////////////////////////////////////

// Close tears down listeners, connections, endpoints and the debug server.
// It is best-effort and idempotent.
func (ns *NetStack) Close() error {
	ns.closeOnce.Do(func() {
		ns.StopDNSServer()

		// Debug HTTP shutdown.
		ns.debugMu.Lock()
		srv := ns.debugSrv
		ln := ns.debugListener
		ns.debugSrv = nil
		ns.debugListener = nil
		ns.debugAddr = ""
		ns.debugMu.Unlock()

		if ln != nil {
			_ = ln.Close()
		}
		if srv != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			if err := srv.Shutdown(ctx); err != nil &&
				!errors.Is(err, http.ErrServerClosed) {
				slog.Error("raw: debug http shutdown", "err", err)
			}
			cancel()
		}

		// Wait on background goroutines bound to debugWG.
		ns.debugWG.Wait()

		// Close TCP listeners and connections.
		// BUG: Potential data race. The commented-out locks suggest this
		// code may run concurrently with other operations that also mutate
		// these maps. Use of locks around iteration would be safer.
		for _, l := range ns.tcpListen {
			l.Close()
		}
		ns.tcpListen = nil
		for _, c := range ns.tcpConns {
			c.Close()
		}
		ns.tcpConns = nil

		// Close UDP endpoints.
		// BUG: Similar concurrency concerns as above.
		ns.udpSockets.Range(func(key, value any) bool {
			_ = value.(io.Closer).Close()
			return true
		})
		ns.udpSockets = sync.Map{}

		// Stop packet capture.
		ns.packetDump = nil
	})
	return nil
}

// SetGuestMAC sets the expected guest MAC for filtering and transmission.
func (ns *NetStack) SetGuestMAC(mac net.HardwareAddr) error {
	if mac == nil {
		ns.guestMAC.Store(uint64(macUnset))
		return nil
	}
	if len(mac) != 6 {
		return fmt.Errorf("invalid MAC address length: %d", len(mac))
	}
	value, ok := macToUint64(mac)
	if !ok {
		return fmt.Errorf("invalid MAC address length: %d", len(mac))
	}
	ns.guestMAC.Store(uint64(value))
	return nil
}

// SetServiceProxyEnabled toggles the localhost proxy feature for TCP flows
// addressed to serviceIPv4.
func (ns *NetStack) SetServiceProxyEnabled(enabled bool) {
	ns.serviceProxyEnabled = enabled
}

// SetInternetAccessEnabled toggles access to real DNS lookups, etc.
func (ns *NetStack) SetInternetAccessEnabled(enabled bool) {
	ns.allowInternet = enabled
}

////////////////////////////////////////////////////////////////////////////////
// Packet capture (pcap).
////////////////////////////////////////////////////////////////////////////////

// OpenPacketCapture enables streaming packet capture to the given writer.
func (ns *NetStack) OpenPacketCapture(out io.Writer) error {
	ns.mu.Lock()
	defer ns.mu.Unlock()

	writer := pcap.NewWriter(out)
	if err := writer.WriteFileHeader(8192, pcap.LinkTypeEthernet); err != nil {
		return fmt.Errorf("write pcap header: %w", err)
	}
	ns.packetDump = writer
	return nil
}

func (ns *NetStack) writePacketCapture(data []byte) {
	ns.mu.RLock()
	writer := ns.packetDump
	ns.mu.RUnlock()

	if writer == nil {
		return
	}

	if err := writer.WritePacket(pcap.CaptureInfo{
		Timestamp:     time.Now(),
		CaptureLength: len(data),
		Length:        len(data),
	}, data); err != nil {
		ns.log.Warn("pcap: write frame failed", "err", err)
	}
}

////////////////////////////////////////////////////////////////////////////////
// Interface attachment and IO glue.
////////////////////////////////////////////////////////////////////////////////

// NetworkInterface is the concrete virtio-like interface that the guest uses
// to deliver and receive frames. It satisfies ns.NetworkInterface.
type NetworkInterface struct {
	stack   *NetStack
	backend func(frame []byte) error // Provided by VirtIO backend driver
}

// AttachNetworkInterface binds a new interface to the stack.
//
// The returned object is used by the hypervisor side to deliver packets.
func (ns *NetStack) AttachNetworkInterface() (*NetworkInterface, error) {
	ns.mu.Lock()
	defer ns.mu.Unlock()

	if ns.iface != nil {
		return nil, fmt.Errorf("network interface already attached")
	}

	if !macIsSet(macAddr(ns.guestMAC.Load())) {
		return nil, errors.New("guest mac must be configured before attaching interface")
	}

	hostMAC := make([]byte, 6)
	if _, err := cryptoRand.Read(hostMAC); err != nil {
		return nil, fmt.Errorf("generate host mac: %w", err)
	}
	hostMAC[0] |= 2 // Locally administered bit
	value, _ := macToUint64(hostMAC)
	ns.hostMAC.Store(uint64(value))

	iface := &NetworkInterface{stack: ns}
	ns.iface = iface
	return iface, nil
}

// AttachVirtioBackend sets the transmit callback to the hypervisor.
func (nic *NetworkInterface) AttachVirtioBackend(handler func(frame []byte) error) {
	nic.backend = handler
}

// DeliverGuestPacket is called by the hypervisor when the guest transmits.
func (nic *NetworkInterface) DeliverGuestPacket(
	packet []byte,
	release func(),
) error {
	needsCopy := release != nil
	if release != nil {
		defer release()
	}
	if len(packet) < ethernetHeaderLen {
		return fmt.Errorf("packet too short: %d", len(packet))
	}
	nic.stack.writePacketCapture(packet)
	return nic.stack.handleEthernetFrameWithReuse(packet, needsCopy)
}

// sendFrame transmits a frame back to the guest via the backend.
func (nic *NetworkInterface) sendFrame(frame []byte) error {
	if nic.backend == nil {
		return fmt.Errorf("virtio backend not attached")
	}
	nic.stack.writePacketCapture(frame)
	// Ownership/lifetime: `frame` is only valid for the duration of this call.
	// Backends must not retain the slice; if they need to keep it (e.g. queue
	// asynchronously), they must make their own copy.
	return nic.backend(frame)
}

// sendFrame transmits a prebuilt Ethernet frame to the attached NIC backend.
//
// IMPORTANT: Do not hold ns.mu while calling into the backend. Some backends
// (including the inline ACK backend used in benchmarks/tests) may synchronously
// re-enter the stack by delivering packets back via DeliverGuestPacket. That
// delivery path calls handleEthernetFrame -> recordGuestMAC which takes ns.mu
// for writing. If we kept a read lock here, the write would block forever,
// deadlocking the caller.
//
// This change fixes TCP benchmark hangs caused by a read->write upgrade
// deadlock when the backend immediately feeds frames back into the stack.
func (ns *NetStack) sendFrame(frame []byte) error {
	ns.mu.RLock()
	iface := ns.iface
	ns.mu.RUnlock()
	if iface == nil {
		return fmt.Errorf("network interface detached")
	}
	return iface.sendFrame(frame)
}

////////////////////////////////////////////////////////////////////////////////
// Ethernet handling and MAC learning.
////////////////////////////////////////////////////////////////////////////////

func (ns *NetStack) handleEthernetFrame(frame []byte) error {
	return ns.handleEthernetFrameWithReuse(frame, false)
}

func (ns *NetStack) handleEthernetFrameWithReuse(frame []byte, releaseUnsafe bool) error {
	dst := net.HardwareAddr(frame[:6])
	src := net.HardwareAddr(frame[6:12])
	etherType := etherType(binary.BigEndian.Uint16(frame[12:14]))
	payload := frame[14:]

	ns.recordGuestMAC(src)

	// Apply simple L2 filter: accept broadcast, host MAC, and configured guest
	// MAC when present. Drop other unicast frames when a guestMAC is set.
	guestMACVal := macAddr(ns.guestMAC.Load())
	hostMACVal := macAddr(ns.hostMAC.Load())
	if macIsSet(guestMACVal) &&
		!isBroadcast(dst) &&
		!macEqualUint64(dst, hostMACVal) &&
		!macEqualUint64(dst, guestMACVal) {
		// expected := ""
		// if mac := macFromUint64(guestMACVal); len(mac) == 6 {
		// 	expected = mac.String()
		// }
		// slog.Info(
		// 	"raw: drop frame not addressed to us",
		// 	"src", src.String(),
		// 	"dst", dst.String(),
		// 	"expectedDst", expected,
		// 	"ethertype", etherType.String(),
		// )
		return nil
	}

	switch etherType {
	case etherTypeARP:
		return ns.handleARP(src, payload)
	case etherTypeIPv4:
		return ns.handleIPv4Internal(src, payload, releaseUnsafe)
	case etherTypeCustom:
		return ns.handleCustom(src, payload)
	default:
		// ns.log.Info(
		// 	"raw: drop unsupported ethertype",
		// 	"type",
		// 	etherType.String(),
		// )
		return nil
	}
}

func isBroadcast(addr net.HardwareAddr) bool {
	for _, b := range addr {
		if b != 0xff {
			return false
		}
	}
	return true
}

type macAddr uint64

func macToUint64(mac net.HardwareAddr) (macAddr, bool) {
	if len(mac) != 6 {
		return 0, false
	}
	return macAddr((uint64(mac[0]) << 40) |
		(uint64(mac[1]) << 32) |
		(uint64(mac[2]) << 24) |
		(uint64(mac[3]) << 16) |
		(uint64(mac[4]) << 8) |
		uint64(mac[5])), true
}

func macFromUint64(v macAddr) net.HardwareAddr {
	if v == macUnset {
		return nil
	}
	v &= macMask
	var buf [6]byte
	buf[0] = byte(v >> 40)
	buf[1] = byte(v >> 32)
	buf[2] = byte(v >> 24)
	buf[3] = byte(v >> 16)
	buf[4] = byte(v >> 8)
	buf[5] = byte(v)
	return net.HardwareAddr(buf[:])
}

func macIsSet(v macAddr) bool {
	return v != macUnset
}

func macEqualUint64(addr net.HardwareAddr, mac macAddr) bool {
	if !macIsSet(mac) {
		return false
	}
	value, ok := macToUint64(addr)
	if !ok {
		return false
	}
	return value == (mac & macMask)
}

func writeMAC(dst []byte, mac macAddr) {
	if len(dst) < 6 || !macIsSet(mac) {
		return
	}
	dst[0] = byte(mac >> 40)
	dst[1] = byte(mac >> 32)
	dst[2] = byte(mac >> 24)
	dst[3] = byte(mac >> 16)
	dst[4] = byte(mac >> 8)
	dst[5] = byte(mac)
}

func (ns *NetStack) recordGuestMAC(mac net.HardwareAddr) {
	if len(mac) != 6 || isBroadcast(mac) {
		return
	}
	value, ok := macToUint64(mac)
	if !ok {
		return
	}
	host := macAddr(ns.hostMAC.Load())
	if macIsSet(host) && value == (host&macMask) {
		return
	}
	if macAddr(ns.observedGuestMAC.Load()) == value {
		return
	}
	ns.observedGuestMAC.Store(uint64(value))
}

// guestMACForTransmit returns a destination MAC usable for outbound frames.
// Prefers the most recent observed MAC, falling back to configured guestMAC.
// Don't modify the returned slice.
func (ns *NetStack) guestMACForTransmit() macAddr {
	if mac := macAddr(ns.observedGuestMAC.Load()); macIsSet(mac) {
		return mac
	}
	if mac := macAddr(ns.guestMAC.Load()); macIsSet(mac) {
		return mac
	}
	return macUnset
}

////////////////////////////////////////////////////////////////////////////////
// Custom: custom protocol handler
////////////////////////////////////////////////////////////////////////////////

func (ns *NetStack) handleCustom(srcMAC net.HardwareAddr, payload []byte) error {
	ns.log.Debug(
		"custom: handle custom packet",
		"src", srcMAC.String(),
		"payload", payload,
	)

	// Respond with the same packet but with the destination MAC changed to the source MAC
	frame := make([]byte, ethernetHeaderLen+len(payload))
	copy(frame[0:6], srcMAC)
	copy(frame[6:12], macFromUint64(macAddr(ns.hostMAC.Load())))
	binary.BigEndian.PutUint16(frame[12:14], uint16(etherTypeCustom))
	copy(frame[ethernetHeaderLen:], payload)
	return ns.sendFrame(frame)
}

////////////////////////////////////////////////////////////////////////////////
// ARP (Address Resolution Protocol).
////////////////////////////////////////////////////////////////////////////////

func (ns *NetStack) handleARP(srcMAC net.HardwareAddr, payload []byte) error {
	if len(payload) < 28 {
		return fmt.Errorf("arp packet too short: %d", len(payload))
	}

	hwType := binary.BigEndian.Uint16(payload[0:2])
	protoType := binary.BigEndian.Uint16(payload[2:4])
	hwSize := payload[4]
	protoSize := payload[5]
	op := binary.BigEndian.Uint16(payload[6:8])

	// We only speak Ethernet/IPv4.
	if hwType != arpHardwareEthernet ||
		protoType != arpProtoIPv4 ||
		hwSize != 6 || protoSize != 4 {
		return nil
	}

	senderMAC := net.HardwareAddr(payload[8:14])
	senderIP := net.IP(payload[14:18])
	targetIP := net.IP(payload[24:28])

	// Only handle ARP requests (op=1).
	if op != 1 {
		return nil
	}

	// Respond if the request targets host IPv4 or service IPv4.
	if ipEqual(targetIP, ns.hostIPv4[:]) || ipEqual(targetIP, ns.serviceIPv4[:]) {
		return ns.sendARPReply(srcMAC, senderMAC, senderIP, targetIP)
	}
	return nil
}

// sendARPReply crafts a unicast ARP reply to the requester.
//
// dstMAC: destination Ethernet MAC.
// senderMAC/senderIP: fields from the ARP request.
// targetIP: IP being queried (we're answering for it).
func (ns *NetStack) sendARPReply(
	dstMAC, senderMAC net.HardwareAddr,
	senderIP, targetIP net.IP,
) error {
	frame := make([]byte, ethernetHeaderLen+28)
	copy(frame[0:6], dstMAC)
	host := macAddr(ns.hostMAC.Load())
	writeMAC(frame[6:12], host)
	binary.BigEndian.PutUint16(frame[12:14], uint16(etherTypeARP))

	payload := frame[ethernetHeaderLen:]
	binary.BigEndian.PutUint16(payload[0:2], arpHardwareEthernet)
	binary.BigEndian.PutUint16(payload[2:4], arpProtoIPv4)
	payload[4] = 6
	payload[5] = 4
	binary.BigEndian.PutUint16(payload[6:8], 2) // reply
	writeMAC(payload[8:14], host)
	copy(payload[14:18], targetIP.To4())
	copy(payload[18:24], senderMAC)
	copy(payload[24:28], senderIP.To4())

	return ns.sendFrame(frame)
}

////////////////////////////////////////////////////////////////////////////////
// IPv4: header parsing/building and ICMP/UDP/TCP demux.
////////////////////////////////////////////////////////////////////////////////

// ipv4Header captures the fixed 20B header + optional options and payload.
//
// BUG: Fragmentation is not supported. The Flags/Fragment Offset field is
// simply captured as-is (in "flags") and ignored by the stack.
type ipv4Header struct {
	version  uint8
	ihl      uint8
	tos      uint8
	length   uint16
	id       uint16
	flags    uint16 // includes flags and fragment offset
	ttl      uint8
	protocol protocolNumber
	checksum uint16
	src      net.IP
	dst      net.IP
	options  []byte
	payload  []byte
}

// parseIPv4Header decodes minimal IPv4 header and returns the header struct.
//
// BUG: Incoming header checksum isn't verified.
// BUG: Options are not interpreted beyond slicing them out.
func parseIPv4Header(data []byte) (ipv4Header, error) {
	if len(data) < ipv4HeaderLen {
		return ipv4Header{}, fmt.Errorf("ipv4 header too short: %d", len(data))
	}
	verIHL := data[0]
	version := verIHL >> 4
	ihl := verIHL & 0x0f
	if version != 4 {
		return ipv4Header{}, fmt.Errorf("unsupported ipv4 version: %d", version)
	}
	headerLen := int(ihl) * 4
	if len(data) < headerLen {
		return ipv4Header{}, fmt.Errorf("ipv4 header length mismatch: %d", headerLen)
	}

	h := ipv4Header{
		version:  version,
		ihl:      ihl,
		tos:      data[1],
		length:   binary.BigEndian.Uint16(data[2:4]),
		id:       binary.BigEndian.Uint16(data[4:6]),
		flags:    binary.BigEndian.Uint16(data[6:8]),
		ttl:      data[8],
		protocol: protocolNumber(data[9]),
		checksum: binary.BigEndian.Uint16(data[10:12]),
		src:      net.IP(data[12:16]),
		dst:      net.IP(data[16:20]),
	}

	if headerLen > ipv4HeaderLen {
		h.options = data[ipv4HeaderLen:headerLen]
	}
	h.payload = data[headerLen:]
	return h, nil
}

// buildIPv4Packet allocates and writes a basic IPv4 packet with the given
// protocol and payload.
func buildIPv4Packet(
	src, dst net.IP,
	protocol protocolNumber,
	payload []byte,
) []byte {
	packet := make([]byte, ipv4HeaderLen+len(payload))
	return buildIPv4PacketInto(packet, src, dst, protocol, payload)
}

// buildIPv4PacketInto writes an IPv4 packet into buf (if large enough) and
// returns the slice to the written packet.
func buildIPv4PacketInto(
	buf []byte,
	src, dst net.IP,
	protocol protocolNumber,
	payload []byte,
) []byte {
	totalLen := ipv4HeaderLen + len(payload)
	if cap(buf) < totalLen {
		buf = make([]byte, totalLen)
	}
	packet := buf[:totalLen]

	buildIPv4HeaderInto(packet[:ipv4HeaderLen], src, dst, protocol, len(payload))

	copy(packet[ipv4HeaderLen:], payload)
	return packet
}

func buildEthernetHeaderInto(buf []byte, dstMac, srcMac macAddr, etherType etherType) {
	if len(buf) < ethernetHeaderLen {
		panic("buildEthernetHeaderInto: buffer too small")
	}
	writeMAC(buf[0:6], dstMac)
	writeMAC(buf[6:12], srcMac)
	binary.BigEndian.PutUint16(buf[12:14], uint16(etherType))
}

func buildIPv4HeaderInto(
	packet []byte,
	src, dst net.IP,
	protocol protocolNumber,
	payloadLen int,
) {
	if len(packet) < ipv4HeaderLen {
		panic("buildIPv4HeaderInto: buffer too small")
	}
	totalLen := ipv4HeaderLen + payloadLen

	packet[0] = byte((4 << 4) | (ipv4HeaderLen / 4)) // Version/IHL
	packet[1] = 0                                    // TOS
	binary.BigEndian.PutUint16(packet[2:4], uint16(totalLen))
	binary.BigEndian.PutUint16(packet[4:6], 0) // ID
	binary.BigEndian.PutUint16(packet[6:8], 0) // Flags/FragOff
	packet[8] = 64                             // TTL
	packet[9] = byte(protocol)
	copy(packet[12:16], src.To4())
	copy(packet[16:20], dst.To4())

	check := ipv4Checksum(packet[:ipv4HeaderLen])
	binary.BigEndian.PutUint16(packet[10:12], check)
}

func ipv4Checksum(data []byte) uint16 {
	var sum uint32
	for i := 0; i < len(data)-1; i += 2 {
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

func (ns *NetStack) handleIPv4Internal(srcMAC net.HardwareAddr, payload []byte, releaseUnsafe bool) error {
	hdr, err := parseIPv4Header(payload)
	if err != nil {
		return err
	}

	// Only accept unicast addressed to our host or service IPs.
	if !ipEqual(hdr.dst.To4(), ns.hostIPv4[:]) &&
		!ipEqual(hdr.dst.To4(), ns.serviceIPv4[:]) {
		slog.Error(
			"raw: drop ipv4 packet not addressed to us",
			"srcIP", hdr.src.String(),
			"dstIP", hdr.dst.String(),
		)
		return nil
	}

	switch hdr.protocol {
	case udpProtocolNumber:
		return ns.handleUDPWithReuse(hdr, hdr.payload, releaseUnsafe)
	case tcpProtocolNumber:
		return ns.handleTCP(hdr, hdr.payload)
	case icmpProtocol:
		// slog.Info("raw: handling icmp packet", "srcIP", hdr.src.String(), "dstIP", hdr.dst.String())
		return ns.handleICMP(hdr, hdr.payload)
	default:
		slog.Error("raw: drop unsupported ipv4 protocol", "proto", hdr.protocol)
		return nil
	}
}

////////////////////////////////////////////////////////////////////////////////
// ICMP (Echo request/reply) - minimal handling.
////////////////////////////////////////////////////////////////////////////////

func (ns *NetStack) handleICMP(h ipv4Header, payload []byte) error {
	if len(payload) < 8 {
		return nil
	}
	typ := payload[0]
	if typ != 8 { // Echo Request
		return nil
	}

	// Validate ICMP checksum
	receivedChecksum := binary.BigEndian.Uint16(payload[2:4])
	binary.BigEndian.PutUint16(payload[2:4], 0) // Zero checksum for validation
	calculatedChecksum := checksum(payload)
	binary.BigEndian.PutUint16(payload[2:4], receivedChecksum) // Restore original
	if receivedChecksum != calculatedChecksum {
		slog.Error("raw: drop icmp packet with invalid checksum",
			"received", fmt.Sprintf("0x%04x", receivedChecksum),
			"calculated", fmt.Sprintf("0x%04x", calculatedChecksum))
		return nil
	}

	// Build Echo Reply: type=0, copy fields after checksum.
	reply := make([]byte, len(payload))
	reply[0] = 0
	reply[1] = payload[1]
	copy(reply[4:], payload[4:])
	binary.BigEndian.PutUint16(reply[2:4], 0)
	check := checksum(reply)
	binary.BigEndian.PutUint16(reply[2:4], check)

	return ns.sendICMP(h.dst, h.src, reply)
}

func (ns *NetStack) sendICMP(src, dst net.IP, payload []byte) error {
	packet := buildIPv4Packet(src, dst, icmpProtocol, payload)
	dstMAC := ns.guestMACForTransmit()
	if !macIsSet(dstMAC) {
		return fmt.Errorf("guest mac unknown for icmp transmit")
	}
	frame := make([]byte, ethernetHeaderLen+len(packet))
	writeMAC(frame[0:6], dstMAC)
	writeMAC(frame[6:12], macAddr(ns.hostMAC.Load()))
	binary.BigEndian.PutUint16(frame[12:14], uint16(etherTypeIPv4))
	copy(frame[ethernetHeaderLen:], packet)
	return ns.sendFrame(frame)
}

////////////////////////////////////////////////////////////////////////////////
// UDP datapath (very small).
////////////////////////////////////////////////////////////////////////////////

type udpPacket struct {
	payload []byte
	addr    net.UDPAddr
}

// handleUDP parses the UDP header and enqueues payloads to bound endpoints.
func (ns *NetStack) handleUDP(h ipv4Header, payload []byte) error {
	return ns.handleUDPWithReuse(h, payload, false)
}

func (ns *NetStack) handleUDPWithReuse(h ipv4Header, payload []byte, releaseUnsafe bool) error {
	if len(payload) < 8 {
		return fmt.Errorf("udp packet too short: %d", len(payload))
	}

	srcPort := binary.BigEndian.Uint16(payload[0:2])
	dstPort := binary.BigEndian.Uint16(payload[2:4])

	length := binary.BigEndian.Uint16(payload[4:6])
	if length < udpHeaderLen {
		return fmt.Errorf("udp length shorter than header: %d", length)
	}

	if int(length) > len(payload) {
		return fmt.Errorf("udp length exceeds payload: %d > %d", length, len(payload))
	}

	data := payload[8:length]

	v, ok := ns.udpSockets.Load(dstPort)
	if !ok {
		slog.Error("raw: drop udp packet not addressed to us",
			"srcIP", h.src.String(),
			"srcPort", srcPort,
			"dstPort", dstPort,
		)
		return nil
	}

	ep := v.(udpEndpoint)

	addrIP := h.src.To4()
	if addrIP == nil {
		return fmt.Errorf("udp source ip is not ipv4: %v", h.src)
	}

	addr := net.UDPAddr{
		IP:   addrIP,
		Port: int(srcPort),
	}

	if releaseUnsafe {
		if _, ok := ep.(*udpCallbackEndpoint); ok {
			addr.IP = append([]byte(nil), addr.IP...)
		}
	}

	if err := ep.enqueue(data, addr); err != nil {
		return err
	}

	// BUG: This counter is incremented even when enqueue() drops due to
	// a full buffer (enqueue returns nil in that path). It should probably
	// reflect packets actually delivered to the application.
	ns.udpRxPackets.Add(1)
	return nil
}

// sendUDP crafts and transmits a UDP packet to the guest.
func (ns *NetStack) sendUDP(
	buf []byte,
	srcPort, dstPort uint16,
	srcIP, dstIP net.IP,
	payloadLen int,
) error {
	if len(buf) < ethernetHeaderLen+ipv4HeaderLen+udpHeaderLen+payloadLen {
		return fmt.Errorf("buffer too small for udp packet")
	}

	totalLen := 8 + payloadLen
	packet := buf[ethernetHeaderLen+ipv4HeaderLen:]
	binary.BigEndian.PutUint16(packet[0:2], srcPort)
	binary.BigEndian.PutUint16(packet[2:4], dstPort)
	binary.BigEndian.PutUint16(packet[4:6], uint16(totalLen))
	copy(packet[8:], buf[ethernetHeaderLen+ipv4HeaderLen+udpHeaderLen:])

	zeroOut := packet[6:8]
	check := udpChecksum(srcIP, dstIP, packet)
	binary.BigEndian.PutUint16(zeroOut, check)

	buildIPv4HeaderInto(buf[ethernetHeaderLen:ethernetHeaderLen+ipv4HeaderLen], srcIP, dstIP, udpProtocolNumber, len(packet))

	dstMAC := ns.guestMACForTransmit()
	if !macIsSet(dstMAC) {
		return fmt.Errorf("guest mac unknown for udp transmit")
	}

	srcMAC := macAddr(ns.hostMAC.Load())
	if !macIsSet(srcMAC) {
		return fmt.Errorf("host mac unknown for udp transmit")
	}

	buildEthernetHeaderInto(buf[:ethernetHeaderLen], dstMAC, srcMAC, etherTypeIPv4)

	if err := ns.sendFrame(buf); err != nil {
		return err
	}

	ns.udpTxPackets.Add(1)
	return nil
}

// UDP checksums (with IPv4 pseudo-header).
func udpChecksum(src, dst net.IP, payload []byte) uint16 {
	ps := pseudoHeaderChecksum(src, dst, udpProtocolNumber, len(payload))
	return checksumWithInitial(payload, ps)
}

// udpTimeoutError conveys a timeout via net.Error.
type udpTimeoutError struct{}

func (udpTimeoutError) Error() string   { return "timeout" }
func (udpTimeoutError) Timeout() bool   { return true }
func (udpTimeoutError) Temporary() bool { return true }

// udpEndpointConn represents a bound UDP "socket" on a given port.
type udpEndpointConn struct {
	stack    *NetStack
	port     uint16
	incoming chan udpPacket

	closed    atomic.Bool
	readDead  time.Time
	writeDead time.Time
}

func newUDPEndpointConn(stack *NetStack, port uint16) *udpEndpointConn {
	return &udpEndpointConn{
		stack:    stack,
		port:     port,
		incoming: make(chan udpPacket, 32),
	}
}

func (ep *udpEndpointConn) enqueue(data []byte, addr net.UDPAddr) error {
	if ep.closed.Load() {
		return net.ErrClosed
	}

	// use a blocking send
	ep.incoming <- udpPacket{
		payload: append([]byte(nil), data...),
		addr:    addr,
	}

	return nil
}

func (ep *udpEndpointConn) ReadFrom(b []byte) (int, net.Addr, error) {
	if ep.closed.Load() {
		return 0, nil, net.ErrClosed
	}
	deadline := ep.readDead
	ch := ep.incoming

	var (
		timer   *time.Timer
		timeout <-chan time.Time
	)
	if !deadline.IsZero() {
		until := time.Until(deadline)
		if until <= 0 {
			return 0, nil, &net.OpError{Op: "read", Net: "udp", Err: udpTimeoutError{}}
		}
		timer = time.NewTimer(until)
		timeout = timer.C
		defer func() {
			if timer != nil {
				timer.Stop()
			}
		}()
	}

	select {
	case pkt, ok := <-ch:
		if !ok {
			return 0, nil, net.ErrClosed
		}
		n := copy(b, pkt.payload)
		return n, &pkt.addr, nil
	case <-timeout:
		return 0, nil, &net.OpError{Op: "read", Net: "udp", Err: udpTimeoutError{}}
	}
}

func (ep *udpEndpointConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	udpAddr, ok := addr.(*net.UDPAddr)
	if !ok {
		return 0, &net.OpError{Op: "write", Net: "udp", Err: errors.New("unexpected addr type")}
	}
	if ep.closed.Load() {
		return 0, net.ErrClosed
	}
	dead := ep.writeDead

	if !dead.IsZero() && time.Now().After(dead) {
		return 0, &net.OpError{Op: "write", Net: "udp", Err: udpTimeoutError{}}
	}

	srcIP := net.IP(ep.stack.hostIPv4[:])
	dstIP := udpAddr.IP

	buf := make([]byte, ethernetHeaderLen+ipv4HeaderLen+udpHeaderLen+len(b))
	copy(buf[ethernetHeaderLen+ipv4HeaderLen+udpHeaderLen:], b)

	err := ep.stack.sendUDP(buf, ep.port, uint16(udpAddr.Port), srcIP, dstIP, len(b))
	if err != nil {
		return 0, err
	}
	return len(b), nil
}

func (ep *udpEndpointConn) Close() error {
	if ep.closed.Load() {
		return nil
	}
	ep.closed.Store(true)
	close(ep.incoming)

	ep.stack.udpSockets.Delete(ep.port)
	return nil
}

func (ep *udpEndpointConn) LocalAddr() net.Addr {
	return &net.UDPAddr{IP: net.IP(ep.stack.hostIPv4[:]), Port: int(ep.port)}
}

func (ep *udpEndpointConn) SetDeadline(t time.Time) error {
	ep.SetReadDeadline(t)
	ep.SetWriteDeadline(t)
	return nil
}

func (ep *udpEndpointConn) SetReadDeadline(t time.Time) error {
	ep.readDead = t
	return nil
}

func (ep *udpEndpointConn) SetWriteDeadline(t time.Time) error {
	ep.writeDead = t
	return nil
}

var (
	_ udpEndpoint = (*udpEndpointConn)(nil)
)

////////////////////////////////////////////////////////////////////////////////
// TCP: tiny acceptor and connection state machine.
////////////////////////////////////////////////////////////////////////////////

const (
	tcpFlagFIN = 0x01
	tcpFlagSYN = 0x02
	tcpFlagRST = 0x04
	tcpFlagPSH = 0x08
	tcpFlagACK = 0x10
)

type tcpHeader struct {
	srcPort  uint16
	dstPort  uint16
	seq      uint32
	ack      uint32
	dataOff  uint8
	flags    uint16
	window   uint16
	checksum uint16
	urgent   uint16
	options  []byte
	payload  []byte
}

func parseTCPHeader(data []byte) (tcpHeader, error) {
	if len(data) < tcpHeaderLen {
		return tcpHeader{}, fmt.Errorf("tcp header too short: %d", len(data))
	}

	hdrLen := (data[12] >> 4) * 4
	if len(data) < int(hdrLen) {
		return tcpHeader{}, fmt.Errorf("tcp header length mismatch: %d", hdrLen)
	}

	h := tcpHeader{
		srcPort:  binary.BigEndian.Uint16(data[0:2]),
		dstPort:  binary.BigEndian.Uint16(data[2:4]),
		seq:      binary.BigEndian.Uint32(data[4:8]),
		ack:      binary.BigEndian.Uint32(data[8:12]),
		dataOff:  data[12],
		flags:    uint16(data[13]),
		window:   binary.BigEndian.Uint16(data[14:16]),
		checksum: binary.BigEndian.Uint16(data[16:18]),
		urgent:   binary.BigEndian.Uint16(data[18:20]),
		payload:  data[hdrLen:],
	}

	if hdrLen > tcpHeaderLen {
		h.options = data[tcpHeaderLen:hdrLen]
	}
	return h, nil
}

// Four-tuple uniquely identifies a TCP connection.
type tcpFourTuple struct {
	srcIP   [4]byte
	dstIP   [4]byte
	srcPort uint16
	dstPort uint16
}

type tcpState int

const (
	tcpStateSynRcvd tcpState = iota
	tcpStateEstablished
	tcpStateFinWait
	tcpStateClosed
)

// tcpAddr implements net.Addr for our tiny TCP endpoints.
type tcpAddr struct {
	ip   net.IP
	port uint16
}

func (a *tcpAddr) Network() string { return "tcp" }
func (a *tcpAddr) String() string  { return net.JoinHostPort(a.ip.String(), itoa(int(a.port))) }

// tcpListener represents a bound port that receives inbound connections.
type tcpListener struct {
	stack *NetStack
	port  uint16

	incoming chan *tcpConn
	closeCh  chan struct{}

	mu     sync.Mutex
	closed bool
}

func newTCPListener(stack *NetStack, port uint16) *tcpListener {
	return &tcpListener{
		stack:    stack,
		port:     port,
		incoming: make(chan *tcpConn, 16),
		closeCh:  make(chan struct{}),
	}
}

func (l *tcpListener) Accept() (net.Conn, error) {
	select {
	case conn, ok := <-l.incoming:
		if !ok {
			return nil, net.ErrClosed
		}
		return conn, nil
	case <-l.closeCh:
		return nil, net.ErrClosed
	}
}

func (l *tcpListener) Close() error {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil
	}
	l.closed = true
	close(l.closeCh)
	l.mu.Unlock()

	l.stack.tcpMu.Lock()
	delete(l.stack.tcpListen, l.port)
	l.stack.tcpMu.Unlock()

	return nil
}

func (l *tcpListener) Addr() net.Addr {
	return &tcpAddr{ip: net.IP(l.stack.hostIPv4[:]), port: l.port}
}

// tcpConn is a minimal half-duplex-ish TCP connection to the guest.
type tcpConn struct {
	stack         *NetStack
	listener      *tcpListener
	key           tcpFourTuple
	onEstablished func(*tcpConn)
	localIPv4     [4]byte

	mu            sync.Mutex
	state         tcpState
	guestSeq      uint32
	hostSeq       uint32
	recvBuf       chan []byte
	readDeadline  time.Time
	writeDeadline time.Time
	closed        bool
}

func newTCPConn(
	stack *NetStack,
	listener *tcpListener,
	key tcpFourTuple,
	guestSeq uint32,
	localIPv4 [4]byte,
	onEstablished func(*tcpConn),
) *tcpConn {
	return &tcpConn{
		stack:         stack,
		listener:      listener,
		key:           key,
		localIPv4:     localIPv4,
		onEstablished: onEstablished,
		state:         tcpStateSynRcvd,
		guestSeq:      guestSeq + 1, // Expect data after SYN
		hostSeq:       uint32(stack.randSource.Int31()),
		recvBuf:       make(chan []byte, 512),
	}
}

// handleTCP demuxes by 4-tuple to an existing conn or establishes a new one.
func (ns *NetStack) handleTCP(h ipv4Header, payload []byte) error {
	hdr, err := parseTCPHeader(payload)
	if err != nil {
		return err
	}
	if DEBUG {
		ns.log.Info(
			"raw: tcp segment",
			"src",
			fmt.Sprintf("%v:%d", h.src, hdr.srcPort),
			"dst",
			fmt.Sprintf("%v:%d", h.dst, hdr.dstPort),
			"flags",
			fmt.Sprintf("0x%02x", hdr.flags),
			"seq",
			hdr.seq,
			"ack",
			hdr.ack,
			"len",
			len(hdr.payload),
		)
	}

	key := tcpFourTuple{
		srcPort: hdr.srcPort,
		dstPort: hdr.dstPort,
	}
	copy(key.srcIP[:], h.src.To4())
	copy(key.dstIP[:], h.dst.To4())

	ns.tcpMu.Lock()
	conn, ok := ns.tcpConns[key]
	if !ok {
		// Only a SYN may open a new connection.
		if hdr.flags&tcpFlagSYN == 0 {
			ns.tcpMu.Unlock()
			return nil
		}

		// Local listener present? Create a conn and complete handshake.
		if listener, ok := ns.tcpListen[hdr.dstPort]; ok {
			conn = newTCPConn(ns, listener, key, hdr.seq, ns.hostIPv4, nil)
			ns.tcpConns[key] = conn
			ns.tcpMu.Unlock()
			conn.sendSynAck()
			return nil
		}

		dstIP := h.dst.To4()
		slog.Info(
			"raw: tcp connection attempt",
			"src",
			fmt.Sprintf("%v:%d", h.src, hdr.srcPort),
			"dst",
			fmt.Sprintf("%v:%d", h.dst, hdr.dstPort),
		)

		// Proxy service connections to 127.0.0.1:dstPort if enabled.
		if ns.shouldProxyService(dstIP) {
			if !ns.serviceProxyEnabled {
				ns.tcpMu.Unlock()
				return ns.sendRST(h, hdr)
			}
			slog.Info(
				"raw: starting service proxy",
				"src",
				fmt.Sprintf("%v:%d", h.src, hdr.srcPort),
				"dst",
				fmt.Sprintf("%v:%d", h.dst, hdr.dstPort),
			)
			onEstablished := func(c *tcpConn) {
				ns.startServiceProxy(c)
			}
			conn = newTCPConn(ns, nil, key, hdr.seq, ns.serviceIPv4, onEstablished)
			ns.tcpConns[key] = conn
			ns.tcpMu.Unlock()
			conn.sendSynAck()
			return nil
		}

		// Deny internet if disabled (except to host IP).
		if !ns.allowInternet && !ipEqual(dstIP, ns.hostIPv4[:]) {
			ns.tcpMu.Unlock()
			return ns.sendRST(h, hdr)
		}

		// No listener and no proxy; reset.
		ns.tcpMu.Unlock()
		return ns.sendRST(h, hdr)
	}
	ns.tcpMu.Unlock()

	return conn.handleSegment(h, hdr)
}

func (c *tcpConn) handleSegment(h ipv4Header, hdr tcpHeader) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return net.ErrClosed
	}
	if DEBUG {
		c.stack.log.Info(
			"raw: conn segment",
			"state", c.state,
			"flags", fmt.Sprintf("0x%02x", hdr.flags),
			"seq", hdr.seq,
			"ack", hdr.ack,
			"payload", len(hdr.payload),
			"expectSeq", c.guestSeq,
		)
	}

	// Track ack of our sent data.
	if hdr.flags&tcpFlagACK != 0 {
		if hdr.ack < c.hostSeq {
			// Old ack; ignore.
		} else {
			c.hostSeq = hdr.ack
		}
	}

	switch c.state {
	case tcpStateSynRcvd:
		if hdr.flags&tcpFlagACK != 0 {
			c.state = tcpStateEstablished
			cb := c.onEstablished
			listener := c.listener
			hasData := len(hdr.payload) > 0 ||
				hdr.flags&(tcpFlagFIN|tcpFlagRST) != 0
			if DEBUG {
				c.stack.log.Info("raw: conn established", "hasData", hasData)
			}
			c.mu.Unlock()
			if listener != nil {
				// Listener can be closed concurrently. Avoid blocking forever
				// or panicking by bailing out when closeCh is closed.
				select {
				case <-listener.closeCh:
					c.Close()
				default:
					select {
					case listener.incoming <- c:
					case <-listener.closeCh:
						c.Close()
					}
				}
			} else if cb != nil {
				go cb(c)
			}
			if hasData {
				return c.handleSegment(h, hdr)
			}
			return nil
		}
	case tcpStateEstablished:
		if len(hdr.payload) > 0 {
			if hdr.seq != c.guestSeq {
				c.stack.log.Info(
					"raw: conn out-of-order",
					"seq", hdr.seq,
					"expect", c.guestSeq,
				)
				c.mu.Unlock()
				return nil
			}
			c.guestSeq += uint32(len(hdr.payload))
			data := append([]byte{}, hdr.payload...)
			if DEBUG {
				c.stack.log.Info(
					"raw: conn data",
					"len", len(data),
					"newGuestSeq", c.guestSeq,
				)
			}
			c.mu.Unlock()
			c.enqueueData(data)
			c.sendAck()
			return nil
		}
		if hdr.flags&tcpFlagFIN != 0 {
			c.guestSeq++
			c.state = tcpStateFinWait
			c.mu.Unlock()
			c.enqueueData(nil) // signal EOF to readers
			c.sendAck()
			c.sendFin()
			return nil
		}
		c.mu.Unlock()
		if hdr.flags&tcpFlagRST != 0 {
			c.Close()
		}
		return nil
	case tcpStateFinWait:
		if hdr.flags&tcpFlagACK != 0 {
			c.state = tcpStateClosed
			c.mu.Unlock()
			c.Close()
			return nil
		}
		c.mu.Unlock()
		return nil
	default:
		c.mu.Unlock()
		return nil
	}

	c.mu.Unlock()
	return nil
}

func (c *tcpConn) enqueueData(data []byte) {
	if DEBUG {
		c.stack.log.Info("raw: conn enqueue", "len", len(data))
	}
	// Synchronize with Close() (which closes recvBuf) to avoid sending on a
	// closed channel.
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	// Avoid blocking the network path if readers are slow.
	select {
	case c.recvBuf <- data:
	default:
	}
	c.mu.Unlock()
}

func (c *tcpConn) sendSynAck() {
	c.mu.Lock()
	seq := c.hostSeq
	ack := c.guestSeq
	c.hostSeq++
	c.mu.Unlock()

	c.stack.sendTCPPacket(c.localIPv4, c.key, seq, ack, tcpFlagSYN|tcpFlagACK, nil)
}

func (c *tcpConn) sendAck() {
	c.mu.Lock()
	seq := c.hostSeq
	ack := c.guestSeq
	c.mu.Unlock()
	c.stack.sendTCPPacket(c.localIPv4, c.key, seq, ack, tcpFlagACK, nil)
}

func (c *tcpConn) sendFin() {
	c.mu.Lock()
	seq := c.hostSeq
	ack := c.guestSeq
	c.hostSeq++
	c.mu.Unlock()
	c.stack.sendTCPPacket(c.localIPv4, c.key, seq, ack, tcpFlagFIN|tcpFlagACK, nil)
}

// Read returns payload delivered by the guest. A nil buffer pushed into the
// queue signals EOF.
func (c *tcpConn) Read(b []byte) (int, error) {
	var timeout <-chan time.Time
	c.mu.Lock()
	if !c.readDeadline.IsZero() {
		// BUG: time.After leaks a timer goroutine if Read returns early.
		// Using a time.Timer would allow cancellation.
		timeout = time.After(time.Until(c.readDeadline))
	}
	buf := c.recvBuf
	c.mu.Unlock()

	select {
	case data, ok := <-buf:
		if !ok {
			return 0, net.ErrClosed
		}
		if data == nil {
			return 0, io.EOF
		}
		n := copy(b, data)
		if DEBUG {
			c.stack.log.Info(
				"raw: conn read",
				"requested", len(b),
				"delivered", n,
				"remaining", len(data)-n,
			)
		}
		if n < len(data) {
			// Push remainder back for the next Read call.
			c.enqueueData(data[n:])
		}
		return n, nil
	case <-timeout:
		return 0, &net.OpError{Op: "read", Net: "tcp", Err: errors.New("timeout")}
	}
}

// Write transmits payload to the guest as a single PSH/ACK segment.
//
// BUG: writeDeadline is not enforced.
func (c *tcpConn) Write(b []byte) (int, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return 0, net.ErrClosed
	}
	seq := c.hostSeq
	ack := c.guestSeq
	c.hostSeq += uint32(len(b))
	c.mu.Unlock()
	if DEBUG {
		c.stack.log.Info("raw: conn write", "len", len(b), "seq", seq, "ack", ack)
	}
	return len(b), c.stack.sendTCPPacket(c.localIPv4, c.key, seq, ack, tcpFlagACK|tcpFlagPSH, b)
}

func (c *tcpConn) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	needFin := c.state == tcpStateEstablished
	if DEBUG {
		c.stack.log.Info("raw: conn close", "needFin", needFin)
	}
	c.state = tcpStateClosed
	c.closed = true
	close(c.recvBuf)
	c.mu.Unlock()

	if needFin {
		c.sendFin()
	}

	c.stack.tcpMu.Lock()
	delete(c.stack.tcpConns, c.key)
	c.stack.tcpMu.Unlock()
	return nil
}

func (c *tcpConn) LocalAddr() net.Addr {
	return &tcpAddr{ip: net.IP(c.localIPv4[:]), port: c.key.dstPort}
}

func (c *tcpConn) RemoteAddr() net.Addr {
	return &tcpAddr{ip: net.IP(c.key.srcIP[:]), port: c.key.srcPort}
}

func (c *tcpConn) SetDeadline(t time.Time) error {
	c.SetReadDeadline(t)
	c.SetWriteDeadline(t)
	return nil
}

func (c *tcpConn) SetReadDeadline(t time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.readDeadline = t
	return nil
}

func (c *tcpConn) SetWriteDeadline(t time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.writeDeadline = t
	return nil
}

func (c *tcpConn) sendRST() error {
	c.mu.Lock()
	seq := c.hostSeq
	ack := c.guestSeq
	c.mu.Unlock()
	return c.stack.sendTCPPacket(c.localIPv4, c.key, seq, ack, tcpFlagRST, nil)
}

// sendTCPPacket crafts and transmits a TCP segment to the guest.
func (ns *NetStack) sendTCPPacket(
	localIPv4 [4]byte,
	key tcpFourTuple,
	seq, ack uint32,
	flags uint16,
	payload []byte,
) error {
	if DEBUG {
		var preview string
		if len(payload) > 0 {
			// BUG: min() is not defined in this file nor in stdlib. This
			// depends on a helper existing elsewhere in the package. If not,
			// this won't compile.
			max := min(len(payload), 64)
			preview = string(payload[:max])
		}
		ns.log.Info(
			"raw: send tcp",
			"srcPort", key.dstPort,
			"dstPort", key.srcPort,
			"seq", seq,
			"ack", ack,
			"flags", fmt.Sprintf("0x%02x", flags),
			"len", len(payload),
			"preview", preview,
		)
	}
	srcIP := net.IP(localIPv4[:])
	dstIP := net.IP(key.srcIP[:])
	srcPort := key.dstPort
	dstPort := key.srcPort

	packet := getTCPPacketBuffer(len(payload))
	binary.BigEndian.PutUint16(packet[0:2], srcPort)
	binary.BigEndian.PutUint16(packet[2:4], dstPort)
	binary.BigEndian.PutUint32(packet[4:8], seq)
	binary.BigEndian.PutUint32(packet[8:12], ack)
	packet[12] = (uint8(tcpHeaderLen/4) << 4)
	packet[13] = uint8(flags)
	binary.BigEndian.PutUint16(packet[14:16], 0xffff) // Window
	copy(packet[tcpHeaderLen:], payload)

	// Compute checksum over pseudo-header + TCP segment.
	binary.BigEndian.PutUint16(packet[16:18], 0)
	check := tcpChecksum(srcIP, dstIP, packet)
	binary.BigEndian.PutUint16(packet[16:18], check)

	// IPv4 encapsulation (pooled buffer).
	ipBuf := getIPv4PacketBuffer(len(packet))
	ip := buildIPv4PacketInto(ipBuf, srcIP, dstIP, tcpProtocolNumber, packet)
	putTCPPacketBuffer(packet)

	// Ethernet framing (pooled buffer).
	frame := getEthernetFrameBuffer(len(ip))
	dstMAC := ns.guestMACForTransmit()
	if !macIsSet(dstMAC) {
		putIPv4PacketBuffer(ip)
		putEthernetFrameBuffer(frame)
		return fmt.Errorf("guest mac unknown for tcp transmit")
	}
	writeMAC(frame[0:6], dstMAC)
	writeMAC(frame[6:12], macAddr(ns.hostMAC.Load()))
	binary.BigEndian.PutUint16(frame[12:14], uint16(etherTypeIPv4))
	copy(frame[ethernetHeaderLen:], ip)
	putIPv4PacketBuffer(ip)

	// The Ethernet buffer is pooled and may be recycled; don't expose it to
	// backends that might retain the slice beyond the call.
	out := append([]byte(nil), frame...)
	putEthernetFrameBuffer(frame)
	return ns.sendFrame(out)
}

// sendRST constructs a reset segment in response to an unexpected inbound.
func (ns *NetStack) sendRST(h ipv4Header, hdr tcpHeader) error {
	key := tcpFourTuple{}
	copy(key.srcIP[:], h.src.To4())
	copy(key.dstIP[:], h.dst.To4())
	key.srcPort = hdr.srcPort
	key.dstPort = hdr.dstPort
	var localIPv4 [4]byte
	if dst := h.dst.To4(); dst != nil {
		copy(localIPv4[:], dst)
	} else {
		copy(localIPv4[:], ns.hostIPv4[:])
	}
	return ns.sendTCPPacket(
		localIPv4,
		key,
		hdr.ack,
		hdr.seq+1,
		tcpFlagRST|tcpFlagACK,
		nil,
	)
}

////////////////////////////////////////////////////////////////////////////////
// Service proxying (TCP only, to localhost).
////////////////////////////////////////////////////////////////////////////////

// shouldProxyService returns true when the destination is the serviceIPv4.
func (ns *NetStack) shouldProxyService(ip net.IP) bool {
	if ip == nil {
		return false
	}
	return ip.To4() != nil && ipEqual(ip.To4(), ns.serviceIPv4[:])
}

// startServiceProxy bridges the TCP conn to 127.0.0.1:dstPort.
func (ns *NetStack) startServiceProxy(conn *tcpConn) {
	go func() {
		addr := &net.TCPAddr{
			IP:   net.IPv4(127, 0, 0, 1),
			Port: int(conn.key.dstPort),
		}
		outbound, err := net.DialTCP("tcp", nil, addr)
		if err != nil {
			slog.Warn(
				"raw: service proxy dial failed",
				"port", conn.key.dstPort,
				"err", err,
			)
			_ = conn.Close()
			return
		}
		defer outbound.Close()
		defer conn.Close()

		if err := proxyConn(outbound, conn, 64*1024); err != nil &&
			!errors.Is(err, io.EOF) &&
			!errors.Is(err, net.ErrClosed) {
			slog.Error("raw: service proxy", "err", err)
		}
	}()
}

////////////////////////////////////////////////////////////////////////////////
// User-facing Listen/Dial for UDP/TCP (limited).
////////////////////////////////////////////////////////////////////////////////

// ListenPacketInternal binds a UDP endpoint on a given port.
func (ns *NetStack) ListenPacketInternal(
	network, address string,
) (net.PacketConn, error) {
	if network != "udp" && network != "udp4" {
		return nil, fmt.Errorf("network %q not supported", network)
	}

	addr, err := splitHostPort(address)
	if err != nil {
		return nil, err
	}

	ep, loaded := ns.udpSockets.LoadOrStore(addr.Port, newUDPEndpointConn(ns, addr.Port))
	if loaded {
		return nil, fmt.Errorf("udp port %d already in use", addr.Port)
	}

	return ep.(*udpEndpointConn), nil
}

// UDPCallback is a function type for handling UDP packets
type UDPCallback func(ep *udpCallbackEndpoint, data []byte, addr net.UDPAddr)

type udpCallbackEndpoint struct {
	stack    *NetStack
	port     uint16
	callback UDPCallback

	closed atomic.Bool
	buf    []byte
}

func newUDPCallbackEndpoint(
	stack *NetStack,
	port uint16,
	callback UDPCallback,
) *udpCallbackEndpoint {
	return &udpCallbackEndpoint{
		stack:    stack,
		port:     port,
		callback: callback,
		buf:      make([]byte, 0, 1500),
	}
}

func (ep *udpCallbackEndpoint) enqueue(data []byte, addr net.UDPAddr) error {
	if ep.closed.Load() {
		return net.ErrClosed
	}

	ep.callback(ep, data, addr)

	return nil
}

func (ep *udpCallbackEndpoint) WriteTo(b []byte, addr net.UDPAddr) (int, error) {
	if ep.closed.Load() {
		return 0, net.ErrClosed
	}

	srcIP := net.IP(ep.stack.hostIPv4[:])
	dstIP := addr.IP

	if len(ep.buf) < ethernetHeaderLen+ipv4HeaderLen+udpHeaderLen+len(b) {
		ep.buf = make([]byte, ethernetHeaderLen+ipv4HeaderLen+udpHeaderLen+len(b))
	}

	copy(ep.buf[ethernetHeaderLen+ipv4HeaderLen+udpHeaderLen:], b)

	err := ep.stack.sendUDP(ep.buf, ep.port, uint16(addr.Port), srcIP, dstIP, len(b))
	if err != nil {
		return 0, err
	}
	return len(b), nil
}

func (ep *udpCallbackEndpoint) Close() error {
	if ep.closed.Load() {
		return nil
	}
	ep.closed.Store(true)

	ep.stack.udpSockets.Delete(ep.port)
	return nil
}

var (
	_ udpEndpoint = (*udpCallbackEndpoint)(nil)
)

// BindUDPCallback binds a UDP port to a callback function.
func (ns *NetStack) BindUDPCallback(address string, callback UDPCallback) error {
	addr, err := splitHostPort(address)
	if err != nil {
		return err
	}

	_, loaded := ns.udpSockets.LoadOrStore(addr.Port, newUDPCallbackEndpoint(ns, addr.Port, callback))
	if loaded {
		return fmt.Errorf("udp port %d already in use", addr.Port)
	}

	return nil
}

// DialInternalContext is not supported in the raw stack.
func (ns *NetStack) DialInternalContext(
	ctx context.Context,
	network, address string,
) (net.Conn, error) {
	return nil, fmt.Errorf("dial not supported in raw netstack")
}

// ListenInternal binds a TCP listener on a given port.
func (ns *NetStack) ListenInternal(
	network, address string,
) (net.Listener, error) {
	if network != "tcp" && network != "tcp4" {
		return nil, fmt.Errorf("network %q not supported", network)
	}

	addr, err := splitHostPort(address)
	if err != nil {
		return nil, err
	}

	ns.tcpMu.Lock()
	defer ns.tcpMu.Unlock()

	if _, ok := ns.tcpListen[addr.Port]; ok {
		return nil, fmt.Errorf("tcp port %d already in use", addr.Port)
	}

	l := newTCPListener(ns, addr.Port)
	ns.tcpListen[addr.Port] = l
	return l, nil
}

////////////////////////////////////////////////////////////////////////////////
// DNS server bridge.
////////////////////////////////////////////////////////////////////////////////

// StartDNSServer binds UDP:53 and serves using a tiny DNS responder.
//
// The server resolves a few internal hostnames, then optionally falls back
// to real DNS if allowInternet is true.
func (ns *NetStack) StartDNSServer() error {
	if ns.dnsServer != nil {
		return nil
	}

	lookup := func(name string) (string, error) {
		n := strings.TrimSuffix(strings.ToLower(name), ".")
		switch n {
		case "host.internal":
			return net.IP(ns.hostIPv4[:]).String(), nil
		case "guest.internal":
			return net.IP(ns.guestIPv4[:]).String(), nil
		case "service.internal":
			return net.IP(ns.serviceIPv4[:]).String(), nil
		}
		if !ns.allowInternet {
			return "", fmt.Errorf("internet access disabled")
		}
		addr, err := net.ResolveIPAddr("ip4", strings.TrimSuffix(name, "."))
		if err != nil {
			return "", err
		}
		return addr.IP.String(), nil
	}

	packetConn, err := ns.ListenPacketInternal("udp", ":53")
	if err != nil {
		return fmt.Errorf("listen udp port 53: %w", err)
	}

	dnsSrv := newDNSServer(ns.log, lookup, packetConn)
	ns.dnsServer = dnsSrv
	dnsSrv.start()
	return nil
}

////////////////////////////////////////////////////////////////////////////////
// Debug HTTP endpoint providing JSON status.
////////////////////////////////////////////////////////////////////////////////

// EnableDebugHTTP starts a small debug server exposing internal state at /status.
//
// BUG: The code uses sync.WaitGroup but calls debugWG.Go(...). WaitGroup
// does not have a Go method; this will not compile unless debugWG is some
// wrapper type elsewhere. Either change to Add/Done or use errgroup.Group.
func (ns *NetStack) EnableDebugHTTP(addr string) error {
	if addr == "" {
		return nil
	}

	ns.debugMu.Lock()
	defer ns.debugMu.Unlock()

	if ns.debugSrv != nil {
		return fmt.Errorf("debug http already enabled at %s", ns.debugAddr)
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen debug http: %w", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/status", ns.handleDebugStatus)

	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	ns.debugSrv = srv
	ns.debugListener = ln
	ns.debugAddr = ln.Addr().String()

	ns.debugWG.Go(func() {
		if err := srv.Serve(ln); err != nil &&
			!errors.Is(err, http.ErrServerClosed) &&
			!errors.Is(err, net.ErrClosed) {
			ns.log.Warn("raw: debug http serve", "err", err)
		}
	})

	if DEBUG {
		ns.log.Info("raw netstack debug http listening", "addr", ns.debugAddr)
	}
	return nil
}

// DebugHTTPAddr returns the bound address of the debug HTTP server.
func (ns *NetStack) DebugHTTPAddr() string {
	ns.debugMu.Lock()
	defer ns.debugMu.Unlock()
	return ns.debugAddr
}

// handleDebugStatus writes a JSON dump of internal state.
func (ns *NetStack) handleDebugStatus(w http.ResponseWriter, r *http.Request) {
	status := ns.collectDebugStatus()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(status); err != nil {
		ns.log.Warn("raw: debug status encode", "err", err)
	}
}

// debugStatus is the JSON structure exposed at /status.
type debugStatus struct {
	HostIPv4       string   `json:"hostIPv4"`
	GuestIPv4      string   `json:"guestIPv4"`
	ServiceIPv4    string   `json:"serviceIPv4"`
	AllowInternet  bool     `json:"allowInternet"`
	ServiceProxy   bool     `json:"serviceProxy"`
	Interfaces     int      `json:"interfaces"`
	TCPListeners   []uint16 `json:"tcpListeners"`
	TCPConnections []string `json:"tcpConnections"`
	UDPSockets     []uint16 `json:"udpSockets"`
	DebugAddr      string   `json:"debugAddr"`
	UDPRxPackets   uint64   `json:"udpRxPackets"`
	UDPTxPackets   uint64   `json:"udpTxPackets"`
	HostMAC        string   `json:"hostMAC"`
	ConfiguredMAC  string   `json:"configuredGuestMAC"`
	ObservedMAC    string   `json:"observedGuestMAC"`
}

func (ns *NetStack) collectDebugStatus() debugStatus {
	status := debugStatus{
		HostIPv4:      net.IP(ns.hostIPv4[:]).String(),
		GuestIPv4:     net.IP(ns.guestIPv4[:]).String(),
		ServiceIPv4:   net.IP(ns.serviceIPv4[:]).String(),
		AllowInternet: ns.allowInternet,
		ServiceProxy:  ns.serviceProxyEnabled,
		DebugAddr:     ns.DebugHTTPAddr(),
	}

	ns.mu.RLock()
	if ns.iface != nil {
		status.Interfaces = 1
	}
	ns.mu.RUnlock()

	if mac := macFromUint64(macAddr(ns.hostMAC.Load())); len(mac) == 6 {
		status.HostMAC = mac.String()
	}
	if mac := macFromUint64(macAddr(ns.guestMAC.Load())); len(mac) == 6 {
		status.ConfiguredMAC = mac.String()
	}
	if mac := macFromUint64(macAddr(ns.observedGuestMAC.Load())); len(mac) == 6 {
		status.ObservedMAC = mac.String()
	}

	ns.tcpMu.Lock()
	for port := range ns.tcpListen {
		status.TCPListeners = append(status.TCPListeners, port)
	}
	for key := range ns.tcpConns {
		connStr := fmt.Sprintf(
			"%s:%d -> %s:%d",
			net.IP(key.srcIP[:]).String(),
			key.srcPort,
			net.IP(key.dstIP[:]).String(),
			key.dstPort,
		)
		status.TCPConnections = append(status.TCPConnections, connStr)
	}
	ns.tcpMu.Unlock()

	ns.udpSockets.Range(func(key, value any) bool {
		if port, ok := key.(uint16); ok {
			status.UDPSockets = append(status.UDPSockets, port)
		}
		return true
	})

	sort.Slice(status.TCPListeners, func(i, j int) bool { return status.TCPListeners[i] < status.TCPListeners[j] })
	sort.Strings(status.TCPConnections)
	sort.Slice(status.UDPSockets, func(i, j int) bool { return status.UDPSockets[i] < status.UDPSockets[j] })

	status.UDPRxPackets = ns.udpRxPackets.Load()
	status.UDPTxPackets = ns.udpTxPackets.Load()

	return status
}

////////////////////////////////////////////////////////////////////////////////
// Helpers: DNS server stop, parsing, checksums, etc.
////////////////////////////////////////////////////////////////////////////////

// hostPort is a helper for parsing "host:port" strings.
type hostPort struct {
	Host string
	Port uint16
}

func splitHostPort(address string) (hostPort, error) {
	if address == "" {
		return hostPort{Host: "", Port: 0}, nil
	}
	if !strings.Contains(address, ":") {
		address = ":" + address
	}

	host, portStr, err := net.SplitHostPort(address)
	if err != nil {
		return hostPort{}, err
	}
	port64, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		return hostPort{}, fmt.Errorf("parse port %q: %w", portStr, err)
	}
	return hostPort{Host: host, Port: uint16(port64)}, nil
}

func ipEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func checksum(data []byte) uint16 {
	var sum uint32
	for i := 0; i+1 < len(data); i += 2 {
		sum += uint32(binary.BigEndian.Uint16(data[i : i+2]))
	}
	if len(data)%2 == 1 {
		sum += uint32(data[len(data)-1]) << 8
	}
	for (sum >> 16) != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return ^uint16(sum)
}

// pseudoHeaderChecksum computes the IPv4 pseudo-header checksum, which is then
// combined with the transport segment checksum.
//
// BUG: No validation is done if src/dst are not IPv4 addresses; callers must
// pass 4-byte addresses.
func pseudoHeaderChecksum(
	src, dst net.IP,
	protocol protocolNumber,
	length int,
) uint32 {
	sum := uint32(0)
	ip4 := src.To4()
	dst4 := dst.To4()
	sum += uint32(binary.BigEndian.Uint16(ip4[0:2]))
	sum += uint32(binary.BigEndian.Uint16(ip4[2:4]))
	sum += uint32(binary.BigEndian.Uint16(dst4[0:2]))
	sum += uint32(binary.BigEndian.Uint16(dst4[2:4]))
	sum += uint32(protocol)
	sum += uint32(length)
	return sum
}

func checksumWithInitial(data []byte, initial uint32) uint16 {
	sum := initial
	for i := 0; i+1 < len(data); i += 2 {
		sum += uint32(binary.BigEndian.Uint16(data[i : i+2]))
	}
	if len(data)%2 == 1 {
		sum += uint32(data[len(data)-1]) << 8
	}
	for (sum >> 16) != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return ^uint16(sum)
}

func tcpChecksum(src, dst net.IP, payload []byte) uint16 {
	ps := pseudoHeaderChecksum(src, dst, tcpProtocolNumber, len(payload))
	return checksumWithInitial(payload, ps)
}

func itoa(v int) string {
	return strconv.Itoa(v)
}

// proxyConn copies data between two connections
func proxyConn(dst, src net.Conn, bufSize int) error {
	buf := make([]byte, bufSize)
	_, err := io.CopyBuffer(dst, src, buf)
	return err
}
