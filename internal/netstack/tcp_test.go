package netstack

import (
	"encoding/binary"
	"testing"
)

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
