//go:build linux && amd64

package hypervisor

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"j5.nz/cc/hypervisor/x86state"
)

func TestX86RealModeInterrupt(t *testing.T) {
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
	// INT 13h calls a ROM stub which traps to Go, then IRET returns to
	// the boot sector. The sector emits a second OUT to prove the return.
	copy(ram[0x7c00:], []byte{0xcd, 0x13, 0xba, 0xf1, 0, 0xee, 0xeb, 0xfe})
	copy(ram[0xf0100:], []byte{0x50, 0xba, 0xf0, 0, 0xee, 0x58, 0xcf})
	ram[0x4c], ram[0x4d], ram[0x4e], ram[0x4f] = 0, 1, 0, 0xf0
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
	s.Idt = x86state.DescriptorTable{Limit: 0x3ff}
	if err = cpu.SetSystemRegisters(s); err != nil {
		t.Fatal(err)
	}
	if err = cpu.SetRegisters(x86state.Registers{Rip: 0x7c00, Rsp: 0x7c00, Rflags: 2, Rax: 0x1234}); err != nil {
		t.Fatal(err)
	}
	for _, port := range []uint16{0xf0, 0xf1} {
		for {
			ex, err := cpu.Run(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if ex.Reason == 0 {
				continue
			}
			if ex.Reason != X86ExitIO || ex.Port != port || !ex.Write || len(ex.Data) != 1 || ex.Data[0] != 0x34 {
				t.Fatalf("unexpected exit: %+v", ex)
			}
			if err := cpu.CompleteIO(); err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	r, err := cpu.Registers()
	if err != nil {
		t.Fatal(err)
	}
	if r.Rsp != 0x7c00 {
		t.Fatalf("IRET did not restore stack: %#x", r.Rsp)
	}
	// An execution breakpoint must stop before the instruction, without
	// changing guest code, and disabling it must allow normal execution.
	debug := cpu.(interface{ SetBreakpoint(uint64) error })
	r.Rip = 0x7c02
	if err := cpu.SetRegisters(r); err != nil {
		t.Fatal(err)
	}
	if err := debug.SetBreakpoint(r.Rip); err != nil {
		t.Fatal(err)
	}
	for {
		ex, err := cpu.Run(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if ex.Reason == 0 {
			continue
		}
		if ex.Reason != X86ExitDebug {
			t.Fatalf("breakpoint: %+v", ex)
		}
		break
	}
	stopped, err := cpu.Registers()
	if err != nil || stopped.Rip != r.Rip || ram[0x7c02] != 0xba {
		t.Fatalf("breakpoint changed execution/code: %+v %v", stopped, err)
	}
	if err := debug.SetBreakpoint(0); err != nil {
		t.Fatal(err)
	}
	for {
		ex, err := cpu.Run(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if ex.Reason == 0 {
			continue
		}
		if ex.Reason != X86ExitIO || ex.Port != 0xf1 {
			t.Fatalf("resume breakpoint: %+v", ex)
		}
		if err := cpu.CompleteIO(); err != nil {
			t.Fatal(err)
		}
		break
	}
	// The next instruction spins. Cancellation must interrupt KVM, and its
	// callback must finish before a subsequent run starts using the same CPU.
	for i := 0; i < 20; i++ {
		deadline, cancel := context.WithTimeout(ctx, 2*time.Millisecond)
		for {
			_, err = cpu.Run(deadline)
			if err != nil {
				break
			}
		}
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("cancel run: %v", err)
		}
		r.Rip = 0x7c02 // mov dx,f1; out dx,al; spin
		if err = cpu.SetRegisters(r); err != nil {
			t.Fatal(err)
		}
		for {
			ex, err := cpu.Run(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if ex.Reason == 0 {
				continue
			}
			if ex.Reason != X86ExitIO || ex.Port != 0xf1 {
				t.Fatalf("exit after cancellation: %+v", ex)
			}
			if err := cpu.CompleteIO(); err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	// A stream of handled exits must remain cancellable. Handler errors and
	// unhandled exits return the exact exit without running another instruction.
	copy(ram[0x7c00:], []byte{0xe6, 0xf2, 0xeb, 0xfc}) // out f2,al; loop
	reset := func() {
		t.Helper()
		r.Rip = 0x7c00
		if err := cpu.SetRegisters(r); err != nil {
			t.Fatal(err)
		}
	}
	reset()
	failure := errors.New("device failure")
	calls := 0
	for {
		ex, err := cpu.RunUntil(ctx, func(ex X86Exit) (bool, error) {
			calls++
			return false, failure
		})
		if ex.Reason == 0 && err == nil {
			continue
		}
		if !errors.Is(err, failure) || calls != 1 || ex.Port != 0xf2 {
			t.Fatalf("handler error: %+v %v calls=%d", ex, err, calls)
		}
		break
	}
	if err := cpu.CompleteIO(); err != nil {
		t.Fatal(err)
	}
	reset()
	stream, cancelStream := context.WithCancel(ctx)
	calls = 0
	for {
		_, err := cpu.RunUntil(stream, func(ex X86Exit) (bool, error) {
			if ex.Reason != X86ExitIO || ex.Port != 0xf2 {
				t.Fatalf("stream exit: %+v", ex)
			}
			calls++
			if calls == 10 {
				cancelStream()
			}
			return true, cpu.CompleteIO()
		})
		if err == nil {
			continue
		}
		if !errors.Is(err, context.Canceled) || calls != 10 {
			t.Fatalf("cancel handled stream: %v calls=%d", err, calls)
		}
		break
	}
	cancelStream()
	reset()
	for {
		ex, err := cpu.RunUntil(ctx, func(X86Exit) (bool, error) { return false, nil })
		if err != nil {
			t.Fatal(err)
		}
		if ex.Reason == 0 {
			continue
		}
		if ex.Reason != X86ExitIO || ex.Port != 0xf2 {
			t.Fatalf("unhandled exit after cancellation: %+v", ex)
		}
		break
	}

}
