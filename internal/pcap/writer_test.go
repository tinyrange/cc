package pcap

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
	"time"
)

func TestWriterProducesExpectedStream(t *testing.T) {
	var buf bytes.Buffer
	writer := NewWriter(&buf)

	const snapLen = 512
	if err := writer.WriteFileHeader(snapLen, LinkTypeEthernet); err != nil {
		t.Fatalf("write header: %v", err)
	}

	ts := time.Unix(1_700_000_000, 250_000_000)
	payload := []byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee}
	info := CaptureInfo{
		Timestamp:     ts,
		CaptureLength: len(payload),
		Length:        len(payload),
	}
	if err := writer.WritePacket(info, payload); err != nil {
		t.Fatalf("write packet: %v", err)
	}

	got := buf.Bytes()
	wantLen := 24 + 16 + len(payload)
	if len(got) != wantLen {
		t.Fatalf("expected %d bytes, got %d", wantLen, len(got))
	}

	global := got[:24]
	if magic := binary.LittleEndian.Uint32(global[0:4]); magic != 0xa1b2c3d4 {
		t.Fatalf("unexpected magic %#x", magic)
	}
	if major := binary.LittleEndian.Uint16(global[4:6]); major != 2 {
		t.Fatalf("unexpected major version %d", major)
	}
	if minor := binary.LittleEndian.Uint16(global[6:8]); minor != 4 {
		t.Fatalf("unexpected minor version %d", minor)
	}
	if zone := binary.LittleEndian.Uint32(global[8:12]); zone != 0 {
		t.Fatalf("unexpected timezone offset %d", zone)
	}
	if sig := binary.LittleEndian.Uint32(global[12:16]); sig != 0 {
		t.Fatalf("unexpected sigfigs %d", sig)
	}
	if snap := binary.LittleEndian.Uint32(global[16:20]); snap != snapLen {
		t.Fatalf("unexpected snaplen %d", snap)
	}
	if link := binary.LittleEndian.Uint32(global[20:24]); link != LinkTypeEthernet {
		t.Fatalf("unexpected linktype %d", link)
	}

	record := got[24 : 24+16]
	if sec := binary.LittleEndian.Uint32(record[0:4]); sec != uint32(ts.Unix()) {
		t.Fatalf("unexpected timestamp seconds %d", sec)
	}
	if usec := binary.LittleEndian.Uint32(record[4:8]); usec != uint32(ts.Nanosecond()/1_000) {
		t.Fatalf("unexpected timestamp microseconds %d", usec)
	}
	if capLen := binary.LittleEndian.Uint32(record[8:12]); capLen != uint32(len(payload)) {
		t.Fatalf("unexpected caplen %d", capLen)
	}
	if origLen := binary.LittleEndian.Uint32(record[12:16]); origLen != uint32(len(payload)) {
		t.Fatalf("unexpected origlen %d", origLen)
	}

	data := got[24+16:]
	if !bytes.Equal(data, payload) {
		t.Fatalf("payload mismatch: got %x, want %x", data, payload)
	}
}

func TestWritePacketRequiresHeader(t *testing.T) {
	writer := NewWriter(new(bytes.Buffer))
	err := writer.WritePacket(CaptureInfo{CaptureLength: 1, Length: 1}, []byte{0x01})
	if !errors.Is(err, ErrHeaderNotWritten) {
		t.Fatalf("expected ErrHeaderNotWritten, got %v", err)
	}
}

func TestSnapLengthEnforced(t *testing.T) {
	var buf bytes.Buffer
	writer := NewWriter(&buf)
	if err := writer.WriteFileHeader(4, LinkTypeEthernet); err != nil {
		t.Fatalf("write header: %v", err)
	}

	payload := []byte{0, 1, 2, 3, 4}
	err := writer.WritePacket(CaptureInfo{
		CaptureLength: len(payload),
		Length:        len(payload),
	}, payload)
	if err == nil {
		t.Fatalf("expected snaplen enforcement error")
	}
}
