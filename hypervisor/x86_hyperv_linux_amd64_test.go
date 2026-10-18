//go:build linux && amd64

package hypervisor

import (
	"context"
	"encoding/binary"
	"j5.nz/cc/hypervisor/x86state"
	"os"
	"testing"
	"time"
)

// Exercise guest WRMSR/RDMSR and the kernel-written reference page, not just
// userspace MSR ioctls or feature-bit construction.
func TestX86HyperVReferenceClock(t *testing.T) {
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
	supported, err := cpu.(X86HyperV).SupportedHyperVCPUID()
	if err != nil {
		t.Fatal(err)
	}
	if len(supported) == 0 {
		t.Skip("kernel lacks Hyper-V CPUID discovery")
	}
	entries, err := cpu.SupportedCPUID()
	if err != nil {
		t.Fatal(err)
	}
	filtered := entries[:0]
	for _, e := range entries {
		if e.Function < 0x40000000 || e.Function >= 0x80000000 {
			filtered = append(filtered, e)
		}
	}
	for _, e := range supported {
		if e.Function > 0x40000005 {
			continue
		}
		if e.Function == 0x40000000 {
			e.Eax = 0x40000005
		}
		if e.Function == 0x40000003 {
			e.Eax &= 1<<1 | 1<<5 | 1<<9
			e.Ebx = 0
			e.Ecx = 0
			e.Edx = 0
		}
		if e.Function == 0x40000004 {
			e.Eax = 0
			e.Ebx = 0xffffffff
			e.Ecx = 0
			e.Edx = 0
		}
		filtered = append(filtered, e)
	}
	if err := cpu.SetCPUID(filtered); err != nil {
		t.Fatal(err)
	}
	ram, err := cpu.MapRAM(0, 2<<20)
	if err != nil {
		t.Fatal(err)
	}
	var code []byte
	wrmsr := func(msr uint32, value uint64) {
		code = append(code, 0xb9)
		code = binary.LittleEndian.AppendUint32(code, msr)
		code = append(code, 0xb8)
		code = binary.LittleEndian.AppendUint32(code, uint32(value))
		code = append(code, 0xba)
		code = binary.LittleEndian.AppendUint32(code, uint32(value>>32))
		code = append(code, 0x0f, 0x30)
	}
	wrmsr(0x40000000, 0x8100000000000001)
	wrmsr(0x40000021, 0x9001)
	code = append(code, 0xb9, 0x20, 0, 0, 0x40, 0x0f, 0x32, 0xe6, 0xf1, 0x0f, 0x32, 0xe6, 0xf2, 0xeb, 0xfe)
	copy(ram[0x1000:], code)
	s, err := cpu.SystemRegisters()
	if err != nil {
		t.Fatal(err)
	}
	seg := x86state.Segment{Limit: 0xffffffff, Present: 1, S: 1, Type: 3, Db: 1, G: 1}
	s.Cs, s.Ds, s.Es, s.Ss = seg, seg, seg, seg
	s.Cs.Type = 11
	s.Cr0 = 0x11
	if err := cpu.SetSystemRegisters(s); err != nil {
		t.Fatal(err)
	}
	if err := cpu.SetRegisters(x86state.Registers{Rip: 0x1000, Rflags: 2}); err != nil {
		t.Fatal(err)
	}
	var previous uint64
	for _, port := range []uint16{0xf1, 0xf2} {
		ex, err := cpu.Run(ctx)
		if err != nil || ex.Reason != X86ExitIO || ex.Port != port {
			t.Fatalf("clock guest: %+v %v", ex, err)
		}
		r, err := cpu.Registers()
		if err != nil {
			t.Fatal(err)
		}
		now := r.Rdx<<32 | uint64(uint32(r.Rax))
		if now <= previous {
			t.Fatalf("reference time did not advance: %d <= %d", now, previous)
		}
		previous = now
		if port == 0xf1 {
			time.Sleep(time.Millisecond)
			if err := cpu.CompleteIO(); err != nil {
				t.Fatal(err)
			}
		}
	}
	seq := binary.LittleEndian.Uint32(ram[0x9000:])
	scale := binary.LittleEndian.Uint64(ram[0x9008:])
	if seq == 0 || seq == 0xffffffff || scale == 0 {
		t.Fatalf("invalid reference page: sequence=%#x scale=%#x", seq, scale)
	}
}
