package amd64

import (
	"bytes"
	"crypto/rand"
	"debug/elf"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	defaultStackGPA    = 0x00007000
	defaultMetadataGPA = 0x02000000
	defaultPagingGPA   = 0x00090000

	pageSize = 4096
	wordSize = 8

	kernBase  = 0xffffffff80000000
	kernStart = kernBase + 0x200000

	modInfoEnd      = 0x0000
	modInfoName     = 0x0001
	modInfoType     = 0x0002
	modInfoAddr     = 0x0003
	modInfoSize     = 0x0004
	modInfoMetadata = 0x8000

	modInfoMDEnvp    = 0x0006
	modInfoMDHowto   = 0x0007
	modInfoMDKernEnd = 0x0008
	modInfoMDModulep = 0x1006
	modInfoMDSMAP    = 0x1001

	freeBSDKernelType          = "elf kernel"
	freeBSDKernelName          = "/boot/kernel/kernel"
	freeBSDBootEntropyType     = "boot_entropy_cache"
	freeBSDPlatformEntropyType = "boot_entropy_platform"

	rbVerbose  = 0x0800
	rbSerial   = 0x1000
	rbMultiple = 0x20000000

	smapTypeMemory   = 1
	smapTypeReserved = 2

	lowMemoryLimit = 3 << 30
	highMemoryBase = 4 << 30
)

type BootOptions struct {
	MemorySize  uint64
	NumCPUs     int
	StackGPA    uint64
	MetadataGPA uint64
	PagingGPA   uint64
	Howto       uint32
	Environment []string
	Entropy     []byte
}

type BootPlan struct {
	EntryGVA     uint64
	StackGPA     uint64
	MetadataGPA  uint64
	MetadataLen  uint32
	PagingGPA    uint64
	KernelEndGPA uint64
	KernEnd      uint32
}

type smapEntry struct {
	base   uint64
	length uint64
	typ    uint32
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
	if opts.StackGPA == 0 {
		opts.StackGPA = defaultStackGPA
	}
	if opts.PagingGPA == 0 {
		opts.PagingGPA = defaultPagingGPA
	}

	entry, kernelStart, kernelEnd, err := loadELF(memory, kernel)
	if err != nil {
		return nil, err
	}
	if opts.MetadataGPA == 0 {
		opts.MetadataGPA = defaultMetadataGPA
		if opts.MetadataGPA < kernelEnd {
			opts.MetadataGPA = alignUp(kernelEnd, pageSize)
		}
	}
	if err := installSMBIOS(memory); err != nil {
		return nil, err
	}
	if _, err := installBootACPI(memory, opts.NumCPUs); err != nil {
		return nil, err
	}
	if entry < kernStart {
		return nil, fmt.Errorf("unsupported FreeBSD kernel entry %#x", entry)
	}

	entropy := opts.Entropy
	if len(entropy) == 0 {
		entropy = make([]byte, 4096)
		if _, err := rand.Read(entropy); err != nil {
			return nil, fmt.Errorf("generate FreeBSD boot entropy: %w", err)
		}
	}

	metadata, kernEnd, err := buildMetadata(kernelStart, kernelEnd, opts.MetadataGPA, opts.Howto|rbSerial|rbMultiple, opts.Environment, entropy, defaultSMAP(opts.MemorySize))
	if err != nil {
		return nil, err
	}
	if err := writeAt(memory, opts.MetadataGPA, metadata); err != nil {
		return nil, fmt.Errorf("write FreeBSD metadata: %w", err)
	}

	stack := buildStack(uint32(opts.MetadataGPA), kernEnd)
	stackGPA := opts.StackGPA - uint64(len(stack))
	if stackGPA >= opts.StackGPA {
		return nil, errors.New("invalid FreeBSD stack placement")
	}
	if err := writeAt(memory, stackGPA, stack); err != nil {
		return nil, fmt.Errorf("write FreeBSD boot stack: %w", err)
	}

	return &BootPlan{
		EntryGVA:     entry,
		StackGPA:     stackGPA,
		MetadataGPA:  opts.MetadataGPA,
		MetadataLen:  uint32(len(metadata)),
		PagingGPA:    opts.PagingGPA,
		KernelEndGPA: kernelEnd,
		KernEnd:      kernEnd,
	}, nil
}

func loadELF(memory []byte, kernel []byte) (entry, kernelStart, kernelEnd uint64, err error) {
	f, err := elf.NewFile(bytes.NewReader(kernel))
	if err != nil {
		return 0, 0, 0, fmt.Errorf("parse FreeBSD kernel ELF: %w", err)
	}
	defer f.Close()
	if f.FileHeader.Class != elf.ELFCLASS64 || f.FileHeader.Data != elf.ELFDATA2LSB || f.FileHeader.Machine != elf.EM_X86_64 {
		return 0, 0, 0, fmt.Errorf("unsupported FreeBSD kernel ELF class=%v data=%v machine=%v", f.FileHeader.Class, f.FileHeader.Data, f.FileHeader.Machine)
	}
	kernelStart = ^uint64(0)
	for _, prog := range f.Progs {
		if prog.Type != elf.PT_LOAD || prog.Memsz == 0 {
			continue
		}
		if prog.Paddr+prog.Memsz > uint64(len(memory)) {
			return 0, 0, 0, fmt.Errorf("kernel segment paddr=%#x memsz=%#x exceeds guest memory %#x", prog.Paddr, prog.Memsz, len(memory))
		}
		seg := memory[prog.Paddr : prog.Paddr+prog.Memsz]
		clear(seg)
		if prog.Filesz != 0 {
			if _, err := io.ReadFull(prog.Open(), seg[:prog.Filesz]); err != nil {
				return 0, 0, 0, fmt.Errorf("read kernel segment at %#x: %w", prog.Paddr, err)
			}
		}
		if prog.Paddr < kernelStart {
			kernelStart = prog.Paddr
		}
		if end := alignUp(prog.Paddr+prog.Memsz, pageSize); end > kernelEnd {
			kernelEnd = end
		}
	}
	if kernelEnd == 0 {
		return 0, 0, 0, errors.New("FreeBSD kernel has no loadable segments")
	}
	if err := applyRelocations(f, memory); err != nil {
		return 0, 0, 0, err
	}
	return f.Entry, kernelStart, kernelEnd, nil
}

func applyRelocations(f *elf.File, memory []byte) error {
	for _, section := range f.Sections {
		if section.Type != elf.SHT_RELA {
			continue
		}
		symbols, err := linkedSymbols(f, section)
		if err != nil {
			return fmt.Errorf("read symbols for %s: %w", section.Name, err)
		}
		data, err := section.Data()
		if err != nil {
			return fmt.Errorf("read relocations %s: %w", section.Name, err)
		}
		if len(data)%24 != 0 {
			return fmt.Errorf("relocation section %s has invalid size %d", section.Name, len(data))
		}
		for off := 0; off < len(data); off += 24 {
			relocOff := f.ByteOrder.Uint64(data[off:])
			info := f.ByteOrder.Uint64(data[off+8:])
			addend := int64(f.ByteOrder.Uint64(data[off+16:]))
			symIndex := info >> 32
			relocType := elf.R_X86_64(info & 0xffffffff)
			var symValue uint64
			if symIndex != 0 {
				if symIndex >= uint64(len(symbols)) {
					return fmt.Errorf("%s relocation references symbol %d beyond %d", section.Name, symIndex, len(symbols))
				}
				symValue = symbols[symIndex].Value
			}
			if err := applyRelocation(memory, relocOff, relocType, symValue, addend); err != nil {
				return fmt.Errorf("%s relocation at %#x: %w", section.Name, relocOff, err)
			}
		}
	}
	return nil
}

func linkedSymbols(f *elf.File, section *elf.Section) ([]elf.Symbol, error) {
	if section.Link == 0 || int(section.Link) >= len(f.Sections) {
		return nil, fmt.Errorf("invalid linked symbol table %d", section.Link)
	}
	symSection := f.Sections[section.Link]
	data, err := symSection.Data()
	if err != nil {
		return nil, err
	}
	if symSection.Entsize == 0 {
		symSection.Entsize = 24
	}
	if len(data)%int(symSection.Entsize) != 0 {
		return nil, fmt.Errorf("symbol table %s has invalid size %d", symSection.Name, len(data))
	}
	symbols := make([]elf.Symbol, len(data)/int(symSection.Entsize))
	for i := range symbols {
		off := i * int(symSection.Entsize)
		if off+24 > len(data) {
			return nil, fmt.Errorf("short symbol %d in %s", i, symSection.Name)
		}
		symbols[i].Info = data[off+4]
		symbols[i].Other = data[off+5]
		symbols[i].Section = elf.SectionIndex(f.ByteOrder.Uint16(data[off+6:]))
		symbols[i].Value = f.ByteOrder.Uint64(data[off+8:])
		symbols[i].Size = f.ByteOrder.Uint64(data[off+16:])
	}
	return symbols, nil
}

func applyRelocation(memory []byte, relocOff uint64, relocType elf.R_X86_64, symValue uint64, addend int64) error {
	phys, err := freeBSDVirtualToPhysical(relocOff)
	if err != nil {
		return err
	}
	if phys+8 > uint64(len(memory)) {
		return fmt.Errorf("target exceeds guest memory")
	}
	value := int64(symValue) + addend
	switch relocType {
	case elf.R_X86_64_64, elf.R_X86_64_JMP_SLOT:
		binary.LittleEndian.PutUint64(memory[phys:phys+8], uint64(value))
	case elf.R_X86_64_PLT32, elf.R_X86_64_PC32:
		pcValue := value - int64(relocOff)
		if int64(int32(pcValue)) != pcValue {
			return fmt.Errorf("pc-relative relocation overflows int32: %#x", pcValue)
		}
		binary.LittleEndian.PutUint32(memory[phys:phys+4], uint32(int32(pcValue)))
	case elf.R_X86_64_32S:
		if int64(int32(value)) != value {
			return fmt.Errorf("signed 32-bit relocation overflows: %#x", value)
		}
		binary.LittleEndian.PutUint32(memory[phys:phys+4], uint32(int32(value)))
	case elf.R_X86_64_NONE:
		return nil
	default:
		return fmt.Errorf("unsupported relocation type %s", relocType)
	}
	return nil
}

func freeBSDVirtualToPhysical(addr uint64) (uint64, error) {
	if addr < kernBase {
		return addr, nil
	}
	return addr - kernBase, nil
}

func buildMetadata(kernelStart, kernelEnd, metadataGPA uint64, howto uint32, env []string, entropy []byte, smap []smapEntry) ([]byte, uint32, error) {
	envBytes := buildEnv(env)
	envGPA := alignUp(metadataGPA+0x1000, pageSize)
	entropyGPA := alignUp(envGPA+uint64(len(envBytes)), 16)
	platformEntropyGPA := alignUp(entropyGPA+uint64(len(entropy)), 16)
	kernEnd := uint32(alignUp(platformEntropyGPA+uint64(len(entropy)), pageSize))
	records := metadataBuilder{}
	records.addString(modInfoName, freeBSDKernelName)
	records.addString(modInfoType, freeBSDKernelType)
	records.addUint64(modInfoAddr, kernelStart)
	records.addUint64(modInfoSize, kernelEnd-kernelStart)
	records.addUint32(modInfoMetadata|modInfoMDHowto, howto)
	records.addUint64(modInfoMetadata|modInfoMDEnvp, envGPA)
	records.addUint64(modInfoMetadata|modInfoMDKernEnd, uint64(kernEnd))
	records.addBytes(modInfoMetadata|modInfoMDSMAP, encodeSMAP(smap))
	records.addUint64(modInfoMetadata|modInfoMDModulep, metadataGPA)
	if len(entropy) > 0 {
		records.addPreload(freeBSDKernelName+".entropy", freeBSDBootEntropyType, entropyGPA, uint64(len(entropy)))
		records.addPreload(freeBSDKernelName+".platform_entropy", freeBSDPlatformEntropyType, platformEntropyGPA, uint64(len(entropy)))
	}
	records.addEnd()

	metadata := records.bytes()
	if len(metadata) > pageSize {
		return nil, 0, fmt.Errorf("FreeBSD metadata too large: %d", len(metadata))
	}
	outLen := int(uint64(kernEnd) - metadataGPA)
	out := make([]byte, outLen)
	copy(out, metadata)
	copy(out[envGPA-metadataGPA:], envBytes)
	if len(entropy) > 0 {
		copy(out[entropyGPA-metadataGPA:], entropy)
		copy(out[platformEntropyGPA-metadataGPA:], entropy)
	}
	return out, kernEnd, nil
}

type metadataBuilder struct {
	buf []byte
}

func (b *metadataBuilder) addString(typ uint32, value string) {
	data := append([]byte(value), 0)
	b.addBytes(typ, data)
}

func (b *metadataBuilder) addUint32(typ uint32, value uint32) {
	var data [4]byte
	binary.LittleEndian.PutUint32(data[:], value)
	b.addBytes(typ, data[:])
}

func (b *metadataBuilder) addUint64(typ uint32, value uint64) {
	var data [8]byte
	binary.LittleEndian.PutUint64(data[:], value)
	b.addBytes(typ, data[:])
}

func (b *metadataBuilder) addPreload(name, typ string, addr, size uint64) {
	b.addString(modInfoName, name)
	b.addString(modInfoType, typ)
	b.addUint64(modInfoAddr, addr)
	b.addUint64(modInfoSize, size)
}

func (b *metadataBuilder) addBytes(typ uint32, data []byte) {
	b.buf = binary.LittleEndian.AppendUint32(b.buf, typ)
	b.buf = binary.LittleEndian.AppendUint32(b.buf, uint32(len(data)))
	b.buf = append(b.buf, data...)
	for len(b.buf)%wordSize != 0 {
		b.buf = append(b.buf, 0)
	}
}

func (b *metadataBuilder) addEnd() {
	b.buf = binary.LittleEndian.AppendUint32(b.buf, modInfoEnd)
	b.buf = binary.LittleEndian.AppendUint32(b.buf, modInfoEnd)
}

func (b *metadataBuilder) bytes() []byte {
	return append([]byte(nil), b.buf...)
}

func buildEnv(values []string) []byte {
	if len(values) == 0 {
		values = []string{
			"console=comconsole",
			"comconsole_speed=115200",
			"comconsole_port=0x3f8",
			"boot_serial=YES",
			"boot_multicons=YES",
			"hw.vga.textmode=1",
			"hint.uart.0.at=isa",
			"hint.uart.0.port=0x3f8",
			"hint.uart.0.irq=4",
			"hint.uart.0.flags=0x10",
			"hint.smbios.0.disabled=1",
			"smbios.memory.enabled=0",
			"vfs.root.mountfrom=ufs:/dev/vtbd0",
		}
	}
	var out []byte
	for _, value := range values {
		if value == "" {
			continue
		}
		out = append(out, value...)
		out = append(out, 0)
	}
	out = append(out, 0)
	return out
}

func encodeSMAP(entries []smapEntry) []byte {
	out := make([]byte, 0, len(entries)*20)
	for _, entry := range entries {
		out = binary.LittleEndian.AppendUint64(out, entry.base)
		out = binary.LittleEndian.AppendUint64(out, entry.length)
		out = binary.LittleEndian.AppendUint32(out, entry.typ)
	}
	return out
}

func defaultSMAP(memorySize uint64) []smapEntry {
	entries := []smapEntry{
		{base: 0, length: 0x9fc00, typ: smapTypeMemory},
		{base: 0x9fc00, length: 0x60400, typ: smapTypeReserved},
	}
	lowSize := memorySize
	if lowSize > lowMemoryLimit {
		lowSize = lowMemoryLimit
	}
	if lowSize > 0x100000 {
		entries = append(entries, smapEntry{base: 0x100000, length: lowSize - 0x100000, typ: smapTypeMemory})
	}
	if memorySize > lowMemoryLimit {
		entries = append(entries, smapEntry{base: lowMemoryLimit, length: highMemoryBase - lowMemoryLimit, typ: smapTypeReserved})
		entries = append(entries, smapEntry{base: highMemoryBase, length: memorySize - lowMemoryLimit, typ: smapTypeMemory})
	}
	return entries
}

func buildStack(modulep, kernEnd uint32) []byte {
	var stack []byte
	stack = binary.LittleEndian.AppendUint32(stack, 0)
	stack = binary.LittleEndian.AppendUint32(stack, modulep)
	stack = binary.LittleEndian.AppendUint32(stack, kernEnd)
	stack = binary.LittleEndian.AppendUint32(stack, 0)
	return stack
}

func installSMBIOS(memory []byte) error {
	const (
		epsAddr   = 0x000f0000
		tableAddr = 0x000f0100
	)
	table := []byte{
		127, 4, 0, 0, // End-of-table structure.
		0, 0,
	}
	if err := writeAt(memory, tableAddr, table); err != nil {
		return fmt.Errorf("write SMBIOS table: %w", err)
	}
	eps := make([]byte, 31)
	copy(eps[0:4], "_SM_")
	eps[5] = byte(len(eps))
	eps[6] = 2
	eps[7] = 8
	binary.LittleEndian.PutUint16(eps[8:10], 4)
	copy(eps[16:21], "_DMI_")
	binary.LittleEndian.PutUint16(eps[22:24], uint16(len(table)))
	binary.LittleEndian.PutUint32(eps[24:28], tableAddr)
	binary.LittleEndian.PutUint16(eps[28:30], 1)
	eps[30] = 0x28
	eps[21] = checksumByte(eps[16:31])
	eps[4] = checksumByte(eps)
	if err := writeAt(memory, epsAddr, eps); err != nil {
		return fmt.Errorf("write SMBIOS entry point: %w", err)
	}
	return nil
}

func checksumByte(data []byte) byte {
	var sum byte
	for _, b := range data {
		sum += b
	}
	return -sum
}

func alignUp(v, align uint64) uint64 {
	if align == 0 {
		return v
	}
	return (v + align - 1) &^ (align - 1)
}

func writeAt(memory []byte, addr uint64, data []byte) error {
	if addr > uint64(len(memory)) || uint64(len(data)) > uint64(len(memory))-addr {
		return fmt.Errorf("write addr=%#x len=%#x exceeds memory %#x", addr, len(data), len(memory))
	}
	copy(memory[addr:], data)
	return nil
}
