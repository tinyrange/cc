package netstack

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

func TestTCPConnCloseWriteKeepsReadSideOpen(t *testing.T) {
	h := newBenchmarkHarness(t)
	key := tcpFourTuple{
		srcIP:   [4]byte{10, 42, 0, 2},
		dstIP:   [4]byte{10, 42, 0, 1},
		srcPort: 8080,
		dstPort: 49152,
	}
	conn := newTCPConn(h.stack, nil, key, 1, 0xffff, [4]byte{10, 42, 0, 1}, nil)
	conn.state = tcpStateEstablished
	h.stack.tcpMu.Lock()
	h.stack.tcpConns[key] = conn
	h.stack.tcpMu.Unlock()
	defer conn.Close()

	if err := conn.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	select {
	case frame := <-h.tcpFrames:
		if frame.flags&tcpFlagFIN == 0 {
			t.Fatalf("CloseWrite flags = %#x, want FIN", frame.flags)
		}
	case <-time.After(time.Second):
		t.Fatal("CloseWrite did not send FIN")
	}
	if _, err := conn.Write([]byte("late write")); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("write after CloseWrite error = %v, want net.ErrClosed", err)
	}

	conn.enqueueData([]byte("response"))
	buf := make([]byte, len("response"))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "response" {
		t.Fatalf("read after CloseWrite = %q, want response", buf)
	}
}

func TestParseTCPOptionsExtractsMSSAndWindowScale(t *testing.T) {
	options := []byte{
		tcpOptNOP,
		tcpOptMSS, 4, 0x05, 0xb4,
		tcpOptWndScale, 3, 7,
		30, 4, 1, 2,
		tcpOptEnd,
	}
	got := parseTCPOptions(options)
	if !got.hasMSS || got.mss != 1460 {
		t.Fatalf("MSS = %d/%v, want 1460/true", got.mss, got.hasMSS)
	}
	if !got.hasWndScale || got.wndScale != 7 {
		t.Fatalf("window scale = %d/%v, want 7/true", got.wndScale, got.hasWndScale)
	}
}

func TestParseTCPOptionsStopsOnInvalidLengths(t *testing.T) {
	got := parseTCPOptions([]byte{tcpOptMSS, 1, tcpOptWndScale, 3, 9})
	if got.hasMSS || !got.hasWndScale || got.wndScale != 9 {
		t.Fatalf("invalid MSS length handling parsed options: %+v", got)
	}

	got = parseTCPOptions([]byte{44, 0, tcpOptMSS, 4, 0x01, 0x02})
	if got.hasMSS {
		t.Fatalf("invalid unknown length should stop parsing: %+v", got)
	}
}

func TestBuildSynAckOptions(t *testing.T) {
	got := buildSynAckOptions(1460, 8, true)
	if len(got) != 8 || got[0] != tcpOptMSS || got[4] != tcpOptNOP || got[5] != tcpOptWndScale {
		t.Fatalf("SYN-ACK options with scale = %#v", got)
	}
	if mss := binary.BigEndian.Uint16(got[2:4]); mss != 1460 {
		t.Fatalf("MSS = %d, want 1460", mss)
	}
	if got[7] != 8 {
		t.Fatalf("window scale byte = %d, want 8", got[7])
	}

	got = buildSynAckOptions(1200, 0, false)
	if len(got) != 4 || got[0] != tcpOptMSS || binary.BigEndian.Uint16(got[2:4]) != 1200 {
		t.Fatalf("SYN-ACK options without scale = %#v", got)
	}
}

func TestHostAccessDisabledAllowsInternalTCPListeners(t *testing.T) {
	ns := New(nil)
	ns.SetHostAccessEnabled(false)
	ln, err := ns.ListenInternal("tcp", ":10777")
	if err != nil {
		t.Fatalf("ListenInternal: %v", err)
	}
	defer ln.Close()

	guestIP := net.IPv4(10, 42, 0, 2)
	hostIP := net.IP(ns.hostIPv4[:])
	syn := buildTestTCPSegment(40000, 10777, 1000, 0, tcpFlagSYN, guestIP, hostIP)
	if err := ns.handleTCP(ipv4Header{
		src: guestIP,
		dst: hostIP,
	}, syn); err != nil {
		t.Fatalf("handleTCP SYN to internal listener: %v", err)
	}

	ns.tcpMu.Lock()
	defer ns.tcpMu.Unlock()
	if len(ns.tcpConns) != 1 {
		t.Fatalf("tcp connections = %d, want internal listener connection", len(ns.tcpConns))
	}
}

func buildTestTCPSegment(srcPort, dstPort uint16, seq, ack uint32, flags uint8, srcIP, dstIP net.IP) []byte {
	seg := make([]byte, tcpHeaderLen)
	binary.BigEndian.PutUint16(seg[0:2], srcPort)
	binary.BigEndian.PutUint16(seg[2:4], dstPort)
	binary.BigEndian.PutUint32(seg[4:8], seq)
	binary.BigEndian.PutUint32(seg[8:12], ack)
	seg[12] = byte(tcpHeaderLen/4) << 4
	seg[13] = flags
	binary.BigEndian.PutUint16(seg[14:16], 65535)
	binary.BigEndian.PutUint16(seg[16:18], tcpChecksum(srcIP, dstIP, seg))
	return seg
}
