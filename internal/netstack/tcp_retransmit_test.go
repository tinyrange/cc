package netstack

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"
)

func TestRetransmitCoalescesContiguousSmallWrites(t *testing.T) {
	b := newTCPSendBuffer(65536)
	defer b.clear()
	// SMB direct TCP splits each response into a four-byte frame length and
	// an 80-byte body. A lost burst must not take one exponential RTO per
	// tiny write when an entire MTU can be retransmitted in one segment.
	seq := uint32(0xfffffff0)
	var want []byte
	for i := 0; i < 20; i++ {
		p := bytes.Repeat([]byte{byte(i)}, 84)
		b.append(tcpSendSegment{seqStart: seq, seqEnd: seq + 84, payload: retainPayload(p)})
		seq += 84
		want = append(want, p...)
	}
	seg, count, ok := b.oldestCoalesced(1460)
	if !ok || count != 17 || len(seg.payload) != 17*84 || seg.seqEnd != uint32(17*84-16) {
		t.Fatalf("snapshot: count=%d ok=%v range=%x..%x length=%d", count, ok, seg.seqStart, seg.seqEnd, len(seg.payload))
	}
	if !bytes.Equal(seg.payload, want[:17*84]) {
		t.Fatal("coalesced bytes differ")
	}
	// A concurrent cumulative ACK frees the original buffers; the outgoing
	// snapshot must remain independent, including across sequence wrap.
	b.ack(seq)
	if !bytes.Equal(seg.payload, want[:17*84]) {
		t.Fatal("ACK changed snapshot")
	}
}

func TestRetransmitSnapshotStopsAtGapAndOwnsPayload(t *testing.T) {
	backing := []byte("first")
	b := newTCPSendBuffer(100)
	b.append(tcpSendSegment{seqStart: 10, seqEnd: 15, payload: backing})
	b.append(tcpSendSegment{seqStart: 20, seqEnd: 24, payload: []byte("last")})
	seg, n, ok := b.oldestCoalesced(100)
	first, firstOK := b.oldest()
	if !ok || !firstOK || n != 1 || seg.seqEnd != 15 {
		t.Fatal("crossed sequence gap")
	}
	copy(backing, "reuse")
	if string(seg.payload) != "first" || string(first.payload) != "first" {
		t.Fatal("borrowed retransmission buffer")
	}
	b.clear()
}

// Losing a complete pipelined response flight must require only the initial
// timeout. Partial cumulative ACKs clock the remaining repairs, including when
// TCP sequence numbers wrap. Previously each 4/80-byte write (then each MSS)
// needed a separate exponentially backed-off timeout and Windows reset SMB.
func TestTCPRecoveryRepairsLostFlightOnACKs(t *testing.T) {
	h := newBenchmarkHarness(t)
	frames := make(chan []byte, 1024)
	h.nic.AttachVirtioBackend(func(frame []byte) error {
		frames <- append([]byte(nil), frame...)
		return nil
	})
	key := tcpFourTuple{srcIP: [4]byte{10, 42, 0, 2}, dstIP: [4]byte{10, 42, 0, 1}, srcPort: 49152, dstPort: 445}
	c := newTCPConn(h.stack, nil, key, 100, 0xffff, key.dstIP, nil)
	c.state = tcpStateEstablished
	defer c.Close()
	c.hostSeq, c.sendAcked = 0xfffff000, 0xfffff000
	start := c.hostSeq
	var want []byte
	for i := 0; i < 128; i++ {
		for _, size := range []int{4, 80} {
			p := bytes.Repeat([]byte{byte(i)}, size)
			c.sendBuf.append(tcpSendSegment{seqStart: c.hostSeq, seqEnd: c.hostSeq + uint32(size), payload: retainPayload(p), sentAt: time.Now().Add(-time.Minute)})
			c.hostSeq += uint32(size)
			want = append(want, p...)
		}
	}
	c.onRetxTimeout()
	c.stopRetxTimer()
	var got []byte
	for len(got) < len(want) {
		var frame []byte
		select {
		case frame = <-frames:
		default:
			t.Fatalf("recovery stalled after %d/%d bytes; waiting for another timeout", len(got), len(want))
		}
		ip := frame[14:]
		tcp := ip[int(ip[0]&15)*4:]
		payload := tcp[int(tcp[12]>>4)*4:]
		if len(payload) == 0 || len(payload) > defaultMSS {
			t.Fatalf("invalid repair size %d", len(payload))
		}
		seq := binary.BigEndian.Uint32(tcp[4:8])
		if seq != start+uint32(len(got)) {
			t.Fatalf("repair sequence %x, want %x", seq, start+uint32(len(got)))
		}
		got = append(got, payload...)
		if err := c.handleSegment(ipv4Header{}, tcpHeader{seq: c.guestSeq, ack: start + uint32(len(got)), flags: tcpFlagACK, window: 0xffff}); err != nil {
			t.Fatal(err)
		}
		c.stopRetxTimer()
	}
	if !bytes.Equal(got, want) {
		t.Fatal("recovered payload differs")
	}
	if c.recoveryActive || c.sendBuf.inFlight() != 0 || c.sendAcked != c.hostSeq {
		t.Fatal("recovery did not finish")
	}
	if c.retxCount != 1 {
		t.Fatalf("needed %d timeouts, want one", c.retxCount)
	}
	select {
	case <-frames:
		t.Fatal("extra repair after final ACK")
	default:
	}
}
