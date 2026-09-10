package ramfb

import (
	"encoding/binary"
	"fmt"
	"math/bits"
	"testing"
)

type testMemory []byte

func (m testMemory) ReadPhysical(a uint64, b []byte) error {
	if a > uint64(len(m)) || uint64(len(b)) > uint64(len(m))-a {
		return fmt.Errorf("outside memory")
	}
	copy(b, m[a:])
	return nil
}
func (m testMemory) WritePhysical(a uint64, b []byte) error {
	if a > uint64(len(m)) || uint64(len(b)) > uint64(len(m))-a {
		return fmt.Errorf("outside memory")
	}
	copy(m[a:], b)
	return nil
}

func TestDriverDMAAndPaddedFramebuffer(t *testing.T) {
	mem := make(testMemory, 4096)
	d, err := New(0x09020000, mem)
	if err != nil {
		t.Fatal(err)
	}
	sig, err := d.Read(0x09020010, 8)
	if err != nil || bits.ReverseBytes64(sig) != 0x51454d5520434647 {
		t.Fatalf("signature %#x %v", sig, err)
	}
	dma := func(control uint32, size uint32, address uint64) uint32 {
		t.Helper()
		binary.BigEndian.PutUint32(mem[16:], control)
		binary.BigEndian.PutUint32(mem[20:], size)
		binary.BigEndian.PutUint64(mem[24:], address)
		if err := d.Write(0x09020010, 8, bits.ReverseBytes64(16)); err != nil {
			t.Fatal(err)
		}
		return binary.BigEndian.Uint32(mem[16:])
	}
	if status := dma(0x19<<16|10, 4, 128); status != 0 || binary.BigEndian.Uint32(mem[128:]) != 1 {
		t.Fatal("directory count")
	}
	if status := dma(2, 64, 128); status != 0 || string(mem[136:145]) != "etc/ramfb" || binary.BigEndian.Uint16(mem[132:]) != 0x20 {
		t.Fatal("directory entry")
	}
	binary.BigEndian.PutUint64(mem[256:], 1024)
	binary.BigEndian.PutUint32(mem[264:], 0x34325258)
	binary.BigEndian.PutUint32(mem[272:], 2)
	binary.BigEndian.PutUint32(mem[276:], 2)
	binary.BigEndian.PutUint32(mem[280:], 12)
	if status := dma(0x20<<16|24, 28, 256); status != 0 {
		t.Fatal("configuration")
	}
	copy(mem[1024:], []byte{1, 2, 3, 0, 4, 5, 6, 0})
	copy(mem[1036:], []byte{7, 8, 9, 0, 10, 11, 12, 0})
	frame, err := d.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if frame.Width != 2 || frame.Height != 2 || len(frame.Pixels) != 16 || frame.Pixels[8] != 7 {
		t.Fatalf("incorrect padded frame: %+v", frame)
	}
	if status := dma(0x19<<16|24, 4, 128); status != 1 {
		t.Fatal("accepted write to read-only directory")
	}
	if status := dma(0x20<<16|10, 28, 4090); status != 1 {
		t.Fatal("accepted invalid DMA destination")
	}
}
