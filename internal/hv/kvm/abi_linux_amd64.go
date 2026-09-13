//go:build linux && amd64

package kvm

import "j5.nz/cc/hypervisor/x86state"

const (
	kvmNrInterrupts = 256
)

type kvmUserspaceMemoryRegion struct {
	Slot          uint32
	Flags         uint32
	GuestPhysAddr uint64
	MemorySize    uint64
	UserspaceAddr uint64
}

type internalErrorSubReason uint32

const (
	internalErrorEmulation            internalErrorSubReason = 1
	internalErrorSimulEx              internalErrorSubReason = 2
	internalErrorDeliveryEv           internalErrorSubReason = 3
	internalErrorUnexpectedExitReason internalErrorSubReason = 4
)

type internalError struct {
	Suberror internalErrorSubReason
	Ndata    uint32
	Data     [16]uint64
}

const syncRegsSizeBytes = 2048

type kvmRunData struct {
	requestInterruptWindow     uint8
	immediateExit              uint8
	padding1                   [6]uint8
	exitReason                 uint32
	readyForInterruptInjection uint8
	ifFlag                     uint8
	flags                      uint16
	cr8                        uint64
	apicBase                   uint64
	anon0                      [256]byte
	kvmValidRegs               uint64
	kvmDirtyRegs               uint64
	s                          struct{ padding [syncRegsSizeBytes]byte }
}

type kvmExitIoData struct {
	direction  uint8
	size       uint8
	port       uint16
	count      uint32
	dataOffset uint64
}

type kvmExitMMIOData struct {
	physAddr uint64
	data     [8]byte
	len      uint32
	isWrite  uint8
}

type kvmSystemEvent struct {
	typ   uint32
	ndata uint32
	data  [16]uint64
}

type kvmIRQLevel struct {
	IRQ   uint32
	Level uint32
}

type kvmMSREntry struct {
	Index    uint32
	Reserved uint32
	Data     uint64
}

type kvmMSRs1 struct {
	NMSRs uint32
	Pad   uint32
	Entry kvmMSREntry
}

type kvmFPU struct {
	FPR        [8][16]byte
	FCW        uint16
	FSW        uint16
	FTWX       uint8
	Pad1       uint8
	LastOpcode uint16
	LastIP     uint64
	LastDP     uint64
	XMM        [16][16]byte
	MXCSR      uint32
	Pad2       uint32
}

type kvmLAPICState struct {
	Regs [1024]byte
}

type kvmMPState struct {
	MPState uint32
}

type kvmVCPUEvents struct {
	Exception struct {
		Injected     uint8
		Nr           uint8
		HasErrorCode uint8
		Pending      uint8
		ErrorCode    uint32
	}
	Interrupt struct {
		Injected uint8
		Nr       uint8
		Soft     uint8
		Shadow   uint8
	}
	NMI struct {
		Injected uint8
		Pending  uint8
		Masked   uint8
		Pad      uint8
	}
	SIPIVector uint32
	Flags      uint32
	SMI        struct {
		SMM          uint8
		Pending      uint8
		SMMInsideNMI uint8
		LatchedInit  uint8
	}
	TripleFault struct {
		Pending uint8
	}
	Reserved            [26]byte
	ExceptionHasPayload uint8
	ExceptionPayload    uint64
}

type kvmDebugRegs struct {
	DB       [4]uint64
	DR6      uint64
	DR7      uint64
	Flags    uint64
	Reserved [9]uint64
}

type kvmXSAVE struct {
	Region [1024]uint32
}

type kvmXCR struct {
	XCR      uint32
	Reserved uint32
	Value    uint64
}

type kvmXCRS struct {
	NrXCRs  uint32
	Flags   uint32
	XCRs    [16]kvmXCR
	Padding [16]uint64
}

type kvmIRQChip struct {
	ChipID uint32
	Pad    uint32
	Chip   [512]byte
}

type kvmPITChannelState struct {
	Count         uint32
	LatchedCount  uint16
	CountLatched  uint8
	StatusLatched uint8
	Status        uint8
	ReadState     uint8
	WriteState    uint8
	WriteLatch    uint8
	RWMode        uint8
	Mode          uint8
	BCD           uint8
	Gate          uint8
	CountLoadTime int64
}

type kvmPITState2 struct {
	Channels [3]kvmPITChannelState
	Flags    uint32
	Reserved [9]uint32
}

type kvmClockData struct {
	Clock    uint64
	Flags    uint32
	Pad0     uint32
	Realtime uint64
	HostTSC  uint64
	Pad      [4]uint32
}

type kvmRegs = x86state.Registers
type kvmSegment = x86state.Segment
type kvmDTable = x86state.DescriptorTable
type kvmSRegs = x86state.SystemRegisters

type kvmCPUIDEntry2 struct {
	Function uint32
	Index    uint32
	Flags    uint32
	Eax      uint32
	Ebx      uint32
	Ecx      uint32
	Edx      uint32
	Padding  [3]uint32
}

type kvmCPUID2 struct {
	Nr      uint32
	Padding uint32
}

type kvmPitConfig struct {
	Flags uint32
	Pad   [15]uint32
}
