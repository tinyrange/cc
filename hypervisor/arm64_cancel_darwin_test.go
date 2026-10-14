//go:build darwin && arm64

package hypervisor

import (
	"context"
	"encoding/binary"
	"os"
	"testing"
	"time"
)

// Channel input can request an exit after device polling but before Run. HVF
// promises to retain that request, which must neither enter the idle guest nor
// poison the following execution.
func TestARM64CancelBeforeRun(t *testing.T) {
	if os.Getenv("CC_HVF_TEST") != "1" {
		t.Skip("requires hypervisor-entitled executable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	vm, err := NewARM64(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()
	const base = 0x70000000
	ram, err := vm.MapRAM(base, 0x10000)
	if err != nil {
		t.Fatal(err)
	}
	// WFI; B .-4. A separate HVC #1 proves that cancellation is consumed.
	for i, word := range []uint32{0xd503207f, 0x17ffffff, 0xd4000022} {
		binary.LittleEndian.PutUint32(ram[i*4:], word)
	}
	for range 4 {
		if err := vm.Restore(ARM64State{PC: base, PState: 0x3c5}); err != nil {
			t.Fatal(err)
		}
		if err := vm.Cancel(); err != nil {
			t.Fatal(err)
		}
		ex, err := vm.Run(ctx)
		if err != nil || ctx.Err() != nil || ex.Reason != ExitCanceled || ex.PC != base {
			t.Fatalf("pre-run cancellation lost or guest entered: exit=%+v err=%v context=%v", ex, err, ctx.Err())
		}
		if err := vm.Restore(ARM64State{PC: base + 8, PState: 0x3c5}); err != nil {
			t.Fatal(err)
		}
		ex, err = vm.Run(ctx)
		if err != nil || ex.Reason != ExitException || ex.Syndrome>>26 != 0x16 || ex.Syndrome&0xffff != 1 || ex.PC != base+12 {
			t.Fatalf("following run retained cancellation: exit=%+v err=%v", ex, err)
		}
	}
}
