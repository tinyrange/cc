//go:build linux && amd64

package kvm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"

	"golang.org/x/sys/unix"
	"j5.nz/cc/internal/amd64vm"
	bootamd64 "j5.nz/cc/internal/freestanding/boot/amd64"
)

// BootFreestandingELFToMarker directly loads a higher-half ELF kernel, enters
// it in long mode, and captures COM1 until marker is printed.
func BootFreestandingELFToMarker(ctx context.Context, kernel []byte, memoryMB uint64, marker string) (string, error) {
	if strings.TrimSpace(marker) == "" {
		return "", fmt.Errorf("boot marker is required")
	}
	vm, err := NewVM()
	if err != nil {
		return "", err
	}
	defer vm.Close()

	memory, err := mapAMD64GuestMemory(vm, memoryMB)
	if err != nil {
		return "", fmt.Errorf("map guest memory: %w", err)
	}
	plan, err := bootamd64.PrepareBoot(memory, kernel, bootamd64.BootOptions{
		MemorySize: amd64vm.MemorySizeBytes(memoryMB),
	})
	if err != nil {
		return "", fmt.Errorf("prepare freestanding boot: %w", err)
	}
	if err := vm.SetFreestandingLongMode(plan.EntryGVA, plan.BootInfoGPA, plan.StackTopGPA, plan.PagingGPA); err != nil {
		return "", fmt.Errorf("set freestanding long mode: %w", err)
	}

	var serialOut bytes.Buffer
	uart := newAMD64UART(vm, &serialOut)
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	vm.SetVCPUTID(0, unix.Gettid())
	defer vm.SetVCPUTID(0, 0)
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
			return serialOut.String(), err
		}
		if err := vm.RunVCPUInterruptible(0, &exit); err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			return serialOut.String(), fmt.Errorf("run step %d: %w", step, err)
		}
		if strings.Contains(serialOut.String(), marker) {
			return serialOut.String(), nil
		}
		switch exit.Reason {
		case ExitIO:
			if err := handleBootIO(uart, exit.IO); err != nil {
				return serialOut.String(), err
			}
		case ExitHLT:
			return serialOut.String(), fmt.Errorf("freestanding guest halted before marker")
		case ExitShutdown:
			pc, _ := vm.GetPC()
			return serialOut.String(), fmt.Errorf("freestanding guest shut down before marker at pc=%#x", pc)
		case ExitSystemEvent:
			return serialOut.String(), fmt.Errorf("unexpected system event %d before marker", exit.SystemEvent)
		default:
			pc, _ := vm.GetPC()
			return serialOut.String(), fmt.Errorf("unexpected exit reason %d at pc=%#x", exit.Reason, pc)
		}
		if strings.Contains(serialOut.String(), marker) {
			return serialOut.String(), nil
		}
	}
}
