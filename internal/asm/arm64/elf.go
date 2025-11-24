package arm64

import (
	"debug/elf"
	"encoding/binary"
	"fmt"

	"github.com/tinyrange/cc/internal/asm"
)

const (
	elfHeaderSize        = 64
	elfProgramHeaderSize = 56
)

var defaultStandaloneELFConfig = StandaloneELFConfig{
	BaseAddress:      0x401000,
	SegmentOffset:    0x1000,
	SegmentAlignment: 0x1000,
	SegmentFlags:     elf.PF_R | elf.PF_W | elf.PF_X,
}

type StandaloneELFConfig struct {
	BaseAddress      uint64
	SegmentOffset    uint64
	SegmentAlignment uint64
	SegmentFlags     elf.ProgFlag
}

func DefaultStandaloneELFConfig() StandaloneELFConfig {
	return defaultStandaloneELFConfig
}

func StandaloneELF(prog asm.Program) ([]byte, error) {
	return StandaloneELFWithConfig(prog, DefaultStandaloneELFConfig())
}

func StandaloneELFWithConfig(prog asm.Program, cfg StandaloneELFConfig) ([]byte, error) {
	cfg = cfg.withDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	code := prog.RelocatedCopy(uintptr(cfg.BaseAddress))
	bssSize := prog.BSSSize()
	fileSize := uint64(len(code))
	memSize := fileSize + uint64(bssSize)

	prefixLen := int(cfg.SegmentOffset)
	headerLimit := elfHeaderSize + elfProgramHeaderSize
	prefix := make([]byte, prefixLen)

	fillELFHeader(prefix[:elfHeaderSize], cfg)
	fillProgramHeader(prefix[elfHeaderSize:headerLimit], cfg, fileSize, memSize)

	return append(prefix, code...), nil
}

func EmitStandaloneELF(f asm.Fragment) ([]byte, error) {
	prog, err := EmitProgram(f)
	if err != nil {
		return nil, err
	}
	return StandaloneELF(prog)
}

func EmitStandaloneELFWithConfig(f asm.Fragment, cfg StandaloneELFConfig) ([]byte, error) {
	prog, err := EmitProgram(f)
	if err != nil {
		return nil, err
	}
	return StandaloneELFWithConfig(prog, cfg)
}

func (cfg StandaloneELFConfig) withDefaults() StandaloneELFConfig {
	def := DefaultStandaloneELFConfig()
	if cfg.BaseAddress == 0 {
		cfg.BaseAddress = def.BaseAddress
	}
	if cfg.SegmentOffset == 0 {
		cfg.SegmentOffset = def.SegmentOffset
	}
	if cfg.SegmentAlignment == 0 {
		cfg.SegmentAlignment = def.SegmentAlignment
	}
	if cfg.SegmentFlags == 0 {
		cfg.SegmentFlags = def.SegmentFlags
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
		return fmt.Errorf("base address %#x must satisfy alignment relative to offset %#x (align %#x)",
			cfg.BaseAddress, cfg.SegmentOffset, cfg.SegmentAlignment)
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

	binary.LittleEndian.PutUint16(buf[16:], uint16(elf.ET_EXEC))
	binary.LittleEndian.PutUint16(buf[18:], uint16(elf.EM_AARCH64))
	binary.LittleEndian.PutUint32(buf[20:], uint32(elf.EV_CURRENT))
	binary.LittleEndian.PutUint64(buf[24:], cfg.BaseAddress)
	binary.LittleEndian.PutUint64(buf[32:], uint64(elfHeaderSize))
	binary.LittleEndian.PutUint64(buf[40:], 0) // section header offset
	binary.LittleEndian.PutUint32(buf[48:], 0) // flags
	binary.LittleEndian.PutUint16(buf[52:], uint16(elfHeaderSize))
	binary.LittleEndian.PutUint16(buf[54:], uint16(elfProgramHeaderSize))
	binary.LittleEndian.PutUint16(buf[56:], 1) // one program header
}

func fillProgramHeader(buf []byte, cfg StandaloneELFConfig, fileSize, memSize uint64) {
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

const maxInt = int(^uint(0) >> 1)
