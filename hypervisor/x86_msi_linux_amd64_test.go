//go:build linux && amd64

package hypervisor

import (
	"context"
	"j5.nz/cc/hypervisor/x86state"
	"os"
	"testing"
	"time"
)

func TestX86MSIReachesLocalAPIC(t *testing.T) {
	if _, err := os.Stat("/dev/kvm"); err != nil {
		t.Skip("KVM unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
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
	// Enable the local APIC, then stop at OUT with IF clear so IRR stays pending.
	copy(ram[0x1000:], []byte{0xc7, 0x05, 0xf0, 0x00, 0xe0, 0xfe, 0xff, 0x01, 0, 0, 0xe6, 0xf1, 0xeb, 0xfe})
	s, err := cpu.SystemRegisters()
	if err != nil {
		t.Fatal(err)
	}
	seg := x86state.Segment{Limit: 0xffffffff, Present: 1, S: 1, Type: 3, Db: 1, G: 1}
	s.Cs = seg
	s.Cs.Type = 11
	s.Ds = seg
	s.Es = seg
	s.Ss = seg
	s.Cr0 = 0x11
	if err := cpu.SetSystemRegisters(s); err != nil {
		t.Fatal(err)
	}
	if err := cpu.SetRegisters(x86state.Registers{Rip: 0x1000, Rflags: 2}); err != nil {
		t.Fatal(err)
	}
	ex, err := cpu.Run(ctx)
	if err != nil || ex.Reason != X86ExitIO {
		t.Fatalf("guest APIC initialization: %+v %v", ex, err)
	}
	if err := cpu.(interface{ SetMSI(uint64, uint32) error }).SetMSI(0xfee00000, 0x51); err != nil {
		t.Fatal(err)
	}
	state, err := cpu.(interface {
		InterruptState() (map[string]uint64, error)
	}).InterruptState()
	if err != nil {
		t.Fatal(err)
	}
	if state["lapic0.irr2"]&(1<<17) == 0 {
		t.Fatalf("MSI vector 0x51 not pending: %x", state["lapic0.irr2"])
	}
}
