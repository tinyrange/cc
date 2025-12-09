//go:build darwin && arm64

package hvf

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"runtime"
	"sync"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/tinyrange/cc/internal/hv"
)

const (
	arm64InstructionSizeBytes = 4
	psciSystemOffFunctionID   = 0x84000008
)

type hypervisor struct{}

func (*hypervisor) Close() error { return nil }

func (*hypervisor) Architecture() hv.CpuArchitecture {
	return hv.ArchitectureARM64
}

func (h *hypervisor) newVirtualMachine(config hv.VMConfig) (*virtualMachine, error) {
	if config == nil {
		return nil, fmt.Errorf("hvf: VMConfig is nil")
	}

	if config.CPUCount() != 1 {
		return nil, fmt.Errorf("hvf: only 1 vCPU is supported (requested %d)", config.CPUCount())
	}

	memSize := config.MemorySize()
	if memSize == 0 {
		return nil, fmt.Errorf("hvf: memory size must be greater than 0")
	}

	pageSize := uint64(unix.Getpagesize())
	if memSize%pageSize != 0 {
		return nil, fmt.Errorf("hvf: memory size (%d) must be aligned to host page size (%d)", memSize, pageSize)
	}

	if err := ensureInitialized(); err != nil {
		return nil, err
	}

	if err := hvVmCreate(0).toError("hv_vm_create"); err != nil {
		return nil, err
	}

	vm := &virtualMachine{
		hv:         h,
		vcpus:      make(map[int]*virtualCPU),
		memoryBase: config.MemoryBase(),
	}
	vm.vmCreated = true

	mem, err := unix.Mmap(
		-1,
		0,
		int(memSize),
		unix.PROT_READ|unix.PROT_WRITE,
		unix.MAP_ANON|unix.MAP_PRIVATE,
	)
	if err != nil {
		vm.closeInternal()
		return nil, fmt.Errorf("hvf: mmap guest memory: %w", err)
	}
	vm.memory = mem

	if err := hvVmMap(unsafe.Pointer(&mem[0]), vm.memoryBase, memSize, hvMemoryRead|hvMemoryWrite|hvMemoryExec).toError("hv_vm_map"); err != nil {
		vm.closeInternal()
		return nil, err
	}
	vm.memoryMapped = true

	if err := h.configureGIC(vm, config); err != nil {
		vm.closeInternal()
		return nil, err
	}

	if err := config.Callbacks().OnCreateVM(vm); err != nil {
		vm.closeInternal()
		return nil, fmt.Errorf("hvf: VM callback OnCreateVM: %w", err)
	}

	for i := 0; i < config.CPUCount(); i++ {
		vcpu, err := vm.createVCPU(i)
		if err != nil {
			vm.closeInternal()
			return nil, err
		}
		vm.vcpus[i] = vcpu

		if err := config.Callbacks().OnCreateVCPU(vcpu); err != nil {
			vm.closeInternal()
			return nil, fmt.Errorf("hvf: VM callback OnCreateVCPU %d: %w", i, err)
		}
	}

	if loader := config.Loader(); loader != nil {
		if err := loader.Load(vm); err != nil {
			vm.closeInternal()
			return nil, fmt.Errorf("hvf: load VM: %w", err)
		}
	}

	return vm, nil
}

func (h *hypervisor) NewVirtualMachine(config hv.VMConfig) (hv.VirtualMachine, error) {
	return h.newVirtualMachine(config)
}

type memoryRegion struct {
	memory []byte
}

func (m *memoryRegion) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 || int(off) >= len(m.memory) {
		return 0, fmt.Errorf("hvf: MemoryRegion ReadAt offset 0x%x out of bounds", off)
	}

	n := copy(p, m.memory[off:])
	if n < len(p) {
		return n, fmt.Errorf("hvf: MemoryRegion ReadAt short read")
	}

	return n, nil
}

func (m *memoryRegion) WriteAt(p []byte, off int64) (int, error) {
	if off < 0 || int(off) >= len(m.memory) {
		return 0, fmt.Errorf("hvf: MemoryRegion WriteAt offset 0x%x out of bounds", off)
	}

	n := copy(m.memory[off:], p)
	if n < len(p) {
		return n, fmt.Errorf("hvf: MemoryRegion WriteAt short write")
	}

	return n, nil
}

func (m *memoryRegion) Size() uint64 {
	return uint64(len(m.memory))
}

var (
	_ hv.MemoryRegion = &memoryRegion{}
)

type virtualMachine struct {
	hv         hv.Hypervisor
	vcpus      map[int]*virtualCPU
	memory     []byte
	memoryBase uint64
	devices    []hv.Device

	arm64GICInfo hv.Arm64GICInfo

	gicConfigured bool
	gicSPIBase    uint32
	gicSPICount   uint32

	vmCreated    bool
	memoryMapped bool

	closeOnce sync.Once
}

// implements hv.VirtualMachine.
func (v *virtualMachine) MemoryBase() uint64        { return v.memoryBase }
func (v *virtualMachine) MemorySize() uint64        { return uint64(len(v.memory)) }
func (v *virtualMachine) Hypervisor() hv.Hypervisor { return v.hv }

// AllocateMemory implements hv.VirtualMachine.
func (v *virtualMachine) AllocateMemory(physAddr uint64, size uint64) (hv.MemoryRegion, error) {
	mem, err := unix.Mmap(
		-1,
		0,
		int(size),
		unix.PROT_READ|unix.PROT_WRITE,
		unix.MAP_ANON|unix.MAP_PRIVATE,
	)
	if err != nil {
		return nil, fmt.Errorf("hvf: mmap guest memory: %w", err)
	}

	if err := hvVmMap(
		unsafe.Pointer(&mem[0]),
		physAddr,
		size,
		hvMemoryRead|hvMemoryWrite|hvMemoryExec,
	).toError("hv_vm_map"); err != nil {
		return nil, err
	}

	return &memoryRegion{
		memory: mem,
	}, nil
}

// CaptureSnapshot implements hv.VirtualMachine.
func (v *virtualMachine) CaptureSnapshot() (hv.Snapshot, error) {
	ret := &arm64Snapshot{
		cpuStates:       make(map[int]arm64VcpuSnapshot),
		deviceSnapshots: make(map[string]interface{}),
	}

	for i := range v.vcpus {
		if err := v.VirtualCPUCall(i, func(vcpu hv.VirtualCPU) error {
			state, err := vcpu.(*virtualCPU).captureSnapshot()
			if err != nil {
				return err
			}
			ret.cpuStates[i] = state
			return nil
		}); err != nil {
			return nil, fmt.Errorf("hvf: capture vCPU %d snapshot: %w", i, err)
		}
	}

	for _, dev := range v.devices {
		if snapshotter, ok := dev.(hv.DeviceSnapshotter); ok {
			id := snapshotter.DeviceId()
			snap, err := snapshotter.CaptureSnapshot()
			if err != nil {
				return nil, fmt.Errorf("hvf: capture device %s snapshot: %w", id, err)
			}
			ret.deviceSnapshots[id] = snap
		}
	}

	if len(v.memory) > 0 {
		ret.memory = make([]byte, len(v.memory))
		copy(ret.memory, v.memory)
	}

	return ret, nil
}

// RestoreSnapshot implements hv.VirtualMachine.
func (v *virtualMachine) RestoreSnapshot(snap hv.Snapshot) error {
	snapshotData, ok := snap.(*arm64Snapshot)
	if !ok {
		return fmt.Errorf("hvf: invalid snapshot type")
	}

	if len(v.memory) != len(snapshotData.memory) {
		return fmt.Errorf("hvf: snapshot memory size mismatch: got %d bytes, want %d bytes", len(snapshotData.memory), len(v.memory))
	}
	if len(v.memory) > 0 {
		copy(v.memory, snapshotData.memory)
	}

	for i := range v.vcpus {
		state, ok := snapshotData.cpuStates[i]
		if !ok {
			return fmt.Errorf("hvf: missing vCPU %d state in snapshot", i)
		}

		if err := v.VirtualCPUCall(i, func(vcpu hv.VirtualCPU) error {
			return vcpu.(*virtualCPU).restoreSnapshot(state)
		}); err != nil {
			return fmt.Errorf("hvf: restore vCPU %d snapshot: %w", i, err)
		}
	}

	for _, dev := range v.devices {
		if snapshotter, ok := dev.(hv.DeviceSnapshotter); ok {
			id := snapshotter.DeviceId()
			snapData, ok := snapshotData.deviceSnapshots[id]
			if !ok {
				return fmt.Errorf("hvf: missing device %s snapshot", id)
			}
			if err := snapshotter.RestoreSnapshot(snapData); err != nil {
				return fmt.Errorf("hvf: restore device %s snapshot: %w", id, err)
			}
		}
	}

	return nil
}

func (v *virtualMachine) AddDevice(dev hv.Device) error {
	v.devices = append(v.devices, dev)
	return dev.Init(v)
}

// AddDeviceFromTemplate implements hv.VirtualMachine.
func (v *virtualMachine) AddDeviceFromTemplate(template hv.DeviceTemplate) error {
	dev, err := template.Create(v)
	if err != nil {
		return fmt.Errorf("create device from template: %w", err)
	}

	return v.AddDevice(dev)
}

func (v *virtualMachine) Run(ctx context.Context, cfg hv.RunConfig) error {
	if cfg == nil {
		return fmt.Errorf("hvf: RunConfig is nil")
	}

	vcpu, ok := v.vcpus[0]
	if !ok {
		return fmt.Errorf("hvf: vCPU 0 not found")
	}

	done := make(chan error, 1)
	vcpu.runQueue <- func() {
		done <- cfg.Run(ctx, vcpu)
	}

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (v *virtualMachine) VirtualCPUCall(id int, f func(vcpu hv.VirtualCPU) error) error {
	vcpu, ok := v.vcpus[id]
	if !ok {
		return fmt.Errorf("hvf: vCPU %d not found", id)
	}

	done := make(chan error, 1)
	vcpu.runQueue <- func() {
		done <- f(vcpu)
	}
	return <-done
}

func (v *virtualMachine) ReadAt(p []byte, off int64) (int, error) {
	offset := off - int64(v.memoryBase)
	if offset < 0 || int(offset) >= len(v.memory) {
		return 0, fmt.Errorf("hvf: ReadAt offset 0x%x out of bounds", off)
	}

	n := copy(p, v.memory[offset:])
	if n < len(p) {
		return n, fmt.Errorf("hvf: ReadAt short read")
	}
	return n, nil
}

func (v *virtualMachine) WriteAt(p []byte, off int64) (int, error) {
	offset := off - int64(v.memoryBase)
	if offset < 0 || int(offset) >= len(v.memory) {
		return 0, fmt.Errorf("hvf: WriteAt offset 0x%x out of bounds", off)
	}

	n := copy(v.memory[offset:], p)
	if n < len(p) {
		return n, fmt.Errorf("hvf: WriteAt short write")
	}
	return n, nil
}

func (v *virtualMachine) Arm64GICInfo() (hv.Arm64GICInfo, bool) {
	if v == nil || !v.gicConfigured || v.arm64GICInfo.Version == hv.Arm64GICVersionUnknown {
		return hv.Arm64GICInfo{}, false
	}
	return v.arm64GICInfo, true
}

// ARM64 KVM IRQ type encoding (bits 31-24 of irq field)
const (
	armIRQTypeShift = 24 // Shift for IRQ type in encoded irqLine
	armIRQTypeSPI   = 1  // Shared Peripheral Interrupt
)

func (v *virtualMachine) SetIRQ(irqLine uint32, level bool) error {
	if !v.gicConfigured {
		return fmt.Errorf("hvf: interrupt controller not configured")
	}

	if hvGicSetSpi == nil {
		return fmt.Errorf("hvf: hv_gic_set_spi unavailable")
	}

	// Decode the KVM-style IRQ encoding used by EncodeIRQLineForArch.
	// Bits 31-24 contain the IRQ type, bits 15-0 contain the GIC INTID.
	irqType := (irqLine >> armIRQTypeShift) & 0xff
	if irqType != 0 {
		if irqType != armIRQTypeSPI {
			return fmt.Errorf("hvf: unsupported IRQ type %d in irqLine %#x", irqType, irqLine)
		}
		// Extract the GIC INTID from low 16 bits.
		irqLine = irqLine & 0xffff
	}

	if irqLine < v.gicSPIBase || irqLine >= v.gicSPIBase+v.gicSPICount {
		return fmt.Errorf("hvf: SPI %d out of range (%d-%d)", irqLine, v.gicSPIBase, v.gicSPIBase+v.gicSPICount-1)
	}

	return hvGicSetSpi(irqLine, level).toError("hv_gic_set_spi")
}

func (v *virtualMachine) Close() error {
	var closeErr error

	v.closeOnce.Do(func() {
		closeErr = v.closeInternal()
	})

	return closeErr
}

func (v *virtualMachine) closeInternal() error {
	var firstErr error

	for _, vcpu := range v.vcpus {
		if err := vcpu.shutdown(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	v.vcpus = nil

	if v.memoryMapped {
		if err := hvVmUnmap(v.memoryBase, uint64(len(v.memory))).toError("hv_vm_unmap"); err != nil && firstErr == nil {
			firstErr = err
		}
		v.memoryMapped = false
	}

	if v.memory != nil {
		if err := unix.Munmap(v.memory); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("hvf: munmap memory: %w", err)
		}
		v.memory = nil
	}

	if v.vmCreated {
		if err := hvVmDestroy().toError("hv_vm_destroy"); err != nil && firstErr == nil {
			firstErr = err
		}
		v.vmCreated = false
	}

	return firstErr
}

func (v *virtualMachine) createVCPU(id int) (*virtualCPU, error) {
	vcpu := &virtualCPU{
		vm:       v,
		index:    id,
		runQueue: make(chan func(), 16),
		initErr:  make(chan error, 1),
	}

	go vcpu.start()

	if err := <-vcpu.initErr; err != nil {
		return nil, err
	}

	return vcpu, nil
}

type virtualCPU struct {
	vm       *virtualMachine
	hostID   uint64
	index    int
	exit     *hvVcpuExit
	runQueue chan func()
	initErr  chan error

	pendingPC *uint64
}

func (v *virtualCPU) start() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	var exitPtr *hvVcpuExit
	var hostID uint64
	ret := hvVcpuCreate(&hostID, &exitPtr, 0)
	if err := ret.toError("hv_vcpu_create"); err != nil {
		v.initErr <- err
		close(v.initErr)
		close(v.runQueue)
		return
	}
	v.hostID = hostID
	v.exit = exitPtr
	v.initErr <- nil
	close(v.initErr)

	for fn := range v.runQueue {
		fn()
	}
}

func (v *virtualCPU) shutdown() error {
	if v.runQueue == nil {
		return nil
	}

	done := make(chan error, 1)
	v.runQueue <- func() {
		done <- hvVcpuDestroy(v.hostID).toError("hv_vcpu_destroy")
	}
	close(v.runQueue)
	return <-done
}

func (v *virtualCPU) VirtualMachine() hv.VirtualMachine { return v.vm }
func (v *virtualCPU) ID() int                           { return v.index }

func (v *virtualCPU) SetRegisters(regs map[hv.Register]hv.RegisterValue) error {
	for reg, value := range regs {
		if reg == hv.RegisterARM64GicrBase {
			return fmt.Errorf("hvf: register %v is read-only", reg)
		}

		raw, ok := value.(hv.Register64)
		if !ok {
			return fmt.Errorf("hvf: unsupported register value type %T for %v", value, reg)
		}

		if sys, ok := hvSysRegFromRegister(reg); ok {
			if err := hvVcpuSetSys(v.hostID, sys, uint64(raw)).toError("hv_vcpu_set_sys_reg"); err != nil {
				return err
			}
			continue
		}

		hreg, ok := hvRegFromRegister(reg)
		if !ok {
			return fmt.Errorf("hvf: unsupported register %v", reg)
		}
		if err := hvVcpuSetReg(v.hostID, hreg, uint64(raw)).toError("hv_vcpu_set_reg"); err != nil {
			return err
		}
	}
	return nil
}

func (v *virtualCPU) GetRegisters(regs map[hv.Register]hv.RegisterValue) error {
	for reg := range regs {
		if reg == hv.RegisterARM64GicrBase {
			info := v.vm.arm64GICInfo
			if info.Version != hv.Arm64GICVersion3 || info.RedistributorBase == 0 || info.RedistributorSize == 0 {
				return fmt.Errorf("hvf: register %v not available", reg)
			}
			base := info.RedistributorBase + uint64(v.index)*info.RedistributorSize
			regs[reg] = hv.Register64(base)
			continue
		}

		if sys, ok := hvSysRegFromRegister(reg); ok {
			var val uint64
			if err := hvVcpuGetSys(v.hostID, sys, &val).toError("hv_vcpu_get_sys_reg"); err != nil {
				return err
			}
			regs[reg] = hv.Register64(val)
			continue
		}

		hreg, ok := hvRegFromRegister(reg)
		if !ok {
			return fmt.Errorf("hvf: unsupported register %v", reg)
		}

		var val uint64
		if err := hvVcpuGetReg(v.hostID, hreg, &val).toError("hv_vcpu_get_reg"); err != nil {
			return err
		}
		regs[reg] = hv.Register64(val)
	}
	return nil
}

func (v *virtualCPU) Run(ctx context.Context) error {
	var stopExit func() bool
	if ctx.Done() != nil {
		stopExit = context.AfterFunc(ctx, func() {
			hostID := v.hostID
			_ = hvVcpusExit(&hostID, 1).toError("hv_vcpus_exit")
		})
	}
	if stopExit != nil {
		defer stopExit()
	}

	for {
		if err := hvVcpuRun(v.hostID).toError("hv_vcpu_run"); err != nil {
			return err
		}

		switch v.exit.Reason {
		case hvExitReasonCanceled:
			if err := ctx.Err(); err != nil {
				return err
			}
			return context.Canceled
		case hvExitReasonException:
			if err := v.handleException(); err != nil {
				return err
			}
			// Continue running after successfully handling the exception
		case hvExitReasonVTimerActivated, hvExitReasonVTimerDeactivated:
			// Spurious timer exits; continue running.
			continue
		default:
			return fmt.Errorf("hvf: unsupported vCPU exit reason %d", v.exit.Reason)
		}
	}
}

const (
	exceptionClassMask             = 0x3F
	exceptionClassShift            = 26
	exceptionClassHvc              = 0x16
	exceptionClassSmc              = 0x17
	exceptionClassMsrAccess        = 0x18
	exceptionClassDataAbortLowerEL = 0x24
)

func (v *virtualCPU) handleException() error {
	syndrome := v.exit.Exception.Syndrome
	ec := (syndrome >> exceptionClassShift) & exceptionClassMask

	switch ec {
	case exceptionClassHvc, exceptionClassSmc:
		return v.handleHypercall(ec)
	case exceptionClassMsrAccess:
		return v.handleMsrAccess(syndrome)
	case exceptionClassDataAbortLowerEL:
		return v.handleDataAbort(syndrome, v.exit.Exception.PhysicalAddress, v.exit.Exception.VirtualAddress)
	default:
		return fmt.Errorf("hvf: unsupported exception class 0x%x (syndrome=0x%x)", ec, syndrome)
	}
}

type msrAccessInfo struct {
	op0, op1, op2 uint8
	crn, crm      uint8
	read          bool // true = MRS (sysreg -> Rt), false = MSR (Rt -> sysreg)
	target        hv.Register
}

func decodeMsrAccess(syndrome uint64) (msrAccessInfo, error) {
	const (
		issMask uint64 = (1 << 25) - 1 // bits [24:0]

		directionBit = 0

		crmShift = 1
		crmMask  = 0xF

		rtShift = 5
		rtMask  = 0x1F

		crnShift = 10
		crnMask  = 0xF

		op1Shift = 14
		op1Mask  = 0x7

		op2Shift = 17
		op2Mask  = 0x7

		op0Shift = 20
		op0Mask  = 0x3
	)

	iss := syndrome & issMask

	read := ((iss >> directionBit) & 0x1) == 1

	crm := uint8((iss >> crmShift) & crmMask)
	rtIndex := int((iss >> rtShift) & rtMask)
	crn := uint8((iss >> crnShift) & crnMask)
	op1 := uint8((iss >> op1Shift) & op1Mask)
	op2 := uint8((iss >> op2Shift) & op2Mask)
	op0 := uint8((iss >> op0Shift) & op0Mask)

	reg, ok := arm64RegisterFromIndex(rtIndex)
	if !ok {
		return msrAccessInfo{}, fmt.Errorf("hvf: unsupported MSR/MRS target register index %d", rtIndex)
	}

	return msrAccessInfo{
		op0:    op0,
		op1:    op1,
		op2:    op2,
		crn:    crn,
		crm:    crm,
		read:   read,
		target: reg,
	}, nil
}

// Small helper so you can pattern-match on specific system registers.
func (m msrAccessInfo) matches(op0, op1, crn, crm, op2 uint8) bool {
	return m.op0 == op0 && m.op1 == op1 && m.crn == crn && m.crm == crm && m.op2 == op2
}

func (v *virtualCPU) handleMsrAccess(syndrome uint64) error {
	info, err := decodeMsrAccess(syndrome)
	if err != nil {
		return err
	}

	// Example: recognize specific system registers by encoding if you want
	// to emulate them specially. For now, everything is treated as:
	//   - MSR: write-ignored
	//   - MRS: read-as-zero
	//
	// Example (CNTVCT_EL0: op0=3, op1=3, CRn=14, CRm=0, op2=2) :contentReference[oaicite:1]{index=1}
	//
	// if info.matches(3, 3, 14, 0, 2) { // CNTVCT_EL0
	//     if info.read {
	//         // TODO: provide a virtual counter value
	//         if err := v.writeRegister(info.target, someCounterValue); err != nil {
	//             return err
	//         }
	//     } else {
	//         // Writes to CNTVCT_EL0 are architecturally ignored.
	//     }
	// } else {
	//     // fall through to default handling below
	// }

	if info.read {
		// Default: read-as-zero for unhandled sysregs.
		if err := v.writeRegister(info.target, 0); err != nil {
			return err
		}
	} else {
		// Default: ignore writes for unhandled sysregs.
		// You *could* log here if you want visibility:
		log.Printf("hvf: ignoring MSR op0=%d op1=%d CRn=%d CRm=%d op2=%d", info.op0, info.op1, info.crn, info.crm, info.op2)
	}

	return v.advanceProgramCounter()
}

// PSCI function IDs (SMC32 calling convention)
const (
	psciVersion         = 0x84000000
	psciCpuSuspend      = 0x84000001
	psciCpuOff          = 0x84000002
	psciCpuOn           = 0x84000003
	psciAffinityInfo    = 0x84000004
	psciMigrateInfoType = 0x84000006
	psciSystemOff       = 0x84000008
	psciSystemReset     = 0x84000009
	psciFeatures        = 0x8400000A

	// PSCI return values
	psciSuccess            = 0
	psciNotSupported       = 0xFFFFFFFF // -1 as uint32
	psciInvalidParameters  = 0xFFFFFFFE // -2 as uint32
	psciDenied             = 0xFFFFFFFD // -3 as uint32
	psciAlreadyOn          = 0xFFFFFFFC // -4 as uint32
	psciOnPending          = 0xFFFFFFFB // -5 as uint32
	psciInternalFailure    = 0xFFFFFFFA // -6 as uint32
	psciNotPresent         = 0xFFFFFFF9 // -7 as uint32
	psciDisabled           = 0xFFFFFFF8 // -8 as uint32
	psciInvalidAddress     = 0xFFFFFFF7 // -9 as uint32
	psciTosNotPresent      = 2          // For MIGRATE_INFO_TYPE: no trusted OS
)

func (v *virtualCPU) handleHypercall(ec uint64) error {
	val, err := v.readRegister(hv.RegisterARM64X0)
	if err != nil {
		return err
	}

	// HVC (EC 0x16) automatically advances PC in Apple's Hypervisor Framework.
	// SMC (EC 0x17) does NOT automatically advance PC, so we must do it manually.
	if ec == exceptionClassSmc {
		if err := v.advanceProgramCounter(); err != nil {
			return err
		}
	}

	switch val {
	case psciVersion:
		// Reply PSCI version 1.0
		return v.writeRegister(hv.RegisterARM64X0, 0x00010000)

	case psciCpuOff:
		// Single CPU VM - CPU_OFF halts the VM
		return hv.ErrVMHalted

	case psciCpuOn:
		// Single CPU VM - CPU_ON not supported
		return v.writeRegister(hv.RegisterARM64X0, psciAlreadyOn)

	case psciAffinityInfo:
		// For single CPU VM, CPU 0 is always ON (return 0)
		return v.writeRegister(hv.RegisterARM64X0, 0)

	case psciMigrateInfoType:
		// Return "Trusted OS not present" - no migration support needed
		return v.writeRegister(hv.RegisterARM64X0, psciTosNotPresent)

	case psciSystemOff:
		return hv.ErrVMHalted

	case psciSystemReset:
		return hv.ErrGuestRequestedReboot

	case psciFeatures:
		// Return NOT_SUPPORTED for feature queries we don't implement
		return v.writeRegister(hv.RegisterARM64X0, psciNotSupported)

	case psciCpuSuspend:
		// For simplicity, treat suspend as a no-op (return success)
		return v.writeRegister(hv.RegisterARM64X0, psciSuccess)

	default:
		// Unknown PSCI function - return NOT_SUPPORTED
		return v.writeRegister(hv.RegisterARM64X0, psciNotSupported)
	}
}

type dataAbortInfo struct {
	sizeBytes int
	write     bool
	target    hv.Register
}

func decodeDataAbort(syndrome uint64) (dataAbortInfo, error) {
	const (
		dataAbortISSMask uint64 = (1 << 25) - 1
		isvBit                  = 24
		sasShift                = 22
		sasMask          uint64 = 0x3
		srtShift                = 16
		srtMask          uint64 = 0x1F
		wnrBit                  = 6
	)

	iss := syndrome & dataAbortISSMask
	if ((iss >> isvBit) & 0x1) == 0 {
		return dataAbortInfo{}, fmt.Errorf("hvf: data abort without ISV set (syndrome=0x%x)", syndrome)
	}

	sas := (iss >> sasShift) & sasMask
	size := 1 << sas
	if sas > 3 {
		return dataAbortInfo{}, fmt.Errorf("hvf: invalid SAS value %d", sas)
	}

	srt := int((iss >> srtShift) & srtMask)
	reg, ok := arm64RegisterFromIndex(srt)
	if !ok {
		return dataAbortInfo{}, fmt.Errorf("hvf: unsupported data abort target register index %d", srt)
	}

	write := ((iss >> wnrBit) & 0x1) == 1

	return dataAbortInfo{
		sizeBytes: int(size),
		write:     write,
		target:    reg,
	}, nil
}

func (v *virtualCPU) handleDataAbort(syndrome, physAddr, virtAddr uint64) error {
	access, err := decodeDataAbort(syndrome)
	if err != nil {
		return err
	}

	addr := physAddr
	if addr == 0 {
		addr = virtAddr
	}

	dev, err := v.findMMIODevice(addr, uint64(access.sizeBytes))
	if err != nil {
		return err
	}

	var pendingError error

	data := make([]byte, access.sizeBytes)
	if access.write {
		value, err := v.readRegister(access.target)
		if err != nil {
			return err
		}
		for i := 0; i < access.sizeBytes; i++ {
			data[i] = byte(value >> (8 * i))
		}

		if err := dev.WriteMMIO(addr, data); err != nil {
			pendingError = fmt.Errorf("hvf: MMIO write 0x%x (%d bytes): %w", addr, access.sizeBytes, err)
		}
	} else {
		if err := dev.ReadMMIO(addr, data); err != nil {
			pendingError = fmt.Errorf("hvf: MMIO read 0x%x (%d bytes): %w", addr, access.sizeBytes, err)
		}

		var tmp [8]byte
		copy(tmp[:], data)
		value := binary.LittleEndian.Uint64(tmp[:])
		if err := v.writeRegister(access.target, value); err != nil {
			return err
		}
	}

	if err := v.advanceProgramCounter(); err != nil {
		return err
	}

	return pendingError
}

func (v *virtualCPU) findMMIODevice(addr, size uint64) (hv.MemoryMappedIODevice, error) {
	for _, dev := range v.vm.devices {
		mmio, ok := dev.(hv.MemoryMappedIODevice)
		if !ok {
			continue
		}
		for _, region := range mmio.MMIORegions() {
			if addr >= region.Address && addr+size <= region.Address+region.Size {
				return mmio, nil
			}
		}
	}
	return nil, fmt.Errorf("hvf: no MMIO device handles address 0x%x (size=%d)", addr, size)
}

func (v *virtualCPU) readRegister(reg hv.Register) (uint64, error) {
	// XZR (zero register) always reads as 0
	if reg == hv.RegisterARM64Xzr {
		return 0, nil
	}

	if sys, ok := hvSysRegFromRegister(reg); ok {
		var val uint64
		if err := hvVcpuGetSys(v.hostID, sys, &val).toError("hv_vcpu_get_sys_reg"); err != nil {
			return 0, err
		}
		return val, nil
	}

	hreg, ok := hvRegFromRegister(reg)
	if !ok {
		return 0, fmt.Errorf("hvf: unsupported register %v", reg)
	}

	var val uint64
	if err := hvVcpuGetReg(v.hostID, hreg, &val).toError("hv_vcpu_get_reg"); err != nil {
		return 0, err
	}
	return val, nil
}

func (v *virtualCPU) writeRegister(reg hv.Register, value uint64) error {
	// XZR (zero register) writes are discarded
	if reg == hv.RegisterARM64Xzr {
		return nil
	}

	if sys, ok := hvSysRegFromRegister(reg); ok {
		return hvVcpuSetSys(v.hostID, sys, value).toError("hv_vcpu_set_sys_reg")
	}

	hreg, ok := hvRegFromRegister(reg)
	if !ok {
		return fmt.Errorf("hvf: unsupported register %v", reg)
	}
	return hvVcpuSetReg(v.hostID, hreg, value).toError("hv_vcpu_set_reg")
}

func (v *virtualCPU) advanceProgramCounter() error {
	pc, err := v.readRegister(hv.RegisterARM64Pc)
	if err != nil {
		return fmt.Errorf("hvf: read PC: %w", err)
	}
	newPC := pc + arm64InstructionSizeBytes
	if err := v.writeRegister(hv.RegisterARM64Pc, newPC); err != nil {
		return err
	}
	return nil
}

// Snapshot helpers

type arm64VcpuSnapshot struct {
	Registers map[hv.Register]uint64
}

type arm64Snapshot struct {
	cpuStates       map[int]arm64VcpuSnapshot
	deviceSnapshots map[string]interface{}
	memory          []byte
}

func (v *virtualCPU) captureSnapshot() (arm64VcpuSnapshot, error) {
	regs := make(map[hv.Register]hv.RegisterValue, len(arm64GeneralRegisterMap)+len(arm64SysRegisterMap))
	for reg := range arm64GeneralRegisterMap {
		regs[reg] = hv.Register64(0)
	}
	for reg := range arm64SysRegisterMap {
		regs[reg] = hv.Register64(0)
	}

	if err := v.GetRegisters(regs); err != nil {
		return arm64VcpuSnapshot{}, fmt.Errorf("hvf: capture registers: %w", err)
	}

	out := arm64VcpuSnapshot{
		Registers: make(map[hv.Register]uint64, len(regs)),
	}
	for reg, val := range regs {
		out.Registers[reg] = uint64(val.(hv.Register64))
	}

	return out, nil
}

func (v *virtualCPU) restoreSnapshot(snap arm64VcpuSnapshot) error {
	regs := make(map[hv.Register]hv.RegisterValue, len(snap.Registers))
	for reg, val := range snap.Registers {
		regs[reg] = hv.Register64(val)
	}

	if err := v.SetRegisters(regs); err != nil {
		return fmt.Errorf("hvf: restore registers: %w", err)
	}

	return nil
}

var (
	_ hv.Hypervisor       = (*hypervisor)(nil)
	_ hv.VirtualCPU       = (*virtualCPU)(nil)
	_ hv.VirtualMachine   = (*virtualMachine)(nil)
	_ hv.Arm64GICProvider = (*virtualMachine)(nil)
)

func Open() (hv.Hypervisor, error) {
	if err := ensureInitialized(); err != nil {
		return nil, err
	}
	return &hypervisor{}, nil
}
