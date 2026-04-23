//go:build linux && amd64

package kvm

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"j5.nz/cc/internal/amd64vm"
	"j5.nz/cc/internal/serial"
)

func BootKernelToSerial(ctx context.Context, kernel []byte, memoryMB uint64, dmesg bool) (string, error) {
	return bootToCondition(ctx, kernel, nil, memoryMB, dmesg, func(serial string) bool {
		return serial != ""
	})
}

func BootInitramfsToMarker(ctx context.Context, kernel []byte, initrd []byte, memoryMB uint64, dmesg bool, marker string) (string, error) {
	if strings.TrimSpace(marker) == "" {
		return "", fmt.Errorf("boot marker is required")
	}
	return bootToCondition(ctx, kernel, initrd, memoryMB, dmesg, func(serial string) bool {
		return strings.Contains(serial, marker)
	})
}

func bootToCondition(ctx context.Context, kernel []byte, initrd []byte, memoryMB uint64, dmesg bool, done func(string) bool) (string, error) {
	vm, err := NewVM()
	if err != nil {
		return "", err
	}
	defer vm.Close()

	mem, err := vm.MapAnonymousMemory(amd64vm.MemorySizeBytes(memoryMB), amd64vm.MemoryBase)
	if err != nil {
		return "", fmt.Errorf("map guest memory: %w", err)
	}

	var serialOut bytes.Buffer
	uart := serial.NewUART8250(amd64vm.COM1Base, 0, &serialOut)
	plan, err := amd64vm.PrepareBoot(mem, kernel, initrd, amd64vm.BootConfig{
		MemoryMB: memoryMB,
		Dmesg:    dmesg,
	})
	if err != nil {
		return "", fmt.Errorf("prepare boot: %w", err)
	}
	if err := vm.SetLongMode(plan.EntryGPA, plan.ZeroPageGPA, plan.StackTopGPA, plan.PagingBase); err != nil {
		return "", fmt.Errorf("set long mode: %w", err)
	}

	for step := 0; ; step++ {
		if err := ctx.Err(); err != nil {
			if done(serialOut.String()) {
				return serialOut.String(), nil
			}
			return serialOut.String(), err
		}
		exit, err := vm.Run()
		if err != nil {
			return serialOut.String(), fmt.Errorf("run step %d: %w", step, err)
		}
		if done(serialOut.String()) {
			return serialOut.String(), nil
		}
		switch exit.Reason {
		case ExitIO:
			if err := handleUARTIO(uart, exit.IO); err != nil {
				return serialOut.String(), err
			}
		case ExitHLT, ExitShutdown:
			return serialOut.String(), fmt.Errorf("guest shut down before serial output")
		case ExitSystemEvent:
			return serialOut.String(), fmt.Errorf("unexpected system event %d before serial output", exit.SystemEvent)
		default:
			pc, _ := vm.GetPC()
			return serialOut.String(), fmt.Errorf("unexpected exit reason %d at pc=%#x", exit.Reason, pc)
		}
	}
}

func BootKernelToSerialWithTimeout(kernel []byte, memoryMB uint64, dmesg bool, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return BootKernelToSerial(ctx, kernel, memoryMB, dmesg)
}

func BootInitramfsToMarkerWithTimeout(kernel []byte, initrd []byte, memoryMB uint64, dmesg bool, marker string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return BootInitramfsToMarker(ctx, kernel, initrd, memoryMB, dmesg, marker)
}

func handleUARTIO(uart *serial.UART8250, ioExit IOExit) error {
	if ioExit.Size == 0 || ioExit.Count == 0 {
		return nil
	}
	for i := uint32(0); i < ioExit.Count; i++ {
		off := uint64(i) * uint64(ioExit.Size)
		port := uint64(ioExit.Port)
		if ioExit.Write {
			if err := uart.Write(port, ioExit.Data[off:off+uint64(ioExit.Size)]); err != nil {
				return err
			}
			continue
		}
		value, err := uart.ReadValue(port, int(ioExit.Size))
		if err != nil {
			return err
		}
		switch ioExit.Size {
		case 1:
			ioExit.Data[off] = byte(value)
		case 2:
			ioExit.Data[off] = byte(value)
			ioExit.Data[off+1] = byte(value >> 8)
		default:
			for j := uint8(0); j < ioExit.Size; j++ {
				ioExit.Data[off+uint64(j)] = byte(value >> (8 * j))
			}
		}
	}
	return nil
}
