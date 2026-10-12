//go:build linux && amd64

package hypervisor

import (
	"context"
	"j5.nz/cc/hypervisor/x86state"
	"os"
	"testing"
	"time"
)

func TestNativeSingleStepIOCompletionDoesNotRunNextInstruction(t *testing.T) {
	if _, err := os.Stat("/dev/kvm"); err != nil {
		t.Skip("KVM unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cpu, err := NewX86(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer cpu.Close()
	ram, err := cpu.MapRAM(0, 2<<20)
	if err != nil {
		t.Fatal(err)
	}
	copy(ram[0x7c00:], []byte{0xb8, 0x34, 0x12, 0xe6, 0xf2, 0xbb, 0x78, 0x56, 0xe6, 0xf2, 0xeb, 0xfe})
	s, err := cpu.SystemRegisters()
	if err != nil {
		t.Fatal(err)
	}
	seg := x86state.Segment{Limit: 0xffff, Present: 1, S: 1, Type: 3}
	s.Cs, s.Ds, s.Es, s.Ss, s.Fs, s.Gs = seg, seg, seg, seg, seg, seg
	s.Cs.Type = 11
	s.Cr0 = 0x10
	s.Cr3 = 0
	s.Cr4 = 0
	s.Efer = 0
	if err = cpu.SetSystemRegisters(s); err != nil {
		t.Fatal(err)
	}
	if err = cpu.SetRegisters(x86state.Registers{Rip: 0x7c00, Rsp: 0x7c00, Rflags: 2}); err != nil {
		t.Fatal(err)
	}
	step := cpu.(interface{ SetSingleStep(bool) error })
	if err = step.SetSingleStep(true); err != nil {
		t.Fatal(err)
	}
	defer step.SetSingleStep(false)
	next := func() X86Exit {
		for {
			ex, err := cpu.Run(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if ex.Reason != 0 {
				return ex
			}
		}
	}
	if ex := next(); ex.Reason != X86ExitDebug {
		t.Fatalf("first step %+v", ex)
	}
	r, err := cpu.Registers()
	if err != nil {
		t.Fatal(err)
	}
	if r.Rip != 0x7c03 || r.Rax != 0x1234 || r.Rbx != 0 {
		t.Fatal("MOV step advanced too far", r)
	}
	if ex := next(); ex.Reason != X86ExitIO || ex.Port != 0xf2 {
		t.Fatalf("OUT step %+v", ex)
	}
	if err = cpu.CompleteIO(); err != nil {
		t.Fatal(err)
	}
	r, err = cpu.Registers()
	if err != nil {
		t.Fatal(err)
	}
	if r.Rip != 0x7c05 || r.Rbx != 0 {
		t.Fatal("IO completion ran following MOV", r)
	}
	if ex := next(); ex.Reason != X86ExitDebug {
		t.Fatalf("second MOV step %+v", ex)
	}
	r, err = cpu.Registers()
	if err != nil {
		t.Fatal(err)
	}
	if r.Rip != 0x7c08 || r.Rbx != 0x5678 {
		t.Fatal("second MOV did not step exactly once", r)
	}
}
