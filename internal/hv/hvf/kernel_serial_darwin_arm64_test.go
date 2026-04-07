//go:build darwin && arm64

package hvf

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"j5.nz/cc/internal/kernel/alpine"
	bootarm64 "j5.nz/cc/internal/linux/boot/arm64"
	"j5.nz/cc/internal/serial"
)

const (
	testMemoryBase    = 0xa0000000
	testMemorySize    = 256 << 20
	gicDistributorMin = 0x08000000
	gicDistributorMax = gicDistributorMin + 0x00010000
	gicRedistribMin   = 0x080a0000
	gicRedistribMax   = gicRedistribMin + 0x00020000
)

func testBootKernelPrintsToSerial(t *testing.T, vm *VM) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	kernelRoot := filepath.Join(t.TempDir(), "kernel")
	manager := alpine.NewManager(kernelRoot)
	if err := manager.Ensure(ctx); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	kernelFile, err := manager.ReadKernel()
	if err != nil {
		t.Fatalf("ReadKernel() error = %v", err)
	}

	mem, err := vm.MapAnonymousMemory(testMemorySize, testMemoryBase, hvMemoryRead|hvMemoryWrite|hvMemoryExec)
	if err != nil {
		t.Fatalf("MapAnonymousMemory() error = %v", err)
	}

	var serialOut bytes.Buffer
	uart := serial.NewUART8250(bootarm64.DefaultUARTBase, bootarm64.DefaultUARTRegShift, &serialOut)

	plan, err := bootarm64.PrepareBoot(mem, kernelFile, bootarm64.BootOptions{
		MemoryBase: testMemoryBase,
		MemorySize: testMemorySize,
		NumCPUs:    1,
		Cmdline: strings.Join([]string{
			"console=ttyS0,115200n8",
			fmt.Sprintf("earlycon=uart8250,mmio,0x%x", bootarm64.DefaultUARTBase),
			"keep_bootcon",
			"nokaslr",
			"loglevel=8",
		}, " "),
	})
	if err != nil {
		t.Fatalf("PrepareBoot() error = %v", err)
	}

	if err := vm.SetReg(hvRegPC, plan.EntryGPA); err != nil {
		t.Fatalf("SetReg(PC) error = %v", err)
	}
	if err := vm.SetReg(hvRegCPSR, bootarm64.DefaultPStateBits); err != nil {
		t.Fatalf("SetReg(CPSR) error = %v", err)
	}
	if err := vm.SetSysReg(hvSysRegSP_EL1, plan.StackTopGPA); err != nil {
		t.Fatalf("SetSysReg(SP_EL1) error = %v", err)
	}
	if err := vm.SetReg(hvRegX0, plan.DeviceTreeGPA); err != nil {
		t.Fatalf("SetReg(X0) error = %v", err)
	}
	if err := vm.SetReg(hvRegX1, 0); err != nil {
		t.Fatalf("SetReg(X1) error = %v", err)
	}
	if err := vm.SetReg(hvRegX2, 0); err != nil {
		t.Fatalf("SetReg(X2) error = %v", err)
	}
	if err := vm.SetReg(hvRegX3, 0); err != nil {
		t.Fatalf("SetReg(X3) error = %v", err)
	}

	deadline := time.Now().Add(90 * time.Second)
	for steps := 0; time.Now().Before(deadline); steps++ {
		exitInfo, err := vm.Run()
		if err != nil {
			t.Fatalf("Run() error = %v\nserial:\n%s", err, serialOut.String())
		}
		if exitInfo == nil {
			t.Fatalf("Run() exit info = nil")
		}
		if serialOut.Len() > 0 {
			t.Logf("serial output:\n%s", serialOut.String())
			return
		}
		if exitInfo.Reason != hvExitReasonException {
			t.Fatalf("Run() exit reason = %v, want %v\nserial:\n%s", exitInfo.Reason, hvExitReasonException, serialOut.String())
		}

		switch DecodeExceptionClass(exitInfo.Exception.Syndrome) {
		case ExceptionClassDataAbortLowerEL:
			if err := handleTestDataAbort(vm, uart, exitInfo); err != nil {
				t.Fatalf("handle data abort: %v\nserial:\n%s", err, serialOut.String())
			}
		case ExceptionClassHVC64:
			halt, err := handleTestHVC(vm)
			if err != nil {
				t.Fatalf("handle hvc: %v\nserial:\n%s", err, serialOut.String())
			}
			if halt {
				if serialOut.Len() > 0 {
					t.Logf("serial output:\n%s", serialOut.String())
					return
				}
				t.Fatalf("guest halted before producing serial output")
			}
		default:
			t.Fatalf("unexpected exception class %#x syndrome=%#x physical=%#x\nserial:\n%s", DecodeExceptionClass(exitInfo.Exception.Syndrome), exitInfo.Exception.Syndrome, uint64(exitInfo.Exception.PhysicalAddress), serialOut.String())
		}
	}

	t.Fatalf("kernel did not print to serial before timeout\nserial:\n%s", serialOut.String())
}

func handleTestDataAbort(vm *VM, uart *serial.UART8250, exitInfo *VcpuExit) error {
	info, err := DecodeDataAbort(exitInfo.Exception.Syndrome)
	if err != nil {
		return err
	}
	addr := uint64(exitInfo.Exception.PhysicalAddress)

	switch {
	case uart.Contains(addr, info.SizeBytes):
		if info.Write {
			value, err := readDataAbortValue(vm, info)
			if err != nil {
				return err
			}
			if err := uart.WriteValue(addr, info.SizeBytes, value); err != nil {
				return err
			}
		} else {
			value, err := uart.ReadValue(addr, info.SizeBytes)
			if err != nil {
				return err
			}
			if err := writeDataAbortValue(vm, info, value); err != nil {
				return err
			}
		}
	case inRange(addr, gicDistributorMin, gicDistributorMax) || inRange(addr, gicRedistribMin, gicRedistribMax):
		if !info.Write {
			if err := writeDataAbortValue(vm, info, 0); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unhandled MMIO access addr=%#x size=%d write=%v", addr, info.SizeBytes, info.Write)
	}

	return vm.AdvanceProgramCounter()
}

func handleTestHVC(vm *VM) (bool, error) {
	x0, err := vm.GetReg(hvRegX0)
	if err != nil {
		return false, err
	}

	const (
		psciVersion         = 0x84000000
		psciCpuSuspend      = 0x84000001
		psciCpuOff          = 0x84000002
		psciCpuOn           = 0x84000003
		psciAffinityInfo    = 0x84000004
		psciMigrateInfoType = 0x84000006
		psciSystemOff       = 0x84000008
		psciSystemReset     = 0x84000009
		psciFeatures        = 0x8400000a
		psciSuccess         = 0
		psciNotSupported    = 0xffffffff
		psciInvalidParams   = 0xfffffffe
		psciTosNotPresent   = 2
	)

	var ret uint64
	switch x0 {
	case psciVersion:
		ret = 0x00010000
	case psciMigrateInfoType:
		ret = psciTosNotPresent
	case psciFeatures:
		ret = psciNotSupported
	case psciCpuSuspend:
		ret = psciNotSupported
	case psciCpuOff:
		ret = psciSuccess
	case psciAffinityInfo:
		ret = psciInvalidParams
	case psciCpuOn:
		ret = psciInvalidParams
	case psciSystemOff, psciSystemReset:
		return true, nil
	default:
		return false, fmt.Errorf("unsupported PSCI call %#x", x0)
	}

	if err := vm.SetReg(hvRegX0, ret); err != nil {
		return false, err
	}
	return false, vm.AdvanceProgramCounter()
}

func readDataAbortValue(vm *VM, info DataAbortInfo) (uint64, error) {
	if info.Target == hvRegXZR {
		return 0, nil
	}
	value, err := vm.GetReg(info.Target)
	if err != nil {
		return 0, err
	}
	if info.SizeBytes >= 8 {
		return value, nil
	}
	return value & ((uint64(1) << (8 * info.SizeBytes)) - 1), nil
}

func writeDataAbortValue(vm *VM, info DataAbortInfo, value uint64) error {
	if info.Target == hvRegXZR {
		return nil
	}
	if info.SizeBytes < 8 {
		value &= (uint64(1) << (8 * info.SizeBytes)) - 1
	}
	return vm.SetReg(info.Target, value)
}

func inRange(addr, start, end uint64) bool {
	return addr >= start && addr < end
}
