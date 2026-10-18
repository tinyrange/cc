package hypervisor

import "j5.nz/cc/hypervisor/x86state"

// X86HyperV optionally reports accelerator-backed Hyper-V CPUID features.
// This is discovery, not permission to advertise every returned feature:
// machine models must configure prerequisites and filter unhandled interfaces.
type X86HyperV interface {
	SupportedHyperVCPUID() ([]x86state.CPUIDEntry, error)
}

// X86HyperVSynIC activates the kernel synthetic interrupt controller before
// this CPU runs. False means unsupported; failures must not be advertised.
type X86HyperVSynIC interface {
	EnableHyperVSynIC() (bool, error)
}
