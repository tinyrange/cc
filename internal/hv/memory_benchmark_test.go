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
	memoryBenchmarkPageTablesOff  = 0x4000
	memoryBenchmarkPageTablesSize = 5 * 0x1000
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
	b.StopTimer()
	for i := 0; i < b.N; i++ {
		mapping, err := mapBenchmarkGuestFile(path, readMemoryBenchmarkSize)
		if err != nil {
			b.Fatal(err)
		}
		vm := newMappedBenchmarkVM(b, mapping.Bytes())
		b.StartTimer()
		if err := vm.RunUntilExit(); err != nil {
			b.StopTimer()
			_ = vm.Close()
			_ = mapping.Close()
			b.Fatal(err)
		}
		b.StopTimer()
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
	b.StopTimer()
	for i := 0; i < b.N; i++ {
		vm := newBenchmarkMicroVM(b, code)
		b.StartTimer()
		if err := vm.RunUntilExit(); err != nil {
			b.StopTimer()
			_ = vm.Close()
			b.Fatal(err)
		}
		b.StopTimer()
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
	installBenchmarkIdentityMap(b, vm.memory)
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
	installBenchmarkIdentityMap(b, vm.memory)
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

func installBenchmarkIdentityMap(b *testing.B, mem []byte) {
	b.Helper()
	if len(mem) < memoryBenchmarkPageTablesOff+memoryBenchmarkPageTablesSize {
		b.Fatalf("guest memory too small for benchmark page tables")
	}
	// WHP/arm64 is pathologically slow for this benchmark with stage-1 MMU
	// disabled, so install a small 4GiB identity map with Normal WB blocks.
	tables := mem[memoryBenchmarkPageTablesOff : memoryBenchmarkPageTablesOff+memoryBenchmarkPageTablesSize]
	clear(tables)
	l1 := tables[:0x1000]
	l2s := tables[0x1000:]
	const (
		tableDescriptor = 0x3
		blockDescriptor = 0x1 |
			(3 << 8) | // Inner shareable.
			(1 << 10) // Access flag.
	)
	for l1i := uint64(0); l1i < 4; l1i++ {
		l2GPA := memoryBenchmarkBase + uint64(memoryBenchmarkPageTablesOff) + 0x1000 + l1i*0x1000
		binary.LittleEndian.PutUint64(l1[l1i*8:], l2GPA|tableDescriptor)
		l2 := l2s[l1i*0x1000 : (l1i+1)*0x1000]
		for l2i := uint64(0); l2i < 512; l2i++ {
			pa := (l1i << 30) | (l2i << 21)
			binary.LittleEndian.PutUint64(l2[l2i*8:], pa|blockDescriptor)
		}
	}
}

func buildReadMemoryHashCode(dataAddr uint64, dataSize int, hashAddr uint64, exitAddr uint64) []byte {
	var a arm64Asm
	emitEnableBenchmarkMMU(&a)
	a.mov64(0, dataAddr)
	a.mov64(1, uint64(dataSize/256))
	a.mov64(2, memoryBenchmarkSeed)
	a.mov64(11, memoryBenchmarkSeed^0x9e3779b185ebca87)
	a.mov64(12, memoryBenchmarkSeed^0xc2b2ae3d27d4eb4f)
	a.mov64(13, memoryBenchmarkSeed^0x165667b19e3779f9)
	a.label("loop")
	a.cbz(1, "done")
	for off := uint32(0); off < 256; off += 64 {
		a.emit(ldpx(3, 4, 0, off))
		a.emit(ldpx(5, 6, 0, off+16))
		a.emit(ldpx(7, 8, 0, off+32))
		a.emit(ldpx(9, 10, 0, off+48))
		emitReadHashChunk(&a)
	}
	a.emit(addImm(0, 0, 256))
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

func emitReadHashChunk(a *arm64Asm) {
	a.emit(addRegShift(2, 2, 3, 0))
	a.emit(eorRegShift(11, 11, 4, arm64ShiftLSL, 0))
	a.emit(addRegShift(12, 12, 5, 0))
	a.emit(eorRegShift(13, 13, 6, arm64ShiftLSL, 0))
	a.emit(addRegShift(2, 2, 7, 0))
	a.emit(eorRegShift(11, 11, 8, arm64ShiftLSL, 0))
	a.emit(addRegShift(12, 12, 9, 0))
	a.emit(eorRegShift(13, 13, 10, arm64ShiftLSL, 0))
}

func buildWriteMemoryRandomCode(dataAddr uint64, dataSize int, exitAddr uint64) []byte {
	var a arm64Asm
	emitEnableBenchmarkMMU(&a)
	a.mov64(0, dataAddr)
	a.mov64(1, uint64(dataSize/64))
	a.mov64(2, memoryBenchmarkSeed)
	a.label("loop")
	a.cbz(1, "done")
	for off := uint32(0); off < 64; off += 16 {
		emitXorshift64(&a, 2)
		a.emit(addRegShift(3, 2, 31, 0))
		emitXorshift64(&a, 3)
		a.emit(stpx(2, 3, 0, off))
		a.emit(addRegShift(2, 3, 31, 0))
	}
	a.emit(addImm(0, 0, 64))
	a.emit(subImm(1, 1, 1))
	a.b("loop")
	a.label("done")
	a.mov64(5, exitAddr)
	a.emit(strx(2, 5, 0))
	return a.bytes()
}

func emitXorshift64(a *arm64Asm, reg uint32) {
	a.emit(eorRegShift(reg, reg, reg, arm64ShiftLSL, 13))
	a.emit(eorRegShift(reg, reg, reg, arm64ShiftLSR, 7))
	a.emit(eorRegShift(reg, reg, reg, arm64ShiftLSL, 17))
}

func emitEnableBenchmarkMMU(a *arm64Asm) {
	const (
		mairEL1 = 0xff
		// T0SZ=32, IRGN0/ORGN0=WB RA/WA, SH0=inner-shareable.
		tcrEL1   = 32 | (1 << 8) | (1 << 10) | (3 << 12)
		sctlrMMU = 1 | (1 << 2) | (1 << 12)
		ttbr0EL1 = memoryBenchmarkBase + memoryBenchmarkPageTablesOff
	)
	a.mov64(14, mairEL1)
	a.emit(msr(sysRegMAIREL1, 14))
	a.mov64(14, tcrEL1)
	a.emit(msr(sysRegTCREL1, 14))
	a.mov64(14, ttbr0EL1)
	a.emit(msr(sysRegTTBR0EL1, 14))
	a.emit(isb())
	a.emit(mrs(15, sysRegSCTLREL1))
	a.mov64(14, sctlrMMU)
	a.emit(orrRegShift(15, 15, 14, 0))
	a.emit(msr(sysRegSCTLREL1, 15))
	a.emit(isb())
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

type arm64SysReg uint32

const (
	sysRegSCTLREL1 arm64SysReg = 0xc080
	sysRegTCREL1   arm64SysReg = 0xc102
	sysRegTTBR0EL1 arm64SysReg = 0xc100
	sysRegMAIREL1  arm64SysReg = 0xc510
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

func orrRegShift(rd, rn, rm, shift uint32) uint32 {
	return 0xaa000000 | ((rm & 0x1f) << 16) | ((shift & 0x3f) << 10) | ((rn & 0x1f) << 5) | (rd & 0x1f)
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

func ldpx(rt, rt2, rn, imm uint32) uint32 {
	return 0xa9400000 | (((imm / 8) & 0x7f) << 15) | ((rt2 & 0x1f) << 10) | ((rn & 0x1f) << 5) | (rt & 0x1f)
}

func stpx(rt, rt2, rn, imm uint32) uint32 {
	return 0xa9000000 | (((imm / 8) & 0x7f) << 15) | ((rt2 & 0x1f) << 10) | ((rn & 0x1f) << 5) | (rt & 0x1f)
}

func mrs(rt uint32, reg arm64SysReg) uint32 {
	return 0xd5300000 | (uint32(reg) << 5) | (rt & 0x1f)
}

func msr(reg arm64SysReg, rt uint32) uint32 {
	return 0xd5100000 | (uint32(reg) << 5) | (rt & 0x1f)
}

func isb() uint32 {
	return 0xd5033fdf
}

func branch(insnOffset int) uint32 {
	return 0x14000000 | uint32(insnOffset)&0x03ffffff
}

func cbz(rt uint32, insnOffset int) uint32 {
	return 0xb4000000 | (uint32(insnOffset)&0x7ffff)<<5 | (rt & 0x1f)
}
