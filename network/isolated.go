// Package network exposes an isolated, packet-only endpoint for guest services.
// By default it never binds a host socket, resolves names, or forwards traffic.
// NewInternet explicitly enables outbound IPv4 TCP and DNS, not host/LAN access.
package network

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"net"
	"strconv"
	"sync"

	"j5.nz/cc/internal/netstack"
)

var hostIP = [4]byte{10, 42, 0, 1}
var guestIP = [4]byte{10, 42, 0, 2}
var broadcastMAC = [6]byte{255, 255, 255, 255, 255, 255}
var broadcastIP = [4]byte{255, 255, 255, 255}

const maxQueuedFrames = 256
const maxFrameSize = 1514

// Isolated connects a single guest (10.42.0.2/24) to internal services on
// 10.42.0.1. NewIsolated supplies only address/subnet via DHCP; NewInternet
// also supplies router/DNS and explicitly enables public IPv4 TCP proxying.
// Receive and Poll exchange Ethernet frames without an FCS. Poll transfers
// ownership; Receive borrows input only for the duration of the call.
// Close unblocks listener/connection operations. The zero value is not usable.
type Isolated struct {
	mu           sync.Mutex
	closed       bool
	stack        *netstack.NetStack
	nic          *netstack.NetworkInterface
	guest, host  [6]byte
	queueMu      sync.Mutex
	queue        [][]byte
	outputClosed bool
	internet     bool
}

func NewIsolated(guest, host [6]byte) (*Isolated, error) {
	return newEndpoint(guest, host, false)
}

// NewInternet enables outbound public IPv4 TCP and DNS. It does not bind host
// listeners or allow access to host/LAN addresses. UDP other than DNS is dropped.
func NewInternet(guest, host [6]byte) (*Isolated, error) {
	return newEndpoint(guest, host, true)
}

func newEndpoint(guest, host [6]byte, internet bool) (*Isolated, error) {
	if guest[0]&1 != 0 || host[0]&1 != 0 || guest == host || guest == [6]byte{} || host == [6]byte{} {
		return nil, errors.New("isolated network requires distinct unicast MACs")
	}
	ns := netstack.New(nil)
	ns.SetServiceProxyEnabled(false)
	ns.SetHostAccessEnabled(false)
	ns.SetInternetAccessEnabled(internet)
	ns.SetAllowedServiceProxyPorts(nil)
	ns.SetChecksumValidationEnabled(true)
	ns.SetTCPConnectionLimit(64)
	if !internet {
		ns.SetOutboundTCPDialer(func(context.Context, *net.TCPAddr) (net.Conn, error) {
			return nil, errors.New("isolated network cannot dial")
		})
	} else if err := ns.StartDNSServer(); err != nil {
		ns.Close()
		return nil, err
	}
	if err := ns.SetGuestMAC(net.HardwareAddr(guest[:])); err != nil {
		ns.Close()
		return nil, err
	}
	if err := ns.SetHostMAC(net.HardwareAddr(host[:])); err != nil {
		ns.Close()
		return nil, err
	}
	nic, err := ns.AttachNetworkInterface()
	if err != nil {
		ns.Close()
		return nil, err
	}
	n := &Isolated{stack: ns, nic: nic, guest: guest, host: host, internet: internet}
	nic.AttachVirtioBackend(n.enqueue)
	return n, nil
}
func (n *Isolated) enqueue(frame []byte) error {
	n.queueMu.Lock()
	defer n.queueMu.Unlock()
	if n.outputClosed {
		return net.ErrClosed
	}
	if len(frame) > maxFrameSize || len(n.queue) >= maxQueuedFrames {
		return nil
	}
	n.queue = append(n.queue, append([]byte(nil), frame...))
	return nil
}

// Poll returns the next outgoing frame, or nil when no frame is ready.
func (n *Isolated) Poll() []byte {
	n.queueMu.Lock()
	defer n.queueMu.Unlock()
	if len(n.queue) == 0 {
		return nil
	}
	frame := n.queue[0]
	n.queue[0] = nil
	n.queue = n.queue[1:]
	return frame
}

// ListenTCP creates an in-memory TCP listener, never a host listener. Only
// packets addressed to the internal service IP can reach any such listener.
func (n *Isolated) ListenTCP(port uint16) (net.Listener, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.closed {
		return nil, net.ErrClosed
	}
	if port == 0 {
		return nil, errors.New("isolated TCP port must be nonzero")
	}
	return n.stack.ListenInternal("tcp4", "10.42.0.1:"+strconv.Itoa(int(port)))
}
func (n *Isolated) Close() error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.closed {
		return nil
	}
	n.closed = true
	n.queueMu.Lock()
	n.outputClosed = true
	n.queue = nil
	n.queueMu.Unlock()
	return n.stack.Close()
}

// Receive drops unsupported or malformed traffic rather than letting a guest
// packet terminate the emulator. The only returned error is use after Close.
func (n *Isolated) Receive(frame []byte) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.closed {
		return net.ErrClosed
	}
	if len(frame) < 14 || len(frame) > maxFrameSize || !bytes.Equal(frame[6:12], n.guest[:]) {
		return nil
	}
	if !bytes.Equal(frame[:6], n.host[:]) && !bytes.Equal(frame[:6], broadcastMAC[:]) {
		return nil
	}
	switch binary.BigEndian.Uint16(frame[12:]) {
	case 0x0806:
		if len(frame) < 42 || !bytes.Equal(frame[38:42], hostIP[:]) {
			return nil
		}
		_ = n.nic.DeliverGuestPacket(frame, true)
	case 0x0800:
		ip := frame[14:]
		if len(ip) < 20 || ip[0]>>4 != 4 {
			return nil
		}
		header := int(ip[0]&15) * 4
		size := int(binary.BigEndian.Uint16(ip[2:]))
		if header < 20 || size < header || size > len(ip) || binary.BigEndian.Uint16(ip[6:])&0x3fff != 0 || checksum(ip[:header]) != 0 {
			return nil
		}
		ip = ip[:size]
		if !bytes.Equal(ip[12:16], guestIP[:]) && !bytes.Equal(ip[12:16], []byte{0, 0, 0, 0}) {
			return nil
		}
		if ip[9] == 17 {
			if bytes.Equal(ip[16:20], hostIP[:]) || bytes.Equal(ip[16:20], broadcastIP[:]) {
				n.dhcp(ip, header)
			}
			if n.internet && bytes.Equal(ip[12:16], guestIP[:]) && bytes.Equal(ip[16:20], hostIP[:]) && len(ip) >= header+8 && binary.BigEndian.Uint16(ip[header+2:]) == 53 {
				_ = n.nic.DeliverGuestPacket(frame[:14+size], true)
			}
			return nil
		}
		if !bytes.Equal(ip[12:16], guestIP[:]) || (!n.internet && !bytes.Equal(ip[16:20], hostIP[:])) {
			return nil
		}
		if ip[9] != 6 && ip[9] != 1 {
			return nil
		}
		_ = n.nic.DeliverGuestPacket(frame[:14+size], true)
	}
	return nil
}

func checksum(b []byte) uint16 {
	var sum uint32
	for len(b) >= 2 {
		sum += uint32(binary.BigEndian.Uint16(b))
		b = b[2:]
	}
	if len(b) != 0 {
		sum += uint32(b[0]) << 8
	}
	for sum>>16 != 0 {
		sum = (sum & 65535) + (sum >> 16)
	}
	return ^uint16(sum)
}
