package amd64

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"testing"

	"github.com/tinyrange/cc/internal/asm"
	linux "github.com/tinyrange/cc/internal/linux/defs/amd64"
)

func Exit(code int) asm.Fragment {
	return Syscall(
		linux.SYS_EXIT,
		asm.Immediate(code),
	)
}

func TestStandaloneELFHeader(t *testing.T) {
	frag := asm.Group{
		SyscallWriteString(asm.Immediate(1), "Hello, ELF!\n"),
		Exit(0),
	}

	prog, err := EmitProgram(frag)
	if err != nil {
		t.Fatalf("EmitProgram failed: %v", err)
	}

	cfg := DefaultStandaloneELFConfig()
	elfBytes, err := StandaloneELF(prog)
	if err != nil {
		t.Fatalf("StandaloneELF failed: %v", err)
	}

	f, err := elf.NewFile(bytes.NewReader(elfBytes))
	if err != nil {
		t.Fatalf("parse ELF: %v", err)
	}
	defer f.Close()

	if got, want := f.FileHeader.Type, elf.ET_EXEC; got != want {
		t.Fatalf("ELF type=%v, want %v", got, want)
	}
	if got, want := f.FileHeader.Entry, cfg.BaseAddress; got != want {
		t.Fatalf("entry point=%#x, want %#x", got, want)
	}
	if len(f.Progs) != 1 {
		t.Fatalf("expected single program header, got %d", len(f.Progs))
	}
	ph := f.Progs[0]
	if got, want := ph.Type, elf.PT_LOAD; got != want {
		t.Fatalf("program header type=%v, want %v", got, want)
	}
	if got, want := ph.Flags, cfg.SegmentFlags; got != want {
		t.Fatalf("segment flags=%v, want %v", got, want)
	}
	if got, want := ph.Off, cfg.SegmentOffset; got != want {
		t.Fatalf("segment offset=%#x, want %#x", got, want)
	}
	if got, want := ph.Vaddr, cfg.BaseAddress; got != want {
		t.Fatalf("segment vaddr=%#x, want %#x", got, want)
	}
	if got, want := ph.Paddr, cfg.BaseAddress; got != want {
		t.Fatalf("segment paddr=%#x, want %#x", got, want)
	}

	relocated := prog.RelocatedCopy(uintptr(cfg.BaseAddress))
	if got, want := ph.Filesz, uint64(len(relocated)); got != want {
		t.Fatalf("segment filesz=%d, want %d", got, want)
	}
	if got, want := ph.Memsz, uint64(len(relocated)); got != want {
		t.Fatalf("segment memsz=%d, want %d", got, want)
	}
	if got, want := ph.Align, cfg.SegmentAlignment; got != want {
		t.Fatalf("segment align=%#x, want %#x", got, want)
	}

	if !bytes.Contains(elfBytes[cfg.SegmentOffset:], []byte("Hello, ELF!\n")) {
		t.Fatalf("standalone ELF missing literal string")
	}
}

func TestStandaloneELFRelocations(t *testing.T) {
	frag := asm.Group{
		LoadConstantString(R8, "greetings"),
		SyscallWrite(asm.Immediate(1), R8, asm.Immediate(9)),
		Exit(0),
	}
	prog, err := EmitProgram(frag)
	if err != nil {
		t.Fatalf("EmitProgram failed: %v", err)
	}

	cfg := DefaultStandaloneELFConfig()
	elfBytes, err := StandaloneELF(prog)
	if err != nil {
		t.Fatalf("StandaloneELF failed: %v", err)
	}

	relocated := prog.RelocatedCopy(uintptr(cfg.BaseAddress))
	for _, off := range prog.Relocations() {
		if off+8 > len(relocated) {
			t.Fatalf("relocation offset %d outside program", off)
		}
		want := binary.LittleEndian.Uint64(relocated[off:])
		filePos := cfg.SegmentOffset + uint64(off)
		if filePos+8 > uint64(len(elfBytes)) {
			t.Fatalf("relocation file position %#x outside ELF", filePos)
		}
		got := binary.LittleEndian.Uint64(elfBytes[filePos:])
		if got != want {
			t.Fatalf("relocation @%d = %#x, want %#x", off, got, want)
		}
	}
}

func TestStandaloneELFCustomConfig(t *testing.T) {
	prog, err := EmitProgram(asm.Group{
		Exit(123),
	})
	if err != nil {
		t.Fatalf("EmitProgram failed: %v", err)
	}

	cfg := StandaloneELFConfig{
		BaseAddress:      0x500000,
		SegmentOffset:    0x2000,
		SegmentAlignment: 0x1000,
		SegmentFlags:     elf.PF_R | elf.PF_X,
	}

	elfBytes, err := StandaloneELFWithConfig(prog, cfg)
	if err != nil {
		t.Fatalf("StandaloneELFWithConfig failed: %v", err)
	}

	f, err := elf.NewFile(bytes.NewReader(elfBytes))
	if err != nil {
		t.Fatalf("parse ELF: %v", err)
	}
	defer f.Close()

	if got, want := f.FileHeader.Entry, cfg.BaseAddress; got != want {
		t.Fatalf("entry point=%#x, want %#x", got, want)
	}
	if len(f.Progs) != 1 {
		t.Fatalf("expected single program header, got %d", len(f.Progs))
	}
	ph := f.Progs[0]
	if got, want := ph.Off, cfg.SegmentOffset; got != want {
		t.Fatalf("segment offset=%#x, want %#x", got, want)
	}
	if got, want := ph.Flags, cfg.SegmentFlags; got != want {
		t.Fatalf("segment flags=%v, want %v", got, want)
	}
}
