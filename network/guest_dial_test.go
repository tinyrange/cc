package network

import (
	"context"
	"encoding/binary"
	"net"
	"testing"
	"time"
)

func sum16(b []byte) uint16 {
	var s uint32
	for i := 0; i+1 < len(b); i += 2 {
		s += uint32(binary.BigEndian.Uint16(b[i:]))
	}
	if len(b)%2 == 1 {
		s += uint32(b[len(b)-1]) << 8
	}
	for s>>16 != 0 {
		s = s&0xffff + s>>16
	}
	return ^uint16(s)
}

// A guest with its own static address accepts a host-side dial.
func TestDialGuestWithStaticAddress(t *testing.T) {
	guest, host := [6]byte{2, 0, 0, 0, 0x39, 2}, [6]byte{2, 0, 0, 0, 0x39, 1}
	n, err := NewIsolated(guest, host)
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	gip := net.IPv4(9, 19, 40, 167).To4()
	if err := n.SetGuestIPv4(gip); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		c, err := n.DialGuest(ctx, 80)
		if err == nil {
			c.Close()
		}
		done <- err
	}()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		f := n.Poll()
		if f == nil {
			time.Sleep(time.Millisecond)
			continue
		}
		t.Logf("frame dst %X type %04X len %d", f[:6], binary.BigEndian.Uint16(f[12:]), len(f))
		if binary.BigEndian.Uint16(f[12:]) != 0x0800 {
			continue
		}
		ip := f[14:]
		t.Logf("ip %v -> %v proto %d flags %02X", net.IP(ip[12:16]), net.IP(ip[16:20]), ip[9], ip[33])
		// Reply SYN-ACK from the guest.
		tcp := make([]byte, 20)
		copy(tcp[0:2], ip[22:24])
		copy(tcp[2:4], ip[20:22])
		binary.BigEndian.PutUint32(tcp[4:], 5000)
		binary.BigEndian.PutUint32(tcp[8:], binary.BigEndian.Uint32(ip[24:])+1)
		tcp[12], tcp[13] = 5<<4, 0x12
		binary.BigEndian.PutUint16(tcp[14:], 65535)
		ph := append(append(append([]byte{}, ip[16:20]...), ip[12:16]...), 0, 6, 0, 20)
		binary.BigEndian.PutUint16(tcp[16:], sum16(append(ph, tcp...)))
		rip := make([]byte, 20)
		rip[0], rip[8], rip[9] = 0x45, 64, 6
		binary.BigEndian.PutUint16(rip[2:], 40)
		copy(rip[12:16], ip[16:20])
		copy(rip[16:20], ip[12:16])
		binary.BigEndian.PutUint16(rip[10:], sum16(rip))
		frame := append(append(append(append([]byte{}, host[:]...), guest[:]...), 8, 0), append(rip, tcp...)...)
		if err := n.Receive(frame); err != nil {
			t.Fatal(err)
		}
		break
	}
	if err := <-done; err != nil {
		t.Fatalf("dial: %v", err)
	}
}
