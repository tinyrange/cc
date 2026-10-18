//go:build linux && amd64

package hypervisor

import (
	"context"
	"encoding/binary"
	"fmt"
	"golang.org/x/sys/unix"
	"os"
	"sync"
	"testing"
	"time"

	"j5.nz/cc/hypervisor/x86state"
)

func TestX86FourCPUStartup(t *testing.T) {
	if _, err := os.Stat("/dev/kvm"); err != nil {
		t.Skip("KVM unavailable")
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
	// Prove register writes are CPU-local before allowing AP INIT/SIPI to reset them.
	for id, cpu := range cpus {
		r, err := cpu.Registers()
		if err != nil {
			t.Fatal(err)
		}
		r.Rax = uint64(0x100 + id)
		if err := cpu.SetRegisters(r); err != nil {
			t.Fatal(err)
		}
		entries, err := cpu.SupportedCPUID()
		if err != nil {
			t.Fatal(err)
		}
		for j := range entries {
			if entries[j].Function == 1 {
				entries[j].Ebx = entries[j].Ebx&0xffff | 4<<16 | uint32(id)<<24
			}
		}
		if err := cpu.SetCPUID(entries); err != nil {
			t.Fatal(err)
		}
	}
	for id, cpu := range cpus {
		r, err := cpu.Registers()
		if err != nil || r.Rax != uint64(0x100+id) {
			t.Fatalf("CPU%d registers: %+v %v", id, r, err)
		}
	}
	// BSP runs 32-bit protected mode and sends broadcast INIT then SIPI vector8.
	// APs start at 8000h in architectural real mode, obtain their APIC ID and OUT it.
	code := []byte{}
	store := func(address, value uint32) {
		code = append(code, 0xc7, 0x05)
		code = binary.LittleEndian.AppendUint32(code, address)
		code = binary.LittleEndian.AppendUint32(code, value)
	}
	store(0xfee000f0, 0x1ff)
	store(0xfee00300, 0x000c4500)
	store(0xfee00300, 0x000c4608)
	code = append(code, 0xba, 0xf1, 0, 0, 0, 0xb0, 0, 0xee, 0xeb, 0xfe)
	copy(ram[0x1000:], code)
	copy(ram[0x8000:], []byte{0x66, 0xb8, 1, 0, 0, 0, 0x0f, 0xa2, 0x66, 0xc1, 0xeb, 24, 0x88, 0xd8, 0xba, 0xf1, 0, 0xee, 0xeb, 0xfe})
	s, err := cpus[0].SystemRegisters()
	if err != nil {
		t.Fatal(err)
	}
	seg := x86state.Segment{Limit: 0xffffffff, Present: 1, S: 1, Type: 3, Db: 1, G: 1}
	s.Cs, s.Ds, s.Es, s.Ss, s.Fs, s.Gs = seg, seg, seg, seg, seg, seg
	s.Cs.Type = 11
	s.Cr0, s.Cr3, s.Cr4, s.Efer = 0x11, 0, 0, 0
	if err := cpus[0].SetSystemRegisters(s); err != nil {
		t.Fatal(err)
	}
	if err := cpus[0].SetRegisters(x86state.Registers{Rip: 0x1000, Rflags: 2}); err != nil {
		t.Fatal(err)
	}
	type result struct {
		id  int
		ex  X86Exit
		err error
	}
	results := make(chan result, len(cpus))
	var wg sync.WaitGroup
	for id, cpu := range cpus {
		wg.Add(1)
		go func(id int, cpu X86) {
			defer wg.Done()
			for {
				ex, err := cpu.RunUntil(ctx, func(ex X86Exit) (bool, error) {
					owner := cpus[0].(*nativeX86).threads[id]
					if int32(unix.Gettid()) != owner.tid.Load() {
						return false, fmt.Errorf("CPU%d callback off owner", id)
					}
					// Real KVM register inspection is reentrant on the owner.
					if _, err := cpu.Registers(); err != nil {
						return false, err
					}
					return false, nil
				})
				if err != nil || ex.Reason != 0 {
					results <- result{id, ex, err}
					return
				}
			}
		}(id, cpu)
	}
	// Always join before reporting a failure or unmapping the shared RAM.
	wg.Wait()
	for range cpus {
		r := <-results
		if r.err != nil || r.ex.Reason != X86ExitIO || r.ex.Port != 0xf1 || len(r.ex.Data) != 1 || int(r.ex.Data[0]) != r.id {
			t.Errorf("CPU%d startup: %+v %v", r.id, r.ex, r.err)
			continue
		}
		if err := cpus[r.id].CompleteIO(); err != nil {
			t.Errorf("CPU%d IO completion: %v", r.id, err)
		}
		regs, err := cpus[r.id].Registers()
		if err != nil {
			t.Error(err)
		}
		want := uint64(18)
		if r.id == 0 {
			want = 0x1000 + uint64(len(code)) - 2
		}
		if regs.Rip != want {
			t.Errorf("CPU%d completion RIP %x want %x", r.id, regs.Rip, want)
		}
	}
	if t.Failed() {
		return
	}
	if _, ok := cpus[1].(interface{ SetBreakpoint(uint64) error }); ok {
		t.Fatal("AP exposes BSP debugger")
	}
	if err := cpus[1].Close(); err == nil {
		t.Fatal("borrowed AP closed its owner")
	}
	// All four CPUs are now in infinite loops. Cancelling CPU2 must not wake
	// its siblings, and the final context cancellation must join all of them.
	runCtx, stop := context.WithCancel(ctx)
	done := make(chan int, 4)
	for id, cpu := range cpus {
		wg.Add(1)
		go func(id int, cpu X86) {
			defer wg.Done()
			_, _ = cpu.Run(runCtx)
			done <- id
		}(id, cpu)
	}
	defer func() { stop(); wg.Wait() }()
	time.Sleep(20 * time.Millisecond)
	if err := cpus[2].Cancel(); err != nil {
		t.Fatal(err)
	}
	select {
	case id := <-done:
		if id != 2 {
			t.Fatalf("Cancel CPU2 woke CPU%d", id)
		}
	case <-time.After(time.Second):
		t.Fatal("targeted cancellation did not wake CPU2")
	}
	select {
	case id := <-done:
		t.Fatalf("sibling CPU%d interrupted by targeted cancellation", id)
	case <-time.After(10 * time.Millisecond):
	}

}
