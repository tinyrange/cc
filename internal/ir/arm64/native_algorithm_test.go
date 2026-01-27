//go:build (darwin || linux) && arm64

package arm64

import (
	"testing"

	"github.com/tinyrange/cc/internal/ir"
)

// TestNative_Algorithm_BubbleSort sorts 5 numbers [5, 3, 8, 1, 9] using bubble sort
// with individual variables (since slot.At() requires constant offsets).
// Returns the smallest element (1) after sorting.
func TestNative_Algorithm_BubbleSort(t *testing.T) {
	// We use 5 individual variables to represent the array elements
	// Array: [5, 3, 8, 1, 9] -> sorted: [1, 3, 5, 8, 9]
	v0 := ir.Var("v0")
	v1 := ir.Var("v1")
	v2 := ir.Var("v2")
	v3 := ir.Var("v3")
	v4 := ir.Var("v4")
	temp := ir.Var("temp")
	swapped := ir.Var("swapped")
	pass := ir.Var("pass")

	result := compileAndRun(t, ir.Method{
		// Initialize array: [5, 3, 8, 1, 9]
		ir.Assign(v0, ir.Int64(5)),
		ir.Assign(v1, ir.Int64(3)),
		ir.Assign(v2, ir.Int64(8)),
		ir.Assign(v3, ir.Int64(1)),
		ir.Assign(v4, ir.Int64(9)),
		ir.Assign(pass, ir.Int64(0)),

		// Bubble sort: we need 4 passes for 5 elements
		ir.DeclareLabel(ir.Label("outer"), ir.Block{
			ir.If(ir.IsGreaterOrEqual(pass, ir.Int64(4)), ir.Goto(ir.Label("done"))),
			ir.Assign(swapped, ir.Int64(0)),

			// Compare v0 and v1
			ir.If(ir.IsGreaterThan(v0, v1), ir.Block{
				ir.Assign(temp, v0),
				ir.Assign(v0, v1),
				ir.Assign(v1, temp),
				ir.Assign(swapped, ir.Int64(1)),
			}),

			// Compare v1 and v2
			ir.If(ir.IsGreaterThan(v1, v2), ir.Block{
				ir.Assign(temp, v1),
				ir.Assign(v1, v2),
				ir.Assign(v2, temp),
				ir.Assign(swapped, ir.Int64(1)),
			}),

			// Compare v2 and v3
			ir.If(ir.IsGreaterThan(v2, v3), ir.Block{
				ir.Assign(temp, v2),
				ir.Assign(v2, v3),
				ir.Assign(v3, temp),
				ir.Assign(swapped, ir.Int64(1)),
			}),

			// Compare v3 and v4
			ir.If(ir.IsGreaterThan(v3, v4), ir.Block{
				ir.Assign(temp, v3),
				ir.Assign(v3, v4),
				ir.Assign(v4, temp),
				ir.Assign(swapped, ir.Int64(1)),
			}),

			// Early exit if no swaps
			ir.If(ir.IsZero(swapped), ir.Goto(ir.Label("done"))),

			ir.Assign(pass, ir.Op(ir.OpAdd, pass, ir.Int64(1))),
			ir.Goto(ir.Label("outer")),
		}),

		ir.DeclareLabel(ir.Label("done"), ir.Block{
			// Return the smallest element (v0 after sorting)
			ir.Return(v0),
		}),
	})

	if result != 1 {
		t.Errorf("Expected 1 (smallest element), got %d", result)
	}
}

// TestNative_Algorithm_BinarySearch searches for a value in a sorted 8-element array
// using binary search with explicit mid-point checking at each level.
// Array: [2, 5, 8, 12, 16, 23, 38, 56], searching for 23. Expected result: index 5.
func TestNative_Algorithm_BinarySearch(t *testing.T) {
	// Since we can't use dynamic array indexing, we implement binary search
	// by explicitly checking each possible mid value in a decision tree.
	// For 8 elements (indices 0-7), we need at most 3 comparisons.
	target := ir.Var("target")
	foundIdx := ir.Var("foundIdx")
	value := ir.Var("value")

	// Array values at each index
	// Index: 0   1   2    3    4    5    6    7
	// Value: 2   5   8   12   16   23   38   56

	result := compileAndRun(t, ir.Method{
		ir.Assign(target, ir.Int64(23)),
		ir.Assign(foundIdx, ir.Int64(-1)), // -1 means not found

		ir.WithStackSlot(ir.StackSlotConfig{
			Size: 64, // 8 * 8 bytes
			Body: func(slot ir.StackSlot) ir.Fragment {
				return ir.Block{
					// Initialize sorted array
					ir.Assign(slot.At(0), ir.Int64(2)),
					ir.Assign(slot.At(8), ir.Int64(5)),
					ir.Assign(slot.At(16), ir.Int64(8)),
					ir.Assign(slot.At(24), ir.Int64(12)),
					ir.Assign(slot.At(32), ir.Int64(16)),
					ir.Assign(slot.At(40), ir.Int64(23)),
					ir.Assign(slot.At(48), ir.Int64(38)),
					ir.Assign(slot.At(56), ir.Int64(56)),

					// Binary search decision tree for indices 0-7
					// Level 1: check mid = 3 (value = 12)
					ir.Assign(value, slot.At(24)), // index 3
					ir.If(ir.IsEqual(value, target), ir.Block{
						ir.Assign(foundIdx, ir.Int64(3)),
						ir.Goto(ir.Label("done")),
					}),
					ir.If(ir.IsLessThan(target, value), ir.Goto(ir.Label("left_half"))),

					// Right half: indices 4-7, mid = 5
					ir.Assign(value, slot.At(40)), // index 5
					ir.If(ir.IsEqual(value, target), ir.Block{
						ir.Assign(foundIdx, ir.Int64(5)),
						ir.Goto(ir.Label("done")),
					}),
					ir.If(ir.IsLessThan(target, value), ir.Goto(ir.Label("check_4"))),

					// Right of 5: indices 6-7
					ir.Assign(value, slot.At(48)), // index 6
					ir.If(ir.IsEqual(value, target), ir.Block{
						ir.Assign(foundIdx, ir.Int64(6)),
						ir.Goto(ir.Label("done")),
					}),
					ir.Assign(value, slot.At(56)), // index 7
					ir.If(ir.IsEqual(value, target), ir.Block{
						ir.Assign(foundIdx, ir.Int64(7)),
						ir.Goto(ir.Label("done")),
					}),
					ir.Goto(ir.Label("done")),

					ir.DeclareLabel(ir.Label("check_4"), ir.Block{
						ir.Assign(value, slot.At(32)), // index 4
						ir.If(ir.IsEqual(value, target), ir.Block{
							ir.Assign(foundIdx, ir.Int64(4)),
						}),
						ir.Goto(ir.Label("done")),
					}),

					ir.DeclareLabel(ir.Label("left_half"), ir.Block{
						// Left half: indices 0-2, mid = 1
						ir.Assign(value, slot.At(8)), // index 1
						ir.If(ir.IsEqual(value, target), ir.Block{
							ir.Assign(foundIdx, ir.Int64(1)),
							ir.Goto(ir.Label("done")),
						}),
						ir.If(ir.IsLessThan(target, value), ir.Goto(ir.Label("check_0"))),

						// index 2
						ir.Assign(value, slot.At(16)), // index 2
						ir.If(ir.IsEqual(value, target), ir.Block{
							ir.Assign(foundIdx, ir.Int64(2)),
						}),
						ir.Goto(ir.Label("done")),
					}),

					ir.DeclareLabel(ir.Label("check_0"), ir.Block{
						ir.Assign(value, slot.At(0)), // index 0
						ir.If(ir.IsEqual(value, target), ir.Block{
							ir.Assign(foundIdx, ir.Int64(0)),
						}),
						ir.Goto(ir.Label("done")),
					}),

					ir.DeclareLabel(ir.Label("done"), ir.Block{}),
				}
			},
		}),
		ir.Return(foundIdx),
	})

	if result != 5 {
		t.Errorf("Expected index 5 (value 23), got %d", result)
	}
}

// TestNative_Algorithm_GCD computes the greatest common divisor of 48 and 18
// using the Euclidean algorithm with subtraction (since division is not available).
// GCD(48, 18) = 6
func TestNative_Algorithm_GCD(t *testing.T) {
	a := ir.Var("a")
	b := ir.Var("b")

	result := compileAndRun(t, ir.Method{
		ir.Assign(a, ir.Int64(48)),
		ir.Assign(b, ir.Int64(18)),

		// Euclidean algorithm using subtraction:
		// while a != b:
		//   if a > b: a = a - b
		//   else: b = b - a
		// return a
		ir.DeclareLabel(ir.Label("loop"), ir.Block{
			ir.If(ir.IsEqual(a, b), ir.Goto(ir.Label("done"))),

			ir.If(ir.IsGreaterThan(a, b), ir.Block{
				ir.Assign(a, ir.Op(ir.OpSub, a, b)),
				ir.Goto(ir.Label("loop")),
			}),

			// else: b > a
			ir.Assign(b, ir.Op(ir.OpSub, b, a)),
			ir.Goto(ir.Label("loop")),
		}),

		ir.DeclareLabel(ir.Label("done"), ir.Block{
			ir.Return(a),
		}),
	})

	if result != 6 {
		t.Errorf("Expected GCD(48, 18) = 6, got %d", result)
	}
}

// TestNative_Algorithm_Factorial computes 5! = 120 using iterative multiplication
// implemented as repeated addition (since OpMul is not available).
// 5! = 1 * 2 * 3 * 4 * 5 = 120
func TestNative_Algorithm_Factorial(t *testing.T) {
	n := ir.Var("n")
	i := ir.Var("i")
	result_var := ir.Var("result")
	multiplier := ir.Var("multiplier")
	product := ir.Var("product")
	count := ir.Var("count")

	result := compileAndRun(t, ir.Method{
		ir.Assign(n, ir.Int64(5)),
		ir.Assign(result_var, ir.Int64(1)),
		ir.Assign(i, ir.Int64(2)),

		// Outer loop: for i = 2 to n
		ir.DeclareLabel(ir.Label("factorial_loop"), ir.Block{
			ir.If(ir.IsGreaterThan(i, n), ir.Goto(ir.Label("done"))),

			// Multiply result by i using repeated addition:
			// product = 0
			// for count = 0; count < i; count++:
			//   product = product + result
			// result = product
			ir.Assign(multiplier, result_var),
			ir.Assign(product, ir.Int64(0)),
			ir.Assign(count, ir.Int64(0)),

			ir.DeclareLabel(ir.Label("multiply_loop"), ir.Block{
				ir.If(ir.IsGreaterOrEqual(count, i), ir.Goto(ir.Label("multiply_done"))),
				ir.Assign(product, ir.Op(ir.OpAdd, product, multiplier)),
				ir.Assign(count, ir.Op(ir.OpAdd, count, ir.Int64(1))),
				ir.Goto(ir.Label("multiply_loop")),
			}),

			ir.DeclareLabel(ir.Label("multiply_done"), ir.Block{
				ir.Assign(result_var, product),
				ir.Assign(i, ir.Op(ir.OpAdd, i, ir.Int64(1))),
				ir.Goto(ir.Label("factorial_loop")),
			}),
		}),

		ir.DeclareLabel(ir.Label("done"), ir.Block{
			ir.Return(result_var),
		}),
	})

	if result != 120 {
		t.Errorf("Expected 5! = 120, got %d", result)
	}
}

// TestNative_Algorithm_PrimeCheck checks if 17 is prime using trial division
// implemented with repeated subtraction (since OpDiv/OpMod is not available).
// Returns 1 if prime, 0 if not prime. 17 is prime, so expected result is 1.
func TestNative_Algorithm_PrimeCheck(t *testing.T) {
	n := ir.Var("n")
	divisor := ir.Var("divisor")
	quotient := ir.Var("quotient")
	remainder := ir.Var("remainder")
	temp := ir.Var("temp")
	isPrime := ir.Var("isPrime")

	result := compileAndRun(t, ir.Method{
		ir.Assign(n, ir.Int64(17)),
		ir.Assign(isPrime, ir.Int64(1)), // Assume prime initially

		// Handle edge cases: n < 2 is not prime
		ir.If(ir.IsLessThan(n, ir.Int64(2)), ir.Block{
			ir.Assign(isPrime, ir.Int64(0)),
			ir.Goto(ir.Label("done")),
		}),

		// n == 2 is prime
		ir.If(ir.IsEqual(n, ir.Int64(2)), ir.Goto(ir.Label("done"))),

		// Even numbers > 2 are not prime (check if n & 1 == 0)
		ir.If(ir.IsZero(ir.Op(ir.OpAnd, n, ir.Int64(1))), ir.Block{
			ir.Assign(isPrime, ir.Int64(0)),
			ir.Goto(ir.Label("done")),
		}),

		// Check divisibility by odd numbers from 3 up to sqrt(n)
		// We approximate sqrt(17) < 5, so check 3
		ir.Assign(divisor, ir.Int64(3)),

		ir.DeclareLabel(ir.Label("check_loop"), ir.Block{
			// Check if divisor * divisor > n using repeated addition
			// We need to compute divisor^2 without multiplication
			// divisor^2 = divisor + divisor + ... (divisor times)
			ir.Assign(quotient, ir.Int64(0)),
			ir.Assign(temp, ir.Int64(0)),

			// Compute divisor * divisor
			ir.DeclareLabel(ir.Label("square_loop"), ir.Block{
				ir.If(ir.IsGreaterOrEqual(temp, divisor), ir.Goto(ir.Label("square_done"))),
				ir.Assign(quotient, ir.Op(ir.OpAdd, quotient, divisor)),
				ir.Assign(temp, ir.Op(ir.OpAdd, temp, ir.Int64(1))),
				ir.Goto(ir.Label("square_loop")),
			}),

			ir.DeclareLabel(ir.Label("square_done"), ir.Block{
				// If divisor^2 > n, we're done (it's prime)
				ir.If(ir.IsGreaterThan(quotient, n), ir.Goto(ir.Label("done"))),

				// Check if n is divisible by divisor using repeated subtraction
				// remainder = n
				// while remainder >= divisor: remainder = remainder - divisor
				// if remainder == 0: not prime
				ir.Assign(remainder, n),

				ir.DeclareLabel(ir.Label("mod_loop"), ir.Block{
					ir.If(ir.IsLessThan(remainder, divisor), ir.Goto(ir.Label("mod_done"))),
					ir.Assign(remainder, ir.Op(ir.OpSub, remainder, divisor)),
					ir.Goto(ir.Label("mod_loop")),
				}),

				ir.DeclareLabel(ir.Label("mod_done"), ir.Block{
					ir.If(ir.IsZero(remainder), ir.Block{
						ir.Assign(isPrime, ir.Int64(0)),
						ir.Goto(ir.Label("done")),
					}),

					// Try next odd divisor
					ir.Assign(divisor, ir.Op(ir.OpAdd, divisor, ir.Int64(2))),
					ir.Goto(ir.Label("check_loop")),
				}),
			}),
		}),

		ir.DeclareLabel(ir.Label("done"), ir.Block{
			ir.Return(isPrime),
		}),
	})

	if result != 1 {
		t.Errorf("Expected 1 (17 is prime), got %d", result)
	}
}
