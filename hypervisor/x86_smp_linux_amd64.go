//go:build linux && amd64

package hypervisor

import (
	"context"
	"fmt"
	"j5.nz/cc/hypervisor/x86state"
)

// The owner supplies VM-wide IRQ delivery and memory-address policy.
// CPU-local architectural operations must never fall through to its BSP.
type nativeX86CPU struct {
	owner *nativeX86
	index int
}

func (c *nativeX86CPU) SetCPUID(entries []x86state.CPUIDEntry) error {
	return c.owner.setCPUID(c.index, entries)
}
func (c *nativeX86CPU) Registers() (x86state.Registers, error) {
	return c.owner.registers(c.index)
}
func (c *nativeX86CPU) SystemRegisters() (x86state.SystemRegisters, error) {
	return c.owner.systemRegisters(c.index)
}
func (c *nativeX86CPU) SetRegisters(r x86state.Registers) error {
	return c.owner.setRegisters(c.index, r)
}
func (c *nativeX86CPU) SetSystemRegisters(r x86state.SystemRegisters) error {
	return c.owner.setSystemRegisters(c.index, r)
}
func (c *nativeX86CPU) CompleteIO() error { return c.owner.completeIO(c.index) }
func (c *nativeX86CPU) CompleteMMIORead(value uint64, size uint32) {
	c.owner.completeMMIORead(c.index, value, size)
}
func (c *nativeX86CPU) Cancel() error                            { c.owner.vm.RequestVCPUExit(c.index); return nil }
func (c *nativeX86CPU) Run(ctx context.Context) (X86Exit, error) { return c.RunUntil(ctx, nil) }
func (c *nativeX86CPU) RunUntil(ctx context.Context, handle func(X86Exit) (bool, error)) (X86Exit, error) {
	return c.owner.runCPU(ctx, c.index, handle)
}
func (c *nativeX86CPU) MapRAM(uint64, uint64) ([]byte, error) {
	return nil, fmt.Errorf("RAM is owned by CPU0")
}
func (c *nativeX86CPU) MapRAMRegions(uint64, []RAMRegion) ([]byte, error) {
	return nil, fmt.Errorf("RAM is owned by CPU0")
}
func (c *nativeX86CPU) Close() error { return fmt.Errorf("close CPU0 after joining all CPUs") }

func (c *nativeX86CPU) SupportedCPUID() ([]x86state.CPUIDEntry, error) {
	return c.owner.SupportedCPUID()
}
func (c *nativeX86CPU) SetIRQ(line uint32, level bool) error       { return c.owner.SetIRQ(line, level) }
func (c *nativeX86CPU) InterruptState() (map[string]uint64, error) { return c.owner.InterruptState() }

// SetA20 is a machine-wide operation: all CPU execution must be joined.
func (c *nativeX86CPU) SetA20(enabled bool) error { return c.owner.SetA20(enabled) }
