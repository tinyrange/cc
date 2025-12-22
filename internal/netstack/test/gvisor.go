package test

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/tinyrange/cc/internal/netstack"

	"gvisor.dev/gvisor/pkg/buffer"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/link/channel"
	"gvisor.dev/gvisor/pkg/tcpip/link/ethernet"
	"gvisor.dev/gvisor/pkg/tcpip/network/arp"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
	"gvisor.dev/gvisor/pkg/waiter"
)

const (
	gvisorNICID tcpip.NICID = 1
)

var (
	hostIPv4  = net.IPv4(10, 42, 0, 1)
	guestIPv4 = net.IPv4(10, 42, 0, 2)
)

type gvisorHarness struct {
	t testing.TB

	ctx    context.Context
	cancel context.CancelFunc

	// custom netstack (host side)
	ns  *netstack.NetStack
	nic *netstack.NetworkInterface

	// gVisor stack (guest side)
	gs      *stack.Stack
	ch      *channel.Endpoint
	guestMA net.HardwareAddr

	// observation channels
	g2c chan []byte // gVisor -> custom (ethernet frames)
	c2g chan []byte // custom -> gVisor (ethernet frames)
}

func mustAddrFrom4(ip net.IP) tcpip.Address {
	ip4 := ip.To4()
	if ip4 == nil || len(ip4) != 4 {
		panic("expected IPv4")
	}
	var b [4]byte
	copy(b[:], ip4)
	return tcpip.AddrFrom4(b)
}

func newGvisorHarness(tb testing.TB) *gvisorHarness {
	tb.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	h := &gvisorHarness{
		t:       tb,
		ctx:     ctx,
		cancel:  cancel,
		guestMA: net.HardwareAddr{0x02, 0x00, 0x00, 0x00, 0x00, 0x02},
		g2c:     make(chan []byte, 4096),
		c2g:     make(chan []byte, 4096),
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	h.ns = netstack.New(logger)
	if err := h.ns.SetGuestMAC(h.guestMA); err != nil {
		tb.Fatalf("set guest mac: %v", err)
	}
	nic, err := h.ns.AttachNetworkInterface()
	if err != nil {
		tb.Fatalf("attach network interface: %v", err)
	}
	h.nic = nic

	// gVisor stack + channel endpoint.
	// channel.Endpoint.MTU is treated as the L2 MTU by ethernet.Endpoint, which
	// subtracts the ethernet header length to get the L3 MTU. Use 1500 L3 MTU.
	h.ch = channel.New(4096, 1500+header.EthernetMinimumSize, tcpip.LinkAddress(string(h.guestMA)))
	ep := ethernet.New(h.ch)
	h.gs = stack.New(stack.Options{
		NetworkProtocols:   []stack.NetworkProtocolFactory{ipv4.NewProtocol, arp.NewProtocol},
		TransportProtocols: []stack.TransportProtocolFactory{tcp.NewProtocol, udp.NewProtocol},
	})
	if err := h.gs.CreateNIC(gvisorNICID, ep); err != nil {
		tb.Fatalf("gvisor CreateNIC: %v", err)
	}
	if err := h.gs.AddProtocolAddress(
		gvisorNICID,
		tcpip.ProtocolAddress{
			Protocol: ipv4.ProtocolNumber,
			AddressWithPrefix: tcpip.AddressWithPrefix{
				Address:   mustAddrFrom4(guestIPv4),
				PrefixLen: 24,
			},
		},
		stack.AddressProperties{},
	); err != nil {
		tb.Fatalf("gvisor AddProtocolAddress: %v", err)
	}
	h.gs.SetRouteTable([]tcpip.Route{
		{
			Destination: header.IPv4EmptySubnet,
			Gateway:     mustAddrFrom4(hostIPv4),
			NIC:         gvisorNICID,
		},
	})

	// custom -> gVisor
	h.nic.AttachVirtioBackend(func(frame []byte) error {
		out := append([]byte(nil), frame...)
		select {
		case h.c2g <- out:
		default:
			tb.Fatalf("c2g frame buffer full")
		}

		pkt := stack.NewPacketBuffer(stack.PacketBufferOptions{
			Payload: buffer.MakeWithData(out),
		})
		// The ethernet link endpoint ignores the network protocol argument and
		// parses the ethernet header from pkt.LinkHeader()/the packet contents.
		h.ch.InjectInbound(0, pkt)
		return nil
	})

	// gVisor -> custom
	go func() {
		for {
			pkt := h.ch.ReadContext(h.ctx)
			if pkt == nil {
				return
			}
			b := pkt.ToView().AsSlice()
			out := append([]byte(nil), b...)
			pkt.DecRef()

			select {
			case h.g2c <- out:
			default:
				tb.Fatalf("g2c frame buffer full")
			}

			_ = h.nic.DeliverGuestPacket(out, nil)
		}
	}()

	tb.Cleanup(func() {
		h.cancel()
		h.ch.Close()
		_ = h.ns.Close()
	})
	return h
}

func awaitFrame(tb testing.TB, ch <-chan []byte, timeout time.Duration) []byte {
	tb.Helper()
	if timeout <= 0 {
		timeout = time.Second
	}
	select {
	case f, ok := <-ch:
		if !ok {
			tb.Fatalf("frame channel closed")
		}
		return f
	case <-time.After(timeout):
		tb.Fatalf("timeout waiting for frame")
		return nil
	}
}

func parseEthernet(frame []byte) (dst, src net.HardwareAddr, etherType uint16, payload []byte) {
	if len(frame) < 14 {
		return nil, nil, 0, nil
	}
	dst = net.HardwareAddr(frame[0:6])
	src = net.HardwareAddr(frame[6:12])
	etherType = binary.BigEndian.Uint16(frame[12:14])
	return dst, src, etherType, frame[14:]
}

func mustIPv4Payload(tb testing.TB, ethPayload []byte) (hdr netstackTestIPv4, l4 []byte) {
	tb.Helper()
	if len(ethPayload) < 20 {
		tb.Fatalf("ipv4 payload too short: %d", len(ethPayload))
	}
	verIHL := ethPayload[0]
	version := verIHL >> 4
	ihl := verIHL & 0x0f
	if version != 4 {
		tb.Fatalf("unexpected ipv4 version: %d", version)
	}
	headerLen := int(ihl) * 4
	if len(ethPayload) < headerLen {
		tb.Fatalf("ipv4 header length mismatch: %d > %d", headerLen, len(ethPayload))
	}
	totalLen := int(binary.BigEndian.Uint16(ethPayload[2:4]))
	if totalLen > len(ethPayload) {
		tb.Fatalf("ipv4 total length exceeds payload: %d > %d", totalLen, len(ethPayload))
	}
	hdr = netstackTestIPv4{
		Proto: ethPayload[9],
		Src:   net.IP(append([]byte(nil), ethPayload[12:16]...)),
		Dst:   net.IP(append([]byte(nil), ethPayload[16:20]...)),
	}
	return hdr, ethPayload[headerLen:totalLen]
}

type netstackTestIPv4 struct {
	Proto uint8
	Src   net.IP
	Dst   net.IP
}

func gvisorDialTCP(tb testing.TB, gs *stack.Stack, dstIP net.IP, dstPort uint16) net.Conn {
	tb.Helper()
	c, err := gvisorTryDialTCP(gs, dstIP, dstPort)
	if err != nil {
		tb.Fatalf("gvisor dial tcp: %v", err)
	}
	tb.Cleanup(func() { _ = c.Close() })
	return c
}

func gvisorTryDialTCP(gs *stack.Stack, dstIP net.IP, dstPort uint16) (net.Conn, error) {
	// Use gVisor's net adapters for a blocking dial.
	return gonet.DialTCP(gs, tcpip.FullAddress{
		NIC:  gvisorNICID,
		Addr: mustAddrFrom4(dstIP),
		Port: dstPort,
	}, ipv4.ProtocolNumber)
}

func gvisorDialUDP(tb testing.TB, gs *stack.Stack, localPort uint16) (tcpip.Endpoint, *waiter.Queue) {
	tb.Helper()
	var wq waiter.Queue
	ep, terr := gs.NewEndpoint(udp.ProtocolNumber, ipv4.ProtocolNumber, &wq)
	if terr != nil {
		tb.Fatalf("gvisor new udp endpoint: %v", terr)
	}
	// Bind to guest port on the NIC.
	if terr := ep.Bind(tcpip.FullAddress{
		NIC:  gvisorNICID,
		Addr: mustAddrFrom4(guestIPv4),
		Port: localPort,
	}); terr != nil {
		ep.Close()
		tb.Fatalf("gvisor udp bind: %v", terr)
	}
	tb.Cleanup(func() { ep.Close() })
	return ep, &wq
}

func gvisorUDPWriteTo(tb testing.TB, ep tcpip.Endpoint, dstIP net.IP, dstPort uint16, payload []byte) {
	tb.Helper()
	n, terr := ep.Write(bytes.NewReader(payload), tcpip.WriteOptions{
		To: &tcpip.FullAddress{
			NIC:  gvisorNICID,
			Addr: mustAddrFrom4(dstIP),
			Port: dstPort,
		},
	})
	if terr != nil {
		tb.Fatalf("gvisor udp write: %v", terr)
	}
	if int(n) != len(payload) {
		tb.Fatalf("gvisor udp short write: %d != %d", n, len(payload))
	}
}

func gvisorUDPRead(tb testing.TB, ep tcpip.Endpoint, timeout time.Duration) (data []byte, from tcpip.FullAddress) {
	tb.Helper()
	deadline := time.Now().Add(timeout)
	for {
		buf := make([]byte, 64*1024)
		w := tcpip.SliceWriter(buf)
		rr, terr := ep.Read(&w, tcpip.ReadOptions{NeedRemoteAddr: true})
		if terr == nil {
			return buf[:rr.Count], rr.RemoteAddr
		}
		if _, ok := terr.(*tcpip.ErrWouldBlock); ok {
			if time.Now().After(deadline) {
				tb.Fatalf("timeout waiting for gvisor udp read")
			}
			time.Sleep(1 * time.Millisecond)
			continue
		}
		tb.Fatalf("gvisor udp read: %v", terr)
	}
}
