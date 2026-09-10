//go:build darwin && arm64

package hypervisor

import (
	"context"
	"encoding/binary"
	"os"
	"testing"
	"time"
)

func TestARM64Restore(t *testing.T) {
	if os.Getenv("CC_HVF_TEST") != "1" {
		t.Skip("requires hypervisor-entitled test executable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	vm, err := NewARM64(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()
	ram, err := vm.MapRAM(0x40000000, 0x10000)
	if err != nil {
		t.Fatal(err)
	}
	// STR Q0,[X0]; STR Q31,[X0,#16]; ADD X1,X1,#1;
	// MRS X2,PMCCNTR_EL0; MRS X3,CNTVCT_EL0; HVC #0.
	for i, w := range []uint32{0x3d800000, 0x3d80041f, 0x91000421, 0xd53b9d02, 0xd53be043, 0xd4000002} {
		binary.LittleEndian.PutUint32(ram[i*4:], w)
	}
	state := ARM64State{PC: 0x40000000, PState: 0x3c5, System: map[uint16]uint64{0xc082: 3 << 20}}
	dfr, err := vm.SystemRegister(0xc028)
	if err != nil {
		t.Fatal(err)
	}
	state.System[0xc028] = dfr&^uint64(0xf00) | 0x100
	state.X[0] = 0x40001000
	state.X[1] = 41
	for i := range state.Q[0] {
		state.Q[0][i] = byte(i + 1)
		state.Q[31][i] = byte(255 - i)
	}
	if err := vm.Restore(state); err != nil {
		t.Fatal(err)
	}
	before, frequency, err := vm.Counter()
	if err != nil || frequency == 0 {
		t.Fatalf("counter frequency: %d %v", frequency, err)
	}
	ex, err := vm.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if ex.Syndrome>>26 != 0x16 {
		t.Fatalf("exit: %+v", ex)
	}
	after, _, err := vm.Counter()
	if err != nil {
		t.Fatal(err)
	}
	guestCounter, err := vm.Register(3)
	if err != nil || guestCounter < before || guestCounter > after {
		t.Fatalf("guest counter %d outside native interval [%d,%d]: %v", guestCounter, before, after, err)
	}
	x1, err := vm.Register(1)
	if err != nil || x1 != 42 {
		t.Fatalf("x1=%d: %v", x1, err)
	}
	for i := 0; i < 16; i++ {
		if ram[0x1000+i] != state.Q[0][i] || ram[0x1010+i] != state.Q[31][i] {
			t.Fatalf("SIMD byte %d not restored", i)
		}
	}
	// A trapped divide breakpoint must be deliverable to the guest vector
	// without losing the interrupted PC or PSTATE.
	binary.LittleEndian.PutUint32(ram[:], 0xd43e0080)
	binary.LittleEndian.PutUint32(ram[0x2200:], 0xd4000002)
	state.System[0xc600] = 0x40002000
	if err := vm.Restore(state); err != nil {
		t.Fatal(err)
	}
	if err := vm.SetDebugExceptionTrapping(true); err != nil {
		t.Fatal(err)
	}
	ex, err = vm.Run(ctx)
	if err != nil || ex.Syndrome>>26 != 0x3c {
		t.Fatalf("BRK: %+v %v", ex, err)
	}
	if err := vm.DeliverDebugException(ex); err != nil {
		t.Fatal(err)
	}
	ex, err = vm.Run(ctx)
	if err != nil || ex.Syndrome>>26 != 0x16 || ex.PC != 0x40002204 {
		t.Fatalf("vector: %+v %v", ex, err)
	}
	elr, err := vm.SystemRegister(0xc201)
	if err != nil || elr != state.PC {
		t.Fatalf("ELR=%#x: %v", elr, err)
	}
	saved, err := vm.SystemRegister(0xc200)
	if err != nil || saved != state.PState {
		t.Fatalf("SPSR=%#x: %v", saved, err)
	}
}

func TestARM64PSCIPowerDiscovery(t *testing.T) {
	if os.Getenv("CC_HVF_TEST") != "1" {
		t.Skip("requires hypervisor-entitled test executable")
	}
	vm, err := NewARM64(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()
	for _, call := range []struct {
		function, argument, want uint64
		terminal                 bool
	}{
		{0x84000000, 0, 0x10001, false},
		{0x80000000, 0, 0x10001, false},
		{0x8400000a, 0x80000000, 0, false},
		{0x8400000a, 0x84000008, 0, false},
		{0x8400000a, 0x84000009, 0, false},
		{0x8400000a, 0xc4000001, 0xffffffff, false},
		{0x8400000a, 0xffffffff, 0xffffffff, false},
		{0x80000001, 0x80000000, 0, false},
		{0x80000001, 0x80000001, 0, false},
		{0x80000001, 0x80008000, 0xffffffff, false},
		{0x84000008, 0, 0x84000008, true},
		{0x84000009, 0, 0x84000009, true},
	} {
		if err := vm.SetRegister(0, call.function); err != nil {
			t.Fatal(err)
		}
		if err := vm.SetRegister(1, call.argument); err != nil {
			t.Fatal(err)
		}
		terminal, err := vm.HandlePSCI()
		if err != nil || terminal != call.terminal {
			t.Fatalf("call %#x/%#x: terminal %v err %v", call.function, call.argument, terminal, err)
		}
		result, err := vm.Register(0)
		if err != nil || result != call.want {
			t.Fatalf("call %#x/%#x: result %#x want %#x err %v", call.function, call.argument, result, call.want, err)
		}
	}
}
