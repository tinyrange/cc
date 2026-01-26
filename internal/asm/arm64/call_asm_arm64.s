//go:build (darwin || linux) && arm64

#include "textflag.h"

// func callAssemblyEntry(entry uintptr)
TEXT ·callAssemblyEntry(SB), NOSPLIT, $0-8
	MOVD entry+0(FP), R9
	BL (R9)
	RET

// func callAssemblyEntryWithArgs(entry uintptr, args *uintptr, nargs uintptr) uintptr
// Uses AAPCS64 calling convention: arguments in X0-X7
TEXT ·callAssemblyEntryWithArgs(SB), NOSPLIT, $0-32
	MOVD entry+0(FP), R9     // entry address
	MOVD args+8(FP), R10     // pointer to args array
	MOVD nargs+16(FP), R11   // number of arguments

	// Load arguments into X0-X7 based on nargs
	CMP $0, R11
	BEQ call
	MOVD (R10), R0

	CMP $1, R11
	BEQ call
	MOVD 8(R10), R1

	CMP $2, R11
	BEQ call
	MOVD 16(R10), R2

	CMP $3, R11
	BEQ call
	MOVD 24(R10), R3

	CMP $4, R11
	BEQ call
	MOVD 32(R10), R4

	CMP $5, R11
	BEQ call
	MOVD 40(R10), R5

	CMP $6, R11
	BEQ call
	MOVD 48(R10), R6

	CMP $7, R11
	BEQ call
	MOVD 56(R10), R7

call:
	// Save LR before call (BL will overwrite it)
	// The called code should preserve X29/X30 per AAPCS64
	BL (R9)
	// Result is in X0/R0
	MOVD R0, ret+24(FP)
	RET

// func clearInstructionCache(addr uintptr, size uintptr)
// Clears the instruction cache for the given memory region.
// On ARM64, we need to ensure data written to memory is visible to the instruction fetch unit.
TEXT ·clearInstructionCache(SB), NOSPLIT, $0-16
	MOVD addr+0(FP), R0
	MOVD size+8(FP), R1
	ADD R0, R1, R1           // R1 = end address

	// Data cache clean and instruction cache invalidate
	// We clean by cache line (typically 64 bytes on Apple Silicon)
loop:
	CMP R0, R1
	BHS done

	// DC CVAU - Clean data cache by VA to PoU
	WORD $0xd50b7b20         // dc cvau, x0

	ADD $64, R0, R0          // Move to next cache line
	B loop

done:
	// Data Synchronization Barrier - ensure all cache maintenance completes
	WORD $0xd5033f9f         // dsb ish

	// Instruction Synchronization Barrier - ensure instruction fetch sees new data
	WORD $0xd5033fdf         // isb

	RET
