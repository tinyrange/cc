//go:build (darwin || linux) && arm64

package arm64

import (
	"fmt"
	"testing"

	"github.com/tinyrange/cc/internal/ir"
)

// Register pressure stress tests for the ARM64 IR compiler.
// ARM64 has 16 general-purpose registers available, so these tests
// specifically stress the register allocator by using more than 16 variables.

// TestNative_RegisterPressure_20Vars tests 20 variables all live simultaneously.
// Expected: sum of 1..20 = 210
func TestNative_RegisterPressure_20Vars(t *testing.T) {
	vars := make([]ir.Var, 20)
	for i := range vars {
		vars[i] = ir.Var(fmt.Sprintf("v%d", i))
	}

	method := ir.Method{}

	// Assign values 1..20 to all variables
	for i, v := range vars {
		method = append(method, ir.Assign(v, ir.Int64(int64(i+1))))
	}

	// Sum them all - all variables must be live at this point
	sum := ir.Var("sum")
	method = append(method, ir.Assign(sum, vars[0]))
	for i := 1; i < len(vars); i++ {
		method = append(method, ir.Assign(sum, ir.Op(ir.OpAdd, sum, vars[i])))
	}
	method = append(method, ir.Return(sum))

	result := compileAndRun(t, method)
	// Sum of 1..20 = 20*21/2 = 210
	if result != 210 {
		t.Errorf("Expected 210, got %d", result)
	}
}

// TestNative_RegisterPressure_ChainedOps tests a long chain of operations.
// x1 = a+b; x2 = x1+c; x3 = x2+d; ... (8 inputs, 7 intermediates)
// Expected: 1+2+3+4+5+6+7+8 = 36
func TestNative_RegisterPressure_ChainedOps(t *testing.T) {
	// 8 input variables
	inputs := make([]ir.Var, 8)
	for i := range inputs {
		inputs[i] = ir.Var(fmt.Sprintf("in%d", i))
	}

	// 7 intermediate variables
	intermediates := make([]ir.Var, 7)
	for i := range intermediates {
		intermediates[i] = ir.Var(fmt.Sprintf("x%d", i))
	}

	method := ir.Method{}

	// Assign values 1..8 to inputs
	for i, v := range inputs {
		method = append(method, ir.Assign(v, ir.Int64(int64(i+1))))
	}

	// Chain: x0 = in0 + in1
	method = append(method, ir.Assign(intermediates[0], ir.Op(ir.OpAdd, inputs[0], inputs[1])))

	// x1 = x0 + in2, x2 = x1 + in3, etc.
	for i := 1; i < len(intermediates); i++ {
		method = append(method, ir.Assign(intermediates[i], ir.Op(ir.OpAdd, intermediates[i-1], inputs[i+1])))
	}

	method = append(method, ir.Return(intermediates[len(intermediates)-1]))

	result := compileAndRun(t, method)
	// Sum of 1..8 = 36
	if result != 36 {
		t.Errorf("Expected 36, got %d", result)
	}
}

// TestNative_RegisterPressure_NestedExpressions tests a binary tree of additions.
// 8 leaves, 7 intermediates forming a tree structure.
// Expected: 1+2+3+4+5+6+7+8 = 36
func TestNative_RegisterPressure_NestedExpressions(t *testing.T) {
	// 8 leaf variables
	leaves := make([]ir.Var, 8)
	for i := range leaves {
		leaves[i] = ir.Var(fmt.Sprintf("leaf%d", i))
	}

	// Intermediate nodes - we'll compute in tree structure
	// Level 1: 4 nodes (pairs of leaves)
	level1 := make([]ir.Var, 4)
	for i := range level1 {
		level1[i] = ir.Var(fmt.Sprintf("l1_%d", i))
	}

	// Level 2: 2 nodes
	level2 := make([]ir.Var, 2)
	for i := range level2 {
		level2[i] = ir.Var(fmt.Sprintf("l2_%d", i))
	}

	// Level 3: 1 root node
	root := ir.Var("root")

	method := ir.Method{}

	// Assign values 1..8 to leaves
	for i, v := range leaves {
		method = append(method, ir.Assign(v, ir.Int64(int64(i+1))))
	}

	// Level 1: combine pairs
	for i := 0; i < 4; i++ {
		method = append(method, ir.Assign(level1[i], ir.Op(ir.OpAdd, leaves[i*2], leaves[i*2+1])))
	}

	// Level 2: combine pairs of level1
	for i := 0; i < 2; i++ {
		method = append(method, ir.Assign(level2[i], ir.Op(ir.OpAdd, level1[i*2], level1[i*2+1])))
	}

	// Root: combine level2
	method = append(method, ir.Assign(root, ir.Op(ir.OpAdd, level2[0], level2[1])))
	method = append(method, ir.Return(root))

	result := compileAndRun(t, method)
	// Sum of 1..8 = 36
	if result != 36 {
		t.Errorf("Expected 36, got %d", result)
	}
}

// TestNative_RegisterPressure_ParallelSums tests 3 independent sums that are combined.
// sum1 = a+b+c, sum2 = d+e+f, sum3 = g+h+i, then total = sum1+sum2+sum3
// Expected: (1+2+3) + (4+5+6) + (7+8+9) = 45
func TestNative_RegisterPressure_ParallelSums(t *testing.T) {
	// 9 input variables
	vars := make([]ir.Var, 9)
	for i := range vars {
		vars[i] = ir.Var(fmt.Sprintf("v%d", i))
	}

	sum1 := ir.Var("sum1")
	sum2 := ir.Var("sum2")
	sum3 := ir.Var("sum3")
	total := ir.Var("total")

	method := ir.Method{}

	// Assign values 1..9
	for i, v := range vars {
		method = append(method, ir.Assign(v, ir.Int64(int64(i+1))))
	}

	// Compute parallel sums
	method = append(method, ir.Assign(sum1, ir.Op(ir.OpAdd, vars[0], ir.Op(ir.OpAdd, vars[1], vars[2]))))
	method = append(method, ir.Assign(sum2, ir.Op(ir.OpAdd, vars[3], ir.Op(ir.OpAdd, vars[4], vars[5]))))
	method = append(method, ir.Assign(sum3, ir.Op(ir.OpAdd, vars[6], ir.Op(ir.OpAdd, vars[7], vars[8]))))

	// Combine
	method = append(method, ir.Assign(total, ir.Op(ir.OpAdd, sum1, ir.Op(ir.OpAdd, sum2, sum3))))
	method = append(method, ir.Return(total))

	result := compileAndRun(t, method)
	// Sum of 1..9 = 45
	if result != 45 {
		t.Errorf("Expected 45, got %d", result)
	}
}

// TestNative_RegisterPressure_InterleavedOps tests 16 vars with interleaved operations.
// v[i] += v[i+8] for i in 0..7, then sum first 8 vars.
// Expected: (1+9) + (2+10) + (3+11) + (4+12) + (5+13) + (6+14) + (7+15) + (8+16) = 136
func TestNative_RegisterPressure_InterleavedOps(t *testing.T) {
	vars := make([]ir.Var, 16)
	for i := range vars {
		vars[i] = ir.Var(fmt.Sprintf("v%d", i))
	}

	method := ir.Method{}

	// Assign values 1..16
	for i, v := range vars {
		method = append(method, ir.Assign(v, ir.Int64(int64(i+1))))
	}

	// v[i] += v[i+8] for i in 0..7
	for i := 0; i < 8; i++ {
		method = append(method, ir.Assign(vars[i], ir.Op(ir.OpAdd, vars[i], vars[i+8])))
	}

	// Sum first 8
	sum := ir.Var("sum")
	method = append(method, ir.Assign(sum, vars[0]))
	for i := 1; i < 8; i++ {
		method = append(method, ir.Assign(sum, ir.Op(ir.OpAdd, sum, vars[i])))
	}
	method = append(method, ir.Return(sum))

	result := compileAndRun(t, method)
	// (1+9)+(2+10)+(3+11)+(4+12)+(5+13)+(6+14)+(7+15)+(8+16) = 10+12+14+16+18+20+22+24 = 136
	if result != 136 {
		t.Errorf("Expected 136, got %d", result)
	}
}

// TestNative_RegisterPressure_LoopWithManyVars tests a loop with 15 variables.
// Loop 3 times, adding all 15 variables to sum each iteration.
// Expected: 3 * (1+2+...+15) = 3 * 120 = 360
func TestNative_RegisterPressure_LoopWithManyVars(t *testing.T) {
	vars := make([]ir.Var, 15)
	for i := range vars {
		vars[i] = ir.Var(fmt.Sprintf("v%d", i))
	}

	sum := ir.Var("sum")
	iter := ir.Var("iter")

	method := ir.Method{}

	// Assign values 1..15
	for i, v := range vars {
		method = append(method, ir.Assign(v, ir.Int64(int64(i+1))))
	}

	method = append(method, ir.Assign(sum, ir.Int64(0)))
	method = append(method, ir.Assign(iter, ir.Int64(0)))

	// Loop 3 times
	loopBody := ir.Block{
		ir.If(ir.IsGreaterOrEqual(iter, ir.Int64(3)), ir.Goto(ir.Label("done"))),
	}

	// Add all vars to sum
	for _, v := range vars {
		loopBody = append(loopBody, ir.Assign(sum, ir.Op(ir.OpAdd, sum, v)))
	}

	loopBody = append(loopBody, ir.Assign(iter, ir.Op(ir.OpAdd, iter, ir.Int64(1))))
	loopBody = append(loopBody, ir.Goto(ir.Label("loop")))

	method = append(method, ir.DeclareLabel(ir.Label("loop"), loopBody))
	method = append(method, ir.DeclareLabel(ir.Label("done"), ir.Block{ir.Return(sum)}))

	result := compileAndRun(t, method)
	// 3 * sum(1..15) = 3 * 120 = 360
	if result != 360 {
		t.Errorf("Expected 360, got %d", result)
	}
}

// TestNative_RegisterPressure_ConditionalPaths tests if/else with different vars in each branch.
// Each branch uses a different set of variables.
// Expected: branch taken based on condition, computes different sums.
func TestNative_RegisterPressure_ConditionalPaths(t *testing.T) {
	// Variables for true branch
	trueVars := make([]ir.Var, 8)
	for i := range trueVars {
		trueVars[i] = ir.Var(fmt.Sprintf("t%d", i))
	}

	// Variables for false branch
	falseVars := make([]ir.Var, 8)
	for i := range falseVars {
		falseVars[i] = ir.Var(fmt.Sprintf("f%d", i))
	}

	cond := ir.Var("cond")
	result := ir.Var("result")

	method := ir.Method{}

	// Assign condition (1 = true branch)
	method = append(method, ir.Assign(cond, ir.Int64(1)))

	// Assign values to true branch vars (1..8)
	for i, v := range trueVars {
		method = append(method, ir.Assign(v, ir.Int64(int64(i+1))))
	}

	// Assign values to false branch vars (10..17)
	for i, v := range falseVars {
		method = append(method, ir.Assign(v, ir.Int64(int64(i+10))))
	}

	method = append(method, ir.Assign(result, ir.Int64(0)))

	// True branch: sum trueVars
	trueBranch := ir.Block{}
	for _, v := range trueVars {
		trueBranch = append(trueBranch, ir.Assign(result, ir.Op(ir.OpAdd, result, v)))
	}
	trueBranch = append(trueBranch, ir.Goto(ir.Label("end")))

	// False branch: sum falseVars
	falseBranch := ir.Block{}
	for _, v := range falseVars {
		falseBranch = append(falseBranch, ir.Assign(result, ir.Op(ir.OpAdd, result, v)))
	}
	falseBranch = append(falseBranch, ir.Goto(ir.Label("end")))

	method = append(method, ir.If(ir.IsNotEqual(cond, ir.Int64(0)), trueBranch, falseBranch))
	method = append(method, ir.DeclareLabel(ir.Label("end"), ir.Block{ir.Return(result)}))

	res := compileAndRun(t, method)
	// True branch: sum(1..8) = 36
	if res != 36 {
		t.Errorf("Expected 36, got %d", res)
	}
}

// TestNative_RegisterPressure_NestedLoops tests 5x5 nested loops with outer/inner variables.
// Expected: sum = 5 * 5 = 25 (counting iterations)
func TestNative_RegisterPressure_NestedLoops(t *testing.T) {
	outerIdx := ir.Var("outer_i")
	innerIdx := ir.Var("inner_j")
	outerLimit := ir.Var("outer_limit")
	innerLimit := ir.Var("inner_limit")
	count := ir.Var("count")

	method := ir.Method{
		ir.Assign(outerLimit, ir.Int64(5)),
		ir.Assign(innerLimit, ir.Int64(5)),
		ir.Assign(count, ir.Int64(0)),
		ir.Assign(outerIdx, ir.Int64(0)),
		ir.DeclareLabel(ir.Label("outer_loop"), ir.Block{
			ir.If(ir.IsGreaterOrEqual(outerIdx, outerLimit), ir.Goto(ir.Label("done"))),
			ir.Assign(innerIdx, ir.Int64(0)),
			ir.DeclareLabel(ir.Label("inner_loop"), ir.Block{
				ir.If(ir.IsGreaterOrEqual(innerIdx, innerLimit), ir.Goto(ir.Label("inner_done"))),
				ir.Assign(count, ir.Op(ir.OpAdd, count, ir.Int64(1))),
				ir.Assign(innerIdx, ir.Op(ir.OpAdd, innerIdx, ir.Int64(1))),
				ir.Goto(ir.Label("inner_loop")),
			}),
			ir.DeclareLabel(ir.Label("inner_done"), ir.Block{
				ir.Assign(outerIdx, ir.Op(ir.OpAdd, outerIdx, ir.Int64(1))),
				ir.Goto(ir.Label("outer_loop")),
			}),
		}),
		ir.DeclareLabel(ir.Label("done"), ir.Block{ir.Return(count)}),
	}

	result := compileAndRun(t, method)
	// 5 * 5 = 25
	if result != 25 {
		t.Errorf("Expected 25, got %d", result)
	}
}

// TestNative_RegisterPressure_ExpressionTree tests a tree with subtract and add operations.
// Computes: ((a-b) + (c-d)) + ((e-f) + (g-h))
// Expected: ((10-1) + (20-2)) + ((30-3) + (40-4)) = (9+18) + (27+36) = 27 + 63 = 90
func TestNative_RegisterPressure_ExpressionTree(t *testing.T) {
	a, b, c, d := ir.Var("a"), ir.Var("b"), ir.Var("c"), ir.Var("d")
	e, f, g, h := ir.Var("e"), ir.Var("f"), ir.Var("g"), ir.Var("h")
	t1, t2, t3, t4 := ir.Var("t1"), ir.Var("t2"), ir.Var("t3"), ir.Var("t4")
	t5, t6, result := ir.Var("t5"), ir.Var("t6"), ir.Var("result")

	method := ir.Method{
		ir.Assign(a, ir.Int64(10)),
		ir.Assign(b, ir.Int64(1)),
		ir.Assign(c, ir.Int64(20)),
		ir.Assign(d, ir.Int64(2)),
		ir.Assign(e, ir.Int64(30)),
		ir.Assign(f, ir.Int64(3)),
		ir.Assign(g, ir.Int64(40)),
		ir.Assign(h, ir.Int64(4)),

		ir.Assign(t1, ir.Op(ir.OpSub, a, b)), // 10-1=9
		ir.Assign(t2, ir.Op(ir.OpSub, c, d)), // 20-2=18
		ir.Assign(t3, ir.Op(ir.OpSub, e, f)), // 30-3=27
		ir.Assign(t4, ir.Op(ir.OpSub, g, h)), // 40-4=36

		ir.Assign(t5, ir.Op(ir.OpAdd, t1, t2)), // 9+18=27
		ir.Assign(t6, ir.Op(ir.OpAdd, t3, t4)), // 27+36=63

		ir.Assign(result, ir.Op(ir.OpAdd, t5, t6)), // 27+63=90

		ir.Return(result),
	}

	res := compileAndRun(t, method)
	if res != 90 {
		t.Errorf("Expected 90, got %d", res)
	}
}

// TestNative_RegisterPressure_SwapChain tests a chain of variable swaps.
// Expected: after swaps, values should be rotated.
func TestNative_RegisterPressure_SwapChain(t *testing.T) {
	v0, v1, v2, v3, v4 := ir.Var("v0"), ir.Var("v1"), ir.Var("v2"), ir.Var("v3"), ir.Var("v4")
	v5, v6, v7 := ir.Var("v5"), ir.Var("v6"), ir.Var("v7")
	tmp := ir.Var("tmp")

	method := ir.Method{
		// Initialize: v0=1, v1=2, ..., v7=8
		ir.Assign(v0, ir.Int64(1)),
		ir.Assign(v1, ir.Int64(2)),
		ir.Assign(v2, ir.Int64(3)),
		ir.Assign(v3, ir.Int64(4)),
		ir.Assign(v4, ir.Int64(5)),
		ir.Assign(v5, ir.Int64(6)),
		ir.Assign(v6, ir.Int64(7)),
		ir.Assign(v7, ir.Int64(8)),

		// Swap chain: v0<->v1, v2<->v3, v4<->v5, v6<->v7
		ir.Assign(tmp, v0),
		ir.Assign(v0, v1),
		ir.Assign(v1, tmp),

		ir.Assign(tmp, v2),
		ir.Assign(v2, v3),
		ir.Assign(v3, tmp),

		ir.Assign(tmp, v4),
		ir.Assign(v4, v5),
		ir.Assign(v5, tmp),

		ir.Assign(tmp, v6),
		ir.Assign(v6, v7),
		ir.Assign(v7, tmp),

		// After swaps: v0=2, v1=1, v2=4, v3=3, v4=6, v5=5, v6=8, v7=7
		// Sum should be 1+2+3+4+5+6+7+8 = 36
		ir.Return(ir.Op(ir.OpAdd, v0, ir.Op(ir.OpAdd, v1, ir.Op(ir.OpAdd, v2, ir.Op(ir.OpAdd, v3, ir.Op(ir.OpAdd, v4, ir.Op(ir.OpAdd, v5, ir.Op(ir.OpAdd, v6, v7)))))))),
	}

	result := compileAndRun(t, method)
	if result != 36 {
		t.Errorf("Expected 36, got %d", result)
	}
}

// TestNative_RegisterPressure_RotateValues tests rotating values through 8 variables in a loop.
// Expected: after 3 rotations, the final sum is still the same (1+2+...+8 = 36)
func TestNative_RegisterPressure_RotateValues(t *testing.T) {
	vars := make([]ir.Var, 8)
	for i := range vars {
		vars[i] = ir.Var(fmt.Sprintf("v%d", i))
	}
	tmp := ir.Var("tmp")
	iter := ir.Var("iter")
	sum := ir.Var("sum")

	method := ir.Method{}

	// Initialize: v[i] = i+1
	for i, v := range vars {
		method = append(method, ir.Assign(v, ir.Int64(int64(i+1))))
	}

	method = append(method, ir.Assign(iter, ir.Int64(0)))

	// Rotate 3 times
	rotateBody := ir.Block{
		ir.If(ir.IsGreaterOrEqual(iter, ir.Int64(3)), ir.Goto(ir.Label("done"))),
		// Rotate: tmp = v7, v7 = v6, v6 = v5, ..., v0 = tmp
		ir.Assign(tmp, vars[7]),
	}
	for i := 7; i > 0; i-- {
		rotateBody = append(rotateBody, ir.Assign(vars[i], vars[i-1]))
	}
	rotateBody = append(rotateBody, ir.Assign(vars[0], tmp))
	rotateBody = append(rotateBody, ir.Assign(iter, ir.Op(ir.OpAdd, iter, ir.Int64(1))))
	rotateBody = append(rotateBody, ir.Goto(ir.Label("rotate")))

	method = append(method, ir.DeclareLabel(ir.Label("rotate"), rotateBody))

	// Compute sum
	doneBody := ir.Block{ir.Assign(sum, vars[0])}
	for i := 1; i < len(vars); i++ {
		doneBody = append(doneBody, ir.Assign(sum, ir.Op(ir.OpAdd, sum, vars[i])))
	}
	doneBody = append(doneBody, ir.Return(sum))

	method = append(method, ir.DeclareLabel(ir.Label("done"), doneBody))

	result := compileAndRun(t, method)
	// Sum of 1..8 = 36 (rotation preserves sum)
	if result != 36 {
		t.Errorf("Expected 36, got %d", result)
	}
}

// TestNative_RegisterPressure_MatrixOps simulates 4x4 matrix element operations (16 vars).
// Computes the sum of all elements after some transformations.
// Expected: sum = 1+2+...+16 = 136
func TestNative_RegisterPressure_MatrixOps(t *testing.T) {
	// 4x4 matrix = 16 variables
	matrix := make([]ir.Var, 16)
	for i := range matrix {
		matrix[i] = ir.Var(fmt.Sprintf("m%d", i))
	}

	method := ir.Method{}

	// Initialize matrix[i] = i+1
	for i, v := range matrix {
		method = append(method, ir.Assign(v, ir.Int64(int64(i+1))))
	}

	// Add diagonal elements to corners (still keeps all vars live)
	// m[0] += m[5], m[3] += m[6], m[12] += m[9], m[15] += m[10]
	method = append(method, ir.Assign(matrix[0], ir.Op(ir.OpAdd, matrix[0], matrix[5])))    // 1+6=7
	method = append(method, ir.Assign(matrix[3], ir.Op(ir.OpAdd, matrix[3], matrix[6])))    // 4+7=11
	method = append(method, ir.Assign(matrix[12], ir.Op(ir.OpAdd, matrix[12], matrix[9])))  // 13+10=23
	method = append(method, ir.Assign(matrix[15], ir.Op(ir.OpAdd, matrix[15], matrix[10]))) // 16+11=27

	// Sum all elements
	sum := ir.Var("sum")
	method = append(method, ir.Assign(sum, matrix[0]))
	for i := 1; i < len(matrix); i++ {
		method = append(method, ir.Assign(sum, ir.Op(ir.OpAdd, sum, matrix[i])))
	}
	method = append(method, ir.Return(sum))

	result := compileAndRun(t, method)
	// Original sum: 1+2+...+16 = 136
	// Added: 5+6+9+10 = 30 (values that were added to corners)
	// But wait, we're adding matrix[5], matrix[6], matrix[9], matrix[10] which are 6,7,10,11
	// So total = 136 + 6+7+10+11 = 136 + 34 = 170
	if result != 170 {
		t.Errorf("Expected 170, got %d", result)
	}
}

// TestNative_RegisterPressure_BitManipulation tests many bitwise ops on many vars.
// Expected: specific bit pattern result.
func TestNative_RegisterPressure_BitManipulation(t *testing.T) {
	vars := make([]ir.Var, 12)
	for i := range vars {
		vars[i] = ir.Var(fmt.Sprintf("v%d", i))
	}

	method := ir.Method{
		// Initialize with bit patterns
		ir.Assign(vars[0], ir.Int64(0xFF)),
		ir.Assign(vars[1], ir.Int64(0xF0)),
		ir.Assign(vars[2], ir.Int64(0x0F)),
		ir.Assign(vars[3], ir.Int64(0xAA)),
		ir.Assign(vars[4], ir.Int64(0x55)),
		ir.Assign(vars[5], ir.Int64(0xCC)),
		ir.Assign(vars[6], ir.Int64(0x33)),
		ir.Assign(vars[7], ir.Int64(0x01)),
		ir.Assign(vars[8], ir.Int64(0x80)),
		ir.Assign(vars[9], ir.Int64(0x7F)),
		ir.Assign(vars[10], ir.Int64(0x00)),
		ir.Assign(vars[11], ir.Int64(0xFF)),

		// Various bitwise operations
		ir.Assign(vars[0], ir.Op(ir.OpAnd, vars[0], vars[1])),     // 0xFF & 0xF0 = 0xF0
		ir.Assign(vars[1], ir.Op(ir.OpOr, vars[2], vars[3])),      // 0x0F | 0xAA = 0xAF
		ir.Assign(vars[2], ir.Op(ir.OpXor, vars[4], vars[5])),     // 0x55 ^ 0xCC = 0x99
		ir.Assign(vars[3], ir.Op(ir.OpShl, vars[7], ir.Int64(4))), // 0x01 << 4 = 0x10
		ir.Assign(vars[4], ir.Op(ir.OpShr, vars[8], ir.Int64(4))), // 0x80 >> 4 = 0x08
		ir.Assign(vars[5], ir.Op(ir.OpAnd, vars[9], vars[11])),    // 0x7F & 0xFF = 0x7F

		// Combine results
		ir.Assign(vars[6], ir.Op(ir.OpOr, vars[0], vars[1])),  // 0xF0 | 0xAF = 0xFF
		ir.Assign(vars[7], ir.Op(ir.OpAnd, vars[2], vars[3])), // 0x99 & 0x10 = 0x10
		ir.Assign(vars[8], ir.Op(ir.OpXor, vars[4], vars[5])), // 0x08 ^ 0x7F = 0x77
	}

	// Final result: v6 + v7 + v8
	method = append(method, ir.Return(ir.Op(ir.OpAdd, vars[6], ir.Op(ir.OpAdd, vars[7], vars[8]))))

	result := compileAndRun(t, method)
	// 0xFF + 0x10 + 0x77 = 255 + 16 + 119 = 390
	if result != 390 {
		t.Errorf("Expected 390, got %d", result)
	}
}

// TestNative_RegisterPressure_MixedOperations tests add, sub, and, or, xor interleaved on 12 vars.
// Expected: specific computed result.
func TestNative_RegisterPressure_MixedOperations(t *testing.T) {
	vars := make([]ir.Var, 12)
	for i := range vars {
		vars[i] = ir.Var(fmt.Sprintf("v%d", i))
	}

	method := ir.Method{}

	// Initialize
	for i, v := range vars {
		method = append(method, ir.Assign(v, ir.Int64(int64(i+1))))
	}

	// Mixed operations
	method = append(method, ir.Assign(vars[0], ir.Op(ir.OpAdd, vars[0], vars[1])))    // 1+2=3
	method = append(method, ir.Assign(vars[2], ir.Op(ir.OpSub, vars[3], vars[2])))    // 4-3=1
	method = append(method, ir.Assign(vars[4], ir.Op(ir.OpAnd, vars[4], vars[5])))    // 5&6=4
	method = append(method, ir.Assign(vars[6], ir.Op(ir.OpOr, vars[6], vars[7])))     // 7|8=15
	method = append(method, ir.Assign(vars[8], ir.Op(ir.OpXor, vars[8], vars[9])))    // 9^10=3
	method = append(method, ir.Assign(vars[10], ir.Op(ir.OpAdd, vars[10], vars[11]))) // 11+12=23

	// Second round
	method = append(method, ir.Assign(vars[1], ir.Op(ir.OpSub, vars[0], vars[2])))  // 3-1=2
	method = append(method, ir.Assign(vars[3], ir.Op(ir.OpOr, vars[4], vars[6])))   // 4|15=15
	method = append(method, ir.Assign(vars[5], ir.Op(ir.OpAnd, vars[8], vars[10]))) // 3&23=3

	// Final sum
	result := ir.Var("result")
	method = append(method, ir.Assign(result, ir.Op(ir.OpAdd, vars[1], ir.Op(ir.OpAdd, vars[3], vars[5]))))
	method = append(method, ir.Return(result))

	res := compileAndRun(t, method)
	// 2 + 15 + 3 = 20
	if res != 20 {
		t.Errorf("Expected 20, got %d", res)
	}
}

// TestNative_RegisterPressure_Accumulator tests 4 accumulators updated in loop, sum at end.
// Expected: each accumulator incremented 10 times, sum = 4*10 = 40
func TestNative_RegisterPressure_Accumulator(t *testing.T) {
	acc0 := ir.Var("acc0")
	acc1 := ir.Var("acc1")
	acc2 := ir.Var("acc2")
	acc3 := ir.Var("acc3")
	iter := ir.Var("iter")
	limit := ir.Var("limit")
	result := ir.Var("result")

	method := ir.Method{
		ir.Assign(acc0, ir.Int64(0)),
		ir.Assign(acc1, ir.Int64(0)),
		ir.Assign(acc2, ir.Int64(0)),
		ir.Assign(acc3, ir.Int64(0)),
		ir.Assign(iter, ir.Int64(0)),
		ir.Assign(limit, ir.Int64(10)),

		ir.DeclareLabel(ir.Label("loop"), ir.Block{
			ir.If(ir.IsGreaterOrEqual(iter, limit), ir.Goto(ir.Label("done"))),
			ir.Assign(acc0, ir.Op(ir.OpAdd, acc0, ir.Int64(1))),
			ir.Assign(acc1, ir.Op(ir.OpAdd, acc1, ir.Int64(1))),
			ir.Assign(acc2, ir.Op(ir.OpAdd, acc2, ir.Int64(1))),
			ir.Assign(acc3, ir.Op(ir.OpAdd, acc3, ir.Int64(1))),
			ir.Assign(iter, ir.Op(ir.OpAdd, iter, ir.Int64(1))),
			ir.Goto(ir.Label("loop")),
		}),

		ir.DeclareLabel(ir.Label("done"), ir.Block{
			ir.Assign(result, ir.Op(ir.OpAdd, acc0, ir.Op(ir.OpAdd, acc1, ir.Op(ir.OpAdd, acc2, acc3)))),
			ir.Return(result),
		}),
	}

	res := compileAndRun(t, method)
	// 4 * 10 = 40
	if res != 40 {
		t.Errorf("Expected 40, got %d", res)
	}
}
