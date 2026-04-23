package amd64

import (
	"encoding/binary"
	"testing"
)

func TestLoadBzImageParsesHeader(t *testing.T) {
	img := testBzImage()
	kernel, err := LoadBzImage(bytesReaderAt(img), int64(len(img)))
	if err != nil {
		t.Fatalf("LoadBzImage() error = %v", err)
	}
	if kernel.PayloadOffset != 512*(1+4) {
		t.Fatalf("PayloadOffset = %d", kernel.PayloadOffset)
	}
	if kernel.EntryPoint(0x100000) != 0x100200 {
		t.Fatalf("EntryPoint() = %#x", kernel.EntryPoint(0x100000))
	}
}

func TestPrepareBootPlacesZeroPage(t *testing.T) {
	mem := make([]byte, 64<<20)
	initrd := []byte("initrd")
	plan, err := PrepareBoot(mem, testBzImage(), BootOptions{
		MemorySize: uint64(len(mem)),
		Cmdline:    "console=ttyS0 rdinit=/init",
		Initrd:     initrd,
	})
	if err != nil {
		t.Fatalf("PrepareBoot() error = %v", err)
	}
	if plan.EntryGPA == 0 || plan.ZeroPageGPA == 0 || plan.StackTopGPA == 0 {
		t.Fatalf("plan has zero fields: %+v", plan)
	}
	if got := string(mem[0x91000 : 0x91000+len("console=ttyS0")]); got != "console=ttyS0" {
		t.Fatalf("cmdline prefix = %q", got)
	}
	if got := binary.LittleEndian.Uint16(mem[0x90000+setupHeaderBootFlagOffset:]); got != 0xaa55 {
		t.Fatalf("boot flag = %#x", got)
	}
}

func testBzImage() []byte {
	buf := make([]byte, 4096)
	buf[headerLengthOffset] = 0x80
	buf[setupHeaderOffset] = 4
	copy(buf[headerMagicOffset:], []byte(headerMagic))
	binary.LittleEndian.PutUint16(buf[protocolVersionOffset:], 0x020c)
	buf[loadFlagsOffset] = 1
	binary.LittleEndian.PutUint32(buf[initrdAddrMaxOffset:], 0x7fffffff)
	binary.LittleEndian.PutUint32(buf[kernelAlignmentOffset:], 0x200000)
	buf[relocatableKernelOffset] = 1
	binary.LittleEndian.PutUint16(buf[xloadflagsOffset:], 1)
	binary.LittleEndian.PutUint32(buf[cmdlineSizeOffset:], 4096)
	binary.LittleEndian.PutUint32(buf[initSizeOffset:], 4096)
	copy(buf[512*(1+4):], []byte("payload"))
	return buf
}
