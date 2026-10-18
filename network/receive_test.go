package network

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"
	"time"
)

func TestReceiveBackpressureDoesNotAcknowledgeLostBytes(t *testing.T) {
	n := testNetwork(t)
	l, err := n.ListenTCP(445)
	if err != nil {
		t.Fatal(err)
	}
	n.Receive(tcpPacket(40000, 445, 100, 0, 2, nil, hostIP))
	syn := n.Poll()
	if len(syn) < 54 {
		t.Fatal("no handshake")
	}
	hostSeq := binary.BigEndian.Uint32(syn[38:]) + 1
	n.Receive(tcpPacket(40000, 445, 101, hostSeq, 0x10, nil, hostIP))
	c, err := l.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(3 * time.Second))
	payload := bytes.Repeat([]byte{0x5a}, 100)
	seq := uint32(101)
	accepted := 0
	stalled := false
	for i := 0; i < 600; i++ {
		n.Receive(tcpPacket(40000, 445, seq, hostSeq, 0x18, payload, hostIP))
		ack := n.Poll()
		if len(ack) < 54 {
			t.Fatal("missing ACK")
		}
		ackSeq := binary.BigEndian.Uint32(ack[42:])
		if ackSeq == seq {
			stalled = true
			break
		}
		if ackSeq != seq+100 {
			t.Fatal("invalid receive acknowledgement", seq, ackSeq)
		}
		seq = ackSeq
		accepted++
	}
	if !stalled {
		t.Fatal("unbounded receive acceptance")
	}
	// Every acknowledged byte must remain readable, even after overflowing the
	// packet pump. Drain, then retransmit the unacknowledged packet and FIN.
	buf := make([]byte, accepted*100)
	if _, err := io.ReadFull(c, buf); err != nil {
		t.Fatalf("acknowledged bytes lost: %v", err)
	}
	if !bytes.Equal(buf, bytes.Repeat([]byte{0x5a}, len(buf))) {
		t.Fatal("payload corruption")
	}
	n.Receive(tcpPacket(40000, 445, seq, hostSeq, 0x19, []byte("last"), hostIP))
	got := make([]byte, 4)
	if _, err := io.ReadFull(c, got); err != nil || string(got) != "last" {
		t.Fatal("retransmission", string(got), err)
	}
	if _, err := c.Read(got); err != io.EOF {
		t.Fatal("missing final EOF", err)
	}
}
