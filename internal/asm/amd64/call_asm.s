//go:build linux && amd64

#include "textflag.h"

TEXT ·callAssemblyEntry(SB), NOSPLIT, $0-8
	MOVQ entry+0(FP), AX
	CALL AX
	RET

TEXT ·callAssemblyEntryWithArgs(SB), NOSPLIT, $0-32
	MOVQ entry+0(FP), AX
	MOVQ args+8(FP), BX
	MOVQ nargs+16(FP), R10

	CMPQ R10, $0
	JE call
	MOVQ (BX), DI

	CMPQ R10, $1
	JE call
	MOVQ 8(BX), SI

	CMPQ R10, $2
	JE call
	MOVQ 16(BX), DX

	CMPQ R10, $3
	JE call
	MOVQ 24(BX), CX

	CMPQ R10, $4
	JE call
	MOVQ 32(BX), R8

	CMPQ R10, $5
	JE call
	MOVQ 40(BX), R9

call:
	CALL AX
	MOVQ AX, ret+24(FP)
	RET
