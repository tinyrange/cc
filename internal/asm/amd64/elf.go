package amd64

import (
	"debug/elf"
	"encoding/binary"
	"fmt"
)

const (
	elfHeaderSize        = 64
	elfProgramHeaderSize = 56
)

var (
	// defaultStandaloneELFConfig holds the defaults used when emitting standalone
	// executables. It is defined as a var so tests within the package can refer
	// to the same values while keeping the exported helper immutable.
	defaultStandaloneELFConfig = StandaloneELFConfig{
		BaseAddress:      0x401000,
		SegmentOffset:    0x1000,
		SegmentAlignment: 0x1000,
		SegmentFlags:     elf.PF_R | elf.PF_W | elf.PF_X,
	}
)

// StandaloneELFConfig controls how Program.StandaloneELF emits the final
// executable.
type StandaloneELFConfig struct {
	// BaseAddress is the virtual address where the first byte of the emitted
	// program will be loaded. The relocation entries in Program are resolved
	// against this address.
	BaseAddress uint64
	// SegmentOffset is the file offset where the loadable segment begins. This
	// must be aligned to SegmentAlignment and large enough to fit the ELF and
	// program headers placed before the segment.
	SegmentOffset uint64
	// SegmentAlignment is the alignment requirement for the loadable segment.
	SegmentAlignment uint64
	// SegmentFlags controls the permission bits on the loadable segment. The
	// default marks the segment readable, writable, and executable so embedded
	// data can be modified by the program.
	SegmentFlags elf.ProgFlag
}

// DefaultStandaloneELFConfig returns the configuration used by StandaloneELF
// when no overrides are provided.
func DefaultStandaloneELFConfig() StandaloneELFConfig {
	return defaultStandaloneELFConfig
}

// StandaloneELF emits the program as a standalone ELF binary using the default
// configuration.
func (p Program) StandaloneELF() ([]byte, error) {
	return p.StandaloneELFWithConfig(DefaultStandaloneELFConfig())
}

// StandaloneELFWithConfig emits the program as a standalone ELF binary using
// the provided configuration. Zero-valued configuration fields are replaced
// with sensible defaults.
func (p Program) StandaloneELFWithConfig(cfg StandaloneELFConfig) ([]byte, error) {
	cfg = cfg.withDefaults()

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	code := p.RelocatedCopy(uintptr(cfg.BaseAddress))
	bssSize := p.BSSSize()
	fileSize := uint64(len(code))
	memSize := fileSize + uint64(bssSize)

	prefixLen := int(cfg.SegmentOffset)
	headerLimit := elfHeaderSize + elfProgramHeaderSize
	prefix := make([]byte, prefixLen)

	eIdent := prefix[:elfHeaderSize]
	fillELFHeader(eIdent, cfg)

	progHeader := prefix[elfHeaderSize:headerLimit]
	fillProgramHeader(progHeader, cfg, fileSize, memSize)

	// Pad up to SegmentOffset before appending the relocated program bytes.
	out := append(prefix, code...)
	return out, nil
}

// EmitStandaloneELF emits the provided fragment as a standalone ELF binary
// using the default configuration.
func EmitStandaloneELF(f Fragment) ([]byte, error) {
	prog, err := EmitProgram(f)
	if err != nil {
		return nil, err
	}
	return prog.StandaloneELF()
}

// EmitStandaloneELFWithConfig emits the provided fragment as a standalone ELF
// binary using the supplied configuration.
func EmitStandaloneELFWithConfig(f Fragment, cfg StandaloneELFConfig) ([]byte, error) {
	prog, err := EmitProgram(f)
	if err != nil {
		return nil, err
	}
	return prog.StandaloneELFWithConfig(cfg)
}

func (cfg StandaloneELFConfig) withDefaults() StandaloneELFConfig {
	defaults := DefaultStandaloneELFConfig()
	if cfg.BaseAddress == 0 {
		cfg.BaseAddress = defaults.BaseAddress
	}
	if cfg.SegmentOffset == 0 {
		cfg.SegmentOffset = defaults.SegmentOffset
	}
	if cfg.SegmentAlignment == 0 {
		cfg.SegmentAlignment = defaults.SegmentAlignment
	}
	if cfg.SegmentFlags == 0 {
		cfg.SegmentFlags = defaults.SegmentFlags
	}
	return cfg
}

func (cfg StandaloneELFConfig) validate() error {
	headerSize := uint64(elfHeaderSize + elfProgramHeaderSize)
	if cfg.SegmentOffset < headerSize {
		return fmt.Errorf("segment offset %#x too small for ELF headers (%#x)", cfg.SegmentOffset, headerSize)
	}
	if cfg.SegmentAlignment == 0 || cfg.SegmentAlignment&(cfg.SegmentAlignment-1) != 0 {
		return fmt.Errorf("segment alignment %#x is not a power of two", cfg.SegmentAlignment)
	}
	if cfg.SegmentOffset%cfg.SegmentAlignment != 0 {
		return fmt.Errorf("segment offset %#x must be aligned to %#x", cfg.SegmentOffset, cfg.SegmentAlignment)
	}
	if cfg.BaseAddress < cfg.SegmentOffset {
		return fmt.Errorf("base address %#x must be >= segment offset %#x", cfg.BaseAddress, cfg.SegmentOffset)
	}
	if (cfg.BaseAddress-cfg.SegmentOffset)%cfg.SegmentAlignment != 0 {
		return fmt.Errorf("base address %#x must satisfy alignment relative to offset %#x (align %#x)", cfg.BaseAddress, cfg.SegmentOffset, cfg.SegmentAlignment)
	}

	if cfg.SegmentOffset > uint64(maxInt) {
		return fmt.Errorf("segment offset %#x exceeds platform limits", cfg.SegmentOffset)
	}
	return nil
}

func fillELFHeader(buf []byte, cfg StandaloneELFConfig) {
	for idx := range buf {
		buf[idx] = 0
	}
	buf[0] = 0x7f
	buf[1] = 'E'
	buf[2] = 'L'
	buf[3] = 'F'
	buf[4] = 2 // 64-bit
	buf[5] = 1 // little-endian
	buf[6] = 1 // current version
	// Remaining bytes in e_ident are already zero.

	binary.LittleEndian.PutUint16(buf[16:], uint16(elf.ET_EXEC))
	binary.LittleEndian.PutUint16(buf[18:], uint16(elf.EM_X86_64))
	binary.LittleEndian.PutUint32(buf[20:], uint32(elf.EV_CURRENT))
	binary.LittleEndian.PutUint64(buf[24:], cfg.BaseAddress)
	binary.LittleEndian.PutUint64(buf[32:], uint64(elfHeaderSize))
	binary.LittleEndian.PutUint64(buf[40:], 0) // section header offset
	binary.LittleEndian.PutUint32(buf[48:], 0) // flags
	binary.LittleEndian.PutUint16(buf[52:], uint16(elfHeaderSize))
	binary.LittleEndian.PutUint16(buf[54:], uint16(elfProgramHeaderSize))
	binary.LittleEndian.PutUint16(buf[56:], 1) // one program header
	// Section header fields remain zero as no section table is emitted.
}

func fillProgramHeader(buf []byte, cfg StandaloneELFConfig, fileSize uint64, memSize uint64) {
	for idx := range buf {
		buf[idx] = 0
	}
	binary.LittleEndian.PutUint32(buf[0:], uint32(elf.PT_LOAD))
	binary.LittleEndian.PutUint32(buf[4:], uint32(cfg.SegmentFlags))
	binary.LittleEndian.PutUint64(buf[8:], cfg.SegmentOffset)
	binary.LittleEndian.PutUint64(buf[16:], cfg.BaseAddress)
	binary.LittleEndian.PutUint64(buf[24:], cfg.BaseAddress)
	binary.LittleEndian.PutUint64(buf[32:], fileSize)
	binary.LittleEndian.PutUint64(buf[40:], memSize)
	binary.LittleEndian.PutUint64(buf[48:], cfg.SegmentAlignment)
}

func init() {
	// Sanity check default configuration at init time so unexpected mutations
	// or platform differences are caught early.
	if err := defaultStandaloneELFConfig.validate(); err != nil {
		panic(err)
	}
}

const maxInt = int(^uint(0) >> 1)
