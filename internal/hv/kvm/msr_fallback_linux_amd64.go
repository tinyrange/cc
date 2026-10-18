//go:build linux && amd64

package kvm

import (
	"encoding/binary"
	"fmt"
	"unsafe"
)

// SetReadOnlyFallbackMSRs supplies values only for MSRs unknown to KVM.
// Known MSRs remain kernel-owned. All unknown writes and unlisted reads #GP.
// Configure on the owning VM before running any CPU. The policy is VM-wide,
// copied here, and is not part of architectural CPU snapshots.
func (v *VM) SetReadOnlyFallbackMSRs(values map[uint32]uint64) error {
	v.fallbackMSRMu.Lock()
	defer v.fallbackMSRMu.Unlock()
	var cap struct {
		Cap, Flags uint32
		Args       [4]uint64
		Pad        [64]byte
	}
	cap.Cap = 188   // KVM_CAP_X86_USER_SPACE_MSR
	cap.Args[0] = 2 // KVM_MSR_EXIT_REASON_UNKNOWN only
	if _, err := ioctlWithRetry(uintptr(v.vmfd), 0x4068aea3, uintptr(unsafe.Pointer(&cap))); err != nil {
		return fmt.Errorf("enable unknown-MSR fallback: %w", err)
	}
	v.fallbackMSRs = make(map[uint32]uint64, len(values))
	for index, value := range values {
		v.fallbackMSRs[index] = value
	}
	return nil
}

// CompleteFallbackMSR prepares the response; the next KVM_RUN completes the
// instruction (or injects #GP), including when execution resumes after cancel.
// The caller owns this vCPU's execution, as for CompleteIO.
func (v *VM) CompleteFallbackMSR(cpu int) error {
	if cpu < 0 || cpu >= len(v.vcpus) {
		return fmt.Errorf("invalid MSR CPU %d", cpu)
	}
	run := (*kvmRunData)(unsafe.Pointer(&v.vcpus[cpu].run[0]))
	if ExitReason(run.exitReason) != ExitRDMSR && ExitReason(run.exitReason) != ExitWRMSR {
		return fmt.Errorf("not an MSR exit")
	}
	data := run.anon0[:24]
	data[0] = 1 // default: inject #GP, never silently ignore an MSR
	v.fallbackMSRMu.RLock()
	value, ok := v.fallbackMSRs[binary.LittleEndian.Uint32(data[12:])]
	v.fallbackMSRMu.RUnlock()
	if ExitReason(run.exitReason) == ExitRDMSR && binary.LittleEndian.Uint32(data[8:]) == 2 && ok {
		binary.LittleEndian.PutUint64(data[16:], value)
		data[0] = 0
	}
	return nil
}
