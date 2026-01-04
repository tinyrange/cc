//go:build darwin && arm64

package hvf

import (
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"runtime"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/hv/hvf/bindings"
	"github.com/tinyrange/cc/internal/timeslice"
	"golang.org/x/sys/unix"
)

var (
	tsHvfGuestTime       = timeslice.RegisterKind("hvf_guest_time", timeslice.SliceFlagGuestTime)
	tsHvfUnknownHostTime = timeslice.RegisterKind("hvf_host_time", 0)

	tsHvfStartTime = time.Now()
)

var globalVM atomic.Pointer[virtualMachine]

var registerMap = map[hv.Register]bindings.Reg{
	hv.RegisterARM64X0:     bindings.HV_REG_X0,
	hv.RegisterARM64X1:     bindings.HV_REG_X1,
	hv.RegisterARM64X2:     bindings.HV_REG_X2,
	hv.RegisterARM64X3:     bindings.HV_REG_X3,
	hv.RegisterARM64X4:     bindings.HV_REG_X4,
	hv.RegisterARM64X5:     bindings.HV_REG_X5,
	hv.RegisterARM64X6:     bindings.HV_REG_X6,
	hv.RegisterARM64X7:     bindings.HV_REG_X7,
	hv.RegisterARM64X8:     bindings.HV_REG_X8,
	hv.RegisterARM64X9:     bindings.HV_REG_X9,
	hv.RegisterARM64X10:    bindings.HV_REG_X10,
	hv.RegisterARM64X11:    bindings.HV_REG_X11,
	hv.RegisterARM64X12:    bindings.HV_REG_X12,
	hv.RegisterARM64X13:    bindings.HV_REG_X13,
	hv.RegisterARM64X14:    bindings.HV_REG_X14,
	hv.RegisterARM64X15:    bindings.HV_REG_X15,
	hv.RegisterARM64X16:    bindings.HV_REG_X16,
	hv.RegisterARM64X17:    bindings.HV_REG_X17,
	hv.RegisterARM64X18:    bindings.HV_REG_X18,
	hv.RegisterARM64X19:    bindings.HV_REG_X19,
	hv.RegisterARM64X20:    bindings.HV_REG_X20,
	hv.RegisterARM64X21:    bindings.HV_REG_X21,
	hv.RegisterARM64X22:    bindings.HV_REG_X22,
	hv.RegisterARM64X23:    bindings.HV_REG_X23,
	hv.RegisterARM64X24:    bindings.HV_REG_X24,
	hv.RegisterARM64X25:    bindings.HV_REG_X25,
	hv.RegisterARM64X26:    bindings.HV_REG_X26,
	hv.RegisterARM64X27:    bindings.HV_REG_X27,
	hv.RegisterARM64X28:    bindings.HV_REG_X28,
	hv.RegisterARM64X29:    bindings.HV_REG_X29,
	hv.RegisterARM64X30:    bindings.HV_REG_X30,
	hv.RegisterARM64Pc:     bindings.HV_REG_PC,
	hv.RegisterARM64Pstate: bindings.HV_REG_CPSR,
}

var sysRegisterMap = map[hv.Register]bindings.SysReg{
	hv.RegisterARM64Vbar: bindings.HV_SYS_REG_VBAR_EL1,
	hv.RegisterARM64Sp:   bindings.HV_SYS_REG_SP_EL1,
}

type virtualCPU struct {
	vm *virtualMachine

	id   bindings.VCPU
	exit *bindings.VcpuExit

	closed bool

	runQueue chan func()

	initError chan error

	lastTime   time.Time
	regionKind timeslice.TimesliceID
}

// implements [hv.VirtualCPU].
func (v *virtualCPU) ID() int                           { return int(v.id) }
func (v *virtualCPU) VirtualMachine() hv.VirtualMachine { return v.vm }

func (v *virtualCPU) Close() error {
	if v.closed {
		return nil
	}

	v.closed = true

	errChan := make(chan error, 1)
	// submit to the run queue a function that will destroy the vCPU
	v.runQueue <- func() {
		if err := bindings.HvVcpuDestroy(v.id); err != bindings.HV_SUCCESS {
			slog.Error("failed to destroy vCPU", "error", err)
			errChan <- fmt.Errorf("failed to destroy vCPU: %w", err)
			return
		}
		errChan <- nil
	}

	val := <-errChan
	close(errChan)
	return val
}

// GetRegisters implements [hv.VirtualCPU].
func (v *virtualCPU) GetRegisters(regs map[hv.Register]hv.RegisterValue) error {
	for reg := range regs {
		if reg == hv.RegisterARM64Xzr {
			regs[reg] = hv.Register64(0)
			continue
		} else if hvReg, ok := registerMap[reg]; ok {
			var value uint64
			if err := bindings.HvVcpuGetReg(v.id, hvReg, &value); err != bindings.HV_SUCCESS {
				return fmt.Errorf("hvf: failed to get register %v: %w", reg, err)
			}
			regs[reg] = hv.Register64(value)
		} else if hvReg, ok := sysRegisterMap[reg]; ok {
			var value uint64
			if err := bindings.HvVcpuGetSysReg(v.id, hvReg, &value); err != bindings.HV_SUCCESS {
				return fmt.Errorf("hvf: failed to get register %v: %w", reg, err)
			}
			regs[reg] = hv.Register64(value)
		} else {
			return fmt.Errorf("hvf: unsupported register %v", reg)
		}
	}

	return nil
}

// SetRegisters implements [hv.VirtualCPU].
func (v *virtualCPU) SetRegisters(regs map[hv.Register]hv.RegisterValue) error {
	for reg, value := range regs {
		if hvReg, ok := registerMap[reg]; ok {
			if err := bindings.HvVcpuSetReg(v.id, hvReg, uint64(value.(hv.Register64))); err != bindings.HV_SUCCESS {
				return fmt.Errorf("hvf: failed to set register %v: %w", reg, err)
			}
		} else if hvReg, ok := sysRegisterMap[reg]; ok {
			if err := bindings.HvVcpuSetSysReg(v.id, hvReg, uint64(value.(hv.Register64))); err != bindings.HV_SUCCESS {
				return fmt.Errorf("hvf: failed to set register %v: %w", reg, err)
			}
		} else {
			return fmt.Errorf("hvf: unsupported register %v", reg)
		}
	}

	return nil
}

// Run implements [hv.VirtualCPU].
func (v *virtualCPU) Run(ctx context.Context) error {
	var stopExit func() bool
	if ctx.Done() != nil {
		stopExit = context.AfterFunc(ctx, func() {
			exitList := []bindings.VCPU{v.id}
			_ = bindings.HvVcpusExit(&exitList[0], uint32(len(exitList)))
		})
	}
	if stopExit != nil {
		defer stopExit()
	}

	var kind timeslice.TimesliceID
	if v.lastTime.IsZero() {
		kind = timeslice.TimesliceInit
		v.lastTime = tsHvfStartTime
	} else if v.regionKind == timeslice.InvalidTimesliceID {
		kind = tsHvfUnknownHostTime
	}
	timeslice.Record(kind, time.Since(v.lastTime))
	v.lastTime = time.Now()

	if err := bindings.HvVcpuRun(v.id); err != bindings.HV_SUCCESS {
		return fmt.Errorf("hvf: failed to run vCPU %d: %w", v.id, err)
	}

	timeslice.Record(tsHvfGuestTime, time.Since(v.lastTime))
	v.lastTime = time.Now()

	switch v.exit.Reason {
	case bindings.HV_EXIT_REASON_EXCEPTION:
		return v.handleException()
	case bindings.HV_EXIT_REASON_CANCELED:
		return ctx.Err()
	default:
		return fmt.Errorf("hvf: unknown exit reason %s", v.exit.Reason)
	}
}

type exceptionClass uint64

const (
	exceptionClassHvc              exceptionClass = 0x16
	exceptionClassSmc              exceptionClass = 0x17
	exceptionClassMsrAccess        exceptionClass = 0x18
	exceptionClassDataAbortLowerEL exceptionClass = 0x24
)

func (ec exceptionClass) String() string {
	switch ec {
	case exceptionClassHvc:
		return "HVC"
	case exceptionClassSmc:
		return "SMC"
	case exceptionClassMsrAccess:
		return "MSR access"
	case exceptionClassDataAbortLowerEL:
		return "Data abort lower EL"
	default:
		return fmt.Sprintf("unknown exception class %d", ec)
	}
}

const (
	exceptionClassMask  = 0x3F
	exceptionClassShift = 26
)

func (v *virtualCPU) handleException() error {
	syndrome := v.exit.Exception.Syndrome
	ec := exceptionClass((syndrome >> exceptionClassShift) & exceptionClassMask)

	switch ec {
	case exceptionClassHvc:
		return v.handleHvc()
	case exceptionClassDataAbortLowerEL:
		return v.handleDataAbort(syndrome, v.exit.Exception.PhysicalAddress)
	case exceptionClassMsrAccess:
		return v.handleMsrAccess(syndrome)
	default:
		return fmt.Errorf("hvf: unsupported exception class %s (syndrome=0x%x)", ec, syndrome)
	}
}

type psciFunctionID uint32

// PSCI function IDs (SMC32 calling convention)
const (
	psciVersion         psciFunctionID = 0x84000000
	psciCpuSuspend      psciFunctionID = 0x84000001
	psciCpuOff          psciFunctionID = 0x84000002
	psciCpuOn           psciFunctionID = 0x84000003
	psciAffinityInfo    psciFunctionID = 0x84000004
	psciMigrateInfoType psciFunctionID = 0x84000006
	psciSystemOff       psciFunctionID = 0x84000008
	psciSystemReset     psciFunctionID = 0x84000009
	psciFeatures        psciFunctionID = 0x8400000A

	// PSCI return values
	psciSuccess           psciFunctionID = 0
	psciNotSupported      psciFunctionID = 0xFFFFFFFF // -1 as uint32
	psciInvalidParameters psciFunctionID = 0xFFFFFFFE // -2 as uint32
	psciDenied            psciFunctionID = 0xFFFFFFFD // -3 as uint32
	psciAlreadyOn         psciFunctionID = 0xFFFFFFFC // -4 as uint32
	psciOnPending         psciFunctionID = 0xFFFFFFFB // -5 as uint32
	psciInternalFailure   psciFunctionID = 0xFFFFFFFA // -6 as uint32
	psciNotPresent        psciFunctionID = 0xFFFFFFF9 // -7 as uint32
	psciDisabled          psciFunctionID = 0xFFFFFFF8 // -8 as uint32
	psciInvalidAddress    psciFunctionID = 0xFFFFFFF7 // -9 as uint32
	psciTosNotPresent     psciFunctionID = 2          // For MIGRATE_INFO_TYPE: no trusted OS
)

func (fid psciFunctionID) String() string {
	switch fid {
	case psciVersion:
		return "PSCI_VERSION"
	case psciCpuSuspend:
		return "PSCI_CPU_SUSPEND"
	case psciCpuOff:
		return "PSCI_CPU_OFF"
	case psciCpuOn:
		return "PSCI_CPU_ON"
	case psciAffinityInfo:
		return "PSCI_AFFINITY_INFO"
	case psciMigrateInfoType:
		return "PSCI_MIGRATE_INFO_TYPE"
	case psciSystemOff:
		return "PSCI_SYSTEM_OFF"
	case psciSystemReset:
		return "PSCI_SYSTEM_RESET"
	case psciFeatures:
		return "PSCI_FEATURES"
	case psciSuccess:
		return "PSCI_SUCCESS"
	case psciNotSupported:
		return "PSCI_NOT_SUPPORTED"
	case psciInvalidParameters:
		return "PSCI_INVALID_PARAMETERS"
	case psciDenied:
		return "PSCI_DENIED"
	case psciAlreadyOn:
		return "PSCI_ALREADY_ON"
	case psciOnPending:
		return "PSCI_ON_PENDING"
	case psciInternalFailure:
		return "PSCI_INTERNAL_FAILURE"
	case psciNotPresent:
		return "PSCI_NOT_PRESENT"
	case psciDisabled:
		return "PSCI_DISABLED"
	case psciInvalidAddress:
		return "PSCI_INVALID_ADDRESS"
	case psciTosNotPresent:
		return "PSCI_TOS_NOT_PRESENT"
	default:
		return fmt.Sprintf("PSCI_FUNCTION_ID_%d", fid)
	}
}

func (v *virtualCPU) handleHvc() error {
	// get the value of x0
	var x0 uint64
	if err := bindings.HvVcpuGetReg(v.id, bindings.HV_REG_X0, &x0); err != bindings.HV_SUCCESS {
		return fmt.Errorf("hvf: failed to get x0: %w", err)
	}

	fid := psciFunctionID(x0)

	switch fid {
	case psciSystemOff:
		return hv.ErrVMHalted
	case psciSystemReset:
		return hv.ErrGuestRequestedReboot
	case psciVersion:
		if err := bindings.HvVcpuSetReg(v.id, bindings.HV_REG_X0, 0x00010000); err != bindings.HV_SUCCESS {
			return fmt.Errorf("hvf: failed to get x0: %w", err)
		}

		return nil
	case psciMigrateInfoType:
		// report no trusted OS
		if err := bindings.HvVcpuSetReg(v.id, bindings.HV_REG_X0, 2); err != bindings.HV_SUCCESS {
			return fmt.Errorf("hvf: failed to get x0: %w", err)
		}

		return nil
	case psciFeatures:
		// report not supported
		if err := bindings.HvVcpuSetReg(v.id, bindings.HV_REG_X0, 0xffff_ffff); err != bindings.HV_SUCCESS {
			return fmt.Errorf("hvf: failed to get x0: %w", err)
		}

		return nil
	default:
		return fmt.Errorf("hvf: HVC %s not implemented", fid)
	}
}

type dataAbortInfo struct {
	sizeBytes int
	write     bool
	target    hv.Register
}

func arm64RegisterFromIndex(idx int) (hv.Register, bool) {
	switch {
	case idx >= 0 && idx <= 30:
		return hv.Register(int(hv.RegisterARM64X0) + idx), true
	case idx == 31:
		// In data abort syndrome, register 31 is XZR (zero register), not SP
		return hv.RegisterARM64Xzr, true
	default:
		return hv.RegisterInvalid, false
	}
}

func decodeDataAbort(syndrome bindings.ExceptionSyndrome) (dataAbortInfo, error) {
	const (
		dataAbortISSMask uint64 = (1 << 25) - 1
		isvBit                  = 24
		sasShift                = 22
		sasMask          uint64 = 0x3
		srtShift                = 16
		srtMask          uint64 = 0x1F
		wnrBit                  = 6
	)

	iss := uint64(syndrome) & dataAbortISSMask
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

func (v *virtualCPU) handleDataAbort(syndrome bindings.ExceptionSyndrome, physAddr bindings.IPA) error {
	decoded, err := decodeDataAbort(syndrome)
	if err != nil {
		return fmt.Errorf("hvf: failed to decode data abort syndrome 0x%X: %w", syndrome, err)
	}

	var addr uint64 = uint64(physAddr)

	dev, err := v.findMMIODevice(addr, uint64(decoded.sizeBytes))
	if err != nil {
		return err
	}

	var pendingError error

	// TODO(joshua): Remove allocation here
	data := make([]byte, decoded.sizeBytes)
	if decoded.write {
		reg := map[hv.Register]hv.RegisterValue{
			decoded.target: hv.Register64(0),
		}
		if err := v.GetRegisters(reg); err != nil {
			return fmt.Errorf("hvf: failed to get register: %w", err)
		}
		value := uint64(reg[decoded.target].(hv.Register64))

		// convert the value to a byte slice
		var tmp [8]byte
		binary.LittleEndian.PutUint64(tmp[:], value)
		copy(data, tmp[:])

		if err := dev.WriteMMIO(addr, data); err != nil {
			pendingError = fmt.Errorf("hvf: failed to write MMIO: %w", err)
		}
	} else {
		if err := dev.ReadMMIO(addr, data); err != nil {
			pendingError = fmt.Errorf("hvf: failed to read MMIO: %w", err)
		}

		var tmp [8]byte
		copy(tmp[:], data)
		value := binary.LittleEndian.Uint64(tmp[:])
		reg := map[hv.Register]hv.RegisterValue{
			decoded.target: hv.Register64(value),
		}
		if err := v.SetRegisters(reg); err != nil {
			return fmt.Errorf("hvf: failed to set register: %w", err)
		}
	}

	if err := v.advanceProgramCounter(); err != nil {
		return fmt.Errorf("hvf: advance PC after MMIO access: %w", err)
	}

	return pendingError
}

type msrAccessInfo struct {
	op0, op1, op2 uint8
	crn, crm      uint8
	read          bool // true = MRS (sysreg -> Rt), false = MSR (Rt -> sysreg)
	target        hv.Register
}

func decodeMsrAccess(syndrome bindings.ExceptionSyndrome) (msrAccessInfo, error) {
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

	iss := uint64(syndrome) & issMask

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

func (v *virtualCPU) handleMsrAccess(syndrome bindings.ExceptionSyndrome) error {
	info, err := decodeMsrAccess(syndrome)
	if err != nil {
		return err
	}

	slog.Debug("ignoring MSR access", "info", info)

	if info.read {
		// write 0 to the target register
		if err := v.SetRegisters(map[hv.Register]hv.RegisterValue{
			info.target: hv.Register64(0),
		}); err != nil {
			return fmt.Errorf("hvf: failed to set register: %w", err)
		}
	} else {

	}

	return v.advanceProgramCounter()
}

const arm64InstructionSizeBytes = 4

func (v *virtualCPU) advanceProgramCounter() error {
	var pc uint64
	if err := bindings.HvVcpuGetReg(v.id, bindings.HV_REG_PC, &pc); err != bindings.HV_SUCCESS {
		return fmt.Errorf("hvf: failed to get PC: %w", err)
	}
	if err := bindings.HvVcpuSetReg(v.id, bindings.HV_REG_PC, pc+arm64InstructionSizeBytes); err != bindings.HV_SUCCESS {
		return fmt.Errorf("hvf: failed to set PC: %w", err)
	}
	return nil
}

func (v *virtualCPU) start() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	cfg := bindings.HvVcpuConfigCreate()

	var id bindings.VCPU
	var exit *bindings.VcpuExit = new(bindings.VcpuExit)

	if err := bindings.HvVcpuCreate(&id, &exit, cfg); err != bindings.HV_SUCCESS {
		v.initError <- err
		return
	}

	// Set the MPIDR_EL1 to the vCPU ID.
	// Okay setting this is required to get the GICv3 to work properly.
	if err := bindings.HvVcpuSetSysReg(id, bindings.HV_SYS_REG_MPIDR_EL1, uint64(id)); err != bindings.HV_SUCCESS {
		v.initError <- fmt.Errorf("failed to set MPIDR_EL1: %w", err)
		return
	}

	v.id = id
	v.exit = exit

	v.initError <- nil

	for fn := range v.runQueue {
		fn()
	}
}

var (
	_ hv.VirtualCPU = &virtualCPU{}
)

type memoryRegion struct {
	memory []byte
}

func (m *memoryRegion) Size() uint64 {
	return uint64(len(m.memory))
}

func (m *memoryRegion) ReadAt(p []byte, off int64) (n int, err error) {
	if off < 0 || int(off) >= len(m.memory) {
		return 0, fmt.Errorf("hvf: ReadAt offset out of bounds")
	}

	n = copy(p, m.memory[off:])
	if n < len(p) {
		err = fmt.Errorf("hvf: ReadAt short read")
	}
	return n, err
}

func (m *memoryRegion) WriteAt(p []byte, off int64) (n int, err error) {
	if off < 0 || int(off) >= len(m.memory) {
		return 0, fmt.Errorf("hvf: memoryRegion WriteAt offset out of bounds: %d, %d", off, len(m.memory))
	}

	n = copy(m.memory[off:], p)
	if n < len(p) {
		err = fmt.Errorf("hvf: WriteAt short write")
	}
	return n, err
}

var (
	_ hv.MemoryRegion = &memoryRegion{}
)

type virtualMachine struct {
	hv         *hypervisor
	memory     []byte
	memoryBase uint64

	runQueue chan func()

	cpus map[int]*virtualCPU

	devices []hv.Device

	closed bool

	gicInfo hv.Arm64GICInfo
}

// implements hv.VirtualMachine
func (v *virtualMachine) Hypervisor() hv.Hypervisor { return v.hv }
func (v *virtualMachine) MemoryBase() uint64        { return v.memoryBase }
func (v *virtualMachine) MemorySize() uint64        { return uint64(len(v.memory)) }

// CaptureSnapshot implements [hv.VirtualMachine].
func (v *virtualMachine) CaptureSnapshot() (hv.Snapshot, error) {
	return nil, fmt.Errorf("hvf: snapshot not implemented")
}

// RestoreSnapshot implements [hv.VirtualMachine].
func (v *virtualMachine) RestoreSnapshot(snap hv.Snapshot) error {
	return fmt.Errorf("hvf: snapshot not implemented")
}

// AddDevice implements [hv.VirtualMachine].
func (v *virtualMachine) AddDevice(dev hv.Device) error {
	v.devices = append(v.devices, dev)

	return dev.Init(v)
}

// SetIRQ implements [hv.VirtualMachine].
func (v *virtualMachine) SetIRQ(irqLine uint32, level bool) error {
	const (
		armIRQTypeShift = 24
		armIRQTypeSPI   = 1
		armSPIBase      = 32 // GIC SPIs start at INTID 32
	)

	// Validate IRQ type encoding
	irqType := (irqLine >> armIRQTypeShift) & 0xff
	if irqType == 0 {
		return fmt.Errorf("hvf: interrupt type missing in irqLine %#x", irqLine)
	}
	if irqType != armIRQTypeSPI {
		return fmt.Errorf("hvf: unsupported IRQ type %d in irqLine %#x", irqType, irqLine)
	}

	// Extract SPI number from irqLine encoding and convert to GIC intid
	// SPIs start at intid 32 in GICv3
	spiOffset := irqLine & 0xFFFF
	intid := spiOffset + armSPIBase

	if err := bindings.HvGicSetSpi(intid, level); err != bindings.HV_SUCCESS {
		return fmt.Errorf("hvf: failed to set SPI (intid=%d): %w", intid, err)
	}

	return nil
}

// Close implements [hv.VirtualMachine].
func (v *virtualMachine) Close() error {
	if v.closed {
		return nil
	}

	v.closed = true

	if v.memory != nil {
		// unmap the memory from the VM
		if err := bindings.HvVmUnmap(bindings.IPA(v.memoryBase), uintptr(len(v.memory))); err != bindings.HV_SUCCESS {
			slog.Error("failed to unmap memory from VM", "error", err)
			return fmt.Errorf("failed to unmap memory from VM: %w", err)
		}

		// unmap the memory
		if err := unix.Munmap(v.memory); err != nil {
			return fmt.Errorf("failed to unmap memory: %w", err)
		}
		v.memory = nil
	}

	for _, cpu := range v.cpus {
		if err := cpu.Close(); err != nil {
			slog.Error("failed to close vCPU", "error", err)
			return fmt.Errorf("failed to close vCPU: %w", err)
		}
	}

	// destroy the VM
	if err := bindings.HvVmDestroy(); err != bindings.HV_SUCCESS {
		slog.Error("failed to destroy VM", "error", err)
		return fmt.Errorf("failed to destroy VM: %w", err)
	}

	// reset the global VM pointer
	globalVM.Store(nil)

	return nil
}

// AddDeviceFromTemplate implements [hv.VirtualMachine].
func (v *virtualMachine) AddDeviceFromTemplate(template hv.DeviceTemplate) error {
	dev, err := template.Create(v)
	if err != nil {
		return fmt.Errorf("failed to create device from template: %w", err)
	}

	return v.AddDevice(dev)
}

// AllocateMemory implements [hv.VirtualMachine].
func (v *virtualMachine) AllocateMemory(physAddr uint64, size uint64) (hv.MemoryRegion, error) {
	mem, err := unix.Mmap(
		-1,
		0,
		int(size),
		unix.PROT_READ|unix.PROT_WRITE,
		unix.MAP_ANONYMOUS|unix.MAP_PRIVATE,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to allocate memory for VM: %w", err)
	}

	if err := bindings.HvVmMap(
		unsafe.Pointer(&mem[0]),
		bindings.IPA(physAddr),
		uintptr(size),
		bindings.HV_MEMORY_READ|bindings.HV_MEMORY_WRITE|bindings.HV_MEMORY_EXEC,
	); err != bindings.HV_SUCCESS {
		return nil, fmt.Errorf("failed to map memory for VM at 0x%X,0x%X: %w", physAddr, size, err)
	}

	return &memoryRegion{
		memory: mem,
	}, nil
}

// ReadAt implements [hv.VirtualMachine].
func (v *virtualMachine) ReadAt(p []byte, off int64) (n int, err error) {
	offset := off - int64(v.memoryBase)
	if offset < 0 || uint64(offset) >= uint64(len(v.memory)) {
		return 0, fmt.Errorf("hvf: ReadAt offset out of bounds")
	}

	n = copy(p, v.memory[offset:])
	if n < len(p) {
		err = fmt.Errorf("hvf: ReadAt short read")
	}
	return n, err
}

// WriteAt implements [hv.VirtualMachine].
func (v *virtualMachine) WriteAt(p []byte, off int64) (n int, err error) {
	offset := off - int64(v.memoryBase)
	if offset < 0 || uint64(offset) >= uint64(len(v.memory)) {
		return 0, fmt.Errorf("hvf: virtualMachine WriteAt offset out of bounds: 0x%x, 0x%x", off, len(v.memory))
	}

	n = copy(v.memory[offset:], p)
	if n < len(p) {
		err = fmt.Errorf("hvf: WriteAt short write")
	}
	return n, err
}

// Run implements [hv.VirtualMachine].
func (v *virtualMachine) Run(ctx context.Context, cfg hv.RunConfig) error {
	if cfg == nil {
		return fmt.Errorf("hvf: RunConfig cannot be nil")
	}

	return v.VirtualCPUCall(0, func(vcpu hv.VirtualCPU) error {
		return cfg.Run(ctx, vcpu)
	})
}

// VirtualCPUCall implements [hv.VirtualMachine].
func (v *virtualMachine) VirtualCPUCall(id int, f func(vcpu hv.VirtualCPU) error) error {
	vcpu, ok := v.cpus[id]
	if !ok {
		return fmt.Errorf("hvf: no vCPU %d found", id)
	}

	done := make(chan error, 1)

	vcpu.runQueue <- func() {
		done <- f(vcpu)
	}

	for {
		select {
		case err := <-done:
			return err
		case f := <-v.runQueue:
			f()
		}
	}
}

func (v *virtualMachine) callMainThread(f func() error) error {
	done := make(chan error, 1)

	v.runQueue <- func() {
		done <- f()
	}
	return <-done
}

// Arm64GICInfo implements [hv.Arm64GICProvider].
func (v *virtualMachine) Arm64GICInfo() (hv.Arm64GICInfo, bool) {
	return v.gicInfo, v.gicInfo.Version != hv.Arm64GICVersionUnknown
}

var (
	_ hv.VirtualMachine   = &virtualMachine{}
	_ hv.Arm64GICProvider = &virtualMachine{}
)

type hypervisor struct {
}

// Architecture implements [hv.Hypervisor].
func (h *hypervisor) Architecture() hv.CpuArchitecture {
	return hv.ArchitectureARM64
}

// Close implements [hv.Hypervisor].
func (h *hypervisor) Close() error {
	if vm := globalVM.Load(); vm != nil {
		if err := vm.Close(); err != nil {
			return fmt.Errorf("failed to close VM: %w", err)
		}
	}

	return nil
}

// NewVirtualMachine implements [hv.Hypervisor].
func (h *hypervisor) NewVirtualMachine(config hv.VMConfig) (hv.VirtualMachine, error) {
	if vm := globalVM.Load(); vm != nil {
		return nil, fmt.Errorf("VM already exists, hvf is limited to a single VM per process")
	}

	ret := &virtualMachine{
		hv:       h,
		cpus:     make(map[int]*virtualCPU),
		runQueue: make(chan func(), 16),
	}

	vmConfig := bindings.HvVmConfigCreate()

	vm := bindings.HvVmCreate(vmConfig)
	if vm != bindings.HV_SUCCESS {
		return nil, fmt.Errorf("failed to create VM: %d", vm)
	}

	// Only one VM can be created at a time for a single process.
	if swapped := globalVM.CompareAndSwap(nil, ret); !swapped {
		return nil, fmt.Errorf("global VM already exists")
	}

	// The VM is now created without memory.
	if err := config.Callbacks().OnCreateVM(ret); err != nil {
		return nil, fmt.Errorf("failed to create VM: %w", err)
	}

	// allocate memory for the VM
	mem, err := ret.AllocateMemory(config.MemoryBase(), config.MemorySize())
	if err != nil {
		return nil, fmt.Errorf("failed to allocate memory for VM: %w", err)
	}

	ret.memory = mem.(*memoryRegion).memory
	ret.memoryBase = config.MemoryBase()

	// create GICv3 device if needed
	if config.NeedsInterruptSupport() {
		var (
			distributorSize            uintptr
			distributorBaseAlignment   uintptr
			redistributorRegionSize    uintptr
			redistributorSize          uintptr
			redistributorBaseAlignment uintptr
			msiRegionSize              uintptr
			msiRegionBaseAlignment     uintptr
			spiIntidBase               uint32
			spiIntidCount              uint32
		)

		if err := bindings.HvGicGetDistributorSize(&distributorSize); err != bindings.HV_SUCCESS {
			return nil, fmt.Errorf("failed to get GIC distributor size: %s", err)
		}
		if err := bindings.HvGicGetDistributorBaseAlignment(&distributorBaseAlignment); err != bindings.HV_SUCCESS {
			return nil, fmt.Errorf("failed to get GIC distributor base alignment: %s", err)
		}
		if err := bindings.HvGicGetRedistributorRegionSize(&redistributorRegionSize); err != bindings.HV_SUCCESS {
			return nil, fmt.Errorf("failed to get GIC redistributor region size: %s", err)
		}
		if err := bindings.HvGicGetRedistributorSize(&redistributorSize); err != bindings.HV_SUCCESS {
			return nil, fmt.Errorf("failed to get GIC redistributor size: %s", err)
		}
		if err := bindings.HvGicGetRedistributorBaseAlignment(&redistributorBaseAlignment); err != bindings.HV_SUCCESS {
			return nil, fmt.Errorf("failed to get GIC redistributor base alignment: %s", err)
		}
		if err := bindings.HvGicGetMsiRegionSize(&msiRegionSize); err != bindings.HV_SUCCESS {
			return nil, fmt.Errorf("failed to get GIC msi region size: %s", err)
		}
		if err := bindings.HvGicGetMsiRegionBaseAlignment(&msiRegionBaseAlignment); err != bindings.HV_SUCCESS {
			return nil, fmt.Errorf("failed to get GIC msi region base alignment: %s", err)
		}
		if err := bindings.HvGicGetSpiInterruptRange(&spiIntidBase, &spiIntidCount); err != bindings.HV_SUCCESS {
			return nil, fmt.Errorf("failed to get GIC spi interrupt range: %s", err)
		}

		cfg := bindings.HvGicConfigCreate()

		var distributorBase bindings.IPA = 0x08000000
		var redistributorBase bindings.IPA = 0x080a0000

		// check alignment
		if uintptr(distributorBase)%uintptr(distributorBaseAlignment) != 0 {
			return nil, fmt.Errorf("failed to set GIC distributor base: %#x is not aligned to %#x", distributorBase, distributorBaseAlignment)
		}
		if uintptr(redistributorBase)%uintptr(redistributorBaseAlignment) != 0 {
			return nil, fmt.Errorf("failed to set GIC redistributor base: %#x is not aligned to %#x", redistributorBase, redistributorBaseAlignment)
		}

		if err := bindings.HvGicConfigSetDistributorBase(cfg, distributorBase); err != bindings.HV_SUCCESS {
			return nil, fmt.Errorf("failed to set GIC distributor base: %s", err)
		}
		if err := bindings.HvGicConfigSetRedistributorBase(cfg, redistributorBase); err != bindings.HV_SUCCESS {
			return nil, fmt.Errorf("failed to set GIC redistributor base: %s", err)
		}

		ret.gicInfo = hv.Arm64GICInfo{
			Version:              hv.Arm64GICVersion3,
			DistributorBase:      uint64(distributorBase),
			DistributorSize:      uint64(distributorSize),
			RedistributorBase:    uint64(redistributorBase),
			RedistributorSize:    uint64(redistributorSize),
			MaintenanceInterrupt: hv.Arm64Interrupt{Type: 1, Num: 9, Flags: 0xF04},
		}

		if err := bindings.HvGicCreate(cfg); err != bindings.HV_SUCCESS {
			return nil, fmt.Errorf("failed to create GICv3: %s", err)
		}
	}

	// Call the callback to allow the user to perform any additional initialization.
	if err := config.Callbacks().OnCreateVMWithMemory(ret); err != nil {
		return nil, fmt.Errorf("failed to create VM with memory: %w", err)
	}

	// create vCPUs
	if config.CPUCount() != 1 {
		return nil, fmt.Errorf("hvf: only 1 vCPU supported, got %d", config.CPUCount())
	}

	for i := 0; i < config.CPUCount(); i++ {
		vcpu := &virtualCPU{
			vm:        ret,
			runQueue:  make(chan func(), 16),
			initError: make(chan error, 1),
		}

		ret.cpus[i] = vcpu

		go vcpu.start()

		err := <-vcpu.initError
		close(vcpu.initError)
		if err != nil {
			return nil, fmt.Errorf("failed to create vCPU %d: %w", i, err)
		}

		if err := config.Callbacks().OnCreateVCPU(vcpu); err != nil {
			return nil, fmt.Errorf("failed to create vCPU %d: %w", i, err)
		}
	}

	if err := config.Loader().Load(ret); err != nil {
		return nil, fmt.Errorf("failed to load VM: %w", err)
	}

	return ret, nil
}

var (
	_ hv.Hypervisor = &hypervisor{}
)

func Open() (hv.Hypervisor, error) {
	if err := bindings.Load(); err != nil {
		return nil, fmt.Errorf("failed to load Hypervisor.framework: %w", err)
	}

	return &hypervisor{}, nil
}
