package amd64

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestPrepareBootFixture(t *testing.T) {
	kernelPath := os.Getenv("CC_NETBSD_KERNEL")
	if kernelPath == "" {
		kernelPath = filepath.Join("..", "..", "..", "local", "netbsd101-amd64", "netbsd-GENERIC")
	}
	kernel, err := os.ReadFile(kernelPath)
	if err != nil {
		t.Skipf("NetBSD kernel fixture not present: %s", kernelPath)
	}
	mem := make([]byte, 128<<20)
	plan, err := PrepareBoot(mem, kernel, BootOptions{MemorySize: uint64(len(mem))})
	if err != nil {
		t.Fatal(err)
	}
	if plan.EntryGPA == 0 {
		t.Fatal("missing native entry")
	}
	if plan.EntryGPA >= kernBase {
		t.Fatalf("entry stayed virtual: %#x", plan.EntryGPA)
	}
	if got := binary.LittleEndian.Uint32(mem[plan.BootInfoGPA:]); got != 3 {
		t.Fatalf("bootinfo entries = %d, want 3", got)
	}
	if got := binary.LittleEndian.Uint32(mem[plan.StackGPA+4:]); got != rebootAutoBoot {
		t.Fatalf("boothowto = %#x, want %#x", got, rebootAutoBoot)
	}
	if got := binary.LittleEndian.Uint32(mem[plan.StackGPA+12:]); got != uint32(plan.BootInfoGPA) {
		t.Fatalf("bootinfo stack arg = %#x, want %#x", got, plan.BootInfoGPA)
	}
}
