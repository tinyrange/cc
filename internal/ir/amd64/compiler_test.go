package amd64

import (
	"testing"

	"github.com/tinyrange/cc/internal/asm"
	"github.com/tinyrange/cc/internal/asm/amd64"
)

func TestReserveRegExclusive(t *testing.T) {
	c := &compiler{
		freeRegs: append([]asm.Variable(nil), initialFreeRegisters...),
		usedRegs: make(map[asm.Variable]bool),
	}

	if !c.reserveReg(amd64.R10) {
		t.Fatalf("expected to reserve R10 on first attempt")
	}
	if c.reserveReg(amd64.R10) {
		t.Fatalf("expected reserveReg to fail for already used register")
	}

	c.freeReg(amd64.R10)
	if !c.reserveReg(amd64.R10) {
		t.Fatalf("expected to reserve R10 after freeing it")
	}
}
