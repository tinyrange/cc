//go:build (darwin || linux) && arm64

package arm64

import (
	"testing"

	"github.com/tinyrange/cc/internal/ir"
)

// TestNative_ControlFlow_MultipleLabels tests jumping between 10+ labels.
func TestNative_ControlFlow_MultipleLabels(t *testing.T) {
	x := ir.Var("x")
	result := compileAndRun(t, ir.Method{
		ir.Assign(x, ir.Int64(0)),
		ir.Goto(ir.Label("L1")),
		ir.DeclareLabel(ir.Label("L10"), ir.Block{
			ir.Assign(x, ir.Op(ir.OpAdd, x, ir.Int64(10))),
			ir.Goto(ir.Label("done")),
		}),
		ir.DeclareLabel(ir.Label("L9"), ir.Block{
			ir.Assign(x, ir.Op(ir.OpAdd, x, ir.Int64(9))),
			ir.Goto(ir.Label("L10")),
		}),
		ir.DeclareLabel(ir.Label("L8"), ir.Block{
			ir.Assign(x, ir.Op(ir.OpAdd, x, ir.Int64(8))),
			ir.Goto(ir.Label("L9")),
		}),
		ir.DeclareLabel(ir.Label("L7"), ir.Block{
			ir.Assign(x, ir.Op(ir.OpAdd, x, ir.Int64(7))),
			ir.Goto(ir.Label("L8")),
		}),
		ir.DeclareLabel(ir.Label("L6"), ir.Block{
			ir.Assign(x, ir.Op(ir.OpAdd, x, ir.Int64(6))),
			ir.Goto(ir.Label("L7")),
		}),
		ir.DeclareLabel(ir.Label("L5"), ir.Block{
			ir.Assign(x, ir.Op(ir.OpAdd, x, ir.Int64(5))),
			ir.Goto(ir.Label("L6")),
		}),
		ir.DeclareLabel(ir.Label("L4"), ir.Block{
			ir.Assign(x, ir.Op(ir.OpAdd, x, ir.Int64(4))),
			ir.Goto(ir.Label("L5")),
		}),
		ir.DeclareLabel(ir.Label("L3"), ir.Block{
			ir.Assign(x, ir.Op(ir.OpAdd, x, ir.Int64(3))),
			ir.Goto(ir.Label("L4")),
		}),
		ir.DeclareLabel(ir.Label("L2"), ir.Block{
			ir.Assign(x, ir.Op(ir.OpAdd, x, ir.Int64(2))),
			ir.Goto(ir.Label("L3")),
		}),
		ir.DeclareLabel(ir.Label("L1"), ir.Block{
			ir.Assign(x, ir.Op(ir.OpAdd, x, ir.Int64(1))),
			ir.Goto(ir.Label("L2")),
		}),
		ir.DeclareLabel(ir.Label("done"), ir.Block{
			ir.Return(x),
		}),
	})
	// Sum of 1..10 = 55
	if result != 55 {
		t.Errorf("Expected 55, got %d", result)
	}
}

// TestNative_ControlFlow_BackwardJump tests jumping backward in code (loop pattern).
func TestNative_ControlFlow_BackwardJump(t *testing.T) {
	i := ir.Var("i")
	sum := ir.Var("sum")
	result := compileAndRun(t, ir.Method{
		ir.Assign(i, ir.Int64(5)),
		ir.Assign(sum, ir.Int64(0)),
		ir.DeclareLabel(ir.Label("start"), ir.Block{
			ir.Assign(sum, ir.Op(ir.OpAdd, sum, i)),
			ir.Assign(i, ir.Op(ir.OpSub, i, ir.Int64(1))),
			ir.If(ir.IsGreaterThan(i, ir.Int64(0)), ir.Goto(ir.Label("start"))),
			ir.Return(sum),
		}),
	})
	// 5 + 4 + 3 + 2 + 1 = 15
	if result != 15 {
		t.Errorf("Expected 15, got %d", result)
	}
}

// TestNative_ControlFlow_ForwardJump tests jumping forward skipping code.
func TestNative_ControlFlow_ForwardJump(t *testing.T) {
	x := ir.Var("x")
	result := compileAndRun(t, ir.Method{
		ir.Assign(x, ir.Int64(100)),
		ir.Goto(ir.Label("skip_modification")),
		// This code should be skipped
		ir.Assign(x, ir.Int64(0)),
		ir.Assign(x, ir.Int64(999)),
		ir.Assign(x, ir.Int64(-1)),
		ir.DeclareLabel(ir.Label("skip_modification"), ir.Block{
			ir.Assign(x, ir.Op(ir.OpAdd, x, ir.Int64(42))),
			ir.Return(x),
		}),
	})
	// 100 + 42 = 142
	if result != 142 {
		t.Errorf("Expected 142, got %d", result)
	}
}

// TestNative_ControlFlow_NestedIf10Levels tests 10-level deeply nested if statements.
func TestNative_ControlFlow_NestedIf10Levels(t *testing.T) {
	x := ir.Var("x")
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
						ir.If(ir.IsNotEqual(x, ir.Int64(0)), ir.Block{
							ir.Assign(x, ir.Op(ir.OpAdd, x, ir.Int64(1))),
							ir.If(ir.IsNotEqual(x, ir.Int64(0)), ir.Block{
								ir.Assign(x, ir.Op(ir.OpAdd, x, ir.Int64(1))),
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
							}),
						}),
					}),
				}),
			}),
		}),
		ir.Return(x),
	})
	// Started with 1, added 1 ten times = 11
	if result != 11 {
		t.Errorf("Expected 11, got %d", result)
	}
}

// TestNative_ControlFlow_IfElseChain tests if/else chain with 5+ branches using labels.
func TestNative_ControlFlow_IfElseChain(t *testing.T) {
	x := ir.Var("x")
	result := ir.Var("result")

	// Test value that should match branch 3 (x == 3)
	res := compileAndRun(t, ir.Method{
		ir.Assign(x, ir.Int64(3)),
		ir.Assign(result, ir.Int64(0)),
		ir.If(ir.IsEqual(x, ir.Int64(1)), ir.Block{
			ir.Assign(result, ir.Int64(100)),
			ir.Goto(ir.Label("done")),
		}),
		ir.If(ir.IsEqual(x, ir.Int64(2)), ir.Block{
			ir.Assign(result, ir.Int64(200)),
			ir.Goto(ir.Label("done")),
		}),
		ir.If(ir.IsEqual(x, ir.Int64(3)), ir.Block{
			ir.Assign(result, ir.Int64(300)),
			ir.Goto(ir.Label("done")),
		}),
		ir.If(ir.IsEqual(x, ir.Int64(4)), ir.Block{
			ir.Assign(result, ir.Int64(400)),
			ir.Goto(ir.Label("done")),
		}),
		ir.If(ir.IsEqual(x, ir.Int64(5)), ir.Block{
			ir.Assign(result, ir.Int64(500)),
			ir.Goto(ir.Label("done")),
		}),
		// Default case
		ir.Assign(result, ir.Int64(-1)),
		ir.DeclareLabel(ir.Label("done"), ir.Block{
			ir.Return(result),
		}),
	})
	if res != 300 {
		t.Errorf("Expected 300, got %d", res)
	}
}

// TestNative_ControlFlow_Switch tests switch-case pattern with 5 cases using labels.
func TestNative_ControlFlow_Switch(t *testing.T) {
	selector := ir.Var("selector")
	result := ir.Var("result")

	// Test case 4
	res := compileAndRun(t, ir.Method{
		ir.Assign(selector, ir.Int64(4)),
		ir.Assign(result, ir.Int64(0)),
		// Jump table pattern
		ir.If(ir.IsEqual(selector, ir.Int64(0)), ir.Goto(ir.Label("case0"))),
		ir.If(ir.IsEqual(selector, ir.Int64(1)), ir.Goto(ir.Label("case1"))),
		ir.If(ir.IsEqual(selector, ir.Int64(2)), ir.Goto(ir.Label("case2"))),
		ir.If(ir.IsEqual(selector, ir.Int64(3)), ir.Goto(ir.Label("case3"))),
		ir.If(ir.IsEqual(selector, ir.Int64(4)), ir.Goto(ir.Label("case4"))),
		ir.Goto(ir.Label("default")),

		ir.DeclareLabel(ir.Label("case0"), ir.Block{
			ir.Assign(result, ir.Int64(10)),
			ir.Goto(ir.Label("end")),
		}),
		ir.DeclareLabel(ir.Label("case1"), ir.Block{
			ir.Assign(result, ir.Int64(20)),
			ir.Goto(ir.Label("end")),
		}),
		ir.DeclareLabel(ir.Label("case2"), ir.Block{
			ir.Assign(result, ir.Int64(30)),
			ir.Goto(ir.Label("end")),
		}),
		ir.DeclareLabel(ir.Label("case3"), ir.Block{
			ir.Assign(result, ir.Int64(40)),
			ir.Goto(ir.Label("end")),
		}),
		ir.DeclareLabel(ir.Label("case4"), ir.Block{
			ir.Assign(result, ir.Int64(50)),
			ir.Goto(ir.Label("end")),
		}),
		ir.DeclareLabel(ir.Label("default"), ir.Block{
			ir.Assign(result, ir.Int64(-1)),
			ir.Goto(ir.Label("end")),
		}),
		ir.DeclareLabel(ir.Label("end"), ir.Block{
			ir.Return(result),
		}),
	})
	if res != 50 {
		t.Errorf("Expected 50, got %d", res)
	}
}

// TestNative_ControlFlow_MultipleExits tests multiple return points from different paths.
func TestNative_ControlFlow_MultipleExits(t *testing.T) {
	x := ir.Var("x")

	// Test early return from first condition
	res1 := compileAndRun(t, ir.Method{
		ir.Assign(x, ir.Int64(-5)),
		ir.If(ir.IsNegative(x), ir.Return(ir.Int64(1))),
		ir.If(ir.IsZero(x), ir.Return(ir.Int64(2))),
		ir.If(ir.IsLessThan(x, ir.Int64(10)), ir.Return(ir.Int64(3))),
		ir.If(ir.IsLessThan(x, ir.Int64(100)), ir.Return(ir.Int64(4))),
		ir.Return(ir.Int64(5)),
	})
	if res1 != 1 {
		t.Errorf("Expected 1 for negative, got %d", res1)
	}

	// Test middle return
	res2 := compileAndRun(t, ir.Method{
		ir.Assign(x, ir.Int64(50)),
		ir.If(ir.IsNegative(x), ir.Return(ir.Int64(1))),
		ir.If(ir.IsZero(x), ir.Return(ir.Int64(2))),
		ir.If(ir.IsLessThan(x, ir.Int64(10)), ir.Return(ir.Int64(3))),
		ir.If(ir.IsLessThan(x, ir.Int64(100)), ir.Return(ir.Int64(4))),
		ir.Return(ir.Int64(5)),
	})
	if res2 != 4 {
		t.Errorf("Expected 4 for x=50, got %d", res2)
	}

	// Test final return
	res3 := compileAndRun(t, ir.Method{
		ir.Assign(x, ir.Int64(500)),
		ir.If(ir.IsNegative(x), ir.Return(ir.Int64(1))),
		ir.If(ir.IsZero(x), ir.Return(ir.Int64(2))),
		ir.If(ir.IsLessThan(x, ir.Int64(10)), ir.Return(ir.Int64(3))),
		ir.If(ir.IsLessThan(x, ir.Int64(100)), ir.Return(ir.Int64(4))),
		ir.Return(ir.Int64(5)),
	})
	if res3 != 5 {
		t.Errorf("Expected 5 for x=500, got %d", res3)
	}
}

// TestNative_ControlFlow_BreakContinue tests nested loops with break/continue patterns.
func TestNative_ControlFlow_BreakContinue(t *testing.T) {
	i := ir.Var("i")
	j := ir.Var("j")
	sum := ir.Var("sum")

	// Outer loop 0..4, inner loop 0..4
	// Continue inner when j == 2, break inner when j == 4
	// Sum should count iterations where j != 2 and j < 4
	result := compileAndRun(t, ir.Method{
		ir.Assign(sum, ir.Int64(0)),
		ir.Assign(i, ir.Int64(0)),
		ir.DeclareLabel(ir.Label("outer_loop"), ir.Block{
			ir.If(ir.IsGreaterOrEqual(i, ir.Int64(5)), ir.Goto(ir.Label("outer_done"))),
			ir.Assign(j, ir.Int64(0)),
			ir.DeclareLabel(ir.Label("inner_loop"), ir.Block{
				ir.If(ir.IsGreaterOrEqual(j, ir.Int64(5)), ir.Goto(ir.Label("inner_done"))),
				// Continue pattern: skip when j == 2
				ir.If(ir.IsEqual(j, ir.Int64(2)), ir.Block{
					ir.Assign(j, ir.Op(ir.OpAdd, j, ir.Int64(1))),
					ir.Goto(ir.Label("inner_loop")),
				}),
				// Break pattern: exit inner loop when j == 4
				ir.If(ir.IsEqual(j, ir.Int64(4)), ir.Goto(ir.Label("inner_done"))),
				// Count this iteration
				ir.Assign(sum, ir.Op(ir.OpAdd, sum, ir.Int64(1))),
				ir.Assign(j, ir.Op(ir.OpAdd, j, ir.Int64(1))),
				ir.Goto(ir.Label("inner_loop")),
			}),
			ir.DeclareLabel(ir.Label("inner_done"), ir.Block{
				ir.Assign(i, ir.Op(ir.OpAdd, i, ir.Int64(1))),
				ir.Goto(ir.Label("outer_loop")),
			}),
		}),
		ir.DeclareLabel(ir.Label("outer_done"), ir.Block{
			ir.Return(sum),
		}),
	})
	// 5 outer iterations * 3 inner iterations (j=0,1,3) = 15
	if result != 15 {
		t.Errorf("Expected 15, got %d", result)
	}
}

// TestNative_ControlFlow_CrossingJumps tests labels and jumps that cross over each other.
func TestNative_ControlFlow_CrossingJumps(t *testing.T) {
	x := ir.Var("x")
	result := compileAndRun(t, ir.Method{
		ir.Assign(x, ir.Int64(0)),
		ir.Goto(ir.Label("A")),

		ir.DeclareLabel(ir.Label("B"), ir.Block{
			ir.Assign(x, ir.Op(ir.OpAdd, x, ir.Int64(2))),
			ir.Goto(ir.Label("D")),
		}),

		ir.DeclareLabel(ir.Label("A"), ir.Block{
			ir.Assign(x, ir.Op(ir.OpAdd, x, ir.Int64(1))),
			ir.Goto(ir.Label("C")),
		}),

		ir.DeclareLabel(ir.Label("D"), ir.Block{
			ir.Assign(x, ir.Op(ir.OpAdd, x, ir.Int64(4))),
			ir.Goto(ir.Label("done")),
		}),

		ir.DeclareLabel(ir.Label("C"), ir.Block{
			ir.Assign(x, ir.Op(ir.OpAdd, x, ir.Int64(3))),
			ir.Goto(ir.Label("B")),
		}),

		ir.DeclareLabel(ir.Label("done"), ir.Block{
			ir.Return(x),
		}),
	})
	// Path: start -> A(+1) -> C(+3) -> B(+2) -> D(+4) -> done = 10
	if result != 10 {
		t.Errorf("Expected 10, got %d", result)
	}
}

// TestNative_ControlFlow_FallThrough tests code that falls through label declarations.
func TestNative_ControlFlow_FallThrough(t *testing.T) {
	x := ir.Var("x")
	result := compileAndRun(t, ir.Method{
		ir.Assign(x, ir.Int64(0)),
		// Fall through to L1
		ir.DeclareLabel(ir.Label("L1"), ir.Block{
			ir.Assign(x, ir.Op(ir.OpAdd, x, ir.Int64(1))),
			// Fall through to L2
		}),
		ir.DeclareLabel(ir.Label("L2"), ir.Block{
			ir.Assign(x, ir.Op(ir.OpAdd, x, ir.Int64(2))),
			// Fall through to L3
		}),
		ir.DeclareLabel(ir.Label("L3"), ir.Block{
			ir.Assign(x, ir.Op(ir.OpAdd, x, ir.Int64(3))),
			// Fall through to L4
		}),
		ir.DeclareLabel(ir.Label("L4"), ir.Block{
			ir.Assign(x, ir.Op(ir.OpAdd, x, ir.Int64(4))),
		}),
		ir.Return(x),
	})
	// 0 + 1 + 2 + 3 + 4 = 10
	if result != 10 {
		t.Errorf("Expected 10, got %d", result)
	}
}

// TestNative_ControlFlow_ConditionalGoto tests conditional with goto instead of block.
func TestNative_ControlFlow_ConditionalGoto(t *testing.T) {
	x := ir.Var("x")
	count := ir.Var("count")

	result := compileAndRun(t, ir.Method{
		ir.Assign(x, ir.Int64(10)),
		ir.Assign(count, ir.Int64(0)),
		ir.DeclareLabel(ir.Label("loop"), ir.Block{
			// Conditional goto instead of if-block
			ir.If(ir.IsZero(x), ir.Goto(ir.Label("done"))),
			ir.Assign(count, ir.Op(ir.OpAdd, count, ir.Int64(1))),
			ir.Assign(x, ir.Op(ir.OpSub, x, ir.Int64(1))),
			ir.Goto(ir.Label("loop")),
		}),
		ir.DeclareLabel(ir.Label("done"), ir.Block{
			ir.Return(count),
		}),
	})
	if result != 10 {
		t.Errorf("Expected 10, got %d", result)
	}
}

// TestNative_ControlFlow_LoopUnroll tests manually unrolled loop pattern.
func TestNative_ControlFlow_LoopUnroll(t *testing.T) {
	x := ir.Var("x")
	i := ir.Var("i")

	// Simulate processing 20 items with 4x unrolled loop
	result := compileAndRun(t, ir.Method{
		ir.Assign(x, ir.Int64(0)),
		ir.Assign(i, ir.Int64(0)),
		ir.DeclareLabel(ir.Label("unrolled_loop"), ir.Block{
			ir.If(ir.IsGreaterOrEqual(i, ir.Int64(20)), ir.Goto(ir.Label("done"))),
			// Iteration 1
			ir.Assign(x, ir.Op(ir.OpAdd, x, ir.Int64(1))),
			ir.Assign(i, ir.Op(ir.OpAdd, i, ir.Int64(1))),
			ir.If(ir.IsGreaterOrEqual(i, ir.Int64(20)), ir.Goto(ir.Label("done"))),
			// Iteration 2
			ir.Assign(x, ir.Op(ir.OpAdd, x, ir.Int64(1))),
			ir.Assign(i, ir.Op(ir.OpAdd, i, ir.Int64(1))),
			ir.If(ir.IsGreaterOrEqual(i, ir.Int64(20)), ir.Goto(ir.Label("done"))),
			// Iteration 3
			ir.Assign(x, ir.Op(ir.OpAdd, x, ir.Int64(1))),
			ir.Assign(i, ir.Op(ir.OpAdd, i, ir.Int64(1))),
			ir.If(ir.IsGreaterOrEqual(i, ir.Int64(20)), ir.Goto(ir.Label("done"))),
			// Iteration 4
			ir.Assign(x, ir.Op(ir.OpAdd, x, ir.Int64(1))),
			ir.Assign(i, ir.Op(ir.OpAdd, i, ir.Int64(1))),
			ir.Goto(ir.Label("unrolled_loop")),
		}),
		ir.DeclareLabel(ir.Label("done"), ir.Block{
			ir.Return(x),
		}),
	})
	if result != 20 {
		t.Errorf("Expected 20, got %d", result)
	}
}

// TestNative_ControlFlow_DiamondPattern tests if-else that rejoins at common point.
func TestNative_ControlFlow_DiamondPattern(t *testing.T) {
	x := ir.Var("x")
	y := ir.Var("y")

	// Test true branch
	res1 := compileAndRun(t, ir.Method{
		ir.Assign(x, ir.Int64(5)),
		ir.Assign(y, ir.Int64(0)),
		ir.If(ir.IsGreaterThan(x, ir.Int64(3)),
			ir.Block{
				ir.Assign(y, ir.Int64(100)),
				ir.Goto(ir.Label("merge")),
			},
			ir.Block{
				ir.Assign(y, ir.Int64(200)),
				ir.Goto(ir.Label("merge")),
			},
		),
		ir.DeclareLabel(ir.Label("merge"), ir.Block{
			// Common code after diamond
			ir.Assign(y, ir.Op(ir.OpAdd, y, ir.Int64(50))),
			ir.Return(y),
		}),
	})
	if res1 != 150 {
		t.Errorf("Expected 150 for true branch, got %d", res1)
	}

	// Test false branch
	res2 := compileAndRun(t, ir.Method{
		ir.Assign(x, ir.Int64(2)),
		ir.Assign(y, ir.Int64(0)),
		ir.If(ir.IsGreaterThan(x, ir.Int64(3)),
			ir.Block{
				ir.Assign(y, ir.Int64(100)),
				ir.Goto(ir.Label("merge")),
			},
			ir.Block{
				ir.Assign(y, ir.Int64(200)),
				ir.Goto(ir.Label("merge")),
			},
		),
		ir.DeclareLabel(ir.Label("merge"), ir.Block{
			ir.Assign(y, ir.Op(ir.OpAdd, y, ir.Int64(50))),
			ir.Return(y),
		}),
	})
	if res2 != 250 {
		t.Errorf("Expected 250 for false branch, got %d", res2)
	}
}

// TestNative_ControlFlow_TrianglePattern tests if without else that continues to common code.
func TestNative_ControlFlow_TrianglePattern(t *testing.T) {
	x := ir.Var("x")
	y := ir.Var("y")

	// Test when condition is true - modify then continue
	res1 := compileAndRun(t, ir.Method{
		ir.Assign(x, ir.Int64(10)),
		ir.Assign(y, ir.Int64(5)),
		ir.If(ir.IsGreaterThan(x, ir.Int64(5)), ir.Block{
			ir.Assign(y, ir.Op(ir.OpAdd, y, ir.Int64(100))),
		}),
		// Common continuation - both paths reach here
		ir.Assign(y, ir.Op(ir.OpAdd, y, ir.Int64(1))),
		ir.Return(y),
	})
	// y = 5 + 100 + 1 = 106
	if res1 != 106 {
		t.Errorf("Expected 106 for true branch, got %d", res1)
	}

	// Test when condition is false - skip to common code
	res2 := compileAndRun(t, ir.Method{
		ir.Assign(x, ir.Int64(3)),
		ir.Assign(y, ir.Int64(5)),
		ir.If(ir.IsGreaterThan(x, ir.Int64(5)), ir.Block{
			ir.Assign(y, ir.Op(ir.OpAdd, y, ir.Int64(100))),
		}),
		// Common continuation
		ir.Assign(y, ir.Op(ir.OpAdd, y, ir.Int64(1))),
		ir.Return(y),
	})
	// y = 5 + 1 = 6
	if res2 != 6 {
		t.Errorf("Expected 6 for false branch, got %d", res2)
	}
}

// TestNative_ControlFlow_SequentialIfsWithNested tests sequential ifs where:
// - First if has nested if inside
// - Code between sequential ifs must always execute
// - Second if is independent
// This pattern appears in init_source.go with capture flags.
func TestNative_ControlFlow_SequentialIfsWithNested(t *testing.T) {
	flags := ir.Var("flags")
	captureStdout := ir.Var("captureStdout")
	innerResult := ir.Var("innerResult")
	marker := ir.Var("marker")
	result := ir.Var("result")

	// Test with flags = 0x1 (should enter first if, AND code after must execute)
	res := compileAndRun(t, ir.Method{
		ir.Assign(flags, ir.Int64(0x1)),
		ir.Assign(marker, ir.Int64(0)),
		ir.Assign(result, ir.Int64(0)),

		// captureStdout = flags & 0x1
		ir.Assign(captureStdout, ir.Op(ir.OpAnd, flags, ir.Int64(0x1))),

		// if captureStdout != 0 { ... nested if ... }
		ir.If(ir.IsNotEqual(captureStdout, ir.Int64(0)), ir.Block{
			ir.Assign(innerResult, ir.Int64(10)),
			// Nested if
			ir.If(ir.IsGreaterOrEqual(innerResult, ir.Int64(0)), ir.Block{
				ir.Assign(result, ir.Op(ir.OpAdd, result, ir.Int64(100))),
			}),
		}),

		// This code MUST execute regardless of whether captureStdout was 0 or not
		ir.Assign(marker, ir.Int64(1)),

		// Second sequential if (independent)
		ir.If(ir.IsNotEqual(captureStdout, ir.Int64(0)), ir.Block{
			ir.Assign(result, ir.Op(ir.OpAdd, result, ir.Int64(1000))),
		}),

		// marker + result: if marker wasn't set, we'd get wrong result
		ir.Return(ir.Op(ir.OpAdd, result, ir.Op(ir.OpShl, marker, ir.Int64(16)))),
	})
	// Expected: marker=1 (shifted left 16 = 65536), result = 100 + 1000 = 1100
	// Total = 65536 + 1100 = 66636
	if res != 66636 {
		t.Errorf("flags=0x1: Expected 66636 (marker=1, result=1100), got %d (marker=%d, result=%d)",
			res, res>>16, res&0xFFFF)
	}

	// Test with flags = 0x0 (should skip first if, but code after must still execute)
	res2 := compileAndRun(t, ir.Method{
		ir.Assign(flags, ir.Int64(0x0)),
		ir.Assign(marker, ir.Int64(0)),
		ir.Assign(result, ir.Int64(0)),

		ir.Assign(captureStdout, ir.Op(ir.OpAnd, flags, ir.Int64(0x1))),

		ir.If(ir.IsNotEqual(captureStdout, ir.Int64(0)), ir.Block{
			ir.Assign(innerResult, ir.Int64(10)),
			ir.If(ir.IsGreaterOrEqual(innerResult, ir.Int64(0)), ir.Block{
				ir.Assign(result, ir.Op(ir.OpAdd, result, ir.Int64(100))),
			}),
		}),

		// This code MUST execute
		ir.Assign(marker, ir.Int64(1)),

		ir.If(ir.IsNotEqual(captureStdout, ir.Int64(0)), ir.Block{
			ir.Assign(result, ir.Op(ir.OpAdd, result, ir.Int64(1000))),
		}),

		ir.Return(ir.Op(ir.OpAdd, result, ir.Op(ir.OpShl, marker, ir.Int64(16)))),
	})
	// Expected: marker=1 (65536), result = 0 (both ifs skipped)
	// Total = 65536 + 0 = 65536
	if res2 != 65536 {
		t.Errorf("flags=0x0: Expected 65536 (marker=1, result=0), got %d (marker=%d, result=%d)",
			res2, res2>>16, res2&0xFFFF)
	}
}

// TestNative_ControlFlow_SequentialIfsNoElse tests the pattern where multiple ifs
// without else blocks are sequential and code between them must always execute.
// This is similar to the capture setup pattern in init_source.go.
func TestNative_ControlFlow_SequentialIfsNoElse(t *testing.T) {
	a := ir.Var("a")
	b := ir.Var("b")
	x := ir.Var("x")

	// Test: all ifs true
	res := compileAndRun(t, ir.Method{
		ir.Assign(a, ir.Int64(1)),
		ir.Assign(b, ir.Int64(1)),
		ir.Assign(x, ir.Int64(0)),

		ir.If(ir.IsNotEqual(a, ir.Int64(0)), ir.Block{
			ir.Assign(x, ir.Op(ir.OpAdd, x, ir.Int64(1))),
		}),
		// Code that MUST execute
		ir.Assign(x, ir.Op(ir.OpAdd, x, ir.Int64(10))),

		ir.If(ir.IsNotEqual(b, ir.Int64(0)), ir.Block{
			ir.Assign(x, ir.Op(ir.OpAdd, x, ir.Int64(100))),
		}),
		// Code that MUST execute
		ir.Assign(x, ir.Op(ir.OpAdd, x, ir.Int64(1000))),

		ir.Return(x),
	})
	// a=1: +1, then +10, b=1: +100, then +1000 = 1111
	if res != 1111 {
		t.Errorf("a=1,b=1: Expected 1111, got %d", res)
	}

	// Test: first if true, second false
	res2 := compileAndRun(t, ir.Method{
		ir.Assign(a, ir.Int64(1)),
		ir.Assign(b, ir.Int64(0)),
		ir.Assign(x, ir.Int64(0)),

		ir.If(ir.IsNotEqual(a, ir.Int64(0)), ir.Block{
			ir.Assign(x, ir.Op(ir.OpAdd, x, ir.Int64(1))),
		}),
		ir.Assign(x, ir.Op(ir.OpAdd, x, ir.Int64(10))),

		ir.If(ir.IsNotEqual(b, ir.Int64(0)), ir.Block{
			ir.Assign(x, ir.Op(ir.OpAdd, x, ir.Int64(100))),
		}),
		ir.Assign(x, ir.Op(ir.OpAdd, x, ir.Int64(1000))),

		ir.Return(x),
	})
	// a=1: +1, then +10, b=0: skip +100, then +1000 = 1011
	if res2 != 1011 {
		t.Errorf("a=1,b=0: Expected 1011, got %d", res2)
	}

	// Test: first if false, second true
	res3 := compileAndRun(t, ir.Method{
		ir.Assign(a, ir.Int64(0)),
		ir.Assign(b, ir.Int64(1)),
		ir.Assign(x, ir.Int64(0)),

		ir.If(ir.IsNotEqual(a, ir.Int64(0)), ir.Block{
			ir.Assign(x, ir.Op(ir.OpAdd, x, ir.Int64(1))),
		}),
		ir.Assign(x, ir.Op(ir.OpAdd, x, ir.Int64(10))),

		ir.If(ir.IsNotEqual(b, ir.Int64(0)), ir.Block{
			ir.Assign(x, ir.Op(ir.OpAdd, x, ir.Int64(100))),
		}),
		ir.Assign(x, ir.Op(ir.OpAdd, x, ir.Int64(1000))),

		ir.Return(x),
	})
	// a=0: skip +1, then +10, b=1: +100, then +1000 = 1110
	if res3 != 1110 {
		t.Errorf("a=0,b=1: Expected 1110, got %d", res3)
	}

	// Test: both ifs false
	res4 := compileAndRun(t, ir.Method{
		ir.Assign(a, ir.Int64(0)),
		ir.Assign(b, ir.Int64(0)),
		ir.Assign(x, ir.Int64(0)),

		ir.If(ir.IsNotEqual(a, ir.Int64(0)), ir.Block{
			ir.Assign(x, ir.Op(ir.OpAdd, x, ir.Int64(1))),
		}),
		ir.Assign(x, ir.Op(ir.OpAdd, x, ir.Int64(10))),

		ir.If(ir.IsNotEqual(b, ir.Int64(0)), ir.Block{
			ir.Assign(x, ir.Op(ir.OpAdd, x, ir.Int64(100))),
		}),
		ir.Assign(x, ir.Op(ir.OpAdd, x, ir.Int64(1000))),

		ir.Return(x),
	})
	// a=0: skip +1, then +10, b=0: skip +100, then +1000 = 1010
	if res4 != 1010 {
		t.Errorf("a=0,b=0: Expected 1010, got %d", res4)
	}
}

// TestNative_ControlFlow_NestedIfWithSyscallPattern tests the exact pattern from
// init_source.go that causes bugs: outer if with syscall, nested if checking
// syscall result, code after outer if must execute.
func TestNative_ControlFlow_NestedIfWithSyscallPattern(t *testing.T) {
	flags := ir.Var("flags")
	captureStdout := ir.Var("captureStdout")
	pipeResult := ir.Var("pipeResult")
	pipeRead := ir.Var("pipeRead")
	pipeWrite := ir.Var("pipeWrite")
	marker1 := ir.Var("marker1")
	marker2 := ir.Var("marker2")
	marker3 := ir.Var("marker3")

	// Test with flags=1: enters outer if, nested if should also execute
	res := compileAndRun(t, ir.Method{
		ir.Assign(flags, ir.Int64(0x1)),
		ir.Assign(marker1, ir.Int64(0)),
		ir.Assign(marker2, ir.Int64(0)),
		ir.Assign(marker3, ir.Int64(0)),
		ir.Assign(pipeResult, ir.Int64(0)),
		ir.Assign(pipeRead, ir.Int64(0)),
		ir.Assign(pipeWrite, ir.Int64(0)),

		// captureStdout = flags & 0x1
		ir.Assign(captureStdout, ir.Op(ir.OpAnd, flags, ir.Int64(0x1))),

		// if captureStdout != 0 {
		ir.If(ir.IsNotEqual(captureStdout, ir.Int64(0)), ir.Block{
			// Simulate syscall result
			ir.Assign(pipeResult, ir.Int64(5)), // Positive result (success)

			// if pipeResult >= 0 {
			ir.If(ir.IsGreaterOrEqual(pipeResult, ir.Int64(0)), ir.Block{
				ir.Assign(pipeRead, ir.Int64(3)),
				ir.Assign(pipeWrite, ir.Int64(4)),
				// Multiple operations to simulate the real code
				ir.Assign(pipeWrite, ir.Int64(-1)),
			}),
			// End of nested if
		}),
		// End of outer if

		// These markers MUST be set - they verify fall-through works
		ir.Assign(marker1, ir.Int64(1)),

		// Second if block
		ir.If(ir.IsNotEqual(captureStdout, ir.Int64(0)), ir.Block{
			ir.Assign(marker2, ir.Int64(1)),
		}),

		// Third marker - verifies we continue after second if
		ir.Assign(marker3, ir.Int64(1)),

		// Return combined value: marker3<<16 | marker2<<8 | marker1
		ir.Return(ir.Op(ir.OpOr,
			ir.Op(ir.OpShl, marker3, ir.Int64(16)),
			ir.Op(ir.OpOr,
				ir.Op(ir.OpShl, marker2, ir.Int64(8)),
				marker1))),
	})
	// Expected: marker1=1, marker2=1, marker3=1
	// Value = 0x10000 + 0x100 + 0x1 = 0x10101 = 65793
	if res != 65793 {
		t.Errorf("flags=0x1: Expected 65793 (0x10101), got %d (0x%x)", res, res)
		t.Logf("  marker1=%d, marker2=%d, marker3=%d", res&0xFF, (res>>8)&0xFF, (res>>16)&0xFF)
	}

	// Test with flags=0: skips outer if, code after must still execute
	res2 := compileAndRun(t, ir.Method{
		ir.Assign(flags, ir.Int64(0x0)),
		ir.Assign(marker1, ir.Int64(0)),
		ir.Assign(marker2, ir.Int64(0)),
		ir.Assign(marker3, ir.Int64(0)),
		ir.Assign(pipeResult, ir.Int64(0)),
		ir.Assign(pipeRead, ir.Int64(0)),
		ir.Assign(pipeWrite, ir.Int64(0)),

		ir.Assign(captureStdout, ir.Op(ir.OpAnd, flags, ir.Int64(0x1))),

		ir.If(ir.IsNotEqual(captureStdout, ir.Int64(0)), ir.Block{
			ir.Assign(pipeResult, ir.Int64(5)),
			ir.If(ir.IsGreaterOrEqual(pipeResult, ir.Int64(0)), ir.Block{
				ir.Assign(pipeRead, ir.Int64(3)),
				ir.Assign(pipeWrite, ir.Int64(4)),
				ir.Assign(pipeWrite, ir.Int64(-1)),
			}),
		}),

		ir.Assign(marker1, ir.Int64(1)),

		ir.If(ir.IsNotEqual(captureStdout, ir.Int64(0)), ir.Block{
			ir.Assign(marker2, ir.Int64(1)),
		}),

		ir.Assign(marker3, ir.Int64(1)),

		ir.Return(ir.Op(ir.OpOr,
			ir.Op(ir.OpShl, marker3, ir.Int64(16)),
			ir.Op(ir.OpOr,
				ir.Op(ir.OpShl, marker2, ir.Int64(8)),
				marker1))),
	})
	// Expected: marker1=1, marker2=0 (skipped), marker3=1
	// Value = 0x10000 + 0x0 + 0x1 = 0x10001 = 65537
	if res2 != 65537 {
		t.Errorf("flags=0x0: Expected 65537 (0x10001), got %d (0x%x)", res2, res2)
		t.Logf("  marker1=%d, marker2=%d, marker3=%d", res2&0xFF, (res2>>8)&0xFF, (res2>>16)&0xFF)
	}
}

// TestNative_ControlFlow_IfWithNestedIfAndFallthrough tests an if with nested if
// where code after the outer if MUST execute after the then block completes.
func TestNative_ControlFlow_IfWithNestedIfAndFallthrough(t *testing.T) {
	outer := ir.Var("outer")
	inner := ir.Var("inner")
	x := ir.Var("x")

	// Both conditions true - nested executes, then falls through
	res := compileAndRun(t, ir.Method{
		ir.Assign(outer, ir.Int64(1)),
		ir.Assign(inner, ir.Int64(1)),
		ir.Assign(x, ir.Int64(0)),

		ir.If(ir.IsNotEqual(outer, ir.Int64(0)), ir.Block{
			ir.Assign(x, ir.Op(ir.OpAdd, x, ir.Int64(1))), // +1 for entering outer
			ir.If(ir.IsNotEqual(inner, ir.Int64(0)), ir.Block{
				ir.Assign(x, ir.Op(ir.OpAdd, x, ir.Int64(10))), // +10 for entering inner
			}),
			ir.Assign(x, ir.Op(ir.OpAdd, x, ir.Int64(100))), // +100 after inner if (still in outer)
		}),

		// This MUST execute after outer if completes
		ir.Assign(x, ir.Op(ir.OpAdd, x, ir.Int64(1000))),
		ir.Return(x),
	})
	// outer=1: +1, inner=1: +10, after inner: +100, after outer: +1000 = 1111
	if res != 1111 {
		t.Errorf("outer=1,inner=1: Expected 1111, got %d", res)
	}

	// Outer true, inner false - inner skipped but code after inner still runs
	res2 := compileAndRun(t, ir.Method{
		ir.Assign(outer, ir.Int64(1)),
		ir.Assign(inner, ir.Int64(0)),
		ir.Assign(x, ir.Int64(0)),

		ir.If(ir.IsNotEqual(outer, ir.Int64(0)), ir.Block{
			ir.Assign(x, ir.Op(ir.OpAdd, x, ir.Int64(1))),
			ir.If(ir.IsNotEqual(inner, ir.Int64(0)), ir.Block{
				ir.Assign(x, ir.Op(ir.OpAdd, x, ir.Int64(10))),
			}),
			ir.Assign(x, ir.Op(ir.OpAdd, x, ir.Int64(100))),
		}),

		ir.Assign(x, ir.Op(ir.OpAdd, x, ir.Int64(1000))),
		ir.Return(x),
	})
	// outer=1: +1, inner=0: skip +10, after inner: +100, after outer: +1000 = 1101
	if res2 != 1101 {
		t.Errorf("outer=1,inner=0: Expected 1101, got %d", res2)
	}

	// Outer false - entire outer block skipped, code after outer still runs
	res3 := compileAndRun(t, ir.Method{
		ir.Assign(outer, ir.Int64(0)),
		ir.Assign(inner, ir.Int64(1)),
		ir.Assign(x, ir.Int64(0)),

		ir.If(ir.IsNotEqual(outer, ir.Int64(0)), ir.Block{
			ir.Assign(x, ir.Op(ir.OpAdd, x, ir.Int64(1))),
			ir.If(ir.IsNotEqual(inner, ir.Int64(0)), ir.Block{
				ir.Assign(x, ir.Op(ir.OpAdd, x, ir.Int64(10))),
			}),
			ir.Assign(x, ir.Op(ir.OpAdd, x, ir.Int64(100))),
		}),

		ir.Assign(x, ir.Op(ir.OpAdd, x, ir.Int64(1000))),
		ir.Return(x),
	})
	// outer=0: skip all, after outer: +1000 = 1000
	if res3 != 1000 {
		t.Errorf("outer=0: Expected 1000, got %d", res3)
	}
}

// TestNative_ControlFlow_ComplexCFG tests complex control flow graph with many edges.
func TestNative_ControlFlow_ComplexCFG(t *testing.T) {
	x := ir.Var("x")
	state := ir.Var("state")
	count := ir.Var("count")

	// State machine with multiple transitions
	// States: 0 -> 1 -> 2 -> 3 -> (loop back to 1 twice) -> 4 -> done
	result := compileAndRun(t, ir.Method{
		ir.Assign(x, ir.Int64(0)),
		ir.Assign(state, ir.Int64(0)),
		ir.Assign(count, ir.Int64(0)),

		ir.DeclareLabel(ir.Label("dispatch"), ir.Block{
			ir.If(ir.IsEqual(state, ir.Int64(0)), ir.Goto(ir.Label("state0"))),
			ir.If(ir.IsEqual(state, ir.Int64(1)), ir.Goto(ir.Label("state1"))),
			ir.If(ir.IsEqual(state, ir.Int64(2)), ir.Goto(ir.Label("state2"))),
			ir.If(ir.IsEqual(state, ir.Int64(3)), ir.Goto(ir.Label("state3"))),
			ir.If(ir.IsEqual(state, ir.Int64(4)), ir.Goto(ir.Label("state4"))),
			ir.Goto(ir.Label("error")),
		}),

		ir.DeclareLabel(ir.Label("state0"), ir.Block{
			ir.Assign(x, ir.Op(ir.OpAdd, x, ir.Int64(1))),
			ir.Assign(state, ir.Int64(1)),
			ir.Goto(ir.Label("dispatch")),
		}),

		ir.DeclareLabel(ir.Label("state1"), ir.Block{
			ir.Assign(x, ir.Op(ir.OpAdd, x, ir.Int64(10))),
			ir.Assign(state, ir.Int64(2)),
			ir.Goto(ir.Label("dispatch")),
		}),

		ir.DeclareLabel(ir.Label("state2"), ir.Block{
			ir.Assign(x, ir.Op(ir.OpAdd, x, ir.Int64(100))),
			ir.Assign(state, ir.Int64(3)),
			ir.Goto(ir.Label("dispatch")),
		}),

		ir.DeclareLabel(ir.Label("state3"), ir.Block{
			ir.Assign(x, ir.Op(ir.OpAdd, x, ir.Int64(1000))),
			ir.Assign(count, ir.Op(ir.OpAdd, count, ir.Int64(1))),
			// Loop back to state 1 twice, then proceed to state 4
			ir.If(ir.IsLessThan(count, ir.Int64(3)), ir.Block{
				ir.Assign(state, ir.Int64(1)),
				ir.Goto(ir.Label("dispatch")),
			}),
			ir.Assign(state, ir.Int64(4)),
			ir.Goto(ir.Label("dispatch")),
		}),

		ir.DeclareLabel(ir.Label("state4"), ir.Block{
			ir.Assign(x, ir.Op(ir.OpAdd, x, ir.Int64(10000))),
			ir.Goto(ir.Label("done")),
		}),

		ir.DeclareLabel(ir.Label("error"), ir.Block{
			ir.Return(ir.Int64(-1)),
		}),

		ir.DeclareLabel(ir.Label("done"), ir.Block{
			ir.Return(x),
		}),
	})
	// Path: 0 -> 1 -> 2 -> 3 -> 1 -> 2 -> 3 -> 1 -> 2 -> 3 -> 4
	// x = 1 + 10 + 100 + 1000 + 10 + 100 + 1000 + 10 + 100 + 1000 + 10000
	// x = 1 + 30 + 300 + 3000 + 10000 = 13331
	if result != 13331 {
		t.Errorf("Expected 13331, got %d", result)
	}
}

// TestNative_ControlFlow_NestedIfWithManyPrintf tests the exact pattern from init_source.go
// that was causing bugs: nested ifs with Printf calls before/after/inside them.
// This pattern generates many internal labels from both:
// 1. Printf (uses global atomic counter for labels like __arm64_printf_hex_loop_N)
// 2. If statements (uses per-compiler counter for labels like .ir_if_true_N)
//
// The bug was that labels could collide when there are many Printf calls
// interspersed with nested ifs without else clauses.
//
// This test only compiles (doesn't run) because Printf makes syscalls that
// don't work on all platforms. The duplicate label detection in SetLabel()
// will panic at compile time if there's a label collision.
func TestNative_ControlFlow_NestedIfWithManyPrintf(t *testing.T) {
	flags := ir.Var("flags")
	captureStdout := ir.Var("captureStdout")
	pipeResult := ir.Var("pipeResult")
	marker1 := ir.Var("marker1")
	marker2 := ir.Var("marker2")
	marker3 := ir.Var("marker3")
	marker4 := ir.Var("marker4")
	marker5 := ir.Var("marker5")

	// This test mimics the pattern in init_source.go:
	// - Printf before the if
	// - Outer if with condition
	// - Printf inside outer if
	// - Nested if checking result
	// - Printf inside nested if
	// - Code after outer if must execute
	// - More Printf calls
	// - Second if block
	// - Printf after

	// Just compile - the duplicate label detection will catch collisions
	method := ir.Method{
		ir.Assign(flags, ir.Int64(0x1)),
		ir.Assign(marker1, ir.Int64(0)),
		ir.Assign(marker2, ir.Int64(0)),
		ir.Assign(marker3, ir.Int64(0)),
		ir.Assign(marker4, ir.Int64(0)),
		ir.Assign(marker5, ir.Int64(0)),
		ir.Assign(pipeResult, ir.Int64(0)),

		// Printf before first if (generates labels)
		ir.Printf("flags: %x\n", flags),

		// captureStdout = flags & 0x1
		ir.Assign(captureStdout, ir.Op(ir.OpAnd, flags, ir.Int64(0x1))),

		// Printf after assignment (more labels)
		ir.Printf("captureStdout: %x\n", captureStdout),

		// First if: if captureStdout != 0
		ir.If(ir.IsNotEqual(captureStdout, ir.Int64(0)), ir.Block{
			// Printf inside first if
			ir.Printf("entering capture block\n"),

			// Simulate syscall result
			ir.Assign(pipeResult, ir.Int64(5)),

			// Printf before nested if
			ir.Printf("pipeResult: %x\n", pipeResult),

			// Nested if: if pipeResult >= 0
			ir.If(ir.IsGreaterOrEqual(pipeResult, ir.Int64(0)), ir.Block{
				// Printf inside nested if
				ir.Printf("pipe success\n"),
				ir.Assign(marker1, ir.Int64(1)),
			}),
			// End of nested if - code here must execute

			// Printf after nested if (still in outer if)
			ir.Printf("after nested if\n"),
			ir.Assign(marker2, ir.Int64(1)),
		}),
		// End of outer if - code here MUST execute

		// Printf after outer if
		ir.Printf("after outer if\n"),
		ir.Assign(marker3, ir.Int64(1)),

		// Second if block (independent)
		ir.If(ir.IsNotEqual(captureStdout, ir.Int64(0)), ir.Block{
			ir.Printf("second if block\n"),
			ir.Assign(marker4, ir.Int64(1)),
		}),

		// Printf and assignment after second if
		ir.Printf("after second if\n"),
		ir.Assign(marker5, ir.Int64(1)),

		// Return encoded markers: m5<<4 | m4<<3 | m3<<2 | m2<<1 | m1
		ir.Return(ir.Op(ir.OpOr,
			ir.Op(ir.OpOr,
				ir.Op(ir.OpOr,
					ir.Op(ir.OpOr,
						ir.Op(ir.OpShl, marker5, ir.Int64(4)),
						ir.Op(ir.OpShl, marker4, ir.Int64(3))),
					ir.Op(ir.OpShl, marker3, ir.Int64(2))),
				ir.Op(ir.OpShl, marker2, ir.Int64(1))),
			marker1)),
	}

	// This should not panic if labels are unique
	prog := &ir.Program{
		Entrypoint: "main",
		Methods: map[string]ir.Method{
			"main": method,
		},
	}
	_, err := BuildStandaloneProgram(prog)
	if err != nil {
		t.Fatalf("Compilation failed: %v", err)
	}
}

// TestNative_ControlFlow_ThreeNestedIfsWithPrintf tests 3-level nesting with Printf
// at each level, matching the pattern that was failing in init_source.go.
// This test only compiles (doesn't run) because Printf makes syscalls.
func TestNative_ControlFlow_ThreeNestedIfsWithPrintf(t *testing.T) {
	a := ir.Var("a")
	b := ir.Var("b")
	c := ir.Var("c")
	m1 := ir.Var("m1")
	m2 := ir.Var("m2")
	m3 := ir.Var("m3")
	m4 := ir.Var("m4")
	m5 := ir.Var("m5")
	m6 := ir.Var("m6")

	method := ir.Method{
		ir.Assign(a, ir.Int64(1)),
		ir.Assign(b, ir.Int64(1)),
		ir.Assign(c, ir.Int64(1)),
		ir.Assign(m1, ir.Int64(0)),
		ir.Assign(m2, ir.Int64(0)),
		ir.Assign(m3, ir.Int64(0)),
		ir.Assign(m4, ir.Int64(0)),
		ir.Assign(m5, ir.Int64(0)),
		ir.Assign(m6, ir.Int64(0)),

		ir.Printf("start: a=%x b=%x c=%x\n", a, b, c),

		// Level 1: if a != 0
		ir.If(ir.IsNotEqual(a, ir.Int64(0)), ir.Block{
			ir.Printf("level 1 entered\n"),
			ir.Assign(m1, ir.Int64(1)),

			// Level 2: if b != 0
			ir.If(ir.IsNotEqual(b, ir.Int64(0)), ir.Block{
				ir.Printf("level 2 entered\n"),
				ir.Assign(m2, ir.Int64(1)),

				// Level 3: if c != 0
				ir.If(ir.IsNotEqual(c, ir.Int64(0)), ir.Block{
					ir.Printf("level 3 entered\n"),
					ir.Assign(m3, ir.Int64(1)),
				}),
				// After level 3 (still in level 2)
				ir.Printf("after level 3\n"),
				ir.Assign(m4, ir.Int64(1)),
			}),
			// After level 2 (still in level 1)
			ir.Printf("after level 2\n"),
			ir.Assign(m5, ir.Int64(1)),
		}),
		// After level 1
		ir.Printf("after level 1\n"),
		ir.Assign(m6, ir.Int64(1)),

		// Return: m6<<5 | m5<<4 | m4<<3 | m3<<2 | m2<<1 | m1
		ir.Return(ir.Op(ir.OpOr,
			ir.Op(ir.OpOr,
				ir.Op(ir.OpOr,
					ir.Op(ir.OpOr,
						ir.Op(ir.OpOr,
							ir.Op(ir.OpShl, m6, ir.Int64(5)),
							ir.Op(ir.OpShl, m5, ir.Int64(4))),
						ir.Op(ir.OpShl, m4, ir.Int64(3))),
					ir.Op(ir.OpShl, m3, ir.Int64(2))),
				ir.Op(ir.OpShl, m2, ir.Int64(1))),
			m1)),
	}

	// This should compile successfully if labels are unique
	prog := &ir.Program{
		Entrypoint: "main",
		Methods: map[string]ir.Method{
			"main": method,
		},
	}
	_, err := BuildStandaloneProgram(prog)
	if err != nil {
		t.Fatalf("Compilation failed: %v", err)
	}
}
