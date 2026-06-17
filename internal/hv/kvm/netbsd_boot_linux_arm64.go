//go:build linux && arm64

package kvm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"

	"golang.org/x/sys/unix"
	"j5.nz/cc/internal/arm64vm"
	netbsdarm64 "j5.nz/cc/internal/netbsd/boot/arm64"
	"j5.nz/cc/internal/serial"
)

const netBSDArm64MemoryBase = 0x40000000

func BootNetBSDKernelToMarker(ctx context.Context, kernel []byte, memoryMB uint64, marker string) (string, error) {
	if strings.TrimSpace(marker) == "" {
		return "", fmt.Errorf("boot marker is required")
	}
	return bootNetBSDToCondition(ctx, kernel, memoryMB, func(serial string) bool {
		return strings.Contains(serial, marker)
	})
}

func BootNetBSDKernelToSerial(ctx context.Context, kernel []byte, memoryMB uint64) (string, error) {
	return bootNetBSDToCondition(ctx, kernel, memoryMB, func(serial string) bool {
		return serial != ""
	})
}

func bootNetBSDToCondition(ctx context.Context, kernel []byte, memoryMB uint64, done func(string) bool) (string, error) {
	vm, err := NewVM()
	if err != nil {
		return "", err
	}
	defer vm.Close()

	mem, err := vm.MapAnonymousMemory(arm64vm.MemorySizeBytes(memoryMB), netBSDArm64MemoryBase)
	if err != nil {
		return "", fmt.Errorf("map guest memory: %w", err)
	}

	var serialOut bytes.Buffer
	uart := serial.NewUART8250(arm64vm.DefaultUARTBase, arm64vm.DefaultUARTRegShift, &serialOut)
	uart.AttachIRQ(vm, arm64vm.UARTSPI)

	plan, err := netbsdarm64.PrepareBoot(mem, kernel, netbsdarm64.BootOptions{
		MemoryBase: netBSDArm64MemoryBase,
		MemorySize: arm64vm.MemorySizeBytes(memoryMB),
		NumCPUs:    1,
		Console:    true,
	})
	if err != nil {
		return "", fmt.Errorf("prepare NetBSD boot: %w", err)
	}
	if err := setupNetBSDArm64Registers(vm, plan); err != nil {
		return "", err
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	vm.SetVCPUTID(unix.Gettid())
	defer vm.SetVCPUTID(0)
	cancelDone := make(chan struct{})
	defer close(cancelDone)
	go func() {
		select {
		case <-ctx.Done():
			vm.RequestImmediateExit()
		case <-cancelDone:
		}
	}()

	var exit Exit
	for step := 0; ; step++ {
		if err := ctx.Err(); err != nil {
			if done(serialOut.String()) {
				return serialOut.String(), nil
			}
			pc, _ := vm.GetPC()
			return serialOut.String(), fmt.Errorf("%w at pc=%#x", err, pc)
		}
		if err := vm.RunInterruptible(&exit); err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			return serialOut.String(), fmt.Errorf("run step %d: %w", step, err)
		}
		if done(serialOut.String()) {
			return serialOut.String(), nil
		}
		switch exit.Reason {
		case ExitMMIO:
			if uart.Contains(exit.MMIO.Addr, int(exit.MMIO.Len)) {
				if err := handleUARTExit(vm, uart, exit.MMIO); err != nil {
					return serialOut.String(), err
				}
				continue
			}
			return serialOut.String(), fmt.Errorf("unhandled NetBSD mmio addr=%#x len=%d write=%v", exit.MMIO.Addr, exit.MMIO.Len, exit.MMIO.Write)
		case ExitShutdown:
			return serialOut.String(), fmt.Errorf("NetBSD guest shut down before marker")
		case ExitSystemEvent:
			return serialOut.String(), fmt.Errorf("unexpected system event %d before marker", exit.SystemEvent)
		default:
			pc, _ := vm.GetPC()
			return serialOut.String(), fmt.Errorf("unexpected exit reason %d at pc=%#x", exit.Reason, pc)
		}
	}
}

func setupNetBSDArm64Registers(vm *VM, plan *netbsdarm64.BootPlan) error {
	if err := vm.SetPC(plan.EntryGPA); err != nil {
		return fmt.Errorf("set PC: %w", err)
	}
	if err := vm.SetPState(0x40000000 | arm64vm.DefaultPStateBits); err != nil {
		return fmt.Errorf("set PSTATE: %w", err)
	}
	if err := vm.SetX(0, plan.DeviceTreeGPA); err != nil {
		return fmt.Errorf("set X0: %w", err)
	}
	for reg := 1; reg <= 3; reg++ {
		if err := vm.SetX(reg, 0); err != nil {
			return fmt.Errorf("set X%d: %w", reg, err)
		}
	}
	return nil
}
