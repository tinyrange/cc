//go:build linux && amd64

package kvm

import (
	"testing"
	"unsafe"
)

func testCPUID(entries ...kvmCPUIDEntry2) *kvmCPUID2 {
	size := unsafe.Sizeof(kvmCPUID2{}) + unsafe.Sizeof(kvmCPUIDEntry2{})*cpuidMaxEntries
	buf := make([]byte, size)
	cpuid := (*kvmCPUID2)(unsafe.Pointer(&buf[0]))
	cpuid.Nr = uint32(len(entries))
	out := unsafe.Slice(
		(*kvmCPUIDEntry2)(unsafe.Pointer(uintptr(unsafe.Pointer(cpuid))+unsafe.Sizeof(*cpuid))),
		cpuidMaxEntries,
	)
	copy(out, entries)
	return cpuid
}

func TestSetCPUIDTopologyConfiguresLeaf1AddressableLogicalIDs(t *testing.T) {
	cpuid := testCPUID(kvmCPUIDEntry2{Function: 1, Ebx: 0x12340000})

	setCPUIDTopology(cpuid, 5, 6)

	entry := cpuidEntries(cpuid)[0]
	if got, want := (entry.Ebx>>16)&0xff, uint32(8); got != want {
		t.Fatalf("leaf 1 logical IDs = %d, want %d", got, want)
	}
	if got, want := entry.Ebx>>24, uint32(5); got != want {
		t.Fatalf("leaf 1 APIC ID = %d, want %d", got, want)
	}
	if entry.Edx&(1<<28) == 0 {
		t.Fatalf("leaf 1 HTT bit was not set")
	}
}

func TestSetCPUIDTopologyConfiguresExtendedTopologyLeaves(t *testing.T) {
	cpuid := testCPUID(
		kvmCPUIDEntry2{Function: 0xb, Index: 0},
		kvmCPUIDEntry2{Function: 0xb, Index: 1},
		kvmCPUIDEntry2{Function: 0x1f, Index: 0},
		kvmCPUIDEntry2{Function: 0x1f, Index: 1},
	)

	setCPUIDTopology(cpuid, 7, 16)

	for _, entry := range cpuidEntries(cpuid) {
		switch entry.Index {
		case 0:
			if entry.Eax != 0 || entry.Ebx != 1 || entry.Ecx != 0x100 || entry.Edx != 7 {
				t.Fatalf("leaf %#x index 0 = eax=%#x ebx=%#x ecx=%#x edx=%#x, want SMT level for APIC 7", entry.Function, entry.Eax, entry.Ebx, entry.Ecx, entry.Edx)
			}
		case 1:
			if entry.Eax != 4 || entry.Ebx != 16 || entry.Ecx != 0x201 || entry.Edx != 7 {
				t.Fatalf("leaf %#x index 1 = eax=%#x ebx=%#x ecx=%#x edx=%#x, want core level for 16 CPUs/APIC 7", entry.Function, entry.Eax, entry.Ebx, entry.Ecx, entry.Edx)
			}
		case 2:
			if entry.Eax != 0 || entry.Ebx != 0 || entry.Ecx != 2 || entry.Edx != 7 {
				t.Fatalf("leaf %#x index 2 = eax=%#x ebx=%#x ecx=%#x edx=%#x, want terminator for APIC 7", entry.Function, entry.Eax, entry.Ebx, entry.Ecx, entry.Edx)
			}
		default:
			t.Fatalf("unexpected entry index %d", entry.Index)
		}
	}
}

func TestSetCPUIDTopologyAddsExtendedTopologyLeavesWhenMissing(t *testing.T) {
	cpuid := testCPUID(kvmCPUIDEntry2{Function: 0, Eax: 7})

	setCPUIDTopology(cpuid, 3, 8)

	if got, want := cpuid.Nr, uint32(7); got != want {
		t.Fatalf("entry count = %d, want %d", got, want)
	}
	entries := cpuidEntries(cpuid)
	if entries[0].Eax != 0x1f {
		t.Fatalf("max basic leaf = %#x, want at least 0x1f", entries[0].Eax)
	}
	for _, function := range []uint32{0xb, 0x1f} {
		for index := uint32(0); index <= 2; index++ {
			found := false
			for _, entry := range entries {
				if entry.Function == function && entry.Index == index {
					found = true
					if entry.Flags&kvmCPUIDFlagSignificantIndex == 0 {
						t.Fatalf("leaf %#x index %d missing significant-index flag", function, index)
					}
					break
				}
			}
			if !found {
				t.Fatalf("missing leaf %#x index %d", function, index)
			}
		}
	}
}
