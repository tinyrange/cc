//go:build linux

package kvm

import "fmt"

const (
	kvmApiVersion = 12

	kvmGetApiVersion          = 0xae00
	kvmCreateVm               = 0xae01
	kvmGetMsrIndexList        = 0xc004ae02
	kvmCheckExtension         = 0xae03
	kvmGetVcpuMmapSize        = 0xae04
	kvmGetSupportedCpuid      = 0xc008ae05
	kvmGetMsrFeatureIndexList = 0xc004ae0a
	kvmCreateVcpu             = 0xae41
	kvmSetTssAddr             = 0xae47
	kvmRun                    = 0xae80
	kvmCreateIrqchip          = 0xae60
	kvmIrqLine                = 0x4008ae61
	kvmGetIrqchip             = 0xc208ae62
	kvmSetIrqchip             = 0x8208ae63
	kvmCreatePit2             = 0x4040ae77
	kvmSetUserMemoryRegion    = 0x4020ae46
	kvmGetClock               = 0x8030ae7c
	kvmSetClock               = 0x4030ae7b
	kvmSetGsiRouting          = 0x4008ae6a
	kvmGetOneReg              = 0x4010aeab
	kvmSetOneReg              = 0x4010aeac
	kvmGetPit2                = 0x8070ae9f
	kvmSetPit2                = 0x4070aea0
	kvmArmSetDeviceAddr       = 0x4010aeab
	kvmCreateDevice           = 0xc00caee0
	kvmSetDeviceAttr          = 0x4018aee1
	kvmGetDeviceAttr          = 0x4018aee2
	kvmHasDeviceAttr          = 0x4018aee3
	kvmGetRegs                = 0x8090ae81
	kvmSetRegs                = 0x4090ae82
	kvmGetSregs               = 0x8138ae83
	kvmSetSregs               = 0x4138ae84
	kvmGetFpu                 = 0x81a0ae8c
	kvmSetFpu                 = 0x41a0ae8d
	kvmGetXsave               = 0x9000aea4
	kvmSetXsave               = 0x5000aea5
	kvmGetXcrs                = 0x8188aea6
	kvmSetXcrs                = 0x4188aea7
	kvmSignalMsi              = 0x4020aea5
	kvmGetLapic               = 0x8400ae8e
	kvmSetLapic               = 0x4400ae8f
	kvmSetCpuid2              = 0x4008ae90
	kvmGetMsrs                = 0xc008ae88
	kvmSetMsrs                = 0x4008ae89
	kvmArmVcpuInitIoctl       = 0x4020aeae
	kvmArmPreferredTarget     = 0x8020aeaf
	kvmEnableCap              = 0x4068aea3

	kvmCapNrMemslots = 10
)

const (
	kvmDevTypeArmVgicV2 = 5
	kvmDevTypeArmVgicV3 = 7

	kvmCapSplitIrqchip = 121
)

type kvmEnableCapArgs struct {
	Cap   uint32
	Flags uint32
	Args  [4]uint64
}

const (
	kvmArmDeviceIdShift = 16
	kvmArmDeviceVgicV2  = 0
)

const (
	kvmDevArmVgicGrpAddr       = 0
	kvmDevArmVgicGrpDistRegs   = 1
	kvmDevArmVgicGrpCpuRegs    = 2
	kvmDevArmVgicGrpNrIrqs     = 3
	kvmDevArmVgicGrpCtrl       = 4
	kvmDevArmVgicGrpRedistRegs = 5
	kvmDevArmVgicGrpCpuSysRegs = 6
	kvmDevArmVgicGrpLevelInfo  = 7
	kvmDevArmVgicGrpItsRegs    = 8
)

const (
	kvmDevArmVgicCtrlInit = 0
)

const (
	kvmVgicV2AddrTypeDist = 0
	kvmVgicV2AddrTypeCpu  = 1
)

const (
	kvmVgicV3AddrTypeDist   = 2
	kvmVgicV3AddrTypeRedist = 3
)

type kvmExitReason uint32

func (kr kvmExitReason) String() string {
	switch kr {
	case kvmExitUnknown:
		return "KVM_EXIT_UNKNOWN"
	case kvmExitException:
		return "KVM_EXIT_EXCEPTION"
	case kvmExitIo:
		return "KVM_EXIT_IO"
	case kvmExitHypercall:
		return "KVM_EXIT_HYPERCALL"
	case kvmExitDebug:
		return "KVM_EXIT_DEBUG"
	case kvmExitHlt:
		return "KVM_EXIT_HLT"
	case kvmExitMmio:
		return "KVM_EXIT_MMIO"
	case kvmExitIrqWindowOpen:
		return "KVM_EXIT_IRQ_WINDOW_OPEN"
	case kvmExitShutdown:
		return "KVM_EXIT_SHUTDOWN"
	case kvmExitFailEntry:
		return "KVM_EXIT_FAIL_ENTRY"
	case kvmExitIntr:
		return "KVM_EXIT_INTR"
	case kvmExitSetTpr:
		return "KVM_EXIT_SET_TPR"
	case kvmExitTprAccess:
		return "KVM_EXIT_TPR_ACCESS"
	case kvmExitS390Sieic:
		return "KVM_EXIT_S390_SIEIC"
	case kvmExitS390Reset:
		return "KVM_EXIT_S390_RESET"
	case kvmExitNmi:
		return "KVM_EXIT_NMI"
	case kvmExitInternalError:
		return "KVM_EXIT_INTERNAL_ERROR"
	case kvmExitOsi:
		return "KVM_EXIT_OSI"
	case kvmExitPaprHcall:
		return "KVM_EXIT_PAPR_HCALL"
	case kvmExitS390Ucontrol:
		return "KVM_EXIT_S390_UCONTROL"
	case kvmExitWatchdog:
		return "KVM_EXIT_WATCHDOG"
	case kvmExitS390Tsch:
		return "KVM_EXIT_S390_TSCH"
	case kvmExitEpr:
		return "KVM_EXIT_EPR"
	case kvmExitSystemEvent:
		return "KVM_EXIT_SYSTEM_EVENT"
	case kvmExitS390Stsi:
		return "KVM_EXIT_S390_STSI"
	case kvmExitIoapicEoi:
		return "KVM_EXIT_IOAPIC_EOI"
	case kvmExitHyperv:
		return "KVM_EXIT_HYPERV"
	case kvmExitArmNisv:
		return "KVM_EXIT_ARM_NISV"
	case kvmExitX86Rdmsr:
		return "KVM_EXIT_X86_RDMSR"
	case kvmExitX86Wrmsr:
		return "KVM_EXIT_X86_WRMSR"
	case kvmExitDirtyRingFull:
		return "KVM_EXIT_DIRTY_RING_FULL"
	case kvmExitApResetHold:
		return "KVM_EXIT_AP_RESET_HOLD"
	case kvmExitX86BusLock:
		return "KVM_EXIT_X86_BUS_LOCK"
	case kvmExitXen:
		return "KVM_EXIT_XEN"
	case kvmExitRiscvSbi:
		return "KVM_EXIT_RISCV_SBI"
	case kvmExitRiscvCsr:
		return "KVM_EXIT_RISCV_CSR"
	case kvmExitNotify:
		return "KVM_EXIT_NOTIFY"
	case kvmExitLoongarchIocsr:
		return "KVM_EXIT_LOONGARCH_IOCSR"
	case kvmExitMemoryFault:
		return "KVM_EXIT_MEMORY_FAULT"
	case kvmExitTdx:
		return "KVM_EXIT_TDX"
	default:
		return fmt.Sprintf("KVM_EXIT_???(%d)", uint32(kr))
	}
}

const (
	kvmExitUnknown        kvmExitReason = 0
	kvmExitException      kvmExitReason = 1
	kvmExitIo             kvmExitReason = 2
	kvmExitHypercall      kvmExitReason = 3
	kvmExitDebug          kvmExitReason = 4
	kvmExitHlt            kvmExitReason = 5
	kvmExitMmio           kvmExitReason = 6
	kvmExitIrqWindowOpen  kvmExitReason = 7
	kvmExitShutdown       kvmExitReason = 8
	kvmExitFailEntry      kvmExitReason = 9
	kvmExitIntr           kvmExitReason = 10
	kvmExitSetTpr         kvmExitReason = 11
	kvmExitTprAccess      kvmExitReason = 12
	kvmExitS390Sieic      kvmExitReason = 13
	kvmExitS390Reset      kvmExitReason = 14
	kvmExitNmi            kvmExitReason = 16
	kvmExitInternalError  kvmExitReason = 17
	kvmExitOsi            kvmExitReason = 18
	kvmExitPaprHcall      kvmExitReason = 19
	kvmExitS390Ucontrol   kvmExitReason = 20
	kvmExitWatchdog       kvmExitReason = 21
	kvmExitS390Tsch       kvmExitReason = 22
	kvmExitEpr            kvmExitReason = 23
	kvmExitSystemEvent    kvmExitReason = 24
	kvmExitS390Stsi       kvmExitReason = 25
	kvmExitIoapicEoi      kvmExitReason = 26
	kvmExitHyperv         kvmExitReason = 27
	kvmExitArmNisv        kvmExitReason = 28
	kvmExitX86Rdmsr       kvmExitReason = 29
	kvmExitX86Wrmsr       kvmExitReason = 30
	kvmExitDirtyRingFull  kvmExitReason = 31
	kvmExitApResetHold    kvmExitReason = 32
	kvmExitX86BusLock     kvmExitReason = 33
	kvmExitXen            kvmExitReason = 34
	kvmExitRiscvSbi       kvmExitReason = 35
	kvmExitRiscvCsr       kvmExitReason = 36
	kvmExitNotify         kvmExitReason = 37
	kvmExitLoongarchIocsr kvmExitReason = 38
	kvmExitMemoryFault    kvmExitReason = 39
	kvmExitTdx            kvmExitReason = 40
)

const (
	kvmSystemEventShutdown = 1
	kvmSystemEventReset    = 2
	kvmSystemEventCrash    = 3
	kvmSystemEventWakeup   = 4
	kvmSystemEventSuspend  = 5
	kvmSystemEventSevTerm  = 6
	kvmSystemEventTdxFatal = 7
)
