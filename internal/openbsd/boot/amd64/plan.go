package amd64

import (
	"bytes"
	"compress/gzip"
	"debug/elf"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	defaultBootArgsGPA = 0x00002000
	defaultStackGPA    = 0x00007000

	legacyKernelEntry = 0xffffffff81001000
	legacyEntryMask   = 0x0fffffff

	pageSize       = 4096
	lowMemoryLimit = 3 << 30
	highMemoryBase = 4 << 30

	bootargAPIVER  = bapivVector | bapivEnv | bapivBMemmap
	bapivVector    = 0x00000002
	bapivEnv       = 0x00000004
	bapivBMemmap   = 0x00000008
	bootargEnd     = 0xffffffff
	bootargMemmap  = 0
	bootargConsdev = 5

	biosMapEnd  = 0
	biosMapFree = 1
	biosMapRes  = 2

	rbSercons = 0x02000

	com1Base = 0x3f8
)

type BootOptions struct {
	MemorySize  uint64
	BootArgsGPA uint64
	StackGPA    uint64
	Howto       uint32
}

type BootPlan struct {
	EntryGPA    uint64
	StackGPA    uint64
	BootArgsGPA uint64
	BootArgsLen uint32
	KernelEnd   uint64
}

type memmapEntry struct {
	addr uint64
	size uint64
	typ  uint32
}

func PrepareBoot(memory []byte, kernelFile []byte, opts BootOptions) (*BootPlan, error) {
	if len(memory) == 0 {
		return nil, errors.New("guest memory is empty")
	}
	if len(kernelFile) == 0 {
		return nil, errors.New("kernel file is empty")
	}
	if opts.MemorySize == 0 {
		opts.MemorySize = uint64(len(memory))
	}
	if opts.BootArgsGPA == 0 {
		opts.BootArgsGPA = defaultBootArgsGPA
	}
	if opts.StackGPA == 0 {
		opts.StackGPA = defaultStackGPA
	}

	kernel, err := decompressKernel(kernelFile)
	if err != nil {
		return nil, err
	}
	entry, kernelEnd, err := loadELF(memory, kernel)
	if err != nil {
		return nil, err
	}
	entryGPA, err := openBSDEntryGPA(entry)
	if err != nil {
		return nil, err
	}

	bootArgs, err := buildBootArgs(defaultMemmap(opts.MemorySize), serialConsdev())
	if err != nil {
		return nil, err
	}
	if err := writeAt(memory, opts.BootArgsGPA, bootArgs); err != nil {
		return nil, fmt.Errorf("write bootargs: %w", err)
	}

	stack := buildLegacyStack(opts.Howto|rbSercons, 0, bootargAPIVER, kernelEnd, 640, uint32(len(bootArgs)), uint32(opts.BootArgsGPA))
	stackGPA := opts.StackGPA - uint64(len(stack))
	if stackGPA >= opts.StackGPA {
		return nil, errors.New("invalid stack placement")
	}
	if err := writeAt(memory, stackGPA, stack); err != nil {
		return nil, fmt.Errorf("write boot stack: %w", err)
	}

	return &BootPlan{
		EntryGPA:    entryGPA,
		StackGPA:    stackGPA,
		BootArgsGPA: opts.BootArgsGPA,
		BootArgsLen: uint32(len(bootArgs)),
		KernelEnd:   kernelEnd,
	}, nil
}

func decompressKernel(kernelFile []byte) ([]byte, error) {
	if len(kernelFile) < 2 || kernelFile[0] != 0x1f || kernelFile[1] != 0x8b {
		return kernelFile, nil
	}
	r, err := gzip.NewReader(bytes.NewReader(kernelFile))
	if err != nil {
		return nil, fmt.Errorf("open gzip kernel: %w", err)
	}
	defer r.Close()
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("decompress kernel: %w", err)
	}
	return data, nil
}

func loadELF(memory []byte, kernel []byte) (uint64, uint64, error) {
	f, err := elf.NewFile(bytes.NewReader(kernel))
	if err != nil {
		return 0, 0, fmt.Errorf("parse OpenBSD kernel ELF: %w", err)
	}
	defer f.Close()
	if f.FileHeader.Class != elf.ELFCLASS64 || f.FileHeader.Data != elf.ELFDATA2LSB || f.FileHeader.Machine != elf.EM_X86_64 {
		return 0, 0, fmt.Errorf("unsupported OpenBSD kernel ELF class=%v data=%v machine=%v", f.FileHeader.Class, f.FileHeader.Data, f.FileHeader.Machine)
	}
	var kernelEnd uint64
	for _, prog := range f.Progs {
		if prog.Type != elf.PT_LOAD {
			continue
		}
		if prog.Paddr == 0 || prog.Memsz == 0 {
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
		return 0, 0, errors.New("OpenBSD kernel has no loadable segments")
	}
	return f.Entry, kernelEnd, nil
}

func openBSDEntryGPA(entry uint64) (uint64, error) {
	if entry == legacyKernelEntry {
		return entry & legacyEntryMask, nil
	}
	if entry < legacyKernelEntry && entry >= 0x100000 {
		return entry, nil
	}
	return 0, fmt.Errorf("unsupported OpenBSD kernel entry %#x", entry)
}

func defaultMemmap(memorySize uint64) []memmapEntry {
	entries := []memmapEntry{
		{addr: 0, size: 0x9fc00, typ: biosMapFree},
		{addr: 0x9fc00, size: 0x60400, typ: biosMapRes},
	}
	lowSize := memorySize
	if lowSize > lowMemoryLimit {
		lowSize = lowMemoryLimit
	}
	if lowSize > 0x100000 {
		entries = append(entries, memmapEntry{addr: 0x100000, size: lowSize - 0x100000, typ: biosMapFree})
	}
	if memorySize > lowMemoryLimit {
		entries = append(entries, memmapEntry{addr: lowMemoryLimit, size: highMemoryBase - lowMemoryLimit, typ: biosMapRes})
		entries = append(entries, memmapEntry{addr: highMemoryBase, size: memorySize - lowMemoryLimit, typ: biosMapFree})
	}
	entries = append(entries, memmapEntry{typ: biosMapEnd})
	return entries
}

func buildBootArgs(memmap []memmapEntry, consdev []byte) ([]byte, error) {
	var args []bootArg
	mem := make([]byte, 0, len(memmap)*20)
	for _, entry := range memmap {
		mem = binary.LittleEndian.AppendUint64(mem, entry.addr)
		mem = binary.LittleEndian.AppendUint64(mem, entry.size)
		mem = binary.LittleEndian.AppendUint32(mem, entry.typ)
	}
	args = append(args, bootArg{typ: bootargMemmap, data: mem})
	args = append(args, bootArg{typ: bootargConsdev, data: consdev})

	var total int
	for _, arg := range args {
		total += 12 + len(arg.data)
	}
	total += 12
	if total >= pageSize {
		return nil, fmt.Errorf("OpenBSD bootargs too large: %d", total)
	}
	out := make([]byte, 0, total)
	for _, arg := range args {
		out = binary.LittleEndian.AppendUint32(out, uint32(arg.typ))
		out = binary.LittleEndian.AppendUint32(out, uint32(12+len(arg.data)))
		out = binary.LittleEndian.AppendUint32(out, 0)
		out = append(out, arg.data...)
	}
	out = binary.LittleEndian.AppendUint32(out, bootargEnd)
	out = binary.LittleEndian.AppendUint32(out, 12)
	out = binary.LittleEndian.AppendUint32(out, 0)
	return out, nil
}

type bootArg struct {
	typ  int32
	data []byte
}

func serialConsdev() []byte {
	const (
		comMajor = 8
		comMinor = 0
	)
	dev := uint32(((comMajor & 0xff) << 8) | (comMinor & 0xff))
	out := make([]byte, 32)
	binary.LittleEndian.PutUint32(out[0:4], dev)
	binary.LittleEndian.PutUint32(out[4:8], 115200)
	binary.LittleEndian.PutUint64(out[8:16], com1Base)
	binary.LittleEndian.PutUint32(out[16:20], 0)
	binary.LittleEndian.PutUint32(out[20:24], 0)
	binary.LittleEndian.PutUint32(out[24:28], 1)
	binary.LittleEndian.PutUint32(out[28:32], 0)
	return out
}

func buildLegacyStack(howto, bootdev, bootapiver uint32, esym uint64, cnvmem uint32, bootArgsLen uint32, bootArgsGPA uint32) []byte {
	stack := make([]byte, 9*4)
	values := []uint32{
		0,
		howto,
		bootdev,
		bootapiver,
		uint32(esym),
		0,
		cnvmem,
		bootArgsLen,
		bootArgsGPA,
	}
	for i, value := range values {
		binary.LittleEndian.PutUint32(stack[i*4:], value)
	}
	return stack
}

func writeAt(memory []byte, gpa uint64, data []byte) error {
	if gpa > uint64(len(memory)) || uint64(len(data)) > uint64(len(memory))-gpa {
		return fmt.Errorf("write gpa=%#x len=%#x outside guest memory %#x", gpa, len(data), len(memory))
	}
	copy(memory[gpa:], data)
	return nil
}

func alignUp(value, align uint64) uint64 {
	if align == 0 {
		return value
	}
	return (value + align - 1) &^ (align - 1)
}
