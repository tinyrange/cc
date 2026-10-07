//go:build linux && amd64

package kvm

// SetLegacyTimerReplacement switches the kernel PIT output off while a caller's
// HPET owns the legacy timer routes. Call only with the owning VCPU stopped.
// Preserve the programmed PIT channels and unrelated flags for restoration.
func (v *VM) SetLegacyTimerReplacement(enabled bool) error {
	state, err := getPIT2(v.vmfd)
	if err != nil {
		return err
	}
	const hpetLegacy = 1 // KVM_PIT_FLAGS_HPET_LEGACY, KVM userspace ABI.
	if enabled {
		state.Flags |= hpetLegacy
	} else {
		state.Flags &^= hpetLegacy
	}
	return setPIT2(v.vmfd, &state)
}
