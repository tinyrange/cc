//go:build linux && amd64

package amd64

import (
	"bytes"
	"encoding/binary"
	"testing"
	"unsafe"

	"github.com/tinyrange/cc/internal/asm"
	"github.com/tinyrange/cc/internal/asm/amd64"
	"github.com/tinyrange/cc/internal/ir"
	"golang.org/x/sys/unix"
)

func TestCompileMethodReturnsParam(t *testing.T) {
	method := ir.Method{
		ir.DeclareParam("value"),
		ir.Return(ir.Var("value")),
	}

	frag, err := Compile(method)
	if err != nil {
		t.Fatalf("compile method: %v", err)
	}

	fn := amd64.MustCompile(frag)
	got := int64(fn.Call(int64(42)))
	if got != 42 {
		t.Fatalf("expected 42, got %d", got)
	}
}

func TestCompileMethodIfElse(t *testing.T) {
	method := ir.Method{
		ir.DeclareParam("value"),
		ir.If(
			ir.IsNegative(ir.Var("value")),
			ir.Block{
				ir.Return(ir.Int64(-1)),
			},
			ir.Block{
				ir.Return(ir.Int64(1)),
			},
		),
	}

	frag, err := Compile(method)
	if err != nil {
		t.Fatalf("compile method: %v", err)
	}

	fn := amd64.MustCompile(frag)
	if got := int64(fn.Call(int64(-5))); got != -1 {
		t.Fatalf("negative branch: expected -1, got %d", got)
	}
	if got := int64(fn.Call(int64(7))); got != 1 {
		t.Fatalf("positive branch: expected 1, got %d", got)
	}
}

func TestCompileMethodIsEqualCondition(t *testing.T) {
	method := ir.Method{
		ir.DeclareParam("a"),
		ir.DeclareParam("b"),
		ir.If(
			ir.IsEqual(ir.Var("a"), ir.Var("b")),
			ir.Block{ir.Return(ir.Int64(1))},
			ir.Block{ir.Return(ir.Int64(0))},
		),
	}

	frag, err := Compile(method)
	if err != nil {
		t.Fatalf("compile method: %v", err)
	}

	fn := amd64.MustCompile(frag)
	if got := int64(fn.Call(int64(7), int64(7))); got != 1 {
		t.Fatalf("expected equal branch to return 1, got %d", got)
	}
	if got := int64(fn.Call(int64(7), int64(5))); got != 0 {
		t.Fatalf("expected not-equal branch to return 0, got %d", got)
	}
}

func TestCompileMethodIsNotEqualCondition(t *testing.T) {
	method := ir.Method{
		ir.DeclareParam("a"),
		ir.DeclareParam("b"),
		ir.If(
			ir.IsNotEqual(ir.Var("a"), ir.Var("b")),
			ir.Block{ir.Return(ir.Int64(42))},
			ir.Block{ir.Return(ir.Int64(-1))},
		),
	}

	frag, err := Compile(method)
	if err != nil {
		t.Fatalf("compile method: %v", err)
	}

	fn := amd64.MustCompile(frag)
	if got := int64(fn.Call(int64(9), int64(3))); got != 42 {
		t.Fatalf("expected not-equal branch to return 42, got %d", got)
	}
	if got := int64(fn.Call(int64(9), int64(9))); got != -1 {
		t.Fatalf("expected equal branch to return -1, got %d", got)
	}
}

func TestCompileMethodMemoryAccess(t *testing.T) {
	const initial = 0x1122334455667788
	buf := make([]uint64, 2)
	buf[0] = initial

	method := ir.Method{
		ir.DeclareParam("ptr"),
		ir.Assign(ir.Var("tmp"), ir.Var("ptr").AsMem()),
		ir.Assign(ir.Var("ptr").AsMem().WithDisp(8), ir.Var("tmp")),
		ir.Return(ir.Var("tmp")),
	}

	frag, err := Compile(method)
	if err != nil {
		t.Fatalf("compile method: %v", err)
	}

	fn := amd64.MustCompile(frag)
	ptr := uintptr(unsafe.Pointer(&buf[0]))
	got := uint64(fn.Call(ptr))
	if got != initial {
		t.Fatalf("expected read %x, got %x", initial, got)
	}
	if buf[1] != initial {
		t.Fatalf("expected write %x, got %x", initial, buf[1])
	}
}

func TestCompileMethodStoreByte(t *testing.T) {
	buf := [2]byte{0x7f, 0x55}

	method := ir.Method{
		ir.DeclareParam("ptr"),
		ir.Assign(ir.Var("value"), ir.Int64(0xAB)),
		ir.Assign(ir.Var("ptr").AsMem(), ir.Var("value").As8()),
		ir.Return(ir.Int64(0)),
	}

	frag, err := Compile(method)
	if err != nil {
		t.Fatalf("compile method: %v", err)
	}

	fn := amd64.MustCompile(frag)
	ptr := uintptr(unsafe.Pointer(&buf[0]))
	fn.Call(ptr)

	if buf[0] != 0xAB {
		t.Fatalf("expected first byte 0xAB, got 0x%02X", buf[0])
	}
	if buf[1] != 0x55 {
		t.Fatalf("expected second byte 0x55, got 0x%02X", buf[1])
	}
}

func TestCompileMethodSyscall(t *testing.T) {
	method := ir.Method{
		ir.Assign(ir.Var("pid"), ir.Syscall(unix.SYS_GETPID)),
		ir.Return(ir.Var("pid")),
	}

	frag, err := Compile(method)
	if err != nil {
		t.Fatalf("compile method: %v", err)
	}

	fn := amd64.MustCompile(frag)
	got := int64(fn.Call())
	if got <= 0 {
		t.Fatalf("expected pid > 0, got %d", got)
	}
}

func TestProgramGlobalsStoreAndLoad(t *testing.T) {
	accumulator := ir.Global("accumulator")
	prog := ir.Program{
		Entrypoint: "main",
		Methods: map[string]ir.Method{
			"main": {
				ir.Assign(accumulator.AsMem(), ir.Int64(0x1122334455667788)),
				ir.CallMethod("loadGlobal", ir.Var("result")),
				ir.Return(ir.Var("result")),
			},
			"loadGlobal": {
				ir.Return(accumulator.AsMem()),
			},
		},
		Globals: map[string]ir.GlobalConfig{
			accumulator.Name(): {Size: 8},
		},
	}

	asmProg, err := BuildStandaloneProgram(&prog)
	if err != nil {
		t.Fatalf("build program: %v", err)
	}
	if asmProg.BSSSize() < 8 {
		t.Fatalf("expected global BSS allocation, got %d", asmProg.BSSSize())
	}
	if containsGlobalPlaceholder(asmProg.Bytes()) {
		t.Fatalf("expected global pointers to be patched")
	}
}

func TestProgramGlobalsRequireDeclaration(t *testing.T) {
	missing := ir.Global("missing")
	token := globalPointerPlaceholder(missing.Name())
	if !isGlobalPointerToken(token) {
		t.Fatalf("placeholder detection failed for %x", token)
	}
	prog := ir.Program{
		Entrypoint: "main",
		Methods: map[string]ir.Method{
			"main": {
				ir.Assign(ir.Var("tmp"), missing.AsMem()),
				ir.Return(ir.Var("tmp")),
			},
		},
	}

	if _, err := BuildStandaloneProgram(&prog); err == nil {
		t.Fatalf("expected build to fail when global is undeclared")
	}
}

func containsGlobalPlaceholder(code []byte) bool {
	for idx := 0; idx+8 <= len(code); idx++ {
		value := binary.LittleEndian.Uint64(code[idx:])
		if isGlobalPointerToken(value) {
			return true
		}
	}
	return false
}

func TestAllocRegPreferSkipsUsed(t *testing.T) {
	c := &compiler{
		freeRegs: append([]asm.Variable(nil), initialFreeRegisters...),
		usedRegs: make(map[asm.Variable]bool),
	}

	first, err := c.allocRegPrefer(amd64.R10)
	if err != nil {
		t.Fatalf("alloc first preferred register: %v", err)
	}
	if first != amd64.R10 {
		t.Fatalf("expected first preferred register R10, got %v", first)
	}

	second, err := c.allocRegPrefer(amd64.R10, amd64.R11)
	if err != nil {
		t.Fatalf("alloc second preferred register: %v", err)
	}
	if second != amd64.R11 {
		t.Fatalf("expected allocator to skip R10 and select R11, got %v", second)
	}
}

func TestSyscallMultipleVarArguments(t *testing.T) {
	method := ir.Method{
		ir.DeclareParam("fd"),
		ir.DeclareParam("buf"),
		ir.DeclareParam("count"),
		ir.Assign(ir.Var("fdCopy"), ir.Var("fd")),
		ir.Assign(ir.Var("bufCopy"), ir.Var("buf")),
		ir.Assign(ir.Var("countCopy"), ir.Var("count")),
		ir.Assign(ir.Var("result"),
			ir.Syscall(
				unix.SYS_WRITE,
				ir.Var("fdCopy"),
				ir.Var("bufCopy"),
				ir.Var("countCopy"),
			),
		),
		ir.Return(ir.Var("result")),
	}

	frag, err := Compile(method)
	if err != nil {
		t.Fatalf("compile method: %v", err)
	}

	fn := amd64.MustCompile(frag)

	var fds [2]int
	if err := unix.Pipe(fds[:]); err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer unix.Close(fds[0])
	defer unix.Close(fds[1])

	message := []byte("register-ok")
	result := int64(fn.Call(
		uintptr(fds[1]),
		uintptr(unsafe.Pointer(&message[0])),
		uintptr(len(message)),
	))
	if result != int64(len(message)) {
		t.Fatalf("write returned %d, want %d", result, len(message))
	}

	buf := make([]byte, len(message))
	n, err := unix.Read(fds[0], buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if n != len(message) {
		t.Fatalf("read bytes %d, want %d", n, len(message))
	}
	if !bytes.Equal(buf[:n], message) {
		t.Fatalf("read data %q, want %q", buf[:n], message)
	}
}

func TestLoadConstantBytesHelper(t *testing.T) {
	constVar := asm.Variable(99)
	original := append([]byte(nil), []byte("hello\x00")...)
	data := append([]byte(nil), original...)

	method := ir.Method{
		ir.LoadConstantBytes(constVar, data),
		ir.Return(ir.Int64(0)),
	}

	// Mutate the original slice to ensure the helper copies the input.
	data[0] = 'x'

	frag, err := Compile(method)
	if err != nil {
		t.Fatalf("compile method: %v", err)
	}

	prog, err := amd64.EmitProgram(frag)
	if err != nil {
		t.Fatalf("emit program: %v", err)
	}

	code := prog.Bytes()
	if !bytes.Contains(code, original) {
		t.Fatalf("program missing constant data %q", original)
	}
	if bytes.Contains(code, data) {
		t.Fatalf("program unexpectedly contains mutated constant %q", data)
	}
}

func TestLoadConstantBytesConfigZeroTerminateAndLengths(t *testing.T) {
	constVar := asm.Variable(100)
	method := ir.Method{
		ir.LoadConstantBytesConfig(ir.ConstantBytesConfig{
			Target:        constVar,
			Data:          []byte("abc"),
			ZeroTerminate: true,
			Length:        ir.Var("len"),
			TotalLength:   ir.Var("totalLen"),
			Pointer:       ir.Var("ptr"),
		}),
		ir.Assign(
			ir.Var("result"),
			ir.Op(
				ir.OpAdd,
				ir.Op(ir.OpShl, ir.Var("totalLen"), ir.Int64(32)),
				ir.Var("len"),
			),
		),
		ir.Return(ir.Var("result")),
	}

	frag, err := Compile(method)
	if err != nil {
		t.Fatalf("compile method: %v", err)
	}

	prog, err := amd64.EmitProgram(frag)
	if err != nil {
		t.Fatalf("emit program: %v", err)
	}

	want := []byte{'a', 'b', 'c', 0}
	if !bytes.Contains(prog.Bytes(), want) {
		t.Fatalf("program missing zero-terminated constant %q", want)
	}

	fn := amd64.MustCompile(frag)
	got := int64(fn.Call())
	totalLen := got >> 32
	length := got & 0xffffffff
	if totalLen != 4 {
		t.Fatalf("total length = %d, want 4", totalLen)
	}
	if length != 3 {
		t.Fatalf("length = %d, want 3", length)
	}
}

func TestLoadConstantBytesConfigPointer(t *testing.T) {
	constVar := asm.Variable(101)
	method := ir.Method{
		ir.LoadConstantBytesConfig(ir.ConstantBytesConfig{
			Target:        constVar,
			Data:          []byte("hello"),
			ZeroTerminate: true,
			Pointer:       ir.Var("ptr"),
			TotalLength:   ir.Var("totalLen"),
		}),
		ir.Return(ir.Var("ptr")),
	}

	frag, err := Compile(method)
	if err != nil {
		t.Fatalf("compile method: %v", err)
	}

	fn := amd64.MustCompile(frag)
	ptr := uintptr(fn.Call())
	if ptr == 0 {
		t.Fatalf("constant pointer is nil")
	}

	data := unsafe.Slice((*byte)(unsafe.Pointer(ptr)), 6)
	if got, want := string(data), "hello\x00"; got != want {
		t.Fatalf("constant contents %q, want %q", got, want)
	}
}

func TestSyscallCheckedSuccess(t *testing.T) {
	method := ir.Method{
		ir.SyscallChecked(ir.SyscallCheckedConfig{
			Result: ir.Var("pid"),
			Number: unix.SYS_GETPID,
			OnError: ir.Block{
				ir.Return(ir.Int64(-1)),
			},
		}),
		ir.Return(ir.Var("pid")),
	}

	frag, err := Compile(method)
	if err != nil {
		t.Fatalf("compile method: %v", err)
	}

	fn := amd64.MustCompile(frag)
	got := int64(fn.Call())
	if got <= 0 {
		t.Fatalf("expected pid > 0, got %d", got)
	}
}

func TestSyscallCheckedErrorPath(t *testing.T) {
	const missingPath = "/definitely/not/present"

	method := ir.Method{
		ir.SyscallChecked(ir.SyscallCheckedConfig{
			Result: ir.Var("fd"),
			Number: unix.SYS_OPENAT,
			Args: []any{
				unix.AT_FDCWD,
				missingPath,
				unix.O_RDONLY,
				0,
			},
			OnError: ir.Block{
				ir.Return(ir.Var("fd")),
			},
		}),
		ir.Return(ir.Int64(0)),
	}

	frag, err := Compile(method)
	if err != nil {
		t.Fatalf("compile method: %v", err)
	}

	fn := amd64.MustCompile(frag)
	got := int64(fn.Call())
	if got != -int64(unix.ENOENT) {
		t.Fatalf("expected -ENOENT, got %d", got)
	}
}

func TestWriteStageResult(t *testing.T) {
	const (
		detail = uint32(0xdeadbeef)
		stage  = uint32(0x00123456)
	)
	value := int64((uint64(stage) << ir.DefaultStageResultShift) | uint64(detail))

	method := ir.Method{
		ir.DeclareParam("ptr"),
		ir.Assign(ir.Var("value"), ir.Int64(value)),
		ir.WriteStageResult(ir.StageResultStoreConfig{
			Base:   ir.Var("ptr").AsMem(),
			Offset: 0,
			Value:  ir.Var("value"),
		}),
		ir.Return(ir.Int64(0)),
	}

	frag, err := Compile(method)
	if err != nil {
		t.Fatalf("compile method: %v", err)
	}

	buf := make([]uint32, 4)
	fn := amd64.MustCompile(frag)
	ptr := uintptr(unsafe.Pointer(&buf[0]))
	fn.Call(ptr)

	if got := buf[0]; got != detail {
		t.Fatalf("detail write: got %#x, want %#x", got, detail)
	}
	if got := buf[1]; got != stage {
		t.Fatalf("stage write: got %#x, want %#x", got, stage)
	}
}

func TestWithStackSlot(t *testing.T) {
	const want = int64(0x0102030405060708)

	method := ir.Method{
		ir.WithStackSlot(ir.StackSlotConfig{
			Size: 16,
			Body: func(slot ir.StackSlot) ir.Fragment {
				return ir.Block{
					ir.Assign(slot.Base(), ir.Int64(want)),
					ir.Assign(ir.Var("result"), slot.Base()),
				}
			},
		}),
		ir.Return(ir.Var("result")),
	}

	frag, err := Compile(method)
	if err != nil {
		t.Fatalf("compile method: %v", err)
	}

	fn := amd64.MustCompile(frag)
	if got := int64(fn.Call()); got != want {
		t.Fatalf("stack slot load: got %#x, want %#x", got, want)
	}
	if got := int64(fn.Call()); got != want {
		t.Fatalf("second call mismatch: got %#x, want %#x", got, want)
	}
}
