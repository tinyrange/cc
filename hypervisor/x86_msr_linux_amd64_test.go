//go:build linux && amd64

package hypervisor

import (
	"context"
	"encoding/binary"
	"os"
	"testing"
	"time"

	"j5.nz/cc/hypervisor/x86state"
)

func TestX86ReadOnlyFallbackMSRs(t *testing.T) {
	if _, err := os.Stat("/dev/kvm"); err != nil {
		t.Skip("KVM unavailable")
	}
	const missing = 0xdeadbeef
	const value = 0x1234567887654321
	for _, tc := range []struct {
		name         string
		index        uint32
		write, fault bool
		want         uint64
	}{
		{"listed-read", missing, false, false, value},
		{"unlisted-read", missing - 1, false, true, 0},
		{"listed-write", missing, true, true, 0},
		{"unlisted-write", missing - 1, true, true, 0},
		{"kernel-owned-EFER", 0xc0000080, false, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			cpu, err := NewX86(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer cpu.Close()
			policy := map[uint32]uint64{missing: value, 0xc0000080: value}
			if err := cpu.(interface{ SetReadOnlyFallbackMSRs(map[uint32]uint64) error }).SetReadOnlyFallbackMSRs(policy); err != nil {
				t.Fatal(err)
			}
			policy[missing] = 0 // API must own its copy.
			ram, err := cpu.MapRAM(0, 2<<20)
			if err != nil {
				t.Fatal(err)
			}
			binary.LittleEndian.PutUint64(ram[0x5008:], 0x00cf9b000000ffff)
			binary.LittleEndian.PutUint64(ram[0x5010:], 0x00cf93000000ffff)
			gate := ram[0x6000+13*8:][:8]
			binary.LittleEndian.PutUint16(gate, 0x2000)
			binary.LittleEndian.PutUint16(gate[2:], 8)
			gate[5] = 0x8e
			// Normal path OUTs 0xf1 without destroying the RDMSR result; #GP OUTs 0xf2.
			code := []byte{0xb9}
			code = binary.LittleEndian.AppendUint32(code, tc.index)
			op := byte(0x32)
			if tc.write {
				op = 0x30
			}
			code = append(code, 0x0f, op, 0xe6, 0xf1, 0xeb, 0xfe)
			copy(ram[0x1000:], code)
			copy(ram[0x2000:], []byte{0xe6, 0xf2, 0xeb, 0xfe})
			s, err := cpu.SystemRegisters()
			if err != nil {
				t.Fatal(err)
			}
			seg := x86state.Segment{Limit: 0xffffffff, Selector: 16, Present: 1, S: 1, Type: 3, Db: 1, G: 1}
			s.Cs, s.Ds, s.Es, s.Ss, s.Fs, s.Gs = seg, seg, seg, seg, seg, seg
			s.Cs.Selector, s.Cs.Type = 8, 11
			s.Cr0, s.Cr3, s.Cr4, s.Efer = 0x11, 0, 0, 0
			s.Gdt = x86state.DescriptorTable{Base: 0x5000, Limit: 23}
			s.Idt = x86state.DescriptorTable{Base: 0x6000, Limit: 14*8 - 1}
			if err := cpu.SetSystemRegisters(s); err != nil {
				t.Fatal(err)
			}
			if err := cpu.SetRegisters(x86state.Registers{Rip: 0x1000, Rsp: 0x8000, Rflags: 2}); err != nil {
				t.Fatal(err)
			}
			ex, err := cpu.Run(ctx)
			if err != nil {
				t.Fatal(err)
			}
			port := uint16(0xf1)
			if tc.fault {
				port = 0xf2
			}
			if ex.Reason != 2 || ex.Port != port {
				t.Fatalf("exit %+v, want port %#x", ex, port)
			}
			if !tc.fault {
				r, err := cpu.Registers()
				if err != nil {
					t.Fatal(err)
				}
				if got := r.Rdx<<32 | uint64(uint32(r.Rax)); got != tc.want {
					t.Fatalf("RDMSR=%#x want %#x", got, tc.want)
				}
			}
		})
	}
}
