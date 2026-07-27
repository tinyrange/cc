//go:build windows && amd64

package whp

import (
	"testing"

	"j5.nz/cc/internal/amd64vm"
)

func TestAMD64GuestMemoryRegionsLeaveDeviceHole(t *testing.T) {
	const memorySize = 12 << 30

	regions := amd64GuestMemoryRegions(memorySize)
	want := []guestMemoryRegion{
		{
			guestPhysAddr: amd64vm.MemoryBase,
			size:          3 << 30,
		},
		{
			guestPhysAddr: amd64vm.HighMemoryBase,
			memoryOffset:  3 << 30,
			size:          9 << 30,
		},
	}
	if len(regions) != len(want) {
		t.Fatalf("len(amd64GuestMemoryRegions()) = %d, want %d", len(regions), len(want))
	}
	for i := range want {
		if regions[i] != want[i] {
			t.Errorf("region %d = %#v, want %#v", i, regions[i], want[i])
		}
	}
}
