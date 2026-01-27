//go:build (darwin || linux) && arm64

package arm64

import (
	"testing"

	"github.com/tinyrange/cc/internal/ir"
)

// TestNative_Memory_StackSlot8Bytes tests basic 8-byte stack slot read/write.
func TestNative_Memory_StackSlot8Bytes(t *testing.T) {
	x := ir.Var("x")
	result := compileAndRun(t, ir.Method{
		ir.WithStackSlot(ir.StackSlotConfig{
			Size: 8,
			Body: func(slot ir.StackSlot) ir.Fragment {
				return ir.Block{
					ir.Assign(slot.Base(), ir.Int64(0xDEADBEEF)),
					ir.Assign(x, slot.Base()),
				}
			},
		}),
		ir.Return(x),
	})
	if result != 0xDEADBEEF {
		t.Errorf("Expected 0xDEADBEEF, got 0x%x", result)
	}
}

// TestNative_Memory_StackSlot64Bytes tests large 64-byte stack slot.
func TestNative_Memory_StackSlot64Bytes(t *testing.T) {
	x := ir.Var("x")
	result := compileAndRun(t, ir.Method{
		ir.WithStackSlot(ir.StackSlotConfig{
			Size: 64,
			Body: func(slot ir.StackSlot) ir.Fragment {
				return ir.Block{
					// Write to first 8 bytes
					ir.Assign(slot.Base(), ir.Int64(111)),
					// Write to last 8 bytes (offset 56)
					ir.Assign(slot.At(ir.Int64(56)), ir.Int64(999)),
					// Read back from last 8 bytes
					ir.Assign(x, slot.At(ir.Int64(56))),
				}
			},
		}),
		ir.Return(x),
	})
	if result != 999 {
		t.Errorf("Expected 999, got %d", result)
	}
}

// TestNative_Memory_StackSlotNested tests stack slot inside stack slot.
func TestNative_Memory_StackSlotNested(t *testing.T) {
	outer := ir.Var("outer")
	inner := ir.Var("inner")
	result := compileAndRun(t, ir.Method{
		ir.WithStackSlot(ir.StackSlotConfig{
			Size: 8,
			Body: func(outerSlot ir.StackSlot) ir.Fragment {
				return ir.Block{
					ir.Assign(outerSlot.Base(), ir.Int64(100)),
					ir.WithStackSlot(ir.StackSlotConfig{
						Size: 8,
						Body: func(innerSlot ir.StackSlot) ir.Fragment {
							return ir.Block{
								ir.Assign(innerSlot.Base(), ir.Int64(200)),
								ir.Assign(inner, innerSlot.Base()),
							}
						},
					}),
					ir.Assign(outer, outerSlot.Base()),
				}
			},
		}),
		// outer should still be 100, inner should be 200
		// Return outer + inner = 300
		ir.Return(ir.Op(ir.OpAdd, outer, inner)),
	})
	if result != 300 {
		t.Errorf("Expected 300, got %d", result)
	}
}

// TestNative_Memory_StackSlotArray uses slot with displacement as array.
func TestNative_Memory_StackSlotArray(t *testing.T) {
	sum := ir.Var("sum")
	result := compileAndRun(t, ir.Method{
		ir.WithStackSlot(ir.StackSlotConfig{
			Size: 40, // 5 elements * 8 bytes
			Body: func(slot ir.StackSlot) ir.Fragment {
				return ir.Block{
					// Write array elements: arr[0]=10, arr[1]=20, arr[2]=30, arr[3]=40, arr[4]=50
					ir.Assign(slot.At(ir.Int64(0*8)), ir.Int64(10)),
					ir.Assign(slot.At(ir.Int64(1*8)), ir.Int64(20)),
					ir.Assign(slot.At(ir.Int64(2*8)), ir.Int64(30)),
					ir.Assign(slot.At(ir.Int64(3*8)), ir.Int64(40)),
					ir.Assign(slot.At(ir.Int64(4*8)), ir.Int64(50)),
					// Sum all elements
					ir.Assign(sum, slot.At(ir.Int64(0*8))),
					ir.Assign(sum, ir.Op(ir.OpAdd, sum, slot.At(ir.Int64(1*8)))),
					ir.Assign(sum, ir.Op(ir.OpAdd, sum, slot.At(ir.Int64(2*8)))),
					ir.Assign(sum, ir.Op(ir.OpAdd, sum, slot.At(ir.Int64(3*8)))),
					ir.Assign(sum, ir.Op(ir.OpAdd, sum, slot.At(ir.Int64(4*8)))),
				}
			},
		}),
		ir.Return(sum),
	})
	// 10 + 20 + 30 + 40 + 50 = 150
	if result != 150 {
		t.Errorf("Expected 150, got %d", result)
	}
}

// TestNative_Memory_StackSlotPreserve tests that value survives across many operations.
func TestNative_Memory_StackSlotPreserve(t *testing.T) {
	a := ir.Var("a")
	b := ir.Var("b")
	c := ir.Var("c")
	d := ir.Var("d")
	preserved := ir.Var("preserved")
	result := compileAndRun(t, ir.Method{
		ir.WithStackSlot(ir.StackSlotConfig{
			Size: 8,
			Body: func(slot ir.StackSlot) ir.Fragment {
				return ir.Block{
					// Store a value in the slot
					ir.Assign(slot.Base(), ir.Int64(42)),
					// Do many operations with other variables
					ir.Assign(a, ir.Int64(1)),
					ir.Assign(b, ir.Int64(2)),
					ir.Assign(c, ir.Op(ir.OpAdd, a, b)),
					ir.Assign(d, ir.Op(ir.OpShl, c, ir.Int64(4))),
					ir.Assign(a, ir.Op(ir.OpXor, a, d)),
					ir.Assign(b, ir.Op(ir.OpOr, b, a)),
					ir.Assign(c, ir.Op(ir.OpAnd, c, ir.Int64(0xFF))),
					ir.Assign(d, ir.Op(ir.OpSub, d, c)),
					// Read back from slot - should still be 42
					ir.Assign(preserved, slot.Base()),
				}
			},
		}),
		ir.Return(preserved),
	})
	if result != 42 {
		t.Errorf("Expected 42, got %d", result)
	}
}

// TestNative_Memory_MultipleSlots tests multiple stack slots simultaneously active.
func TestNative_Memory_MultipleSlots(t *testing.T) {
	v1 := ir.Var("v1")
	v2 := ir.Var("v2")
	v3 := ir.Var("v3")
	result := compileAndRun(t, ir.Method{
		ir.WithStackSlot(ir.StackSlotConfig{
			Size: 8,
			Body: func(slot1 ir.StackSlot) ir.Fragment {
				return ir.Block{
					ir.Assign(slot1.Base(), ir.Int64(100)),
					ir.WithStackSlot(ir.StackSlotConfig{
						Size: 8,
						Body: func(slot2 ir.StackSlot) ir.Fragment {
							return ir.Block{
								ir.Assign(slot2.Base(), ir.Int64(200)),
								ir.WithStackSlot(ir.StackSlotConfig{
									Size: 8,
									Body: func(slot3 ir.StackSlot) ir.Fragment {
										return ir.Block{
											ir.Assign(slot3.Base(), ir.Int64(300)),
											// Read all three slots
											ir.Assign(v1, slot1.Base()),
											ir.Assign(v2, slot2.Base()),
											ir.Assign(v3, slot3.Base()),
										}
									},
								}),
							}
						},
					}),
				}
			},
		}),
		// v1 + v2 + v3 = 100 + 200 + 300 = 600
		ir.Return(ir.Op(ir.OpAdd, ir.Op(ir.OpAdd, v1, v2), v3)),
	})
	if result != 600 {
		t.Errorf("Expected 600, got %d", result)
	}
}

// TestNative_Memory_SlotWidths tests 32-bit access using slot.Base().As32().
func TestNative_Memory_SlotWidths(t *testing.T) {
	low := ir.Var("low")
	high := ir.Var("high")
	result := compileAndRun(t, ir.Method{
		ir.WithStackSlot(ir.StackSlotConfig{
			Size: 8,
			Body: func(slot ir.StackSlot) ir.Fragment {
				// Access slot as StackSlotMemFragment which has As32()
				base := slot.Base().(ir.StackSlotMemFragment)
				at4 := slot.At(ir.Int64(4)).(ir.StackSlotMemFragment)
				return ir.Block{
					// Write 32-bit value to low part (offset 0)
					ir.Assign(base.As32(), ir.Int32(0x12345678)),
					// Write 32-bit value to high part (offset 4)
					ir.Assign(at4.As32(), ir.Int32(0x1ABCDEF0)),
					// Read back as 32-bit values
					ir.Assign(low, base.As32()),
					ir.Assign(high, at4.As32()),
				}
			},
		}),
		// Combine: (high << 32) | low
		ir.Return(ir.Op(ir.OpOr, ir.Op(ir.OpShl, high, ir.Int64(32)), low)),
	})
	expected := uintptr(0x1ABCDEF012345678)
	if result != expected {
		t.Errorf("Expected 0x%x, got 0x%x", expected, result)
	}
}

// TestNative_Memory_SlotAlignment tests aligned access patterns.
func TestNative_Memory_SlotAlignment(t *testing.T) {
	sum := ir.Var("sum")
	result := compileAndRun(t, ir.Method{
		ir.WithStackSlot(ir.StackSlotConfig{
			Size: 32, // 4 aligned 8-byte slots
			Body: func(slot ir.StackSlot) ir.Fragment {
				return ir.Block{
					// Write to 8-byte aligned offsets
					ir.Assign(slot.At(ir.Int64(0)), ir.Int64(1000)),
					ir.Assign(slot.At(ir.Int64(8)), ir.Int64(2000)),
					ir.Assign(slot.At(ir.Int64(16)), ir.Int64(3000)),
					ir.Assign(slot.At(ir.Int64(24)), ir.Int64(4000)),
					// Read back and sum
					ir.Assign(sum, slot.At(ir.Int64(0))),
					ir.Assign(sum, ir.Op(ir.OpAdd, sum, slot.At(ir.Int64(8)))),
					ir.Assign(sum, ir.Op(ir.OpAdd, sum, slot.At(ir.Int64(16)))),
					ir.Assign(sum, ir.Op(ir.OpAdd, sum, slot.At(ir.Int64(24)))),
				}
			},
		}),
		ir.Return(sum),
	})
	// 1000 + 2000 + 3000 + 4000 = 10000
	if result != 10000 {
		t.Errorf("Expected 10000, got %d", result)
	}
}

// TestNative_Memory_SlotInLoop tests stack slot used in loop to accumulate values.
func TestNative_Memory_SlotInLoop(t *testing.T) {
	i := ir.Var("i")
	accumulated := ir.Var("accumulated")
	result := compileAndRun(t, ir.Method{
		ir.WithStackSlot(ir.StackSlotConfig{
			Size: 8,
			Body: func(slot ir.StackSlot) ir.Fragment {
				return ir.Block{
					// Initialize slot to 0
					ir.Assign(slot.Base(), ir.Int64(0)),
					ir.Assign(i, ir.Int64(1)),
					ir.DeclareLabel(ir.Label("loop"), ir.Block{
						ir.If(ir.IsGreaterThan(i, ir.Int64(10)), ir.Goto(ir.Label("done"))),
						// Add i to accumulated value in slot
						ir.Assign(accumulated, slot.Base()),
						ir.Assign(accumulated, ir.Op(ir.OpAdd, accumulated, i)),
						ir.Assign(slot.Base(), accumulated),
						ir.Assign(i, ir.Op(ir.OpAdd, i, ir.Int64(1))),
						ir.Goto(ir.Label("loop")),
					}),
					ir.DeclareLabel(ir.Label("done"), ir.Block{
						ir.Assign(accumulated, slot.Base()),
					}),
				}
			},
		}),
		ir.Return(accumulated),
	})
	// Sum of 1..10 = 55
	if result != 55 {
		t.Errorf("Expected 55, got %d", result)
	}
}

// TestNative_Memory_SlotInConditional tests stack slot used in conditional branches.
func TestNative_Memory_SlotInConditional(t *testing.T) {
	x := ir.Var("x")
	flag := ir.Var("flag")
	slotVal := ir.Var("slotVal")
	result := compileAndRun(t, ir.Method{
		ir.Assign(flag, ir.Int64(1)), // flag is true
		ir.WithStackSlot(ir.StackSlotConfig{
			Size: 8,
			Body: func(slot ir.StackSlot) ir.Fragment {
				return ir.Block{
					ir.Assign(slot.Base(), ir.Int64(0)),
					// If flag is non-zero, write 100 to slot
					ir.If(ir.IsNotEqual(flag, ir.Int64(0)), ir.Block{
						ir.Assign(slot.Base(), ir.Int64(100)),
					}),
					// Read slot value
					ir.Assign(slotVal, slot.Base()),
					// If slotVal >= 50, add 50 more
					ir.If(ir.IsGreaterOrEqual(slotVal, ir.Int64(50)), ir.Block{
						ir.Assign(slotVal, ir.Op(ir.OpAdd, slotVal, ir.Int64(50))),
						ir.Assign(slot.Base(), slotVal),
					}),
					ir.Assign(x, slot.Base()),
				}
			},
		}),
		ir.Return(x),
	})
	// flag=1, so slot=100, then 100>=50, so slot=150
	if result != 150 {
		t.Errorf("Expected 150, got %d", result)
	}
}
