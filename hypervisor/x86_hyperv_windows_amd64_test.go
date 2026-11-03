//go:build windows && amd64

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
	if os.Getenv("CC_WHP_TEST") != "1" {
		t.Skip("set CC_WHP_TEST=1 for native WHP execution")
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
		t.Skip("WHP lacks Hyper-V clock support")
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
	code = append(code, 0xb9, 0x21, 0, 0, 0x40, 0x0f, 0x32, 0xa3, 0x00, 0xa0, 0, 0)
	// WHP overlays this GPA with a read-only hypervisor page. Read it inside
	// the guest: the caller's original RAM backing is not the overlay's data.
	for _, pair := range [][2]uint32{{0x9000, 0xa100}, {0x9008, 0xa108}, {0x900c, 0xa10c}} {
		code = append(code, 0xa1)
		code = binary.LittleEndian.AppendUint32(code, pair[0])
		code = append(code, 0xa3)
		code = binary.LittleEndian.AppendUint32(code, pair[1])
	}
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
	seq := binary.LittleEndian.Uint32(ram[0xa100:])
	scale := binary.LittleEndian.Uint64(ram[0xa108:])
	if seq == 0 || seq == 0xffffffff || scale == 0 {
		t.Fatalf("invalid reference page: sequence=%#x scale=%#x MSR=%#x", seq, scale, binary.LittleEndian.Uint32(ram[0xa000:]))
	}
}

func TestWHPHyperVSyntheticTimer(t *testing.T) {
	if os.Getenv("CC_WHP_TEST") != "1" {
		t.Skip("set CC_WHP_TEST=1 for native WHP execution")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cpu, err := NewX86(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer cpu.Close()
	enabled, err := cpu.(X86HyperVSynIC).EnableHyperVSynIC()
	if err != nil {
		t.Fatal(err)
	}
	if !enabled {
		t.Skip("WHP synthetic timers unavailable")
	}
	entries, err := cpu.(X86HyperV).SupportedHyperVCPUID()
	if err != nil {
		t.Fatal(err)
	}
	direct := false
	for _, e := range entries {
		if e.Function == 0x40000003 {
			direct = e.Edx&(1<<19) != 0
		}
	}
	if !direct {
		t.Skip("WHP direct synthetic timers unavailable")
	}
	if err = cpu.SetCPUID(entries); err != nil {
		t.Fatal(err)
	}
	ram, err := cpu.MapRAM(0, 2<<20)
	if err != nil {
		t.Fatal(err)
	}
	binary.LittleEndian.PutUint64(ram[0x2008:], 0x00cf9b000000ffff)
	binary.LittleEndian.PutUint64(ram[0x2010:], 0x00cf93000000ffff)
	gate := ram[0x3000+0x51*8:]
	binary.LittleEndian.PutUint16(gate, 0x6000)
	binary.LittleEndian.PutUint16(gate[2:], 8)
	gate[5] = 0x8e
	copy(ram[0x6000:], []byte{0xb0, 0x77, 0xe6, 0xf1, 0xeb, 0xfe})
	code := []byte{0xc7, 0x05, 0xf0, 0x00, 0xe0, 0xfe, 0xff, 0x01, 0, 0}
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
	wrmsr(0x40000080, 1)
	wrmsr(0x400000b1, 10000)
	wrmsr(0x400000b0, 1<<12|0x51<<4|3)
	for n, msr := range []uint32{0x400000b0, 0x400000b1, 0x40000020} {
		code = append(code, 0xb9)
		code = binary.LittleEndian.AppendUint32(code, msr)
		code = append(code, 0x0f, 0x32, 0xa3)
		code = binary.LittleEndian.AppendUint32(code, uint32(0x9000+n*4))
	}
	code = append(code, 0xfb, 0xf4, 0xeb, 0xfd)
	copy(ram[0x1000:], code)
	s, err := cpu.SystemRegisters()
	if err != nil {
		t.Fatal(err)
	}
	seg := x86state.Segment{Limit: 0xffffffff, Selector: 16, Present: 1, S: 1, Type: 3, Db: 1, G: 1}
	s.Cs, s.Ds, s.Es, s.Ss, s.Fs, s.Gs = seg, seg, seg, seg, seg, seg
	s.Cs.Selector, s.Cs.Type = 8, 11
	s.Cr0, s.Cr3, s.Cr4, s.Efer = 0x11, 0, 0, 0
	s.Gdt = x86state.DescriptorTable{Base: 0x2000, Limit: 23}
	s.Idt = x86state.DescriptorTable{Base: 0x3000, Limit: 2047}
	if err = cpu.SetSystemRegisters(s); err != nil {
		t.Fatal(err)
	}
	if err = cpu.SetRegisters(x86state.Registers{Rip: 0x1000, Rsp: 0x8000, Rflags: 2}); err != nil {
		t.Fatal(err)
	}
	ex, err := cpu.Run(ctx)
	if err != nil || ex.Reason != X86ExitIO || ex.Port != 0xf1 || ex.Data[0] != 0x77 {
		counters, _ := cpu.(interface {
			ExecutionCounters() (map[string]uint64, error)
		}).ExecutionCounters()
		t.Fatalf("synthetic timer interrupt: %+v %v config=%#x count=%d clock=%d counters=%v", ex, err, binary.LittleEndian.Uint32(ram[0x9000:]), binary.LittleEndian.Uint32(ram[0x9004:]), binary.LittleEndian.Uint32(ram[0x9008:]), counters)
	}
	if err = cpu.CompleteIO(); err != nil {
		t.Fatal(err)
	}
	counters, err := cpu.(interface {
		ExecutionCounters() (map[string]uint64, error)
	}).ExecutionCounters()
	if err != nil || counters["runtime_ns"] == 0 || counters["exit_X64IoPortAccess"] == 0 {
		t.Fatalf("native execution counters: %v %v", counters, err)
	}
}
