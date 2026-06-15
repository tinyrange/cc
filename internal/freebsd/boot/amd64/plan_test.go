package amd64

import (
	"encoding/binary"
	"testing"
)

func TestBuildMetadataContainsSMAP(t *testing.T) {
	metadata, _, err := buildMetadata(0x200000, 0x2000000, 0x2000000, rbSerial, nil, []byte("test-entropy"), defaultSMAP(1024<<20))
	if err != nil {
		t.Fatal(err)
	}
	foundType := false
	foundSMAP := false
	foundEntropy := false
	foundPlatformEntropy := false
	for off := 0; off+8 <= len(metadata); {
		typ := binary.LittleEndian.Uint32(metadata[off:])
		size := binary.LittleEndian.Uint32(metadata[off+4:])
		value := metadata[off+8 : off+8+int(size)]
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
		if typ == modInfoType && string(value[:len(value)-1]) == freeBSDBootEntropyType {
			foundEntropy = true
		}
		if typ == modInfoType && string(value[:len(value)-1]) == freeBSDPlatformEntropyType {
			foundPlatformEntropy = true
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
	if !foundEntropy {
		t.Fatalf("metadata does not contain boot entropy preload")
	}
	if !foundPlatformEntropy {
		t.Fatalf("metadata does not contain platform entropy preload")
	}
}

func TestBuildBootFADTMarksNoVGA(t *testing.T) {
	const fadtBootArchNoVGA = 1 << 2
	body := buildBootFADT(0x1000, 0x2000)
	flags := binary.LittleEndian.Uint16(body[73:])
	if flags&fadtBootArchNoVGA == 0 {
		t.Fatalf("FADT boot architecture flags = %#x, want NO_VGA", flags)
	}
}
