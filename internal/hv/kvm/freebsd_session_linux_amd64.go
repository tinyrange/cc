//go:build linux && amd64

package kvm

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"runtime"

	"golang.org/x/sys/unix"
	"j5.nz/cc/client"
	"j5.nz/cc/internal/amd64vm"
	freebsdamd64 "j5.nz/cc/internal/freebsd/boot/amd64"
	"j5.nz/cc/internal/managed/machine"
	"j5.nz/cc/internal/netstack"
	"j5.nz/cc/internal/serial"
	"j5.nz/cc/internal/virtio"
	"j5.nz/cc/internal/vmruntime"
)

type FreeBSDManagedConfig struct {
	Kernel      []byte
	Root        virtio.BlockBackend
	ExtraBlocks []virtio.BlockBackend
	MemoryMB    uint64
	Dmesg       bool
	GuestIPv4   net.IP
	GuestMAC    net.HardwareAddr
	NetDevice   *virtio.Net
	NetStack    *netstack.NetStack
}

func StartFreeBSDManagedSession(ctx context.Context, cfg FreeBSDManagedConfig, onEvent func(client.BootEvent) error) (*ManagedSession, error) {
	cfg, err := normalizeFreeBSDManagedConfig(cfg)
	if err != nil {
		return nil, err
	}

	netdev, stack, ownStack := freeBSDManagedNet(cfg)
	devices := []machine.DeviceSpec{{Kind: "virtio-block", Name: "root", Bus: "pci", Slot: 1, IOBase: 0x1000, IRQ: 10}}
	for idx := range cfg.ExtraBlocks {
		devices = append(devices, machine.DeviceSpec{
			Kind:   "virtio-block",
			Name:   fmt.Sprintf("extra%d", idx),
			Bus:    "pci",
			Slot:   uint8(2 + idx),
			IOBase: uint16(0x1100 + idx*0x100),
			IRQ:    uint8(11 + idx),
		})
	}
	netIndex := len(devices) + 1
	devices = append(devices, machine.DeviceSpec{
		Kind:   "virtio-net",
		Name:   "net0",
		Bus:    "pci",
		Slot:   uint8(netIndex),
		IOBase: uint16(0x1000 + netIndex*0x100),
		IRQ:    uint8(10 + netIndex),
	})
	return startBSDPCManagedSession(ctx, bsdPCSessionConfig{
		Spec: machine.Spec{
			Guest:    "FreeBSD",
			Arch:     "amd64",
			MemoryMB: cfg.MemoryMB,
			Dmesg:    cfg.Dmesg,
			Control:  machine.ControlSpec{Kind: "tcp", Port: bsdControlPort},
			Network:  &machine.NetworkSpec{GuestIPv4: cfg.GuestIPv4.String(), MAC: cfg.GuestMAC.String()},
			Devices:  devices,
		},
		GuestName:   "FreeBSD",
		Kernel:      cfg.Kernel,
		Root:        cfg.Root,
		ExtraBlocks: cfg.ExtraBlocks,
		MemoryMB:    cfg.MemoryMB,
		Dmesg:       cfg.Dmesg,
		NetDevice:   netdev,
		NetStack:    stack,
		OwnNetStack: ownStack,
		Prepare: func(vm *VM, mem []byte) error {
			plan, err := freebsdamd64.PrepareBoot(mem, cfg.Kernel, freebsdamd64.BootOptions{
				MemorySize: amd64vm.MemorySizeBytes(cfg.MemoryMB),
			})
			if err != nil {
				return fmt.Errorf("prepare FreeBSD boot: %w", err)
			}
			if err := vm.SetFreeBSDLongMode(plan.EntryGVA, plan.StackGPA, plan.PagingGPA); err != nil {
				return fmt.Errorf("set FreeBSD long mode: %w", err)
			}
			return nil
		},
		Run: runFreeBSDManagedVM,
	}, onEvent)
}

func freeBSDManagedNet(cfg FreeBSDManagedConfig) (*virtio.Net, *netstack.NetStack, bool) {
	if cfg.NetDevice != nil && cfg.NetStack != nil {
		return cfg.NetDevice, cfg.NetStack, false
	}
	netdev, stack := newFreeBSDManagedNet(cfg.GuestIPv4, cfg.GuestMAC)
	return netdev, stack, true
}

func normalizeFreeBSDManagedConfig(cfg FreeBSDManagedConfig) (FreeBSDManagedConfig, error) {
	if len(cfg.Kernel) == 0 {
		return FreeBSDManagedConfig{}, fmt.Errorf("FreeBSD kernel is required")
	}
	if cfg.Root == nil {
		return FreeBSDManagedConfig{}, fmt.Errorf("FreeBSD root filesystem is required")
	}
	if cfg.MemoryMB == 0 {
		cfg.MemoryMB = 1024
	}
	if cfg.GuestIPv4 == nil {
		cfg.GuestIPv4 = net.IPv4(10, 42, 0, 2)
	}
	if cfg.GuestMAC == nil {
		cfg.GuestMAC = net.HardwareAddr{0x02, 0x42, 0x0a, 0x2a, 0x00, 0x02}
	}
	return cfg, nil
}

func freeBSDStartupError(err error, serialText, controlText string) error {
	return bsdStartupError(err, serialText, controlText)
}

func runFreeBSDManagedVM(ctx context.Context, vm *VM, uart *serial.UART8250, pci *PCIBus, serialOut *vmruntime.SerialTranscript) error {
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
			return err
		}
		if err := vm.RunVCPUInterruptible(0, &exit); err != nil {
			if errorsIsEINTR(err) {
				continue
			}
			return fmt.Errorf("run step %d: %w", step, err)
		}
		switch exit.Reason {
		case ExitIO:
			if err := handleBootIOWithPCI(func(ioExit IOExit) error {
				return handleBootIO(uart, ioExit)
			}, pci, exit.IO); err != nil {
				return err
			}
		case ExitMMIO:
			return fmt.Errorf("unhandled FreeBSD mmio addr=%#x len=%d write=%v", exit.MMIO.Addr, exit.MMIO.Len, exit.MMIO.Write)
		case ExitHLT:
			return fmt.Errorf("FreeBSD guest halted\nserial:\n%s", serialOut.String())
		case ExitShutdown:
			return fmt.Errorf("FreeBSD guest shut down\nserial:\n%s", serialOut.String())
		case ExitSystemEvent:
			return fmt.Errorf("unexpected system event %d\nserial:\n%s", exit.SystemEvent, serialOut.String())
		default:
			pc, _ := vm.GetPC()
			return fmt.Errorf("unexpected exit reason %d at pc=%#x\nserial:\n%s", exit.Reason, pc, serialOut.String())
		}
	}
}

type freeBSDManagedNetBackend struct {
	iface *netstack.NetworkInterface
}

func (b freeBSDManagedNetBackend) HandleTxPacket(packet []byte) error {
	return b.iface.DeliverGuestPacket(packet, true)
}

func newFreeBSDManagedNet(guestIPv4 net.IP, mac net.HardwareAddr) (*virtio.Net, *netstack.NetStack) {
	stack := netstack.New(slog.Default())
	_ = stack.SetGuestMAC(mac)
	_ = stack.SetGuestIPv4(guestIPv4)
	iface, _ := stack.AttachNetworkInterface()
	dev := virtio.NewNet(0, 0x1000, 11, mac, freeBSDManagedNetBackend{iface: iface})
	dev.DisableMergeRX = true
	iface.AttachVirtioBackend(func(frame []byte) error {
		copied := append([]byte(nil), frame...)
		go func() {
			_ = dev.EnqueueRxPacketOwned(copied)
		}()
		return nil
	})
	return dev, stack
}
