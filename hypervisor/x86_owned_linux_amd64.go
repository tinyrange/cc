//go:build linux && amd64

package hypervisor

import (
	"context"
	"j5.nz/cc/hypervisor/x86state"
)

// Explicit forwarding (rather than embedding *kvm.VM) prevents accidental
// promotion of a CPU ioctl that bypasses its lifetime owner.
func (n *nativeX86) runCPU(ctx context.Context, index int, handle func(X86Exit) (bool, error)) (out X86Exit, err error) {
	err = n.threads[index].call(func() error { var e error; out, e = runX86CPU(ctx, n.vm, index, handle); return e })
	return
}
func (n *nativeX86) SetCPUID(entries []x86state.CPUIDEntry) error { return n.setCPUID(0, entries) }
func (n *nativeX86) setCPUID(index int, entries []x86state.CPUIDEntry) error {
	return n.threads[index].call(func() error { return n.vm.SetVCPUCPUID(index, entries) })
}
func (n *nativeX86) SupportedCPUID() ([]x86state.CPUIDEntry, error) { return n.vm.SupportedCPUID() }
func (n *nativeX86) Registers() (x86state.Registers, error)         { return n.registers(0) }
func (n *nativeX86) registers(index int) (out x86state.Registers, err error) {
	err = n.threads[index].call(func() error { var e error; out, e = n.vm.VCPURegistersState(index); return e })
	return
}
func (n *nativeX86) SystemRegisters() (x86state.SystemRegisters, error) { return n.systemRegisters(0) }
func (n *nativeX86) systemRegisters(index int) (out x86state.SystemRegisters, err error) {
	err = n.threads[index].call(func() error { var e error; out, e = n.vm.VCPUSystemRegisters(index); return e })
	return
}
func (n *nativeX86) SetRegisters(r x86state.Registers) error { return n.setRegisters(0, r) }
func (n *nativeX86) setRegisters(index int, r x86state.Registers) error {
	return n.threads[index].call(func() error { return n.vm.SetVCPURegisters(index, r) })
}
func (n *nativeX86) SetSystemRegisters(s x86state.SystemRegisters) error {
	return n.setSystemRegisters(0, s)
}
func (n *nativeX86) setSystemRegisters(index int, s x86state.SystemRegisters) error {
	return n.threads[index].call(func() error { return n.vm.SetVCPUSystemRegisters(index, s) })
}
func (n *nativeX86) CompleteIO() error { return n.completeIO(0) }
func (n *nativeX86) completeIO(index int) error {
	return n.threads[index].call(func() error { return n.vm.CompleteVCPUIO(index) })
}
func (n *nativeX86) CompleteMMIORead(value uint64, size uint32) { n.completeMMIORead(0, value, size) }
func (n *nativeX86) completeMMIORead(index int, value uint64, size uint32) {
	// The public void method retains its existing contract; execution/state APIs
	// report closure errors, and owners must not use borrowed exit data after Close.
	_ = n.threads[index].call(func() error { n.vm.CompleteVCPUMMIORead(index, value, size); return nil })
}
func (n *nativeX86) SetSingleStep(enabled bool) error {
	return n.threads[0].call(func() error { return n.vm.SetSingleStep(enabled) })
}
func (n *nativeX86) SetBreakpoint(address uint64) error {
	return n.threads[0].call(func() error { return n.vm.SetBreakpoint(address) })
}
func (n *nativeX86) TSCFrequency() (out uint64, err error) {
	err = n.threads[0].call(func() error { var e error; out, e = n.vm.TSCFrequency(); return e })
	return
}
func (n *nativeX86) ReadMSR(index uint32) (out uint64, err error) {
	err = n.threads[0].call(func() error { var e error; out, e = n.vm.ReadMSR(index); return e })
	return
}

// VM-wide operations do not load vCPU state. IRQ delivery and cancellation
// must remain out of band so they can wake a CPU blocked in KVM_RUN.
func (n *nativeX86) SetIRQ(line uint32, level bool) error       { return n.vm.SetIRQ(line, level) }
func (n *nativeX86) InterruptState() (map[string]uint64, error) { return n.vm.InterruptState() }
func (n *nativeX86) SetLegacyTimerReplacement(enabled bool) error {
	return n.vm.SetLegacyTimerReplacement(enabled)
}
func (n *nativeX86) SetReadOnlyFallbackMSRs(values map[uint32]uint64) error {
	return n.vm.SetReadOnlyFallbackMSRs(values)
}
