package rtg

import (
	"reflect"
	"testing"

	"github.com/tinyrange/cc/internal/ir"
	"github.com/tinyrange/cc/internal/linux/defs"
)

func TestCompileReturnLiteral(t *testing.T) {
	src := `package main
func main() int64 {
	return 7
}`
	got, err := CompileProgram(src)
	if err != nil {
		t.Fatalf("CompileProgram returned error: %v", err)
	}

	want := &ir.Program{
		Entrypoint: "main",
		Methods: map[string]ir.Method{
			"main": {
				ir.Return(ir.Int64(7)),
			},
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected program\n got: %#v\nwant: %#v", got, want)
	}
}

func TestCompileBinaryAdd(t *testing.T) {
	src := `package main
func main() int64 {
	return 1 + 2
}`
	got, err := CompileProgram(src)
	if err != nil {
		t.Fatalf("CompileProgram returned error: %v", err)
	}

	want := &ir.Program{
		Entrypoint: "main",
		Methods: map[string]ir.Method{
			"main": {
				ir.Return(ir.Op(ir.OpAdd, ir.Int64(1), ir.Int64(2))),
			},
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected program\n got: %#v\nwant: %#v", got, want)
	}
}

func TestCompileSyscallAssign(t *testing.T) {
	src := `package main
func main() int64 {
	fd := syscall(1, 2, 3)
	return fd
}`
	got, err := CompileProgram(src)
	if err != nil {
		t.Fatalf("CompileProgram returned error: %v", err)
	}

	want := &ir.Program{
		Entrypoint: "main",
		Methods: map[string]ir.Method{
			"main": {
				ir.Assign(ir.Var("fd"), ir.Syscall(defs.Syscall(1), ir.Int64(2), ir.Int64(3))),
				ir.Return(ir.Var("fd")),
			},
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected program\n got: %#v\nwant: %#v", got, want)
	}
}
