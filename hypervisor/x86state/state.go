// Package x86state describes architectural x86 CPU state without host resources.
package x86state

type Registers struct {
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

type Segment struct {
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

type DescriptorTable struct {
	Base    uint64
	Limit   uint16
	Padding [3]uint16
}

type SystemRegisters struct {
	Cs, Ds, Es, Fs, Gs, Ss Segment
	Tr, Ldt                Segment
	Gdt, Idt               DescriptorTable
	Cr0                    uint64
	Cr2                    uint64
	Cr3                    uint64
	Cr4                    uint64
	Cr8                    uint64
	Efer                   uint64
	ApicBase               uint64
	InterruptBitmap        [4]uint64
}
