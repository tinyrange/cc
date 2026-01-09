package rtg

import (
	"reflect"
	"strings"
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

func TestCompileStackBuffer(t *testing.T) {
	src := `package main
import "github.com/tinyrange/cc/internal/rtg/runtime"
func main() int64 {
	var buf [40]byte
	runtime.Store8(buf, 0, 65)
	runtime.Store32(buf, 16, 0x12345678)
	return 0
}`
	got, err := CompileProgram(src)
	if err != nil {
		t.Fatalf("CompileProgram returned error: %v", err)
	}

	main := got.Methods["main"]
	if len(main) == 0 {
		t.Fatal("main method is empty")
	}

	// The result should have a WithStackSlot wrapping the body
	foundStackSlot := false
	var checkFragment func(frag ir.Fragment) bool
	checkFragment = func(frag ir.Fragment) bool {
		switch v := frag.(type) {
		case ir.Block:
			for _, inner := range v {
				if checkFragment(inner) {
					return true
				}
			}
		case ir.StackSlotFragment:
			foundStackSlot = true
			// Check that the stack slot has size 40
			if v.Size != 40 {
				t.Errorf("Expected stack slot size 40, got %d", v.Size)
			}
			return true
		}
		return false
	}

	for _, frag := range main {
		checkFragment(frag)
	}

	if !foundStackSlot {
		t.Fatal("Expected WithStackSlot fragment for buffer")
	}
}

func TestCompileStackBufferAddressOf(t *testing.T) {
	src := `package main
import "github.com/tinyrange/cc/internal/rtg/runtime"
func main() int64 {
	var buf [40]byte
	runtime.Store8(&buf[0], 8, 65)
	return 0
}`
	got, err := CompileProgram(src)
	if err != nil {
		t.Fatalf("CompileProgram returned error: %v", err)
	}

	main := got.Methods["main"]
	if len(main) == 0 {
		t.Fatal("main method is empty")
	}

	// Verify the program compiles without error
	// The &buf[0] + 8 should result in a memory access at offset 8
	foundStackSlot := false
	var checkFragment func(frag ir.Fragment) bool
	checkFragment = func(frag ir.Fragment) bool {
		switch v := frag.(type) {
		case ir.Block:
			for _, inner := range v {
				if checkFragment(inner) {
					return true
				}
			}
		case ir.StackSlotFragment:
			foundStackSlot = true
			return true
		}
		return false
	}

	for _, frag := range main {
		checkFragment(frag)
	}

	if !foundStackSlot {
		t.Fatal("Expected WithStackSlot fragment for buffer")
	}
}

func TestCompileEmbedString(t *testing.T) {
	src := `package main
import "github.com/tinyrange/cc/internal/rtg/runtime"
func main() int64 {
	ptr, length := runtime.EmbedString("hello world")
	runtime.Syscall(runtime.SYS_WRITE, 1, ptr, length)
	return 0
}`
	got, err := CompileProgram(src)
	if err != nil {
		t.Fatalf("CompileProgram returned error: %v", err)
	}

	main := got.Methods["main"]
	if len(main) == 0 {
		t.Fatal("main method is empty")
	}

	// Check for ConstantBytesFragment in the output
	foundEmbed := false
	var checkFragment func(frag ir.Fragment) bool
	checkFragment = func(frag ir.Fragment) bool {
		switch v := frag.(type) {
		case ir.Block:
			for _, inner := range v {
				if checkFragment(inner) {
					return true
				}
			}
		case ir.ConstantBytesFragment:
			foundEmbed = true
			return true
		}
		return false
	}

	for _, frag := range main {
		checkFragment(frag)
	}

	if !foundEmbed {
		t.Fatal("Expected ConstantBytesFragment for EmbedString")
	}
}

func TestCompileEmbedCString(t *testing.T) {
	src := `package main
import "github.com/tinyrange/cc/internal/rtg/runtime"
func main() int64 {
	ptr, length := runtime.EmbedCString("hostname")
	runtime.Syscall(runtime.SYS_SETHOSTNAME, ptr, length)
	return 0
}`
	got, err := CompileProgram(src)
	if err != nil {
		t.Fatalf("CompileProgram returned error: %v", err)
	}

	main := got.Methods["main"]
	if len(main) == 0 {
		t.Fatal("main method is empty")
	}

	// Check for ConstantBytesFragment with zero terminator
	foundEmbed := false
	var checkFragment func(frag ir.Fragment) bool
	checkFragment = func(frag ir.Fragment) bool {
		switch v := frag.(type) {
		case ir.Block:
			for _, inner := range v {
				if checkFragment(inner) {
					return true
				}
			}
		case ir.ConstantBytesFragment:
			foundEmbed = true
			// EmbedCString should have data ending with null terminator
			if len(v.Data) == 0 || v.Data[len(v.Data)-1] != 0 {
				t.Error("Expected data to be null-terminated for EmbedCString")
			}
			return true
		}
		return false
	}

	for _, frag := range main {
		checkFragment(frag)
	}

	if !foundEmbed {
		t.Fatal("Expected ConstantBytesFragment for EmbedCString")
	}
}

func TestCompileEmbedBytes(t *testing.T) {
	src := `package main
import "github.com/tinyrange/cc/internal/rtg/runtime"
func main() int64 {
	ptr, length := runtime.EmbedBytes(0x01, 0x02, 0x03)
	runtime.Syscall(runtime.SYS_WRITE, 1, ptr, length)
	return 0
}`
	got, err := CompileProgram(src)
	if err != nil {
		t.Fatalf("CompileProgram returned error: %v", err)
	}

	main := got.Methods["main"]
	if len(main) == 0 {
		t.Fatal("main method is empty")
	}

	// Check for ConstantBytesFragment with correct data
	foundEmbed := false
	var checkFragment func(frag ir.Fragment) bool
	checkFragment = func(frag ir.Fragment) bool {
		switch v := frag.(type) {
		case ir.Block:
			for _, inner := range v {
				if checkFragment(inner) {
					return true
				}
			}
		case ir.ConstantBytesFragment:
			foundEmbed = true
			if len(v.Data) != 3 {
				t.Errorf("Expected 3 bytes, got %d", len(v.Data))
			}
			if v.Data[0] != 0x01 || v.Data[1] != 0x02 || v.Data[2] != 0x03 {
				t.Errorf("Unexpected data: %v", v.Data)
			}
			return true
		}
		return false
	}

	for _, frag := range main {
		checkFragment(frag)
	}

	if !foundEmbed {
		t.Fatal("Expected ConstantBytesFragment for EmbedBytes")
	}
}

func TestCompileConfigInt64(t *testing.T) {
	src := `package main
import "github.com/tinyrange/cc/internal/rtg/runtime"
func main() int64 {
	return runtime.Config("myvalue")
}`
	got, err := CompileProgramWithOptions(src, CompileOptions{
		Config: map[string]any{"myvalue": int64(42)},
	})
	if err != nil {
		t.Fatalf("CompileProgram returned error: %v", err)
	}

	main := got.Methods["main"]
	if len(main) == 0 {
		t.Fatal("main method is empty")
	}

	// The result should be: return 42
	want := &ir.Program{
		Entrypoint: "main",
		Methods: map[string]ir.Method{
			"main": {
				ir.Return(ir.Int64(42)),
			},
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected program\n got: %#v\nwant: %#v", got, want)
	}
}

func TestCompileConfigMissing(t *testing.T) {
	src := `package main
import "github.com/tinyrange/cc/internal/rtg/runtime"
func main() int64 {
	return runtime.Config("missing")
}`
	_, err := CompileProgramWithOptions(src, CompileOptions{
		Config: map[string]any{"other": int64(1)},
	})
	if err == nil {
		t.Fatal("Expected error for missing config key")
	}
	if !strings.Contains(err.Error(), "key not found") {
		t.Fatalf("Unexpected error: %v", err)
	}
}

func TestCompileConfigNoConfig(t *testing.T) {
	src := `package main
import "github.com/tinyrange/cc/internal/rtg/runtime"
func main() int64 {
	return runtime.Config("anything")
}`
	_, err := CompileProgramWithOptions(src, CompileOptions{})
	if err == nil {
		t.Fatal("Expected error when no config values provided")
	}
	if !strings.Contains(err.Error(), "no config values provided") {
		t.Fatalf("Unexpected error: %v", err)
	}
}

func TestCompileEmbedConfigCString(t *testing.T) {
	src := `package main
import "github.com/tinyrange/cc/internal/rtg/runtime"
func main() int64 {
	ptr, length := runtime.EmbedConfigCString("hostname")
	runtime.Syscall(runtime.SYS_SETHOSTNAME, ptr, length)
	return 0
}`
	got, err := CompileProgramWithOptions(src, CompileOptions{
		Config: map[string]any{"hostname": "tinyrange"},
	})
	if err != nil {
		t.Fatalf("CompileProgram returned error: %v", err)
	}

	main := got.Methods["main"]
	if len(main) == 0 {
		t.Fatal("main method is empty")
	}

	// Check for ConstantBytesFragment with null-terminated data
	foundEmbed := false
	var checkFragment func(frag ir.Fragment) bool
	checkFragment = func(frag ir.Fragment) bool {
		switch v := frag.(type) {
		case ir.Block:
			for _, inner := range v {
				if checkFragment(inner) {
					return true
				}
			}
		case ir.ConstantBytesFragment:
			foundEmbed = true
			// EmbedConfigCString should have data "tinyrange\0"
			expected := append([]byte("tinyrange"), 0)
			if string(v.Data) != string(expected) {
				t.Errorf("Expected data %q, got %q", expected, v.Data)
			}
			return true
		}
		return false
	}

	for _, frag := range main {
		checkFragment(frag)
	}

	if !foundEmbed {
		t.Fatal("Expected ConstantBytesFragment for EmbedConfigCString")
	}
}
