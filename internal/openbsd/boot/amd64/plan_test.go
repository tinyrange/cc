package amd64

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrepareBootOpenBSD79BSD(t *testing.T) {
	testPrepareBootFixture(t, filepath.Join("..", "..", "..", "..", "local", "openbsd79-amd64", "bsd"))
}

func TestPrepareBootOpenBSD79BSDRD(t *testing.T) {
	testPrepareBootFixture(t, filepath.Join("..", "..", "..", "..", "local", "openbsd79-amd64", "bsd.rd"))
}

func testPrepareBootFixture(t *testing.T, path string) {
	kernel, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			t.Skipf("OpenBSD fixture not present: %s", path)
		}
		t.Fatalf("read fixture: %v", err)
	}
	mem := make([]byte, 128<<20)
	plan, err := PrepareBoot(mem, kernel, BootOptions{MemorySize: uint64(len(mem))})
	if err != nil {
		t.Fatalf("prepare boot: %v", err)
	}
	if plan.EntryGPA != 0x1001000 {
		t.Fatalf("EntryGPA = %#x, want %#x", plan.EntryGPA, uint64(0x1001000))
	}
	if plan.KernelEnd <= 0x1000000 {
		t.Fatalf("KernelEnd = %#x, want above kernel load base", plan.KernelEnd)
	}
	if plan.BootArgsGPA != defaultBootArgsGPA {
		t.Fatalf("BootArgsGPA = %#x, want %#x", plan.BootArgsGPA, uint64(defaultBootArgsGPA))
	}
	if plan.BootArgsLen == 0 || plan.BootArgsLen >= pageSize {
		t.Fatalf("BootArgsLen = %d, want one page bounded", plan.BootArgsLen)
	}
	if got := mem[defaultBootArgsGPA]; got != bootargMemmap {
		t.Fatalf("bootargs first type byte = %#x, want BOOTARG_MEMMAP", got)
	}
	if plan.StackGPA == 0 || plan.StackGPA >= defaultStackGPA {
		t.Fatalf("StackGPA = %#x, want below default stack top", plan.StackGPA)
	}
}
