//go:build linux && amd64

package kvm

import (
	"encoding/binary"
	"testing"
	"unsafe"
)

func TestHyperVExitCompletion(t *testing.T) {
	for _, tc := range []struct {
		name        string
		kind        uint32
		synic, fail bool
	}{
		{"extended-query", 2, false, false},
		{"synic-notification", 1, true, false},
		{"unconfigured-synic", 1, false, true},
		{"unhandled-debugger", 3, true, true},
		{"unknown", 99, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cpu := &VCPU{fd: 0, run: make([]byte, unsafe.Sizeof(kvmRunData{})), hypervSynIC: tc.synic}
			vm := &VM{vcpus: []*VCPU{cpu}}
			run := (*kvmRunData)(unsafe.Pointer(&cpu.run[0]))
			run.exitReason = 27
			binary.LittleEndian.PutUint32(run.anon0[:], tc.kind)
			binary.LittleEndian.PutUint64(run.anon0[8:], 0x8001)
			binary.LittleEndian.PutUint64(run.anon0[16:], 0xdeadbeef)
			err := vm.CompleteHyperVExit(0)
			if (err != nil) != tc.fail {
				t.Fatalf("error=%v want failure=%v", err, tc.fail)
			}
			if tc.kind == 2 {
				if got := binary.LittleEndian.Uint64(run.anon0[16:]); got != 2 {
					t.Fatalf("hypercall status %#x", got)
				}
				if cpu.hypervCalls != 1 || cpu.hypervLastCall != 0x8001 {
					t.Fatal("missing bounded diagnostics")
				}
			}
		})
	}
}
