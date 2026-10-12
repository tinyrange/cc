package kernelvm

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// Independent ELF64 section/symbol records; no compiler or host ELF tool.
func symbolFixture() []byte {
	b := make([]byte, 512)
	le := binary.LittleEndian
	copy(b, []byte{0x7f, 'E', 'L', 'F', 2, 1, 1})
	le.PutUint16(b[16:], 2)
	le.PutUint16(b[18:], 62)
	le.PutUint32(b[20:], 1)
	le.PutUint64(b[40:], 64)
	le.PutUint16(b[52:], 64)
	le.PutUint16(b[58:], 64)
	le.PutUint16(b[60:], 4)
	section := func(index int, kind uint32, flags, addr, offset, size uint64, link uint32, entrySize uint64) {
		s := b[64+index*64:]
		le.PutUint32(s[4:], kind)
		le.PutUint64(s[8:], flags)
		le.PutUint64(s[16:], addr)
		le.PutUint64(s[24:], offset)
		le.PutUint64(s[32:], size)
		le.PutUint32(s[40:], link)
		le.PutUint64(s[48:], 8)
		le.PutUint64(s[56:], entrySize)
	}
	section(1, 1, 6, 0x1000, 320, 32, 0, 0)
	section(2, 2, 0, 0, 352, 96, 3, 24)
	section(3, 3, 0, 0, 448, 14, 0, 0)
	copy(b[448:], "\x00foo\x00bar\x00data\x00")
	for i, s := range []struct {
		name    uint32
		address uint64
		section uint16
	}{{1, 0x1000, 1}, {5, 0x1010, 1}, {9, 0x1013, 3}} {
		entry := b[352+(i+1)*24:]
		le.PutUint32(entry, s.name)
		entry[4] = 0x12
		le.PutUint16(entry[6:], s.section)
		le.PutUint64(entry[8:], s.address)
		le.PutUint64(entry[16:], 8)
	}
	return b
}

func TestSymbolAtUsesExecutableSectionAndNearestOriginalAddress(t *testing.T) {
	b := symbolFixture()
	symbol, err := SymbolAt(bytes.NewReader(b), int64(len(b)), 0x1014)
	if err != nil {
		t.Fatal(err)
	}
	if symbol == nil || symbol.Name != "bar" || symbol.Address != 0x1010 || symbol.Offset != 4 || symbol.Size != 8 {
		t.Fatalf("symbol: %+v", symbol)
	}
	symbol, err = SymbolAt(bytes.NewReader(b), int64(len(b)), 0xfff)
	if err != nil || symbol != nil {
		t.Fatalf("unexpected preceding symbol: %+v %v", symbol, err)
	}
	binary.LittleEndian.PutUint64(b[64+2*64+32:], (32<<20)+1)
	if _, err = SymbolAt(bytes.NewReader(b), int64(len(b)), 0x1014); err == nil {
		t.Fatal("accepted oversized symbol table")
	}
}
