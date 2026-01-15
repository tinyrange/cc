//go:build darwin && arm64

package hvf

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/hv/hvf/bindings"
	"github.com/tinyrange/cc/internal/timeslice"
	"golang.org/x/sys/unix"
)

var (
	tsHvfGuestTime     = timeslice.RegisterKind("hvf_guest_time", timeslice.SliceFlagGuestTime)
	tsHvfHostTime      = timeslice.RegisterKind("hvf_host_time", 0)
	tsHvfFirstRunStart = timeslice.RegisterKind("hvf_first_run_start", 0)

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

// GP registers to capture for snapshots (X0-X30, PC, CPSR, FPCR, FPSR)
var hvfGPRegsToCapture = []bindings.Reg{
	bindings.HV_REG_X0, bindings.HV_REG_X1, bindings.HV_REG_X2, bindings.HV_REG_X3,
	bindings.HV_REG_X4, bindings.HV_REG_X5, bindings.HV_REG_X6, bindings.HV_REG_X7,
	bindings.HV_REG_X8, bindings.HV_REG_X9, bindings.HV_REG_X10, bindings.HV_REG_X11,
	bindings.HV_REG_X12, bindings.HV_REG_X13, bindings.HV_REG_X14, bindings.HV_REG_X15,
	bindings.HV_REG_X16, bindings.HV_REG_X17, bindings.HV_REG_X18, bindings.HV_REG_X19,
	bindings.HV_REG_X20, bindings.HV_REG_X21, bindings.HV_REG_X22, bindings.HV_REG_X23,
	bindings.HV_REG_X24, bindings.HV_REG_X25, bindings.HV_REG_X26, bindings.HV_REG_X27,
	bindings.HV_REG_X28, bindings.HV_REG_X29, bindings.HV_REG_X30,
	bindings.HV_REG_PC, bindings.HV_REG_CPSR, bindings.HV_REG_FPCR, bindings.HV_REG_FPSR,
}

// System registers to capture for snapshots
var hvfSysRegsToCapture = []bindings.SysReg{
	// Memory management
	bindings.HV_SYS_REG_SCTLR_EL1,
	bindings.HV_SYS_REG_TCR_EL1,
	bindings.HV_SYS_REG_TTBR0_EL1,
	bindings.HV_SYS_REG_TTBR1_EL1,
	bindings.HV_SYS_REG_MAIR_EL1,
	// Exception handling
	bindings.HV_SYS_REG_VBAR_EL1,
	bindings.HV_SYS_REG_ELR_EL1,
	bindings.HV_SYS_REG_SPSR_EL1,
	bindings.HV_SYS_REG_ESR_EL1,
	bindings.HV_SYS_REG_FAR_EL1,
	// Stack pointers
	bindings.HV_SYS_REG_SP_EL0,
	bindings.HV_SYS_REG_SP_EL1,
	// Timers (virtual timer only - physical timer is not accessible via HVF)
	bindings.HV_SYS_REG_CNTKCTL_EL1,
	bindings.HV_SYS_REG_CNTV_CTL_EL0,
	bindings.HV_SYS_REG_CNTV_CVAL_EL0,
	// Misc
	bindings.HV_SYS_REG_CPACR_EL1,
	bindings.HV_SYS_REG_CONTEXTIDR_EL1,
	bindings.HV_SYS_REG_TPIDR_EL0,
	bindings.HV_SYS_REG_TPIDR_EL1,
	bindings.HV_SYS_REG_TPIDRRO_EL0,
	bindings.HV_SYS_REG_PAR_EL1,
	bindings.HV_SYS_REG_AFSR0_EL1,
	bindings.HV_SYS_REG_AFSR1_EL1,
	bindings.HV_SYS_REG_AMAIR_EL1,
}

// GIC ICC (CPU interface) registers to capture for snapshots.
// These control interrupt processing at the CPU level and are per-vCPU.
var hvfICCRegsToCapture = []bindings.GICICCReg{
	bindings.HV_GIC_ICC_REG_PMR_EL1,     // Priority Mask Register
	bindings.HV_GIC_ICC_REG_BPR0_EL1,    // Binary Point Register 0
	bindings.HV_GIC_ICC_REG_AP0R0_EL1,   // Active Priority Register 0
	bindings.HV_GIC_ICC_REG_AP1R0_EL1,   // Active Priority Register 1
	bindings.HV_GIC_ICC_REG_BPR1_EL1,    // Binary Point Register 1
	bindings.HV_GIC_ICC_REG_CTLR_EL1,    // Control Register
	bindings.HV_GIC_ICC_REG_SRE_EL1,     // System Register Enable
	bindings.HV_GIC_ICC_REG_IGRPEN0_EL1, // Interrupt Group Enable 0
	bindings.HV_GIC_ICC_REG_IGRPEN1_EL1, // Interrupt Group Enable 1
}

// arm64HvfVcpuSnapshot holds the vCPU state for ARM64 on HVF
type arm64HvfVcpuSnapshot struct {
	GPRegisters     map[bindings.Reg]uint64
	SysRegisters    map[bindings.SysReg]uint64
	ICCRegisters    map[bindings.GICICCReg]uint64 // GIC CPU interface registers
	SimdFPRegisters map[bindings.SIMDReg]bindings.SimdFP
	VTimerOffset    uint64
}

// arm64HvfSnapshot holds the complete VM snapshot for HVF ARM64
type arm64HvfSnapshot struct {
	cpuStates       map[int]arm64HvfVcpuSnapshot
	deviceSnapshots map[string]interface{}
	memory          []byte
	gicState        []byte
}

type exitContext struct {
	kind timeslice.TimesliceID
}

func (ctx *exitContext) SetExitTimeslice(id timeslice.TimesliceID) {
	ctx.kind = id
}

// vcpuState represents the power state of a vCPU for SMP support.
type vcpuState int

const (
	vcpuStateParked  vcpuState = iota // Waiting for PSCI CPU_ON
	vcpuStateRunning                  // Actively executing
	vcpuStateOff                      // Powered off via PSCI CPU_OFF
	vcpuStatePaused                   // Running but paused between RunAll calls
)

// vcpuWakeup contains the parameters for booting a secondary vCPU via PSCI CPU_ON.
type vcpuWakeup struct {
	entryPoint uint64 // Entry point address (from PSCI CPU_ON x2)
	contextID  uint64 // Context ID passed to entry point (from PSCI CPU_ON x3)
}

type virtualCPU struct {
	vm *virtualMachine

	rec *timeslice.Recorder

	id   bindings.VCPU
	exit *bindings.VcpuExit

	closed bool

	runQueue chan func()

	initError chan error

	// SMP support: vCPU state tracking
	state    vcpuState
	stateMu  sync.Mutex
	wakeupCh chan vcpuWakeup // Channel to receive CPU_ON wakeup signal
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

// captureSnapshot captures the vCPU state for a snapshot.
// Must be called from the vCPU's thread (via runQueue).
func (v *virtualCPU) captureSnapshot() (arm64HvfVcpuSnapshot, error) {
	ret := arm64HvfVcpuSnapshot{
		GPRegisters:     make(map[bindings.Reg]uint64, len(hvfGPRegsToCapture)),
		SysRegisters:    make(map[bindings.SysReg]uint64, len(hvfSysRegsToCapture)),
		ICCRegisters:    make(map[bindings.GICICCReg]uint64, len(hvfICCRegsToCapture)),
		SimdFPRegisters: make(map[bindings.SIMDReg]bindings.SimdFP, 32),
	}

	// Capture GP registers (X0-X30, PC, CPSR, FPCR, FPSR)
	for _, reg := range hvfGPRegsToCapture {
		var value uint64
		if err := bindings.HvVcpuGetReg(v.id, reg, &value); err != bindings.HV_SUCCESS {
			return ret, fmt.Errorf("hvf: capture GP reg %d: %w", reg, err)
		}
		ret.GPRegisters[reg] = value
	}

	// Capture system registers
	for _, reg := range hvfSysRegsToCapture {
		var value uint64
		if err := bindings.HvVcpuGetSysReg(v.id, reg, &value); err != bindings.HV_SUCCESS {
			return ret, fmt.Errorf("hvf: capture sys reg 0x%x: %w", reg, err)
		}
		ret.SysRegisters[reg] = value
	}

	// Capture GIC ICC (CPU interface) registers if GIC is configured
	if v.vm.gicInfo.Version != hv.Arm64GICVersionUnknown {
		for _, reg := range hvfICCRegsToCapture {
			var value uint64
			if err := bindings.HvGicGetIccReg(v.id, reg, &value); err != bindings.HV_SUCCESS {
				return ret, fmt.Errorf("hvf: capture ICC reg 0x%x: %w", reg, err)
			}
			ret.ICCRegisters[reg] = value
		}
	}

	// Capture SIMD/FP registers (Q0-Q31)
	for i := bindings.SIMDReg(0); i < 32; i++ {
		var value bindings.SimdFP
		if err := bindings.HvVcpuGetSimdFpReg(v.id, i, &value); err != bindings.HV_SUCCESS {
			return ret, fmt.Errorf("hvf: capture SIMD reg Q%d: %w", i, err)
		}
		ret.SimdFPRegisters[i] = value
	}

	// Capture virtual timer offset
	if err := bindings.HvVcpuGetVtimerOffset(v.id, &ret.VTimerOffset); err != bindings.HV_SUCCESS {
		return ret, fmt.Errorf("hvf: capture vtimer offset: %w", err)
	}

	return ret, nil
}

// restoreSnapshot restores the vCPU state from a snapshot.
// Must be called from the vCPU's thread (via runQueue).
func (v *virtualCPU) restoreSnapshot(snap arm64HvfVcpuSnapshot) error {
	// Restore virtual timer offset first
	if err := bindings.HvVcpuSetVtimerOffset(v.id, snap.VTimerOffset); err != bindings.HV_SUCCESS {
		return fmt.Errorf("hvf: restore vtimer offset: %w", err)
	}

	// Restore SIMD/FP registers (Q0-Q31)
	for reg, value := range snap.SimdFPRegisters {
		if err := bindings.HvVcpuSetSimdFpReg(v.id, reg, value); err != bindings.HV_SUCCESS {
			return fmt.Errorf("hvf: restore SIMD reg Q%d: %w", reg, err)
		}
	}

	// Restore system registers
	for reg, value := range snap.SysRegisters {
		if err := bindings.HvVcpuSetSysReg(v.id, reg, value); err != bindings.HV_SUCCESS {
			return fmt.Errorf("hvf: restore sys reg 0x%x: %w", reg, err)
		}
	}

	// Restore GIC ICC (CPU interface) registers if GIC is configured
	if v.vm.gicInfo.Version != hv.Arm64GICVersionUnknown && len(snap.ICCRegisters) > 0 {
		for reg, value := range snap.ICCRegisters {
			if err := bindings.HvGicSetIccReg(v.id, reg, value); err != bindings.HV_SUCCESS {
				return fmt.Errorf("hvf: restore ICC reg 0x%x: %w", reg, err)
			}
		}
	}

	// Restore GP registers last
	for reg, value := range snap.GPRegisters {
		if err := bindings.HvVcpuSetReg(v.id, reg, value); err != bindings.HV_SUCCESS {
			return fmt.Errorf("hvf: restore GP reg %d: %w", reg, err)
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

	v.rec.Record(tsHvfHostTime)

	if err := bindings.HvVcpuRun(v.id); err != bindings.HV_SUCCESS {
		return fmt.Errorf("hvf: failed to run vCPU %d: %w", v.id, err)
	}

	v.rec.Record(tsHvfGuestTime)

	exitCtx := &exitContext{
		kind: timeslice.InvalidTimesliceID,
	}

	switch v.exit.Reason {
	case bindings.HV_EXIT_REASON_EXCEPTION:
		if err := v.handleException(exitCtx); err != nil {
			return err
		}
	case bindings.HV_EXIT_REASON_CANCELED:
		return ctx.Err()
	default:
		return fmt.Errorf("hvf: unknown exit reason %s", v.exit.Reason)
	}

	if exitCtx.kind != timeslice.InvalidTimesliceID {
		v.rec.Record(exitCtx.kind)
	}

	return nil
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

func (v *virtualCPU) handleException(exitCtx *exitContext) error {
	syndrome := v.exit.Exception.Syndrome
	ec := exceptionClass((syndrome >> exceptionClassShift) & exceptionClassMask)

	switch ec {
	case exceptionClassHvc:
		return v.handleHvc(exitCtx)
	case exceptionClassDataAbortLowerEL:
		return v.handleDataAbort(exitCtx, syndrome, v.exit.Exception.PhysicalAddress)
	case exceptionClassMsrAccess:
		return v.handleMsrAccess(exitCtx, syndrome)
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

	// PSCI function IDs (SMC64 calling convention - bit 30 set)
	psciCpuSuspend64   psciFunctionID = 0xC4000001
	psciCpuOff64       psciFunctionID = 0xC4000002
	psciCpuOn64        psciFunctionID = 0xC4000003
	psciAffinityInfo64 psciFunctionID = 0xC4000004

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

var (
	tsHvc = timeslice.RegisterKind("hvf_hvc", 0)
)

func (v *virtualCPU) handleHvc(exitCtx *exitContext) error {
	exitCtx.SetExitTimeslice(tsHvc)

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
		// Get the queried function ID from x1
		var x1 uint64
		if err := bindings.HvVcpuGetReg(v.id, bindings.HV_REG_X1, &x1); err != bindings.HV_SUCCESS {
			return fmt.Errorf("hvf: failed to get x1 for FEATURES: %w", err)
		}

		// Report support for implemented PSCI functions (both 32-bit and 64-bit versions)
		var result psciFunctionID
		switch psciFunctionID(x1) {
		case psciVersion, psciCpuOn, psciCpuOff, psciAffinityInfo, psciSystemOff, psciSystemReset, psciFeatures, psciMigrateInfoType,
			psciCpuOn64, psciCpuOff64, psciAffinityInfo64:
			result = psciSuccess
		default:
			result = psciNotSupported
		}

		if err := bindings.HvVcpuSetReg(v.id, bindings.HV_REG_X0, uint64(result)); err != bindings.HV_SUCCESS {
			return fmt.Errorf("hvf: failed to set FEATURES return value: %w", err)
		}
		return nil

	case psciCpuOn, psciCpuOn64:
		// Get target CPU MPIDR from x1, entry point from x2, context ID from x3
		var x1, x2, x3 uint64
		if err := bindings.HvVcpuGetReg(v.id, bindings.HV_REG_X1, &x1); err != bindings.HV_SUCCESS {
			return fmt.Errorf("hvf: failed to get x1 for CPU_ON: %w", err)
		}
		if err := bindings.HvVcpuGetReg(v.id, bindings.HV_REG_X2, &x2); err != bindings.HV_SUCCESS {
			return fmt.Errorf("hvf: failed to get x2 for CPU_ON: %w", err)
		}
		if err := bindings.HvVcpuGetReg(v.id, bindings.HV_REG_X3, &x3); err != bindings.HV_SUCCESS {
			return fmt.Errorf("hvf: failed to get x3 for CPU_ON: %w", err)
		}

		slog.Debug("hvf: PSCI CPU_ON", "caller", v.id, "targetMPIDR", x1, "entryPoint", fmt.Sprintf("0x%x", x2), "contextID", x3)

		result := v.vm.handlePsciCpuOn(x1, x2, x3)

		slog.Debug("hvf: PSCI CPU_ON result", "result", result)

		if err := bindings.HvVcpuSetReg(v.id, bindings.HV_REG_X0, uint64(result)); err != bindings.HV_SUCCESS {
			return fmt.Errorf("hvf: failed to set CPU_ON return value: %w", err)
		}
		return nil

	case psciCpuOff, psciCpuOff64:
		// Mark this vCPU as off and return success
		v.stateMu.Lock()
		v.state = vcpuStateOff
		v.stateMu.Unlock()

		if err := bindings.HvVcpuSetReg(v.id, bindings.HV_REG_X0, uint64(psciSuccess)); err != bindings.HV_SUCCESS {
			return fmt.Errorf("hvf: failed to set CPU_OFF return value: %w", err)
		}
		// Signal that this vCPU should stop running
		return hv.ErrVMHalted

	case psciAffinityInfo, psciAffinityInfo64:
		// Get target CPU MPIDR from x1, lowest affinity level from x2
		var x1 uint64
		if err := bindings.HvVcpuGetReg(v.id, bindings.HV_REG_X1, &x1); err != bindings.HV_SUCCESS {
			return fmt.Errorf("hvf: failed to get x1 for AFFINITY_INFO: %w", err)
		}

		result := v.vm.handlePsciAffinityInfo(x1)
		if err := bindings.HvVcpuSetReg(v.id, bindings.HV_REG_X0, uint64(result)); err != bindings.HV_SUCCESS {
			return fmt.Errorf("hvf: failed to set AFFINITY_INFO return value: %w", err)
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

var (
	tsDataAbort = timeslice.RegisterKind("hvf_data_abort", 0)
)

func (v *virtualCPU) handleDataAbort(exitCtx *exitContext, syndrome bindings.ExceptionSyndrome, physAddr bindings.IPA) error {
	exitCtx.SetExitTimeslice(tsDataAbort)

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

		if err := dev.WriteMMIO(exitCtx, addr, data); err != nil {
			pendingError = fmt.Errorf("hvf: failed to write MMIO: %w", err)
		}
	} else {
		if err := dev.ReadMMIO(exitCtx, addr, data); err != nil {
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

var (
	tsMsrAccess = timeslice.RegisterKind("hvf_msr_access", 0)
)

func (v *virtualCPU) handleMsrAccess(exitCtx *exitContext, syndrome bindings.ExceptionSyndrome) error {
	exitCtx.SetExitTimeslice(tsMsrAccess)

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
	rec *timeslice.Recorder

	hv         *hypervisor
	memMu      sync.RWMutex // protects memory during snapshot
	memory     []byte
	memoryBase uint64

	runQueue chan func()

	cpus map[int]*virtualCPU

	devices []hv.Device

	// Physical address space allocator for MMIO regions
	addressSpace *hv.AddressSpace

	closed bool

	gicInfo hv.Arm64GICInfo

	// Secondary vCPU lifecycle management (persists across RunAll calls)
	secondaryCtx     context.Context    // VM-lifetime context for secondary vCPUs
	secondaryCancel  context.CancelFunc // Cancels secondaryCtx when VM closes
	secondaryWg      sync.WaitGroup     // Tracks running secondary vCPU goroutines
	secondaryStarted bool               // Whether secondary loops have been started
	secondaryErrCh   chan error         // Collects errors from secondary vCPUs

	// Resume signaling using channel close for broadcast
	resumeCh  chan struct{}
	resumeMu  sync.Mutex // Protects resumeCh replacement
	resumeGen atomic.Int64

	// Per-RunAll context (cancelled when RunAll ends, allows next RunAll to set new context)
	runMu     sync.RWMutex
	runCtx    context.Context
	runCancel context.CancelFunc

	// secondaryYielded is set to true when a secondary vCPU triggers VM yield.
	// This is used to distinguish between intentional cancellation (success) and errors.
	secondaryYielded atomic.Bool
}

// implements hv.VirtualMachine
func (v *virtualMachine) Hypervisor() hv.Hypervisor { return v.hv }
func (v *virtualMachine) MemoryBase() uint64        { return v.memoryBase }
func (v *virtualMachine) MemorySize() uint64        { return uint64(len(v.memory)) }

// captureGICState captures the GIC state if configured.
func (v *virtualMachine) captureGICState() ([]byte, error) {
	if v.gicInfo.Version == hv.Arm64GICVersionUnknown {
		return nil, nil // No GIC configured
	}

	state := bindings.HvGicStateCreate()
	if state == 0 {
		return nil, fmt.Errorf("hvf: failed to create GIC state object")
	}
	// Note: state is an os_object that should be released, but we don't have os_release binding

	var size uintptr
	if err := bindings.HvGicStateGetSize(state, &size); err != bindings.HV_SUCCESS {
		return nil, fmt.Errorf("hvf: get GIC state size: %w", err)
	}

	if size == 0 {
		return nil, nil
	}

	data := make([]byte, size)
	if err := bindings.HvGicStateGetData(state, unsafe.Pointer(&data[0])); err != bindings.HV_SUCCESS {
		return nil, fmt.Errorf("hvf: get GIC state data: %w", err)
	}

	return data, nil
}

// restoreGICState restores the GIC state from snapshot data.
func (v *virtualMachine) restoreGICState(data []byte) error {
	if len(data) == 0 {
		return nil
	}

	if err := bindings.HvGicSetState(unsafe.Pointer(&data[0]), uintptr(len(data))); err != bindings.HV_SUCCESS {
		return fmt.Errorf("hvf: restore GIC state: %w", err)
	}

	return nil
}

// CaptureSnapshot implements [hv.VirtualMachine].
func (v *virtualMachine) CaptureSnapshot() (hv.Snapshot, error) {
	ret := &arm64HvfSnapshot{
		cpuStates:       make(map[int]arm64HvfVcpuSnapshot),
		deviceSnapshots: make(map[string]any),
	}

	// Capture each vCPU state
	for id, cpu := range v.cpus {
		errChan := make(chan error, 1)
		var state arm64HvfVcpuSnapshot

		cpu.runQueue <- func() {
			var err error
			state, err = cpu.captureSnapshot()
			errChan <- err
		}

		if err := <-errChan; err != nil {
			return nil, fmt.Errorf("hvf: capture vCPU %d snapshot: %w", id, err)
		}
		ret.cpuStates[id] = state
	}

	// Capture GIC state
	gicData, err := v.captureGICState()
	if err != nil {
		return nil, fmt.Errorf("hvf: capture GIC state: %w", err)
	}
	ret.gicState = gicData

	// Capture device snapshots
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

	// Capture memory (full copy of main guest RAM)
	v.memMu.Lock()
	if len(v.memory) > 0 {
		ret.memory = make([]byte, len(v.memory))
		copy(ret.memory, v.memory)
	}
	v.memMu.Unlock()

	return ret, nil
}

// RestoreSnapshot implements [hv.VirtualMachine].
func (v *virtualMachine) RestoreSnapshot(snap hv.Snapshot) error {
	snapshotData, ok := snap.(*arm64HvfSnapshot)
	if !ok {
		return fmt.Errorf("hvf: invalid snapshot type")
	}

	// Restore memory first
	v.memMu.Lock()
	if len(v.memory) != len(snapshotData.memory) {
		v.memMu.Unlock()
		return fmt.Errorf("hvf: snapshot memory size mismatch: got %d, want %d",
			len(snapshotData.memory), len(v.memory))
	}
	if len(v.memory) > 0 {
		copy(v.memory, snapshotData.memory)
	}
	v.memMu.Unlock()

	// Restore GIC state (before vCPUs to ensure interrupts are configured)
	if err := v.restoreGICState(snapshotData.gicState); err != nil {
		return fmt.Errorf("hvf: restore GIC state: %w", err)
	}

	// Restore each vCPU state
	for id, cpu := range v.cpus {
		state, ok := snapshotData.cpuStates[id]
		if !ok {
			return fmt.Errorf("hvf: missing vCPU %d state in snapshot", id)
		}

		errChan := make(chan error, 1)
		cpu.runQueue <- func() {
			errChan <- cpu.restoreSnapshot(state)
		}

		if err := <-errChan; err != nil {
			return fmt.Errorf("hvf: restore vCPU %d snapshot: %w", id, err)
		}
	}

	// Restore device snapshots
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

	// Cancel secondary vCPU context and wait for goroutines to exit
	if v.secondaryCancel != nil {
		v.secondaryCancel()
		v.secondaryWg.Wait()
	}

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
func (v *virtualMachine) AddDeviceFromTemplate(template hv.DeviceTemplate) (hv.Device, error) {
	dev, err := template.Create(v)
	if err != nil {
		return nil, fmt.Errorf("failed to create device from template: %w", err)
	}

	if err := v.AddDevice(dev); err != nil {
		return nil, err
	}
	return dev, nil
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

// AllocateMMIO implements [hv.VirtualMachine].
func (v *virtualMachine) AllocateMMIO(req hv.MMIOAllocationRequest) (hv.MMIOAllocation, error) {
	if v.addressSpace == nil {
		return hv.MMIOAllocation{}, fmt.Errorf("hvf: address space not initialized")
	}
	return v.addressSpace.Allocate(req)
}

// RegisterFixedMMIO implements [hv.VirtualMachine].
func (v *virtualMachine) RegisterFixedMMIO(name string, base, size uint64) error {
	if v.addressSpace == nil {
		return fmt.Errorf("hvf: address space not initialized")
	}
	return v.addressSpace.RegisterFixed(name, base, size)
}

// GetAllocatedMMIORegions implements [hv.VirtualMachine].
func (v *virtualMachine) GetAllocatedMMIORegions() []hv.MMIOAllocation {
	if v.addressSpace == nil {
		return nil
	}
	return v.addressSpace.Allocations()
}

// ReadAt implements [hv.VirtualMachine].
func (v *virtualMachine) ReadAt(p []byte, off int64) (n int, err error) {
	v.memMu.RLock()
	defer v.memMu.RUnlock()

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
	v.memMu.RLock()
	defer v.memMu.RUnlock()

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
// Note: This only runs vCPU 0. For SMP support, use RunAll.
func (v *virtualMachine) Run(ctx context.Context, cfg hv.RunConfig) error {
	if cfg == nil {
		return fmt.Errorf("hvf: RunConfig cannot be nil")
	}

	return v.VirtualCPUCall(0, func(vcpu hv.VirtualCPU) error {
		return cfg.Run(ctx, vcpu)
	})
}

// RunAll runs all vCPUs concurrently. vCPU 0 uses the provided RunConfig,
// while secondary vCPUs wait in parked state for PSCI CPU_ON.
//
// Secondary vCPUs persist across multiple RunAll calls - they are started once
// and paused/resumed between calls. This allows Linux to boot secondary CPUs
// in one RunAll and have them continue executing in subsequent RunAll calls.
func (v *virtualMachine) RunAll(ctx context.Context, cfg hv.RunConfig) error {
	if cfg == nil {
		return fmt.Errorf("hvf: RunConfig cannot be nil")
	}

	// If only 1 vCPU, use the simpler Run path
	if len(v.cpus) == 1 {
		return v.Run(ctx, cfg)
	}

	// Reset state for this run
	v.secondaryYielded.Store(false)

	// Create per-RunAll context for coordinating secondary vCPUs
	runCtx, runCancel := context.WithCancel(ctx)
	v.runMu.Lock()
	v.runCtx = runCtx
	v.runCancel = runCancel
	v.runMu.Unlock()
	defer runCancel()

	// Start secondary loops once (first RunAll) or resume paused secondaries
	if !v.secondaryStarted {
		// First RunAll: reset secondary vCPU states and start loops
		for id, vcpu := range v.cpus {
			if id == 0 {
				continue
			}
			// Drain stale wakeups
			select {
			case <-vcpu.wakeupCh:
				slog.Debug("hvf: RunAll drained stale wakeup", "vcpuID", id)
			default:
			}
			vcpu.stateMu.Lock()
			vcpu.state = vcpuStateParked
			vcpu.stateMu.Unlock()
		}
		v.startSecondaryLoops()
	} else {
		// Subsequent RunAll: call PreRun before resuming secondaries.
		// This ensures the program is loaded before secondaries see it.
		if preRun, ok := cfg.(hv.PreRunConfig); ok {
			if err := preRun.PreRun(); err != nil {
				return fmt.Errorf("hvf: PreRun: %w", err)
			}
		}

		// Resume paused secondaries by closing the channel.
		// We close and immediately create a new channel atomically to ensure that:
		// 1. Secondaries already waiting on the old channel wake up
		// 2. Secondaries about to wait will get the new (blocking) channel
		slog.Info("hvf: RunAll resuming paused secondaries", "numSecondaries", len(v.cpus)-1)
		v.resumeMu.Lock()
		close(v.resumeCh)
		v.resumeCh = make(chan struct{})
		v.resumeGen.Add(1)
		v.resumeMu.Unlock()
		slog.Info("hvf: RunAll resume complete")
	}

	// Run primary vCPU (vCPU 0) with the provided RunConfig
	var primaryErr error
	v.VirtualCPUCall(0, func(vcpu hv.VirtualCPU) error {
		primaryErr = cfg.Run(runCtx, vcpu)
		return nil
	})

	// Cancel runCtx to signal secondaries to pause (they stay running but paused)
	runCancel()

	// Create new resumeCh for next pause/resume cycle.
	// This must happen AFTER runCancel() so secondaries transitioning to Paused
	// will wait on the new (blocking) channel.
	// We also close the old channel to wake up any secondaries that already
	// grabbed a reference to it before the replacement.
	v.resumeMu.Lock()
	close(v.resumeCh)
	v.resumeCh = make(chan struct{})
	v.resumeGen.Add(1)
	v.resumeMu.Unlock()

	// Check for secondary errors (non-blocking)
	select {
	case err := <-v.secondaryErrCh:
		if primaryErr == nil {
			primaryErr = err
		}
	default:
	}

	// If a secondary CPU triggered the yield, the VM completed successfully
	// even though the primary CPU got context.Canceled
	if v.secondaryYielded.Load() && errors.Is(primaryErr, context.Canceled) {
		return nil
	}

	return primaryErr
}

// startSecondaryLoops starts the persistent goroutines for secondary vCPUs.
// These goroutines persist for the lifetime of the VM and handle pause/resume.
func (v *virtualMachine) startSecondaryLoops() {
	slog.Debug("hvf: starting secondary vCPU loops", "count", len(v.cpus)-1)

	// Initialize resume channel
	v.resumeCh = make(chan struct{})

	for id, vcpu := range v.cpus {
		if id == 0 {
			continue
		}
		v.secondaryWg.Add(1)
		go func(vcpu *virtualCPU, cpuID int) {
			defer v.secondaryWg.Done()
			if err := vcpu.runSecondaryLoop(); err != nil {
				if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
					v.secondaryErrCh <- fmt.Errorf("hvf: secondary vCPU %d: %w", cpuID, err)
				}
			}
			slog.Debug("hvf: secondary vCPU loop exited", "vcpuID", cpuID)
		}(vcpu, id)
	}
	v.secondaryStarted = true
}

// runSecondaryLoop is the main loop for secondary vCPUs.
// This loop persists for the lifetime of the VM - it pauses between RunAll calls
// and resumes when the next RunAll starts, preserving guest CPU state.
func (v *virtualCPU) runSecondaryLoop() error {
	slog.Debug("hvf: runSecondaryLoop started", "vcpuID", v.id)

	for {
		v.stateMu.Lock()
		state := v.state
		v.stateMu.Unlock()

		// If paused, wait for resume signal or VM shutdown
		if state == vcpuStatePaused {
			slog.Info("hvf: runSecondaryLoop waiting for resume", "vcpuID", v.id)

			// Get current resumeCh under lock to avoid racing with channel replacement
			v.vm.resumeMu.Lock()
			resumeCh := v.vm.resumeCh
			resumeGen := v.vm.resumeGen.Load()
			v.vm.resumeMu.Unlock()

			// Wait for resume signal using channel
			select {
			case <-v.vm.secondaryCtx.Done():
				slog.Info("hvf: runSecondaryLoop VM closed while paused", "vcpuID", v.id)
				return v.vm.secondaryCtx.Err()
			case <-resumeCh:
				// Resume signal received
			}

			// Resume signal received - continue execution
			slog.Info("hvf: runSecondaryLoop resumed", "vcpuID", v.id)
			v.stateMu.Lock()
			v.state = vcpuStateRunning
			v.stateMu.Unlock()

			// Continue running the vCPU from where it left off
			err := v.continueRun()
			slog.Info("hvf: runSecondaryLoop continueRun returned", "vcpuID", v.id, "error", err)
			if err != nil {
				if errors.Is(err, hv.ErrVMHalted) {
					v.stateMu.Lock()
					v.state = vcpuStateOff
					v.stateMu.Unlock()
					continue
				}
				if errors.Is(err, hv.ErrYield) {
					// Secondary vCPU hit yield - this happens when guest code that yields
					// is scheduled on a secondary CPU. We need to signal this to end the
					// current RunAll stage. Set the flag and cancel runCtx.
					slog.Info("hvf: runSecondaryLoop yield from secondary, signaling", "vcpuID", v.id)
					v.vm.secondaryYielded.Store(true)
					v.cancelCurrentRun()
					v.stateMu.Lock()
					v.state = vcpuStatePaused
					v.stateMu.Unlock()
					continue
				}
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					// RunAll ended - wait for next generation before going back to paused
					// This prevents busy-looping on a closed channel
					currentGen := v.vm.resumeGen.Load()
					if currentGen == resumeGen {
						slog.Info("hvf: runSecondaryLoop waiting for gen change", "vcpuID", v.id, "resumeGen", resumeGen)
						for v.vm.resumeGen.Load() == resumeGen {
							select {
							case <-v.vm.secondaryCtx.Done():
								return v.vm.secondaryCtx.Err()
							case <-time.After(100 * time.Microsecond):
							}
						}
						slog.Info("hvf: runSecondaryLoop gen changed", "vcpuID", v.id, "newGen", v.vm.resumeGen.Load())
					}
					v.stateMu.Lock()
					v.state = vcpuStatePaused
					v.stateMu.Unlock()
					continue
				}
				return fmt.Errorf("hvf: secondary vCPU run failed after resume: %w", err)
			}
			continue
		}

		// Normal operation: wait for wakeup, VM shutdown, or run context end
		// Get current run context for checking cancellation
		v.vm.runMu.RLock()
		runCtx := v.vm.runCtx
		v.vm.runMu.RUnlock()

		select {
		case <-v.vm.secondaryCtx.Done():
			slog.Debug("hvf: runSecondaryLoop VM closed", "vcpuID", v.id)
			return v.vm.secondaryCtx.Err()

		case <-runCtx.Done():
			// RunAll ended but we never got CPU_ON, transition to Paused
			slog.Debug("hvf: runSecondaryLoop runCtx cancelled while waiting for wakeup", "vcpuID", v.id)
			v.stateMu.Lock()
			v.state = vcpuStatePaused
			v.stateMu.Unlock()
			continue

		case wakeup := <-v.wakeupCh:
			slog.Debug("hvf: runSecondaryLoop received wakeup", "vcpuID", v.id, "entryPoint", fmt.Sprintf("0x%x", wakeup.entryPoint), "contextID", wakeup.contextID)

			// Get current run context
			v.vm.runMu.RLock()
			runCtx := v.vm.runCtx
			v.vm.runMu.RUnlock()

			// Configure vCPU for entry and run
			if err := v.runFromWakeup(runCtx, wakeup); err != nil {
				slog.Debug("hvf: runSecondaryLoop runFromWakeup error", "vcpuID", v.id, "error", err)
				if errors.Is(err, hv.ErrVMHalted) {
					// CPU_OFF was called, go back to parked state
					v.stateMu.Lock()
					v.state = vcpuStateParked
					v.stateMu.Unlock()
					slog.Debug("hvf: runSecondaryLoop CPU_OFF, going to parked", "vcpuID", v.id)
					continue
				}
				if errors.Is(err, hv.ErrYield) {
					// Secondary vCPU hit yield - signal to end the stage
					slog.Info("hvf: runSecondaryLoop yield from secondary (normal op), signaling", "vcpuID", v.id)
					v.vm.secondaryYielded.Store(true)
					v.cancelCurrentRun()
					v.stateMu.Lock()
					v.state = vcpuStatePaused
					v.stateMu.Unlock()
					continue
				}
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					// RunAll ended (primary yielded or timeout), pause and wait for resume
					slog.Debug("hvf: runSecondaryLoop context cancelled, pausing", "vcpuID", v.id)
					v.stateMu.Lock()
					v.state = vcpuStatePaused
					v.stateMu.Unlock()
					continue
				}
				return fmt.Errorf("hvf: secondary vCPU run failed: %w", err)
			}
		}
	}
}

// continueRun continues the vCPU execution from where it was paused.
func (v *virtualCPU) continueRun() error {
	v.vm.runMu.RLock()
	runCtx := v.vm.runCtx
	v.vm.runMu.RUnlock()

	// Check if context is already cancelled
	select {
	case <-runCtx.Done():
		slog.Info("hvf: continueRun runCtx already cancelled", "vcpuID", v.id)
		return runCtx.Err()
	default:
	}

	done := make(chan error, 1)
	v.runQueue <- func() {
		done <- v.runLoop(runCtx)
	}

	// Wait for the run to complete while pumping the VM runQueue
	for {
		select {
		case err := <-done:
			return err
		case f := <-v.vm.runQueue:
			f()
		}
	}
}

// cancelCurrentRun cancels the current RunAll context to signal other vCPUs to pause.
func (v *virtualCPU) cancelCurrentRun() {
	v.vm.runMu.Lock()
	if v.vm.runCancel != nil {
		v.vm.runCancel()
	}
	v.vm.runMu.Unlock()
}

// runFromWakeup configures the vCPU with the wakeup parameters and runs it.
func (v *virtualCPU) runFromWakeup(ctx context.Context, wakeup vcpuWakeup) error {
	done := make(chan error, 1)

	v.runQueue <- func() {
		// Configure vCPU registers for entry
		// PC = entry point, X0 = context ID
		// PSTATE = EL1h mode with interrupts masked
		const pstateEL1h = 0x3c5 // EL1h, DAIF masked

		if err := bindings.HvVcpuSetReg(v.id, bindings.HV_REG_PC, wakeup.entryPoint); err != bindings.HV_SUCCESS {
			done <- fmt.Errorf("hvf: failed to set PC for secondary vCPU: %w", err)
			return
		}
		if err := bindings.HvVcpuSetReg(v.id, bindings.HV_REG_X0, wakeup.contextID); err != bindings.HV_SUCCESS {
			done <- fmt.Errorf("hvf: failed to set X0 for secondary vCPU: %w", err)
			return
		}
		if err := bindings.HvVcpuSetReg(v.id, bindings.HV_REG_CPSR, pstateEL1h); err != bindings.HV_SUCCESS {
			done <- fmt.Errorf("hvf: failed to set PSTATE for secondary vCPU: %w", err)
			return
		}

		// Run the vCPU until it exits
		done <- v.runLoop(ctx)
	}

	// Wait for the run to complete while pumping the VM runQueue.
	// IMPORTANT: We must always wait for 'done' even if context is cancelled,
	// because the vCPU thread is still executing runLoop. Returning early would
	// cause a race condition on the next RunAll call.
	for {
		select {
		case err := <-done:
			return err
		case f := <-v.vm.runQueue:
			f()
		}
	}
}

// runLoop runs the vCPU in a loop, handling exits until it halts or errors.
func (v *virtualCPU) runLoop(ctx context.Context) error {
	for {
		if err := v.Run(ctx); err != nil {
			// Handle expected exit conditions for secondary vCPUs
			if errors.Is(err, hv.ErrVMHalted) {
				return hv.ErrVMHalted
			}
			if errors.Is(err, hv.ErrYield) {
				// Yield means the VM is done, return ErrYield so runSecondaryLoop exits
				return hv.ErrYield
			}
			return err
		}
	}
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

// handlePsciCpuOn handles the PSCI CPU_ON call to boot a secondary vCPU.
// targetMPIDR is the MPIDR of the target CPU, entryPoint is the address to start
// execution, and contextID is passed to the target CPU in X0.
func (v *virtualMachine) handlePsciCpuOn(targetMPIDR, entryPoint, contextID uint64) psciFunctionID {
	// Extract CPU ID from MPIDR (Aff0 field, bits [7:0])
	targetID := int(targetMPIDR & 0xFF)

	slog.Debug("hvf: handlePsciCpuOn", "targetID", targetID, "numCPUs", len(v.cpus))

	vcpu, ok := v.cpus[targetID]
	if !ok {
		slog.Debug("hvf: handlePsciCpuOn invalid target", "targetID", targetID)
		return psciInvalidParameters
	}

	vcpu.stateMu.Lock()
	defer vcpu.stateMu.Unlock()

	slog.Debug("hvf: handlePsciCpuOn target state", "targetID", targetID, "state", vcpu.state)

	switch vcpu.state {
	case vcpuStateRunning, vcpuStatePaused:
		// Paused CPUs are still "on" from the guest's perspective
		return psciAlreadyOn
	case vcpuStateParked, vcpuStateOff:
		// Send wakeup signal to the target vCPU
		select {
		case vcpu.wakeupCh <- vcpuWakeup{entryPoint: entryPoint, contextID: contextID}:
			slog.Debug("hvf: handlePsciCpuOn wakeup sent", "targetID", targetID)
			vcpu.state = vcpuStateRunning
			return psciSuccess
		default:
			// Channel full - CPU_ON already pending
			slog.Debug("hvf: handlePsciCpuOn channel full", "targetID", targetID)
			return psciOnPending
		}
	default:
		return psciInternalFailure
	}
}

// handlePsciAffinityInfo returns the power state of a vCPU.
// Returns 0 for ON, 1 for OFF, 2 for ON_PENDING.
func (v *virtualMachine) handlePsciAffinityInfo(targetMPIDR uint64) psciFunctionID {
	// Extract CPU ID from MPIDR (Aff0 field, bits [7:0])
	targetID := int(targetMPIDR & 0xFF)

	vcpu, ok := v.cpus[targetID]
	if !ok {
		return psciInvalidParameters
	}

	vcpu.stateMu.Lock()
	state := vcpu.state
	vcpu.stateMu.Unlock()

	switch state {
	case vcpuStateRunning, vcpuStatePaused:
		// Paused CPUs are still "on" from the guest's perspective
		return 0 // AFFINITY_LEVEL_ON
	case vcpuStateOff:
		return 1 // AFFINITY_LEVEL_OFF
	case vcpuStateParked:
		return 1 // Also OFF (parked means not yet started)
	default:
		return psciInternalFailure
	}
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

var (
	tsHvfPreInit              = timeslice.RegisterKind("hvf_pre_init", 0)
	tsHvfVmCreate             = timeslice.RegisterKind("hvf_vm_create", 0)
	tsHvfOnCreateVM           = timeslice.RegisterKind("hvf_on_create_vm", 0)
	tsHvfAllocateMemory       = timeslice.RegisterKind("hvf_allocate_memory", 0)
	tsHvfGicCreate            = timeslice.RegisterKind("hvf_gic_create", 0)
	tsHvfOnCreateVMWithMemory = timeslice.RegisterKind("hvf_on_create_vm_with_memory", 0)
	tsHvfOnCreateVCPU         = timeslice.RegisterKind("hvf_on_create_vcpu", 0)
	tsHvfLoaded               = timeslice.RegisterKind("hvf_loaded", 0)
)

// NewVirtualMachine implements [hv.Hypervisor].
func (h *hypervisor) NewVirtualMachine(config hv.VMConfig) (hv.VirtualMachine, error) {
	if vm := globalVM.Load(); vm != nil {
		return nil, fmt.Errorf("VM already exists, hvf is limited to a single VM per process")
	}

	ret := &virtualMachine{
		hv:       h,
		rec:      timeslice.NewState(),
		cpus:     make(map[int]*virtualCPU),
		runQueue: make(chan func(), 16),
	}

	vmConfig := bindings.HvVmConfigCreate()

	timeslice.Record(tsHvfPreInit, time.Since(tsHvfStartTime))

	vm := bindings.HvVmCreate(vmConfig)
	if vm != bindings.HV_SUCCESS {
		return nil, fmt.Errorf("failed to create VM: %d", vm)
	}

	ret.rec.Record(tsHvfVmCreate)

	// Only one VM can be created at a time for a single process.
	if swapped := globalVM.CompareAndSwap(nil, ret); !swapped {
		return nil, fmt.Errorf("global VM already exists")
	}

	// The VM is now created without memory.
	if err := config.Callbacks().OnCreateVM(ret); err != nil {
		return nil, fmt.Errorf("failed to create VM: %w", err)
	}

	ret.rec.Record(tsHvfOnCreateVM)

	// allocate memory for the VM
	mem, err := ret.AllocateMemory(config.MemoryBase(), config.MemorySize())
	if err != nil {
		return nil, fmt.Errorf("failed to allocate memory for VM: %w", err)
	}

	ret.rec.Record(tsHvfAllocateMemory)

	ret.memory = mem.(*memoryRegion).memory
	ret.memoryBase = config.MemoryBase()

	// Initialize the physical address space allocator
	ret.addressSpace = hv.NewAddressSpace(h.Architecture(), config.MemoryBase(), config.MemorySize())

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

		ret.rec.Record(tsHvfGicCreate)
	}

	// Call the callback to allow the user to perform any additional initialization.
	if err := config.Callbacks().OnCreateVMWithMemory(ret); err != nil {
		return nil, fmt.Errorf("failed to create VM with memory: %w", err)
	}

	ret.rec.Record(tsHvfOnCreateVMWithMemory)

	// create vCPUs
	if config.CPUCount() < 1 || config.CPUCount() > 16 {
		return nil, fmt.Errorf("hvf: CPU count must be between 1 and 16, got %d", config.CPUCount())
	}

	// Initialize secondary vCPU lifecycle management (persists across RunAll calls)
	ret.secondaryCtx, ret.secondaryCancel = context.WithCancel(context.Background())
	ret.secondaryErrCh = make(chan error, config.CPUCount())

	for i := 0; i < config.CPUCount(); i++ {
		// Determine initial state: vCPU 0 starts running, others start parked
		initialState := vcpuStateParked
		if i == 0 {
			initialState = vcpuStateRunning
		}

		vcpu := &virtualCPU{
			vm:        ret,
			runQueue:  make(chan func(), 16),
			initError: make(chan error, 1),
			rec:       timeslice.NewState(),
			state:     initialState,
			wakeupCh:  make(chan vcpuWakeup, 1),
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

	ret.rec.Record(tsHvfOnCreateVCPU)

	if loader := config.Loader(); loader != nil {
		if err := loader.Load(ret); err != nil {
			return nil, fmt.Errorf("failed to load VM: %w", err)
		}
	}

	ret.rec.Record(tsHvfLoaded)

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
