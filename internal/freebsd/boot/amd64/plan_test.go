package amd64

import (
	"encoding/binary"
	"testing"
)

func TestBuildMetadataContainsSMAP(t *testing.T) {
	metadata, _, err := buildMetadata(0x200000, 0x2000000, 0x2000000, rbSerial, nil, defaultSMAP(1024<<20))
	if err != nil {
		t.Fatal(err)
	}
	foundType := false
	foundSMAP := false
	for off := 0; off+8 <= len(metadata); {
		typ := binary.LittleEndian.Uint32(metadata[off:])
		size := binary.LittleEndian.Uint32(metadata[off+4:])
		t.Logf("record off=%#x type=%#x size=%d", off, typ, size)
		if typ == 0 && size == 0 {
			break
		}
		if typ == modInfoType {
			foundType = true
		}
		if typ == modInfoMetadata|modInfoMDSMAP {
			foundSMAP = true
		}
		next := off + 8 + int(size)
		for next%wordSize != 0 {
			next++
		}
		off = next
	}
	if !foundType {
		t.Fatalf("metadata does not contain MODINFO_TYPE")
	}
	if !foundSMAP {
		t.Fatalf("metadata does not contain SMAP")
	}
}
