//go:build linux && amd64

package kvm

import (
	"fmt"

	"j5.nz/cc/hypervisor/x86state"
)

// SupportedCPUID returns an owned copy of the features the accelerator can run.
func (v *VM) SupportedCPUID() ([]x86state.CPUIDEntry, error) {
	entries, err := getSupportedCPUID(v.kvm.fd)
	if err != nil {
		return nil, err
	}
	result := make([]x86state.CPUIDEntry, 0, entries.Nr)
	for _, e := range cpuidEntries(entries) {
		result = append(result, x86state.CPUIDEntry{Function: e.Function, Index: e.Index, Flags: e.Flags, Eax: e.Eax, Ebx: e.Ebx, Ecx: e.Ecx, Edx: e.Edx})
	}
	return result, nil
}

// SetCPUID configures the single CPU before its first execution. Feature and
// topology policy belongs to the machine model, not this accelerator boundary.
func (v *VM) SetCPUID(entries []x86state.CPUIDEntry) error {
	if len(v.vcpus) != 1 || len(entries) == 0 || len(entries) > cpuidMaxEntries {
		return fmt.Errorf("invalid single-CPU CPUID table")
	}
	table, err := getSupportedCPUID(v.kvm.fd)
	if err != nil {
		return err
	}
	table.Nr = uint32(len(entries))
	for i, e := range entries {
		cpuidStorage(table)[i] = kvmCPUIDEntry2{Function: e.Function, Index: e.Index, Flags: e.Flags, Eax: e.Eax, Ebx: e.Ebx, Ecx: e.Ecx, Edx: e.Edx}
	}
	return setVCPUID(v.vcpufd, table)
}
