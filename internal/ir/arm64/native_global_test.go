//go:build (darwin || linux) && arm64

package arm64

import (
	"testing"

	"github.com/tinyrange/cc/internal/ir"
)

// TestNative_Global_ReadWrite tests simple global read/write operations.
func TestNative_Global_ReadWrite(t *testing.T) {
	val := ir.Var("val")
	g := ir.Global("myGlobal")

	prog := &ir.Program{
		Entrypoint: "main",
		Methods: map[string]ir.Method{
			"main": {
				// Write 42 to global
				ir.Assign(g.AsMem(), ir.Int64(42)),
				// Read it back
				ir.Assign(val, g.AsMem()),
				ir.Return(val),
			},
		},
		Globals: map[string]ir.GlobalConfig{
			"myGlobal": {Size: 8},
		},
	}

	got := compileAndRunProgram(t, prog)
	if got != 42 {
		t.Errorf("Expected 42, got %d", got)
	}
}

// TestNative_Global_MultipleGlobals tests operations on 3 independent globals.
func TestNative_Global_MultipleGlobals(t *testing.T) {
	a := ir.Var("a")
	b := ir.Var("b")
	c := ir.Var("c")
	sum := ir.Var("sum")

	gA := ir.Global("globalA")
	gB := ir.Global("globalB")
	gC := ir.Global("globalC")

	prog := &ir.Program{
		Entrypoint: "main",
		Methods: map[string]ir.Method{
			"main": {
				// Write values to globals
				ir.Assign(gA.AsMem(), ir.Int64(10)),
				ir.Assign(gB.AsMem(), ir.Int64(20)),
				ir.Assign(gC.AsMem(), ir.Int64(30)),
				// Read them back
				ir.Assign(a, gA.AsMem()),
				ir.Assign(b, gB.AsMem()),
				ir.Assign(c, gC.AsMem()),
				// Sum them: 10 + 20 + 30 = 60
				ir.Assign(sum, ir.Op(ir.OpAdd, a, b)),
				ir.Assign(sum, ir.Op(ir.OpAdd, sum, c)),
				ir.Return(sum),
			},
		},
		Globals: map[string]ir.GlobalConfig{
			"globalA": {Size: 8},
			"globalB": {Size: 8},
			"globalC": {Size: 8},
		},
	}

	got := compileAndRunProgram(t, prog)
	if got != 60 {
		t.Errorf("Expected 60, got %d", got)
	}
}

// TestNative_Global_GlobalInLoop tests incrementing a global 100 times in a loop.
func TestNative_Global_GlobalInLoop(t *testing.T) {
	i := ir.Var("i")
	val := ir.Var("val")
	counter := ir.Global("counter")

	prog := &ir.Program{
		Entrypoint: "main",
		Methods: map[string]ir.Method{
			"main": {
				// Initialize counter to 0
				ir.Assign(counter.AsMem(), ir.Int64(0)),
				// Loop 100 times, incrementing counter each time
				ir.Assign(i, ir.Int64(0)),
				ir.DeclareLabel(ir.Label("loop"), ir.Block{
					ir.If(ir.IsEqual(i, ir.Int64(100)), ir.Goto(ir.Label("done"))),
					// counter = counter + 1
					ir.Assign(val, counter.AsMem()),
					ir.Assign(counter.AsMem(), ir.Op(ir.OpAdd, val, ir.Int64(1))),
					// i++
					ir.Assign(i, ir.Op(ir.OpAdd, i, ir.Int64(1))),
					ir.Goto(ir.Label("loop")),
				}),
				ir.DeclareLabel(ir.Label("done"), ir.Block{
					ir.Assign(val, counter.AsMem()),
					ir.Return(val),
				}),
			},
		},
		Globals: map[string]ir.GlobalConfig{
			"counter": {Size: 8},
		},
	}

	got := compileAndRunProgram(t, prog)
	if got != 100 {
		t.Errorf("Expected 100, got %d", got)
	}
}

// TestNative_Global_GlobalAcrossMethod tests a global modified by a called method.
func TestNative_Global_GlobalAcrossMethod(t *testing.T) {
	val := ir.Var("val")
	result := ir.Var("result")
	shared := ir.Global("shared")

	prog := &ir.Program{
		Entrypoint: "main",
		Methods: map[string]ir.Method{
			"main": {
				// Set shared = 50
				ir.Assign(shared.AsMem(), ir.Int64(50)),
				// Call helper which triples the global value
				ir.CallMethod("tripleGlobal", result),
				// Read back the modified global
				ir.Assign(val, shared.AsMem()),
				ir.Return(val),
			},
			"tripleGlobal": {
				ir.Assign(val, shared.AsMem()),
				// shared = val * 3 = val + val + val
				ir.Assign(shared.AsMem(), ir.Op(ir.OpAdd, val, ir.Op(ir.OpAdd, val, val))),
				ir.Return(ir.Int64(0)),
			},
		},
		Globals: map[string]ir.GlobalConfig{
			"shared": {Size: 8},
		},
	}

	got := compileAndRunProgram(t, prog)
	if got != 150 {
		t.Errorf("Expected 150 (50 tripled), got %d", got)
	}
}

// TestNative_Global_GlobalArray tests using a global with displacement as an array.
func TestNative_Global_GlobalArray(t *testing.T) {
	val := ir.Var("val")
	sum := ir.Var("sum")
	arr := ir.Global("array")

	prog := &ir.Program{
		Entrypoint: "main",
		Methods: map[string]ir.Method{
			"main": {
				// Write values at offsets: array[0]=1, array[8]=2, array[16]=3
				ir.Assign(arr.MemWithDisp(ir.Int64(0)), ir.Int64(1)),
				ir.Assign(arr.MemWithDisp(ir.Int64(8)), ir.Int64(2)),
				ir.Assign(arr.MemWithDisp(ir.Int64(16)), ir.Int64(3)),
				// Read them back and sum: 1 + 2 + 3 = 6
				ir.Assign(val, arr.MemWithDisp(ir.Int64(0))),
				ir.Assign(sum, val),
				ir.Assign(val, arr.MemWithDisp(ir.Int64(8))),
				ir.Assign(sum, ir.Op(ir.OpAdd, sum, val)),
				ir.Assign(val, arr.MemWithDisp(ir.Int64(16))),
				ir.Assign(sum, ir.Op(ir.OpAdd, sum, val)),
				ir.Return(sum),
			},
		},
		Globals: map[string]ir.GlobalConfig{
			"array": {Size: 24, Align: 8}, // 3 x 8-byte elements
		},
	}

	got := compileAndRunProgram(t, prog)
	if got != 6 {
		t.Errorf("Expected 6 (1+2+3), got %d", got)
	}
}
