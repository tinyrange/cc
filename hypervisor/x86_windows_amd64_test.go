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

func TestWHPCallerPC(t *testing.T) {
	if os.Getenv("CC_WHP_TEST") != "1" {
		t.Skip("set CC_WHP_TEST=1 for native WHP execution")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cpu, err := NewX86(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer cpu.Close()
	ram, err := cpu.MapRAMRegions(2<<20, []RAMRegion{{Size: 1 << 20}, {Address: 2 << 20, Offset: 1 << 20, Size: 1 << 20}})
	if err != nil {
		t.Fatal(err)
	}
	// Real mode OUT, IN, RAM store, then another OUT. CompleteIO must commit
	// the first operation without executing the next instruction.
	copy(ram[0x1000:], []byte{0xb0, 0x42, 0xe6, 0x80, 0xe4, 0x81, 0xa2, 0x00, 0x20, 0xe6, 0x82, 0xeb, 0xfe})
	s, err := cpu.SystemRegisters()
	if err != nil {
		t.Fatal(err)
	}
	s.Cs.Base = 0
	s.Cs.Selector = 0
	s.Cs.Limit = 0xffff
	s.Cs.Type = 0xb
	s.Cs.Present = 1
	s.Cs.S = 1
	s.Ds = s.Cs
	s.Ds.Type = 3
	s.Es = s.Ds
	s.Ss = s.Ds
	s.Cr0 = 0x10
	s.Efer = 0
	if err = cpu.SetSystemRegisters(s); err != nil {
		t.Fatal(err)
	}
	r, err := cpu.Registers()
	if err != nil {
		t.Fatal(err)
	}
	r.Rip = 0x1000
	r.Rflags = 2
	if err = cpu.SetRegisters(r); err != nil {
		t.Fatal(err)
	}
	ex, err := cpu.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if ex.Reason != X86ExitIO || ex.Port != 0x80 || !ex.Write || len(ex.Data) != 1 || ex.Data[0] != 0x42 {
		t.Fatalf("OUT: %+v", ex)
	}
	if err = cpu.CompleteIO(); err != nil {
		t.Fatal(err)
	}
	r, err = cpu.Registers()
	if err != nil || r.Rip != 0x1004 {
		t.Fatalf("committed RIP=%x err=%v", r.Rip, err)
	}
	ex, err = cpu.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if ex.Port != 0x81 || ex.Write {
		t.Fatalf("IN: %+v", ex)
	}
	ex.Data[0] = 0x73
	ex, err = cpu.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if ex.Port != 0x82 || ex.Data[0] != 0x73 || ram[0x2000] != 0x73 {
		t.Fatalf("IN result: %+v ram=%x", ex, ram[0x2000])
	}
	if err = cpu.CompleteIO(); err != nil {
		t.Fatal(err)
	}
	short, stop := context.WithTimeout(ctx, 20*time.Millisecond)
	defer stop()
	if _, err = cpu.Run(short); err != context.DeadlineExceeded {
		t.Fatalf("cancel: %v", err)
	}
	entries, err := cpu.SupportedCPUID()
	if err != nil || len(entries) == 0 {
		t.Fatalf("supported CPUID: %v", err)
	}
	if err = cpu.SetCPUID([]x86state.CPUIDEntry{{Function: 7, Index: 2, Flags: 1, Eax: 0x12345678}, {Function: 0, Eax: 0x79}}); err != nil {
		t.Fatal(err)
	}
	copy(ram[0x3000:], []byte{0x66, 0xb8, 7, 0, 0, 0, 0x66, 0xb9, 2, 0, 0, 0, 0x0f, 0xa2, 0xe6, 0x83})
	r.Rip = 0x3000
	if err = cpu.SetRegisters(r); err != nil {
		t.Fatal(err)
	}
	ex, err = cpu.Run(ctx)
	if err != nil || ex.Port != 0x83 || ex.Data[0] != 0x78 {
		t.Fatalf("indexed CPUID: %+v %v", ex, err)
	}
	if err = cpu.CompleteIO(); err != nil {
		t.Fatal(err)
	}
	copy(ram[0x3500:], []byte{0x66, 0x31, 0xc0, 0x0f, 0xa2, 0xe6, 0x87})
	r.Rip = 0x3500
	r.Rcx = 2 // Leaf0 ignores the incoming subleaf register.
	if err = cpu.SetRegisters(r); err != nil {
		t.Fatal(err)
	}
	ex, err = cpu.Run(ctx)
	if err != nil || ex.Port != 0x87 || ex.Data[0] != 0x79 {
		t.Fatalf("unindexed CPUID: %+v %v", ex, err)
	}
	if err = cpu.CompleteIO(); err != nil {
		t.Fatal(err)
	}
	// Read-modify-write has two callbacks from one native instruction. Each
	// request must be visible once, and completing a read must not lose a write.
	copy(ram[0x4000:], []byte{0xfe, 0x05, 0x00, 0x00, 0x10, 0x00, 0xe6, 0x84})
	s.Cr0 |= 1
	s.Cs.Db = 1
	s.Cs.Selector = 8
	s.Cs.Limit = 0xffffffff
	s.Ds.Limit = 0xffffffff
	s.Ds.Db = 1
	s.Ds.Selector = 16
	s.Es = s.Ds
	s.Ss = s.Ds
	if err = cpu.SetSystemRegisters(s); err != nil {
		t.Fatal(err)
	}
	r.Rip = 0x4000
	if err = cpu.SetRegisters(r); err != nil {
		t.Fatal(err)
	}
	ex, err = cpu.Run(ctx)
	if err != nil || ex.Reason != X86ExitMMIO || ex.Write || ex.Address != 0x100000 {
		t.Fatalf("MMIO read: %+v %v", ex, err)
	}
	cpu.CompleteMMIORead(0x35, 1)
	ex, err = cpu.Run(ctx)
	if err != nil || !ex.Write || ex.Address != 0x100000 || ex.Data[0] != 0x36 {
		t.Fatalf("MMIO write: %+v %v", ex, err)
	}
	ex, err = cpu.Run(ctx)
	if err != nil || ex.Port != 0x84 {
		t.Fatalf("MMIO completion: %+v %v", ex, err)
	}
	if err = cpu.CompleteIO(); err != nil {
		t.Fatal(err)
	}
	copy(ram[0x5000:], []byte{0xb9, 0xa0, 1, 0, 0, 0x0f, 0x32, 0x0f, 0x30, 0x0f, 0x32, 0xe6, 0x85})
	r.Rip = 0x5000
	if err = cpu.SetRegisters(r); err != nil {
		t.Fatal(err)
	}
	ex, err = cpu.Run(ctx)
	if err != nil || ex.Port != 0x85 || ex.Data[0] != 1 {
		t.Fatalf("MISC_ENABLE: %+v %v", ex, err)
	}
}

func TestWHPFourCPUStartup(t *testing.T) {
	if os.Getenv("CC_WHP_TEST") != "1" {
		t.Skip("set CC_WHP_TEST=1 for native WHP execution")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cpus, err := NewX86WithCPUs(ctx, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer cpus[0].Close()
	ram, err := cpus[0].MapRAM(0, 2<<20)
	if err != nil {
		t.Fatal(err)
	}
	for id, cpu := range cpus {
		r, err := cpu.Registers()
		if err != nil {
			t.Fatal(err)
		}
		r.Rax = uint64(0x100 + id)
		if err = cpu.SetRegisters(r); err != nil {
			t.Fatal(err)
		}
		if err = cpu.SetCPUID([]x86state.CPUIDEntry{{Function: 1, Ebx: uint32(id)<<24 | 4<<16}}); err != nil {
			t.Fatal(err)
		}
	}
	for id, cpu := range cpus {
		r, e := cpu.Registers()
		if e != nil || r.Rax != uint64(0x100+id) {
			t.Fatalf("CPU%d state: %+v %v", id, r, e)
		}
	}
	var code []byte
	store := func(a, v uint32) {
		code = append(code, 0xc7, 0x05)
		code = binary.LittleEndian.AppendUint32(code, a)
		code = binary.LittleEndian.AppendUint32(code, v)
	}
	store(0xfee000f0, 0x1ff)
	store(0xfee00300, 0xc4500)
	code = append(code, 0xe6, 0xf0)
	store(0xfee00300, 0xc4608)
	code = append(code, 0xb0, 0, 0xe6, 0xf1, 0xeb, 0xfe)
	copy(ram[0x1000:], code)
	copy(ram[0x8000:], []byte{0x66, 0xb8, 1, 0, 0, 0, 0x0f, 0xa2, 0x66, 0xc1, 0xeb, 24, 0x88, 0xd8, 0xe6, 0xf1, 0xeb, 0xfe})
	s, err := cpus[0].SystemRegisters()
	if err != nil {
		t.Fatal(err)
	}
	seg := x86state.Segment{Limit: 0xffffffff, Selector: 16, Present: 1, S: 1, Type: 3, Db: 1, G: 1}
	s.Cs, s.Ds, s.Es, s.Ss, s.Fs, s.Gs = seg, seg, seg, seg, seg, seg
	s.Cs.Selector = 8
	s.Cs.Type = 11
	s.Cr0 = 0x11
	s.Cr3 = 0
	s.Cr4 = 0
	s.Efer = 0
	if err = cpus[0].SetSystemRegisters(s); err != nil {
		t.Fatal(err)
	}
	if err = cpus[0].SetRegisters(x86state.Registers{Rip: 0x1000, Rflags: 2}); err != nil {
		t.Fatal(err)
	}
	type result struct {
		id  int
		ex  X86Exit
		err error
	}
	results := make(chan result, 4)
	for id, cpu := range cpus {
		go func(id int, cpu X86) {
			for {
				ex, e := cpu.Run(ctx)
				if e == nil && id == 0 && ex.Reason == X86ExitIO && ex.Port == 0xf0 {
					if e = cpu.CompleteIO(); e != nil {
						results <- result{id, ex, e}
						return
					}
					time.Sleep(10 * time.Millisecond)
					continue
				}
				if e != nil || ex.Reason != 0 {
					results <- result{id, ex, e}
					return
				}
			}
		}(id, cpu)
	}
	for range cpus {
		r := <-results
		if r.err != nil || r.ex.Reason != X86ExitIO || r.ex.Port != 0xf1 || len(r.ex.Data) != 1 || int(r.ex.Data[0]) != r.id {
			t.Errorf("CPU%d startup: %+v %v", r.id, r.ex, r.err)
		} else if err = cpus[r.id].CompleteIO(); err != nil {
			t.Error(err)
		}
	}
}

func TestWHPInterruptWakesHalt(t *testing.T) {
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
	var code []byte
	store := func(a, v uint32) {
		code = append(code, 0xc7, 0x05)
		code = binary.LittleEndian.AppendUint32(code, a)
		code = binary.LittleEndian.AppendUint32(code, v)
	}
	store(0xfee000f0, 0x1ff)
	store(0xfec00000, 0x20)
	store(0xfec00010, 0x51) // pin8, edge, physical BSP
	code = append(code, 0xe6, 0xf0, 0xfb, 0xf4, 0xeb, 0xfd)
	copy(ram[0x1000:], code)
	s, err := cpu.SystemRegisters()
	if err != nil {
		t.Fatal(err)
	}
	seg := x86state.Segment{Limit: 0xffffffff, Selector: 16, Present: 1, S: 1, Type: 3, Db: 1, G: 1}
	s.Cs, s.Ds, s.Es, s.Ss, s.Fs, s.Gs = seg, seg, seg, seg, seg, seg
	s.Cs.Selector = 8
	s.Cs.Type = 11
	s.Cr0 = 0x11
	s.Cr3 = 0
	s.Cr4 = 0
	s.Efer = 0
	s.Gdt = x86state.DescriptorTable{Base: 0x2000, Limit: 23}
	s.Idt = x86state.DescriptorTable{Base: 0x3000, Limit: 2047}
	if err = cpu.SetSystemRegisters(s); err != nil {
		t.Fatal(err)
	}
	if err = cpu.SetRegisters(x86state.Registers{Rip: 0x1000, Rsp: 0x8000, Rflags: 2}); err != nil {
		t.Fatal(err)
	}
	ex, err := cpu.Run(ctx)
	if err != nil || ex.Port != 0xf0 {
		t.Fatalf("setup: %+v %v", ex, err)
	}
	if err = cpu.CompleteIO(); err != nil {
		t.Fatal(err)
	}
	type result struct {
		ex  X86Exit
		err error
	}
	done := make(chan result, 1)
	go func() {
		for {
			ex, e := cpu.Run(ctx)
			if e != nil || ex.Reason != 0 {
				done <- result{ex, e}
				return
			}
		}
	}()
	time.Sleep(10 * time.Millisecond)
	if err = cpu.SetIRQ(8, true); err != nil {
		cancel()
		<-done
		t.Fatal(err)
	}
	r := <-done
	if r.err != nil || r.ex.Port != 0xf1 || r.ex.Data[0] != 0x77 {
		t.Fatalf("IRQ8: %+v %v", r.ex, r.err)
	}
	if err = cpu.CompleteIO(); err != nil {
		t.Fatal(err)
	}
	gate = ram[0x3000+13*8:]
	binary.LittleEndian.PutUint16(gate, 0x6100)
	binary.LittleEndian.PutUint16(gate[2:], 8)
	gate[5] = 0x8e
	copy(ram[0x6100:], []byte{0xb0, 0x66, 0xe6, 0xf2, 0xeb, 0xfe})
	copy(ram[0x7000:], []byte{0xb9, 0xef, 0xbe, 0xad, 0xde, 0x0f, 0x32, 0xeb, 0xfe})
	if err = cpu.SetRegisters(x86state.Registers{Rip: 0x7000, Rsp: 0x8000, Rflags: 2}); err != nil {
		t.Fatal(err)
	}
	ex, err = cpu.Run(ctx)
	if err != nil || ex.Port != 0xf2 || ex.Data[0] != 0x66 {
		t.Fatalf("unrelated #GP reinjection: %+v %v", ex, err)
	}
}
