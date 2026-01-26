//go:build (darwin || linux) && arm64

package arm64

import (
	"testing"

	"github.com/tinyrange/cc/internal/ir"
)

// TestNative_MultiMethod_SimpleCall tests that main can call a helper method
// that returns a constant value.
func TestNative_MultiMethod_SimpleCall(t *testing.T) {
	result := ir.Var("result")
	prog := &ir.Program{
		Entrypoint: "main",
		Methods: map[string]ir.Method{
			"main": {
				ir.CallMethod("helper", result),
				ir.Return(result),
			},
			"helper": {
				ir.Return(ir.Int64(42)),
			},
		},
	}

	got := compileAndRunProgram(t, prog)
	if got != 42 {
		t.Errorf("Expected 42, got %d", got)
	}
}

// TestNative_MultiMethod_ChainedCalls tests a chain of method calls:
// main -> methodA -> methodB -> methodC, where methodC returns a value.
func TestNative_MultiMethod_ChainedCalls(t *testing.T) {
	result := ir.Var("result")
	prog := &ir.Program{
		Entrypoint: "main",
		Methods: map[string]ir.Method{
			"main": {
				ir.CallMethod("methodA", result),
				ir.Return(result),
			},
			"methodA": {
				ir.CallMethod("methodB", result),
				ir.Return(result),
			},
			"methodB": {
				ir.CallMethod("methodC", result),
				ir.Return(result),
			},
			"methodC": {
				ir.Return(ir.Int64(123)),
			},
		},
	}

	got := compileAndRunProgram(t, prog)
	if got != 123 {
		t.Errorf("Expected 123, got %d", got)
	}
}

// TestNative_MultiMethod_Recursive tests recursive method calls by computing
// the sum of 1..n using recursion. sum(5) = 5 + 4 + 3 + 2 + 1 = 15.
// Uses a global variable to pass the counter between recursive calls.
func TestNative_MultiMethod_Recursive(t *testing.T) {

	n := ir.Var("n")
	result := ir.Var("result")
	one := ir.Var("one")
	counter := ir.Global("counter")

	prog := &ir.Program{
		Entrypoint: "main",
		Methods: map[string]ir.Method{
			"main": {
				// Initialize counter to 5
				ir.Assign(counter.AsMem(), ir.Int64(5)),
				ir.CallMethod("sum", result),
				ir.Return(result),
			},
			"sum": {
				// Read n from global counter
				ir.Assign(n, counter.AsMem()),
				// Base case: if n == 0, return 0
				ir.If(ir.IsZero(n), ir.Return(ir.Int64(0))),
				// Recursive case: decrement counter, call sum, add n
				ir.Assign(one, ir.Int64(1)),
				ir.Assign(counter.AsMem(), ir.Op(ir.OpSub, n, one)),
				ir.CallMethod("sum", result),
				// Return n + sum(n-1)
				ir.Return(ir.Op(ir.OpAdd, n, result)),
			},
		},
		Globals: map[string]ir.GlobalConfig{
			"counter": {Size: 8},
		},
	}

	got := compileAndRunProgram(t, prog)
	if got != 15 {
		t.Errorf("Expected 15 (sum of 1..5), got %d", got)
	}
}

// TestNative_MultiMethod_MutualRecursion tests mutual recursion where
// isEven calls isOdd and isOdd calls isEven. Tests isEven(4) = 1 (true).
// Uses a global variable to pass state between methods.
func TestNative_MultiMethod_MutualRecursion(t *testing.T) {

	n := ir.Var("n")
	result := ir.Var("result")
	counter := ir.Global("counter")

	prog := &ir.Program{
		Entrypoint: "main",
		Methods: map[string]ir.Method{
			"main": {
				// Test isEven(4) -> should return 1 (true)
				ir.Assign(counter.AsMem(), ir.Int64(4)),
				ir.CallMethod("isEven", result),
				ir.Return(result),
			},
			"isEven": {
				ir.Assign(n, counter.AsMem()),
				// Base case: 0 is even
				ir.If(ir.IsZero(n), ir.Return(ir.Int64(1))),
				// Recursive case: n is even if n-1 is odd
				ir.Assign(counter.AsMem(), ir.Op(ir.OpSub, n, ir.Int64(1))),
				ir.CallMethod("isOdd", result),
				ir.Return(result),
			},
			"isOdd": {
				ir.Assign(n, counter.AsMem()),
				// Base case: 0 is not odd
				ir.If(ir.IsZero(n), ir.Return(ir.Int64(0))),
				// Recursive case: n is odd if n-1 is even
				ir.Assign(counter.AsMem(), ir.Op(ir.OpSub, n, ir.Int64(1))),
				ir.CallMethod("isEven", result),
				ir.Return(result),
			},
		},
		Globals: map[string]ir.GlobalConfig{
			"counter": {Size: 8},
		},
	}

	got := compileAndRunProgram(t, prog)
	if got != 1 {
		t.Errorf("Expected 1 (4 is even), got %d", got)
	}
}

// TestNative_MultiMethod_MultipleHelpers tests main calling three different
// helper methods and combining their results.
func TestNative_MultiMethod_MultipleHelpers(t *testing.T) {
	a := ir.Var("a")
	b := ir.Var("b")
	c := ir.Var("c")
	sum := ir.Var("sum")

	prog := &ir.Program{
		Entrypoint: "main",
		Methods: map[string]ir.Method{
			"main": {
				ir.CallMethod("getTen", a),
				ir.CallMethod("getTwenty", b),
				ir.CallMethod("getThirty", c),
				// sum = a + b + c = 10 + 20 + 30 = 60
				ir.Assign(sum, ir.Op(ir.OpAdd, a, b)),
				ir.Assign(sum, ir.Op(ir.OpAdd, sum, c)),
				ir.Return(sum),
			},
			"getTen": {
				ir.Return(ir.Int64(10)),
			},
			"getTwenty": {
				ir.Return(ir.Int64(20)),
			},
			"getThirty": {
				ir.Return(ir.Int64(30)),
			},
		},
	}

	got := compileAndRunProgram(t, prog)
	if got != 60 {
		t.Errorf("Expected 60 (10+20+30), got %d", got)
	}
}

// TestNative_MultiMethod_PassViaGlobal tests passing values between methods
// using a global variable. Main sets a global, helper reads and modifies it.
func TestNative_MultiMethod_PassViaGlobal(t *testing.T) {

	val := ir.Var("val")
	result := ir.Var("result")
	shared := ir.Global("shared")

	prog := &ir.Program{
		Entrypoint: "main",
		Methods: map[string]ir.Method{
			"main": {
				// Set shared = 100
				ir.Assign(shared.AsMem(), ir.Int64(100)),
				// Call helper which doubles the global value
				ir.CallMethod("doubleGlobal", result),
				// Read back the modified global
				ir.Assign(val, shared.AsMem()),
				ir.Return(val),
			},
			"doubleGlobal": {
				ir.Assign(val, shared.AsMem()),
				// shared = val + val = 200
				ir.Assign(shared.AsMem(), ir.Op(ir.OpAdd, val, val)),
				ir.Return(ir.Int64(0)),
			},
		},
		Globals: map[string]ir.GlobalConfig{
			"shared": {Size: 8},
		},
	}

	got := compileAndRunProgram(t, prog)
	if got != 200 {
		t.Errorf("Expected 200 (100 doubled), got %d", got)
	}
}

// TestNative_MultiMethod_SharedGlobal tests multiple methods reading and
// writing to the same global variable.
func TestNative_MultiMethod_SharedGlobal(t *testing.T) {

	val := ir.Var("val")
	result := ir.Var("result")
	accumulator := ir.Global("accumulator")

	prog := &ir.Program{
		Entrypoint: "main",
		Methods: map[string]ir.Method{
			"main": {
				// Initialize accumulator = 0
				ir.Assign(accumulator.AsMem(), ir.Int64(0)),
				// Call add5 three times: 0 + 5 + 5 + 5 = 15
				ir.CallMethod("add5", result),
				ir.CallMethod("add5", result),
				ir.CallMethod("add5", result),
				// Read final accumulator value
				ir.Assign(val, accumulator.AsMem()),
				ir.Return(val),
			},
			"add5": {
				ir.Assign(val, accumulator.AsMem()),
				ir.Assign(accumulator.AsMem(), ir.Op(ir.OpAdd, val, ir.Int64(5))),
				ir.Return(ir.Int64(0)),
			},
		},
		Globals: map[string]ir.GlobalConfig{
			"accumulator": {Size: 8},
		},
	}

	got := compileAndRunProgram(t, prog)
	if got != 15 {
		t.Errorf("Expected 15 (0+5+5+5), got %d", got)
	}
}

// TestNative_MultiMethod_MethodPointer tests getting the address of a method
// using MethodPointer. The pointer should be non-zero.
func TestNative_MultiMethod_MethodPointer(t *testing.T) {
	ptr := ir.Var("ptr")

	prog := &ir.Program{
		Entrypoint: "main",
		Methods: map[string]ir.Method{
			"main": {
				ir.Assign(ptr, ir.MethodPointer("helper")),
				// Return the pointer value - should be non-zero
				ir.Return(ptr),
			},
			"helper": {
				ir.Return(ir.Int64(999)),
			},
		},
	}

	got := compileAndRunProgram(t, prog)
	if got == 0 {
		t.Errorf("Expected non-zero method pointer, got 0")
	}
}

// TestNative_MultiMethod_ConditionalCall tests calling different methods
// based on a condition.
func TestNative_MultiMethod_ConditionalCall(t *testing.T) {
	flag := ir.Var("flag")
	result := ir.Var("result")

	prog := &ir.Program{
		Entrypoint: "main",
		Methods: map[string]ir.Method{
			"main": {
				// flag = 1, so we should call branchTrue
				ir.Assign(flag, ir.Int64(1)),
				ir.If(ir.IsNotEqual(flag, ir.Int64(0)), ir.Block{
					ir.CallMethod("branchTrue", result),
					ir.Return(result),
				}),
				ir.CallMethod("branchFalse", result),
				ir.Return(result),
			},
			"branchTrue": {
				ir.Return(ir.Int64(111)),
			},
			"branchFalse": {
				ir.Return(ir.Int64(222)),
			},
		},
	}

	got := compileAndRunProgram(t, prog)
	if got != 111 {
		t.Errorf("Expected 111 (branchTrue), got %d", got)
	}
}

// TestNative_MultiMethod_LoopWithCall tests a loop that calls a method on
// each iteration. The helper method returns a value that gets accumulated.
func TestNative_MultiMethod_LoopWithCall(t *testing.T) {
	i := ir.Var("i")
	result := ir.Var("result")
	sum := ir.Var("sum")

	prog := &ir.Program{
		Entrypoint: "main",
		Methods: map[string]ir.Method{
			"main": {
				// Initialize sum = 0
				ir.Assign(sum, ir.Int64(0)),
				// Loop 10 times, calling getOne each iteration and accumulating
				ir.Assign(i, ir.Int64(0)),
				ir.DeclareLabel(ir.Label("loop"), ir.Block{
					ir.If(ir.IsEqual(i, ir.Int64(10)), ir.Goto(ir.Label("done"))),
					ir.CallMethod("getOne", result),
					ir.Assign(sum, ir.Op(ir.OpAdd, sum, result)),
					ir.Assign(i, ir.Op(ir.OpAdd, i, ir.Int64(1))),
					ir.Goto(ir.Label("loop")),
				}),
				ir.DeclareLabel(ir.Label("done"), ir.Block{
					// Return sum: should be 10 (1 added 10 times)
					ir.Return(sum),
				}),
			},
			"getOne": {
				ir.Return(ir.Int64(1)),
			},
		},
	}

	got := compileAndRunProgram(t, prog)
	if got != 10 {
		t.Errorf("Expected 10 (1 added 10 times), got %d", got)
	}
}
