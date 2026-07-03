package hv

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

const (
	memoryBenchmarkDataOff        = 0x10000
	readMemoryBenchmarkSize       = 512 << 20
	writeMemoryBenchmarkSize      = 64 << 20
	writeMemoryBenchmarkHashOff   = memoryBenchmarkDataOff + writeMemoryBenchmarkSize
	writeMemoryBenchmarkGuestSize = writeMemoryBenchmarkHashOff + 0x10000
	memoryBenchmarkSeed           = 0xcbf29ce484222325
)

func BenchmarkArm64VMReadMemory(b *testing.B) {
	dataSize := readMemoryBenchmarkSize - memoryBenchmarkDataOff - 0x10000
	hashOff := memoryBenchmarkDataOff + dataSize
	code := buildReadMemoryHashCode(
		memoryBenchmarkBase+memoryBenchmarkDataOff,
		dataSize,
		memoryBenchmarkBase+uint64(hashOff),
		memoryBenchmarkExitAddr,
	)
	image := make([]byte, readMemoryBenchmarkSize)
	copy(image, code)
	fillBenchmarkData(image[memoryBenchmarkDataOff:hashOff])
	want := guestReadHash(image[memoryBenchmarkDataOff:hashOff])
	tempDir := b.TempDir()
	path := filepath.Join(tempDir, "guest-memory.bin")
	if err := os.WriteFile(path, image, 0o600); err != nil {
		b.Fatal(err)
	}
	image = nil
	runtime.GC()

	b.SetBytes(int64(dataSize))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mapping, err := mapBenchmarkGuestFile(path, readMemoryBenchmarkSize)
		if err != nil {
			b.Fatal(err)
		}
		vm := newMappedBenchmarkVM(b, mapping.Bytes())
		if err := vm.RunUntilExit(); err != nil {
			_ = vm.Close()
			_ = mapping.Close()
			b.Fatal(err)
		}
		got := binary.LittleEndian.Uint64(vm.memory[hashOff:])
		if got != want {
			_ = vm.Close()
			_ = mapping.Close()
			b.Fatalf("guest hash = %#x, want %#x", got, want)
		}
		if err := vm.Close(); err != nil {
			_ = mapping.Close()
			b.Fatal(err)
		}
		if err := mapping.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkArm64VMWriteMemory(b *testing.B) {
	code := buildWriteMemoryRandomCode(
		memoryBenchmarkBase+memoryBenchmarkDataOff,
		writeMemoryBenchmarkSize,
		memoryBenchmarkExitAddr,
	)
	want := guestHashWords(makeGuestRandomBytes(writeMemoryBenchmarkSize))

	b.SetBytes(writeMemoryBenchmarkSize)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		vm := newBenchmarkMicroVM(b, code)
		if err := vm.RunUntilExit(); err != nil {
			_ = vm.Close()
			b.Fatal(err)
		}
		got := guestHashWords(vm.memory[memoryBenchmarkDataOff : memoryBenchmarkDataOff+writeMemoryBenchmarkSize])
		if got != want {
			_ = vm.Close()
			b.Fatalf("host hash = %#x, want %#x", got, want)
		}
		if err := vm.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func newBenchmarkMicroVM(b *testing.B, code []byte) *memoryBenchmarkVM {
	b.Helper()
	vm, err := newAnonymousMemoryBenchmarkVM(writeMemoryBenchmarkGuestSize)
	if errors.Is(err, errMemoryBenchmarkUnsupported) {
		b.Skip(err)
	}
	if err != nil {
		b.Fatal(err)
	}
	copy(vm.memory, code)
	if err := vm.SetEntry(memoryBenchmarkBase, memoryBenchmarkBase+writeMemoryBenchmarkGuestSize); err != nil {
		_ = vm.Close()
		b.Fatal(err)
	}
	return vm
}

func newMappedBenchmarkVM(b *testing.B, mem []byte) *memoryBenchmarkVM {
	b.Helper()
	vm, err := newMappedMemoryBenchmarkVM(mem)
	if errors.Is(err, errMemoryBenchmarkUnsupported) {
		b.Skip(err)
	}
	if err != nil {
		b.Fatal(err)
	}
	if err := vm.SetEntry(memoryBenchmarkBase, memoryBenchmarkBase+uint64(len(mem))); err != nil {
		_ = vm.Close()
		b.Fatal(err)
	}
	return vm
}

func fillBenchmarkData(data []byte) {
	var x uint64 = memoryBenchmarkSeed
	for off := 0; off < len(data); off += 8 {
		x = xorshift64(x)
		binary.LittleEndian.PutUint64(data[off:], x^uint64(off)*0x9e3779b185ebca87)
	}
}

func makeGuestRandomBytes(size int) []byte {
	data := make([]byte, size)
	var x uint64 = memoryBenchmarkSeed
	for off := 0; off < len(data); off += 8 {
		x = xorshift64(x)
		binary.LittleEndian.PutUint64(data[off:], x)
	}
	return data
}

func guestHashWords(data []byte) uint64 {
	h := uint64(memoryBenchmarkSeed)
	for off := 0; off < len(data); off += 8 {
		h += binary.LittleEndian.Uint64(data[off:])
		h += h << 10
		h ^= h >> 6
	}
	h += h << 3
	h ^= h >> 11
	h += h << 15
	return h
}

func guestReadHash(data []byte) uint64 {
	h0 := uint64(memoryBenchmarkSeed)
	h1 := uint64(memoryBenchmarkSeed ^ 0x9e3779b185ebca87)
	h2 := uint64(memoryBenchmarkSeed ^ 0xc2b2ae3d27d4eb4f)
	h3 := uint64(memoryBenchmarkSeed ^ 0x165667b19e3779f9)
	for off := 0; off < len(data); off += 64 {
		h0 += binary.LittleEndian.Uint64(data[off:])
		h1 ^= binary.LittleEndian.Uint64(data[off+8:])
		h2 += binary.LittleEndian.Uint64(data[off+16:])
		h3 ^= binary.LittleEndian.Uint64(data[off+24:])
		h0 += binary.LittleEndian.Uint64(data[off+32:])
		h1 ^= binary.LittleEndian.Uint64(data[off+40:])
		h2 += binary.LittleEndian.Uint64(data[off+48:])
		h3 ^= binary.LittleEndian.Uint64(data[off+56:])
	}
	h := h0 + h1 + h2
	h ^= h3
	h += h << 3
	h ^= h >> 11
	h += h << 15
	return h
}

func xorshift64(x uint64) uint64 {
	x ^= x << 13
	x ^= x >> 7
	x ^= x << 17
	return x
}

func buildReadMemoryHashCode(dataAddr uint64, dataSize int, hashAddr uint64, exitAddr uint64) []byte {
	var a arm64Asm
	a.mov64(0, dataAddr)
	a.mov64(1, uint64(dataSize/64))
	a.mov64(2, memoryBenchmarkSeed)
	a.mov64(11, memoryBenchmarkSeed^0x9e3779b185ebca87)
	a.mov64(12, memoryBenchmarkSeed^0xc2b2ae3d27d4eb4f)
	a.mov64(13, memoryBenchmarkSeed^0x165667b19e3779f9)
	a.label("loop")
	a.cbz(1, "done")
	a.emit(ldrx(3, 0, 0))
	a.emit(ldrx(4, 0, 8))
	a.emit(ldrx(5, 0, 16))
	a.emit(ldrx(6, 0, 24))
	a.emit(ldrx(7, 0, 32))
	a.emit(ldrx(8, 0, 40))
	a.emit(ldrx(9, 0, 48))
	a.emit(ldrx(10, 0, 56))
	a.emit(addImm(0, 0, 64))
	a.emit(addRegShift(2, 2, 3, 0))
	a.emit(eorRegShift(11, 11, 4, arm64ShiftLSL, 0))
	a.emit(addRegShift(12, 12, 5, 0))
	a.emit(eorRegShift(13, 13, 6, arm64ShiftLSL, 0))
	a.emit(addRegShift(2, 2, 7, 0))
	a.emit(eorRegShift(11, 11, 8, arm64ShiftLSL, 0))
	a.emit(addRegShift(12, 12, 9, 0))
	a.emit(eorRegShift(13, 13, 10, arm64ShiftLSL, 0))
	a.emit(subImm(1, 1, 1))
	a.b("loop")
	a.label("done")
	a.emit(addRegShift(2, 2, 11, 0))
	a.emit(addRegShift(2, 2, 12, 0))
	a.emit(eorRegShift(2, 2, 13, arm64ShiftLSL, 0))
	a.emit(addRegShift(2, 2, 2, 3))
	a.emit(eorRegShift(2, 2, 2, arm64ShiftLSR, 11))
	a.emit(addRegShift(2, 2, 2, 15))
	a.mov64(4, hashAddr)
	a.emit(strx(2, 4, 0))
	a.mov64(5, exitAddr)
	a.emit(strx(2, 5, 0))
	return a.bytes()
}

func buildWriteMemoryRandomCode(dataAddr uint64, dataSize int, exitAddr uint64) []byte {
	var a arm64Asm
	a.mov64(0, dataAddr)
	a.mov64(1, uint64(dataSize/8))
	a.mov64(2, memoryBenchmarkSeed)
	a.label("loop")
	a.cbz(1, "done")
	a.emit(eorRegShift(2, 2, 2, arm64ShiftLSL, 13))
	a.emit(eorRegShift(2, 2, 2, arm64ShiftLSR, 7))
	a.emit(eorRegShift(2, 2, 2, arm64ShiftLSL, 17))
	a.emit(strx(2, 0, 0))
	a.emit(addImm(0, 0, 8))
	a.emit(subImm(1, 1, 1))
	a.b("loop")
	a.label("done")
	a.mov64(5, exitAddr)
	a.emit(strx(2, 5, 0))
	return a.bytes()
}

type arm64Asm struct {
	insns  []uint32
	labels map[string]int
	fixups []arm64Fixup
}

type arm64Fixup struct {
	at    int
	label string
	kind  arm64FixupKind
	rt    uint32
}

type arm64FixupKind int

const (
	arm64FixupB arm64FixupKind = iota
	arm64FixupCBZ
)

func (a *arm64Asm) emit(insn uint32) {
	a.insns = append(a.insns, insn)
}

func (a *arm64Asm) label(name string) {
	if a.labels == nil {
		a.labels = make(map[string]int)
	}
	a.labels[name] = len(a.insns)
}

func (a *arm64Asm) b(label string) {
	a.fixups = append(a.fixups, arm64Fixup{at: len(a.insns), label: label, kind: arm64FixupB})
	a.emit(0)
}

func (a *arm64Asm) cbz(rt uint32, label string) {
	a.fixups = append(a.fixups, arm64Fixup{at: len(a.insns), label: label, kind: arm64FixupCBZ, rt: rt})
	a.emit(0)
}

func (a *arm64Asm) mov64(rd uint32, value uint64) {
	a.emit(movz(rd, uint16(value), 0))
	a.emit(movk(rd, uint16(value>>16), 1))
	a.emit(movk(rd, uint16(value>>32), 2))
	a.emit(movk(rd, uint16(value>>48), 3))
}

func (a *arm64Asm) bytes() []byte {
	for _, fixup := range a.fixups {
		target, ok := a.labels[fixup.label]
		if !ok {
			panic("missing arm64 label " + fixup.label)
		}
		off := target - fixup.at
		switch fixup.kind {
		case arm64FixupB:
			a.insns[fixup.at] = branch(off)
		case arm64FixupCBZ:
			a.insns[fixup.at] = cbz(fixup.rt, off)
		}
	}
	out := make([]byte, len(a.insns)*4)
	for i, insn := range a.insns {
		binary.LittleEndian.PutUint32(out[i*4:], insn)
	}
	return out
}

const (
	arm64ShiftLSL uint32 = 0
	arm64ShiftLSR uint32 = 1
)

func movz(rd uint32, imm uint16, hw uint32) uint32 {
	return 0xd2800000 | ((hw & 0x3) << 21) | (uint32(imm) << 5) | (rd & 0x1f)
}

func movk(rd uint32, imm uint16, hw uint32) uint32 {
	return 0xf2800000 | ((hw & 0x3) << 21) | (uint32(imm) << 5) | (rd & 0x1f)
}

func addImm(rd, rn, imm uint32) uint32 {
	return 0x91000000 | ((imm & 0xfff) << 10) | ((rn & 0x1f) << 5) | (rd & 0x1f)
}

func subImm(rd, rn, imm uint32) uint32 {
	return 0xd1000000 | ((imm & 0xfff) << 10) | ((rn & 0x1f) << 5) | (rd & 0x1f)
}

func addRegShift(rd, rn, rm, shift uint32) uint32 {
	return 0x8b000000 | ((rm & 0x1f) << 16) | ((shift & 0x3f) << 10) | ((rn & 0x1f) << 5) | (rd & 0x1f)
}

func eorRegShift(rd, rn, rm, shiftType, shift uint32) uint32 {
	return 0xca000000 | ((shiftType & 0x3) << 22) | ((rm & 0x1f) << 16) | ((shift & 0x3f) << 10) | ((rn & 0x1f) << 5) | (rd & 0x1f)
}

func ldrx(rt, rn, imm uint32) uint32 {
	return 0xf9400000 | (((imm / 8) & 0xfff) << 10) | ((rn & 0x1f) << 5) | (rt & 0x1f)
}

func strx(rt, rn, imm uint32) uint32 {
	return 0xf9000000 | (((imm / 8) & 0xfff) << 10) | ((rn & 0x1f) << 5) | (rt & 0x1f)
}

func branch(insnOffset int) uint32 {
	return 0x14000000 | uint32(insnOffset)&0x03ffffff
}

func cbz(rt uint32, insnOffset int) uint32 {
	return 0xb4000000 | (uint32(insnOffset)&0x7ffff)<<5 | (rt & 0x1f)
}
