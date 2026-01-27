//go:build (darwin || linux) && arm64

package arm64

import (
	"testing"

	"github.com/tinyrange/cc/internal/ir"
)

// TestNative_Arith_OverflowAdd tests addition that overflows (0x7FFFFFFFFFFFFFFF + 1)
func TestNative_Arith_OverflowAdd(t *testing.T) {
	a := ir.Var("a")
	result := compileAndRun(t, ir.Method{
		ir.Assign(a, ir.Int64(0x7FFFFFFFFFFFFFFF)),
		ir.Return(ir.Op(ir.OpAdd, a, ir.Int64(1))),
	})
	// Overflow wraps to MinInt64
	expected := int64(-0x8000000000000000)
	if int64(result) != expected {
		t.Errorf("Expected %d (0x%x), got %d (0x%x)", expected, uint64(expected), int64(result), result)
	}
}

// TestNative_Arith_UnderflowSub tests subtraction that underflows (-0x8000000000000000 - 1)
func TestNative_Arith_UnderflowSub(t *testing.T) {
	a := ir.Var("a")
	result := compileAndRun(t, ir.Method{
		ir.Assign(a, ir.Int64(-0x8000000000000000)),
		ir.Return(ir.Op(ir.OpSub, a, ir.Int64(1))),
	})
	// Underflow wraps to MaxInt64
	expected := int64(0x7FFFFFFFFFFFFFFF)
	if int64(result) != expected {
		t.Errorf("Expected %d (0x%x), got %d (0x%x)", expected, uint64(expected), int64(result), result)
	}
}

// TestNative_Arith_ChainedAdd tests a + b + c + d + e + f (6 values chained)
func TestNative_Arith_ChainedAdd(t *testing.T) {
	a := ir.Var("a")
	b := ir.Var("b")
	c := ir.Var("c")
	d := ir.Var("d")
	e := ir.Var("e")
	f := ir.Var("f")
	result := compileAndRun(t, ir.Method{
		ir.Assign(a, ir.Int64(10)),
		ir.Assign(b, ir.Int64(20)),
		ir.Assign(c, ir.Int64(30)),
		ir.Assign(d, ir.Int64(40)),
		ir.Assign(e, ir.Int64(50)),
		ir.Assign(f, ir.Int64(60)),
		// Chain: ((((a + b) + c) + d) + e) + f = 210
		ir.Return(ir.Op(ir.OpAdd,
			ir.Op(ir.OpAdd,
				ir.Op(ir.OpAdd,
					ir.Op(ir.OpAdd,
						ir.Op(ir.OpAdd, a, b),
						c),
					d),
				e),
			f)),
	})
	if result != 210 {
		t.Errorf("Expected 210, got %d", result)
	}
}

// TestNative_Arith_ChainedSub tests a - b - c - d - e - f (6 values chained)
func TestNative_Arith_ChainedSub(t *testing.T) {
	a := ir.Var("a")
	b := ir.Var("b")
	c := ir.Var("c")
	d := ir.Var("d")
	e := ir.Var("e")
	f := ir.Var("f")
	result := compileAndRun(t, ir.Method{
		ir.Assign(a, ir.Int64(1000)),
		ir.Assign(b, ir.Int64(100)),
		ir.Assign(c, ir.Int64(50)),
		ir.Assign(d, ir.Int64(25)),
		ir.Assign(e, ir.Int64(10)),
		ir.Assign(f, ir.Int64(5)),
		// Chain: ((((a - b) - c) - d) - e) - f = 1000 - 100 - 50 - 25 - 10 - 5 = 810
		ir.Return(ir.Op(ir.OpSub,
			ir.Op(ir.OpSub,
				ir.Op(ir.OpSub,
					ir.Op(ir.OpSub,
						ir.Op(ir.OpSub, a, b),
						c),
					d),
				e),
			f)),
	})
	if result != 810 {
		t.Errorf("Expected 810, got %d", result)
	}
}

// TestNative_Arith_MixedAddSub tests a + b - c + d - e + f
func TestNative_Arith_MixedAddSub(t *testing.T) {
	a := ir.Var("a")
	b := ir.Var("b")
	c := ir.Var("c")
	d := ir.Var("d")
	e := ir.Var("e")
	f := ir.Var("f")
	result := compileAndRun(t, ir.Method{
		ir.Assign(a, ir.Int64(100)),
		ir.Assign(b, ir.Int64(50)),
		ir.Assign(c, ir.Int64(30)),
		ir.Assign(d, ir.Int64(20)),
		ir.Assign(e, ir.Int64(10)),
		ir.Assign(f, ir.Int64(5)),
		// ((((a + b) - c) + d) - e) + f = 100 + 50 - 30 + 20 - 10 + 5 = 135
		ir.Return(ir.Op(ir.OpAdd,
			ir.Op(ir.OpSub,
				ir.Op(ir.OpAdd,
					ir.Op(ir.OpSub,
						ir.Op(ir.OpAdd, a, b),
						c),
					d),
				e),
			f)),
	})
	if result != 135 {
		t.Errorf("Expected 135, got %d", result)
	}
}

// TestNative_Arith_AddToZero tests x + 0 should equal x
func TestNative_Arith_AddToZero(t *testing.T) {
	testCases := []int64{0, 1, -1, 42, -42, 0x7FFFFFFFFFFFFFFF, -0x8000000000000000}
	for _, tc := range testCases {
		x := ir.Var("x")
		result := compileAndRun(t, ir.Method{
			ir.Assign(x, ir.Int64(tc)),
			ir.Return(ir.Op(ir.OpAdd, x, ir.Int64(0))),
		})
		if int64(result) != tc {
			t.Errorf("Expected %d + 0 = %d, got %d", tc, tc, int64(result))
		}
	}
}

// TestNative_Arith_SubFromSelf tests x - x should equal 0
func TestNative_Arith_SubFromSelf(t *testing.T) {
	testCases := []int64{0, 1, -1, 42, -42, 0x7FFFFFFFFFFFFFFF, -0x8000000000000000, 123456789}
	for _, tc := range testCases {
		x := ir.Var("x")
		result := compileAndRun(t, ir.Method{
			ir.Assign(x, ir.Int64(tc)),
			ir.Return(ir.Op(ir.OpSub, x, x)),
		})
		if int64(result) != 0 {
			t.Errorf("Expected %d - %d = 0, got %d", tc, tc, int64(result))
		}
	}
}

// TestNative_Arith_AddNegative tests adding negative values (-5 + -3 = -8)
func TestNative_Arith_AddNegative(t *testing.T) {
	a := ir.Var("a")
	b := ir.Var("b")
	result := compileAndRun(t, ir.Method{
		ir.Assign(a, ir.Int64(-5)),
		ir.Assign(b, ir.Int64(-3)),
		ir.Return(ir.Op(ir.OpAdd, a, b)),
	})
	if int64(result) != -8 {
		t.Errorf("Expected -8, got %d", int64(result))
	}
}

// TestNative_Arith_SubNegative tests subtracting negative (5 - (-3) = 8)
func TestNative_Arith_SubNegative(t *testing.T) {
	a := ir.Var("a")
	b := ir.Var("b")
	result := compileAndRun(t, ir.Method{
		ir.Assign(a, ir.Int64(5)),
		ir.Assign(b, ir.Int64(-3)),
		ir.Return(ir.Op(ir.OpSub, a, b)),
	})
	if int64(result) != 8 {
		t.Errorf("Expected 8, got %d", int64(result))
	}
}

// TestNative_Arith_LargeValues tests operations on large 64-bit values
func TestNative_Arith_LargeValues(t *testing.T) {
	a := ir.Var("a")
	b := ir.Var("b")

	// Test large positive addition
	result := compileAndRun(t, ir.Method{
		ir.Assign(a, ir.Int64(0x1000000000000000)),
		ir.Assign(b, ir.Int64(0x2000000000000000)),
		ir.Return(ir.Op(ir.OpAdd, a, b)),
	})
	expected := int64(0x3000000000000000)
	if int64(result) != expected {
		t.Errorf("Large add: Expected 0x%x, got 0x%x", uint64(expected), result)
	}

	// Test large subtraction
	result = compileAndRun(t, ir.Method{
		ir.Assign(a, ir.Int64(0x5000000000000000)),
		ir.Assign(b, ir.Int64(0x3000000000000000)),
		ir.Return(ir.Op(ir.OpSub, a, b)),
	})
	expected = int64(0x2000000000000000)
	if int64(result) != expected {
		t.Errorf("Large sub: Expected 0x%x, got 0x%x", uint64(expected), result)
	}

	// Test large negative values
	result = compileAndRun(t, ir.Method{
		ir.Assign(a, ir.Int64(-0x1000000000000000)),
		ir.Assign(b, ir.Int64(-0x2000000000000000)),
		ir.Return(ir.Op(ir.OpAdd, a, b)),
	})
	expected = int64(-0x3000000000000000)
	if int64(result) != expected {
		t.Errorf("Large negative add: Expected %d (0x%x), got %d (0x%x)", expected, uint64(expected), int64(result), result)
	}
}

// TestNative_Arith_SmallValues tests operations on values 0, 1, -1
func TestNative_Arith_SmallValues(t *testing.T) {
	a := ir.Var("a")
	b := ir.Var("b")

	// 0 + 0 = 0
	result := compileAndRun(t, ir.Method{
		ir.Assign(a, ir.Int64(0)),
		ir.Assign(b, ir.Int64(0)),
		ir.Return(ir.Op(ir.OpAdd, a, b)),
	})
	if result != 0 {
		t.Errorf("0 + 0: Expected 0, got %d", result)
	}

	// 1 + 1 = 2
	result = compileAndRun(t, ir.Method{
		ir.Assign(a, ir.Int64(1)),
		ir.Assign(b, ir.Int64(1)),
		ir.Return(ir.Op(ir.OpAdd, a, b)),
	})
	if result != 2 {
		t.Errorf("1 + 1: Expected 2, got %d", result)
	}

	// -1 + -1 = -2
	result = compileAndRun(t, ir.Method{
		ir.Assign(a, ir.Int64(-1)),
		ir.Assign(b, ir.Int64(-1)),
		ir.Return(ir.Op(ir.OpAdd, a, b)),
	})
	if int64(result) != -2 {
		t.Errorf("-1 + -1: Expected -2, got %d", int64(result))
	}

	// 1 - 1 = 0
	result = compileAndRun(t, ir.Method{
		ir.Assign(a, ir.Int64(1)),
		ir.Assign(b, ir.Int64(1)),
		ir.Return(ir.Op(ir.OpSub, a, b)),
	})
	if result != 0 {
		t.Errorf("1 - 1: Expected 0, got %d", result)
	}

	// 0 - 1 = -1
	result = compileAndRun(t, ir.Method{
		ir.Assign(a, ir.Int64(0)),
		ir.Assign(b, ir.Int64(1)),
		ir.Return(ir.Op(ir.OpSub, a, b)),
	})
	if int64(result) != -1 {
		t.Errorf("0 - 1: Expected -1, got %d", int64(result))
	}

	// -1 - (-1) = 0
	result = compileAndRun(t, ir.Method{
		ir.Assign(a, ir.Int64(-1)),
		ir.Assign(b, ir.Int64(-1)),
		ir.Return(ir.Op(ir.OpSub, a, b)),
	})
	if result != 0 {
		t.Errorf("-1 - (-1): Expected 0, got %d", result)
	}
}

// TestNative_Arith_PowerOf2 tests operations on powers of 2
func TestNative_Arith_PowerOf2(t *testing.T) {
	a := ir.Var("a")
	b := ir.Var("b")

	// 2^10 + 2^10 = 2^11
	result := compileAndRun(t, ir.Method{
		ir.Assign(a, ir.Int64(1<<10)),
		ir.Assign(b, ir.Int64(1<<10)),
		ir.Return(ir.Op(ir.OpAdd, a, b)),
	})
	if result != 1<<11 {
		t.Errorf("2^10 + 2^10: Expected %d, got %d", 1<<11, result)
	}

	// 2^32 + 2^32 = 2^33
	result = compileAndRun(t, ir.Method{
		ir.Assign(a, ir.Int64(1<<32)),
		ir.Assign(b, ir.Int64(1<<32)),
		ir.Return(ir.Op(ir.OpAdd, a, b)),
	})
	if result != 1<<33 {
		t.Errorf("2^32 + 2^32: Expected %d, got %d", int64(1<<33), result)
	}

	// 2^62 - 2^61 = 2^61
	result = compileAndRun(t, ir.Method{
		ir.Assign(a, ir.Int64(1<<62)),
		ir.Assign(b, ir.Int64(1<<61)),
		ir.Return(ir.Op(ir.OpSub, a, b)),
	})
	if result != 1<<61 {
		t.Errorf("2^62 - 2^61: Expected %d, got %d", int64(1<<61), result)
	}

	// 2^63 - 1 (MaxInt64) operations
	result = compileAndRun(t, ir.Method{
		ir.Assign(a, ir.Int64((1<<62)-1)),
		ir.Assign(b, ir.Int64(1<<62)),
		ir.Return(ir.Op(ir.OpAdd, a, b)),
	})
	expected := int64((1 << 62) - 1 + (1 << 62))
	if int64(result) != expected {
		t.Errorf("(2^62-1) + 2^62: Expected %d, got %d", expected, int64(result))
	}
}

// TestNative_Arith_AlternatingSign tests alternating +/- operations
func TestNative_Arith_AlternatingSign(t *testing.T) {
	a := ir.Var("a")
	b := ir.Var("b")
	c := ir.Var("c")
	d := ir.Var("d")

	// Start with 100, alternate between positive and negative additions
	// 100 + 50 - 75 + 25 - 40 + 10 = 70
	result := compileAndRun(t, ir.Method{
		ir.Assign(a, ir.Int64(100)),
		ir.Assign(b, ir.Int64(50)),
		ir.Assign(c, ir.Op(ir.OpAdd, a, b)),   // 150
		ir.Assign(c, ir.Op(ir.OpSub, c, ir.Int64(75))), // 75
		ir.Assign(c, ir.Op(ir.OpAdd, c, ir.Int64(25))), // 100
		ir.Assign(c, ir.Op(ir.OpSub, c, ir.Int64(40))), // 60
		ir.Assign(d, ir.Op(ir.OpAdd, c, ir.Int64(10))), // 70
		ir.Return(d),
	})
	if result != 70 {
		t.Errorf("Alternating: Expected 70, got %d", result)
	}

	// Test crossing zero: 10 - 15 + 3 - 8 + 20 = 10
	result = compileAndRun(t, ir.Method{
		ir.Assign(a, ir.Int64(10)),
		ir.Assign(a, ir.Op(ir.OpSub, a, ir.Int64(15))), // -5
		ir.Assign(a, ir.Op(ir.OpAdd, a, ir.Int64(3))),  // -2
		ir.Assign(a, ir.Op(ir.OpSub, a, ir.Int64(8))),  // -10
		ir.Assign(a, ir.Op(ir.OpAdd, a, ir.Int64(20))), // 10
		ir.Return(a),
	})
	if result != 10 {
		t.Errorf("Crossing zero: Expected 10, got %d", result)
	}
}

// TestNative_Arith_AccumulateInLoop tests sum 1 to 100 in a loop
func TestNative_Arith_AccumulateInLoop(t *testing.T) {
	i := ir.Var("i")
	sum := ir.Var("sum")
	result := compileAndRun(t, ir.Method{
		ir.Assign(i, ir.Int64(1)),
		ir.Assign(sum, ir.Int64(0)),
		ir.DeclareLabel(ir.Label("loop"), ir.Block{
			ir.If(ir.IsGreaterThan(i, ir.Int64(100)), ir.Goto(ir.Label("done"))),
			ir.Assign(sum, ir.Op(ir.OpAdd, sum, i)),
			ir.Assign(i, ir.Op(ir.OpAdd, i, ir.Int64(1))),
			ir.Goto(ir.Label("loop")),
		}),
		ir.DeclareLabel(ir.Label("done"), ir.Block{ir.Return(sum)}),
	})
	// Sum of 1 to 100 = 100 * 101 / 2 = 5050
	if result != 5050 {
		t.Errorf("Sum 1-100: Expected 5050, got %d", result)
	}
}

// TestNative_Arith_ParallelAccumulators tests 3 independent sums computed in parallel
func TestNative_Arith_ParallelAccumulators(t *testing.T) {
	i := ir.Var("i")
	sum1 := ir.Var("sum1") // Sum of 1, 2, 3, ...
	sum2 := ir.Var("sum2") // Sum of 2, 4, 6, ... (evens)
	sum3 := ir.Var("sum3") // Sum of 1, 3, 5, ... (odds)
	total := ir.Var("total")

	result := compileAndRun(t, ir.Method{
		ir.Assign(i, ir.Int64(1)),
		ir.Assign(sum1, ir.Int64(0)),
		ir.Assign(sum2, ir.Int64(0)),
		ir.Assign(sum3, ir.Int64(0)),
		ir.DeclareLabel(ir.Label("loop"), ir.Block{
			ir.If(ir.IsGreaterThan(i, ir.Int64(20)), ir.Goto(ir.Label("done"))),
			// sum1 accumulates all values
			ir.Assign(sum1, ir.Op(ir.OpAdd, sum1, i)),
			// sum2 accumulates evens (2, 4, 6, ..., 20)
			ir.Assign(sum2, ir.Op(ir.OpAdd, sum2, ir.Op(ir.OpAdd, i, i))),
			// sum3 accumulates odds (1, 3, 5, ..., 39) using 2*i - 1
			ir.Assign(sum3, ir.Op(ir.OpAdd, sum3, ir.Op(ir.OpSub, ir.Op(ir.OpAdd, i, i), ir.Int64(1)))),
			ir.Assign(i, ir.Op(ir.OpAdd, i, ir.Int64(1))),
			ir.Goto(ir.Label("loop")),
		}),
		ir.DeclareLabel(ir.Label("done"), ir.Block{
			// Return sum1 + sum2 + sum3
			ir.Assign(total, ir.Op(ir.OpAdd, sum1, ir.Op(ir.OpAdd, sum2, sum3))),
			ir.Return(total),
		}),
	})
	// sum1 = 1+2+...+20 = 210
	// sum2 = 2+4+...+40 = 420 (sum of first 20 even numbers)
	// sum3 = 1+3+...+39 = 400 (sum of first 20 odd numbers)
	// total = 210 + 420 + 400 = 1030
	if result != 1030 {
		t.Errorf("Parallel accumulators: Expected 1030, got %d", result)
	}
}
