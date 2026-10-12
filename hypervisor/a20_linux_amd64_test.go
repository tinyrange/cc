//go:build linux && amd64

package hypervisor

import (
	"context"
	"j5.nz/cc/hypervisor/x86state"
	"os"
	"testing"
	"time"
)

func TestNativeA20AliasesWritesAndRestoresHighRAM(t *testing.T) {
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
	ram, err := cpu.MapRAMRegions(4<<20, []RAMRegion{{Address: 0, Offset: 0, Size: 0xa0000}, {Address: 0xc0000, Offset: 0xc0000, Size: 0x340000}})
	if err != nil {
		t.Fatal(err)
	}
	gate := cpu.(interface{ SetA20(bool) error })
	ram[0x500], ram[0x100500] = 0x11, 0x22
	// Read high alias, write high alias, read restored high RAM, and finally
	// access the VGA alias hole. Every operation is a native guest instruction.
	copy(ram[0x7c00:], []byte{
		0xb8, 0xff, 0xff, 0x8e, 0xd8, // real-mode DS=FFFF
		0xa0, 0x10, 5, 0xe6, 0xf2,
		0xb0, 0x77, 0xa2, 0x10, 5, 0xe6, 0xf2,
		0xa0, 0x10, 5, 0xe6, 0xf2,
		0xb8, 0x00, 0xb8, 0x8e, 0xd8, 0xa0, 0, 0, 0xe6, 0xf2, 0xeb, 0xfe,
	})
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
	if err = gate.SetA20(false); err != nil {
		t.Fatal(err)
	}
	for _, want := range []byte{0x11, 0x77} {
		ex := next()
		if ex.Reason != X86ExitIO || ex.Port != 0xf2 || ex.Data[0] != want {
			t.Fatalf("alias exit %+v want %x", ex, want)
		}
		if err = cpu.CompleteIO(); err != nil {
			t.Fatal(err)
		}
	}
	if ram[0x500] != 0x77 || ram[0x100500] != 0x22 {
		t.Fatal("gate copied/clobbered high backing")
	}
	if err = gate.SetA20(true); err != nil {
		t.Fatal(err)
	}
	ex := next()
	if ex.Reason != X86ExitIO || ex.Data[0] != 0x22 {
		t.Fatalf("restored high read %+v", ex)
	}
	if err = cpu.CompleteIO(); err != nil {
		t.Fatal(err)
	}
	if err = gate.SetA20(false); err != nil {
		t.Fatal(err)
	}
	ex = next()
	if ex.Reason != X86ExitMMIO || ex.Address != 0xb8000 {
		t.Fatalf("VGA hole lost while A20 disabled: %+v", ex)
	}
}
