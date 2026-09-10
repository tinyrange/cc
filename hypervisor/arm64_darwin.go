//go:build darwin && arm64

package hypervisor

import (
	"context"
	"fmt"
	"j5.nz/cc/internal/hv/hvf"
	"sort"
)

type arm64VM struct{ vm *hvf.VM }

func hostCounterFrequency() uint64

func NewARM64(ctx context.Context) (ARM64, error) {
	vm, err := hvf.NewVMWithContext(ctx)
	if err != nil {
		return nil, err
	}
	return &arm64VM{vm}, nil
}
func (v *arm64VM) MapRAM(base, size uint64) ([]byte, error) {
	if size == 0 || size > 1<<40 || base+size < base || base&0x3fff != 0 || size&0x3fff != 0 {
		return nil, fmt.Errorf("invalid RAM range")
	}
	return v.vm.MapAnonymousMemory(uintptr(size), hvf.IPA(base), 7)
}
func (v *arm64VM) Restore(s ARM64State) error {
	for i, x := range s.X {
		if err := v.vm.SetReg(hvf.Reg(i), x); err != nil {
			return err
		}
	}
	for i, q := range s.Q {
		if err := v.vm.SetVector(i, q); err != nil {
			return err
		}
	}
	keys := make([]int, 0, len(s.System))
	for k := range s.System {
		keys = append(keys, int(k))
	}
	sort.Ints(keys)
	for _, k := range keys {
		if err := v.vm.SetSysReg(hvf.SysReg(k), s.System[uint16(k)]); err != nil {
			return fmt.Errorf("restore system register %#x: %w", k, err)
		}
	}
	for _, r := range []struct {
		id    hvf.Reg
		value uint64
	}{{31, s.PC}, {34, s.PState}, {32, s.FPCR}, {33, s.FPSR}} {
		if err := v.vm.SetReg(r.id, r.value); err != nil {
			return err
		}
	}
	return nil
}
func (v *arm64VM) Run(ctx context.Context) (Exit, error) {
	if err := ctx.Err(); err != nil {
		return Exit{}, err
	}
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { _ = v.vm.CancelRun(); close(done) })
	ex, err := v.vm.Run()
	if !stop() {
		<-done
	}
	if err != nil {
		return Exit{}, err
	}
	pc, err := v.vm.GetProgramCounter()
	if err != nil {
		return Exit{}, err
	}
	if ex.Reason != 1 {
		return Exit{Reason: uint32(ex.Reason), PC: pc}, nil
	}
	return Exit{uint32(ex.Reason), pc, ex.Exception.Syndrome, ex.Exception.VirtualAddress, uint64(ex.Exception.PhysicalAddress)}, nil
}
func (v *arm64VM) Register(i uint32) (uint64, error)       { return v.vm.GetReg(hvf.Reg(i)) }
func (v *arm64VM) SetRegister(i uint32, x uint64) error    { return v.vm.SetReg(hvf.Reg(i), x) }
func (v *arm64VM) SystemRegister(i uint16) (uint64, error) { return v.vm.GetSysReg(hvf.SysReg(i)) }
func (v *arm64VM) HandleSystemInstruction(s uint64) (bool, error) {
	return v.vm.HandleSystemInstruction(s)
}
func (v *arm64VM) Advance() error            { return v.vm.AdvanceProgramCounter() }
func (v *arm64VM) HandlePSCI() (bool, error) { return v.vm.HandlePSCI() }
func (v *arm64VM) SetDebugExceptionTrapping(enabled bool) error {
	return v.vm.SetDebugExceptionTrapping(enabled)
}

// DeliverDebugException reinjects a trapped AArch64 BRK into EL1. It leaves
// general and SIMD registers intact and selects the architectural vector for
// the source exception level and stack selection.
func (v *arm64VM) DeliverDebugException(ex Exit) error {
	if ex.Reason != 1 || ex.Syndrome>>26 != 0x3c {
		return fmt.Errorf("expected AArch64 BRK exit")
	}
	state, err := v.vm.GetReg(34)
	if err != nil {
		return err
	}
	mode := state & 0x1f
	if mode != 0 && mode != 4 && mode != 5 {
		return fmt.Errorf("unsupported exception source mode %#x", mode)
	}
	vbar, err := v.vm.GetSysReg(0xc600)
	if err != nil {
		return err
	}
	sctlr, err := v.vm.GetSysReg(0xc080)
	if err != nil {
		return err
	}
	vector := vbar
	if mode == 0 {
		vector += 0x400
	} else if mode == 5 {
		vector += 0x200
	}
	for _, r := range []struct {
		reg   hvf.SysReg
		value uint64
	}{{0xc201, ex.PC}, {0xc200, state}, {0xc290, ex.Syndrome}} {
		if err := v.vm.SetSysReg(r.reg, r.value); err != nil {
			return err
		}
	}
	next := state&^uint64(0x1f|1<<20|1<<21|1<<23|3<<10) | 0x3c5
	if sctlr&(1<<23) == 0 {
		next |= 1 << 22
	}
	if err := v.vm.SetReg(34, next); err != nil {
		return err
	}
	return v.vm.SetReg(31, vector)
}
func (v *arm64VM) Cancel() error { return v.vm.CancelRun() }
func (v *arm64VM) Close() error  { return v.vm.Close() }

func (v *arm64VM) InterruptState() (map[string]uint64, error) {
	out := map[string]uint64{}
	frequency := hostCounterFrequency()
	ticks := v.vm.CounterTicks()
	offset, err := v.vm.GetVTimerOffset()
	if err != nil {
		return nil, err
	}
	out["counter"], out["frequency"], out["counter_offset"] = ticks-offset, frequency, offset
	masked, err := v.vm.GetVTimerMask()
	if err != nil {
		return nil, err
	}
	if masked {
		out["timer_masked"] = 1
	}
	for name, reg := range map[string]hvf.GICRedistributorReg{"group": 0x10080, "enabled": 0x10100, "pending": 0x10200, "active": 0x10300} {
		value, err := v.vm.GetGICRedistributorReg(reg)
		if err != nil {
			return nil, err
		}
		out[name] = value
	}
	for name, reg := range map[string]hvf.GICICCReg{"priority_mask": 0xc230, "group1_enable": 0xc667} {
		value, err := v.vm.GetGICICCReg(reg)
		if err != nil {
			return nil, err
		}
		out[name] = value
	}
	return out, nil
}

func (v *arm64VM) Counter() (uint64, uint64, error) {
	offset, err := v.vm.GetVTimerOffset()
	if err != nil {
		return 0, 0, err
	}
	return v.vm.CounterTicks() - offset, hostCounterFrequency(), nil
}

func (v *arm64VM) NewNVMePCI(c NVMePCIConfiguration, d BlockDevice) (MMIODevice, error) {
	return v.vm.NewNVMePCI(c.ConfigBase, c.ConfigSize, c.WindowBase, c.WindowSize, c.BAR, c.Device, c.Interrupt, d)
}

func (v *arm64VM) EmulateMMIO(ex Exit, d MMIODevice) (bool, error) {
	if ex.Reason != 1 || ex.Syndrome>>26 != 0x24 {
		return false, nil
	}
	info, err := hvf.DecodeDataAbort(ex.Syndrome)
	if err != nil {
		return false, err
	}
	if !d.Contains(ex.PhysicalAddress, info.SizeBytes) {
		return false, nil
	}
	if info.Write {
		var value uint64
		if uint32(info.Target) != 0xffffffff {
			value, err = v.vm.GetReg(info.Target)
			if err != nil {
				return false, err
			}
		}
		if err := d.Write(ex.PhysicalAddress, info.SizeBytes, value); err != nil {
			return false, err
		}
	} else {
		value, err := d.Read(ex.PhysicalAddress, info.SizeBytes)
		if err != nil {
			return false, err
		}
		if ex.Syndrome&(1<<21) != 0 {
			shift := 64 - info.SizeBytes*8
			value = uint64(int64(value<<shift) >> shift)
		}
		if ex.Syndrome&(1<<15) == 0 {
			value = uint64(uint32(value))
		}
		if uint32(info.Target) != 0xffffffff {
			if err := v.vm.SetReg(info.Target, value); err != nil {
				return false, err
			}
		}
	}
	return true, v.vm.AdvanceProgramCounter()
}

func (v *arm64VM) addInputPCI(bus MMIODevice, c InputPCIConfiguration) (InputDevice, error) {
	return v.vm.AddInputPCI(bus, c.Device, c.BAR, c.Interrupt, c.Pointer, c.Width, c.Height)
}
