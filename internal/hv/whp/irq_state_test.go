//go:build windows && (amd64 || arm64)

package whp

import "testing"

// Pure logic test; no WHP bindings required.
func TestArm64ShouldFireRisingEdgeOnly(t *testing.T) {
	vm := &virtualMachine{}
	const intid = 42

	if !vm.arm64ShouldFire(intid, true) {
		t.Fatalf("first assert should fire")
	}
	if vm.arm64ShouldFire(intid, true) {
		t.Fatalf("second assert should not fire")
	}
	if vm.arm64ShouldFire(intid, false) {
		t.Fatalf("deassert should not fire")
	}
	if !vm.arm64ShouldFire(intid, true) {
		t.Fatalf("assert after deassert should fire again")
	}
}

func TestArm64ShouldFireSeparateIntids(t *testing.T) {
	vm := &virtualMachine{}
	if !vm.arm64ShouldFire(1, true) {
		t.Fatalf("intid 1 first assert should fire")
	}
	if !vm.arm64ShouldFire(2, true) {
		t.Fatalf("intid 2 first assert should fire")
	}
	if vm.arm64ShouldFire(1, true) {
		t.Fatalf("intid 1 second assert should not fire")
	}
	if vm.arm64ShouldFire(2, true) {
		t.Fatalf("intid 2 second assert should not fire")
	}
}
