//go:build !cgo
// +build !cgo

package main

import (
	"sync"
	"testing"
)

// TestHandleTableBasic tests basic handle operations.
func TestHandleTableBasic(t *testing.T) {
	// Store a value
	value := "test value"
	h := newHandle(value)
	if h == 0 {
		t.Fatal("expected non-zero handle")
	}

	// Retrieve the value
	got := getHandle(h)
	if got != value {
		t.Fatalf("expected %v, got %v", value, got)
	}

	// Retrieve with type assertion
	typed, ok := getHandleTyped[string](h)
	if !ok {
		t.Fatal("type assertion failed")
	}
	if typed != value {
		t.Fatalf("expected %v, got %v", value, typed)
	}

	// Free the handle
	freed := freeHandle(h)
	if freed != value {
		t.Fatalf("expected freed value %v, got %v", value, freed)
	}

	// Verify handle is gone
	got = getHandle(h)
	if got != nil {
		t.Fatalf("expected nil after free, got %v", got)
	}
}

// TestHandleTableTypes tests type-specific handle operations.
func TestHandleTableTypes(t *testing.T) {
	// Store different types
	strVal := "string"
	intVal := 42
	structVal := struct{ Name string }{"test"}

	h1 := newHandle(strVal)
	h2 := newHandle(intVal)
	h3 := newHandle(structVal)

	// Verify correct type retrieval
	if v, ok := getHandleTyped[string](h1); !ok || v != strVal {
		t.Errorf("string retrieval failed: got %v, ok=%v", v, ok)
	}
	if v, ok := getHandleTyped[int](h2); !ok || v != intVal {
		t.Errorf("int retrieval failed: got %v, ok=%v", v, ok)
	}
	if v, ok := getHandleTyped[struct{ Name string }](h3); !ok || v != structVal {
		t.Errorf("struct retrieval failed: got %v, ok=%v", v, ok)
	}

	// Verify wrong type returns false
	if _, ok := getHandleTyped[int](h1); ok {
		t.Error("expected type assertion to fail for wrong type")
	}
	if _, ok := getHandleTyped[string](h2); ok {
		t.Error("expected type assertion to fail for wrong type")
	}

	// Cleanup
	freeHandle(h1)
	freeHandle(h2)
	freeHandle(h3)
}

// TestHandleTableInvalid tests behavior with invalid handles.
func TestHandleTableInvalid(t *testing.T) {
	// Handle 0 should always return nil
	if got := getHandle(0); got != nil {
		t.Errorf("expected nil for handle 0, got %v", got)
	}

	// getHandleTyped should return (zero, false) for handle 0
	if v, ok := getHandleTyped[string](0); ok || v != "" {
		t.Errorf("expected ('', false) for handle 0, got (%v, %v)", v, ok)
	}

	// freeHandle(0) should return nil
	if freed := freeHandle(0); freed != nil {
		t.Errorf("expected nil from freeHandle(0), got %v", freed)
	}

	// Non-existent handle should return nil
	if got := getHandle(999999); got != nil {
		t.Errorf("expected nil for non-existent handle, got %v", got)
	}
}

// TestHandleTableConcurrency tests thread-safety of the handle table.
func TestHandleTableConcurrency(t *testing.T) {
	const numGoroutines = 100
	const opsPerGoroutine = 1000

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()

			for j := 0; j < opsPerGoroutine; j++ {
				// Create, read, and free a handle
				value := id*opsPerGoroutine + j
				h := newHandle(value)

				// Read it back
				got, ok := getHandleTyped[int](h)
				if !ok || got != value {
					t.Errorf("goroutine %d: expected %v, got %v (ok=%v)", id, value, got, ok)
				}

				// Free it
				freed, ok := freeHandleTyped[int](h)
				if !ok || freed != value {
					t.Errorf("goroutine %d: expected freed %v, got %v (ok=%v)", id, value, freed, ok)
				}
			}
		}(i)
	}

	wg.Wait()
}

// TestHandleTableUniqueHandles verifies handles are unique.
func TestHandleTableUniqueHandles(t *testing.T) {
	const numHandles = 1000
	handles := make(map[uint64]bool, numHandles)

	for i := 0; i < numHandles; i++ {
		h := newHandle(i)
		if handles[h] {
			t.Fatalf("duplicate handle: %d", h)
		}
		handles[h] = true
	}

	// Cleanup
	for h := range handles {
		freeHandle(h)
	}
}

// TestFreeHandleTyped tests typed free operations.
func TestFreeHandleTyped(t *testing.T) {
	value := "test"
	h := newHandle(value)

	// Try to free with wrong type
	if v, ok := freeHandleTyped[int](h); ok {
		t.Errorf("expected freeHandleTyped[int] to fail, got %v", v)
	}

	// Handle should still exist
	if got := getHandle(h); got == nil {
		t.Error("handle was incorrectly freed")
	}

	// Free with correct type
	if v, ok := freeHandleTyped[string](h); !ok || v != value {
		t.Errorf("expected (%v, true), got (%v, %v)", value, v, ok)
	}

	// Handle should be gone
	if got := getHandle(h); got != nil {
		t.Errorf("handle should be freed, got %v", got)
	}
}

// TestHandleTableWithPointers tests storing pointers.
func TestHandleTableWithPointers(t *testing.T) {
	type MyStruct struct {
		Value int
	}

	original := &MyStruct{Value: 42}
	h := newHandle(original)

	// Retrieve as pointer
	got, ok := getHandleTyped[*MyStruct](h)
	if !ok {
		t.Fatal("type assertion failed")
	}
	if got != original {
		t.Fatal("pointer address changed")
	}
	if got.Value != 42 {
		t.Fatalf("expected Value=42, got %d", got.Value)
	}

	// Modify through retrieved pointer
	got.Value = 100

	// Verify original was modified
	if original.Value != 100 {
		t.Fatalf("modification didn't affect original")
	}

	freeHandle(h)
}
