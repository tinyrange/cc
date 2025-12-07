//go:build windows && amd64

package whp

import "testing"

// Minimal fake IOAPIC to observe calls without WHP bindings.
type fakeIOAPIC struct {
	calls []struct {
		line  uint32
		level bool
	}
}

func (f *fakeIOAPIC) SetIRQ(line uint32, level bool) {
	f.calls = append(f.calls, struct {
		line  uint32
		level bool
	}{line, level})
}

func TestAmd64SetIRQForwardsToIOAPIC(t *testing.T) {
	f := &fakeIOAPIC{}
	vm := &virtualMachine{ioapic: f}

	if err := vm.SetIRQ(9, true); err != nil {
		t.Fatalf("SetIRQ assert: %v", err)
	}
	if err := vm.SetIRQ(9, false); err != nil {
		t.Fatalf("SetIRQ deassert: %v", err)
	}

	if len(f.calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(f.calls))
	}
	if f.calls[0].line != 9 || !f.calls[0].level {
		t.Fatalf("first call mismatch: %+v", f.calls[0])
	}
	if f.calls[1].line != 9 || f.calls[1].level {
		t.Fatalf("second call mismatch: %+v", f.calls[1])
	}
}
