//go:build windows && arm64

package whp

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"j5.nz/cc/internal/arm64vm"
	"j5.nz/cc/internal/fdt"
	bootarm64 "j5.nz/cc/internal/linux/boot/arm64"
	"j5.nz/cc/internal/serial"
	"j5.nz/cc/internal/virtio"
)

func BootKernelToSerial(ctx context.Context, kernel []byte, memoryMB uint64, dmesg bool) (string, error) {
	return bootToCondition(ctx, kernel, nil, memoryMB, dmesg, nil, nil, nil, nil, func(serial string) bool {
		return serial != ""
	})
}

func BootInitramfsToMarker(ctx context.Context, kernel []byte, initrd []byte, memoryMB uint64, dmesg bool, marker string) (string, error) {
	if strings.TrimSpace(marker) == "" {
		return "", fmt.Errorf("boot marker is required")
	}
	return bootToCondition(ctx, kernel, initrd, memoryMB, dmesg, nil, nil, nil, nil, func(serial string) bool {
		return strings.Contains(serial, marker)
	})
}

func BootInitramfsToMarkerWithFSAndNet(ctx context.Context, kernel []byte, initrd []byte, memoryMB uint64, dmesg bool, marker string, fsdevs []*virtio.FS, netdev *virtio.Net) (string, error) {
	if strings.TrimSpace(marker) == "" {
		return "", fmt.Errorf("boot marker is required")
	}
	return bootToCondition(ctx, kernel, initrd, memoryMB, dmesg, fsdevs, nil, nil, netdev, func(serial string) bool {
		return strings.Contains(serial, marker)
	})
}

func bootToCondition(ctx context.Context, kernel []byte, initrd []byte, memoryMB uint64, dmesg bool, fsdevs []*virtio.FS, vsock *virtio.Vsock, rng *virtio.RNG, netdev *virtio.Net, done func(string) bool) (string, error) {
	vm, uart, serialOut, err := prepareArm64VM(kernel, initrd, memoryMB, dmesg, fsdevs, vsock, rng, netdev, nil)
	if err != nil {
		return "", err
	}
	defer vm.Close()
	defer closeFSDevices(fsdevs)
	if vsock != nil {
		defer vsock.Close()
	}
	for step := 0; ; step++ {
		if err := ctx.Err(); err != nil {
			if done(serialOut.String()) {
				return serialOut.String(), nil
			}
			return serialOut.String(), err
		}
		var exit Exit
		if err := vm.RunInterruptible(ctx, &exit); err != nil {
			return serialOut.String(), fmt.Errorf("run step %d: %w", step, err)
		}
		if done(serialOut.String()) {
			return serialOut.String(), nil
		}
		if err := handleArm64Exit(vm, uart, fsdevs, vsock, rng, netdev, exit); err != nil {
			return serialOut.String(), err
		}
	}
}

func prepareArm64VM(kernel []byte, initrd []byte, memoryMB uint64, dmesg bool, fsdevs []*virtio.FS, vsock *virtio.Vsock, rng *virtio.RNG, netdev *virtio.Net, serialWriter io.Writer) (*VM, *serial.UART8250, fmt.Stringer, error) {
	vm, err := NewVM(arm64vm.MemorySizeBytes(memoryMB), arm64vm.MemoryBase)
	if err != nil {
		return nil, nil, nil, err
	}
	mem := vm.Memory()
	var out bytes.Buffer
	if serialWriter == nil {
		serialWriter = &out
	}
	uart := serial.NewUART8250(arm64vm.DefaultUARTBase, arm64vm.DefaultUARTRegShift, serialWriter)
	uart.AttachIRQ(vm, arm64vm.UARTSPI)

	nodes := make([]fdt.Node, 0, len(fsdevs)+3)
	for _, fsdev := range fsdevs {
		if fsdev != nil {
			fsdev.Attach(vm, vm)
			nodes = append(nodes, fsdev.DeviceTreeNode())
		}
	}
	if vsock != nil {
		vsock.Attach(vm, vm)
		nodes = append(nodes, vsock.DeviceTreeNode())
	}
	if rng != nil {
		rng.Attach(vm, vm)
		nodes = append(nodes, rng.DeviceTreeNode())
	}
	if netdev != nil {
		netdev.Attach(vm, vm)
		nodes = append(nodes, netdev.DeviceTreeNode())
	}
	plan, err := arm64vm.PrepareBoot(mem, kernel, initrd, arm64vm.BootConfig{
		MemoryMB:   memoryMB,
		GICVersion: arm64vm.GICVersionV3,
		Dmesg:      dmesg,
		ExtraNodes: nodes,
	})
	if err != nil {
		_ = vm.Close()
		return nil, nil, nil, fmt.Errorf("prepare boot: %w", err)
	}
	if err := setupBootRegisters(vm, plan); err != nil {
		_ = vm.Close()
		return nil, nil, nil, err
	}
	if stringer, ok := serialWriter.(fmt.Stringer); ok {
		return vm, uart, stringer, nil
	}
	return vm, uart, &out, nil
}

func setupBootRegisters(vm *VM, plan *bootarm64.BootPlan) error {
	if err := vm.SetPC(plan.EntryGPA); err != nil {
		return fmt.Errorf("set PC: %w", err)
	}
	if err := vm.SetPState(arm64vm.DefaultPStateBits); err != nil {
		return fmt.Errorf("set PSTATE: %w", err)
	}
	if err := vm.SetSpEl1(plan.StackTopGPA); err != nil {
		return fmt.Errorf("set SP_EL1: %w", err)
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

func handleArm64Exit(vm *VM, uart *serial.UART8250, fsdevs []*virtio.FS, vsock *virtio.Vsock, rng *virtio.RNG, netdev *virtio.Net, exit Exit) error {
	switch exit.Reason {
	case runVPExitReasonUnmappedGPA, runVPExitReasonGPAIntercept:
		return handleBootMMIO(vm, uart, fsdevs, vsock, rng, netdev, exit.MMIO)
	case runVPExitReasonCanceled:
		return nil
	case runVPExitReasonArm64Reset:
		return fmt.Errorf("guest requested arm64 reset")
	default:
		pc, _ := vm.GetPC()
		return fmt.Errorf("unexpected exit %s at pc=%#x", exit.Reason, pc)
	}
}

func handleBootMMIO(vm *VM, uart *serial.UART8250, fsdevs []*virtio.FS, vsock *virtio.Vsock, rng *virtio.RNG, netdev *virtio.Net, mmio MMIOExit) error {
	if uart.Contains(mmio.Addr, int(mmio.Len)) {
		if mmio.Write {
			if err := uart.Write(mmio.Addr, mmio.Data[:mmio.Len]); err != nil {
				return err
			}
			return vm.CompleteMMIOWrite(mmio)
		}
		value, err := uart.ReadValue(mmio.Addr, int(mmio.Len))
		if err != nil {
			return err
		}
		return vm.CompleteMMIORead(mmio, value)
	}
	for _, fsdev := range fsdevs {
		if fsdev == nil || !fsdev.Contains(mmio.Addr, int(mmio.Len)) {
			continue
		}
		if mmio.Write {
			if err := fsdev.Write(mmio.Addr, int(mmio.Len), mmioValue(mmio)); err != nil {
				return err
			}
			return vm.CompleteMMIOWrite(mmio)
		}
		value, err := fsdev.Read(mmio.Addr, int(mmio.Len))
		if err != nil {
			return err
		}
		return vm.CompleteMMIORead(mmio, value)
	}
	if vsock != nil && vsock.Contains(mmio.Addr, int(mmio.Len)) {
		if mmio.Write {
			if err := vsock.Write(mmio.Addr, int(mmio.Len), mmioValue(mmio)); err != nil {
				return err
			}
			return vm.CompleteMMIOWrite(mmio)
		}
		value, err := vsock.Read(mmio.Addr, int(mmio.Len))
		if err != nil {
			return err
		}
		return vm.CompleteMMIORead(mmio, value)
	}
	if rng != nil && rng.Contains(mmio.Addr, int(mmio.Len)) {
		if mmio.Write {
			if err := rng.Write(mmio.Addr, int(mmio.Len), mmioValue(mmio)); err != nil {
				return err
			}
			return vm.CompleteMMIOWrite(mmio)
		}
		value, err := rng.Read(mmio.Addr, int(mmio.Len))
		if err != nil {
			return err
		}
		return vm.CompleteMMIORead(mmio, value)
	}
	if netdev != nil && netdev.Contains(mmio.Addr, int(mmio.Len)) {
		if mmio.Write {
			if err := netdev.Write(mmio.Addr, int(mmio.Len), mmioValue(mmio)); err != nil {
				return err
			}
			return vm.CompleteMMIOWrite(mmio)
		}
		value, err := netdev.Read(mmio.Addr, int(mmio.Len))
		if err != nil {
			return err
		}
		return vm.CompleteMMIORead(mmio, value)
	}
	if inRange(mmio.Addr, arm64vm.GICDistributorMin, arm64vm.GICDistributorMax) {
		if mmio.Write {
			return vm.CompleteMMIOWrite(mmio)
		}
		return vm.CompleteMMIORead(mmio, readBootGICDistributor(mmio.Addr-arm64vm.GICDistributorMin))
	}
	if inRange(mmio.Addr, arm64vm.GICRedistributorMin, arm64vm.GICRedistributorMax) {
		if mmio.Write {
			return vm.CompleteMMIOWrite(mmio)
		}
		return vm.CompleteMMIORead(mmio, readBootGICRedistributor(mmio.Addr-arm64vm.GICRedistributorMin))
	}
	return fmt.Errorf("unhandled mmio addr=%#x len=%d write=%v", mmio.Addr, mmio.Len, mmio.Write)
}

func runManagedExecVM(ctx context.Context, vm *VM, uart *serial.UART8250, fsdevs []*virtio.FS, vsock *virtio.Vsock, rng *virtio.RNG, netdev *virtio.Net, serialOut fmt.Stringer) error {
	for step := 0; ; step++ {
		var exit Exit
		if err := vm.RunInterruptible(ctx, &exit); err != nil {
			return fmt.Errorf("run step %d: %w", step, err)
		}
		if err := handleArm64Exit(vm, uart, fsdevs, vsock, rng, netdev, exit); err != nil {
			return err
		}
	}
}

func closeFSDevices(fsdevs []*virtio.FS) {
	for _, fsdev := range fsdevs {
		if fsdev != nil {
			_ = fsdev.Close()
		}
	}
}

func mmioValue(mmio MMIOExit) uint64 {
	switch mmio.Len {
	case 1:
		return uint64(mmio.Data[0])
	case 2:
		return uint64(mmio.Data[0]) | uint64(mmio.Data[1])<<8
	case 4:
		return uint64(mmio.Data[0]) | uint64(mmio.Data[1])<<8 | uint64(mmio.Data[2])<<16 | uint64(mmio.Data[3])<<24
	default:
		var value uint64
		for i := uint32(0); i < mmio.Len && i < 8; i++ {
			value |= uint64(mmio.Data[i]) << (8 * i)
		}
		return value
	}
}

func readBootGICDistributor(offset uint64) uint64 {
	switch offset {
	case 0xffe8:
		return 0x30
	default:
		return 0
	}
}

func readBootGICRedistributor(offset uint64) uint64 {
	switch offset {
	case 0x0:
		return 0
	case 0x8:
		return 1 << 4
	case 0x14:
		return 0
	case 0xffe8:
		return 0x30
	default:
		return 0
	}
}

func inRange(addr, start, end uint64) bool {
	return addr >= start && addr < end
}

func BootKernelToSerialWithTimeout(kernel []byte, memoryMB uint64, dmesg bool, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return BootKernelToSerial(ctx, kernel, memoryMB, dmesg)
}
