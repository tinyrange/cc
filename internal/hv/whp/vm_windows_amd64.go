//go:build windows && amd64

package whp

import (
	"context"
	"encoding/binary"
	"fmt"
	"sync/atomic"
	"time"
	"unsafe"
)

type VM struct {
	part         partitionHandle
	mem          *allocation
	memGPA       uint64
	memSize      uint64
	vcpuCreated  bool
	emulator     emulatorHandle
	emuCallbacks emulatorCallbacks
	emuContext   *emulatorContext
	emuErr       error
	running      atomic.Bool
}

type Exit struct {
	Reason runVPExitReason
	RIP    uint64
	RFLAGS uint64
}

func Supports() error {
	present, err := isHypervisorPresent()
	if err != nil {
		if probeErr := probePartitionSupport(); probeErr == nil {
			return nil
		} else {
			return fmt.Errorf("whp unavailable: query hypervisor presence: %w; partition probe: %w", err, probeErr)
		}
	}
	if !present {
		return fmt.Errorf("whp unavailable: hypervisor not present")
	}
	return nil
}

func probePartitionSupport() error {
	part, err := createPartition()
	if err != nil {
		return fmt.Errorf("create partition: %w", err)
	}
	if err := deletePartition(part); err != nil {
		return fmt.Errorf("delete partition: %w", err)
	}
	return nil
}

func NewVM(memorySize uint64) (*VM, error) {
	return newVM(memorySize, false)
}

func newBootVM(memorySize uint64) (*VM, error) {
	return newVM(memorySize, true)
}

func newVM(memorySize uint64, localAPIC bool) (*VM, error) {
	if memorySize == 0 {
		return nil, fmt.Errorf("memory size must be non-zero")
	}
	part, err := createPartition()
	if err != nil {
		return nil, fmt.Errorf("create partition: %w", err)
	}
	vm := &VM{part: part}
	if err := setPartitionProperty(part, partitionPropertyCodeProcessorCount, uint32(1)); err != nil {
		_ = vm.Close()
		return nil, fmt.Errorf("set processor count: %w", err)
	}
	if localAPIC {
		if err := setPartitionProperty(part, partitionPropertyCodeLocalAPICEmulationMode, localAPICEmulationModeXAPIC); err != nil {
			_ = vm.Close()
			return nil, fmt.Errorf("set local APIC emulation mode: %w", err)
		}
	}
	if err := setupPartition(part); err != nil {
		_ = vm.Close()
		return nil, fmt.Errorf("setup partition: %w", err)
	}
	mem, err := virtualAlloc(uintptr(memorySize))
	if err != nil {
		_ = vm.Close()
		return nil, fmt.Errorf("allocate guest memory: %w", err)
	}
	vm.mem = mem
	vm.memSize = memorySize
	if err := mapGPARange(part, unsafe.Pointer(mem.addr), 0, memorySize, mapGPARangeFlagRead|mapGPARangeFlagWrite|mapGPARangeFlagExecute); err != nil {
		_ = vm.Close()
		return nil, fmt.Errorf("map guest memory: %w", err)
	}
	if err := createVirtualProcessor(part, 0); err != nil {
		_ = vm.Close()
		return nil, fmt.Errorf("create virtual processor: %w", err)
	}
	vm.vcpuCreated = true
	return vm, nil
}

func (v *VM) Close() error {
	if v == nil {
		return nil
	}
	var first error
	if v.emulator != 0 {
		if err := destroyEmulator(v.emulator); err != nil && first == nil {
			first = err
		}
		v.emulator = 0
	}
	if v.part != 0 && v.vcpuCreated {
		_ = cancelRunVirtualProcessor(v.part, 0)
		if err := deleteVirtualProcessor(v.part, 0); err != nil && first == nil {
			first = err
		}
		v.vcpuCreated = false
	}
	if v.part != 0 && v.mem != nil {
		if err := unmapGPARange(v.part, v.memGPA, v.memSize); err != nil && first == nil {
			first = err
		}
	}
	if v.mem != nil {
		if err := v.mem.free(); err != nil && first == nil {
			first = err
		}
		v.mem = nil
	}
	if v.part != 0 {
		if err := deletePartition(v.part); err != nil && first == nil {
			first = err
		}
		v.part = 0
		time.Sleep(3 * time.Second)
	}
	return first
}

func (v *VM) Memory() []byte {
	if v == nil || v.mem == nil {
		return nil
	}
	return v.mem.bytes()
}

func (v *VM) ReadIPA(addr uint64, size int) ([]byte, error) {
	if size < 0 {
		return nil, fmt.Errorf("invalid read size %d", size)
	}
	if v == nil || v.mem == nil {
		return nil, fmt.Errorf("guest memory is not mapped")
	}
	if addr > v.memSize || uint64(size) > v.memSize-addr {
		return nil, fmt.Errorf("read ipa %#x size %d out of range %#x", addr, size, v.memSize)
	}
	out := make([]byte, size)
	copy(out, v.mem.bytes()[addr:addr+uint64(size)])
	return out, nil
}

func (v *VM) WriteIPA(addr uint64, data []byte) error {
	if v == nil || v.mem == nil {
		return fmt.Errorf("guest memory is not mapped")
	}
	if addr > v.memSize || uint64(len(data)) > v.memSize-addr {
		return fmt.Errorf("write ipa %#x size %d out of range %#x", addr, len(data), v.memSize)
	}
	copy(v.mem.bytes()[addr:addr+uint64(len(data))], data)
	return nil
}

func (v *VM) SetFlatProtectedMode(entry uint64) error {
	code := x64SegmentRegister{Base: 0, Limit: 0xffffffff, Selector: 0x8, Attributes: segmentAttributes(11, 1, 0, 1, 0, 0, 1, 1)}
	data := x64SegmentRegister{Base: 0, Limit: 0xffffffff, Selector: 0x10, Attributes: segmentAttributes(3, 1, 0, 1, 0, 0, 1, 1)}
	names := []registerName{
		registerCr0,
		registerCr3,
		registerCr4,
		registerEfer,
		registerCs,
		registerDs,
		registerEs,
		registerFs,
		registerGs,
		registerSs,
		registerRip,
		registerRsp,
		registerRflags,
	}
	values := []registerValue{
		uint64RegisterValue(1), // CR0.PE
		uint64RegisterValue(0),
		uint64RegisterValue(0),
		uint64RegisterValue(0),
		segmentRegisterValue(code),
		segmentRegisterValue(data),
		segmentRegisterValue(data),
		segmentRegisterValue(data),
		segmentRegisterValue(data),
		segmentRegisterValue(data),
		uint64RegisterValue(entry),
		uint64RegisterValue(v.memSize - 0x10),
		uint64RegisterValue(0x2),
	}
	if err := setVirtualProcessorRegisters(v.part, 0, names, values); err != nil {
		return fmt.Errorf("set flat protected-mode registers: %w", err)
	}
	return nil
}

func (v *VM) SetLongMode(entry, zeroPage, stack, pagingBase uint64) error {
	if err := v.setupPageTables(pagingBase, 4); err != nil {
		return err
	}
	const (
		cr0PE   = 1 << 0
		cr0MP   = 1 << 1
		cr0ET   = 1 << 4
		cr0NE   = 1 << 5
		cr0WP   = 1 << 16
		cr0AM   = 1 << 18
		cr0PG   = 1 << 31
		cr4PAE  = 1 << 5
		eferLME = 1 << 8
		eferLMA = 1 << 10
	)
	code := x64SegmentRegister{Base: 0, Limit: 0xffffffff, Selector: 0x10, Attributes: segmentAttributes(11, 1, 0, 1, 0, 1, 0, 1)}
	data := x64SegmentRegister{Base: 0, Limit: 0xffffffff, Selector: 0x18, Attributes: segmentAttributes(3, 1, 0, 1, 0, 0, 1, 1)}
	names := []registerName{
		registerCr3,
		registerCr4,
		registerCr0,
		registerEfer,
		registerCs,
		registerDs,
		registerEs,
		registerFs,
		registerGs,
		registerSs,
		registerRip,
		registerRsi,
		registerRsp,
		registerRflags,
	}
	values := make([]registerValue, len(names))
	if err := getVirtualProcessorRegisters(v.part, 0, names, values); err != nil {
		return fmt.Errorf("get long-mode registers: %w", err)
	}
	values[0] = uint64RegisterValue(pagingBase)
	values[1] = uint64RegisterValue(values[1].uint64() | cr4PAE)
	values[2] = uint64RegisterValue(values[2].uint64() | cr0PE | cr0MP | cr0ET | cr0NE | cr0WP | cr0AM | cr0PG)
	values[3] = uint64RegisterValue(values[3].uint64() | eferLME | eferLMA)
	values[4] = segmentRegisterValue(code)
	values[5] = segmentRegisterValue(data)
	values[6] = segmentRegisterValue(data)
	values[7] = segmentRegisterValue(data)
	values[8] = segmentRegisterValue(data)
	values[9] = segmentRegisterValue(data)
	values[10] = uint64RegisterValue(entry)
	values[11] = uint64RegisterValue(zeroPage)
	values[12] = uint64RegisterValue(stack)
	values[13] = uint64RegisterValue(0x2)
	if err := setVirtualProcessorRegisters(v.part, 0, names, values); err != nil {
		return fmt.Errorf("set long-mode registers: %w", err)
	}
	return nil
}

func (v *VM) setupPageTables(pagingBase uint64, giB int) error {
	needed := pagingBase + uint64(0x3000+giB*0x1000)
	if needed > v.memSize {
		return fmt.Errorf("paging structures require %#x bytes, memory size %#x", needed, v.memSize)
	}
	mem := v.Memory()
	put64 := func(addr, value uint64) {
		binary.LittleEndian.PutUint64(mem[addr:addr+8], value)
	}
	pml4 := pagingBase
	pdpt := pagingBase + 0x1000
	pdBase := pagingBase + 0x2000
	const (
		p  = 1 << 0
		rw = 1 << 1
		us = 1 << 2
		ps = 1 << 7
	)
	for off := pagingBase; off < needed; off += 8 {
		put64(off, 0)
	}
	put64(pml4, pdpt|p|rw|us)
	for g := 0; g < giB; g++ {
		pd := pdBase + uint64(g)*0x1000
		put64(pdpt+uint64(g)*8, pd|p|rw|us)
		for i := 0; i < 512; i++ {
			phys := (uint64(g) << 30) | (uint64(i) << 21)
			put64(pd+uint64(i)*8, phys|p|rw|us|ps)
		}
	}
	return nil
}

func (v *VM) Run() (Exit, error) {
	var ctx runVPExitContext
	return v.runWithContext(&ctx)
}

func (v *VM) runWithContext(ctx *runVPExitContext) (Exit, error) {
	if err := runVirtualProcessor(v.part, 0, ctx); err != nil {
		return Exit{}, fmt.Errorf("run virtual processor: %w", err)
	}
	return Exit{Reason: ctx.ExitReason, RIP: ctx.VpContext.Rip, RFLAGS: ctx.VpContext.Rflags}, nil
}

func (v *VM) runWithCancel(ctx context.Context, raw *runVPExitContext) (Exit, error) {
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = cancelRunVirtualProcessor(v.part, 0)
		case <-done:
		}
	}()
	v.running.Store(true)
	exit, err := v.runWithContext(raw)
	v.running.Store(false)
	close(done)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return Exit{}, ctxErr
		}
		return Exit{}, err
	}
	if ctxErr := ctx.Err(); ctxErr != nil && exit.Reason == runVPExitReasonCanceled {
		return exit, ctxErr
	}
	return exit, nil
}

func (v *VM) GetRIP() (uint64, error) {
	names := []registerName{registerRip}
	values := make([]registerValue, 1)
	if err := getVirtualProcessorRegisters(v.part, 0, names, values); err != nil {
		return 0, err
	}
	return values[0].uint64(), nil
}

func (v *VM) RequestInterrupt(vector uint32) error {
	return v.RequestInterruptWithTrigger(vector, interruptTriggerEdge)
}

func (v *VM) RequestInterruptWithTrigger(vector uint32, trigger interruptTriggerMode) error {
	return requestInterrupt(v.part, vector, trigger)
}

func (v *VM) NotifyInterruptWindow() error {
	if v == nil || v.part == 0 {
		return nil
	}
	const value = uint64(1 << 1)
	names := []registerName{registerDeliverabilityNotifications}
	values := []registerValue{uint64RegisterValue(value)}
	return setVirtualProcessorRegisters(v.part, 0, names, values)
}

func (v *VM) SetPendingInterruption(vector uint8) error {
	if v == nil || v.part == 0 {
		return nil
	}
	const interruptionPending = uint64(1)
	value := interruptionPending | uint64(vector)<<16
	names := []registerName{registerPendingInterruption}
	values := []registerValue{uint64RegisterValue(value)}
	return setVirtualProcessorRegisters(v.part, 0, names, values)
}

func (v *VM) kickOutOfHLT() error {
	if v == nil || v.part == 0 {
		return nil
	}
	names := []registerName{registerInternalActivityState}
	values := make([]registerValue, 1)
	if err := getVirtualProcessorRegisters(v.part, 0, names, values); err != nil {
		return err
	}
	const haltSuspend = uint64(1 << 1)
	raw := values[0].uint64()
	if raw&haltSuspend == 0 {
		return nil
	}
	values[0] = uint64RegisterValue(raw &^ haltSuspend)
	return setVirtualProcessorRegisters(v.part, 0, names, values)
}

func (v *VM) kickIfRunning() {
	if v == nil || !v.running.Load() {
		return
	}
	_ = cancelRunVirtualProcessor(v.part, 0)
}

func segmentAttributes(typ, s, dpl, present, avl, long, db, gran uint16) uint16 {
	return (typ & 0xf) |
		((s & 0x1) << 4) |
		((dpl & 0x3) << 5) |
		((present & 0x1) << 7) |
		((avl & 0x1) << 12) |
		((long & 0x1) << 13) |
		((db & 0x1) << 14) |
		((gran & 0x1) << 15)
}
