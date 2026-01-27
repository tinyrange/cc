//go:build darwin && arm64

package hvf

import (
	"context"
	"encoding/binary"
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

// timesliceMMIOAddr is the MMIO address for guest timeslice recording.
// Writes to this address record a timeslice marker with the written value as ID (0-255).
const timesliceMMIOAddr uint64 = 0xf0001000

// Guest timeslice ID constants - these must match the values used in guest code
const (
	// init_source.go phases (0-49)
	GuestTsInitStart         = 0
	GuestTsPhase1DevCreate   = 1
	GuestTsPhase2MountDev    = 2
	GuestTsPhase2MountShm    = 3
	GuestTsPhase4ConsoleOpen = 5
	GuestTsPhase4Setsid      = 6
	GuestTsPhase4Dup         = 8
	GuestTsPhase5MemOpen     = 9
	GuestTsPhase5MailboxMap  = 10
	GuestTsPhase5TsMap       = 11
	GuestTsPhase5ConfigMap   = 12
	GuestTsPhase5AnonMap     = 13
	GuestTsPhase6TimeSetup   = 14
	GuestTsPhase7LoopStart   = 15
	GuestTsPhase7CopyPayload = 16
	GuestTsPhase7Relocate    = 17
	GuestTsPhase7ISB         = 18
	GuestTsPhase7CallPayload = 19
	GuestTsPhase7PayloadDone = 20

	// container_init_source.go phases (50-99)
	GuestTsContainerStart    = 50
	GuestTsContainerMkdir    = 51
	GuestTsContainerVirtiofs = 52
	GuestTsContainerMkdirMnt = 53
	GuestTsContainerMountFs  = 54
	GuestTsContainerChroot   = 55
	GuestTsContainerDevpts   = 56
	GuestTsContainerQemu     = 57
	GuestTsContainerHostname = 58
	GuestTsContainerLoopback = 59
	GuestTsContainerHosts    = 60
	GuestTsContainerNetwork  = 61
	GuestTsContainerWorkdir  = 62
	GuestTsContainerDropPriv = 63
	GuestTsContainerExec     = 64
	GuestTsContainerComplete = 65

	// ForkExecWait phases (66-70)
	GuestTsForkStart = 66
	GuestTsForkDone  = 67
	GuestTsWaitStart = 68
	GuestTsWaitDone  = 69

	// Command loop phases (70-79)
	GuestTsContainerCmdLoopStart = 70
	GuestTsContainerCmdRead      = 71
	GuestTsContainerCmdExec      = 72
	GuestTsContainerCmdDone      = 73
)

// guestTimesliceNames maps guest timeslice IDs to descriptive names
var guestTimesliceNames = map[int]string{
	// init_source.go phases
	GuestTsInitStart:         "guest::init_start",
	GuestTsPhase1DevCreate:   "guest::phase1_dev_create",
	GuestTsPhase2MountDev:    "guest::phase2_mount_dev",
	GuestTsPhase2MountShm:    "guest::phase2_mount_shm",
	GuestTsPhase4ConsoleOpen: "guest::phase4_console_open",
	GuestTsPhase4Setsid:      "guest::phase4_setsid",
	GuestTsPhase4Dup:         "guest::phase4_dup",
	GuestTsPhase5MemOpen:     "guest::phase5_mem_open",
	GuestTsPhase5MailboxMap:  "guest::phase5_mailbox_map",
	GuestTsPhase5TsMap:       "guest::phase5_ts_map",
	GuestTsPhase5ConfigMap:   "guest::phase5_config_map",
	GuestTsPhase5AnonMap:     "guest::phase5_anon_map",
	GuestTsPhase6TimeSetup:   "guest::phase6_time_setup",
	GuestTsPhase7LoopStart:   "guest::phase7_loop_start",
	GuestTsPhase7CopyPayload: "guest::phase7_copy_payload",
	GuestTsPhase7Relocate:    "guest::phase7_relocate",
	GuestTsPhase7ISB:         "guest::phase7_isb",
	GuestTsPhase7CallPayload: "guest::phase7_call_payload",
	GuestTsPhase7PayloadDone: "guest::phase7_payload_done",

	// container_init_source.go phases
	GuestTsContainerStart:    "guest::container_start",
	GuestTsContainerMkdir:    "guest::container_mkdir",
	GuestTsContainerVirtiofs: "guest::container_virtiofs",
	GuestTsContainerMkdirMnt: "guest::container_mkdir_mnt",
	GuestTsContainerMountFs:  "guest::container_mount_fs",
	GuestTsContainerChroot:   "guest::container_chroot",
	GuestTsContainerDevpts:   "guest::container_devpts",
	GuestTsContainerQemu:     "guest::container_qemu",
	GuestTsContainerHostname: "guest::container_hostname",
	GuestTsContainerLoopback: "guest::container_loopback",
	GuestTsContainerHosts:    "guest::container_hosts",
	GuestTsContainerNetwork:  "guest::container_network",
	GuestTsContainerWorkdir:  "guest::container_workdir",
	GuestTsContainerDropPriv: "guest::container_drop_priv",
	GuestTsContainerExec:     "guest::container_exec",
	GuestTsContainerComplete: "guest::container_complete",

	// ForkExecWait phases
	GuestTsForkStart: "guest::fork_start",
	GuestTsForkDone:  "guest::fork_done",
	GuestTsWaitStart: "guest::wait_start",
	GuestTsWaitDone:  "guest::wait_done",

	// Command loop phases
	GuestTsContainerCmdLoopStart: "guest::container_cmd_loop_start",
	GuestTsContainerCmdRead:      "guest::container_cmd_read",
	GuestTsContainerCmdExec:      "guest::container_cmd_exec",
	GuestTsContainerCmdDone:      "guest::container_cmd_done",
}

// guestTimeslices holds pre-registered timeslice IDs for guest-recorded markers.
// These are registered at init time to avoid allocation during MMIO handling.
var guestTimeslices [256]timeslice.TimesliceID

func init() {
	for i := range guestTimeslices {
		name, ok := guestTimesliceNames[i]
		if !ok {
			name = fmt.Sprintf("guest::%d", i)
		}
		guestTimeslices[i] = timeslice.RegisterKind(name, timeslice.SliceFlagGuestTime)
	}
}

var (
	tsHvfGuestTime     = timeslice.RegisterKind("hvf_guest_time", timeslice.SliceFlagGuestTime)
	tsHvfHostTime      = timeslice.RegisterKind("hvf_host_time", 0)
	tsHvfFirstRunStart = timeslice.RegisterKind("hvf_first_run_start", 0)

	// Snapshot capture timeslice markers
	tsCaptureVcpuState = timeslice.RegisterKind("snapshot::capture_vcpu_state", 0)
	tsCaptureGicState  = timeslice.RegisterKind("snapshot::capture_gic_state", 0)
	tsCaptureDevices   = timeslice.RegisterKind("snapshot::capture_devices", 0)
	tsCaptureMemory    = timeslice.RegisterKind("snapshot::capture_memory", 0)

	// Snapshot restore timeslice markers
	tsRestoreMemory    = timeslice.RegisterKind("snapshot::restore_memory", 0)
	tsRestoreGicState  = timeslice.RegisterKind("snapshot::restore_gic_state", 0)
	tsRestoreVcpuState = timeslice.RegisterKind("snapshot::restore_vcpu_state", 0)
	tsRestoreDevices   = timeslice.RegisterKind("snapshot::restore_devices", 0)

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
	memory          []byte // The mmap'd memory buffer
	memoryOwned     bool   // If true, snapshot owns the memory and must free it on Close
	gicState        []byte

	// captureTimeNanos records the wall clock time when the snapshot was captured.
	// Used to adjust vtimer offset during restore to prevent guest time jumps.
	captureTimeNanos int64
}

// Close frees the snapshot's memory if it owns it.
func (s *arm64HvfSnapshot) Close() error {
	if s.memoryOwned && s.memory != nil {
		if err := unix.Munmap(s.memory); err != nil {
			return fmt.Errorf("hvf: failed to unmap snapshot memory: %w", err)
		}
		s.memory = nil
		s.memoryOwned = false
	}
	return nil
}

type exitContext struct {
	kind timeslice.TimesliceID
}

func (ctx *exitContext) SetExitTimeslice(id timeslice.TimesliceID) {
	ctx.kind = id
}

type virtualCPU struct {
	vm *virtualMachine

	rec *timeslice.Recorder

	id   bindings.VCPU
	exit *bindings.VcpuExit

	closed bool

	runQueue chan func()

	initError chan error
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
	close(v.runQueue)
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
// vtimerAdjustment is added to the snapshot's VTimerOffset to compensate for
// wall clock time that has passed since the snapshot was captured. This prevents
// the guest from seeing a large time jump when the snapshot is restored.
func (v *virtualCPU) restoreSnapshot(snap arm64HvfVcpuSnapshot, vtimerAdjustment uint64) error {
	// Restore virtual timer offset with adjustment for wall clock drift
	// The adjustment prevents the guest from perceiving time jumps between
	// snapshot capture and restore.
	adjustedOffset := snap.VTimerOffset + vtimerAdjustment
	if err := bindings.HvVcpuSetVtimerOffset(v.id, adjustedOffset); err != bindings.HV_SUCCESS {
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

var (
	tsDataAbort = timeslice.RegisterKind("hvf_data_abort", 0)
)

func (v *virtualCPU) handleDataAbort(exitCtx *exitContext, syndrome bindings.ExceptionSyndrome, physAddr bindings.IPA) error {
	addr := uint64(physAddr)

	// Fast-path: timeslice recording MMIO
	// This check is done before any other processing to minimize overhead.
	// We use v.rec.Record() directly to capture timing relative to the run loop.
	if addr == timesliceMMIOAddr {
		decoded, err := decodeDataAbort(syndrome)
		if err != nil {
			return fmt.Errorf("hvf: timeslice MMIO decode error: %w", err)
		}
		if decoded.write {
			// Read the timeslice ID from the source register
			var val uint64 = 0 // default to 0 for XZR
			if decoded.target != hv.RegisterARM64Xzr {
				hvReg, ok := registerMap[decoded.target]
				if !ok {
					return fmt.Errorf("hvf: timeslice MMIO unsupported register %v", decoded.target)
				}
				if err := bindings.HvVcpuGetReg(v.id, hvReg, &val); err != bindings.HV_SUCCESS {
					return fmt.Errorf("hvf: timeslice MMIO failed to get register: %w", err)
				}
			}
			// Record the guest timeslice directly for proper timing
			if val < 256 {
				v.vm.guestTimesliceState.Record(guestTimeslices[val])
			}
		}
		// For reads, we just return 0 (no register write needed)
		return v.advanceProgramCounter()
	}

	// Normal MMIO path
	exitCtx.SetExitTimeslice(tsDataAbort)

	decoded, err := decodeDataAbort(syndrome)
	if err != nil {
		return fmt.Errorf("hvf: failed to decode data abort syndrome 0x%X: %w", syndrome, err)
	}

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
		bindings.OsRelease(uintptr(cfg))
		v.initError <- err
		return
	}
	bindings.OsRelease(uintptr(cfg))

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

// allocatedMemoryRegion tracks memory regions allocated via AllocateMemory
// for proper cleanup when the VM is closed.
type allocatedMemoryRegion struct {
	memory   []byte
	physAddr uint64
	size     uint64
}

type virtualMachine struct {
	rec *timeslice.Recorder

	hv         *hypervisor
	memMu      sync.RWMutex // protects memory during snapshot
	memory     []byte
	memoryBase uint64

	// additionalMemory tracks memory regions allocated via AllocateMemory
	// (excluding the main VM memory). These must be unmapped and freed on Close().
	additionalMemory []allocatedMemoryRegion

	runQueue chan func()

	cpus map[int]*virtualCPU

	devices []hv.Device

	// Physical address space allocator for MMIO regions
	addressSpace *hv.AddressSpace

	closed bool

	gicInfo hv.Arm64GICInfo

	guestTimesliceState *timeslice.Recorder
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
	defer bindings.OsRelease(uintptr(state))

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
// Uses COW (copy-on-write) via vm_copy for O(1) memory capture.
func (v *virtualMachine) CaptureSnapshot() (hv.Snapshot, error) {
	rec := timeslice.NewState()

	ret := &arm64HvfSnapshot{
		cpuStates:        make(map[int]arm64HvfVcpuSnapshot),
		deviceSnapshots:  make(map[string]any),
		captureTimeNanos: time.Now().UnixNano(),
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
	rec.Record(tsCaptureVcpuState)

	// Capture GIC state
	gicData, err := v.captureGICState()
	if err != nil {
		return nil, fmt.Errorf("hvf: capture GIC state: %w", err)
	}
	ret.gicState = gicData
	rec.Record(tsCaptureGicState)

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
	rec.Record(tsCaptureDevices)

	// Capture memory using COW (copy-on-write)
	// The snapshot takes ownership of the current memory, and the VM gets a new
	// COW copy. This makes capture O(1) instead of O(n).
	v.memMu.Lock()
	if len(v.memory) > 0 {
		memSize := len(v.memory)

		// Allocate new memory for the VM to continue using
		newMem, err := unix.Mmap(-1, 0, memSize,
			unix.PROT_READ|unix.PROT_WRITE, unix.MAP_ANONYMOUS|unix.MAP_PRIVATE)
		if err != nil {
			v.memMu.Unlock()
			return nil, fmt.Errorf("hvf: allocate new memory for COW: %w", err)
		}

		// COW copy from old memory to new memory (O(1) operation)
		if err := bindings.VmCopy(
			uintptr(unsafe.Pointer(&v.memory[0])),
			uintptr(unsafe.Pointer(&newMem[0])),
			uintptr(memSize),
		); err != nil {
			unix.Munmap(newMem)
			v.memMu.Unlock()
			return nil, fmt.Errorf("hvf: vm_copy for snapshot COW: %w", err)
		}

		// Unmap old memory from VM guest physical address space
		if err := bindings.HvVmUnmap(bindings.IPA(v.memoryBase), uintptr(memSize)); err != bindings.HV_SUCCESS {
			unix.Munmap(newMem)
			v.memMu.Unlock()
			return nil, fmt.Errorf("hvf: unmap old memory from VM: %w", err)
		}

		// Map new memory to VM guest physical address space
		if err := bindings.HvVmMap(
			unsafe.Pointer(&newMem[0]),
			bindings.IPA(v.memoryBase),
			uintptr(memSize),
			bindings.HV_MEMORY_READ|bindings.HV_MEMORY_WRITE|bindings.HV_MEMORY_EXEC,
		); err != bindings.HV_SUCCESS {
			unix.Munmap(newMem)
			v.memMu.Unlock()
			return nil, fmt.Errorf("hvf: map new memory to VM: %w", err)
		}

		// Snapshot takes ownership of old memory
		ret.memory = v.memory
		ret.memoryOwned = true

		// VM now uses new memory
		v.memory = newMem
	}
	v.memMu.Unlock()
	rec.Record(tsCaptureMemory)

	return ret, nil
}

// RestoreSnapshot implements [hv.VirtualMachine].
// Uses COW (copy-on-write) via vm_copy for O(1) memory restore.
// We allocate fresh memory and vm_copy snapshot data to it, then swap with VM memory.
// This ensures true COW behavior since we're copying to fresh pages.
func (v *virtualMachine) RestoreSnapshot(snap hv.Snapshot) error {
	rec := timeslice.NewState()

	snapshotData, ok := snap.(*arm64HvfSnapshot)
	if !ok {
		return fmt.Errorf("hvf: invalid snapshot type")
	}

	// Restore memory using COW - allocate fresh memory and swap
	// vm_copy to existing pages doesn't benefit from COW, so we need fresh pages
	v.memMu.Lock()
	if len(v.memory) != len(snapshotData.memory) {
		v.memMu.Unlock()
		return fmt.Errorf("hvf: snapshot memory size mismatch: got %d, want %d",
			len(snapshotData.memory), len(v.memory))
	}
	if len(v.memory) > 0 {
		memSize := len(v.memory)

		// Allocate fresh memory for COW restore
		newMem, err := unix.Mmap(-1, 0, memSize,
			unix.PROT_READ|unix.PROT_WRITE, unix.MAP_ANONYMOUS|unix.MAP_PRIVATE)
		if err != nil {
			v.memMu.Unlock()
			return fmt.Errorf("hvf: allocate new memory for COW restore: %w", err)
		}

		// COW copy from snapshot to fresh memory (O(1) operation)
		if err := bindings.VmCopy(
			uintptr(unsafe.Pointer(&snapshotData.memory[0])),
			uintptr(unsafe.Pointer(&newMem[0])),
			uintptr(memSize),
		); err != nil {
			unix.Munmap(newMem)
			v.memMu.Unlock()
			return fmt.Errorf("hvf: vm_copy for snapshot restore: %w", err)
		}

		// Unmap old memory from VM guest physical address space
		if err := bindings.HvVmUnmap(bindings.IPA(v.memoryBase), uintptr(memSize)); err != bindings.HV_SUCCESS {
			unix.Munmap(newMem)
			v.memMu.Unlock()
			return fmt.Errorf("hvf: unmap old memory from VM for restore: %w", err)
		}

		// Free old VM memory. Failure here indicates a bug (invalid slice) and leaves
		// the VM unusable since guest memory is already unmapped above.
		if err := unix.Munmap(v.memory); err != nil {
			unix.Munmap(newMem)
			v.memMu.Unlock()
			return fmt.Errorf("hvf: munmap old VM memory (VM now unusable): %w", err)
		}

		// Map new memory to VM guest physical address space
		if err := bindings.HvVmMap(
			unsafe.Pointer(&newMem[0]),
			bindings.IPA(v.memoryBase),
			uintptr(memSize),
			bindings.HV_MEMORY_READ|bindings.HV_MEMORY_WRITE|bindings.HV_MEMORY_EXEC,
		); err != bindings.HV_SUCCESS {
			unix.Munmap(newMem)
			v.memMu.Unlock()
			return fmt.Errorf("hvf: map new memory to VM for restore: %w", err)
		}

		// VM now uses restored memory
		v.memory = newMem
	}
	v.memMu.Unlock()
	rec.Record(tsRestoreMemory)

	// Restore GIC state (before vCPUs to ensure interrupts are configured)
	if err := v.restoreGICState(snapshotData.gicState); err != nil {
		return fmt.Errorf("hvf: restore GIC state: %w", err)
	}
	rec.Record(tsRestoreGicState)

	// Calculate vtimer offset adjustment for wall clock drift.
	// This prevents the guest from seeing a time jump when the snapshot is restored.
	// ARM64 timer runs at 24MHz (24_000_000 Hz), so we convert nanoseconds to ticks.
	// ticks = nanoseconds * 24 / 1000
	var vtimerAdjustment uint64
	if snapshotData.captureTimeNanos > 0 {
		nowNanos := time.Now().UnixNano()
		deltaNanos := nowNanos - snapshotData.captureTimeNanos
		if deltaNanos > 0 {
			// Convert nanoseconds to timer ticks (24MHz timer)
			// Using uint64 to avoid overflow: deltaNanos * 24 / 1000
			vtimerAdjustment = uint64(deltaNanos) * 24 / 1000
		}
	}

	// Restore each vCPU state
	for id, cpu := range v.cpus {
		state, ok := snapshotData.cpuStates[id]
		if !ok {
			return fmt.Errorf("hvf: missing vCPU %d state in snapshot", id)
		}

		errChan := make(chan error, 1)
		cpu.runQueue <- func() {
			errChan <- cpu.restoreSnapshot(state, vtimerAdjustment)
		}

		if err := <-errChan; err != nil {
			return fmt.Errorf("hvf: restore vCPU %d snapshot: %w", id, err)
		}
	}
	rec.Record(tsRestoreVcpuState)

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
	rec.Record(tsRestoreDevices)

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

	// Clean up additional memory regions allocated via AllocateMemory
	for _, region := range v.additionalMemory {
		if err := bindings.HvVmUnmap(bindings.IPA(region.physAddr), uintptr(region.size)); err != bindings.HV_SUCCESS {
			slog.Error("failed to unmap additional memory region", "physAddr", region.physAddr, "error", err)
		}
		if err := unix.Munmap(region.memory); err != nil {
			slog.Error("failed to munmap additional memory region", "error", err)
		}
	}
	v.additionalMemory = nil

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
		unix.Munmap(mem)
		return nil, fmt.Errorf("failed to map memory for VM at 0x%X,0x%X: %w", physAddr, size, err)
	}

	// Track the allocated region for cleanup on Close()
	v.additionalMemory = append(v.additionalMemory, allocatedMemoryRegion{
		memory:   mem,
		physAddr: physAddr,
		size:     size,
	})

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
func (v *virtualMachine) Run(ctx context.Context, cfg hv.RunConfig) error {
	if cfg == nil {
		return fmt.Errorf("hvf: RunConfig cannot be nil")
	}

	v.guestTimesliceState = timeslice.NewState()

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

var (
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

	vm := bindings.HvVmCreate(vmConfig)
	if vm != bindings.HV_SUCCESS {
		bindings.OsRelease(uintptr(vmConfig))
		// HV_UNSUPPORTED, HV_DENIED, HV_NO_DEVICE, and HV_ERROR typically indicate
		// the hypervisor is not available (no entitlements, running in CI, etc.)
		switch vm {
		case bindings.HV_UNSUPPORTED, bindings.HV_DENIED, bindings.HV_NO_DEVICE, bindings.HV_ERROR:
			return nil, fmt.Errorf("%w: %s", hv.ErrHypervisorUnsupported, vm)
		default:
			return nil, fmt.Errorf("failed to create VM: %s", vm)
		}
	}
	bindings.OsRelease(uintptr(vmConfig))

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

	// Remove main memory from additionalMemory tracking since it's tracked separately
	// in v.memory and cleaned up by the main Close() logic.
	if len(ret.additionalMemory) > 0 {
		ret.additionalMemory = ret.additionalMemory[:len(ret.additionalMemory)-1]
	}

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
			bindings.OsRelease(uintptr(cfg))
			return nil, fmt.Errorf("failed to set GIC distributor base: %#x is not aligned to %#x", distributorBase, distributorBaseAlignment)
		}
		if uintptr(redistributorBase)%uintptr(redistributorBaseAlignment) != 0 {
			bindings.OsRelease(uintptr(cfg))
			return nil, fmt.Errorf("failed to set GIC redistributor base: %#x is not aligned to %#x", redistributorBase, redistributorBaseAlignment)
		}

		if err := bindings.HvGicConfigSetDistributorBase(cfg, distributorBase); err != bindings.HV_SUCCESS {
			bindings.OsRelease(uintptr(cfg))
			return nil, fmt.Errorf("failed to set GIC distributor base: %s", err)
		}
		if err := bindings.HvGicConfigSetRedistributorBase(cfg, redistributorBase); err != bindings.HV_SUCCESS {
			bindings.OsRelease(uintptr(cfg))
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
			bindings.OsRelease(uintptr(cfg))
			return nil, fmt.Errorf("failed to create GICv3: %s", err)
		}
		bindings.OsRelease(uintptr(cfg))

		ret.rec.Record(tsHvfGicCreate)
	}

	// Call the callback to allow the user to perform any additional initialization.
	if err := config.Callbacks().OnCreateVMWithMemory(ret); err != nil {
		return nil, fmt.Errorf("failed to create VM with memory: %w", err)
	}

	ret.rec.Record(tsHvfOnCreateVMWithMemory)

	// create vCPUs
	if config.CPUCount() != 1 {
		return nil, fmt.Errorf("hvf: only 1 vCPU supported, got %d", config.CPUCount())
	}

	for i := 0; i < config.CPUCount(); i++ {
		vcpu := &virtualCPU{
			vm:        ret,
			runQueue:  make(chan func(), 16),
			initError: make(chan error, 1),
			rec:       timeslice.NewState(),
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
