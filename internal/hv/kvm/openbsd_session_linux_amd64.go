//go:build linux && amd64

package kvm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/sys/unix"
	"j5.nz/cc/client"
	"j5.nz/cc/internal/amd64vm"
	"j5.nz/cc/internal/managed/machine"
	"j5.nz/cc/internal/netstack"
	openbsdamd64 "j5.nz/cc/internal/openbsd/boot/amd64"
	"j5.nz/cc/internal/serial"
	"j5.nz/cc/internal/virtio"
	"j5.nz/cc/internal/vmruntime"
)

type OpenBSDManagedConfig struct {
	Kernel    []byte
	Root      virtio.BlockBackend
	MemoryMB  uint64
	Dmesg     bool
	GuestIPv4 net.IP
	GuestMAC  net.HardwareAddr
	NetDevice *virtio.Net
	NetStack  *netstack.NetStack
}

func StartOpenBSDManagedSession(ctx context.Context, cfg OpenBSDManagedConfig, onEvent func(client.BootEvent) error) (*ManagedSession, error) {
	cfg, err := normalizeOpenBSDManagedConfig(cfg)
	if err != nil {
		return nil, err
	}

	netdev, stack, ownStack := openBSDManagedNet(cfg)
	return startBSDPCManagedSession(ctx, bsdPCSessionConfig{
		Spec: machine.Spec{
			Guest:    "OpenBSD",
			Arch:     "amd64",
			MemoryMB: cfg.MemoryMB,
			Dmesg:    cfg.Dmesg,
			Control:  machine.ControlSpec{Kind: "tcp", Port: bsdControlPort},
			Network:  &machine.NetworkSpec{GuestIPv4: cfg.GuestIPv4.String(), MAC: cfg.GuestMAC.String()},
			Devices: []machine.DeviceSpec{
				{Kind: "nvme", Name: "root", Bus: "pci", Slot: 1, IRQ: 10},
				{Kind: "virtio-net", Name: "net0", Bus: "pci", Slot: 2, IOBase: 0x1100, IRQ: 11},
			},
		},
		GuestName:   "OpenBSD",
		Kernel:      cfg.Kernel,
		Root:        cfg.Root,
		MemoryMB:    cfg.MemoryMB,
		Dmesg:       cfg.Dmesg,
		NetDevice:   netdev,
		NetStack:    stack,
		OwnNetStack: ownStack,
		Prepare: func(vm *VM, mem []byte) error {
			plan, err := openbsdamd64.PrepareBoot(mem, cfg.Kernel, openbsdamd64.BootOptions{
				MemorySize: amd64vm.MemorySizeBytes(cfg.MemoryMB),
				BootDev:    openbsdamd64.SCSIBootDev(0, 0),
			})
			if err != nil {
				return fmt.Errorf("prepare OpenBSD boot: %w", err)
			}
			if err := vm.SetProtectedMode32(plan.EntryGPA, plan.StackGPA); err != nil {
				return fmt.Errorf("set protected mode: %w", err)
			}
			return nil
		},
		Run: runOpenBSDManagedVM,
	}, onEvent)
}

func openBSDManagedNet(cfg OpenBSDManagedConfig) (*virtio.Net, *netstack.NetStack, bool) {
	if cfg.NetDevice != nil && cfg.NetStack != nil {
		return cfg.NetDevice, cfg.NetStack, false
	}
	netdev, stack := newOpenBSDManagedNet(cfg.GuestIPv4, cfg.GuestMAC)
	return netdev, stack, true
}

func normalizeOpenBSDManagedConfig(cfg OpenBSDManagedConfig) (OpenBSDManagedConfig, error) {
	if len(cfg.Kernel) == 0 {
		return OpenBSDManagedConfig{}, fmt.Errorf("OpenBSD kernel is required")
	}
	if cfg.Root == nil {
		return OpenBSDManagedConfig{}, fmt.Errorf("OpenBSD root filesystem is required")
	}
	if cfg.MemoryMB == 0 {
		cfg.MemoryMB = 768
	}
	if cfg.GuestIPv4 == nil {
		cfg.GuestIPv4 = net.IPv4(10, 42, 0, 2)
	}
	if cfg.GuestMAC == nil {
		cfg.GuestMAC = net.HardwareAddr{0x02, 0x42, 0x0a, 0x2a, 0x00, 0x02}
	}
	return cfg, nil
}

func openBSDStartupError(err error, serialText, controlText string) error {
	return bsdStartupError(err, serialText, controlText)
}

func runOpenBSDManagedVM(ctx context.Context, vm *VM, uart *serial.UART8250, pci *PCIBus, serialOut *vmruntime.SerialTranscript) error {
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

	var answeredRoot atomic.Bool
	var answeredSwap atomic.Bool
	go func() {
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				serial := serialOut.String()
				if !answeredRoot.Load() && strings.Contains(serial, "root device:") {
					answeredRoot.Store(true)
					_ = uart.InjectRXBytes([]byte("sd0a\n"))
				}
				if answeredRoot.Load() && !answeredSwap.Load() && strings.Contains(serial, "swap device") {
					answeredSwap.Store(true)
					_ = uart.InjectRXBytes([]byte("\n"))
				}
			}
		}
	}()
	kbc := NewI8042()
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
				if handled, err := kbc.HandleIO(ioExit); handled || err != nil {
					return err
				}
				return handleBootIO(uart, ioExit)
			}, pci, exit.IO); err != nil {
				return err
			}
		case ExitMMIO:
			if err := handleBootMMIOWithPCI(vm, 0, pci, nil, nil, nil, nil, exit.MMIO); err != nil {
				return fmt.Errorf("unhandled OpenBSD mmio addr=%#x len=%d write=%v: %w", exit.MMIO.Addr, exit.MMIO.Len, exit.MMIO.Write, err)
			}
		case ExitHLT:
			return fmt.Errorf("OpenBSD guest halted\nserial:\n%s", serialOut.String())
		case ExitShutdown:
			return fmt.Errorf("OpenBSD guest shut down\nserial:\n%s", serialOut.String())
		case ExitSystemEvent:
			return fmt.Errorf("unexpected system event %d\nserial:\n%s", exit.SystemEvent, serialOut.String())
		default:
			pc, _ := vm.GetPC()
			return fmt.Errorf("unexpected exit reason %d at pc=%#x\nserial:\n%s", exit.Reason, pc, serialOut.String())
		}
	}
}

func errorsIsEINTR(err error) bool {
	return errors.Is(err, unix.EINTR)
}

type openBSDManagedNetBackend struct {
	iface *netstack.NetworkInterface
}

func (b openBSDManagedNetBackend) HandleTxPacket(packet []byte) error {
	return b.iface.DeliverGuestPacket(packet, true)
}

func newOpenBSDManagedNet(guestIPv4 net.IP, mac net.HardwareAddr) (*virtio.Net, *netstack.NetStack) {
	stack := netstack.New(slog.Default())
	_ = stack.SetGuestMAC(mac)
	_ = stack.SetGuestIPv4(guestIPv4)
	if hostMAC, err := net.ParseMAC("02:42:0a:2a:00:01"); err == nil {
		_ = stack.SetHostMAC(hostMAC)
	}
	iface, _ := stack.AttachNetworkInterface()
	dev := virtio.NewNet(0, 0x1000, 11, mac, openBSDManagedNetBackend{iface: iface})
	iface.AttachVirtioBackend(func(frame []byte) error {
		copied := append([]byte(nil), frame...)
		go func() {
			_ = dev.EnqueueRxPacketOwned(copied)
		}()
		return nil
	})
	return dev, stack
}
