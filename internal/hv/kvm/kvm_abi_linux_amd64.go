//go:build linux && amd64

package kvm

const (
	kvmNrInterrupts = 256
	kvmAPICRegSize  = 0x400
	kvmMaxXCRS      = 16
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
