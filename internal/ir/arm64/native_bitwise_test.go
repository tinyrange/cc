//go:build (darwin || linux) && arm64

package arm64

import (
	"testing"

	"github.com/tinyrange/cc/internal/ir"
)

// Bitwise operation tests for the ARM64 IR compiler

// TestNative_Bitwise_AndMask tests AND with various bitmasks
func TestNative_Bitwise_AndMask(t *testing.T) {
	tests := []struct {
		value    int64
		mask     int64
		expected uintptr
	}{
		{0xDEADBEEF, 0xFF, 0xEF},
		{0xDEADBEEF, 0xF0F0, 0xB0E0},
		{0xFFFFFFFF, 0x0F0F0F0F, 0x0F0F0F0F},
		{0x123456789ABCDEF0, 0xFFFF, 0xDEF0},
		{-6148914691236517206, 0x5555555555555555, 0x0}, // 0xAAAAAAAAAAAAAAAA as int64
	}

	for _, tc := range tests {
		a := ir.Var("a")
		b := ir.Var("b")
		result := compileAndRun(t, ir.Method{
			ir.Assign(a, ir.Int64(tc.value)),
			ir.Assign(b, ir.Int64(tc.mask)),
			ir.Return(ir.Op(ir.OpAnd, a, b)),
		})
		if result != tc.expected {
			t.Errorf("0x%X & 0x%X: expected 0x%X, got 0x%X", tc.value, tc.mask, tc.expected, result)
		}
	}
}

// TestNative_Bitwise_OrCombine tests OR to combine bit fields
func TestNative_Bitwise_OrCombine(t *testing.T) {
	tests := []struct {
		a, b     int64
		expected uintptr
	}{
		{0xFF00, 0x00FF, 0xFFFF},
		{0xF0F0F0F0, 0x0F0F0F0F, 0xFFFFFFFF},
		{0x1234000000000000, 0x5678, 0x1234000000005678},
		{0x0, 0xDEADBEEF, 0xDEADBEEF},
		{0x1111000000000000, 0x0000222200002222, 0x1111222200002222},
	}

	for _, tc := range tests {
		a := ir.Var("a")
		b := ir.Var("b")
		result := compileAndRun(t, ir.Method{
			ir.Assign(a, ir.Int64(tc.a)),
			ir.Assign(b, ir.Int64(tc.b)),
			ir.Return(ir.Op(ir.OpOr, a, b)),
		})
		if result != tc.expected {
			t.Errorf("0x%X | 0x%X: expected 0x%X, got 0x%X", tc.a, tc.b, tc.expected, result)
		}
	}
}

// TestNative_Bitwise_XorToggle tests XOR to toggle bits
func TestNative_Bitwise_XorToggle(t *testing.T) {
	tests := []struct {
		value    int64
		toggle   int64
		expected uintptr
	}{
		{0xFF00, 0xFFFF, 0x00FF},
		{0xAAAAAAAA, 0xFFFFFFFF, 0x55555555},
		{0x12345678, 0x12345678, 0x0}, // XOR with self = 0
		{0x0, 0xDEADBEEF, 0xDEADBEEF},
		{-1, -1, 0x0}, // 0xFFFFFFFFFFFFFFFF ^ 0xFFFFFFFFFFFFFFFF = 0
	}

	for _, tc := range tests {
		a := ir.Var("a")
		b := ir.Var("b")
		result := compileAndRun(t, ir.Method{
			ir.Assign(a, ir.Int64(tc.value)),
			ir.Assign(b, ir.Int64(tc.toggle)),
			ir.Return(ir.Op(ir.OpXor, a, b)),
		})
		if result != tc.expected {
			t.Errorf("0x%X ^ 0x%X: expected 0x%X, got 0x%X", tc.value, tc.toggle, tc.expected, result)
		}
	}
}

// TestNative_Bitwise_ShiftLeftChain tests multiple left shifts
func TestNative_Bitwise_ShiftLeftChain(t *testing.T) {
	// 1 << 4 << 4 = 1 << 8 = 256
	a := ir.Var("a")
	tmp := ir.Var("tmp")
	result := compileAndRun(t, ir.Method{
		ir.Assign(a, ir.Int64(1)),
		ir.Assign(tmp, ir.Op(ir.OpShl, a, ir.Int64(4))),
		ir.Assign(tmp, ir.Op(ir.OpShl, tmp, ir.Int64(4))),
		ir.Return(tmp),
	})
	if result != 256 {
		t.Errorf("1 << 4 << 4: expected 256, got %d", result)
	}

	// 0x1 << 16 << 16 << 16 = 0x1000000000000
	result = compileAndRun(t, ir.Method{
		ir.Assign(a, ir.Int64(1)),
		ir.Assign(tmp, ir.Op(ir.OpShl, a, ir.Int64(16))),
		ir.Assign(tmp, ir.Op(ir.OpShl, tmp, ir.Int64(16))),
		ir.Assign(tmp, ir.Op(ir.OpShl, tmp, ir.Int64(16))),
		ir.Return(tmp),
	})
	if result != 0x1000000000000 {
		t.Errorf("1 << 16 << 16 << 16: expected 0x1000000000000, got 0x%X", result)
	}
}

// TestNative_Bitwise_ShiftRightChain tests multiple right shifts
func TestNative_Bitwise_ShiftRightChain(t *testing.T) {
	// 0x10000 >> 4 >> 4 = 0x10000 >> 8 = 0x100
	a := ir.Var("a")
	tmp := ir.Var("tmp")
	result := compileAndRun(t, ir.Method{
		ir.Assign(a, ir.Int64(0x10000)),
		ir.Assign(tmp, ir.Op(ir.OpShr, a, ir.Int64(4))),
		ir.Assign(tmp, ir.Op(ir.OpShr, tmp, ir.Int64(4))),
		ir.Return(tmp),
	})
	if result != 0x100 {
		t.Errorf("0x10000 >> 4 >> 4: expected 0x100, got 0x%X", result)
	}

	// 0x1234567800000000 >> 16 >> 16 = 0x12345678
	result = compileAndRun(t, ir.Method{
		ir.Assign(a, ir.Int64(0x1234567800000000)),
		ir.Assign(tmp, ir.Op(ir.OpShr, a, ir.Int64(16))),
		ir.Assign(tmp, ir.Op(ir.OpShr, tmp, ir.Int64(16))),
		ir.Return(tmp),
	})
	if result != 0x12345678 {
		t.Errorf("0x1234567800000000 >> 32: expected 0x12345678, got 0x%X", result)
	}
}

// TestNative_Bitwise_RotateLeft simulates rotate left: (x << n) | (x >> (64-n))
func TestNative_Bitwise_RotateLeft(t *testing.T) {
	// Rotate 0x8000000000000001 left by 4 should give 0x0000000000000018
	x := ir.Var("x")
	left := ir.Var("left")
	right := ir.Var("right")

	result := compileAndRun(t, ir.Method{
		ir.Assign(x, ir.Int64(-9223372036854775807)), // 0x8000000000000001 as int64
		ir.Assign(left, ir.Op(ir.OpShl, x, ir.Int64(4))),
		ir.Assign(right, ir.Op(ir.OpShr, x, ir.Int64(60))), // 64-4=60
		ir.Return(ir.Op(ir.OpOr, left, right)),
	})
	// 0x8000000000000001 rotated left 4 = 0x0000000000000018
	if result != 0x18 {
		t.Errorf("ROL(0x8000000000000001, 4): expected 0x18, got 0x%X", result)
	}

	// Rotate 0x123456789ABCDEF0 left by 8
	result = compileAndRun(t, ir.Method{
		ir.Assign(x, ir.Int64(0x123456789ABCDEF0)),
		ir.Assign(left, ir.Op(ir.OpShl, x, ir.Int64(8))),
		ir.Assign(right, ir.Op(ir.OpShr, x, ir.Int64(56))), // 64-8=56
		ir.Return(ir.Op(ir.OpOr, left, right)),
	})
	// 0x123456789ABCDEF0 rotated left 8 = 0x3456789ABCDEF012
	if result != 0x3456789ABCDEF012 {
		t.Errorf("ROL(0x123456789ABCDEF0, 8): expected 0x3456789ABCDEF012, got 0x%X", result)
	}
}

// TestNative_Bitwise_RotateRight simulates rotate right: (x >> n) | (x << (64-n))
func TestNative_Bitwise_RotateRight(t *testing.T) {
	// Rotate 0x123456789ABCDEF0 right by 8
	x := ir.Var("x")
	right := ir.Var("right")
	left := ir.Var("left")

	result := compileAndRun(t, ir.Method{
		ir.Assign(x, ir.Int64(0x123456789ABCDEF0)),
		ir.Assign(right, ir.Op(ir.OpShr, x, ir.Int64(8))),
		ir.Assign(left, ir.Op(ir.OpShl, x, ir.Int64(56))), // 64-8=56
		ir.Return(ir.Op(ir.OpOr, right, left)),
	})
	// 0x123456789ABCDEF0 rotated right 8 = 0xF0123456789ABCDE
	if result != 0xF0123456789ABCDE {
		t.Errorf("ROR(0x123456789ABCDEF0, 8): expected 0xF0123456789ABCDE, got 0x%X", result)
	}

	// Rotate 0x0000000000000001 right by 1 = 0x8000000000000000
	result = compileAndRun(t, ir.Method{
		ir.Assign(x, ir.Int64(1)),
		ir.Assign(right, ir.Op(ir.OpShr, x, ir.Int64(1))),
		ir.Assign(left, ir.Op(ir.OpShl, x, ir.Int64(63))), // 64-1=63
		ir.Return(ir.Op(ir.OpOr, right, left)),
	})
	if result != 0x8000000000000000 {
		t.Errorf("ROR(1, 1): expected 0x8000000000000000, got 0x%X", result)
	}
}

// TestNative_Bitwise_ExtractField extracts a bit field with shift+mask
func TestNative_Bitwise_ExtractField(t *testing.T) {
	// Extract bits 8-15 from 0xDEADBEEF (should be 0xBE)
	x := ir.Var("x")
	tmp := ir.Var("tmp")

	result := compileAndRun(t, ir.Method{
		ir.Assign(x, ir.Int64(0xDEADBEEF)),
		ir.Assign(tmp, ir.Op(ir.OpShr, x, ir.Int64(8))),
		ir.Return(ir.Op(ir.OpAnd, tmp, ir.Int64(0xFF))),
	})
	if result != 0xBE {
		t.Errorf("Extract bits 8-15 from 0xDEADBEEF: expected 0xBE, got 0x%X", result)
	}

	// Extract bits 16-23 from 0x123456789ABCDEF0 (should be 0xBC)
	// bits 16-23 = byte at position 2 (0-indexed from LSB) = 0xBC
	result = compileAndRun(t, ir.Method{
		ir.Assign(x, ir.Int64(0x123456789ABCDEF0)),
		ir.Assign(tmp, ir.Op(ir.OpShr, x, ir.Int64(16))),
		ir.Return(ir.Op(ir.OpAnd, tmp, ir.Int64(0xFF))),
	})
	if result != 0xBC {
		t.Errorf("Extract bits 16-23 from 0x123456789ABCDEF0: expected 0xBC, got 0x%X", result)
	}

	// Extract 4-bit nibble at position 20 from 0xABCDEF12 (should be 0xC)
	result = compileAndRun(t, ir.Method{
		ir.Assign(x, ir.Int64(0xABCDEF12)),
		ir.Assign(tmp, ir.Op(ir.OpShr, x, ir.Int64(20))),
		ir.Return(ir.Op(ir.OpAnd, tmp, ir.Int64(0xF))),
	})
	if result != 0xC {
		t.Errorf("Extract nibble at position 20 from 0xABCDEF12: expected 0xC, got 0x%X", result)
	}
}

// TestNative_Bitwise_InsertField inserts a bit field: (base & ~mask) | ((val << pos) & mask)
func TestNative_Bitwise_InsertField(t *testing.T) {
	// Insert 0xAA into bits 8-15 of 0x12345678, resulting in 0x1234AA78
	base := ir.Var("base")
	val := ir.Var("val")
	mask := ir.Var("mask")
	invMask := ir.Var("invMask")
	shifted := ir.Var("shifted")
	cleared := ir.Var("cleared")
	masked := ir.Var("masked")

	result := compileAndRun(t, ir.Method{
		ir.Assign(base, ir.Int64(0x12345678)),
		ir.Assign(val, ir.Int64(0xAA)),
		ir.Assign(mask, ir.Int64(0xFF00)),                       // mask for bits 8-15
		ir.Assign(invMask, ir.Op(ir.OpXor, mask, ir.Int64(-1))), // ~mask
		ir.Assign(shifted, ir.Op(ir.OpShl, val, ir.Int64(8))),
		ir.Assign(cleared, ir.Op(ir.OpAnd, base, invMask)),
		ir.Assign(masked, ir.Op(ir.OpAnd, shifted, mask)),
		ir.Return(ir.Op(ir.OpOr, cleared, masked)),
	})
	// 0x12345678 with bits 8-15 replaced by 0xAA = 0x1234AA78
	if result != 0x1234AA78 {
		t.Errorf("Insert 0xAA into bits 8-15 of 0x12345678: expected 0x1234AA78, got 0x%X", result)
	}
}

// TestNative_Bitwise_ClearBits clears specific bits with AND ~mask
func TestNative_Bitwise_ClearBits(t *testing.T) {
	x := ir.Var("x")
	mask := ir.Var("mask")
	invMask := ir.Var("invMask")

	// Clear bits 0-7 of 0xDEADBEEF
	result := compileAndRun(t, ir.Method{
		ir.Assign(x, ir.Int64(0xDEADBEEF)),
		ir.Assign(mask, ir.Int64(0xFF)),
		ir.Assign(invMask, ir.Op(ir.OpXor, mask, ir.Int64(-1))), // ~mask
		ir.Return(ir.Op(ir.OpAnd, x, invMask)),
	})
	if result != 0xDEADBE00 {
		t.Errorf("Clear bits 0-7 of 0xDEADBEEF: expected 0xDEADBE00, got 0x%X", result)
	}

	// Clear bits 8-15 of 0xFFFFFFFF
	result = compileAndRun(t, ir.Method{
		ir.Assign(x, ir.Int64(0xFFFFFFFF)),
		ir.Assign(mask, ir.Int64(0xFF00)),
		ir.Assign(invMask, ir.Op(ir.OpXor, mask, ir.Int64(-1))),
		ir.Return(ir.Op(ir.OpAnd, x, invMask)),
	})
	if result != 0xFFFF00FF {
		t.Errorf("Clear bits 8-15 of 0xFFFFFFFF: expected 0xFFFF00FF, got 0x%X", result)
	}
}

// TestNative_Bitwise_SetBits sets specific bits with OR
func TestNative_Bitwise_SetBits(t *testing.T) {
	x := ir.Var("x")
	bits := ir.Var("bits")

	// Set bits 4-7 of 0x00
	result := compileAndRun(t, ir.Method{
		ir.Assign(x, ir.Int64(0x00)),
		ir.Assign(bits, ir.Int64(0xF0)),
		ir.Return(ir.Op(ir.OpOr, x, bits)),
	})
	if result != 0xF0 {
		t.Errorf("Set bits 4-7 of 0x00: expected 0xF0, got 0x%X", result)
	}

	// Set bit 63 of 0x0
	result = compileAndRun(t, ir.Method{
		ir.Assign(x, ir.Int64(0)),
		ir.Assign(bits, ir.Op(ir.OpShl, ir.Int64(1), ir.Int64(63))),
		ir.Return(ir.Op(ir.OpOr, x, bits)),
	})
	if result != 0x8000000000000000 {
		t.Errorf("Set bit 63 of 0: expected 0x8000000000000000, got 0x%X", result)
	}
}

// TestNative_Bitwise_FlipBits flips specific bits with XOR
func TestNative_Bitwise_FlipBits(t *testing.T) {
	x := ir.Var("x")
	flip := ir.Var("flip")

	// Flip bits 0-7 of 0xFF (should become 0x00 in those bits)
	result := compileAndRun(t, ir.Method{
		ir.Assign(x, ir.Int64(0xFF)),
		ir.Assign(flip, ir.Int64(0xFF)),
		ir.Return(ir.Op(ir.OpXor, x, flip)),
	})
	if result != 0x0 {
		t.Errorf("Flip bits 0-7 of 0xFF: expected 0x0, got 0x%X", result)
	}

	// Flip alternating bits of 0xAAAAAAAAAAAAAAAA
	result = compileAndRun(t, ir.Method{
		ir.Assign(x, ir.Int64(-6148914691236517206)), // 0xAAAAAAAAAAAAAAAA as int64
		ir.Assign(flip, ir.Int64(0x5555555555555555)),
		ir.Return(ir.Op(ir.OpXor, x, flip)),
	})
	if result != 0xFFFFFFFFFFFFFFFF {
		t.Errorf("Flip alternating bits: expected 0xFFFFFFFFFFFFFFFF, got 0x%X", result)
	}
}

// TestNative_Bitwise_CountLeadingZeros implements manual CLZ using binary search
func TestNative_Bitwise_CountLeadingZeros(t *testing.T) {
	// Manual CLZ for a 64-bit value using binary search
	// This tests complex control flow with bitwise operations
	x := ir.Var("x")
	n := ir.Var("n")
	tmp := ir.Var("tmp")

	// Test with 0x0000000080000000 (32 leading zeros)
	result := compileAndRun(t, ir.Method{
		ir.Assign(x, ir.Int64(0x80000000)),
		ir.Assign(n, ir.Int64(0)),

		// Check if upper 32 bits are zero
		ir.Assign(tmp, ir.Op(ir.OpShr, x, ir.Int64(32))),
		ir.If(ir.IsZero(tmp), ir.Block{
			ir.Assign(n, ir.Op(ir.OpAdd, n, ir.Int64(32))),
			ir.Assign(x, ir.Op(ir.OpShl, x, ir.Int64(32))),
		}),

		// Check if upper 16 bits are zero
		ir.Assign(tmp, ir.Op(ir.OpShr, x, ir.Int64(48))),
		ir.If(ir.IsZero(tmp), ir.Block{
			ir.Assign(n, ir.Op(ir.OpAdd, n, ir.Int64(16))),
			ir.Assign(x, ir.Op(ir.OpShl, x, ir.Int64(16))),
		}),

		// Check if upper 8 bits are zero
		ir.Assign(tmp, ir.Op(ir.OpShr, x, ir.Int64(56))),
		ir.If(ir.IsZero(tmp), ir.Block{
			ir.Assign(n, ir.Op(ir.OpAdd, n, ir.Int64(8))),
			ir.Assign(x, ir.Op(ir.OpShl, x, ir.Int64(8))),
		}),

		// Check if upper 4 bits are zero
		ir.Assign(tmp, ir.Op(ir.OpShr, x, ir.Int64(60))),
		ir.If(ir.IsZero(tmp), ir.Block{
			ir.Assign(n, ir.Op(ir.OpAdd, n, ir.Int64(4))),
			ir.Assign(x, ir.Op(ir.OpShl, x, ir.Int64(4))),
		}),

		// Check if upper 2 bits are zero
		ir.Assign(tmp, ir.Op(ir.OpShr, x, ir.Int64(62))),
		ir.If(ir.IsZero(tmp), ir.Block{
			ir.Assign(n, ir.Op(ir.OpAdd, n, ir.Int64(2))),
			ir.Assign(x, ir.Op(ir.OpShl, x, ir.Int64(2))),
		}),

		// Check if uppermost bit is zero
		ir.Assign(tmp, ir.Op(ir.OpShr, x, ir.Int64(63))),
		ir.If(ir.IsZero(tmp), ir.Block{
			ir.Assign(n, ir.Op(ir.OpAdd, n, ir.Int64(1))),
		}),

		ir.Return(n),
	})
	// 0x80000000 has 32 leading zeros in a 64-bit representation
	if result != 32 {
		t.Errorf("CLZ(0x80000000): expected 32, got %d", result)
	}

	// Test with 0x8000000000000000 (0 leading zeros)
	result = compileAndRun(t, ir.Method{
		ir.Assign(x, ir.Int64(-9223372036854775808)), // 0x8000000000000000 as int64
		ir.Assign(n, ir.Int64(0)),

		ir.Assign(tmp, ir.Op(ir.OpShr, x, ir.Int64(32))),
		ir.If(ir.IsZero(tmp), ir.Block{
			ir.Assign(n, ir.Op(ir.OpAdd, n, ir.Int64(32))),
			ir.Assign(x, ir.Op(ir.OpShl, x, ir.Int64(32))),
		}),

		ir.Assign(tmp, ir.Op(ir.OpShr, x, ir.Int64(48))),
		ir.If(ir.IsZero(tmp), ir.Block{
			ir.Assign(n, ir.Op(ir.OpAdd, n, ir.Int64(16))),
			ir.Assign(x, ir.Op(ir.OpShl, x, ir.Int64(16))),
		}),

		ir.Assign(tmp, ir.Op(ir.OpShr, x, ir.Int64(56))),
		ir.If(ir.IsZero(tmp), ir.Block{
			ir.Assign(n, ir.Op(ir.OpAdd, n, ir.Int64(8))),
			ir.Assign(x, ir.Op(ir.OpShl, x, ir.Int64(8))),
		}),

		ir.Assign(tmp, ir.Op(ir.OpShr, x, ir.Int64(60))),
		ir.If(ir.IsZero(tmp), ir.Block{
			ir.Assign(n, ir.Op(ir.OpAdd, n, ir.Int64(4))),
			ir.Assign(x, ir.Op(ir.OpShl, x, ir.Int64(4))),
		}),

		ir.Assign(tmp, ir.Op(ir.OpShr, x, ir.Int64(62))),
		ir.If(ir.IsZero(tmp), ir.Block{
			ir.Assign(n, ir.Op(ir.OpAdd, n, ir.Int64(2))),
			ir.Assign(x, ir.Op(ir.OpShl, x, ir.Int64(2))),
		}),

		ir.Assign(tmp, ir.Op(ir.OpShr, x, ir.Int64(63))),
		ir.If(ir.IsZero(tmp), ir.Block{
			ir.Assign(n, ir.Op(ir.OpAdd, n, ir.Int64(1))),
		}),

		ir.Return(n),
	})
	if result != 0 {
		t.Errorf("CLZ(0x8000000000000000): expected 0, got %d", result)
	}
}

// TestNative_Bitwise_PopulationCount implements manual popcount using parallel bit counting
func TestNative_Bitwise_PopulationCount(t *testing.T) {
	// Simplified popcount for 8-bit value using parallel counting
	x := ir.Var("x")
	tmp := ir.Var("tmp")

	// Count bits in 0xFF (should be 8)
	result := compileAndRun(t, ir.Method{
		ir.Assign(x, ir.Int64(0xFF)),

		// x = (x & 0x55) + ((x >> 1) & 0x55) - count bits in pairs
		ir.Assign(tmp, ir.Op(ir.OpAnd, x, ir.Int64(0x55))),
		ir.Assign(x, ir.Op(ir.OpShr, x, ir.Int64(1))),
		ir.Assign(x, ir.Op(ir.OpAnd, x, ir.Int64(0x55))),
		ir.Assign(x, ir.Op(ir.OpAdd, tmp, x)),

		// x = (x & 0x33) + ((x >> 2) & 0x33) - count bits in nibbles
		ir.Assign(tmp, ir.Op(ir.OpAnd, x, ir.Int64(0x33))),
		ir.Assign(x, ir.Op(ir.OpShr, x, ir.Int64(2))),
		ir.Assign(x, ir.Op(ir.OpAnd, x, ir.Int64(0x33))),
		ir.Assign(x, ir.Op(ir.OpAdd, tmp, x)),

		// x = (x & 0x0F) + ((x >> 4) & 0x0F) - count bits in bytes
		ir.Assign(tmp, ir.Op(ir.OpAnd, x, ir.Int64(0x0F))),
		ir.Assign(x, ir.Op(ir.OpShr, x, ir.Int64(4))),
		ir.Assign(x, ir.Op(ir.OpAnd, x, ir.Int64(0x0F))),
		ir.Assign(x, ir.Op(ir.OpAdd, tmp, x)),

		ir.Return(x),
	})
	if result != 8 {
		t.Errorf("popcount(0xFF): expected 8, got %d", result)
	}

	// Count bits in 0xAA (10101010 = 4 bits)
	result = compileAndRun(t, ir.Method{
		ir.Assign(x, ir.Int64(0xAA)),

		ir.Assign(tmp, ir.Op(ir.OpAnd, x, ir.Int64(0x55))),
		ir.Assign(x, ir.Op(ir.OpShr, x, ir.Int64(1))),
		ir.Assign(x, ir.Op(ir.OpAnd, x, ir.Int64(0x55))),
		ir.Assign(x, ir.Op(ir.OpAdd, tmp, x)),

		ir.Assign(tmp, ir.Op(ir.OpAnd, x, ir.Int64(0x33))),
		ir.Assign(x, ir.Op(ir.OpShr, x, ir.Int64(2))),
		ir.Assign(x, ir.Op(ir.OpAnd, x, ir.Int64(0x33))),
		ir.Assign(x, ir.Op(ir.OpAdd, tmp, x)),

		ir.Assign(tmp, ir.Op(ir.OpAnd, x, ir.Int64(0x0F))),
		ir.Assign(x, ir.Op(ir.OpShr, x, ir.Int64(4))),
		ir.Assign(x, ir.Op(ir.OpAnd, x, ir.Int64(0x0F))),
		ir.Assign(x, ir.Op(ir.OpAdd, tmp, x)),

		ir.Return(x),
	})
	if result != 4 {
		t.Errorf("popcount(0xAA): expected 4, got %d", result)
	}

	// Count bits in 0x12 (00010010 = 2 bits)
	result = compileAndRun(t, ir.Method{
		ir.Assign(x, ir.Int64(0x12)),

		ir.Assign(tmp, ir.Op(ir.OpAnd, x, ir.Int64(0x55))),
		ir.Assign(x, ir.Op(ir.OpShr, x, ir.Int64(1))),
		ir.Assign(x, ir.Op(ir.OpAnd, x, ir.Int64(0x55))),
		ir.Assign(x, ir.Op(ir.OpAdd, tmp, x)),

		ir.Assign(tmp, ir.Op(ir.OpAnd, x, ir.Int64(0x33))),
		ir.Assign(x, ir.Op(ir.OpShr, x, ir.Int64(2))),
		ir.Assign(x, ir.Op(ir.OpAnd, x, ir.Int64(0x33))),
		ir.Assign(x, ir.Op(ir.OpAdd, tmp, x)),

		ir.Assign(tmp, ir.Op(ir.OpAnd, x, ir.Int64(0x0F))),
		ir.Assign(x, ir.Op(ir.OpShr, x, ir.Int64(4))),
		ir.Assign(x, ir.Op(ir.OpAnd, x, ir.Int64(0x0F))),
		ir.Assign(x, ir.Op(ir.OpAdd, tmp, x)),

		ir.Return(x),
	})
	if result != 2 {
		t.Errorf("popcount(0x12): expected 2, got %d", result)
	}
}

// TestNative_Bitwise_MixedOps tests complex expression: ((a & b) | (c ^ d)) >> n
func TestNative_Bitwise_MixedOps(t *testing.T) {
	a := ir.Var("a")
	b := ir.Var("b")
	c := ir.Var("c")
	d := ir.Var("d")
	andResult := ir.Var("andResult")
	xorResult := ir.Var("xorResult")
	orResult := ir.Var("orResult")

	// ((0xFF00 & 0x0FF0) | (0xAAAA ^ 0x5555)) >> 4
	// = (0x0F00 | 0xFFFF) >> 4
	// = 0xFFFF >> 4
	// = 0x0FFF
	result := compileAndRun(t, ir.Method{
		ir.Assign(a, ir.Int64(0xFF00)),
		ir.Assign(b, ir.Int64(0x0FF0)),
		ir.Assign(c, ir.Int64(0xAAAA)),
		ir.Assign(d, ir.Int64(0x5555)),

		ir.Assign(andResult, ir.Op(ir.OpAnd, a, b)),
		ir.Assign(xorResult, ir.Op(ir.OpXor, c, d)),
		ir.Assign(orResult, ir.Op(ir.OpOr, andResult, xorResult)),
		ir.Return(ir.Op(ir.OpShr, orResult, ir.Int64(4))), // shift amount must be immediate
	})
	if result != 0x0FFF {
		t.Errorf("((0xFF00 & 0x0FF0) | (0xAAAA ^ 0x5555)) >> 4: expected 0x0FFF, got 0x%X", result)
	}

	// More complex: ((0xDEADBEEF & 0xFFFF0000) | (0x12345678 ^ 0x87654321)) >> 8
	// = (0xDEAD0000 | 0x95511559) >> 8
	// = 0xDFFD1559 >> 8
	// = 0x00DFFD15
	result = compileAndRun(t, ir.Method{
		ir.Assign(a, ir.Int64(0xDEADBEEF)),
		ir.Assign(b, ir.Int64(0xFFFF0000)),
		ir.Assign(c, ir.Int64(0x12345678)),
		ir.Assign(d, ir.Int64(0x87654321)),

		ir.Assign(andResult, ir.Op(ir.OpAnd, a, b)),
		ir.Assign(xorResult, ir.Op(ir.OpXor, c, d)),
		ir.Assign(orResult, ir.Op(ir.OpOr, andResult, xorResult)),
		ir.Return(ir.Op(ir.OpShr, orResult, ir.Int64(8))), // shift amount must be immediate
	})
	// 0xDEAD0000 | 0x95511559 = 0xDFFD1559
	// 0xDFFD1559 >> 8 = 0x00DFFD15
	if result != 0x00DFFD15 {
		t.Errorf("((0xDEADBEEF & 0xFFFF0000) | (0x12345678 ^ 0x87654321)) >> 8: expected 0x00DFFD15, got 0x%X", result)
	}

	// Test with 64-bit values
	// ((0x123456789ABCDEF0 & 0xFFFFFFFF00000000) | (0xAAAAAAAAAAAAAAAA ^ 0x5555555555555555)) >> 16
	result = compileAndRun(t, ir.Method{
		ir.Assign(a, ir.Int64(0x123456789ABCDEF0)),
		ir.Assign(b, ir.Int64(-4294967296)),          // 0xFFFFFFFF00000000 as int64
		ir.Assign(c, ir.Int64(-6148914691236517206)), // 0xAAAAAAAAAAAAAAAA as int64
		ir.Assign(d, ir.Int64(0x5555555555555555)),

		ir.Assign(andResult, ir.Op(ir.OpAnd, a, b)),
		ir.Assign(xorResult, ir.Op(ir.OpXor, c, d)),
		ir.Assign(orResult, ir.Op(ir.OpOr, andResult, xorResult)),
		ir.Return(ir.Op(ir.OpShr, orResult, ir.Int64(16))), // shift amount must be immediate
	})
	// 0x123456789ABCDEF0 & 0xFFFFFFFF00000000 = 0x1234567800000000
	// 0xAAAAAAAAAAAAAAAA ^ 0x5555555555555555 = 0xFFFFFFFFFFFFFFFF
	// 0x1234567800000000 | 0xFFFFFFFFFFFFFFFF = 0xFFFFFFFFFFFFFFFF
	// 0xFFFFFFFFFFFFFFFF >> 16 = 0x0000FFFFFFFFFFFF
	if result != 0x0000FFFFFFFFFFFF {
		t.Errorf("64-bit mixed ops: expected 0x0000FFFFFFFFFFFF, got 0x%X", result)
	}
}
