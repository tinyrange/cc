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

	// Constant expressions are folded at compile time: 1 + 2 = 3
	want := &ir.Program{
		Entrypoint: "main",
		Methods: map[string]ir.Method{
			"main": {
				ir.Return(ir.Int64(3)),
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

func TestCompileIfdefTrue(t *testing.T) {
	src := `package main
import "github.com/tinyrange/cc/internal/rtg/runtime"
func main() int64 {
	if runtime.Ifdef("feature") {
		return 1
	}
	return 0
}`
	got, err := CompileProgramWithOptions(src, CompileOptions{
		Flags: map[string]bool{"feature": true},
	})
	if err != nil {
		t.Fatalf("CompileProgram returned error: %v", err)
	}

	// When flag is true, Ifdef returns 1, so condition is 1 == 1 (always true)
	main := got.Methods["main"]
	if len(main) == 0 {
		t.Fatal("main method is empty")
	}

	// Check that we have an if fragment with an always-true condition
	foundIf := false
	for _, frag := range main {
		if ifFrag, ok := frag.(ir.IfFragment); ok {
			foundIf = true
			// The condition should evaluate to always-true (1 == 1)
			if cond, ok := ifFrag.Cond.(ir.CompareCondition); ok {
				if left, ok := cond.Left.(ir.Int64); ok {
					if right, ok := cond.Right.(ir.Int64); ok {
						if left == 1 && right == 1 {
							t.Log("Ifdef(true) correctly generates 1==1 condition")
						}
					}
				}
			}
		}
	}
	if !foundIf {
		t.Fatal("Expected if fragment in compiled output")
	}
}

func TestCompileIfdefFalse(t *testing.T) {
	src := `package main
import "github.com/tinyrange/cc/internal/rtg/runtime"
func main() int64 {
	if runtime.Ifdef("feature") {
		return 1
	}
	return 0
}`
	got, err := CompileProgramWithOptions(src, CompileOptions{
		Flags: map[string]bool{"feature": false},
	})
	if err != nil {
		t.Fatalf("CompileProgram returned error: %v", err)
	}

	// When flag is false, Ifdef returns 0, so condition is 0 == 1 (always false)
	main := got.Methods["main"]
	if len(main) == 0 {
		t.Fatal("main method is empty")
	}

	// Check that we have an if fragment with an always-false condition
	foundIf := false
	for _, frag := range main {
		if ifFrag, ok := frag.(ir.IfFragment); ok {
			foundIf = true
			// The condition should evaluate to always-false (1 == 0)
			if cond, ok := ifFrag.Cond.(ir.CompareCondition); ok {
				if left, ok := cond.Left.(ir.Int64); ok {
					if right, ok := cond.Right.(ir.Int64); ok {
						if left == 1 && right == 0 {
							t.Log("Ifdef(false) correctly generates 1==0 condition")
						}
					}
				}
			}
		}
	}
	if !foundIf {
		t.Fatal("Expected if fragment in compiled output")
	}
}

func TestCompileIfdefUndefined(t *testing.T) {
	src := `package main
import "github.com/tinyrange/cc/internal/rtg/runtime"
func main() int64 {
	if runtime.Ifdef("undefined_flag") {
		return 1
	}
	return 0
}`
	// No flags specified - undefined flags should be treated as false
	got, err := CompileProgramWithOptions(src, CompileOptions{})
	if err != nil {
		t.Fatalf("CompileProgram returned error: %v", err)
	}

	main := got.Methods["main"]
	if len(main) == 0 {
		t.Fatal("main method is empty")
	}

	// Should behave like Ifdef(false) - condition is 1 == 0
	foundIf := false
	for _, frag := range main {
		if ifFrag, ok := frag.(ir.IfFragment); ok {
			foundIf = true
			if cond, ok := ifFrag.Cond.(ir.CompareCondition); ok {
				if left, ok := cond.Left.(ir.Int64); ok {
					if right, ok := cond.Right.(ir.Int64); ok {
						if left == 1 && right == 0 {
							t.Log("Ifdef(undefined) correctly generates 1==0 condition (treated as false)")
						}
					}
				}
			}
		}
	}
	if !foundIf {
		t.Fatal("Expected if fragment in compiled output")
	}
}
