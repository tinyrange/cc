package testutil

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

const (
	// MachineX86_64 is the ELF e_machine value for AMD64.
	MachineX86_64 = 62
	// MachineAArch64 is the ELF e_machine value for AArch64.
	MachineAArch64 = 183
)

// DisasmLine represents a single instruction line emitted by objdump.
type DisasmLine struct {
	Text       string
	Normalized string
	Mnemonic   string
}

// Contains reports whether the normalized instruction text contains the provided substring.
func (l DisasmLine) Contains(substr string) bool {
	return strings.Contains(l.Normalized, substr)
}

// DisassembleWithObjdump wraps the provided code bytes into a minimal ELF for
// the supplied machine type and runs GNU objdump -d --no-show-raw-insn.
func DisassembleWithObjdump(t *testing.T, code []byte, machine uint16, extraArgs ...string) []DisasmLine {
	t.Helper()
	args := []string{"-d", "--no-show-raw-insn"}
	args = append(args, extraArgs...)
	return DisassembleWithTool(t, "objdump", code, machine, args...)
}

// DisassembleWithTool wraps the provided code bytes into a minimal ELF for the
// supplied machine type and invokes the requested disassembler.
func DisassembleWithTool(t *testing.T, tool string, code []byte, machine uint16, args ...string) []DisasmLine {
	t.Helper()

	toolPath, err := exec.LookPath(tool)
	if err != nil {
		t.Skipf("%s not found: %v", tool, err)
	}

	elf := buildMinimalELF(code, machine)

	tmp, err := os.CreateTemp("", "cc-objdump-*.elf")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer func() {
		_ = os.Remove(tmp.Name())
	}()

	if _, err := tmp.Write(elf); err != nil {
		t.Fatalf("write temp ELF: %v", err)
	}
	if err := tmp.Close(); err != nil {
		t.Fatalf("close temp ELF: %v", err)
	}

	cmdArgs := append([]string{}, args...)
	cmdArgs = append(cmdArgs, tmp.Name())
	cmd := exec.Command(toolPath, cmdArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s failed: %v\n\n%s", tool, err, output)
	}

	lines, err := parseObjdumpOutput(string(output))
	if err != nil {
		t.Fatalf("parse objdump output: %v", err)
	}
	if len(lines) == 0 {
		t.Fatalf("objdump produced no instructions:\n%s", output)
	}
	if len(lines) < 5 {
		t.Logf("objdump output:\n%s", output)
	}
	return lines
}

func buildMinimalELF(code []byte, machine uint16) []byte {
	const (
		elfHeaderSize = 64
		sectionCount  = 3 // null, .text, .shstrtab
		secHeaderSize = 64
		textAlign     = 16
	)

	textOffset := elfHeaderSize
	textPadded := align(textOffset+len(code), textAlign) - textOffset
	shstr := []byte{0, '.', 't', 'e', 'x', 't', 0, '.', 's', 'h', 's', 't', 'r', 't', 'a', 'b', 0}
	shstrOffset := textOffset + textPadded
	shstrPadded := align(len(shstr), 8)
	sectionOffset := shstrOffset + shstrPadded
	totalSize := sectionOffset + sectionCount*secHeaderSize

	buf := make([]byte, totalSize)
	copy(buf[textOffset:], code)
	copy(buf[shstrOffset:], shstr)

	// ELF header.
	eIdent := buf[:16]
	eIdent[0] = 0x7f
	eIdent[1] = 'E'
	eIdent[2] = 'L'
	eIdent[3] = 'F'
	eIdent[4] = 2 // 64-bit
	eIdent[5] = 1 // little endian
	eIdent[6] = 1 // current version

	binary.LittleEndian.PutUint16(buf[16:], 2)                     // e_type (ET_EXEC)
	binary.LittleEndian.PutUint16(buf[18:], machine)               // e_machine
	binary.LittleEndian.PutUint32(buf[20:], 1)                     // e_version
	binary.LittleEndian.PutUint64(buf[24:], 0)                     // e_entry
	binary.LittleEndian.PutUint64(buf[32:], 0)                     // e_phoff
	binary.LittleEndian.PutUint64(buf[40:], uint64(sectionOffset)) // e_shoff
	binary.LittleEndian.PutUint32(buf[48:], 0)                     // e_flags
	binary.LittleEndian.PutUint16(buf[52:], elfHeaderSize)         // e_ehsize
	binary.LittleEndian.PutUint16(buf[54:], 0)                     // e_phentsize
	binary.LittleEndian.PutUint16(buf[56:], 0)                     // e_phnum
	binary.LittleEndian.PutUint16(buf[58:], secHeaderSize)         // e_shentsize
	binary.LittleEndian.PutUint16(buf[60:], sectionCount)          // e_shnum
	binary.LittleEndian.PutUint16(buf[62:], 2)                     // e_shstrndx

	// Section headers.
	shdr := buf[sectionOffset:]
	// Null section already zeroed.

	// .text
	textNameOff := uint32(1) // index into shstrtab
	textSection := shdr[secHeaderSize : 2*secHeaderSize]
	binary.LittleEndian.PutUint32(textSection[0:], textNameOff)
	binary.LittleEndian.PutUint32(textSection[4:], 1) // SHT_PROGBITS
	binary.LittleEndian.PutUint64(textSection[8:], 0x6)
	binary.LittleEndian.PutUint64(textSection[16:], 0)                  // sh_addr
	binary.LittleEndian.PutUint64(textSection[24:], uint64(textOffset)) // sh_offset
	binary.LittleEndian.PutUint64(textSection[32:], uint64(len(code)))  // sh_size
	binary.LittleEndian.PutUint32(textSection[40:], 0)                  // sh_link
	binary.LittleEndian.PutUint32(textSection[44:], 0)                  // sh_info
	binary.LittleEndian.PutUint64(textSection[48:], textAlign)          // sh_addralign
	binary.LittleEndian.PutUint64(textSection[56:], 0)                  // sh_entsize

	// .shstrtab
	shstrNameOff := uint32(1 + len(".text") + 1)
	shstrSection := shdr[2*secHeaderSize : 3*secHeaderSize]
	binary.LittleEndian.PutUint32(shstrSection[0:], shstrNameOff)
	binary.LittleEndian.PutUint32(shstrSection[4:], 3) // SHT_STRTAB
	binary.LittleEndian.PutUint64(shstrSection[8:], 0)
	binary.LittleEndian.PutUint64(shstrSection[16:], 0)
	binary.LittleEndian.PutUint64(shstrSection[24:], uint64(shstrOffset))
	binary.LittleEndian.PutUint64(shstrSection[32:], uint64(len(shstr)))
	binary.LittleEndian.PutUint32(shstrSection[40:], 0)
	binary.LittleEndian.PutUint32(shstrSection[44:], 0)
	binary.LittleEndian.PutUint64(shstrSection[48:], 1) // sh_addralign
	binary.LittleEndian.PutUint64(shstrSection[56:], 0)

	return buf
}

func parseObjdumpOutput(out string) ([]DisasmLine, error) {
	scanner := bufio.NewScanner(strings.NewReader(out))
	var lines []DisasmLine
	for scanner.Scan() {
		line := scanner.Text()
		colon := strings.IndexRune(line, ':')
		if colon == -1 {
			continue
		}
		text := strings.TrimSpace(line[colon+1:])
		if text == "" || strings.HasPrefix(text, "<") {
			continue
		}
		if strings.HasPrefix(text, ".") || strings.HasPrefix(text, "file format") {
			continue
		}
		fields := strings.Fields(text)
		if len(fields) == 0 {
			continue
		}
		normalized := strings.Join(fields, " ")
		lines = append(lines, DisasmLine{
			Text:       text,
			Normalized: normalized,
			Mnemonic:   strings.ToLower(fields[0]),
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scanner error: %w", err)
	}
	return lines, nil
}

func align(value int, boundary int) int {
	if boundary <= 0 {
		return value
	}
	rem := value % boundary
	if rem == 0 {
		return value
	}
	return value + boundary - rem
}
