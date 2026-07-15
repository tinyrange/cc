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
	"j5.nz/cc/internal/managed/machine"
	"j5.nz/cc/internal/netbsd/boot/amd64"
	"j5.nz/cc/internal/netstack"
	"j5.nz/cc/internal/serial"
	"j5.nz/cc/internal/virtio"
	"j5.nz/cc/internal/vmruntime"
)

type NetBSDManagedConfig struct {
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

func StartNetBSDManagedSession(ctx context.Context, cfg NetBSDManagedConfig, onEvent func(client.BootEvent) error) (*ManagedSession, error) {
	cfg, err := normalizeNetBSDManagedConfig(cfg)
	if err != nil {
		return nil, err
	}

	netdev, stack, ownStack := netBSDManagedNet(cfg)
	devices := []machine.DeviceSpec{{Kind: "nvme", Name: "root", Bus: "pci", Slot: 1, IRQ: 10}}
	for idx := range cfg.ExtraBlocks {
		devices = append(devices, machine.DeviceSpec{
			Kind: "nvme",
			Name: fmt.Sprintf("extra%d", idx),
			Bus:  "pci",
			Slot: uint8(2 + idx),
			IRQ:  uint8(11 + idx),
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
			Guest:    "NetBSD",
			Arch:     "amd64",
			MemoryMB: cfg.MemoryMB,
			Dmesg:    cfg.Dmesg,
			Control:  machine.ControlSpec{Kind: "tcp", Port: bsdControlPort},
			Network:  &machine.NetworkSpec{GuestIPv4: cfg.GuestIPv4.String(), MAC: cfg.GuestMAC.String()},
			Devices:  devices,
		},
		GuestName:   "NetBSD",
		Kernel:      cfg.Kernel,
		Root:        cfg.Root,
		ExtraBlocks: cfg.ExtraBlocks,
		MemoryMB:    cfg.MemoryMB,
		Dmesg:       cfg.Dmesg,
		NetDevice:   netdev,
		NetStack:    stack,
		OwnNetStack: ownStack,
		Prepare: func(vm *VM, mem []byte) error {
			plan, err := amd64.PrepareBoot(mem, cfg.Kernel, amd64.BootOptions{
				MemorySize: amd64vm.MemorySizeBytes(cfg.MemoryMB),
			})
			if err != nil {
				return fmt.Errorf("prepare NetBSD boot: %w", err)
			}
			if err := vm.SetProtectedMode32(plan.EntryGPA, plan.StackGPA); err != nil {
				return fmt.Errorf("set NetBSD protected mode: %w", err)
			}
			return nil
		},
		Run: runNetBSDManagedVM,
	}, onEvent)
}

func netBSDManagedNet(cfg NetBSDManagedConfig) (*virtio.Net, *netstack.NetStack, bool) {
	if cfg.NetDevice != nil && cfg.NetStack != nil {
		return cfg.NetDevice, cfg.NetStack, false
	}
	netdev, stack := newNetBSDManagedNet(cfg.GuestIPv4, cfg.GuestMAC)
	return netdev, stack, true
}

func normalizeNetBSDManagedConfig(cfg NetBSDManagedConfig) (NetBSDManagedConfig, error) {
	if len(cfg.Kernel) == 0 {
		return NetBSDManagedConfig{}, fmt.Errorf("NetBSD kernel is required")
	}
	if cfg.Root == nil {
		return NetBSDManagedConfig{}, fmt.Errorf("NetBSD root filesystem is required")
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

func runNetBSDManagedVM(ctx context.Context, vm *VM, uart *serial.UART8250, pci *PCIBus, serialOut *vmruntime.SerialTranscript) error {
	runtime.LockOSThread()
	// The caller owns a dedicated goroutine; terminate its vCPU thread when it exits.
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
				if handled, err := acpiPM.HandleIO(ioExit); handled || err != nil {
					return err
				}
				if handled, err := rtc.HandleIO(ioExit); handled || err != nil {
					return err
				}
				return handleBootIO(uart, ioExit)
			}, pci, exit.IO); err != nil {
				return err
			}
		case ExitMMIO:
			if err := handleBootMMIOWithPCI(vm, 0, pci, nil, nil, nil, nil, nil, exit.MMIO); err != nil {
				return fmt.Errorf("unhandled NetBSD mmio addr=%#x len=%d write=%v: %w", exit.MMIO.Addr, exit.MMIO.Len, exit.MMIO.Write, err)
			}
		case ExitHLT:
			pc, _ := vm.GetPC()
			return fmt.Errorf("NetBSD guest halted at pc=%#x\nserial:\n%s", pc, serialOut.String())
		case ExitShutdown:
			pc, _ := vm.GetPC()
			return fmt.Errorf("NetBSD guest shut down at pc=%#x\nserial:\n%s", pc, serialOut.String())
		case ExitSystemEvent:
			return fmt.Errorf("unexpected system event %d\nserial:\n%s", exit.SystemEvent, serialOut.String())
		default:
			pc, _ := vm.GetPC()
			return fmt.Errorf("unexpected exit reason %d at pc=%#x\nserial:\n%s", exit.Reason, pc, serialOut.String())
		}
	}
}

type netBSDManagedNetBackend struct {
	iface *netstack.NetworkInterface
}

func (b netBSDManagedNetBackend) HandleTxPacket(packet []byte) error {
	return b.iface.DeliverGuestPacket(packet, true)
}

func newNetBSDManagedNet(guestIPv4 net.IP, mac net.HardwareAddr) (*virtio.Net, *netstack.NetStack) {
	stack := netstack.New(slog.Default())
	_ = stack.SetGuestMAC(mac)
	_ = stack.SetGuestIPv4(guestIPv4)
	if hostMAC, err := net.ParseMAC("02:42:0a:2a:00:01"); err == nil {
		_ = stack.SetHostMAC(hostMAC)
	}
	iface, _ := stack.AttachNetworkInterface()
	dev := virtio.NewNet(0, 0x1000, 11, mac, netBSDManagedNetBackend{iface: iface})
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
