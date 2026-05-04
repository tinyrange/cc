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
		NumCPUs:    5,
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
	if got := binary.LittleEndian.Uint64(mem[0x90000+zeroPageACPIRSDPAddr:]); got != acpiRSDPAddress {
		t.Fatalf("ACPI RSDP boot param = %#x, want %#x", got, acpiRSDPAddress)
	}
	if got := string(mem[acpiRSDPAddress : acpiRSDPAddress+8]); got != "RSD PTR " {
		t.Fatalf("RSDP signature = %q", got)
	}
	if got := string(mem[acpiTableAddress : acpiTableAddress+4]); got != "FACS" {
		t.Fatalf("FACS signature = %q", got)
	}
	if got := string(mem[acpiTableAddress+0x40 : acpiTableAddress+0x44]); got != "DSDT" {
		t.Fatalf("DSDT signature = %q", got)
	}
	if got := string(mem[acpiTableAddress+0x70 : acpiTableAddress+0x74]); got != "FACP" {
		t.Fatalf("FADT signature = %q", got)
	}
	fadtBody := mem[acpiTableAddress+0x70+36:]
	if got := binary.LittleEndian.Uint32(fadtBody[20:]); got != 0x400 {
		t.Fatalf("FADT PM1a event block = %#x, want 0x400", got)
	}
	if got := binary.LittleEndian.Uint32(fadtBody[28:]); got != 0x404 {
		t.Fatalf("FADT PM1a control block = %#x, want 0x404", got)
	}
	if got := fadtBody[52]; got != 4 {
		t.Fatalf("FADT PM1 event length = %d, want 4", got)
	}
	if got := fadtBody[53]; got != 2 {
		t.Fatalf("FADT PM1 control length = %d, want 2", got)
	}
	if got := string(mem[0x000f0000 : 0x000f0000+4]); got != "_MP_" {
		t.Fatalf("MP floating pointer signature = %q", got)
	}
}

func TestPrepareBootAcceptsHighMemoryE820(t *testing.T) {
	mem := make([]byte, 3<<20)
	plan, err := PrepareBoot(mem, testBzImage(), BootOptions{
		MemorySize: 8 << 30,
		Cmdline:    "console=ttyS0 rdinit=/init",
		E820: []E820Entry{
			{Addr: 0, Size: 0x9f000, Type: 1},
			{Addr: 0x9f000, Size: 0x61000, Type: 2},
			{Addr: 0x100000, Size: (3 << 20) - 0x100000, Type: 1},
			{Addr: 4 << 30, Size: 5 << 30, Type: 1},
		},
	})
	if err != nil {
		t.Fatalf("PrepareBoot() error = %v", err)
	}
	if plan.EntryGPA == 0 || plan.ZeroPageGPA == 0 || plan.StackTopGPA == 0 {
		t.Fatalf("plan has zero fields: %+v", plan)
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
