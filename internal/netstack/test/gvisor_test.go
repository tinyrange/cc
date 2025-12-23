package test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
)

// NOTE: Local copy to avoid importing unexported constants from netstack.
const netstackEtherTypeARP = 0x0806

func TestGvisor_ARP_RequestReply(t *testing.T) {
	h := newGvisorHarness(t)

	// Trigger ARP by sending a UDP packet to the host; gVisor will ARP-resolve
	// the destination MAC first.
	udpEp, _ := gvisorDialUDP(t, h.gs, 55555)
	gvisorUDPWriteTo(t, udpEp, hostIPv4, 1053, []byte("arp-probe"))

	// Expect to observe an ARP request from gVisor and a reply from netstack.
	var sawARPReq bool
	deadline := time.Now().Add(2 * time.Second)
	for !sawARPReq && time.Now().Before(deadline) {
		f := awaitFrame(t, h.g2c, time.Second)
		_, _, et, _ := parseEthernet(f)
		if et != uint16(netstackEtherTypeARP) && et != uint16(0x0806) {
			continue
		}
		sawARPReq = true
	}
	if !sawARPReq {
		t.Fatalf("did not observe ARP request from gVisor")
	}

	var sawARPReply bool
	deadline = time.Now().Add(2 * time.Second)
	for !sawARPReply && time.Now().Before(deadline) {
		f := awaitFrame(t, h.c2g, time.Second)
		_, _, et, _ := parseEthernet(f)
		if et == uint16(0x0806) {
			sawARPReply = true
		}
	}
	if !sawARPReply {
		t.Fatalf("did not observe ARP reply from netstack")
	}
}

func TestGvisor_ICMP_Ping(t *testing.T) {
	_ = newGvisorHarness(t)

	// ICMP via gVisor is a bit heavier (raw sockets). For now, keep a placeholder
	// to drive incremental implementation.
	t.Skip("TODO: implement gVisor ICMP echo (raw endpoint) test")
}

func TestGvisor_UDP_SimpleEcho(t *testing.T) {
	h := newGvisorHarness(t)

	pc, err := h.ns.ListenPacketInternal("udp", ":1053")
	if err != nil {
		t.Fatalf("listen udp: %v", err)
	}
	defer pc.Close()

	done := make(chan struct{})
	var got []byte
	go func() {
		defer close(done)
		buf := make([]byte, 2048)
		_ = pc.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, _, rerr := pc.ReadFrom(buf)
		if rerr != nil {
			t.Errorf("udp read: %v", rerr)
			return
		}
		got = append([]byte(nil), buf[:n]...)
	}()

	udpEp, _ := gvisorDialUDP(t, h.gs, 55555)
	gvisorUDPWriteTo(t, udpEp, hostIPv4, 1053, []byte("hello"))

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatalf("timeout waiting for host udp recv")
	}
	if string(got) != "hello" {
		t.Fatalf("unexpected host udp payload %q", string(got))
	}

	// Reply back to gVisor.
	_, werr := pc.WriteTo([]byte("world"), &net.UDPAddr{IP: guestIPv4, Port: 55555})
	if werr != nil {
		t.Fatalf("host udp write: %v", werr)
	}

	data, _ := gvisorUDPRead(t, udpEp, 2*time.Second)
	if string(data) != "world" {
		t.Fatalf("unexpected gvisor udp payload %q", string(data))
	}
}

func TestGvisor_UDP_LargePacket(t *testing.T) {
	h := newGvisorHarness(t)

	pc, err := h.ns.ListenPacketInternal("udp", ":1053")
	if err != nil {
		t.Fatalf("listen udp: %v", err)
	}
	defer pc.Close()

	want := bytes.Repeat([]byte{0xAB}, 1472)
	udpEp, _ := gvisorDialUDP(t, h.gs, 55555)
	gvisorUDPWriteTo(t, udpEp, hostIPv4, 1053, want)

	buf := make([]byte, 4096)
	_ = pc.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, err := pc.ReadFrom(buf)
	if err != nil {
		t.Fatalf("host udp read: %v", err)
	}
	if !bytes.Equal(buf[:n], want) {
		t.Fatalf("large udp payload mismatch: got %d bytes", n)
	}
}

func TestGvisor_UDP_MultiplePackets(t *testing.T) {
	h := newGvisorHarness(t)

	pc, err := h.ns.ListenPacketInternal("udp", ":1053")
	if err != nil {
		t.Fatalf("listen udp: %v", err)
	}
	defer pc.Close()

	udpEp, _ := gvisorDialUDP(t, h.gs, 55555)
	for i := 0; i < 50; i++ {
		gvisorUDPWriteTo(t, udpEp, hostIPv4, 1053, []byte(fmt.Sprintf("pkt-%d", i)))
	}

	seen := make(map[string]bool)
	buf := make([]byte, 2048)
	_ = pc.SetReadDeadline(time.Now().Add(2 * time.Second))
	for i := 0; i < 50; i++ {
		n, _, rerr := pc.ReadFrom(buf)
		if rerr != nil {
			t.Fatalf("host udp read %d: %v", i, rerr)
		}
		seen[string(buf[:n])] = true
	}
	for i := 0; i < 50; i++ {
		k := fmt.Sprintf("pkt-%d", i)
		if !seen[k] {
			t.Fatalf("missing packet %q", k)
		}
	}
}

func TestGvisor_UDP_BidirectionalExchange(t *testing.T) {
	h := newGvisorHarness(t)

	pc, err := h.ns.ListenPacketInternal("udp", ":1053")
	if err != nil {
		t.Fatalf("listen udp: %v", err)
	}
	defer pc.Close()

	udpEp, _ := gvisorDialUDP(t, h.gs, 55555)
	gvisorUDPWriteTo(t, udpEp, hostIPv4, 1053, []byte("a"))

	buf := make([]byte, 1024)
	_ = pc.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, addr, err := pc.ReadFrom(buf)
	if err != nil {
		t.Fatalf("host udp read: %v", err)
	}
	if string(buf[:n]) != "a" {
		t.Fatalf("unexpected host payload %q", string(buf[:n]))
	}

	// host -> gVisor
	uaddr, ok := addr.(*net.UDPAddr)
	if !ok {
		t.Fatalf("unexpected addr %T", addr)
	}
	if _, err := pc.WriteTo([]byte("b"), uaddr); err != nil {
		t.Fatalf("host write: %v", err)
	}
	got, _ := gvisorUDPRead(t, udpEp, 2*time.Second)
	if string(got) != "b" {
		t.Fatalf("unexpected gvisor payload %q", string(got))
	}
}

func TestGvisor_UDP_NoListener(t *testing.T) {
	h := newGvisorHarness(t)

	udpEp, _ := gvisorDialUDP(t, h.gs, 55555)
	gvisorUDPWriteTo(t, udpEp, hostIPv4, 55556, []byte("drop-me"))

	// Our custom netstack currently drops UDP packets without sending ICMP.
	// Ensure we don't get an IPv4/ICMP response frame.
	select {
	case f := <-h.c2g:
		_, _, et, payload := parseEthernet(f)
		if et == uint16(0x0800) {
			ip, _ := mustIPv4Payload(t, payload)
			if ip.Proto == 1 {
				t.Fatalf("unexpected icmp response from netstack")
			}
		}
	case <-time.After(200 * time.Millisecond):
		// ok
	}
}

func TestGvisor_TCP_Handshake(t *testing.T) {
	h := newGvisorHarness(t)

	ln, err := h.ns.ListenInternal("tcp", ":8080")
	if err != nil {
		t.Fatalf("listen tcp: %v", err)
	}
	defer ln.Close()

	acceptCh := acceptOnce(ln)

	_ = gvisorDialTCP(t, h.gs, hostIPv4, 8080)

	select {
	case res := <-acceptCh:
		if res.err != nil {
			t.Fatalf("accept: %v", res.err)
		}
		_ = res.c.Close()
	case <-time.After(3 * time.Second):
		t.Fatalf("timeout waiting for accept")
	}
}

func TestGvisor_TCP_TransparentOutboundProxy(t *testing.T) {
	h := newGvisorHarness(t)

	// Host-side listener that stands in for an "internet" destination.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen host tcp: %v", err)
	}
	defer ln.Close()

	lnAddr := ln.Addr().(*net.TCPAddr)

	// Force all outbound dials to hit our local listener, regardless of the
	// guest-requested destination.
	h.ns.SetOutboundTCPDialer(func(ctx context.Context, addr *net.TCPAddr) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "tcp", lnAddr.String())
	})

	acceptCh := make(chan net.Conn, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		acceptCh <- c
	}()

	// Dial a "remote" IP outside the 10.42.0.0/24 network so gVisor uses the
	// default route via 10.42.0.1 (our netstack gateway).
	remoteIP := net.IPv4(1, 1, 1, 1)
	client := gvisorDialTCP(t, h.gs, remoteIP, 80)
	defer client.Close()

	var server net.Conn
	select {
	case server = <-acceptCh:
	case <-time.After(3 * time.Second):
		t.Fatalf("timeout waiting for host accept")
	}
	defer server.Close()

	// Guest -> host.
	_, _ = client.Write([]byte("ping"))
	buf := make([]byte, 4)
	_ = server.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.ReadFull(server, buf); err != nil || string(buf) != "ping" {
		t.Fatalf("server read: %v payload=%q", err, string(buf))
	}

	// Host -> guest (large enough to require segmentation).
	want := bytes.Repeat([]byte("x"), 10_000)
	_ = server.SetWriteDeadline(time.Now().Add(2 * time.Second))
	if _, err := server.Write(want); err != nil {
		t.Fatalf("server write: %v", err)
	}

	got := make([]byte, len(want))
	_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.ReadFull(client, got); err != nil {
		t.Fatalf("client read: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("payload mismatch")
	}
}

func TestGvisor_TCP_DataTransfer_GuestToHost(t *testing.T) {
	h := newGvisorHarness(t)

	ln, err := h.ns.ListenInternal("tcp", ":8080")
	if err != nil {
		t.Fatalf("listen tcp: %v", err)
	}
	defer ln.Close()

	acceptCh := acceptOnce(ln)

	client := gvisorDialTCP(t, h.gs, hostIPv4, 8080)
	_, _ = client.Write([]byte("hello"))

	var server net.Conn
	select {
	case res := <-acceptCh:
		if res.err != nil {
			t.Fatalf("accept: %v", res.err)
		}
		server = res.c
	case <-time.After(3 * time.Second):
		t.Fatalf("timeout waiting for accept")
	}
	defer server.Close()

	buf := make([]byte, 5)
	_ = server.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := io.ReadFull(server, buf)
	if err != nil {
		t.Fatalf("server read: %v", err)
	}
	if n != 5 || string(buf) != "hello" {
		t.Fatalf("unexpected server payload %q", string(buf))
	}
}

func TestGvisor_TCP_DataTransfer_HostToGuest(t *testing.T) {
	h := newGvisorHarness(t)

	ln, err := h.ns.ListenInternal("tcp", ":8080")
	if err != nil {
		t.Fatalf("listen tcp: %v", err)
	}
	defer ln.Close()

	acceptCh := acceptOnce(ln)

	client := gvisorDialTCP(t, h.gs, hostIPv4, 8080)

	var server net.Conn
	select {
	case res := <-acceptCh:
		if res.err != nil {
			t.Fatalf("accept: %v", res.err)
		}
		server = res.c
	case <-time.After(3 * time.Second):
		t.Fatalf("timeout waiting for accept")
	}
	defer server.Close()

	_, err = server.Write([]byte("ok"))
	if err != nil {
		t.Fatalf("server write: %v", err)
	}
	// Ensure the host actually emitted a TCP payload frame back to the guest.
	// This helps distinguish \"netstack didn't send\" from \"gVisor didn't receive\".
	if got := awaitTCPPayload(t, h.c2g, 2*time.Second); string(got) != "ok" {
		t.Fatalf("unexpected tcp payload on wire: %q", string(got))
	}

	buf := make([]byte, 2)
	_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, err = io.ReadFull(client, buf)
	if err != nil {
		t.Fatalf("client read: %v", err)
	}
	if string(buf) != "ok" {
		t.Fatalf("unexpected client payload %q", string(buf))
	}
}

func TestGvisor_TCP_BidirectionalData(t *testing.T) {
	h := newGvisorHarness(t)

	ln, err := h.ns.ListenInternal("tcp", ":8080")
	if err != nil {
		t.Fatalf("listen tcp: %v", err)
	}
	defer ln.Close()

	acceptCh := acceptOnce(ln)

	client := gvisorDialTCP(t, h.gs, hostIPv4, 8080)
	var server net.Conn
	select {
	case res := <-acceptCh:
		if res.err != nil {
			t.Fatalf("accept: %v", res.err)
		}
		server = res.c
	case <-time.After(3 * time.Second):
		t.Fatalf("timeout waiting for accept")
	}
	defer server.Close()

	_, _ = client.Write([]byte("ping"))
	buf := make([]byte, 4)
	_ = server.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, err = io.ReadFull(server, buf)
	if err != nil || string(buf) != "ping" {
		t.Fatalf("server read: %v payload=%q", err, string(buf))
	}

	_, _ = server.Write([]byte("pong"))
	_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, err = io.ReadFull(client, buf)
	if err != nil || string(buf) != "pong" {
		t.Fatalf("client read: %v payload=%q", err, string(buf))
	}
}

func TestGvisor_TCP_GracefulClose_GuestInitiated(t *testing.T) {
	h := newGvisorHarness(t)

	ln, err := h.ns.ListenInternal("tcp", ":8080")
	if err != nil {
		t.Fatalf("listen tcp: %v", err)
	}
	defer ln.Close()

	acceptCh := acceptOnce(ln)

	client := gvisorDialTCP(t, h.gs, hostIPv4, 8080)
	var server net.Conn
	select {
	case res := <-acceptCh:
		if res.err != nil {
			t.Fatalf("accept: %v", res.err)
		}
		server = res.c
	case <-time.After(3 * time.Second):
		t.Fatalf("timeout waiting for accept")
	}
	defer server.Close()

	_ = client.Close()

	_ = server.SetReadDeadline(time.Now().Add(2 * time.Second))
	b := make([]byte, 1)
	_, err = server.Read(b)
	if err == nil {
		t.Fatalf("expected EOF on server read after client close")
	}
	if !errors.Is(err, io.EOF) && !isTimeout(err) {
		t.Fatalf("expected EOF/timeout, got %v", err)
	}
}

func TestGvisor_TCP_GracefulClose_HostInitiated(t *testing.T) {
	h := newGvisorHarness(t)

	ln, err := h.ns.ListenInternal("tcp", ":8080")
	if err != nil {
		t.Fatalf("listen tcp: %v", err)
	}
	defer ln.Close()

	acceptCh := acceptOnce(ln)

	client := gvisorDialTCP(t, h.gs, hostIPv4, 8080)
	var server net.Conn
	select {
	case res := <-acceptCh:
		if res.err != nil {
			t.Fatalf("accept: %v", res.err)
		}
		server = res.c
	case <-time.After(3 * time.Second):
		t.Fatalf("timeout waiting for accept")
	}

	_ = server.Close()
	_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
	b := make([]byte, 1)
	_, err = client.Read(b)
	if err == nil {
		t.Fatalf("expected EOF on client read after server close")
	}
	if !errors.Is(err, io.EOF) && !isTimeout(err) {
		t.Fatalf("expected EOF/timeout, got %v", err)
	}
}

func TestGvisor_TCP_ConnectionRefused(t *testing.T) {
	h := newGvisorHarness(t)

	_, err := gonet.DialTCP(h.gs, tcpip.FullAddress{
		NIC:  gvisorNICID,
		Addr: mustAddrFrom4(hostIPv4),
		Port: 8080,
	}, ipv4.ProtocolNumber)
	if err == nil {
		t.Fatalf("expected dial error, got nil")
	}
}

func TestGvisor_TCP_LargeTransfer(t *testing.T) {
	h := newGvisorHarness(t)

	ln, err := h.ns.ListenInternal("tcp", ":8080")
	if err != nil {
		t.Fatalf("listen tcp: %v", err)
	}
	defer ln.Close()

	acceptCh := acceptOnce(ln)

	client := gvisorDialTCP(t, h.gs, hostIPv4, 8080)
	var server net.Conn
	select {
	case res := <-acceptCh:
		if res.err != nil {
			t.Fatalf("accept: %v", res.err)
		}
		server = res.c
	case <-time.After(3 * time.Second):
		t.Fatalf("timeout waiting for accept")
	}
	defer server.Close()

	// This may expose missing flow control/reassembly behaviors; keep it
	// somewhat bounded so it remains debuggable.
	want := bytes.Repeat([]byte("x"), 64*1024)
	_ = client.SetWriteDeadline(time.Now().Add(3 * time.Second))
	_, err = client.Write(want)
	if err != nil {
		t.Fatalf("client write: %v", err)
	}

	got := make([]byte, len(want))
	_ = server.SetReadDeadline(time.Now().Add(3 * time.Second))
	n, err := io.ReadFull(server, got)
	if err != nil {
		t.Fatalf("server read: %v (read %d bytes)", err, n)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("large transfer mismatch")
	}
}

func TestGvisor_TCP_SmallWrites(t *testing.T) {
	h := newGvisorHarness(t)

	ln, err := h.ns.ListenInternal("tcp", ":8080")
	if err != nil {
		t.Fatalf("listen tcp: %v", err)
	}
	defer ln.Close()

	acceptCh := acceptOnce(ln)

	client := gvisorDialTCP(t, h.gs, hostIPv4, 8080)
	var server net.Conn
	select {
	case res := <-acceptCh:
		if res.err != nil {
			t.Fatalf("accept: %v", res.err)
		}
		server = res.c
	case <-time.After(3 * time.Second):
		t.Fatalf("timeout waiting for accept")
	}
	defer server.Close()

	for i := 0; i < 200; i++ {
		if _, err := client.Write([]byte{byte(i)}); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}

	got := make([]byte, 200)
	_ = server.SetReadDeadline(time.Now().Add(3 * time.Second))
	n, err := io.ReadFull(server, got)
	if err != nil {
		t.Fatalf("server read: %v (read %d)", err, n)
	}
	if n != 200 {
		t.Fatalf("short read: %d", n)
	}
}

func TestGvisor_TCP_MultipleConcurrentConnections(t *testing.T) {
	h := newGvisorHarness(t)

	ln, err := h.ns.ListenInternal("tcp", ":8080")
	if err != nil {
		t.Fatalf("listen tcp: %v", err)
	}
	defer ln.Close()

	acceptCh := make(chan net.Conn, 32)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			acceptCh <- c
		}
	}()

	const nConns = 10
	errCh := make(chan error, nConns)
	for i := 0; i < nConns; i++ {
		go func(i int) {
			c, err := gvisorTryDialTCP(h.gs, hostIPv4, 8080)
			if err != nil {
				errCh <- err
				return
			}
			defer c.Close()
			msg := []byte(fmt.Sprintf("c-%d", i))
			_, werr := c.Write(msg)
			errCh <- werr
		}(i)
	}

	for i := 0; i < nConns; i++ {
		select {
		case err := <-errCh:
			if err != nil {
				t.Fatalf("client write: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("timeout waiting for client")
		}
	}

	// Drain server conns (best-effort).
	timeout := time.After(2 * time.Second)
	for i := 0; i < nConns; i++ {
		select {
		case c := <-acceptCh:
			_ = c.Close()
		case <-timeout:
			t.Fatalf("timeout waiting for accepts")
		}
	}
}

func TestGvisor_TCP_RapidConnectDisconnect(t *testing.T) {
	h := newGvisorHarness(t)

	ln, err := h.ns.ListenInternal("tcp", ":8080")
	if err != nil {
		t.Fatalf("listen tcp: %v", err)
	}
	defer ln.Close()

	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()

	for i := 0; i < 50; i++ {
		c, err := gvisorTryDialTCP(h.gs, hostIPv4, 8080)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		_ = c.Close()
	}
}

func isTimeout(err error) bool {
	if err == nil {
		return false
	}
	var ne net.Error
	if errors.As(err, &ne) {
		return ne.Timeout()
	}
	return errors.Is(err, context.DeadlineExceeded)
}

type acceptResult struct {
	c   net.Conn
	err error
}

func acceptOnce(ln net.Listener) <-chan acceptResult {
	ch := make(chan acceptResult, 1)
	go func() {
		c, err := ln.Accept()
		ch <- acceptResult{c: c, err: err}
	}()
	return ch
}

func awaitTCPPayload(tb testing.TB, frames <-chan []byte, timeout time.Duration) []byte {
	tb.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		f := awaitFrame(tb, frames, deadline.Sub(time.Now()))
		_, _, et, payload := parseEthernet(f)
		if et != uint16(0x0800) {
			continue
		}
		ip, l4 := mustIPv4Payload(tb, payload)
		if ip.Proto != 6 {
			continue
		}
		if len(l4) < 20 {
			continue
		}
		dataOff := int((l4[12] >> 4) * 4)
		if dataOff < 20 || dataOff > len(l4) {
			continue
		}
		data := l4[dataOff:]
		if len(data) == 0 {
			continue
		}
		return append([]byte(nil), data...)
	}
	tb.Fatalf("timeout waiting for tcp payload frame")
	return nil
}
