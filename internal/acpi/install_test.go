package acpi

import (
	"context"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/tinyrange/cc/internal/hv"
)

func TestInstallProducesTables(t *testing.T) {
	vm := newFakeVM(2 << 20) // 2 MiB

	cfg := Config{
		MemoryBase: 0,
		MemorySize: uint64(len(vm.mem)),
		HPET:       &HPETConfig{Address: 0xFED00000},
	}
	cfg.normalize(vm)

	if err := Install(vm, cfg); err != nil {
		t.Fatalf("install ACPI: %v", err)
	}

	tables := parseTables(t, vm.mem, cfg.MemoryBase, cfg.TablesBase, cfg.TablesSize)

	for _, sig := range []string{"DSDT", "APIC", "FACP", "XSDT", "HPET"} {
		if _, ok := tables[sig]; !ok {
			t.Fatalf("missing %s table", sig)
		}
	}

	rsdpOff := int(cfg.RSDPBase - cfg.MemoryBase)
	rsdp := vm.mem[rsdpOff : rsdpOff+36]
	if string(rsdp[:8]) != "RSD PTR " {
		t.Fatalf("bad RSDP signature: %q", rsdp[:8])
	}
	xsdtAddr := binary.LittleEndian.Uint64(rsdp[24:32])
	if xsdtAddr != tables["XSDT"] {
		t.Fatalf("xsdt pointer mismatch: got 0x%x want 0x%x", xsdtAddr, tables["XSDT"])
	}

	xsdtBytes := readTableBytes(t, vm.mem, cfg.MemoryBase, tables["XSDT"])
	entries := parseXSDTEntries(xsdtBytes)
	want := []uint64{tables["FACP"], tables["APIC"], tables["HPET"]}
	if len(entries) != len(want) {
		t.Fatalf("xsdt entry count mismatch: got %d want %d", len(entries), len(want))
	}
	for i := range entries {
		if entries[i] != want[i] {
			t.Fatalf("xsdt entry %d mismatch: got 0x%x want 0x%x", i, entries[i], want[i])
		}
	}
}

func TestInstallWithoutHPET(t *testing.T) {
	vm := newFakeVM(2 << 20)

	cfg := Config{
		MemoryBase: 0,
		MemorySize: uint64(len(vm.mem)),
	}
	cfg.normalize(vm)

	if err := Install(vm, cfg); err != nil {
		t.Fatalf("install ACPI: %v", err)
	}

	tables := parseTables(t, vm.mem, cfg.MemoryBase, cfg.TablesBase, cfg.TablesSize)
	if _, ok := tables["HPET"]; ok {
		t.Fatalf("unexpected HPET table present")
	}

	xsdtBytes := readTableBytes(t, vm.mem, cfg.MemoryBase, tables["XSDT"])
	entries := parseXSDTEntries(xsdtBytes)
	want := []uint64{tables["FACP"], tables["APIC"]}
	if len(entries) != len(want) {
		t.Fatalf("xsdt entries mismatch: got %d want %d", len(entries), len(want))
	}
	for i := range entries {
		if entries[i] != want[i] {
			t.Fatalf("xsdt entry %d mismatch: got 0x%x want 0x%x", i, entries[i], want[i])
		}
	}
}

func parseTables(t *testing.T, mem []byte, memBase, tablesBase uint64, size uint64) map[string]uint64 {
	t.Helper()
	tables := make(map[string]uint64)
	start := int(tablesBase - memBase)
	end := start + int(size)
	for pos := start; pos+36 <= end; {
		sig := string(mem[pos : pos+4])
		if sig == "\x00\x00\x00\x00" {
			break
		}
		length := int(binary.LittleEndian.Uint32(mem[pos+4 : pos+8]))
		if pos+length > end {
			t.Fatalf("table %s overruns region", sig)
		}
		tableBytes := mem[pos : pos+length]
		if sum(tableBytes) != 0 {
			t.Fatalf("table %s checksum mismatch", sig)
		}
		tables[sig] = memBase + uint64(pos)
		pos += align(length, 8)
	}
	return tables
}

func sum(b []byte) byte {
	var total byte
	for _, v := range b {
		total += v
	}
	return total
}

func align(n, a int) int {
	if r := n % a; r != 0 {
		return n + (a - r)
	}
	return n
}

func readTableBytes(t *testing.T, mem []byte, base uint64, phys uint64) []byte {
	t.Helper()
	off := int(phys - base)
	length := int(binary.LittleEndian.Uint32(mem[off+4 : off+8]))
	return mem[off : off+length]
}

func parseXSDTEntries(xsdt []byte) []uint64 {
	body := xsdt[36:]
	entries := make([]uint64, 0, len(body)/8)
	for len(body) >= 8 {
		entries = append(entries, binary.LittleEndian.Uint64(body[:8]))
		body = body[8:]
	}
	return entries
}

type fakeVM struct {
	mem  []byte
	base uint64
}

func newFakeVM(size int) *fakeVM {
	return &fakeVM{mem: make([]byte, size)}
}

func (f *fakeVM) MemoryBase() uint64        { return f.base }
func (f *fakeVM) MemorySize() uint64        { return uint64(len(f.mem)) }
func (f *fakeVM) Hypervisor() hv.Hypervisor { return nil }
func (f *fakeVM) Close() error              { return nil }

func (f *fakeVM) ReadAt(p []byte, off int64) (int, error) {
	idx, err := f.translate(off, len(p))
	if err != nil {
		return 0, err
	}
	return copy(p, f.mem[idx:]), nil
}

func (f *fakeVM) WriteAt(p []byte, off int64) (int, error) {
	idx, err := f.translate(off, len(p))
	if err != nil {
		return 0, err
	}
	return copy(f.mem[idx:], p), nil
}

func (f *fakeVM) translate(off int64, n int) (int, error) {
	idx := int(off - int64(f.base))
	if idx < 0 || idx+n > len(f.mem) {
		return 0, fmt.Errorf("offset out of range")
	}
	return idx, nil
}

func (f *fakeVM) Run(context.Context, hv.RunConfig) error {
	return fmt.Errorf("not implemented")
}

func (f *fakeVM) VirtualCPUCall(int, func(hv.VirtualCPU) error) error {
	return fmt.Errorf("not implemented")
}

func (f *fakeVM) AddDevice(hv.Device) error {
	return fmt.Errorf("not implemented")
}

func (f *fakeVM) AddDeviceFromTemplate(hv.DeviceTemplate) error {
	return fmt.Errorf("not implemented")
}
