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

func TestX86HyperVSyntheticTimer(t *testing.T) {
	for _, direct := range []bool{false, true} {
		name := "message"
		if direct {
			name = "direct"
		}
		t.Run(name, func(t *testing.T) {

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
					if e.Eax&(1<<2|1<<3|1<<6) != (1<<2 | 1<<3 | 1<<6) {
						t.Skip("synthetic timers unavailable")
					}
					if direct && e.Edx&(1<<19) == 0 {
						t.Skip("direct timers unavailable")
					}
					e.Eax &= 1<<1 | 1<<2 | 1<<3 | 1<<5 | 1<<6 | 1<<9
					e.Ebx = 0
					e.Ecx = 0
					e.Edx &= 1 << 19
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

			enabled, err := cpu.(X86HyperVSynIC).EnableHyperVSynIC()
			if err != nil {
				t.Fatal(err)
			}
			if !enabled {
				t.Skip("SynIC unavailable")
			}
			ram, err := cpu.MapRAM(0, 2<<20)
			if err != nil {
				t.Fatal(err)
			}
			code := []byte{0xc7, 0x05, 0xf0, 0x00, 0xe0, 0xfe, 0xff, 0x01, 0, 0} // enable APIC
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
			wrmsr(0x40000083, 0xa001) // message page; notification exit must resume
			wrmsr(0x40000080, 1)
			wrmsr(0x40000093, 0x51) // SINT3 unmasked
			config := uint64(3<<16 | 3)
			if direct {
				config = 1<<12 | 0x51<<4 | 3
			}
			wrmsr(0x400000b0, config) // enabled periodic timer delivering to SINT3
			wrmsr(0x400000b1, 10000)  // one millisecond in 100ns units
			code = append(code, 0xe6, 0xf1, 0xe6, 0xf2, 0xeb, 0xfe)
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
			for _, port := range []uint16{0xf1, 0xf2} {
				ex, err := cpu.Run(ctx)
				if err != nil || ex.Reason != X86ExitIO || ex.Port != port {
					t.Fatalf("timer guest: %+v %v", ex, err)
				}
				if port == 0xf1 {
					time.Sleep(10 * time.Millisecond)
					if err := cpu.CompleteIO(); err != nil {
						t.Fatal(err)
					}
				}
			}
			message := ram[0xa000+3*256:]
			if got := binary.LittleEndian.Uint32(message); !direct && got != 0x80000010 {
				t.Fatalf("missing timer-expired message: %#x", got)
			}
			state, err := cpu.(interface {
				InterruptState() (map[string]uint64, error)
			}).InterruptState()
			if err != nil {
				t.Fatal(err)
			}
			if state["lapic0.irr2"]&(1<<17) == 0 {
				t.Fatal("synthetic timer interrupt not pending")
			}

		})
	}
}
