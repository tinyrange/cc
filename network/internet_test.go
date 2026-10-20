package network

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"
)

func TestInternetDHCPAndPublicTCP(t *testing.T) {
	n, err := NewInternet(testGuest, testHost)
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	n.Receive(dhcpRequest(1))
	frame := n.Poll()
	if len(frame) < 342 {
		t.Fatal("no DHCP offer")
	}
	options := frame[42+240:]
	found := map[byte]bool{}
	for len(options) >= 2 && options[0] != 255 {
		code, size := options[0], int(options[1])
		options = options[2:]
		if size > len(options) {
			t.Fatal("bad DHCP length")
		}
		if code == 3 || code == 6 {
			if !bytes.Equal(options[:size], hostIP[:]) {
				t.Fatal("bad router/DNS")
			}
			found[code] = true
		}
		options = options[size:]
	}
	if !found[3] || !found[6] {
		t.Fatal("missing router or DNS")
	}
	// An internal listener must NOT capture traffic to the same public port.
	if _, err = n.ListenTCP(443); err != nil {
		t.Fatal(err)
	}
	dialed := make(chan *net.TCPAddr, 1)
	a, b := net.Pipe()
	defer b.Close()
	n.stack.SetOutboundTCPDialer(func(_ context.Context, addr *net.TCPAddr) (net.Conn, error) { dialed <- addr; return a, nil })
	dst := [4]byte{1, 1, 1, 1}
	n.Receive(tcpPacket(40000, 443, 100, 0, 2, nil, dst))
	syn := n.Poll()
	if len(syn) < 54 || syn[47]&0x12 != 0x12 || !bytes.Equal(syn[26:30], dst[:]) {
		t.Fatalf("bad outbound SYN ACK: %x", syn)
	}
	seq := binary.BigEndian.Uint32(syn[38:]) + 1
	n.Receive(tcpPacket(40000, 443, 101, seq, 0x10, nil, dst))
	select {
	case addr := <-dialed:
		if addr.String() != "1.1.1.1:443" {
			t.Fatal(addr)
		}
	case <-time.After(time.Second):
		t.Fatal("no outbound dial")
	}
	n.Receive(tcpPacket(40000, 443, 101, seq, 0x18, []byte("hello"), dst))
	b.SetReadDeadline(time.Now().Add(time.Second))
	got := make([]byte, 5)
	if _, err := io.ReadFull(b, got); err != nil || string(got) != "hello" {
		t.Fatal(string(got), err)
	}
	for _, dst := range [][4]byte{{127, 0, 0, 1}, {192, 168, 1, 1}, {169, 254, 169, 254}, {10, 42, 0, 100}} {
		for n.Poll() != nil {
		}
		n.Receive(tcpPacket(40001, 443, 100, 0, 2, nil, dst))
		reply := n.Poll()
		if len(reply) >= 54 && reply[47]&0x12 == 0x12 {
			t.Fatalf("LAN destination accepted: %v", dst)
		}
	}
	select {
	case addr := <-dialed:
		t.Fatal("unexpected dial", addr)
	default:
	}
}
func TestInternetDNSGuestName(t *testing.T) {
	n, err := NewInternet(testGuest, testHost)
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	// Query synthetic guest.internal: exercises UDP/DNS without external DNS.
	dns := []byte{0x12, 0x34, 1, 0, 0, 1, 0, 0, 0, 0, 0, 0, 5, 'g', 'u', 'e', 's', 't', 8, 'i', 'n', 't', 'e', 'r', 'n', 'a', 'l', 0, 0, 1, 0, 1}
	udp := make([]byte, 8)
	binary.BigEndian.PutUint16(udp, 40000)
	binary.BigEndian.PutUint16(udp[2:], 53)
	binary.BigEndian.PutUint16(udp[4:], uint16(8+len(dns)))
	n.Receive(ipv4Frame(17, guestIP, hostIP, append(udp, dns...)))
	until := time.Now().Add(time.Second)
	for time.Now().Before(until) {
		frame := n.Poll()
		if len(frame) >= 54 && frame[23] == 17 {
			reply := frame[42:]
			if reply[0] != 0x12 || reply[1] != 0x34 || reply[3]&15 != 0 || !bytes.Contains(reply, guestIP[:]) {
				t.Fatalf("bad DNS response %x", reply)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("no DNS reply")
}
