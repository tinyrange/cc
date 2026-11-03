//go:build windows && amd64

package whp

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"unsafe"

	"j5.nz/cc/hypervisor/x86state"
)

// NativeCPU is a caller-constructed PC CPU. CPU0 owns the partition and RAM;
// secondary CPUs borrow them. Join execution and IRQ delivery before Close.
// The caller owns firmware and devices; only interrupt controllers and PIT are
// supplied here, matching the native KVM machine contract.
type NativeCPU struct {
	machine    *nativeMachine
	index      uint32
	cpuid      []x86state.CPUIDEntry
	raw        runVPExitContext
	pending    bool
	io         []byte
	emulating  bool
	requests   chan nativeRequest
	replies    chan error
	jobs       chan bool
	results    chan error
	workerDone chan struct{}
	queued     *x86state.Exit
	miscEnable uint64
}
type nativeRequest struct{ exit x86state.Exit }
type nativeMachine struct {
	vm                *VM
	cpus              []*NativeCPU
	pic               bootPIC
	pit               *bootPIT
	ioapic            bootIOAPIC
	irqMu             sync.Mutex
	irqErr            error
	legacyReplacement atomic.Bool
	fallback          map[uint32]uint64
	regions           []x86state.RAMRegion
	a20Disabled       bool
}

func NewNativeCPUs(ctx context.Context, count int) ([]*NativeCPU, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if count < 1 || count > 64 {
		return nil, fmt.Errorf("CPU count must be 1..64")
	}
	if err := Supports(); err != nil {
		return nil, err
	}
	// Processor policy must be installed before WHvSetupPartition. Allocate
	// caller-owned RAM later, using the caller's physical layout.
	vm, err := newVMConfigured(0, true, false, count, nil, func(part partitionHandle) error {
		if err := setPartitionProperty(part, partitionPropertyCodeExtendedVMExits, uint64(7)); err != nil {
			return err
		}
		if err := setPartitionProperty(part, partitionPropertyCode(5), uint64(1|1<<4)); err != nil {
			return err
		}
		// AMD WHP faults Intel-only MSRs before its MSR-exit path. Intercept
		// #GP to supply only explicitly supported reads, reinjecting all others.
		return setPartitionProperty(part, partitionPropertyCode(2), uint64(1<<13))
	})
	if err != nil {
		return nil, err
	}
	m := &nativeMachine{vm: vm}
	m.ioapic.init()
	m.pic.master.vectorBase, m.pic.slave.vectorBase = 8, 0x70
	m.pic.master.mask, m.pic.slave.mask = 0xff, 0xff
	for i := 0; i < count; i++ {
		c := &NativeCPU{machine: m, index: uint32(i), requests: make(chan nativeRequest), replies: make(chan error), jobs: make(chan bool), results: make(chan error), workerDone: make(chan struct{})}
		c.miscEnable = 1 // Fast strings enabled; CPUID maximum unrestricted.
		m.cpus = append(m.cpus, c)
	}
	if err = vm.EnableEmulation(m.cpus[0]); err != nil {
		vm.Close()
		return nil, err
	}
	for i, c := range m.cpus {
		vm.emuContexts[i].platform = c
		go c.emulationWorker()
	}
	m.pit = newBootPIT(func() {
		if !m.legacyReplacement.Load() {
			if err := m.cpus[0].SetIRQ(0, true); err != nil {
				m.irqMu.Lock()
				m.irqErr = err
				m.irqMu.Unlock()
			}
			_ = m.cpus[0].SetIRQ(0, false)
		}
	})
	return m.cpus, nil
}

func (c *NativeCPU) MapRAM(base, size uint64) ([]byte, error) {
	return c.MapRAMRegions(size, []x86state.RAMRegion{{Address: base, Size: size}})
}
func (c *NativeCPU) MapRAMRegions(size uint64, regions []x86state.RAMRegion) ([]byte, error) {
	m := c.machine
	if c.index != 0 || m.vm.mem != nil || size == 0 || size > uint64(^uint(0)>>1) || size%4096 != 0 || len(regions) == 0 {
		return nil, fmt.Errorf("invalid RAM layout or RAM already mapped")
	}
	for i, r := range regions {
		if r.Size == 0 || r.Offset > size || r.Size > size-r.Offset || r.Address > ^uint64(0)-r.Size || (r.Address|r.Offset|r.Size)%4096 != 0 {
			return nil, fmt.Errorf("invalid RAM region %d", i)
		}
		for _, p := range regions[:i] {
			if r.Address < p.Address+p.Size && p.Address < r.Address+r.Size {
				return nil, fmt.Errorf("overlapping RAM regions")
			}
		}
	}
	mem, err := virtualAlloc(uintptr(size))
	if err != nil {
		return nil, err
	}
	m.vm.mem, m.vm.memSize = mem, size
	m.regions = append([]x86state.RAMRegion(nil), regions...)
	if err = m.replaceRAM(regions); err != nil {
		return nil, err
	}
	return mem.bytes(), nil
}
func (m *nativeMachine) replaceRAM(regions []x86state.RAMRegion) error {
	for _, r := range m.vm.memRegions {
		if err := unmapGPARange(m.vm.part, r.guestPhysAddr, r.size); err != nil {
			return err
		}
	}
	m.vm.memRegions = nil
	for _, r := range regions {
		if err := mapGPARange(m.vm.part, unsafe.Pointer(m.vm.mem.addr+uintptr(r.Offset)), r.Address, r.Size, mapGPARangeFlagRead|mapGPARangeFlagWrite|mapGPARangeFlagExecute); err != nil {
			return err
		}
		m.vm.memRegions = append(m.vm.memRegions, guestMemoryRegion{guestPhysAddr: r.Address, memoryOffset: r.Offset, size: r.Size})
	}
	return nil
}
func (c *NativeCPU) SetA20(enabled bool) error {
	m := c.machine
	if m.a20Disabled == !enabled {
		return nil
	}
	regions := m.regions
	if !enabled {
		var err error
		regions, err = x86state.A20Layout(regions)
		if err != nil {
			return err
		}
	}
	if err := m.replaceRAM(regions); err != nil {
		return err
	}
	m.a20Disabled = !enabled
	return nil
}
func (c *NativeCPU) Close() error {
	if c.index != 0 {
		return fmt.Errorf("close CPU0 after joining all CPUs")
	}
	m := c.machine
	if m.vm.part == 0 {
		return nil
	}
	m.pit.Close()
	for _, cpu := range m.cpus {
		if cpu.emulating {
			if cpu.pending {
				cpu.replies <- errors.New("CPU closed")
				cpu.pending = false
			}
			// Drain any additional callback without allowing more device mutations.
			for cpu.emulating {
				select {
				case <-cpu.requests:
					cpu.replies <- errors.New("CPU closed")
				case <-cpu.results:
					cpu.emulating = false
				}
			}
		}
		close(cpu.jobs)
		<-cpu.workerDone
	}
	return m.vm.Close()
}

var nativeRegisterNames = []registerName{registerRax, registerRbx, registerRcx, registerRdx, registerRsi, registerRdi, registerRsp, registerRbp, registerR8, registerR9, registerR10, registerR11, registerR12, registerR13, registerR14, registerR15, registerRip, registerRflags}

func (c *NativeCPU) Registers() (out x86state.Registers, err error) {
	values := make([]registerValue, len(nativeRegisterNames))
	err = getVirtualProcessorRegisters(c.machine.vm.part, c.index, nativeRegisterNames, values)
	fields := []*uint64{&out.Rax, &out.Rbx, &out.Rcx, &out.Rdx, &out.Rsi, &out.Rdi, &out.Rsp, &out.Rbp, &out.R8, &out.R9, &out.R10, &out.R11, &out.R12, &out.R13, &out.R14, &out.R15, &out.Rip, &out.Rflags}
	for i, p := range fields {
		*p = values[i].uint64()
	}
	return
}
func (c *NativeCPU) SetRegisters(r x86state.Registers) error {
	fields := []uint64{r.Rax, r.Rbx, r.Rcx, r.Rdx, r.Rsi, r.Rdi, r.Rsp, r.Rbp, r.R8, r.R9, r.R10, r.R11, r.R12, r.R13, r.R14, r.R15, r.Rip, r.Rflags}
	values := make([]registerValue, len(fields))
	for i, v := range fields {
		values[i] = uint64RegisterValue(v)
	}
	return setVirtualProcessorRegisters(c.machine.vm.part, c.index, nativeRegisterNames, values)
}

var nativeSystemNames = []registerName{registerCs, registerDs, registerEs, registerFs, registerGs, registerSs, registerTr, registerLdtr, registerGdtr, registerIdtr, registerCr0, registerCr2, registerCr3, registerCr4, registerCr8, registerEfer, registerApicBase}

func (c *NativeCPU) SystemRegisters() (out x86state.SystemRegisters, err error) {
	values := make([]registerValue, len(nativeSystemNames))
	err = getVirtualProcessorRegisters(c.machine.vm.part, c.index, nativeSystemNames, values)
	if err != nil {
		return
	}
	segments := []*x86state.Segment{&out.Cs, &out.Ds, &out.Es, &out.Fs, &out.Gs, &out.Ss, &out.Tr, &out.Ldt}
	for i, s := range segments {
		v := *(*x64SegmentRegister)(unsafe.Pointer(&values[i]))
		a := v.Attributes
		*s = x86state.Segment{Base: v.Base, Limit: v.Limit, Selector: v.Selector, Type: uint8(a & 15), S: uint8(a >> 4 & 1), Dpl: uint8(a >> 5 & 3), Present: uint8(a >> 7 & 1), Avl: uint8(a >> 12 & 1), L: uint8(a >> 13 & 1), Db: uint8(a >> 14 & 1), G: uint8(a >> 15 & 1)}
		if s.Present == 0 {
			s.Unusable = 1
		}
	}
	tables := []*x86state.DescriptorTable{&out.Gdt, &out.Idt}
	for i, t := range tables {
		v := values[8+i].raw
		t.Limit = uint16(v.Low64 >> 48)
		t.Base = v.High64
	}
	fields := []*uint64{&out.Cr0, &out.Cr2, &out.Cr3, &out.Cr4, &out.Cr8, &out.Efer, &out.ApicBase}
	for i, p := range fields {
		*p = values[10+i].uint64()
	}
	return
}
func (c *NativeCPU) SetSystemRegisters(s x86state.SystemRegisters) error {
	segments := []x86state.Segment{s.Cs, s.Ds, s.Es, s.Fs, s.Gs, s.Ss, s.Tr, s.Ldt}
	values := make([]registerValue, len(nativeSystemNames))
	for i, s := range segments {
		a := segmentAttributes(uint16(s.Type), uint16(s.S), uint16(s.Dpl), uint16(s.Present), uint16(s.Avl), uint16(s.L), uint16(s.Db), uint16(s.G))
		values[i] = segmentRegisterValue(x64SegmentRegister{Base: s.Base, Limit: s.Limit, Selector: s.Selector, Attributes: a})
	}
	for i, t := range []x86state.DescriptorTable{s.Gdt, s.Idt} {
		values[8+i] = registerValue{raw: uint128{Low64: uint64(t.Limit) << 48, High64: t.Base}}
	}
	for i, v := range []uint64{s.Cr0, s.Cr2, s.Cr3, s.Cr4, s.Cr8, s.Efer, s.ApicBase} {
		values[10+i] = uint64RegisterValue(v)
	}
	return setVirtualProcessorRegisters(c.machine.vm.part, c.index, nativeSystemNames, values)
}

var procNativeCPUID = winhvplatform.NewProc("WHvGetVirtualProcessorCpuidOutput")

func (c *NativeCPU) SupportedCPUID() ([]x86state.CPUIDEntry, error) {
	var out []x86state.CPUIDEntry
	for _, span := range [][2]uint32{{0, 0x20}, {0x80000000, 0x80000008}} {
		maximum := span[1]
		for leaf := span[0]; leaf <= maximum; leaf++ {
			for index := uint32(0); index < 64; index++ {
				var regs [4]uint32
				if err := callHRESULT(procNativeCPUID, uintptr(c.machine.vm.part), uintptr(c.index), uintptr(leaf), uintptr(index), uintptr(unsafe.Pointer(&regs[0]))); err != nil {
					return nil, fmt.Errorf("query CPUID: %w", err)
				}
				out = append(out, x86state.CPUIDEntry{Function: leaf, Index: index, Eax: regs[0], Ebx: regs[1], Ecx: regs[2], Edx: regs[3]})
				if leaf == 4 || leaf == 7 || leaf == 0xb || leaf == 0xd || leaf == 0x1f {
					out[len(out)-1].Flags = 1
				}
				if leaf == span[0] {
					maximum = min(maximum, regs[0])
				}
				if leaf != 4 && leaf != 0xb && leaf != 0x1f {
					break
				}
				if leaf == 4 && regs[0]&31 == 0 || leaf != 4 && regs[1] == 0 {
					break
				}
			}
		}
	}
	return out, nil
}
func (c *NativeCPU) SetCPUID(entries []x86state.CPUIDEntry) error {
	c.cpuid = append([]x86state.CPUIDEntry(nil), entries...)
	return nil
}
func (c *NativeCPU) SetReadOnlyFallbackMSRs(values map[uint32]uint64) error {
	c.machine.fallback = make(map[uint32]uint64, len(values))
	for k, v := range values {
		c.machine.fallback[k] = v
	}
	return nil
}
func (c *NativeCPU) SetLegacyTimerReplacement(enabled bool) error {
	c.machine.legacyReplacement.Store(enabled)
	return nil
}
func (c *NativeCPU) SetMSI(addr uint64, data uint32) error {
	if addr>>20 != 0xfee {
		return fmt.Errorf("invalid MSI address %#x", addr)
	}
	trigger := interruptTriggerEdge
	if data&(1<<15) != 0 {
		trigger = interruptTriggerLevel
	}
	mode := interruptDestinationPhysical
	if addr&4 != 0 {
		mode = interruptDestinationLogical
	}
	return c.machine.vm.RequestInterruptRoute(data&255, trigger, interruptType(data>>8&7), mode, uint32(addr>>12&255))
}
func (m *nativeMachine) inject(route bootIOAPICRoute) error {
	trigger := interruptTriggerEdge
	if route.level {
		trigger = interruptTriggerLevel
	}
	return m.vm.RequestInterruptRoute(uint32(route.vector), trigger, route.interruptType, route.destinationMode, route.destination)
}
func (c *NativeCPU) SetIRQ(line uint32, high bool) error {
	m := c.machine
	if line >= ioapicRedirEntries {
		return fmt.Errorf("IRQ out of range")
	}
	m.irqMu.Lock()
	defer m.irqMu.Unlock()
	pin := uint8(line)
	if line == 0 {
		pin = 2
	}
	if route, ready := m.ioapic.assert(pin, high); ready {
		if err := m.inject(route); err != nil {
			m.ioapic.cancel(route)
			return err
		}
	}
	if line < 16 {
		m.pic.SetIRQ(uint8(line), high)
	}
	// The legacy PIC is injected on CPU0's execution thread. Wake it to inspect
	// IF/shadow without racing register access from a timer callback.
	m.vm.kickVCPUIfRunning(0)
	return nil
}
func (c *NativeCPU) InterruptState() (map[string]uint64, error) {
	m := c.machine
	out := map[string]uint64{}
	pic := m.pic.SnapshotState()
	out["pic.master.irr"] = uint64(pic.Master.IRR)
	out["pic.master.isr"] = uint64(pic.Master.ISR)
	out["pic.master.imr"] = uint64(pic.Master.Mask)
	out["pic.slave.irr"] = uint64(pic.Slave.IRR)
	for i, cpu := range m.cpus {
		r, e := cpu.Registers()
		if e != nil {
			return nil, e
		}
		out[fmt.Sprintf("cpu.%d.rip", i)] = r.Rip
	}
	return out, nil
}
func (c *NativeCPU) Cancel() error                                  { return cancelRunVirtualProcessor(c.machine.vm.part, c.index) }
func (c *NativeCPU) Run(ctx context.Context) (x86state.Exit, error) { return c.RunUntil(ctx, nil) }

// Instruction emulation uses a persistent worker so a device exit can be
// returned before the instruction commits. CompleteIO resumes the callback and
// waits for completion or the instruction's next device access.
func (c *NativeCPU) emulationWorker() {
	defer close(c.workerDone)
	for mmio := range c.jobs {
		var err error
		if mmio {
			err = c.machine.vm.emulateVCPUMMIO(c.index, &c.raw)
			if err != nil {
				mem := c.raw.memoryAccess()
				err = fmt.Errorf("MMIO rip=%#x gpa=%#x gva=%#x instruction=%x: %w", c.raw.VpContext.Rip, mem.GPA, mem.GVA, mem.InstructionBytes[:min(mem.InstructionByteCount, 16)], err)
			}
		} else {
			err = c.machine.vm.emulateVCPUIO(c.index, &c.raw)
		}
		c.results <- err
	}
}
func (c *NativeCPU) request(ex x86state.Exit) error {
	c.requests <- nativeRequest{exit: ex}
	return <-c.replies
}
func (c *NativeCPU) ReadIO(port uint16, data []byte) error  { return c.deviceIO(port, data, false) }
func (c *NativeCPU) WriteIO(port uint16, data []byte) error { return c.deviceIO(port, data, true) }
func (c *NativeCPU) deviceIO(port uint16, data []byte, write bool) error {
	m := c.machine
	if write {
		if m.pic.Write(port, data) {
			m.pic.Resample()
			return nil
		}
		if port >= 0x40 && port <= 0x43 && m.pit.Write(port, data) {
			return nil
		}
	} else {
		if m.pic.Read(port, data) {
			return nil
		}
		if port >= 0x40 && port <= 0x43 && m.pit.Read(port, data) {
			return nil
		}
	}
	return c.request(x86state.Exit{Reason: 2, Port: port, Size: uint8(len(data)), Count: 1, Write: write, Data: data})
}
func (c *NativeCPU) ReadMMIO(addr uint64, data []byte) error {
	if c.machine.ioapic.Read(addr, data) {
		return nil
	}
	return c.request(x86state.Exit{Reason: 6, Address: addr, Size: uint8(len(data)), Count: 1, Data: data})
}
func (c *NativeCPU) WriteMMIO(addr uint64, data []byte) error {
	if handled, route, ready := c.machine.ioapic.Write(addr, data); handled {
		if ready {
			return c.machine.inject(route)
		}
		return nil
	}
	return c.request(x86state.Exit{Reason: 6, Address: addr, Size: uint8(len(data)), Count: 1, Write: true, Data: data})
}
func (c *NativeCPU) CompleteMMIORead(value uint64, size uint32) {
	if c.pending && len(c.io) >= int(size) && size <= 8 {
		var b [8]byte
		binary.LittleEndian.PutUint64(b[:], value)
		copy(c.io, b[:size])
	}
}
func (c *NativeCPU) CompleteIO() error {
	if !c.pending {
		return nil
	}
	c.pending = false
	c.replies <- nil
	select {
	case err := <-c.results:
		c.emulating = false
		return err
	case req := <-c.requests:
		c.io = req.exit.Data
		c.pending = true
		c.queued = &req.exit
		return nil
	}
}
func (c *NativeCPU) RunUntil(ctx context.Context, handle func(x86state.Exit) (bool, error)) (x86state.Exit, error) {
	if err := ctx.Err(); err != nil {
		return x86state.Exit{}, err
	}
	joined := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { _ = c.Cancel(); close(joined) })
	defer func() {
		if !stop() {
			<-joined
		}
	}()
	for {
		if c.queued != nil {
			ex := *c.queued
			c.queued = nil
			if handle == nil {
				return ex, nil
			}
			handled, err := handle(ex)
			if err != nil || !handled {
				return ex, err
			}
		}
		if c.pending {
			if err := c.CompleteIO(); err != nil {
				return x86state.Exit{}, err
			}
			if c.queued != nil {
				continue
			}
		}
		if err := ctx.Err(); err != nil {
			return x86state.Exit{}, err
		}
		m := c.machine
		m.irqMu.Lock()
		irqErr := m.irqErr
		m.irqMu.Unlock()
		if irqErr != nil {
			return x86state.Exit{}, irqErr
		}
		// PIC vectors belong to the BSP. APIC-routed lines must not also arrive
		// through the legacy controller after the guest unmasks its IOAPIC.
		if c.index == 0 {
			vals := make([]registerValue, 3)
			if err := getVirtualProcessorRegisters(m.vm.part, 0, []registerName{registerRflags, registerPendingInterruption, registerName(0x80000001)}, vals); err != nil {
				return x86state.Exit{}, err
			}
			if vals[0].uint64()&512 != 0 && vals[1].uint64() == 0 && vals[2].uint64()&1 == 0 {
				if vector, line, ok := m.pic.AcknowledgePending(); ok {
					pin := line
					if pin == 0 {
						pin = 2
					}
					if !m.ioapic.enabled(pin) {
						if err := m.vm.SetPendingInterruption(vector); err != nil {
							return x86state.Exit{}, err
						}
						_ = m.vm.kickOutOfHLT()
					}
				}
			}
		}
		_, err := m.vm.runVCPUWithContext(c.index, &c.raw)
		if err != nil {
			return x86state.Exit{}, err
		}
		switch c.raw.ExitReason {
		case runVPExitReasonCanceled:
			return x86state.Exit{}, ctx.Err()
		case runVPExitReasonX64Halt, runVPExitReasonX64InterruptWindow:
			// WHP retains HLT activity; interrupt requests and cancellation wake it.
			continue
		case runVPExitReasonX64ApicEoi:
			if route, ready := m.ioapic.handleEOI(c.raw.apicEoi().InterruptVector); ready {
				if err := m.inject(route); err != nil {
					return x86state.Exit{}, err
				}
			}
			continue
		case runVPExitReasonX64Cpuid:
			words := (*[8]uint64)(unsafe.Pointer(&c.raw.Payload[0]))
			leaf, index := uint32(words[0]), uint32(words[1])
			r := x86state.CPUIDEntry{}
			for _, e := range c.cpuid {
				if e.Function == leaf && (e.Flags&1 == 0 || e.Index == index) {
					r = e
					break
				}
			}
			if c.miscEnable&(1<<22) != 0 && leaf == 0 {
				r.Eax = min(r.Eax, 2)
			}
			if err := m.vm.SetVCPURegisters(c.index, map[registerName]uint64{registerRax: uint64(r.Eax), registerRbx: uint64(r.Ebx), registerRcx: uint64(r.Ecx), registerRdx: uint64(r.Edx), registerRip: c.raw.VpContext.Rip + uint64(c.raw.instructionLength())}); err != nil {
				return x86state.Exit{}, err
			}
			continue
		case runVPExitReasonX64MsrAccess:
			msr := c.raw.msrAccess()
			v, ok := m.fallback[msr.MSRNumber]
			if msr.MSRNumber == 0x1a0 {
				if msr.AccessInfo.isWrite() {
					value := uint64(uint32(msr.Rax)) | uint64(uint32(msr.Rdx))<<32
					if value & ^uint64(1|1<<22) == 0 {
						c.miscEnable = value
						if err := m.vm.SetVCPURIP(c.index, c.raw.VpContext.Rip+2); err != nil {
							return x86state.Exit{}, err
						}
						continue
					}
				}
				v, ok = c.miscEnable, true
			}
			if !ok || msr.AccessInfo.isWrite() {
				if err := c.injectException(13, 0, 0, true); err != nil {
					return x86state.Exit{}, err
				}
				continue
			}
			if err := m.vm.SetVCPURegisters(c.index, map[registerName]uint64{registerRax: v & 0xffffffff, registerRdx: v >> 32, registerRip: c.raw.VpContext.Rip + uint64(c.raw.instructionLength())}); err != nil {
				return x86state.Exit{}, err
			}
			continue
		case runVPExitReasonException:
			fault := (*nativeException)(unsafe.Pointer(&c.raw.Payload[0]))
			r, err := c.Registers()
			if err != nil {
				return x86state.Exit{}, err
			}
			if fault.Vector == 13 && fault.Count >= 2 && fault.Bytes[0] == 0x0f && (fault.Bytes[1] == 0x32 || fault.Bytes[1] == 0x30) && c.raw.VpContext.ExecutionState.AsUint16&3 == 0 {
				v, ok := m.fallback[uint32(r.Rcx)]
				if uint32(r.Rcx) == 0x1a0 {
					if fault.Bytes[1] == 0x30 {
						value := uint64(uint32(r.Rax)) | uint64(uint32(r.Rdx))<<32
						if value & ^uint64(1|1<<22) == 0 {
							c.miscEnable = value
							r.Rip += 2
							if err := c.SetRegisters(r); err != nil {
								return x86state.Exit{}, err
							}
							continue
						}
					}
					v, ok = c.miscEnable, true
				}
				if ok && fault.Bytes[1] == 0x32 {
					r.Rax = v & 0xffffffff
					r.Rdx = v >> 32
					r.Rip += 2
					if err := c.SetRegisters(r); err != nil {
						return x86state.Exit{}, err
					}
					continue
				}
			}
			if err := c.injectException(fault.Vector, fault.Error, fault.Parameter, fault.Info&1 != 0); err != nil {
				return x86state.Exit{}, err
			}
			continue
		case runVPExitReasonUnrecoverableException:
			return x86state.Exit{Reason: 8}, nil
		case runVPExitReasonX64IoPortAccess, runVPExitReasonMemoryAccess:
			c.emulating = true
			c.jobs <- c.raw.ExitReason == runVPExitReasonMemoryAccess
			select {
			case err := <-c.results:
				c.emulating = false
				if err != nil {
					return x86state.Exit{}, err
				}
				continue
			case req := <-c.requests:
				c.pending = true
				c.io = req.exit.Data
				if handle == nil {
					return req.exit, nil
				}
				handled, err := handle(req.exit)
				if err != nil || !handled {
					return req.exit, err
				}
			}
		default:
			return x86state.Exit{}, fmt.Errorf("WHP exit %s at %#x", c.raw.ExitReason, c.raw.VpContext.Rip)
		}
	}
}

func (c *NativeCPU) injectException(vector uint8, code uint32, parameter uint64, hasCode bool) error {
	value := registerValue{raw: uint128{Low64: 1 | uint64(vector)<<16 | uint64(code)<<32, High64: parameter}}
	if hasCode {
		value.raw.Low64 |= 1 << 8
	}
	return setVirtualProcessorRegisters(c.machine.vm.part, c.index, []registerName{registerName(0x80000002)}, []registerValue{value})
}

type nativeException struct {
	Count     uint8
	Reserved  [3]byte
	Bytes     [16]byte
	Info      uint32
	Vector    uint8
	Reserved2 [3]byte
	Error     uint32
	Parameter uint64
}
