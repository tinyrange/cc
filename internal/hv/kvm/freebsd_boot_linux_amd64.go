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
	freebsdamd64 "j5.nz/cc/internal/freebsd/boot/amd64"
	"j5.nz/cc/internal/nvme"
	"j5.nz/cc/internal/serial"
	"j5.nz/cc/internal/virtio"
)

func BootFreeBSDKernelToMarker(ctx context.Context, kernel []byte, memoryMB uint64, marker string) (string, error) {
	if strings.TrimSpace(marker) == "" {
		return "", fmt.Errorf("boot marker is required")
	}
	return bootFreeBSDToCondition(ctx, kernel, memoryMB, nil, nil, nil, func(serial string) bool {
		return strings.Contains(serial, marker)
	})
}

func BootFreeBSDKernelToMarkerWithNVMENetConsole(ctx context.Context, kernel []byte, memoryMB uint64, marker string, block *nvme.Controller, netdev *virtio.Net, input func(string) []byte) (string, error) {
	if strings.TrimSpace(marker) == "" {
		return "", fmt.Errorf("boot marker is required")
	}
	return bootFreeBSDToCondition(ctx, kernel, memoryMB, []*nvme.Controller{block}, netdev, input, func(serial string) bool {
		return strings.Contains(serial, marker)
	})
}

func BootFreeBSDKernelToSerial(ctx context.Context, kernel []byte, memoryMB uint64) (string, error) {
	return bootFreeBSDToCondition(ctx, kernel, memoryMB, nil, nil, nil, func(serial string) bool {
		return serial != ""
	})
}

func bootFreeBSDToCondition(ctx context.Context, kernel []byte, memoryMB uint64, blocks []*nvme.Controller, netdev *virtio.Net, input func(string) []byte, done func(string) bool) (string, error) {
	vm, err := NewVM()
	if err != nil {
		return "", err
	}
	defer vm.Close()

	mem, err := mapAMD64GuestMemory(vm, memoryMB)
	if err != nil {
		return "", fmt.Errorf("map guest memory: %w", err)
	}

	var serialOut bytes.Buffer
	uart := serial.NewUART8250(amd64vm.COM1Base, 0, &serialOut)
	uart.AttachIRQ(vm, amd64vm.COM1IRQ)
	var pci *PCIBus
	var pciDevices []*PCIDevice
	for i, block := range blocks {
		if block == nil {
			continue
		}
		block.Attach(vm, vm)
		pciDevices = append(pciDevices, NewNVMePCIDevice(uint8(1+i), 0xfeb00000+uint64(i)*0x10000, uint8(10+i), block))
	}
	if netdev != nil {
		netdev.Attach(vm, vm)
		netIndex := len(pciDevices) + 1
		pciDevices = append(pciDevices, NewVirtioNetPCIDevice(uint8(netIndex), uint16(0x1000+netIndex*0x100), uint8(10+netIndex), netdev))
	}
	if len(pciDevices) > 0 {
		pci = NewPCIBus(pciDevices...)
	}
	plan, err := freebsdamd64.PrepareBoot(mem, kernel, freebsdamd64.BootOptions{
		MemorySize: amd64vm.MemorySizeBytes(memoryMB),
	})
	if err != nil {
		return "", fmt.Errorf("prepare FreeBSD boot: %w", err)
	}
	if err := vm.SetFreeBSDLongMode(plan.EntryGVA, plan.StackGPA, plan.PagingGPA); err != nil {
		return "", fmt.Errorf("set FreeBSD long mode: %w", err)
	}

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
			if done(serialOut.String()) {
				return serialOut.String(), nil
			}
			return serialOut.String(), err
		}
		if err := vm.RunVCPUInterruptible(0, &exit); err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			return serialOut.String(), fmt.Errorf("run step %d: %w", step, err)
		}
		if done(serialOut.String()) {
			return serialOut.String(), nil
		}
		if input != nil {
			if data := input(serialOut.String()); len(data) != 0 {
				go func(data []byte) {
					_ = uart.InjectRXBytes(data)
				}(append([]byte(nil), data...))
			}
		}
		switch exit.Reason {
		case ExitIO:
			if err := handleBootIOWithPCI(func(ioExit IOExit) error {
				return handleBootIO(uart, ioExit)
			}, pci, exit.IO); err != nil {
				return serialOut.String(), err
			}
		case ExitMMIO:
			if err := handleBootMMIOWithPCI(vm, 0, pci, nil, nil, nil, nil, nil, exit.MMIO); err != nil {
				return serialOut.String(), fmt.Errorf("unhandled FreeBSD mmio addr=%#x len=%d write=%v: %w", exit.MMIO.Addr, exit.MMIO.Len, exit.MMIO.Write, err)
			}
		case ExitHLT:
			return serialOut.String(), fmt.Errorf("FreeBSD guest halted before marker")
		case ExitShutdown:
			pc, _ := vm.GetPC()
			return serialOut.String(), fmt.Errorf("FreeBSD guest shut down before marker at pc=%#x", pc)
		case ExitSystemEvent:
			return serialOut.String(), fmt.Errorf("unexpected system event %d before marker", exit.SystemEvent)
		default:
			pc, _ := vm.GetPC()
			return serialOut.String(), fmt.Errorf("unexpected exit reason %d at pc=%#x", exit.Reason, pc)
		}
	}
}
