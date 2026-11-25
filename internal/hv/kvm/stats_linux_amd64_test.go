//go:build linux && amd64

package kvm

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestReadScalarStats(t *testing.T) {
	image := make([]byte, 128)
	put32 := func(offset int, value uint32) { binary.LittleEndian.PutUint32(image[offset:], value) }
	put32(4, 8)
	put32(8, 2)
	put32(16, 32)
	put32(20, 104)
	binary.LittleEndian.PutUint16(image[38:], 1)
	put32(40, 16)
	copy(image[48:], "exits")
	binary.LittleEndian.PutUint16(image[62:], 2)
	copy(image[72:], "hist")
	binary.LittleEndian.PutUint64(image[120:], 42)
	got := make(map[string]uint64)
	if err := readScalarStats(bytes.NewReader(image), "vcpu.", got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got["vcpu.exits"] != 42 {
		t.Fatalf("unexpected scalar statistics: %v", got)
	}
	put32(40, 1<<30)
	if err := readScalarStats(bytes.NewReader(image), "", got); err == nil {
		t.Fatal("accepted invalid statistics bounds")
	}
}
