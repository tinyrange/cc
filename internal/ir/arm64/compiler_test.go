package arm64

import (
	"bytes"
	"go/parser"
	"go/token"
	"reflect"
	"strings"
	"testing"

	"github.com/tinyrange/cc/internal/asm"
	arm64asm "github.com/tinyrange/cc/internal/asm/arm64"
	"github.com/tinyrange/cc/internal/ir"
)

func TestCompilerDoesNotImportAMD64Assembler(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "compiler.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse compiler.go: %v", err)
	}
	for _, imp := range file.Imports {
		path := strings.Trim(imp.Path.Value, "\"")
		if path == "github.com/tinyrange/cc/internal/asm/amd64" {
			t.Fatalf("arm64 compiler should not import amd64 assembler (found %q)", path)
		}
	}
}

func TestRegisterMappingsMatchABI(t *testing.T) {
	wantParams := []asm.Variable{
		arm64asm.X0,
		arm64asm.X1,
		arm64asm.X2,
		arm64asm.X3,
		arm64asm.X4,
		arm64asm.X5,
		arm64asm.X6,
		arm64asm.X7,
	}
	if !reflect.DeepEqual(paramRegisters, wantParams) {
		t.Fatalf("paramRegisters = %v, want %v", paramRegisters, wantParams)
	}

	wantSyscall := []asm.Variable{
		arm64asm.X0,
		arm64asm.X1,
		arm64asm.X2,
		arm64asm.X3,
		arm64asm.X4,
		arm64asm.X5,
	}
	if !reflect.DeepEqual(syscallArgRegisters, wantSyscall) {
		t.Fatalf("syscallArgRegisters = %v, want %v", syscallArgRegisters, wantSyscall)
	}

	for _, reg := range initialFreeRegisters {
		if reg < arm64asm.X0 || reg > arm64asm.X30 {
			t.Fatalf("initialFreeRegisters contains non-general register %d", reg)
		}
	}
}

func TestCompileAddProducesArm64Instructions(t *testing.T) {
	method := ir.Method{
		ir.DeclareParam("a"),
		ir.DeclareParam("b"),
		ir.Assign(ir.Var("sum"), ir.Op(ir.OpAdd, ir.Var("a"), ir.Var("b"))),
		ir.Return(ir.Var("sum")),
	}

	code := emitMethodBytes(t, method)
	if len(code)%4 != 0 {
		t.Fatalf("arm64 instructions must be 4 bytes, len=%d", len(code))
	}
	if bytes.Contains(code, []byte{0xC3}) {
		t.Fatalf("found x86 RET opcode (0xC3) in arm64 output %x", code)
	}

	retBytes := emitFragmentBytes(t, arm64asm.Ret())
	if !bytes.HasSuffix(code, retBytes) {
		t.Fatalf("expected program to end with RET (%x), bytes=%x", retBytes, code[len(code)-len(retBytes):])
	}
}

func TestCompileAdjustsStackViaSP(t *testing.T) {
	method := ir.Method{
		ir.Assign(ir.Var("tmp0"), ir.Int64(1)),
		ir.Assign(ir.Var("tmp1"), ir.Int64(2)),
		ir.Assign(ir.Var("tmp2"), ir.Int64(3)),
		ir.Assign(ir.Var("tmp3"), ir.Int64(4)),
		ir.Assign(ir.Var("tmp4"), ir.Int64(5)),
		ir.Return(ir.Int64(0)),
	}

	c, err := newCompiler(method)
	if err != nil {
		t.Fatalf("build compiler: %v", err)
	}
	frameSize := c.frameSize
	if frameSize == 0 {
		t.Fatalf("expected non-zero frame size for locals, got 0")
	}

	code := emitMethodBytes(t, method)
	expectPrologue := emitFragmentBytes(t, arm64asm.AddRegImm(arm64asm.Reg64(arm64asm.SP), -frameSize))
	if !bytes.HasPrefix(code, expectPrologue) {
		t.Fatalf("expected prologue %x, got %x", expectPrologue, code[:len(expectPrologue)])
	}
}

func TestCompileSyscallEmitsSvc(t *testing.T) {
	method := ir.Method{
		ir.Return(ir.Syscall(64, ir.Int64(1), ir.Int64(0), ir.Int64(0))),
	}
	code := emitMethodBytes(t, method)
	svc := []byte{0x01, 0x00, 0x00, 0xd4}
	if !bytes.Contains(code, svc) {
		t.Fatalf("expected svc #0 (d4000001) in program, bytes=%x", code)
	}
}

func emitMethodBytes(t *testing.T, method ir.Method) []byte {
	t.Helper()
	frag, err := Compile(method)
	if err != nil {
		t.Fatalf("compile method: %v", err)
	}
	return emitFragmentBytes(t, frag)
}

func emitFragmentBytes(t *testing.T, frag asm.Fragment) []byte {
	t.Helper()
	prog, err := arm64asm.EmitProgram(frag)
	if err != nil {
		t.Fatalf("emit program: %v", err)
	}
	return prog.Bytes()
}
