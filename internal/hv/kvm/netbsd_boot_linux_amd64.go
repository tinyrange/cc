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
	"j5.nz/cc/internal/serial"
	"j5.nz/cc/internal/virtio"
)

func BootNetBSDKernelToSerial(ctx context.Context, kernel []byte, memoryMB uint64) (string, error) {
	return bootNetBSDKernelToSerial(ctx, kernel, memoryMB, nil, nil)
}

func BootNetBSDKernelWithPCIBlockNetToSerial(ctx context.Context, kernel []byte, memoryMB uint64, block *virtio.Block, netdev *virtio.Net) (string, error) {
	return bootNetBSDKernelToSerial(ctx, kernel, memoryMB, block, netdev)
}

func bootNetBSDKernelToSerial(ctx context.Context, kernel []byte, memoryMB uint64, block *virtio.Block, netdev *virtio.Net) (string, error) {
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
	if block != nil {
		block.Attach(vm, vm)
		pciDevices = append(pciDevices, NewVirtioBlockPCIDevice(1, 0x1000, 10, block))
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
		if text := serialOut.String(); strings.Contains(text, "root on ld0") || len(text) >= 16384 {
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
			if err := handleBootMMIOForVCPU(vm, 0, nil, nil, nil, nil, exit.MMIO); err != nil {
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
