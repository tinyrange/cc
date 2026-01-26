//go:build (darwin || linux) && arm64

package arm64

import (
	"testing"

	arm64asm "github.com/tinyrange/cc/internal/asm/arm64"
	"github.com/tinyrange/cc/internal/ir"
)

// TestNativeExecution tests the IR compiler by running generated code natively.
// This catches IR compiler bugs without needing to boot a VM.

func compileAndRun(t *testing.T, method ir.Method, args ...any) uintptr {
	t.Helper()

	prog := &ir.Program{
		Entrypoint: "main",
		Methods: map[string]ir.Method{
			"main": method,
		},
	}

	asmProg, err := BuildStandaloneProgram(prog)
	if err != nil {
		t.Fatalf("BuildStandaloneProgram failed: %v", err)
	}

	fn, cleanup, err := arm64asm.PrepareAssemblyWithArgs(asmProg.Bytes(), asmProg.Relocations(), asmProg.BSSSize())
	if err != nil {
		t.Fatalf("PrepareAssemblyWithArgs failed: %v", err)
	}
	defer cleanup()

	return fn.Call(args...)
}

// Basic arithmetic tests

func TestNative_ReturnConstant(t *testing.T) {
	result := compileAndRun(t, ir.Method{
		ir.Return(ir.Int64(42)),
	})
	if result != 42 {
		t.Errorf("Expected 42, got %d", result)
	}
}

func TestNative_ReturnZero(t *testing.T) {
	result := compileAndRun(t, ir.Method{
		ir.Return(ir.Int64(0)),
	})
	if result != 0 {
		t.Errorf("Expected 0, got %d", result)
	}
}

func TestNative_ReturnNegative(t *testing.T) {
	result := compileAndRun(t, ir.Method{
		ir.Return(ir.Int64(-1)),
	})
	if int64(result) != -1 {
		t.Errorf("Expected -1, got %d", int64(result))
	}
}

func TestNative_ReturnLargeConstant(t *testing.T) {
	result := compileAndRun(t, ir.Method{
		ir.Return(ir.Int64(0x123456789ABCDEF0)),
	})
	if result != 0x123456789ABCDEF0 {
		t.Errorf("Expected 0x123456789ABCDEF0, got 0x%x", result)
	}
}

// Variable assignment and retrieval

func TestNative_AssignAndReturn(t *testing.T) {
	x := ir.Var("x")
	result := compileAndRun(t, ir.Method{
		ir.Assign(x, ir.Int64(100)),
		ir.Return(x),
	})
	if result != 100 {
		t.Errorf("Expected 100, got %d", result)
	}
}

func TestNative_MultipleAssignments(t *testing.T) {
	a := ir.Var("a")
	b := ir.Var("b")
	c := ir.Var("c")
	result := compileAndRun(t, ir.Method{
		ir.Assign(a, ir.Int64(10)),
		ir.Assign(b, ir.Int64(20)),
		ir.Assign(c, ir.Int64(30)),
		ir.Return(c),
	})
	if result != 30 {
		t.Errorf("Expected 30, got %d", result)
	}
}

func TestNative_Reassignment(t *testing.T) {
	x := ir.Var("x")
	result := compileAndRun(t, ir.Method{
		ir.Assign(x, ir.Int64(1)),
		ir.Assign(x, ir.Int64(2)),
		ir.Assign(x, ir.Int64(3)),
		ir.Return(x),
	})
	if result != 3 {
		t.Errorf("Expected 3, got %d", result)
	}
}

// Arithmetic operations

func TestNative_Add(t *testing.T) {
	a := ir.Var("a")
	b := ir.Var("b")
	result := compileAndRun(t, ir.Method{
		ir.Assign(a, ir.Int64(10)),
		ir.Assign(b, ir.Int64(20)),
		ir.Return(ir.Op(ir.OpAdd, a, b)),
	})
	if result != 30 {
		t.Errorf("Expected 30, got %d", result)
	}
}

func TestNative_Sub(t *testing.T) {
	a := ir.Var("a")
	b := ir.Var("b")
	result := compileAndRun(t, ir.Method{
		ir.Assign(a, ir.Int64(50)),
		ir.Assign(b, ir.Int64(20)),
		ir.Return(ir.Op(ir.OpSub, a, b)),
	})
	if result != 30 {
		t.Errorf("Expected 30, got %d", result)
	}
}

func TestNative_Mul(t *testing.T) {
	t.Skip("OpMul not yet implemented in ARM64 IR compiler")
	a := ir.Var("a")
	b := ir.Var("b")
	result := compileAndRun(t, ir.Method{
		ir.Assign(a, ir.Int64(6)),
		ir.Assign(b, ir.Int64(7)),
		ir.Return(ir.Op(ir.OpMul, a, b)),
	})
	if result != 42 {
		t.Errorf("Expected 42, got %d", result)
	}
}

func TestNative_Div(t *testing.T) {
	t.Skip("OpDiv not yet implemented in ARM64 IR compiler")
	a := ir.Var("a")
	b := ir.Var("b")
	result := compileAndRun(t, ir.Method{
		ir.Assign(a, ir.Int64(100)),
		ir.Assign(b, ir.Int64(10)),
		ir.Return(ir.Op(ir.OpDiv, a, b)),
	})
	if result != 10 {
		t.Errorf("Expected 10, got %d", result)
	}
}

// Bitwise operations

func TestNative_And(t *testing.T) {
	a := ir.Var("a")
	b := ir.Var("b")
	result := compileAndRun(t, ir.Method{
		ir.Assign(a, ir.Int64(0xFF00)),
		ir.Assign(b, ir.Int64(0x0FF0)),
		ir.Return(ir.Op(ir.OpAnd, a, b)),
	})
	if result != 0x0F00 {
		t.Errorf("Expected 0x0F00, got 0x%x", result)
	}
}

func TestNative_Or(t *testing.T) {
	a := ir.Var("a")
	b := ir.Var("b")
	result := compileAndRun(t, ir.Method{
		ir.Assign(a, ir.Int64(0xFF00)),
		ir.Assign(b, ir.Int64(0x00FF)),
		ir.Return(ir.Op(ir.OpOr, a, b)),
	})
	if result != 0xFFFF {
		t.Errorf("Expected 0xFFFF, got 0x%x", result)
	}
}

func TestNative_Xor(t *testing.T) {
	a := ir.Var("a")
	b := ir.Var("b")
	result := compileAndRun(t, ir.Method{
		ir.Assign(a, ir.Int64(0xFF00)),
		ir.Assign(b, ir.Int64(0xF0F0)),
		ir.Return(ir.Op(ir.OpXor, a, b)),
	})
	if result != 0x0FF0 {
		t.Errorf("Expected 0x0FF0, got 0x%x", result)
	}
}

func TestNative_Shl(t *testing.T) {
	a := ir.Var("a")
	result := compileAndRun(t, ir.Method{
		ir.Assign(a, ir.Int64(1)),
		ir.Return(ir.Op(ir.OpShl, a, ir.Int64(4))),
	})
	if result != 16 {
		t.Errorf("Expected 16, got %d", result)
	}
}

func TestNative_Shr(t *testing.T) {
	a := ir.Var("a")
	result := compileAndRun(t, ir.Method{
		ir.Assign(a, ir.Int64(256)),
		ir.Return(ir.Op(ir.OpShr, a, ir.Int64(4))),
	})
	if result != 16 {
		t.Errorf("Expected 16, got %d", result)
	}
}

// Control flow - If/Then

func TestNative_IfTrue(t *testing.T) {
	x := ir.Var("x")
	result := compileAndRun(t, ir.Method{
		ir.Assign(x, ir.Int64(0)),
		ir.If(ir.IsZero(x), ir.Return(ir.Int64(1))),
		ir.Return(ir.Int64(0)),
	})
	if result != 1 {
		t.Errorf("Expected 1, got %d", result)
	}
}

func TestNative_IfFalse(t *testing.T) {
	x := ir.Var("x")
	result := compileAndRun(t, ir.Method{
		ir.Assign(x, ir.Int64(5)),
		ir.If(ir.IsZero(x), ir.Return(ir.Int64(1))),
		ir.Return(ir.Int64(0)),
	})
	if result != 0 {
		t.Errorf("Expected 0, got %d", result)
	}
}

func TestNative_IfNegative(t *testing.T) {
	x := ir.Var("x")
	result := compileAndRun(t, ir.Method{
		ir.Assign(x, ir.Int64(-5)),
		ir.If(ir.IsNegative(x), ir.Return(ir.Int64(1))),
		ir.Return(ir.Int64(0)),
	})
	if result != 1 {
		t.Errorf("Expected 1, got %d", result)
	}
}

func TestNative_IfNotNegative(t *testing.T) {
	x := ir.Var("x")
	result := compileAndRun(t, ir.Method{
		ir.Assign(x, ir.Int64(5)),
		ir.If(ir.IsNegative(x), ir.Return(ir.Int64(1))),
		ir.Return(ir.Int64(0)),
	})
	if result != 0 {
		t.Errorf("Expected 0, got %d", result)
	}
}

func TestNative_IfEqual(t *testing.T) {
	a := ir.Var("a")
	b := ir.Var("b")
	result := compileAndRun(t, ir.Method{
		ir.Assign(a, ir.Int64(42)),
		ir.Assign(b, ir.Int64(42)),
		ir.If(ir.IsEqual(a, b), ir.Return(ir.Int64(1))),
		ir.Return(ir.Int64(0)),
	})
	if result != 1 {
		t.Errorf("Expected 1, got %d", result)
	}
}

func TestNative_IfNotEqual(t *testing.T) {
	a := ir.Var("a")
	b := ir.Var("b")
	result := compileAndRun(t, ir.Method{
		ir.Assign(a, ir.Int64(42)),
		ir.Assign(b, ir.Int64(43)),
		ir.If(ir.IsNotEqual(a, b), ir.Return(ir.Int64(1))),
		ir.Return(ir.Int64(0)),
	})
	if result != 1 {
		t.Errorf("Expected 1, got %d", result)
	}
}

func TestNative_IfLessThan(t *testing.T) {
	a := ir.Var("a")
	b := ir.Var("b")
	result := compileAndRun(t, ir.Method{
		ir.Assign(a, ir.Int64(10)),
		ir.Assign(b, ir.Int64(20)),
		ir.If(ir.IsLessThan(a, b), ir.Return(ir.Int64(1))),
		ir.Return(ir.Int64(0)),
	})
	if result != 1 {
		t.Errorf("Expected 1, got %d", result)
	}
}

func TestNative_IfGreaterThan(t *testing.T) {
	a := ir.Var("a")
	b := ir.Var("b")
	result := compileAndRun(t, ir.Method{
		ir.Assign(a, ir.Int64(30)),
		ir.Assign(b, ir.Int64(20)),
		ir.If(ir.IsGreaterThan(a, b), ir.Return(ir.Int64(1))),
		ir.Return(ir.Int64(0)),
	})
	if result != 1 {
		t.Errorf("Expected 1, got %d", result)
	}
}

// Goto and Labels

func TestNative_GotoSkip(t *testing.T) {
	result := compileAndRun(t, ir.Method{
		ir.Goto(ir.Label("end")),
		ir.Return(ir.Int64(0)), // Should be skipped
		ir.DeclareLabel(ir.Label("end"), ir.Block{ir.Return(ir.Int64(42))}),
	})
	if result != 42 {
		t.Errorf("Expected 42, got %d", result)
	}
}

func TestNative_Loop(t *testing.T) {
	// Compute sum of 1..10 = 55
	i := ir.Var("i")
	sum := ir.Var("sum")
	result := compileAndRun(t, ir.Method{
		ir.Assign(i, ir.Int64(1)),
		ir.Assign(sum, ir.Int64(0)),
		ir.DeclareLabel(ir.Label("loop"), ir.Block{
			ir.If(ir.IsGreaterThan(i, ir.Int64(10)), ir.Goto(ir.Label("done"))),
			ir.Assign(sum, ir.Op(ir.OpAdd, sum, i)),
			ir.Assign(i, ir.Op(ir.OpAdd, i, ir.Int64(1))),
			ir.Goto(ir.Label("loop")),
		}),
		ir.DeclareLabel(ir.Label("done"), ir.Block{ir.Return(sum)}),
	})
	if result != 55 {
		t.Errorf("Expected 55, got %d", result)
	}
}

// Stack slots - store and load via memory

func TestNative_StackSlotBasic(t *testing.T) {
	x := ir.Var("x")
	result := compileAndRun(t, ir.Method{
		ir.WithStackSlot(ir.StackSlotConfig{
			Size: 8,
			Body: func(slot ir.StackSlot) ir.Fragment {
				return ir.Block{
					ir.Assign(slot.Base(), ir.Int64(12345)),
					ir.Assign(x, slot.Base()),
				}
			},
		}),
		ir.Return(x),
	})
	if result != 12345 {
		t.Errorf("Expected 12345, got %d", result)
	}
}

// Complex expressions

func TestNative_ChainedOperations(t *testing.T) {
	t.Skip("OpMul not yet implemented in ARM64 IR compiler")
	// (10 + 20) * 3 = 90
	a := ir.Var("a")
	b := ir.Var("b")
	c := ir.Var("c")
	result := compileAndRun(t, ir.Method{
		ir.Assign(a, ir.Int64(10)),
		ir.Assign(b, ir.Int64(20)),
		ir.Assign(c, ir.Op(ir.OpAdd, a, b)),
		ir.Return(ir.Op(ir.OpMul, c, ir.Int64(3))),
	})
	if result != 90 {
		t.Errorf("Expected 90, got %d", result)
	}
}

func TestNative_Fibonacci(t *testing.T) {
	// Compute fib(10) = 55
	n := ir.Var("n")
	a := ir.Var("a")
	b := ir.Var("b")
	tmp := ir.Var("tmp")
	result := compileAndRun(t, ir.Method{
		ir.Assign(n, ir.Int64(10)),
		ir.Assign(a, ir.Int64(0)),
		ir.Assign(b, ir.Int64(1)),
		ir.DeclareLabel(ir.Label("loop"), ir.Block{
			ir.If(ir.IsZero(n), ir.Goto(ir.Label("done"))),
			ir.Assign(tmp, ir.Op(ir.OpAdd, a, b)),
			ir.Assign(a, b),
			ir.Assign(b, tmp),
			ir.Assign(n, ir.Op(ir.OpSub, n, ir.Int64(1))),
			ir.Goto(ir.Label("loop")),
		}),
		ir.DeclareLabel(ir.Label("done"), ir.Block{ir.Return(a)}),
	})
	if result != 55 {
		t.Errorf("Expected 55 (fib(10)), got %d", result)
	}
}

// Edge cases

func TestNative_ManyVariables(t *testing.T) {
	// Test with many variables to stress register allocation
	vars := make([]ir.Var, 20)
	for i := range vars {
		vars[i] = ir.Var(string(rune('a' + i)))
	}

	method := ir.Method{}
	for i, v := range vars {
		method = append(method, ir.Assign(v, ir.Int64(int64(i+1))))
	}

	// Sum all variables
	sum := ir.Var("sum")
	method = append(method, ir.Assign(sum, ir.Int64(0)))
	for _, v := range vars {
		method = append(method, ir.Assign(sum, ir.Op(ir.OpAdd, sum, v)))
	}
	method = append(method, ir.Return(sum))

	result := compileAndRun(t, method)
	// Sum of 1..20 = 210
	if result != 210 {
		t.Errorf("Expected 210, got %d", result)
	}
}

func TestNative_DeepNesting(t *testing.T) {
	x := ir.Var("x")
	// Note: Using IsEqual(x, ir.Int64(0)) as "IsNotZero" isn't defined
	// We want: if x != 0, so we use: if NOT (x == 0), implemented as checking x != 0
	result := compileAndRun(t, ir.Method{
		ir.Assign(x, ir.Int64(1)),
		ir.If(ir.IsNotEqual(x, ir.Int64(0)), ir.Block{
			ir.Assign(x, ir.Op(ir.OpAdd, x, ir.Int64(1))),
			ir.If(ir.IsNotEqual(x, ir.Int64(0)), ir.Block{
				ir.Assign(x, ir.Op(ir.OpAdd, x, ir.Int64(1))),
				ir.If(ir.IsNotEqual(x, ir.Int64(0)), ir.Block{
					ir.Assign(x, ir.Op(ir.OpAdd, x, ir.Int64(1))),
					ir.If(ir.IsNotEqual(x, ir.Int64(0)), ir.Block{
						ir.Assign(x, ir.Op(ir.OpAdd, x, ir.Int64(1))),
					}),
				}),
			}),
		}),
		ir.Return(x),
	})
	if result != 5 {
		t.Errorf("Expected 5, got %d", result)
	}
}

// Test that verifies negative numbers in comparisons work correctly
func TestNative_NegativeComparison(t *testing.T) {
	a := ir.Var("a")
	b := ir.Var("b")
	result := compileAndRun(t, ir.Method{
		ir.Assign(a, ir.Int64(-10)),
		ir.Assign(b, ir.Int64(10)),
		ir.If(ir.IsLessThan(a, b), ir.Return(ir.Int64(1))),
		ir.Return(ir.Int64(0)),
	})
	if result != 1 {
		t.Errorf("Expected 1 (-10 < 10), got %d", result)
	}
}

// Test boundary values
func TestNative_MaxInt64(t *testing.T) {
	result := compileAndRun(t, ir.Method{
		ir.Return(ir.Int64(0x7FFFFFFFFFFFFFFF)),
	})
	if result != 0x7FFFFFFFFFFFFFFF {
		t.Errorf("Expected 0x7FFFFFFFFFFFFFFF, got 0x%x", result)
	}
}

func TestNative_MinInt64(t *testing.T) {
	result := compileAndRun(t, ir.Method{
		ir.Return(ir.Int64(-0x8000000000000000)),
	})
	if int64(result) != -0x8000000000000000 {
		t.Errorf("Expected -0x8000000000000000, got 0x%x", result)
	}
}
