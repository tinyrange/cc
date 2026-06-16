//go:build linux && amd64

package kvm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/sys/unix"
	"j5.nz/cc/client"
	"j5.nz/cc/internal/amd64vm"
	"j5.nz/cc/internal/netstack"
	openbsdamd64 "j5.nz/cc/internal/openbsd/boot/amd64"
	"j5.nz/cc/internal/serial"
	"j5.nz/cc/internal/virtio"
	"j5.nz/cc/internal/vmruntime"
)

const openBSDControlPort = 10777

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
	ln, err := stack.ListenInternal("tcp", fmt.Sprintf(":%d", openBSDControlPort))
	if err != nil {
		if ownStack {
			stack.Close()
		}
		return nil, fmt.Errorf("listen OpenBSD control tcp: %w", err)
	}

	var kvmVM *VM
	var cancel context.CancelFunc
	var bootWriter *vmruntime.BootEventWriter
	cleanupStartup := func() {
		if cancel != nil {
			cancel()
		}
		_ = ln.Close()
		if ownStack {
			stack.Close()
		}
		if bootWriter != nil {
			_ = bootWriter.Close()
		}
		if kvmVM != nil {
			kvmVM.Close()
		}
	}

	connCh := make(chan net.Conn, 1)
	acceptErrCh := make(chan error, 1)
	controlTranscript := vmruntime.NewSerialTranscript()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			acceptErrCh <- err
			return
		}
		connCh <- conn
		_, _ = io.Copy(controlTranscript, conn)
	}()

	kvmVM, err = NewVM()
	if err != nil {
		cleanupStartup()
		return nil, err
	}
	mem, err := mapAMD64GuestMemory(kvmVM, cfg.MemoryMB)
	if err != nil {
		cleanupStartup()
		return nil, fmt.Errorf("map guest memory: %w", err)
	}

	serialOut := vmruntime.NewSerialTranscript()
	var serialWriter io.Writer = serialOut
	if onEvent != nil {
		bootWriter = vmruntime.NewBootEventWriter(onEvent)
		serialWriter = io.MultiWriter(serialOut, bootWriter)
	}
	uart := serial.NewUART8250(amd64vm.COM1Base, 0, serialWriter)
	uart.AttachIRQ(kvmVM, amd64vm.COM1IRQ)

	block := virtio.NewBlock(0, 0x1000, 10, cfg.Root)
	block.Attach(kvmVM, kvmVM)
	netdev.Attach(kvmVM, kvmVM)
	pci := NewPCIBus(
		NewVirtioBlockPCIDevice(1, 0x1000, 10, block),
		NewVirtioNetPCIDevice(2, 0x1100, 11, netdev),
	)

	plan, err := openbsdamd64.PrepareBoot(mem, cfg.Kernel, openbsdamd64.BootOptions{
		MemorySize: amd64vm.MemorySizeBytes(cfg.MemoryMB),
	})
	if err != nil {
		cleanupStartup()
		return nil, fmt.Errorf("prepare OpenBSD boot: %w", err)
	}
	if err := kvmVM.SetProtectedMode32(plan.EntryGPA, plan.StackGPA); err != nil {
		cleanupStartup()
		return nil, fmt.Errorf("set protected mode: %w", err)
	}

	runCtx, runCancel := context.WithCancel(context.Background())
	cancel = runCancel
	doneCh := make(chan error, 1)
	vmForRun := kvmVM
	kvmVM = nil
	go func() {
		defer vmForRun.Close()
		doneCh <- runOpenBSDManagedVM(runCtx, vmForRun, uart, pci, serialOut)
	}()

	var control net.Conn
	select {
	case err := <-acceptErrCh:
		cleanupStartup()
		return nil, openBSDStartupError(err, serialOut.String(), controlTranscript.String())
	case conn := <-connCh:
		control = conn
	case err := <-doneCh:
		cleanupStartup()
		return nil, openBSDStartupError(err, serialOut.String(), controlTranscript.String())
	case <-ctx.Done():
		cleanupStartup()
		err := fmt.Errorf("OpenBSD guest did not connect to control TCP port %d before startup deadline: %w", openBSDControlPort, ctx.Err())
		return nil, openBSDStartupError(err, serialOut.String(), controlTranscript.String())
	}

	if _, err := controlTranscript.WaitFor(ctx, 0, func(text string) bool {
		return strings.Contains(text, vmruntime.InstanceReadyMarker) || vmruntime.HasFatalBootText(text)
	}); err != nil {
		_ = control.Close()
		cleanupStartup()
		err = fmt.Errorf("OpenBSD control connection did not report ready marker %q: %w", vmruntime.InstanceReadyMarker, err)
		return nil, openBSDStartupError(err, serialOut.String(), controlTranscript.String())
	}
	if vmruntime.HasFatalBootText(controlTranscript.String()) {
		_ = control.Close()
		cleanupStartup()
		return nil, openBSDStartupError(fmt.Errorf("OpenBSD guest reported boot failure"), serialOut.String(), controlTranscript.String())
	}

	return &ManagedSession{
		cancel:     cancel,
		doneCh:     doneCh,
		control:    control,
		listener:   ln,
		bootWriter: bootWriter,
		transcript: controlTranscript,
		serialOut:  serialOut,
		cleanup: func() {
			if ownStack {
				_ = stack.Close()
			}
		},
		dmesg: cfg.Dmesg,
	}, nil
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
	return transcriptError(err, serialText, controlText)
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
			return fmt.Errorf("unhandled OpenBSD mmio addr=%#x len=%d write=%v", exit.MMIO.Addr, exit.MMIO.Len, exit.MMIO.Write)
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
