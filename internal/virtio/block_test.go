package virtio

import (
	"bytes"
	"encoding/binary"
	"testing"
)

type testGuestMemory []byte

func (m testGuestMemory) ReadIPA(addr uint64, size int) ([]byte, error) {
	out := make([]byte, size)
	copy(out, m[addr:addr+uint64(size)])
	return out, nil
}

func (m testGuestMemory) WriteIPA(addr uint64, data []byte) error {
	copy(m[addr:addr+uint64(len(data))], data)
	return nil
}

type testIRQ struct {
	line  uint32
	level bool
}

func (i *testIRQ) SetIRQ(line uint32, level bool) error {
	i.line = line
	i.level = level
	return nil
}

type testBlockBackend []byte

func (b testBlockBackend) ReadAt(p []byte, off int64) (int, error) {
	return bytes.NewReader(b).ReadAt(p, off)
}

func (b testBlockBackend) WriteAt(p []byte, off int64) (int, error) {
	return copy(b[off:], p), nil
}

func (b testBlockBackend) Size() int64 {
	return int64(len(b))
}

func TestBlockLegacyReadRequest(t *testing.T) {
	mem := make(testGuestMemory, 0x20000)
	backend := make(testBlockBackend, 4096)
	copy(backend[512:], []byte("sector-one"))
	irq := &testIRQ{}
	dev := NewBlock(0, 0x1000, 10, backend)
	dev.Attach(mem, irq)

	if err := dev.WriteLegacy(14, 2, 0); err != nil {
		t.Fatal(err)
	}
	if err := dev.WriteLegacy(8, 4, 0x10); err != nil {
		t.Fatal(err)
	}
	writeBlockRequest(t, mem, 0x10000, blockReqIn, 1, 32)

	if err := dev.WriteLegacy(16, 2, 0); err != nil {
		t.Fatal(err)
	}
	if got := string(bytes.TrimRight(mem[0x3000:0x3020], "\x00")); got != "sector-one" {
		t.Fatalf("read data = %q", got)
	}
	if status := mem[0x4000]; status != blockStatusOK {
		t.Fatalf("status = %d", status)
	}
	if !irq.level || irq.line != 10 {
		t.Fatalf("irq = line %d level %t", irq.line, irq.level)
	}
	if usedIdx := binary.LittleEndian.Uint16(mem[0x11000+2:]); usedIdx != 1 {
		t.Fatalf("used idx = %d", usedIdx)
	}
}

func TestBlockLegacyWriteRequest(t *testing.T) {
	mem := make(testGuestMemory, 0x20000)
	backend := make(testBlockBackend, 4096)
	irq := &testIRQ{}
	dev := NewBlock(0, 0x1000, 10, backend)
	dev.Attach(mem, irq)

	if err := dev.WriteLegacy(14, 2, 0); err != nil {
		t.Fatal(err)
	}
	if err := dev.WriteLegacy(8, 4, 0x10); err != nil {
		t.Fatal(err)
	}
	copy(mem[0x3000:], []byte("guest-write"))
	writeBlockRequest(t, mem, 0x10000, blockReqOut, 2, 32)

	if err := dev.WriteLegacy(16, 2, 0); err != nil {
		t.Fatal(err)
	}
	if got := string(bytes.TrimRight(backend[1024:1056], "\x00")); got != "guest-write" {
		t.Fatalf("backend data = %q", got)
	}
	if status := mem[0x4000]; status != blockStatusOK {
		t.Fatalf("status = %d", status)
	}
}

func writeBlockRequest(t *testing.T, mem testGuestMemory, queueBase uint64, reqType uint32, sector uint64, dataLen uint32) {
	t.Helper()
	descBase := queueBase
	availBase := queueBase + 16*blockQueueSize

	header := make([]byte, 16)
	binary.LittleEndian.PutUint32(header[0:4], reqType)
	binary.LittleEndian.PutUint64(header[8:16], sector)
	copy(mem[0x2000:], header)

	writeDesc(mem, descBase+0*16, 0x2000, 16, descFNext, 1)
	dataFlags := uint16(descFNext)
	if reqType == blockReqIn {
		dataFlags |= descFWrite
	}
	writeDesc(mem, descBase+1*16, 0x3000, dataLen, dataFlags, 2)
	writeDesc(mem, descBase+2*16, 0x4000, 1, descFWrite, 0)

	binary.LittleEndian.PutUint16(mem[availBase+2:], 1)
	binary.LittleEndian.PutUint16(mem[availBase+4:], 0)
}

func writeDesc(mem testGuestMemory, off uint64, addr uint64, length uint32, flags uint16, next uint16) {
	binary.LittleEndian.PutUint64(mem[off+0:], addr)
	binary.LittleEndian.PutUint32(mem[off+8:], length)
	binary.LittleEndian.PutUint16(mem[off+12:], flags)
	binary.LittleEndian.PutUint16(mem[off+14:], next)
}
