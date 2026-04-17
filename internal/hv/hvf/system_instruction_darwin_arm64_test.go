//go:build darwin && arm64

package hvf

import "testing"

func encodeSystemInstructionSyndrome(read bool, op0, op1, op2, crn, crm, rt uint8) uint64 {
	var direction uint64
	if read {
		direction = 1
	}
	iss := (uint64(op0) << 20) |
		(uint64(op2) << 17) |
		(uint64(op1) << 14) |
		(uint64(crn) << 10) |
		(uint64(rt) << 5) |
		(uint64(crm) << 1) |
		direction
	return (uint64(ExceptionClassSystemRegister) << 26) | (1 << 25) | iss
}

func TestDecodeSystemInstructionDITRegisterAccess(t *testing.T) {
	syndrome := encodeSystemInstructionSyndrome(true, 0x3, 0x3, 0x5, 0x4, 0x2, 0x0)
	info, err := DecodeSystemInstruction(syndrome)
	if err != nil {
		t.Fatalf("DecodeSystemInstruction() error = %v", err)
	}
	if !info.Read {
		t.Fatalf("expected read access")
	}
	if !info.IsDITRegisterAccess() {
		t.Fatalf("expected DIT register access, got %+v", info)
	}
	if info.RawRt != 0 {
		t.Fatalf("unexpected Rt = %d", info.RawRt)
	}
}

func TestDecodeSystemInstructionDITImmediateAccess(t *testing.T) {
	syndrome := encodeSystemInstructionSyndrome(false, 0x0, 0x3, 0x2, 0x4, 0x1, 0x1f)
	info, err := DecodeSystemInstruction(syndrome)
	if err != nil {
		t.Fatalf("DecodeSystemInstruction() error = %v", err)
	}
	if info.Read {
		t.Fatalf("expected write access")
	}
	if !info.IsDITImmediateAccess() {
		t.Fatalf("expected DIT immediate access, got %+v", info)
	}
	if got := info.ImmediateValue(); got != 1 {
		t.Fatalf("ImmediateValue() = %d, want 1", got)
	}
	if info.Rt != hvRegXZR {
		t.Fatalf("expected XZR for Rt 31, got %v", info.Rt)
	}
}

func TestDecodeSystemInstructionOSDLREL1Access(t *testing.T) {
	syndrome := encodeSystemInstructionSyndrome(false, 0x2, 0x0, 0x4, 0x1, 0x3, 0x1f)
	info, err := DecodeSystemInstruction(syndrome)
	if err != nil {
		t.Fatalf("DecodeSystemInstruction() error = %v", err)
	}
	if !info.IsOSDLREL1Access() {
		t.Fatalf("expected OSDLR_EL1 access, got %+v", info)
	}
	if info.Read {
		t.Fatalf("expected write access")
	}
}
