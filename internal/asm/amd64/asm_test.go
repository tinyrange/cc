//go:build linux && amd64

package amd64

import (
	"bytes"
	"encoding/hex"
	"io"
	"os"
	"testing"

	"github.com/tinyrange/cc/internal/asm"
	"golang.org/x/sys/unix"
)

func TestASMFunctionCall(t *testing.T) {
	callee := asm.Label("callee")
	fn := MustCompile(asm.Group{
		MovImmediate(Reg64(RDI), 5),
		Call(callee),
		AddRegImm(Reg64(RAX), 1),
		Ret(),
		asm.MarkLabel(callee),
		MovReg(Reg64(RAX), Reg64(RDI)),
		AddRegImm(Reg64(RAX), 10),
		Ret(),
	})

	if got, want := fn.Call(), uintptr(16); got != want {
		t.Fatalf("Call()=0x%x, want 0x%x", got, want)
	}
}

func TestASMCallBetweenCompiledFunctions(t *testing.T) {
	callee := MustCompile(asm.Group{
		AddRegImm(Reg64(RDI), 2),
		MovReg(Reg64(RAX), Reg64(RDI)),
		Ret(),
	})

	caller := MustCompile(asm.Group{
		AddRegImm(Reg64(RDI), 5),
		MovImmediate(Reg64(R11), int64(callee.Entry())),
		CallReg(Reg64(R11)),
		AddRegImm(Reg64(RAX), 3),
		Ret(),
	})

	if got, want := caller.Call(4), uintptr(14); got != want {
		t.Fatalf("Call()=0x%x, want 0x%x", got, want)
	}
}

func TestASMPrintf(t *testing.T) {
	frag := asm.Group{
		MovImmediate(Reg64(RAX), 0x1234),
		MovImmediate(Reg64(RBX), 0xABCD),
		Printf("Value: rax=0x%x, rbx=0x%x\n", Reg64(RAX), Reg64(RBX)),
		Ret(),
	}

	prog, err := EmitProgram(frag)
	if err != nil {
		t.Fatalf("EmitProgram failed: %v", err)
	}

	fn, release, err := PrepareAssemblyWithArgs(prog.Bytes(), prog.Relocations())
	if err != nil {
		t.Fatalf("PrepareAssemblyWithArgs failed: %v", err)
	}
	defer release()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe failed: %v", err)
	}
	defer func() {
		_ = r.Close()
	}()

	origStdout, err := unix.Dup(1)
	if err != nil {
		_ = w.Close()
		t.Fatalf("dup stdout: %v", err)
	}
	defer func() {
		if origStdout >= 0 {
			_ = unix.Dup2(origStdout, 1)
			_ = unix.Close(origStdout)
		}
	}()

	if err := unix.Dup2(int(w.Fd()), 1); err != nil {
		_ = w.Close()
		t.Fatalf("redirect stdout: %v", err)
	}

	fn.Call()

	if err := unix.Dup2(origStdout, 1); err != nil {
		_ = w.Close()
		t.Fatalf("restore stdout: %v", err)
	}
	_ = unix.Close(origStdout)
	origStdout = -1

	_ = w.Close()
	output, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}

	const want = "Value: rax=0x1234, rbx=0xabcd\n"
	if got := string(output); got != want {
		t.Fatalf("unexpected stdout: got %q want %q", got, want)
	}

	t.Logf("native stdout: %s", string(output))
}

func expectPrefix(t *testing.T, code []byte, prefixHex string) {
	t.Helper()
	expect, err := hex.DecodeString(prefixHex)
	if err != nil {
		t.Fatalf("invalid hex prefix %q: %v", prefixHex, err)
	}
	if !bytes.HasPrefix(code, expect) {
		t.Fatalf("unexpected instruction prefix:\n got: %x\nwant: %x", code[:len(expect)], expect)
	}
}
