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
	netbsdamd64 "j5.nz/cc/internal/netbsd/boot/amd64"
	"j5.nz/cc/internal/nvme"
	"j5.nz/cc/internal/virtio"
)

func BootNetBSDKernelToSerial(ctx context.Context, kernel []byte, memoryMB uint64) (string, error) {
	return bootNetBSDKernelToSerial(ctx, kernel, memoryMB, nil, nil)
}

func BootNetBSDKernelWithNVMENetToSerial(ctx context.Context, kernel []byte, memoryMB uint64, block *nvme.Controller, netdev *virtio.Net) (string, error) {
	return bootNetBSDKernelToSerial(ctx, kernel, memoryMB, block, netdev)
}

func bootNetBSDKernelToSerial(ctx context.Context, kernel []byte, memoryMB uint64, block *nvme.Controller, netdev *virtio.Net) (string, error) {
	return bootNetBSDToCondition(ctx, kernel, memoryMB, block, netdev, nil, func(text string) bool { return strings.Contains(text, "root on ld0") || len(text) >= 16384 })
}

func BootNetBSDKernelToMarkerWithNVMeConsole(ctx context.Context, kernel []byte, memoryMB uint64, marker string, block *nvme.Controller, input func(string) []byte) (string, error) {
	if strings.TrimSpace(marker) == "" {
		return "", fmt.Errorf("boot marker is required")
	}
	return bootNetBSDToCondition(ctx, kernel, memoryMB, block, nil, input, func(text string) bool { return strings.Contains(text, marker) })
}

func bootNetBSDToCondition(ctx context.Context, kernel []byte, memoryMB uint64, block *nvme.Controller, netdev *virtio.Net, input func(string) []byte, done func(string) bool) (string, error) {
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
	uart := newAMD64UART(vm, &serialOut)
	var pci *PCIBus
	var pciDevices []*PCIDevice
	if block != nil {
		block.Attach(vm, vm)
		pciDevices = append(pciDevices, NewNVMePCIDevice(1, 0xfeb00000, 10, block))
	}
	if netdev != nil {
		netdev.Attach(vm, vm)
		netIndex := len(pciDevices) + 1
		pciDevices = append(pciDevices, NewVirtioNetPCIDevice(uint8(netIndex), uint16(0x1000+netIndex*0x100), uint8(10+netIndex), netdev))
	}
	if len(pciDevices) != 0 {
		pci = NewPCIBus(pciDevices...)
	}

	plan, err := netbsdamd64.PrepareBoot(mem, kernel, netbsdamd64.BootOptions{
		MemorySize: amd64vm.MemorySizeBytes(memoryMB),
	})
	if err != nil {
		return "", fmt.Errorf("prepare NetBSD boot: %w", err)
	}
	if err := vm.SetProtectedMode32(plan.EntryGPA, plan.StackGPA); err != nil {
		return "", fmt.Errorf("set NetBSD protected mode: %w", err)
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

	rtc := NewCMOSRTC(nil)
	acpiPM := NewACPIPM()
	var exit Exit
	for step := 0; ; step++ {
		if serialOut.Len() >= 1<<20 {
			return serialOut.String()[:1<<20], fmt.Errorf("BSD serial output exceeds 1 MiB")
		}
		if done(serialOut.String()) {
			return serialOut.String(), nil
		}
		if err := ctx.Err(); err != nil {
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
				go func(data []byte) { _ = uart.InjectRXBytes(data) }(append([]byte(nil), data...))
			}
		}
		switch exit.Reason {
		case ExitIO:
			if err := handleBootIOWithPCI(func(ioExit IOExit) error {
				if handled, err := acpiPM.HandleIO(ioExit); handled || err != nil {
					return err
				}
				if handled, err := rtc.HandleIO(ioExit); handled || err != nil {
					return err
				}
				return handleBootIO(uart, ioExit)
			}, pci, exit.IO); err != nil {
				return serialOut.String(), err
			}
		case ExitMMIO:
			if err := handleBootMMIOWithPCI(vm, 0, pci, nil, nil, nil, nil, nil, exit.MMIO); err != nil {
				return serialOut.String(), fmt.Errorf("unhandled NetBSD mmio addr=%#x len=%d write=%v: %w", exit.MMIO.Addr, exit.MMIO.Len, exit.MMIO.Write, err)
			}
		case ExitHLT:
			pc, _ := vm.GetPC()
			return serialOut.String(), fmt.Errorf("NetBSD guest halted at pc=%#x", pc)
		case ExitShutdown:
			pc, _ := vm.GetPC()
			return serialOut.String(), fmt.Errorf("NetBSD guest shut down at pc=%#x", pc)
		default:
			pc, _ := vm.GetPC()
			return serialOut.String(), fmt.Errorf("unexpected exit reason %d at pc=%#x", exit.Reason, pc)
		}
	}
}
