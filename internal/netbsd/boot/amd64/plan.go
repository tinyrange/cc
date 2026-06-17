package amd64

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	defaultBootInfoGPA = 0x02000000
	defaultStackTopGPA = 0x00070000

	pageSize = 4096

	kernBase = 0xffffffff80000000

	rebootAutoBoot = 0

	btinfoConsole    = 6
	btinfoRootDevice = 1
	btinfoMemmap     = 9

	bootInfoMaxSize = 4096

	memmapTypeRAM      = 1
	memmapTypeReserved = 2

	lowMemoryLimit = 3 << 30
	highMemoryBase = 4 << 30
)

type BootOptions struct {
	MemorySize  uint64
	BootInfoGPA uint64
	StackTopGPA uint64
	Howto       uint32
}

type BootPlan struct {
	EntryGPA     uint64
	StackGPA     uint64
	BootInfoGPA  uint64
	BootInfoLen  uint32
	KernelEndGPA uint64
	ESymGPA      uint32
	BaseMemKB    uint32
	ExtMemKB     uint32
	Howto        uint32
}

type memmapEntry struct {
	addr uint64
	size uint64
	typ  uint32
}

func PrepareBoot(memory []byte, kernel []byte, opts BootOptions) (*BootPlan, error) {
	if len(memory) == 0 {
		return nil, errors.New("guest memory is empty")
	}
	if len(kernel) == 0 {
		return nil, errors.New("kernel file is empty")
	}
	if opts.MemorySize == 0 {
		opts.MemorySize = uint64(len(memory))
	}
	if opts.BootInfoGPA == 0 {
		opts.BootInfoGPA = defaultBootInfoGPA
	}
	if opts.StackTopGPA == 0 {
		opts.StackTopGPA = defaultStackTopGPA
	}
	entry, kernelEnd, err := loadELF(memory, kernel)
	if err != nil {
		return nil, err
	}
	if opts.BootInfoGPA < kernelEnd {
		opts.BootInfoGPA = alignUp(kernelEnd, pageSize)
	}

	memmap := defaultMemmap(opts.MemorySize)
	bootInfo := buildBootInfo(opts.BootInfoGPA, serialConsole(), rootDevice("ld0a"), memmapInfo(memmap))
	if len(bootInfo) > bootInfoMaxSize {
		return nil, fmt.Errorf("NetBSD bootinfo size %d exceeds %d", len(bootInfo), bootInfoMaxSize)
	}
	if err := writeAt(memory, opts.BootInfoGPA, bootInfo); err != nil {
		return nil, fmt.Errorf("write NetBSD bootinfo: %w", err)
	}
	if _, err := installBootACPI(memory, 1); err != nil {
		return nil, fmt.Errorf("install NetBSD boot ACPI: %w", err)
	}

	baseMemKB, extMemKB := memoryKB(opts.MemorySize)
	stack, stackGPA, err := buildStack(opts.StackTopGPA, []uint32{
		opts.Howto,
		0,
		uint32(opts.BootInfoGPA),
		uint32(kernelEnd),
		extMemKB,
		baseMemKB,
	})
	if err != nil {
		return nil, err
	}
	if err := writeAt(memory, stackGPA, stack); err != nil {
		return nil, fmt.Errorf("write NetBSD boot stack: %w", err)
	}

	return &BootPlan{
		EntryGPA:     entry,
		StackGPA:     stackGPA,
		BootInfoGPA:  opts.BootInfoGPA,
		BootInfoLen:  uint32(len(bootInfo)),
		KernelEndGPA: kernelEnd,
		ESymGPA:      uint32(kernelEnd),
		BaseMemKB:    baseMemKB,
		ExtMemKB:     extMemKB,
		Howto:        opts.Howto,
	}, nil
}

func loadELF(memory []byte, kernel []byte) (uint64, uint64, error) {
	f, err := elf.NewFile(bytes.NewReader(kernel))
	if err != nil {
		return 0, 0, fmt.Errorf("parse NetBSD kernel ELF: %w", err)
	}
	defer f.Close()
	if f.FileHeader.Class != elf.ELFCLASS64 || f.FileHeader.Data != elf.ELFDATA2LSB || f.FileHeader.Machine != elf.EM_X86_64 {
		return 0, 0, fmt.Errorf("unsupported NetBSD kernel ELF class=%v data=%v machine=%v", f.FileHeader.Class, f.FileHeader.Data, f.FileHeader.Machine)
	}
	entry, err := physicalEntry(f.Entry)
	if err != nil {
		return 0, 0, err
	}
	var kernelEnd uint64
	for _, prog := range f.Progs {
		if prog.Type != elf.PT_LOAD || prog.Memsz == 0 {
			continue
		}
		if prog.Paddr+prog.Memsz > uint64(len(memory)) {
			return 0, 0, fmt.Errorf("kernel segment paddr=%#x memsz=%#x exceeds guest memory %#x", prog.Paddr, prog.Memsz, len(memory))
		}
		seg := memory[prog.Paddr : prog.Paddr+prog.Memsz]
		clear(seg)
		if prog.Filesz != 0 {
			if _, err := io.ReadFull(prog.Open(), seg[:prog.Filesz]); err != nil {
				return 0, 0, fmt.Errorf("read kernel segment at %#x: %w", prog.Paddr, err)
			}
		}
		if end := alignUp(prog.Paddr+prog.Memsz, pageSize); end > kernelEnd {
			kernelEnd = end
		}
	}
	if kernelEnd == 0 {
		return 0, 0, errors.New("NetBSD kernel has no loadable segments")
	}
	if kernelEnd > uint64(^uint32(0)) {
		return 0, 0, fmt.Errorf("NetBSD loaded image end %#x exceeds native boot ABI", kernelEnd)
	}
	return entry, kernelEnd, nil
}

func physicalEntry(entry uint64) (uint64, error) {
	if entry >= kernBase {
		entry -= kernBase
	}
	if entry > uint64(^uint32(0)) {
		return 0, fmt.Errorf("NetBSD entry %#x exceeds native boot ABI", entry)
	}
	return entry, nil
}

func buildBootInfo(base uint64, entries ...[]byte) []byte {
	total := 4 + len(entries)*4
	for _, entry := range entries {
		total += align4(len(entry))
	}
	out := make([]byte, total)
	binary.LittleEndian.PutUint32(out[0:4], uint32(len(entries)))
	tableOff := 4
	entryOff := 4 + len(entries)*4
	for i, entry := range entries {
		binary.LittleEndian.PutUint32(out[tableOff+i*4:tableOff+i*4+4], uint32(base)+uint32(entryOff))
		copy(out[entryOff:], entry)
		entryOff += align4(len(entry))
	}
	return out
}

func serialConsole() []byte {
	out := make([]byte, 32)
	binary.LittleEndian.PutUint32(out[0:4], uint32(len(out)))
	binary.LittleEndian.PutUint32(out[4:8], btinfoConsole)
	copy(out[8:24], "com\x00")
	binary.LittleEndian.PutUint32(out[24:28], 0x3f8)
	binary.LittleEndian.PutUint32(out[28:32], 115200)
	return out
}

func rootDevice(name string) []byte {
	out := make([]byte, 24)
	binary.LittleEndian.PutUint32(out[0:4], uint32(len(out)))
	binary.LittleEndian.PutUint32(out[4:8], btinfoRootDevice)
	copy(out[8:24], name)
	return out
}

func memmapInfo(memmap []memmapEntry) []byte {
	out := make([]byte, 12+len(memmap)*20)
	binary.LittleEndian.PutUint32(out[0:4], uint32(len(out)))
	binary.LittleEndian.PutUint32(out[4:8], btinfoMemmap)
	binary.LittleEndian.PutUint32(out[8:12], uint32(len(memmap)))
	for i, entry := range memmap {
		off := 12 + i*20
		binary.LittleEndian.PutUint64(out[off:off+8], entry.addr)
		binary.LittleEndian.PutUint64(out[off+8:off+16], entry.size)
		binary.LittleEndian.PutUint32(out[off+16:off+20], entry.typ)
	}
	return out
}

func defaultMemmap(memorySize uint64) []memmapEntry {
	entries := []memmapEntry{
		{addr: 0, size: 0x9fc00, typ: memmapTypeRAM},
		{addr: 0x9fc00, size: 0x60400, typ: memmapTypeReserved},
	}
	lowSize := memorySize
	if lowSize > lowMemoryLimit {
		lowSize = lowMemoryLimit
	}
	if lowSize > 0x100000 {
		entries = append(entries, memmapEntry{addr: 0x100000, size: lowSize - 0x100000, typ: memmapTypeRAM})
	}
	if memorySize > lowMemoryLimit {
		entries = append(entries, memmapEntry{addr: lowMemoryLimit, size: highMemoryBase - lowMemoryLimit, typ: memmapTypeReserved})
		entries = append(entries, memmapEntry{addr: highMemoryBase, size: memorySize - lowMemoryLimit, typ: memmapTypeRAM})
	}
	return entries
}

func memoryKB(memorySize uint64) (base uint32, ext uint32) {
	baseBytes := uint64(640 << 10)
	if memorySize < baseBytes {
		baseBytes = memorySize
	}
	if memorySize <= 1<<20 {
		return uint32(baseBytes >> 10), 0
	}
	return uint32(baseBytes >> 10), uint32((memorySize - (1 << 20)) >> 10)
}

func buildStack(stackTop uint64, args []uint32) ([]byte, uint64, error) {
	if len(args) == 0 {
		return nil, 0, errors.New("NetBSD boot stack requires arguments")
	}
	size := 4 + len(args)*4
	stackGPA := stackTop - uint64(size)
	if stackGPA >= stackTop {
		return nil, 0, errors.New("invalid NetBSD stack placement")
	}
	out := make([]byte, size)
	for i, arg := range args {
		binary.LittleEndian.PutUint32(out[4+i*4:8+i*4], arg)
	}
	return out, stackGPA, nil
}

func writeAt(memory []byte, addr uint64, data []byte) error {
	if addr+uint64(len(data)) > uint64(len(memory)) {
		return fmt.Errorf("write addr=%#x len=%#x exceeds guest memory %#x", addr, len(data), len(memory))
	}
	copy(memory[addr:addr+uint64(len(data))], data)
	return nil
}

func align4(n int) int {
	return (n + 3) &^ 3
}

func alignUp(value, align uint64) uint64 {
	return (value + align - 1) &^ (align - 1)
}
