package hypervisor

import "testing"

func TestA20LayoutPreservesHolesAndHighBacking(t *testing.T) {
	original := []RAMRegion{{Address: 0, Offset: 0, Size: 0xa0000}, {Address: 0xc0000, Offset: 0xc0000, Size: 0x340000}}
	layout, err := A20Layout(original)
	if err != nil {
		t.Fatal(err)
	}
	lookup := func(address uint64) (uint64, bool) {
		for _, r := range layout {
			if address >= r.Address && address-r.Address < r.Size {
				return r.Offset + address - r.Address, true
			}
		}
		return 0, false
	}
	for _, pair := range [][2]uint64{{0x500, 0x500}, {0x100500, 0x500}, {0x1c0500, 0xc0500}, {0x300500, 0x200500}} {
		got, ok := lookup(pair[0])
		if !ok || got != pair[1] {
			t.Fatalf("address %x got %x mapped %t want %x", pair[0], got, ok, pair[1])
		}
	}
	for _, hole := range []uint64{0xa0500, 0x1a0500, 0xbffff, 0x1bffff} {
		if _, ok := lookup(hole); ok {
			t.Fatalf("device hole %x became RAM", hole)
		}
	}
	if original[1].Offset != 0xc0000 || original[1].Size != 0x340000 {
		t.Fatal("mutated original layout")
	}
	if _, err := A20Layout([]RAMRegion{{Address: ^uint64(0) - 10, Size: 20}}); err == nil {
		t.Fatal("accepted overflow")
	}
}
