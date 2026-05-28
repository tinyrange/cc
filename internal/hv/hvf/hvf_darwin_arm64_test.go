//go:build darwin && arm64

package hvf

import (
	"encoding/binary"
	"os"
	"runtime"
	"testing"

	"j5.nz/cc/internal/macos"
)

const initialPStateEL1h = 0x3c5

func TestMain(m *testing.M) {
	if err := macos.EnsureExecutableIsSigned(); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

func TestNestedVirtualizationSupportedProbe(t *testing.T) {
	if _, err := NestedVirtualizationSupported(); err != nil {
		t.Fatalf("NestedVirtualizationSupported() error = %v", err)
	}
}

func TestHVFBringupStages(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	withVM := func(t *testing.T, fn func(*VM)) {
		t.Helper()

		vm, err := NewVM()
		if err != nil {
			t.Fatalf("NewVM() error = %v", err)
		}
		defer func() {
			if err := vm.Close(); err != nil {
				t.Fatalf("Close() error = %v", err)
			}
		}()

		fn(vm)
	}

	pageSize := os.Getpagesize()

	t.Run("gic icc cpu interface is initialized", func(t *testing.T) {
		withVM(t, func(vm *VM) {
			sre, err := vm.GetGICICCReg(hvGICICCRegSRE_EL1)
			if err != nil {
				t.Fatalf("GetGICICCReg(SRE_EL1) error = %v", err)
			}
			if sre&0x1 == 0 {
				t.Fatalf("ICC_SRE_EL1 = %#x, want SRE bit set", sre)
			}

			pmr, err := vm.GetGICICCReg(hvGICICCRegPMR_EL1)
			if err != nil {
				t.Fatalf("GetGICICCReg(PMR_EL1) error = %v", err)
			}
			if pmr&0xf8 != 0xf8 {
				t.Fatalf("ICC_PMR_EL1 = %#x, want implemented priority mask bits set", pmr)
			}
		})
	})

	t.Run("virtual timer is unmasked", func(t *testing.T) {
		withVM(t, func(vm *VM) {
			masked, err := vm.GetVTimerMask()
			if err != nil {
				t.Fatalf("GetVTimerMask() error = %v", err)
			}
			if masked {
				t.Fatal("virtual timer is masked, want unmasked")
			}
		})
	})

	t.Run("single brk exits", func(t *testing.T) {
		withVM(t, func(vm *VM) {
			const guestAddr IPA = 0x80000000
			mem, err := vm.MapAnonymousMemory(uintptr(pageSize), guestAddr, hvMemoryRead|hvMemoryWrite|hvMemoryExec)
			if err != nil {
				t.Fatalf("MapAnonymousMemory() error = %v", err)
			}
			binary.LittleEndian.PutUint32(mem[0:4], 0xd4200000) // brk #0

			if err := vm.SetReg(hvRegPC, uint64(guestAddr)); err != nil {
				t.Fatalf("SetReg(PC) error = %v", err)
			}
			if err := vm.SetReg(hvRegCPSR, initialPStateEL1h); err != nil {
				t.Fatalf("SetReg(CPSR) error = %v", err)
			}

			exitInfo, err := vm.Run()
			if err != nil {
				t.Fatalf("Run() error = %v", err)
			}
			if exitInfo == nil {
				t.Fatal("Run() exit info = nil")
			}
			if exitInfo.Reason != hvExitReasonException {
				t.Fatalf("Run() exit reason = %v, want %v", exitInfo.Reason, hvExitReasonException)
			}
		})
	})

	t.Run("mmio write trap exits with physical address", func(t *testing.T) {
		withVM(t, func(vm *VM) {
			const guestAddr IPA = 0x80004000
			mem, err := vm.MapAnonymousMemory(uintptr(pageSize), guestAddr, hvMemoryRead|hvMemoryWrite|hvMemoryExec)
			if err != nil {
				t.Fatalf("MapAnonymousMemory() error = %v", err)
			}

			// str w0, [x1]
			binary.LittleEndian.PutUint32(mem[0:4], 0xb9000020)

			const mmioAddr = 0x09000000
			if err := vm.SetReg(hvRegX0, 0x41); err != nil {
				t.Fatalf("SetReg(X0) error = %v", err)
			}
			if err := vm.SetReg(hvRegX1, mmioAddr); err != nil {
				t.Fatalf("SetReg(X1) error = %v", err)
			}
			if err := vm.SetReg(hvRegPC, uint64(guestAddr)); err != nil {
				t.Fatalf("SetReg(PC) error = %v", err)
			}
			if err := vm.SetReg(hvRegCPSR, initialPStateEL1h); err != nil {
				t.Fatalf("SetReg(CPSR) error = %v", err)
			}

			exitInfo, err := vm.Run()
			if err != nil {
				t.Fatalf("Run() error = %v", err)
			}
			if exitInfo == nil {
				t.Fatal("Run() exit info = nil")
			}
			if exitInfo.Reason != hvExitReasonException {
				t.Fatalf("Run() exit reason = %v, want %v", exitInfo.Reason, hvExitReasonException)
			}
			if got := uint64(exitInfo.Exception.PhysicalAddress); got != mmioAddr {
				t.Fatalf("Run() physical address = %#x, want %#x", got, mmioAddr)
			}
		})
	})

	t.Run("initramfs init prints hello world", func(t *testing.T) {
		if os.Getenv("CCX3_RUN_HVF_INITRAMFS_TEST") == "" {
			t.Skip("set CCX3_RUN_HVF_INITRAMFS_TEST=1 to run the initramfs userspace boot test")
		}
		withVM(t, func(vm *VM) {
			testBootHelloWorldInit(t, vm)
		})
	})

	t.Run("serial interrupt delivery", func(t *testing.T) {
		if os.Getenv("CCX3_RUN_HVF_IRQ_TEST") == "" {
			t.Skip("set CCX3_RUN_HVF_IRQ_TEST=1 to run the serial interrupt delivery test")
		}
		withVM(t, func(vm *VM) {
			testSerialInterruptDelivery(t, vm)
		})
	})
}
