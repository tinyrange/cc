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
