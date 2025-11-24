//go:build linux && amd64

package kvm

import "fmt"

const (
	kvmNrInterrupts   = 256
	kvmAPICRegSize    = 0x400
	kvmMaxXCRS        = 16
	syncRegsSizeBytes = 2048
)

type kvmRegs struct {
	Rax    uint64
	Rbx    uint64
	Rcx    uint64
	Rdx    uint64
	Rsi    uint64
	Rdi    uint64
	Rsp    uint64
	Rbp    uint64
	R8     uint64
	R9     uint64
	R10    uint64
	R11    uint64
	R12    uint64
	R13    uint64
	R14    uint64
	R15    uint64
	Rip    uint64
	Rflags uint64
}

type kvmSegment struct {
	Base     uint64
	Limit    uint32
	Selector uint16
	Type     uint8
	Present  uint8
	Dpl      uint8
	Db       uint8
	S        uint8
	L        uint8
	G        uint8
	Avl      uint8
	Unusable uint8
	Padding  uint8
}

type kvmDTable struct {
	Base    uint64
	Limit   uint16
	Padding [3]uint16
}

type kvmSRegs struct {
	Cs, Ds, Es, Fs, Gs, Ss kvmSegment
	Tr, Ldt                kvmSegment
	Gdt, Idt               kvmDTable
	Cr0                    uint64
	Cr2                    uint64
	Cr3                    uint64
	Cr4                    uint64
	Cr8                    uint64
	Efer                   uint64
	ApicBase               uint64
	InterruptBitmap        [(kvmNrInterrupts + 63) / 64]uint64
}

type kvmFPU struct {
	Fpr        [8][16]uint8
	Fcw        uint16
	Fsw        uint16
	Ftwx       uint8
	Pad1       uint8
	LastOpcode uint16
	LastIP     uint64
	LastDP     uint64
	Xmm        [16][16]uint8
	Mxcsr      uint32
	Pad2       uint32
}

type kvmXsave struct {
	Region [1024]uint32
}

type kvmXcr struct {
	Xcr      uint32
	Reserved uint32
	Value    uint64
}

type kvmXcrs struct {
	NrXcrs  uint32
	Flags   uint32
	Xcrs    [kvmMaxXCRS]kvmXcr
	Padding [16]uint64
}

type kvmLapicState struct {
	Regs [kvmAPICRegSize]byte
}

type kvmMsrEntry struct {
	Index    uint32
	Reserved uint32
	Data     uint64
}

type kvmMsrs struct {
	Nmsrs uint32
	Pad   uint32
}

type kvmMsrList struct {
	Nmsrs uint32
}

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

type kvmClockData struct {
	Clock    uint64
	Flags    uint32
	Pad0     uint32
	Realtime uint64
	HostTSC  uint64
	Pad      [4]uint32
}

type kvmIRQChip struct {
	ChipID uint32
	Pad    uint32
	Chip   [512]byte
}

type kvmPitChannelState struct {
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
	Bcd           uint8
	Gate          uint8
	CountLoadTime int64
}

type kvmPitState2 struct {
	Channels [3]kvmPitChannelState
	Flags    uint32
	Reserved [9]uint32
}

type kvmIRQLevel struct {
	IRQOrStatus uint32
	Level       uint32
}

type kvmMSI struct {
	AddressLo uint32
	AddressHi uint32
	Data      uint32
	Flags     uint32
	Devid     uint32
	Pad       [12]uint8
}

type internalErrorSubReason uint32

const (
	internalErrorEmulation            internalErrorSubReason = 1
	internalErrorSimulEx              internalErrorSubReason = 2
	internalErrorDeliveryEv           internalErrorSubReason = 3
	internalErrorUnexpectedExitReason internalErrorSubReason = 4
)

func (k internalErrorSubReason) String() string {
	switch k {
	case internalErrorEmulation:
		return "KVM_INTERNAL_ERROR_EMULATION"
	case internalErrorSimulEx:
		return "KVM_INTERNAL_ERROR_SIMUL_EX"
	case internalErrorDeliveryEv:
		return "KVM_INTERNAL_ERROR_DELIVERY_EV"
	case internalErrorUnexpectedExitReason:
		return "KVM_INTERNAL_ERROR_UNEXPECTED_EXIT_REASON"
	default:
		return fmt.Sprintf("KVMInternalErrorSubreason(%d)", uint32(k))
	}
}

type internalError struct {
	Suberror internalErrorSubReason
	Ndata    uint32
	Data     [16]uint64
}

type kvmRunData struct {
	request_interrupt_window      uint8
	immediate_exit                uint8
	padding1                      [6]uint8
	exit_reason                   uint32
	ready_for_interrupt_injection uint8
	if_flag                       uint8
	flags                         uint16
	cr8                           uint64
	apic_base                     uint64
	anon0                         [256]byte
	kvm_valid_regs                uint64
	kvm_dirty_regs                uint64
	s                             struct{ padding [syncRegsSizeBytes]byte }
}

type kvmExitIoData struct {
	// __u8 direction;
	// __u8 size; /* bytes */
	// __u16 port;
	// __u32 count;
	// __u64 data_offset; /* relative to kvm_run start */
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
