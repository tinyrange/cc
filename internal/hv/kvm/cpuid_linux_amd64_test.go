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
	if got, want := (entry.Ebx>>16)&0xff, uint32(6); got != want {
		t.Fatalf("leaf 1 logical processor count = %d, want %d", got, want)
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
	)

	setCPUIDTopology(cpuid, 7, 16)

	for _, entry := range cpuidEntries(cpuid) {
		switch entry.Index {
		case 0:
			if entry.Function == 0xb && (entry.Eax != 0 || entry.Ebx != 1 || entry.Ecx != 0x100 || entry.Edx != 7) {
				t.Fatalf("leaf %#x index 0 = eax=%#x ebx=%#x ecx=%#x edx=%#x, want SMT level for APIC 7", entry.Function, entry.Eax, entry.Ebx, entry.Ecx, entry.Edx)
			}
		case 1:
			if entry.Function == 0xb && (entry.Eax != 4 || entry.Ebx != 16 || entry.Ecx != 0x201 || entry.Edx != 7) {
				t.Fatalf("leaf %#x index 1 = eax=%#x ebx=%#x ecx=%#x edx=%#x, want core level for 16 CPUs/APIC 7", entry.Function, entry.Eax, entry.Ebx, entry.Ecx, entry.Edx)
			}
		case 2:
			if entry.Function == 0xb && (entry.Eax != 0 || entry.Ebx != 0 || entry.Ecx != 2 || entry.Edx != 7) {
				t.Fatalf("leaf %#x index 2 = eax=%#x ebx=%#x ecx=%#x edx=%#x, want terminator for APIC 7", entry.Function, entry.Eax, entry.Ebx, entry.Ecx, entry.Edx)
			}
		default:
			t.Fatalf("unexpected entry index %d", entry.Index)
		}
	}
}

func TestSetCPUIDTopologySeparatesAPICIDWidthFromCPUCount(t *testing.T) {
	cpuid := testCPUID(
		kvmCPUIDEntry2{Function: 4, Index: 0, Eax: (1 << 5)},
		kvmCPUIDEntry2{Function: 4, Index: 1, Eax: (3 << 5)},
		kvmCPUIDEntry2{Function: 0xb, Index: 0},
		kvmCPUIDEntry2{Function: 0xb, Index: 1},
	)

	setCPUIDTopology(cpuid, 3, 5)

	for _, entry := range cpuidEntries(cpuid) {
		switch {
		case entry.Function == 0xb && entry.Index == 1:
			if entry.Eax != 3 || entry.Ebx != 5 {
				t.Fatalf("leaf 0xb index 1 = eax=%#x ebx=%#x, want 3-bit APIC ID width and 5 logical CPUs", entry.Eax, entry.Ebx)
			}
		case entry.Function == 4 && entry.Index == 0:
			if got := (entry.Eax >> 26) & 0x3f; got != 4 {
				t.Fatalf("leaf 4 private cache cores field = %d, want 4", got)
			}
			if got := (entry.Eax >> 14) & 0xfff; got != 0 {
				t.Fatalf("leaf 4 private cache sharing = %d, want 0", got)
			}
		case entry.Function == 4 && entry.Index == 1:
			if got := (entry.Eax >> 26) & 0x3f; got != 4 {
				t.Fatalf("leaf 4 shared cache cores field = %d, want 4", got)
			}
			if got := (entry.Eax >> 14) & 0xfff; got != 4 {
				t.Fatalf("leaf 4 shared cache sharing = %d, want 4", got)
			}
		}
	}
}

func TestSetCPUIDTopologyMakesCacheSharingConsistent(t *testing.T) {
	cpuid := testCPUID(
		kvmCPUIDEntry2{Function: 4, Index: 0, Eax: (1 << 5) | (15 << 14) | (31 << 26)},
		kvmCPUIDEntry2{Function: 4, Index: 1, Eax: (2 << 5) | (15 << 14) | (31 << 26)},
		kvmCPUIDEntry2{Function: 4, Index: 2, Eax: (3 << 5) | (15 << 14) | (31 << 26)},
	)

	setCPUIDTopology(cpuid, 0, 5)

	entries := cpuidEntries(cpuid)
	for _, entry := range entries {
		if entry.Function != 4 {
			continue
		}
		cacheLevel := (entry.Eax >> 5) & 0x7
		sharing := (entry.Eax >> 14) & 0xfff
		cores := (entry.Eax >> 26) & 0x3f
		if cores != 4 {
			t.Fatalf("leaf 4 index %d cores field = %d, want 4", entry.Index, cores)
		}
		if cacheLevel < 3 && sharing != 0 {
			t.Fatalf("leaf 4 index %d private-cache sharing = %d, want 0", entry.Index, sharing)
		}
		if cacheLevel >= 3 && sharing != 4 {
			t.Fatalf("leaf 4 index %d shared-cache sharing = %d, want 4", entry.Index, sharing)
		}
	}
}

func TestSetCPUIDTopologyAddsLeafBWhenMissing(t *testing.T) {
	cpuid := testCPUID(kvmCPUIDEntry2{Function: 0, Eax: 7})

	setCPUIDTopology(cpuid, 3, 8)

	if got, want := cpuid.Nr, uint32(4); got != want {
		t.Fatalf("entry count = %d, want %d", got, want)
	}
	entries := cpuidEntries(cpuid)
	if entries[0].Eax != 0xb {
		t.Fatalf("max basic leaf = %#x, want at least 0xb", entries[0].Eax)
	}
	for index := uint32(0); index <= 2; index++ {
		found := false
		for _, entry := range entries {
			if entry.Function == 0xb && entry.Index == index {
				found = true
				if entry.Flags&kvmCPUIDFlagSignificantIndex == 0 {
					t.Fatalf("leaf 0xb index %d missing significant-index flag", index)
				}
				break
			}
		}
		if !found {
			t.Fatalf("missing leaf 0xb index %d", index)
		}
	}
}

func TestSetCPUIDTopologyZerosLeaf1F(t *testing.T) {
	cpuid := testCPUID(kvmCPUIDEntry2{Function: 0x1f, Index: 1, Eax: 9, Ebx: 9, Ecx: 9, Edx: 9})

	setCPUIDTopology(cpuid, 3, 5)

	entry := cpuidEntries(cpuid)[0]
	if entry.Eax != 0 || entry.Ebx != 0 || entry.Ecx != 1 || entry.Edx != 0 {
		t.Fatalf("leaf 0x1f index 1 = eax=%#x ebx=%#x ecx=%#x edx=%#x, want disabled leaf", entry.Eax, entry.Ebx, entry.Ecx, entry.Edx)
	}
}

func TestSetCPUIDTopologyAppliesLeaf7Policy(t *testing.T) {
	cpuid := testCPUID(
		kvmCPUIDEntry2{Function: 7, Index: 0, Eax: 2, Ebx: (1 << 6) | (1 << 13), Ecx: 1 << 5},
		kvmCPUIDEntry2{Function: 7, Index: 2, Edx: 1},
	)

	setCPUIDTopology(cpuid, 0, 2)

	entries := cpuidEntries(cpuid)
	if entries[0].Eax != 1 {
		t.Fatalf("leaf 7 max subleaf = %#x, want 1", entries[0].Eax)
	}
	if entries[0].Ebx != 0 {
		t.Fatalf("leaf 7 EBX = %#x, want masked compatibility bits", entries[0].Ebx)
	}
	if entries[0].Ecx&(1<<5) == 0 {
		t.Fatalf("leaf 7 ECX WAITPKG bit was unexpectedly masked: ecx=%#x", entries[0].Ecx)
	}
	if entries[1].Eax != 0 || entries[1].Ebx != 0 || entries[1].Ecx != 0 || entries[1].Edx != 0 {
		t.Fatalf("leaf 7 subleaf 2 = eax=%#x ebx=%#x ecx=%#x edx=%#x, want zeroed", entries[1].Eax, entries[1].Ebx, entries[1].Ecx, entries[1].Edx)
	}
}

func TestSetCPUIDBrandStringPopulatesExtendedBrandLeaves(t *testing.T) {
	cpuid := testCPUID(kvmCPUIDEntry2{Function: 0x80000000, Eax: 0x80000001})

	setCPUIDBrandString(cpuid, "TinyRange Test CPU 123")

	entries := cpuidEntries(cpuid)
	if entries[0].Eax != 0x80000004 {
		t.Fatalf("max extended leaf = %#x, want %#x", entries[0].Eax, uint32(0x80000004))
	}

	var encoded [48]byte
	for _, function := range []uint32{0x80000002, 0x80000003, 0x80000004} {
		found := false
		for _, entry := range entries {
			if entry.Function != function {
				continue
			}
			found = true
			if entry.Flags&kvmCPUIDFlagSignificantIndex != 0 {
				t.Fatalf("brand leaf %#x has significant-index flag", function)
			}
			off := (function - 0x80000002) * 16
			putLE32(encoded[off:], entry.Eax)
			putLE32(encoded[off+4:], entry.Ebx)
			putLE32(encoded[off+8:], entry.Ecx)
			putLE32(encoded[off+12:], entry.Edx)
			break
		}
		if !found {
			t.Fatalf("missing brand leaf %#x", function)
		}
	}

	if got := string(encoded[:22]); got != "TinyRange Test CPU 123" {
		t.Fatalf("brand string = %q", got)
	}
}

func putLE32(data []byte, value uint32) {
	data[0] = byte(value)
	data[1] = byte(value >> 8)
	data[2] = byte(value >> 16)
	data[3] = byte(value >> 24)
}
