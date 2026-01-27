//go:build (darwin || linux) && arm64

package arm64

import (
	"testing"

	"github.com/tinyrange/cc/internal/ir"
)

// TestNative_Compare_AllConditions tests all 8 comparison types with known values.
func TestNative_Compare_AllConditions(t *testing.T) {
	a := ir.Var("a")
	b := ir.Var("b")

	tests := []struct {
		name     string
		aVal     int64
		bVal     int64
		cond     func(ir.Var, ir.Var) ir.Condition
		expected uintptr
	}{
		{"IsZero_true", 0, 0, func(a, b ir.Var) ir.Condition { return ir.IsZero(a) }, 1},
		{"IsZero_false", 5, 0, func(a, b ir.Var) ir.Condition { return ir.IsZero(a) }, 0},
		{"IsNegative_true", -5, 0, func(a, b ir.Var) ir.Condition { return ir.IsNegative(a) }, 1},
		{"IsNegative_false", 5, 0, func(a, b ir.Var) ir.Condition { return ir.IsNegative(a) }, 0},
		{"IsEqual_true", 42, 42, func(a, b ir.Var) ir.Condition { return ir.IsEqual(a, b) }, 1},
		{"IsEqual_false", 42, 43, func(a, b ir.Var) ir.Condition { return ir.IsEqual(a, b) }, 0},
		{"IsNotEqual_true", 42, 43, func(a, b ir.Var) ir.Condition { return ir.IsNotEqual(a, b) }, 1},
		{"IsNotEqual_false", 42, 42, func(a, b ir.Var) ir.Condition { return ir.IsNotEqual(a, b) }, 0},
		{"IsLessThan_true", 10, 20, func(a, b ir.Var) ir.Condition { return ir.IsLessThan(a, b) }, 1},
		{"IsLessThan_false", 20, 10, func(a, b ir.Var) ir.Condition { return ir.IsLessThan(a, b) }, 0},
		{"IsLessOrEqual_true_less", 10, 20, func(a, b ir.Var) ir.Condition { return ir.IsLessOrEqual(a, b) }, 1},
		{"IsLessOrEqual_true_equal", 20, 20, func(a, b ir.Var) ir.Condition { return ir.IsLessOrEqual(a, b) }, 1},
		{"IsLessOrEqual_false", 30, 20, func(a, b ir.Var) ir.Condition { return ir.IsLessOrEqual(a, b) }, 0},
		{"IsGreaterThan_true", 30, 20, func(a, b ir.Var) ir.Condition { return ir.IsGreaterThan(a, b) }, 1},
		{"IsGreaterThan_false", 10, 20, func(a, b ir.Var) ir.Condition { return ir.IsGreaterThan(a, b) }, 0},
		{"IsGreaterOrEqual_true_greater", 30, 20, func(a, b ir.Var) ir.Condition { return ir.IsGreaterOrEqual(a, b) }, 1},
		{"IsGreaterOrEqual_true_equal", 20, 20, func(a, b ir.Var) ir.Condition { return ir.IsGreaterOrEqual(a, b) }, 1},
		{"IsGreaterOrEqual_false", 10, 20, func(a, b ir.Var) ir.Condition { return ir.IsGreaterOrEqual(a, b) }, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := compileAndRun(t, ir.Method{
				ir.Assign(a, ir.Int64(tt.aVal)),
				ir.Assign(b, ir.Int64(tt.bVal)),
				ir.If(tt.cond(a, b), ir.Return(ir.Int64(1))),
				ir.Return(ir.Int64(0)),
			})
			if result != tt.expected {
				t.Errorf("Expected %d, got %d", tt.expected, result)
			}
		})
	}
}

// TestNative_Compare_ChainedConditions tests multiple conditions in sequence.
func TestNative_Compare_ChainedConditions(t *testing.T) {
	a := ir.Var("a")
	b := ir.Var("b")
	c := ir.Var("c")
	count := ir.Var("count")

	// Test: if a < b, count++; if b < c, count++; if a < c, count++
	// With a=10, b=20, c=30: all three conditions are true, count should be 3
	result := compileAndRun(t, ir.Method{
		ir.Assign(a, ir.Int64(10)),
		ir.Assign(b, ir.Int64(20)),
		ir.Assign(c, ir.Int64(30)),
		ir.Assign(count, ir.Int64(0)),
		ir.If(ir.IsLessThan(a, b), ir.Assign(count, ir.Op(ir.OpAdd, count, ir.Int64(1)))),
		ir.If(ir.IsLessThan(b, c), ir.Assign(count, ir.Op(ir.OpAdd, count, ir.Int64(1)))),
		ir.If(ir.IsLessThan(a, c), ir.Assign(count, ir.Op(ir.OpAdd, count, ir.Int64(1)))),
		ir.Return(count),
	})
	if result != 3 {
		t.Errorf("Expected 3 (all conditions true), got %d", result)
	}

	// Test with a=30, b=20, c=10: no conditions are true, count should be 0
	result = compileAndRun(t, ir.Method{
		ir.Assign(a, ir.Int64(30)),
		ir.Assign(b, ir.Int64(20)),
		ir.Assign(c, ir.Int64(10)),
		ir.Assign(count, ir.Int64(0)),
		ir.If(ir.IsLessThan(a, b), ir.Assign(count, ir.Op(ir.OpAdd, count, ir.Int64(1)))),
		ir.If(ir.IsLessThan(b, c), ir.Assign(count, ir.Op(ir.OpAdd, count, ir.Int64(1)))),
		ir.If(ir.IsLessThan(a, c), ir.Assign(count, ir.Op(ir.OpAdd, count, ir.Int64(1)))),
		ir.Return(count),
	})
	if result != 0 {
		t.Errorf("Expected 0 (no conditions true), got %d", result)
	}
}

// TestNative_Compare_BoundaryValues tests comparisons at MaxInt64, MinInt64, and 0.
func TestNative_Compare_BoundaryValues(t *testing.T) {
	const MaxInt64 int64 = 0x7FFFFFFFFFFFFFFF
	const MinInt64 int64 = -0x8000000000000000

	a := ir.Var("a")
	b := ir.Var("b")

	tests := []struct {
		name     string
		aVal     int64
		bVal     int64
		cond     func(ir.Var, ir.Var) ir.Condition
		expected uintptr
	}{
		// MaxInt64 comparisons
		{"MaxInt64_gt_0", MaxInt64, 0, func(a, b ir.Var) ir.Condition { return ir.IsGreaterThan(a, b) }, 1},
		{"MaxInt64_gt_MinInt64", MaxInt64, MinInt64, func(a, b ir.Var) ir.Condition { return ir.IsGreaterThan(a, b) }, 1},
		{"MaxInt64_eq_MaxInt64", MaxInt64, MaxInt64, func(a, b ir.Var) ir.Condition { return ir.IsEqual(a, b) }, 1},

		// MinInt64 comparisons
		{"MinInt64_lt_0", MinInt64, 0, func(a, b ir.Var) ir.Condition { return ir.IsLessThan(a, b) }, 1},
		{"MinInt64_lt_MaxInt64", MinInt64, MaxInt64, func(a, b ir.Var) ir.Condition { return ir.IsLessThan(a, b) }, 1},
		{"MinInt64_eq_MinInt64", MinInt64, MinInt64, func(a, b ir.Var) ir.Condition { return ir.IsEqual(a, b) }, 1},

		// Zero comparisons
		{"0_gt_MinInt64", 0, MinInt64, func(a, b ir.Var) ir.Condition { return ir.IsGreaterThan(a, b) }, 1},
		{"0_lt_MaxInt64", 0, MaxInt64, func(a, b ir.Var) ir.Condition { return ir.IsLessThan(a, b) }, 1},
		{"0_isZero", 0, 0, func(a, b ir.Var) ir.Condition { return ir.IsZero(a) }, 1},

		// Edge: MinInt64 is negative
		{"MinInt64_isNegative", MinInt64, 0, func(a, b ir.Var) ir.Condition { return ir.IsNegative(a) }, 1},
		// Edge: MaxInt64 is not negative
		{"MaxInt64_notNegative", MaxInt64, 0, func(a, b ir.Var) ir.Condition { return ir.IsNegative(a) }, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := compileAndRun(t, ir.Method{
				ir.Assign(a, ir.Int64(tt.aVal)),
				ir.Assign(b, ir.Int64(tt.bVal)),
				ir.If(tt.cond(a, b), ir.Return(ir.Int64(1))),
				ir.Return(ir.Int64(0)),
			})
			if result != tt.expected {
				t.Errorf("Expected %d, got %d", tt.expected, result)
			}
		})
	}
}

// TestNative_Compare_NearZero tests comparisons of values -1, 0, 1 with all comparison types.
func TestNative_Compare_NearZero(t *testing.T) {
	a := ir.Var("a")
	b := ir.Var("b")

	tests := []struct {
		name     string
		aVal     int64
		bVal     int64
		cond     func(ir.Var, ir.Var) ir.Condition
		expected uintptr
	}{
		// -1 vs 0
		{"-1_lt_0", -1, 0, func(a, b ir.Var) ir.Condition { return ir.IsLessThan(a, b) }, 1},
		{"-1_le_0", -1, 0, func(a, b ir.Var) ir.Condition { return ir.IsLessOrEqual(a, b) }, 1},
		{"-1_gt_0", -1, 0, func(a, b ir.Var) ir.Condition { return ir.IsGreaterThan(a, b) }, 0},
		{"-1_ge_0", -1, 0, func(a, b ir.Var) ir.Condition { return ir.IsGreaterOrEqual(a, b) }, 0},
		{"-1_eq_0", -1, 0, func(a, b ir.Var) ir.Condition { return ir.IsEqual(a, b) }, 0},
		{"-1_ne_0", -1, 0, func(a, b ir.Var) ir.Condition { return ir.IsNotEqual(a, b) }, 1},

		// 0 vs 1
		{"0_lt_1", 0, 1, func(a, b ir.Var) ir.Condition { return ir.IsLessThan(a, b) }, 1},
		{"0_le_1", 0, 1, func(a, b ir.Var) ir.Condition { return ir.IsLessOrEqual(a, b) }, 1},
		{"0_gt_1", 0, 1, func(a, b ir.Var) ir.Condition { return ir.IsGreaterThan(a, b) }, 0},
		{"0_ge_1", 0, 1, func(a, b ir.Var) ir.Condition { return ir.IsGreaterOrEqual(a, b) }, 0},
		{"0_eq_1", 0, 1, func(a, b ir.Var) ir.Condition { return ir.IsEqual(a, b) }, 0},
		{"0_ne_1", 0, 1, func(a, b ir.Var) ir.Condition { return ir.IsNotEqual(a, b) }, 1},

		// -1 vs 1
		{"-1_lt_1", -1, 1, func(a, b ir.Var) ir.Condition { return ir.IsLessThan(a, b) }, 1},
		{"-1_le_1", -1, 1, func(a, b ir.Var) ir.Condition { return ir.IsLessOrEqual(a, b) }, 1},
		{"-1_gt_1", -1, 1, func(a, b ir.Var) ir.Condition { return ir.IsGreaterThan(a, b) }, 0},
		{"-1_ge_1", -1, 1, func(a, b ir.Var) ir.Condition { return ir.IsGreaterOrEqual(a, b) }, 0},

		// IsZero and IsNegative
		{"-1_isZero", -1, 0, func(a, b ir.Var) ir.Condition { return ir.IsZero(a) }, 0},
		{"0_isZero", 0, 0, func(a, b ir.Var) ir.Condition { return ir.IsZero(a) }, 1},
		{"1_isZero", 1, 0, func(a, b ir.Var) ir.Condition { return ir.IsZero(a) }, 0},
		{"-1_isNegative", -1, 0, func(a, b ir.Var) ir.Condition { return ir.IsNegative(a) }, 1},
		{"0_isNegative", 0, 0, func(a, b ir.Var) ir.Condition { return ir.IsNegative(a) }, 0},
		{"1_isNegative", 1, 0, func(a, b ir.Var) ir.Condition { return ir.IsNegative(a) }, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := compileAndRun(t, ir.Method{
				ir.Assign(a, ir.Int64(tt.aVal)),
				ir.Assign(b, ir.Int64(tt.bVal)),
				ir.If(tt.cond(a, b), ir.Return(ir.Int64(1))),
				ir.Return(ir.Int64(0)),
			})
			if result != tt.expected {
				t.Errorf("Expected %d, got %d", tt.expected, result)
			}
		})
	}
}

// TestNative_Compare_SignChange tests comparisons across the sign boundary (negative vs positive).
func TestNative_Compare_SignChange(t *testing.T) {
	a := ir.Var("a")
	b := ir.Var("b")

	tests := []struct {
		name     string
		aVal     int64
		bVal     int64
		cond     func(ir.Var, ir.Var) ir.Condition
		expected uintptr
	}{
		// Negative vs positive
		{"-100_lt_100", -100, 100, func(a, b ir.Var) ir.Condition { return ir.IsLessThan(a, b) }, 1},
		{"-100_gt_100", -100, 100, func(a, b ir.Var) ir.Condition { return ir.IsGreaterThan(a, b) }, 0},
		{"100_gt_-100", 100, -100, func(a, b ir.Var) ir.Condition { return ir.IsGreaterThan(a, b) }, 1},
		{"100_lt_-100", 100, -100, func(a, b ir.Var) ir.Condition { return ir.IsLessThan(a, b) }, 0},

		// Large negative vs small positive
		{"-1000000_lt_1", -1000000, 1, func(a, b ir.Var) ir.Condition { return ir.IsLessThan(a, b) }, 1},
		{"1_gt_-1000000", 1, -1000000, func(a, b ir.Var) ir.Condition { return ir.IsGreaterThan(a, b) }, 1},

		// Small negative vs large positive
		{"-1_lt_1000000", -1, 1000000, func(a, b ir.Var) ir.Condition { return ir.IsLessThan(a, b) }, 1},
		{"1000000_gt_-1", 1000000, -1, func(a, b ir.Var) ir.Condition { return ir.IsGreaterThan(a, b) }, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := compileAndRun(t, ir.Method{
				ir.Assign(a, ir.Int64(tt.aVal)),
				ir.Assign(b, ir.Int64(tt.bVal)),
				ir.If(tt.cond(a, b), ir.Return(ir.Int64(1))),
				ir.Return(ir.Int64(0)),
			})
			if result != tt.expected {
				t.Errorf("Expected %d, got %d", tt.expected, result)
			}
		})
	}
}

// TestNative_Compare_SameValue tests comparisons of identical values with all comparison types.
func TestNative_Compare_SameValue(t *testing.T) {
	a := ir.Var("a")
	b := ir.Var("b")

	values := []int64{0, 1, -1, 42, -42, 1000000, -1000000}

	for _, val := range values {
		t.Run("equal_"+string(rune('0'+val%10)), func(t *testing.T) {
			// IsEqual should be true
			result := compileAndRun(t, ir.Method{
				ir.Assign(a, ir.Int64(val)),
				ir.Assign(b, ir.Int64(val)),
				ir.If(ir.IsEqual(a, b), ir.Return(ir.Int64(1))),
				ir.Return(ir.Int64(0)),
			})
			if result != 1 {
				t.Errorf("IsEqual(%d, %d): expected 1, got %d", val, val, result)
			}

			// IsNotEqual should be false
			result = compileAndRun(t, ir.Method{
				ir.Assign(a, ir.Int64(val)),
				ir.Assign(b, ir.Int64(val)),
				ir.If(ir.IsNotEqual(a, b), ir.Return(ir.Int64(1))),
				ir.Return(ir.Int64(0)),
			})
			if result != 0 {
				t.Errorf("IsNotEqual(%d, %d): expected 0, got %d", val, val, result)
			}

			// IsLessThan should be false
			result = compileAndRun(t, ir.Method{
				ir.Assign(a, ir.Int64(val)),
				ir.Assign(b, ir.Int64(val)),
				ir.If(ir.IsLessThan(a, b), ir.Return(ir.Int64(1))),
				ir.Return(ir.Int64(0)),
			})
			if result != 0 {
				t.Errorf("IsLessThan(%d, %d): expected 0, got %d", val, val, result)
			}

			// IsLessOrEqual should be true
			result = compileAndRun(t, ir.Method{
				ir.Assign(a, ir.Int64(val)),
				ir.Assign(b, ir.Int64(val)),
				ir.If(ir.IsLessOrEqual(a, b), ir.Return(ir.Int64(1))),
				ir.Return(ir.Int64(0)),
			})
			if result != 1 {
				t.Errorf("IsLessOrEqual(%d, %d): expected 1, got %d", val, val, result)
			}

			// IsGreaterThan should be false
			result = compileAndRun(t, ir.Method{
				ir.Assign(a, ir.Int64(val)),
				ir.Assign(b, ir.Int64(val)),
				ir.If(ir.IsGreaterThan(a, b), ir.Return(ir.Int64(1))),
				ir.Return(ir.Int64(0)),
			})
			if result != 0 {
				t.Errorf("IsGreaterThan(%d, %d): expected 0, got %d", val, val, result)
			}

			// IsGreaterOrEqual should be true
			result = compileAndRun(t, ir.Method{
				ir.Assign(a, ir.Int64(val)),
				ir.Assign(b, ir.Int64(val)),
				ir.If(ir.IsGreaterOrEqual(a, b), ir.Return(ir.Int64(1))),
				ir.Return(ir.Int64(0)),
			})
			if result != 1 {
				t.Errorf("IsGreaterOrEqual(%d, %d): expected 1, got %d", val, val, result)
			}
		})
	}
}

// TestNative_Compare_LargeVsSmall tests comparisons of very large positive vs very small (or negative) values.
func TestNative_Compare_LargeVsSmall(t *testing.T) {
	a := ir.Var("a")
	b := ir.Var("b")

	const Large int64 = 0x7000000000000000  // Large positive
	const Small int64 = -0x7000000000000000 // Large negative (small value)

	tests := []struct {
		name     string
		aVal     int64
		bVal     int64
		cond     func(ir.Var, ir.Var) ir.Condition
		expected uintptr
	}{
		{"large_gt_small", Large, Small, func(a, b ir.Var) ir.Condition { return ir.IsGreaterThan(a, b) }, 1},
		{"large_ge_small", Large, Small, func(a, b ir.Var) ir.Condition { return ir.IsGreaterOrEqual(a, b) }, 1},
		{"large_lt_small", Large, Small, func(a, b ir.Var) ir.Condition { return ir.IsLessThan(a, b) }, 0},
		{"large_le_small", Large, Small, func(a, b ir.Var) ir.Condition { return ir.IsLessOrEqual(a, b) }, 0},
		{"large_eq_small", Large, Small, func(a, b ir.Var) ir.Condition { return ir.IsEqual(a, b) }, 0},
		{"large_ne_small", Large, Small, func(a, b ir.Var) ir.Condition { return ir.IsNotEqual(a, b) }, 1},

		{"small_lt_large", Small, Large, func(a, b ir.Var) ir.Condition { return ir.IsLessThan(a, b) }, 1},
		{"small_le_large", Small, Large, func(a, b ir.Var) ir.Condition { return ir.IsLessOrEqual(a, b) }, 1},
		{"small_gt_large", Small, Large, func(a, b ir.Var) ir.Condition { return ir.IsGreaterThan(a, b) }, 0},
		{"small_ge_large", Small, Large, func(a, b ir.Var) ir.Condition { return ir.IsGreaterOrEqual(a, b) }, 0},

		// Large vs 0
		{"large_gt_0", Large, 0, func(a, b ir.Var) ir.Condition { return ir.IsGreaterThan(a, b) }, 1},
		{"0_lt_large", 0, Large, func(a, b ir.Var) ir.Condition { return ir.IsLessThan(a, b) }, 1},

		// Small (negative) vs 0
		{"small_lt_0", Small, 0, func(a, b ir.Var) ir.Condition { return ir.IsLessThan(a, b) }, 1},
		{"0_gt_small", 0, Small, func(a, b ir.Var) ir.Condition { return ir.IsGreaterThan(a, b) }, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := compileAndRun(t, ir.Method{
				ir.Assign(a, ir.Int64(tt.aVal)),
				ir.Assign(b, ir.Int64(tt.bVal)),
				ir.If(tt.cond(a, b), ir.Return(ir.Int64(1))),
				ir.Return(ir.Int64(0)),
			})
			if result != tt.expected {
				t.Errorf("Expected %d, got %d", tt.expected, result)
			}
		})
	}
}

// TestNative_Compare_NegativeVsNegative tests comparisons between two negative values.
func TestNative_Compare_NegativeVsNegative(t *testing.T) {
	a := ir.Var("a")
	b := ir.Var("b")

	tests := []struct {
		name     string
		aVal     int64
		bVal     int64
		cond     func(ir.Var, ir.Var) ir.Condition
		expected uintptr
	}{
		// -10 vs -5: -10 is less than -5
		{"-10_lt_-5", -10, -5, func(a, b ir.Var) ir.Condition { return ir.IsLessThan(a, b) }, 1},
		{"-10_le_-5", -10, -5, func(a, b ir.Var) ir.Condition { return ir.IsLessOrEqual(a, b) }, 1},
		{"-10_gt_-5", -10, -5, func(a, b ir.Var) ir.Condition { return ir.IsGreaterThan(a, b) }, 0},
		{"-10_ge_-5", -10, -5, func(a, b ir.Var) ir.Condition { return ir.IsGreaterOrEqual(a, b) }, 0},
		{"-10_eq_-5", -10, -5, func(a, b ir.Var) ir.Condition { return ir.IsEqual(a, b) }, 0},
		{"-10_ne_-5", -10, -5, func(a, b ir.Var) ir.Condition { return ir.IsNotEqual(a, b) }, 1},

		// -5 vs -10: -5 is greater than -10
		{"-5_gt_-10", -5, -10, func(a, b ir.Var) ir.Condition { return ir.IsGreaterThan(a, b) }, 1},
		{"-5_ge_-10", -5, -10, func(a, b ir.Var) ir.Condition { return ir.IsGreaterOrEqual(a, b) }, 1},
		{"-5_lt_-10", -5, -10, func(a, b ir.Var) ir.Condition { return ir.IsLessThan(a, b) }, 0},
		{"-5_le_-10", -5, -10, func(a, b ir.Var) ir.Condition { return ir.IsLessOrEqual(a, b) }, 0},

		// Large negative values
		{"-1000000_lt_-1", -1000000, -1, func(a, b ir.Var) ir.Condition { return ir.IsLessThan(a, b) }, 1},
		{"-1_gt_-1000000", -1, -1000000, func(a, b ir.Var) ir.Condition { return ir.IsGreaterThan(a, b) }, 1},

		// Both negative, both are negative (IsNegative)
		{"-10_isNegative", -10, 0, func(a, b ir.Var) ir.Condition { return ir.IsNegative(a) }, 1},
		{"-5_isNegative", -5, 0, func(a, b ir.Var) ir.Condition { return ir.IsNegative(a) }, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := compileAndRun(t, ir.Method{
				ir.Assign(a, ir.Int64(tt.aVal)),
				ir.Assign(b, ir.Int64(tt.bVal)),
				ir.If(tt.cond(a, b), ir.Return(ir.Int64(1))),
				ir.Return(ir.Int64(0)),
			})
			if result != tt.expected {
				t.Errorf("Expected %d, got %d", tt.expected, result)
			}
		})
	}
}

// TestNative_Compare_InLoopCondition tests comparison as a loop termination condition.
func TestNative_Compare_InLoopCondition(t *testing.T) {
	// Count down from 10 to 0 using IsGreaterThan as loop condition
	i := ir.Var("i")
	count := ir.Var("count")

	result := compileAndRun(t, ir.Method{
		ir.Assign(i, ir.Int64(10)),
		ir.Assign(count, ir.Int64(0)),
		ir.DeclareLabel(ir.Label("loop"), ir.Block{
			ir.If(ir.IsLessOrEqual(i, ir.Int64(0)), ir.Goto(ir.Label("done"))),
			ir.Assign(count, ir.Op(ir.OpAdd, count, ir.Int64(1))),
			ir.Assign(i, ir.Op(ir.OpSub, i, ir.Int64(1))),
			ir.Goto(ir.Label("loop")),
		}),
		ir.DeclareLabel(ir.Label("done"), ir.Block{ir.Return(count)}),
	})
	if result != 10 {
		t.Errorf("Expected 10 iterations, got %d", result)
	}

	// Count from -5 to 5 using IsLessThan as loop condition
	result = compileAndRun(t, ir.Method{
		ir.Assign(i, ir.Int64(-5)),
		ir.Assign(count, ir.Int64(0)),
		ir.DeclareLabel(ir.Label("loop2"), ir.Block{
			ir.If(ir.IsGreaterThan(i, ir.Int64(5)), ir.Goto(ir.Label("done2"))),
			ir.Assign(count, ir.Op(ir.OpAdd, count, ir.Int64(1))),
			ir.Assign(i, ir.Op(ir.OpAdd, i, ir.Int64(1))),
			ir.Goto(ir.Label("loop2")),
		}),
		ir.DeclareLabel(ir.Label("done2"), ir.Block{ir.Return(count)}),
	})
	// From -5 to 5 inclusive = 11 iterations
	if result != 11 {
		t.Errorf("Expected 11 iterations (-5 to 5), got %d", result)
	}

	// Loop until values are equal
	a := ir.Var("a")
	b := ir.Var("b")
	result = compileAndRun(t, ir.Method{
		ir.Assign(a, ir.Int64(0)),
		ir.Assign(b, ir.Int64(7)),
		ir.Assign(count, ir.Int64(0)),
		ir.DeclareLabel(ir.Label("loop3"), ir.Block{
			ir.If(ir.IsEqual(a, b), ir.Goto(ir.Label("done3"))),
			ir.Assign(count, ir.Op(ir.OpAdd, count, ir.Int64(1))),
			ir.Assign(a, ir.Op(ir.OpAdd, a, ir.Int64(1))),
			ir.Goto(ir.Label("loop3")),
		}),
		ir.DeclareLabel(ir.Label("done3"), ir.Block{ir.Return(count)}),
	})
	if result != 7 {
		t.Errorf("Expected 7 iterations (0 to 7), got %d", result)
	}
}

// TestNative_Compare_NestedConditions tests conditions inside conditions.
func TestNative_Compare_NestedConditions(t *testing.T) {
	a := ir.Var("a")
	b := ir.Var("b")
	c := ir.Var("c")

	// Test: if a < b { if b < c { return 1 } else { return 2 } } else { return 3 }
	// With a=10, b=20, c=30: both conditions true, return 1
	result := compileAndRun(t, ir.Method{
		ir.Assign(a, ir.Int64(10)),
		ir.Assign(b, ir.Int64(20)),
		ir.Assign(c, ir.Int64(30)),
		ir.If(ir.IsLessThan(a, b), ir.Block{
			ir.If(ir.IsLessThan(b, c), ir.Return(ir.Int64(1)), ir.Return(ir.Int64(2))),
		}, ir.Return(ir.Int64(3))),
		ir.Return(ir.Int64(0)), // Should never reach here
	})
	if result != 1 {
		t.Errorf("Expected 1 (a<b and b<c), got %d", result)
	}

	// With a=10, b=20, c=15: a<b true, b<c false, return 2
	result = compileAndRun(t, ir.Method{
		ir.Assign(a, ir.Int64(10)),
		ir.Assign(b, ir.Int64(20)),
		ir.Assign(c, ir.Int64(15)),
		ir.If(ir.IsLessThan(a, b), ir.Block{
			ir.If(ir.IsLessThan(b, c), ir.Return(ir.Int64(1)), ir.Return(ir.Int64(2))),
		}, ir.Return(ir.Int64(3))),
		ir.Return(ir.Int64(0)),
	})
	if result != 2 {
		t.Errorf("Expected 2 (a<b but not b<c), got %d", result)
	}

	// With a=25, b=20, c=30: a<b false, return 3
	result = compileAndRun(t, ir.Method{
		ir.Assign(a, ir.Int64(25)),
		ir.Assign(b, ir.Int64(20)),
		ir.Assign(c, ir.Int64(30)),
		ir.If(ir.IsLessThan(a, b), ir.Block{
			ir.If(ir.IsLessThan(b, c), ir.Return(ir.Int64(1)), ir.Return(ir.Int64(2))),
		}, ir.Return(ir.Int64(3))),
		ir.Return(ir.Int64(0)),
	})
	if result != 3 {
		t.Errorf("Expected 3 (not a<b), got %d", result)
	}

	// Deeper nesting with multiple comparison types
	x := ir.Var("x")
	y := ir.Var("y")
	z := ir.Var("z")
	result = compileAndRun(t, ir.Method{
		ir.Assign(x, ir.Int64(-5)),
		ir.Assign(y, ir.Int64(0)),
		ir.Assign(z, ir.Int64(5)),
		ir.If(ir.IsNegative(x), ir.Block{
			ir.If(ir.IsZero(y), ir.Block{
				ir.If(ir.IsGreaterThan(z, y), ir.Return(ir.Int64(100))),
			}),
		}),
		ir.Return(ir.Int64(0)),
	})
	if result != 100 {
		t.Errorf("Expected 100 (all nested conditions true), got %d", result)
	}

	// Test where middle condition fails
	result = compileAndRun(t, ir.Method{
		ir.Assign(x, ir.Int64(-5)),
		ir.Assign(y, ir.Int64(1)), // Not zero
		ir.Assign(z, ir.Int64(5)),
		ir.If(ir.IsNegative(x), ir.Block{
			ir.If(ir.IsZero(y), ir.Block{
				ir.If(ir.IsGreaterThan(z, y), ir.Return(ir.Int64(100))),
			}),
		}),
		ir.Return(ir.Int64(0)),
	})
	if result != 0 {
		t.Errorf("Expected 0 (middle condition false), got %d", result)
	}
}
