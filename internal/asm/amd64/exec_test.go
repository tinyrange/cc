//go:build linux && amd64

package amd64

import (
	"testing"
	"unsafe"

	"github.com/tinyrange/cc/internal/asm"
)

func TestPrepareAssemblyWithArgs(t *testing.T) {
	prog, err := EmitProgram(asm.Group{
		MovToMemory(Mem(Reg64(RDI)), Reg64(RSI)),
		MovToMemory(Mem(Reg64(RDI)).WithDisp(8), Reg64(RDX)),
		MovToMemory(Mem(Reg64(RDI)).WithDisp(16), Reg64(RCX)),
		MovToMemory(Mem(Reg64(RDI)).WithDisp(24), Reg64(R8)),
		MovToMemory(Mem(Reg64(RDI)).WithDisp(32), Reg64(R9)),
		Ret(),
	})
	if err != nil {
		t.Fatalf("EmitProgram failed: %v", err)
	}

	fn, release, err := PrepareAssemblyWithArgs(prog.Bytes(), prog.Relocations())
	if err != nil {
		t.Fatalf("PrepareAssemblyWithArgs failed: %v", err)
	}
	defer release()

	target := make([]uint64, 5)
	extra := uint64(0xfeedbead)

	fn.Call(&target[0], uint32(0x11223344), int16(0x5566), int64(0x778899aabbccdd), uintptr(0xdeadbeefcafebabe), &extra)

	if got, want := target[0], uint64(0x11223344); got != want {
		t.Fatalf("target[0]=0x%x, want 0x%x", got, want)
	}
	if got, want := target[1], uint64(0x5566); got != want {
		t.Fatalf("target[1]=0x%x, want 0x%x", got, want)
	}
	if got, want := target[2], uint64(0x778899aabbccdd); got != want {
		t.Fatalf("target[2]=0x%x, want 0x%x", got, want)
	}
	if got, want := target[3], uint64(0xdeadbeefcafebabe); got != want {
		t.Fatalf("target[3]=0x%x, want 0x%x", got, want)
	}
	if got, want := target[4], uint64(uintptr(unsafe.Pointer(&extra))); got != want {
		t.Fatalf("target[4]=0x%x, want 0x%x", got, want)
	}
}

func TestPrepareAssemblyWithArgsNoArguments(t *testing.T) {
	prog, err := EmitProgram(Ret())
	if err != nil {
		t.Fatalf("EmitProgram failed: %v", err)
	}

	fn, release, err := PrepareAssemblyWithArgs(prog.Bytes(), prog.Relocations())
	if err != nil {
		t.Fatalf("PrepareAssemblyWithArgs failed: %v", err)
	}
	defer release()

	fn.Call()
}
