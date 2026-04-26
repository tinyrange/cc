//go:build windows && amd64

package whp

import (
	"strings"
	"testing"
)

func TestSingleInstructionHLT(t *testing.T) {
	if err := Supports(); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "hypervisor not present") ||
			strings.Contains(strings.ToLower(err.Error()), "not supported") {
			t.Skip(err)
		}
		t.Fatalf("Supports() error = %v", err)
	}
	vm, err := NewVM(0x1000)
	if err != nil {
		t.Fatalf("NewVM() error = %v", err)
	}
	defer func() {
		if err := vm.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}()

	vm.Memory()[0] = 0xf4 // hlt
	if err := vm.SetFlatProtectedMode(0); err != nil {
		t.Fatalf("SetFlatProtectedMode() error = %v", err)
	}
	exit, err := vm.Run()
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if exit.Reason != runVPExitReasonX64Halt {
		t.Fatalf("Run() exit reason = %s, want %s (rip=%#x rflags=%#x)", exit.Reason, runVPExitReasonX64Halt, exit.RIP, exit.RFLAGS)
	}
}
