//go:build linux

package kvm

import "fmt"

type kvmUserspaceMemoryRegion struct {
	Slot          uint32
	Flags         uint32
	GuestPhysAddr uint64
	MemorySize    uint64
	UserspaceAddr uint64
}

const syncRegsSizeBytes = 2048

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
