//go:build darwin && arm64

package hypervisor

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"testing"
	"time"
)

type mmioStoreCapture struct {
	size  int
	value uint64
}

type mmioAccessCapture struct {
	address uint64
	size    int
	value   uint64
}

func (d *mmioAccessCapture) Contains(a uint64, n int) bool         { return a == d.address && n == d.size }
func (d *mmioAccessCapture) Read(uint64, int) (uint64, error)      { return d.value, nil }
func (d *mmioAccessCapture) Write(_ uint64, _ int, v uint64) error { d.value = v; return nil }

// Compare emulated accesses against the same instructions executing natively
// against RAM, including signed loads, store width and base writeback.
func TestARM64MMIOIndexedAgainstNativeRAM(t *testing.T) {
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
	ram, err := vm.MapRAM(0x70000000, 0x10000)
	if err != nil {
		t.Fatal(err)
	}
	words := []uint32{0xb8004c20, 0xf81f8420, 0x38c01c20, 0xb85fc420, 0xb8004c3f}
	// Populate every code slot before execution; host writes to previously
	// executed guest code do not imply guest instruction-cache invalidation.
	for j, word := range words {
		binary.LittleEndian.PutUint32(ram[j*8:], word)
		binary.LittleEndian.PutUint32(ram[j*8+4:], 0xd4000002)
	}
	for j, word := range words {
		i, err := DecodeARM64MMIOInstruction(word)
		if err != nil {
			t.Fatal(err)
		}
		s := ARM64State{PC: 0x70000000 + uint64(j)*8, PState: 0x3c5}
		s.X[0] = 0xfedcba9876543210
		s.X[1] = 0x70008000
		address := s.X[1]
		if i.Mode != 1 {
			address += uint64(i.Offset)
		}
		binary.LittleEndian.PutUint64(ram[address-0x70000000:], 0xfedcba9876543280)
		initial := binary.LittleEndian.Uint64(ram[address-0x70000000:])
		if err := vm.Restore(s); err != nil {
			t.Fatal(err)
		}
		ex, err := vm.Run(ctx)
		if err != nil || ex.Syndrome>>26 != 0x16 {
			t.Fatalf("native %#x: %+v %v", word, ex, err)
		}
		want0, _ := vm.Register(0)
		want1, _ := vm.Register(1)
		wantStore := binary.LittleEndian.Uint64(ram[address-0x70000000:])
		if i.Size < 8 {
			wantStore &= uint64(1)<<(i.Size*8) - 1
		}
		s.X[1] = 0x30000000
		if err := vm.Restore(s); err != nil {
			t.Fatal(err)
		}
		ex, err = vm.Run(ctx)
		if err != nil || ex.Syndrome>>26 != 0x24 || ex.Syndrome&(1<<24) != 0 {
			t.Fatalf("fault %#x: %+v %v", word, ex, err)
		}
		d := &mmioAccessCapture{address: address - 0x70008000 + 0x30000000, size: i.Size, value: initial}
		handled, err := vm.EmulateMMIOInstruction(ex, word, d)
		got0, _ := vm.Register(0)
		got1, _ := vm.Register(1)
		pc, _ := vm.Register(RegisterPC)
		if !handled || err != nil || got0 != want0 || got1-0x30000000 != want1-0x70008000 || pc != s.PC+4 || i.Write && d.value != wantStore {
			t.Fatalf("%#x: handled=%v err=%v x0=%#x/%#x x1=%#x/%#x pc=%#x store=%#x/%#x", word, handled, err, got0, want0, got1, want1, pc, d.value, wantStore)
		}
	}
}

func TestARM64RAMProtection(t *testing.T) {
	if os.Getenv("CC_HVF_TEST") != "1" {
		t.Skip("requires hypervisor-entitled executable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	vm, err := NewARM64(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()
	ram, err := vm.MapRAM(0x70000000, 0x10000)
	if err != nil {
		t.Fatal(err)
	}
	// STR X0,[X1]; HVC #0, and a separate read/execute control.
	for j, w := range []uint32{0xf9000020, 0xd4000002, 0xf9400020, 0xd4000002} {
		binary.LittleEndian.PutUint32(ram[j*4:], w)
	}
	binary.LittleEndian.PutUint64(ram[0x8000:], 0x123456789abcdef0)
	if err := vm.ProtectRAM(0x70008000, 0x4000, true, false, true); err != nil {
		t.Fatal(err)
	}
	s := ARM64State{PC: 0x70000000, PState: 0x3c5}
	s.X[0] = 0xdead
	s.X[1] = 0x70008000
	if err := vm.Restore(s); err != nil {
		t.Fatal(err)
	}
	ex, err := vm.Run(ctx)
	if err != nil || ex.Syndrome>>26 != 0x24 || ex.Syndrome&0x40 == 0 || ex.PhysicalAddress != s.X[1] || binary.LittleEndian.Uint64(ram[0x8000:]) != 0x123456789abcdef0 {
		t.Fatalf("write protection: %+v %v", ex, err)
	}
	s.PC += 8
	if err := vm.Restore(s); err != nil {
		t.Fatal(err)
	}
	ex, err = vm.Run(ctx)
	got, _ := vm.Register(0)
	if err != nil || ex.Syndrome>>26 != 0x16 || got != 0x123456789abcdef0 {
		t.Fatalf("read control: %+v %#x %v", ex, got, err)
	}
	if err := vm.ProtectRAM(0x70010000, 0x4000, true, false, true); err == nil {
		t.Fatal("protected unmapped range")
	}
}

func TestARM64PrivateApplePACInitialization(t *testing.T) {
	if os.Getenv("CC_HVF_PRIVATE_ISA_TEST") != "1" {
		t.Skip("requires private-hypervisor-entitled executable and configured host")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	vm, err := NewARM64WithConfiguration(ctx, ARM64Configuration{PrivateAppleISA: true})
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()
	ram, err := vm.MapRAM(0x70000000, 0x10000)
	if err != nil {
		t.Fatal(err)
	}
	// Original PAC initialization HVC, followed by a trapped breakpoint.
	binary.LittleEndian.PutUint32(ram, 0xd4000002)
	binary.LittleEndian.PutUint32(ram[4:], 0xd4200000)
	if err := vm.SetDebugExceptionTrapping(true); err != nil {
		t.Fatal(err)
	}
	s := ARM64State{PC: 0x70000000, PState: 0x3c5}
	s.X[0] = 0xc1000000
	if err := vm.Restore(s); err != nil {
		t.Fatal(err)
	}
	ex, err := vm.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	x0, err := vm.Register(0)
	if err != nil || ex.PC != 0x70000004 || ex.Syndrome>>26 != 0x3c || x0 != 0 {
		t.Fatalf("private ISA PAC initialization: %+v x0=%#x err=%v", ex, x0, err)
	}
}

// Generic and nested HVF both reserve Apple's CPU-service namespace. This
// control distinguishes an absent board handler from a call consumed by HVF.
func TestARM64AppleCPUServiceRouting(t *testing.T) {
	if os.Getenv("CC_HVF_TEST") != "1" {
		t.Skip("requires hypervisor-entitled test executable")
	}
	for _, nested := range []bool{false, true} {
		t.Run(fmt.Sprintf("nested=%v", nested), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			vm, err := NewARM64WithConfiguration(ctx, ARM64Configuration{NestedVirtualization: nested})
			if err != nil {
				t.Fatal(err)
			}
			defer vm.Close()
			ram, err := vm.MapRAM(0x70000000, 0x10000)
			if err != nil {
				t.Fatal(err)
			}
			// HVC #0 followed by BRK #0; guest EL2 vector contains BRK #1.
			binary.LittleEndian.PutUint32(ram, 0xd4000002)
			binary.LittleEndian.PutUint32(ram[4:], 0xd4200000)
			for i := 0; i < 16; i++ {
				binary.LittleEndian.PutUint32(ram[0x4000+i*128:], 0xd4200020)
			}
			binary.LittleEndian.PutUint32(ram[0x5000:], 0xd69f03e0)
			if err := vm.SetDebugExceptionTrapping(true); err != nil {
				t.Fatal(err)
			}
			for _, function := range []uint64{0, 0xc1000000} {
				s := ARM64State{PC: 0x70000000, PState: 0x3c5}
				s.X[0] = function
				if nested {
					s.PC = 0x70005000
					s.PState = 0x3c9
					s.System = map[uint16]uint64{0xe088: 1<<31 | 1<<40 | 1<<41, 0xe600: 0x70004000, 0xe201: 0x70000000, 0xe200: 0x3c5, 0xf208: 0x7000c000}
				}
				if err := vm.Restore(s); err != nil {
					t.Fatal(err)
				}
				ex, err := vm.Run(ctx)
				if err != nil {
					t.Fatal(err)
				}
				x0, _ := vm.Register(0)
				t.Logf("function=%#x pc=%#x ec=%#x x0=%#x", function, ex.PC, ex.Syndrome>>26, x0)
				if function == 0 {
					wantPC, wantEC := uint64(0x70000004), uint64(0x16)
					if nested {
						wantPC, wantEC = 0x70004400, 0x3c
					}
					if ex.PC != wantPC || ex.Syndrome>>26 != wantEC {
						t.Fatalf("ordinary HVC routing: %+v", ex)
					}
				}
				if function == 0xc1000000 && (ex.PC != 0x70000004 || ex.Syndrome>>26 != 0x3c || x0 != 0xffffffff) {
					t.Fatalf("Apple CPU call routing changed: %+v x0=%#x", ex, x0)
				}
			}
		})
	}
}

func (*mmioStoreCapture) Contains(address uint64, size int) bool {
	return address == 0x30000000 && (size == 1 || size == 2 || size == 4 || size == 8)
}
func (*mmioStoreCapture) Read(uint64, int) (uint64, error) { return 0, nil }
func (d *mmioStoreCapture) Write(_ uint64, size int, value uint64) error {
	d.size, d.value = size, value
	return nil
}

func TestARM64MMIOStoreWidths(t *testing.T) {
	if os.Getenv("CC_HVF_TEST") != "1" {
		t.Skip("requires hypervisor-entitled test executable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	vm, err := NewARM64WithConfiguration(ctx, ARM64Configuration{GICDistributor: 0x10000000, GICRedistributor: 0x10010000})
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()
	ram, err := vm.MapRAM(0x70000000, 0x10000)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		instruction uint32
		size        int
		want        uint64
	}{{0x39000020, 1, 0x10}, {0x79000020, 2, 0x3210}, {0xb9000020, 4, 0x76543210}, {0xf9000020, 8, 0xfedcba9876543210}}
	for j, test := range tests {
		// STRB/STRH/STR W0/STR X0,[X1], followed by HVC #0.
		binary.LittleEndian.PutUint32(ram[j*8:], test.instruction)
		binary.LittleEndian.PutUint32(ram[j*8+4:], 0xd4000002)
	}
	for j, test := range tests {
		state := ARM64State{PC: 0x70000000 + uint64(j)*8, PState: 0x3c5}
		state.X[0], state.X[1] = 0xfedcba9876543210, 0x30000000
		if err := vm.Restore(state); err != nil {
			t.Fatal(err)
		}
		ex, err := vm.Run(ctx)
		if err != nil {
			t.Fatal(err)
		}
		device := &mmioStoreCapture{}
		handled, err := vm.EmulateMMIO(ex, device)
		if !handled || err != nil || device.size != test.size || device.value != test.want {
			t.Fatalf("store%d: handled=%v size=%d value=%#x err=%v", test.size, handled, device.size, device.value, err)
		}
		ex, err = vm.Run(ctx)
		if err != nil || ex.Syndrome>>26 != 0x16 {
			t.Fatalf("resume after store%d: %+v %v", test.size, ex, err)
		}
	}
}

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
	binary.LittleEndian.PutUint32(ram[0x100:], 0xd43e0080)
	binary.LittleEndian.PutUint32(ram[0x2200:], 0xd4000002)
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
	state.PC = 0x40000100
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
