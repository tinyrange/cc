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
