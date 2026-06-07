//go:build windows && amd64

package whp

import (
	"testing"
	"unsafe"
)

func TestBindingLayouts(t *testing.T) {
	tests := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"registerValue", unsafe.Sizeof(registerValue{}), 16},
		{"x64SegmentRegister", unsafe.Sizeof(x64SegmentRegister{}), 16},
		{"vpExitContext", unsafe.Sizeof(vpExitContext{}), 40},
		{"runVPExitContext", unsafe.Sizeof(runVPExitContext{}), 224},
		{"memoryAccessContext", unsafe.Sizeof(memoryAccessContext{}), 40},
		{"x64IOPortAccessContext", unsafe.Sizeof(x64IOPortAccessContext{}), 96},
		{"interruptControl", unsafe.Sizeof(interruptControl{}), 16},
		{"emulatorCallbacks", unsafe.Sizeof(emulatorCallbacks{}), 48},
		{"emulatorMemoryAccessInfo", unsafe.Sizeof(emulatorMemoryAccessInfo{}), 24},
		{"emulatorIOAccessInfo", unsafe.Sizeof(emulatorIOAccessInfo{}), 12},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Fatalf("%s size = %d, want %d", tt.name, tt.got, tt.want)
		}
	}
}

func TestAlignedRegisterValues(t *testing.T) {
	values, backing := alignedRegisterValues(3)
	if len(values) != 3 {
		t.Fatalf("len(values) = %d, want 3", len(values))
	}
	if len(backing) == 0 {
		t.Fatal("backing buffer is empty")
	}
	if addr := uintptr(unsafe.Pointer(&values[0])); addr%16 != 0 {
		t.Fatalf("values address %#x is not 16-byte aligned", addr)
	}
	for i := 1; i < len(values); i++ {
		prev := uintptr(unsafe.Pointer(&values[i-1]))
		next := uintptr(unsafe.Pointer(&values[i]))
		if next-prev != unsafe.Sizeof(registerValue{}) {
			t.Fatalf("values[%d] stride = %d, want %d", i, next-prev, unsafe.Sizeof(registerValue{}))
		}
	}
}
