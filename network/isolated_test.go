package network

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"
)

var testGuest = [6]byte{2, 0x54, 0x52, 0, 0, 1}
var testHost = [6]byte{2, 0x54, 0x52, 0, 0, 2}

func testNetwork(t *testing.T) *Isolated {
	t.Helper()
	n, err := NewIsolated(testGuest, testHost)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { n.Close() })
	return n
}
func ipv4Frame(proto byte, src, dst [4]byte, payload []byte) []byte {
	f := make([]byte, 34)
	copy(f, testHost[:])
	copy(f[6:], testGuest[:])
	binary.BigEndian.PutUint16(f[12:], 0x800)
	h := f[14:]
	h[0], h[8], h[9] = 0x45, 64, proto
	binary.BigEndian.PutUint16(h[2:], uint16(20+len(payload)))
	copy(h[12:16], src[:])
	copy(h[16:20], dst[:])
	binary.BigEndian.PutUint16(h[10:], checksum(h))
	return append(f, payload...)
}
func dhcpRequest(kind byte, options ...byte) []byte {
	p := make([]byte, 248)
	binary.BigEndian.PutUint16(p, 68)
	binary.BigEndian.PutUint16(p[2:], 67)
	b := p[8:]
	b[0], b[1], b[2] = 1, 1, 6
	copy(b[4:8], []byte{1, 2, 3, 4})
	copy(b[28:], testGuest[:])
	copy(b[236:], []byte{99, 130, 83, 99})
	p = append(p, 53, 1, kind)
	p = append(p, options...)
	p = append(p, 255)
	binary.BigEndian.PutUint16(p[4:], uint16(len(p)))
	return ipv4Frame(17, [4]byte{}, broadcastIP, p)
}
func TestDHCPLeaseAndIsolation(t *testing.T) {
	n := testNetwork(t)
	for _, kind := range []byte{1, 3} {
		opts := []byte{50, 4, 10, 42, 0, 2, 54, 4, 10, 42, 0, 1}
		if err := n.Receive(dhcpRequest(kind, opts...)); err != nil {
			t.Fatal(err)
		}
		frame := n.Poll()
		if len(frame) < 342 {
			t.Fatal("missing DHCP response")
		}
		if checksum(frame[14:34]) != 0 {
			t.Fatal("bad IPv4 checksum")
		}
		b := frame[42:]
		if !bytes.Equal(b[4:8], []byte{1, 2, 3, 4}) || !bytes.Equal(b[16:20], guestIP[:]) {
			t.Fatal("wrong transaction or lease")
		}
		want := byte(2)
		if kind == 3 {
			want = 5
		}
		if b[242] != want {
			t.Fatal("wrong DHCP reply", b[242])
		}
		for at := 240; b[at] != 255; {
			code, size := b[at], int(b[at+1])
			at += 2
			if code == 3 || code == 6 {
				t.Fatal("router/DNS leaked into private network")
			}
			at += size
		}
	}
	bad := dhcpRequest(3, 50, 4, 10, 42, 0, 2, 54, 4, 10, 42, 0, 99)
	n.Receive(bad)
	if n.Poll() != nil {
		t.Fatal("responded for another DHCP server")
	}
	bad = dhcpRequest(1)
	bad[6] ^= 2
	n.Receive(bad)
	if n.Poll() != nil {
		t.Fatal("spoofed MAC accepted")
	}
	bad = dhcpRequest(1, 53, 1, 1)
	n.Receive(bad)
	if n.Poll() != nil {
		t.Fatal("duplicate DHCP option accepted")
	}
}
func tcpPacket(srcPort, dstPort uint16, seq, ack uint32, flags byte, payload []byte, dst [4]byte) []byte {
	tcp := make([]byte, 20)
	binary.BigEndian.PutUint16(tcp, srcPort)
	binary.BigEndian.PutUint16(tcp[2:], dstPort)
	binary.BigEndian.PutUint32(tcp[4:], seq)
	binary.BigEndian.PutUint32(tcp[8:], ack)
	tcp[12], tcp[13] = 0x50, flags
	binary.BigEndian.PutUint16(tcp[14:], 65535)
	tcp = append(tcp, payload...)
	pseudo := append([]byte(nil), guestIP[:]...)
	pseudo = append(pseudo, dst[:]...)
	pseudo = append(pseudo, 0, 6, byte(len(tcp)>>8), byte(len(tcp)))
	binary.BigEndian.PutUint16(tcp[16:], checksum(append(pseudo, tcp...)))
	return ipv4Frame(6, guestIP, dst, tcp)
}
func TestInternalTCPAndDestinationFilter(t *testing.T) {
	n := testNetwork(t)
	l, err := n.ListenTCP(445)
	if err != nil {
		t.Fatal(err)
	}
	for _, dst := range [][4]byte{{127, 0, 0, 1}, {10, 42, 0, 100}, {1, 1, 1, 1}, {169, 254, 169, 254}} {
		n.Receive(tcpPacket(40000, 445, 100, 0, 2, nil, dst))
		if n.Poll() != nil {
			t.Fatalf("accepted nonlocal destination %v", dst)
		}
	}
	n.Receive(tcpPacket(40000, 445, 100, 0, 2, nil, hostIP))
	synack := n.Poll()
	if len(synack) < 54 || synack[47]&0x12 != 0x12 {
		t.Fatalf("no SYN ACK: %x", synack)
	}
	hostSeq := binary.BigEndian.Uint32(synack[38:]) + 1
	n.Receive(tcpPacket(40000, 445, 101, hostSeq, 0x10, nil, hostIP))
	c, err := l.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(time.Second))
	n.Receive(tcpPacket(40000, 445, 101, hostSeq, 0x18, []byte("hello"), hostIP))
	b := make([]byte, 5)
	if _, err := io.ReadFull(c, b); err != nil || string(b) != "hello" {
		t.Fatal("payload", string(b), err)
	}
	for n.Poll() != nil {
	}
	if _, err := c.Write([]byte("world")); err != nil {
		t.Fatal(err)
	}
	frame := n.Poll()
	if len(frame) < 59 || string(frame[54:]) != "world" {
		t.Fatalf("bad reply: %x", frame)
	}
	n.Receive(tcpPacket(40000, 445, 106, hostSeq+5, 0x10, nil, hostIP))
	if err := n.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Accept(); err != net.ErrClosed {
		t.Fatal("listener still open", err)
	}
	if err := n.Receive(nil); err != net.ErrClosed {
		t.Fatal(err)
	}
}
func TestConnectionAndOutputBounds(t *testing.T) {
	n := testNetwork(t)
	if _, err := n.ListenTCP(445); err != nil {
		t.Fatal(err)
	}
	// More completed handshakes than the accept backlog must not block Receive.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 20; i++ {
			port := uint16(40000 + i)
			n.Receive(tcpPacket(port, 445, 100, 0, 2, nil, hostIP))
			reply := n.Poll()
			if len(reply) < 54 {
				return
			}
			seq := binary.BigEndian.Uint32(reply[38:]) + 1
			n.Receive(tcpPacket(port, 445, 101, seq, 0x10, nil, hostIP))
			for n.Poll() != nil {
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("accept backlog blocked packet pump")
	}
	// Half-open SYNs are also capped, not just accepted application streams.
	replies := 0
	for i := 0; i < 128; i++ {
		n.Receive(tcpPacket(uint16(41000+i), 445, 100, 0, 2, nil, hostIP))
		if n.Poll() != nil {
			replies++
		}
	}
	if replies > 64 || replies == 0 {
		t.Fatal("unbounded connection count", replies)
	}
	f := bytes.Repeat([]byte{7}, 60)
	for i := 0; i < maxQueuedFrames+10; i++ {
		n.enqueue(f)
	}
	f[0] = 9
	for i := 0; i < maxQueuedFrames; i++ {
		if p := n.Poll(); len(p) != 60 || p[0] != 7 {
			t.Fatal("queue does not own bounded copies")
		}
	}
	if n.Poll() != nil {
		t.Fatal("unbounded output queue")
	}
}
func FuzzReceive(f *testing.F) {
	f.Add(dhcpRequest(1))
	f.Add(tcpPacket(40000, 445, 100, 0, 2, nil, hostIP))
	f.Fuzz(func(t *testing.T, frame []byte) {
		if len(frame) > 1514 {
			return
		}
		n := testNetwork(t)
		n.Receive(frame)
	})
}
