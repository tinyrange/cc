//go:build linux && arm64

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
	"j5.nz/cc/internal/arm64vm"
	"j5.nz/cc/internal/fdt"
	freebsdarm64 "j5.nz/cc/internal/freebsd/boot/arm64"
	"j5.nz/cc/internal/managed/machine"
	"j5.nz/cc/internal/netstack"
	"j5.nz/cc/internal/nvme"
	"j5.nz/cc/internal/serial"
	"j5.nz/cc/internal/virtio"
	"j5.nz/cc/internal/vmruntime"
)

type FreeBSDManagedConfig struct {
	Kernel    []byte
	Root      virtio.BlockBackend
	MemoryMB  uint64
	Dmesg     bool
	GuestIPv4 net.IP
	GuestMAC  net.HardwareAddr
	NetDevice *virtio.Net
	NetStack  *netstack.NetStack
}

func StartFreeBSDManagedSession(ctx context.Context, cfg FreeBSDManagedConfig, onEvent func(client.BootEvent) error) (*ManagedSession, error) {
	cfg, err := normalizeFreeBSDManagedConfig(cfg)
	if err != nil {
		return nil, err
	}

	netdev, stack, ownStack := freeBSDManagedNet(cfg)
	return startFreeBSDArm64ManagedSession(ctx, freeBSDArm64SessionConfig{
		Spec: machine.Spec{
			Guest:    "FreeBSD",
			Arch:     "arm64",
			MemoryMB: cfg.MemoryMB,
			Dmesg:    cfg.Dmesg,
			Control:  machine.ControlSpec{Kind: "tcp", Port: bsdControlPort},
			Network:  &machine.NetworkSpec{GuestIPv4: cfg.GuestIPv4.String(), MAC: cfg.GuestMAC.String()},
		},
		Kernel:      cfg.Kernel,
		Root:        cfg.Root,
		MemoryMB:    cfg.MemoryMB,
		Dmesg:       cfg.Dmesg,
		NetDevice:   netdev,
		NetStack:    stack,
		OwnNetStack: ownStack,
	}, onEvent)
}

type freeBSDArm64SessionConfig struct {
	Spec        machine.Spec
	Kernel      []byte
	Root        virtio.BlockBackend
	MemoryMB    uint64
	Dmesg       bool
	NetDevice   *virtio.Net
	NetStack    *netstack.NetStack
	OwnNetStack bool
}

func startFreeBSDArm64ManagedSession(ctx context.Context, cfg freeBSDArm64SessionConfig, onEvent func(client.BootEvent) error) (*ManagedSession, error) {
	cfg = normalizeFreeBSDArm64SessionConfig(cfg)
	if cfg.NetDevice == nil || cfg.NetStack == nil {
		return nil, fmt.Errorf("FreeBSD network device and stack are required")
	}
	if err := emitManagedBootStatus(onEvent, "starting VM"); err != nil {
		return nil, err
	}
	ln, err := cfg.NetStack.ListenInternal("tcp", fmt.Sprintf(":%d", bsdControlPort))
	if err != nil {
		if cfg.OwnNetStack {
			cfg.NetStack.Close()
		}
		return nil, fmt.Errorf("listen FreeBSD control tcp: %w", err)
	}

	var kvmVM *VM
	var cancel context.CancelFunc
	var bootWriter *vmruntime.BootEventWriter
	cleanupStartup := func() {
		if cancel != nil {
			cancel()
		}
		_ = ln.Close()
		if cfg.OwnNetStack {
			cfg.NetStack.Close()
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
	mem, err := kvmVM.MapAnonymousMemory(arm64vm.MemorySizeBytes(cfg.MemoryMB), arm64vm.MemoryBase)
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
	uart := serial.NewUART8250(arm64vm.DefaultUARTBase, arm64vm.DefaultUARTRegShift, serialWriter)
	uart.AttachIRQ(kvmVM, arm64vm.UARTSPI)
	nvmeBlock := nvme.NewController(cfg.Root)
	nvmeBlock.Attach(kvmVM, kvmVM)
	pci := NewArm64PCIHost(NewArm64NVMePCIDevice(1, arm64vm.NVMeBase, arm64vm.NVMeIRQ, nvmeBlock))
	cfg.NetDevice.DisableMergeRX = true
	cfg.NetDevice.HeaderLength = 12
	cfg.NetDevice.Attach(kvmVM, kvmVM)
	rng := virtio.NewRNG(arm64vm.RNGBase, arm64vm.RNGSize, arm64vm.RNGIRQ)
	rng.Attach(kvmVM, kvmVM)

	plan, err := freebsdarm64.PrepareBoot(mem, cfg.Kernel, freebsdarm64.BootOptions{
		MemoryBase: arm64vm.MemoryBase,
		MemorySize: arm64vm.MemorySizeBytes(cfg.MemoryMB),
		NumCPUs:    1,
		Console:    true,
		ExtraNodes: []fdt.Node{
			pci.DeviceTreeNode(),
			cfg.NetDevice.DeviceTreeNode(),
			rng.DeviceTreeNode(),
		},
	})
	if err != nil {
		cleanupStartup()
		return nil, fmt.Errorf("prepare FreeBSD boot: %w", err)
	}
	if err := setupFreeBSDArm64Registers(kvmVM, plan); err != nil {
		cleanupStartup()
		return nil, err
	}

	runCtx, runCancel := context.WithCancel(context.Background())
	cancel = runCancel
	done := newSessionDone()
	vmForRun := kvmVM
	kvmVM = nil
	go func() {
		defer vmForRun.Close()
		done.finish(runFreeBSDArm64ManagedVM(runCtx, vmForRun, uart, pci, cfg.NetDevice, rng, serialOut))
	}()

	var control net.Conn
	select {
	case err := <-acceptErrCh:
		cleanupStartup()
		return nil, freeBSDStartupError(err, serialOut.String(), controlTranscript.String())
	case conn := <-connCh:
		control = conn
	case <-done.done():
		err := done.result()
		cleanupStartup()
		return nil, freeBSDStartupError(err, serialOut.String(), controlTranscript.String())
	case <-ctx.Done():
		err := fmt.Errorf("FreeBSD guest did not connect to control TCP port %d before startup deadline: %w", bsdControlPort, ctx.Err())
		if cancel != nil {
			cancel()
			select {
			case <-done.done():
				if runErr := done.result(); runErr != nil {
					err = fmt.Errorf("%w; VM run result: %v", err, runErr)
				}
			case <-time.After(time.Second):
			}
		}
		cleanupStartup()
		return nil, freeBSDStartupError(err, serialOut.String(), controlTranscript.String())
	}

	if _, err := controlTranscript.WaitFor(ctx, 0, func(text string) bool {
		return strings.Contains(text, vmruntime.InstanceReadyMarker) || vmruntime.HasFatalBootText(text)
	}); err != nil {
		_ = control.Close()
		cleanupStartup()
		err = fmt.Errorf("FreeBSD control connection did not report ready marker %q: %w", vmruntime.InstanceReadyMarker, err)
		return nil, freeBSDStartupError(err, serialOut.String(), controlTranscript.String())
	}
	if vmruntime.HasFatalBootText(controlTranscript.String()) {
		_ = control.Close()
		cleanupStartup()
		return nil, freeBSDStartupError(fmt.Errorf("FreeBSD guest reported boot failure"), serialOut.String(), controlTranscript.String())
	}
	if err := emitManagedBootStatus(onEvent, "guest ready"); err != nil {
		_ = control.Close()
		cleanupStartup()
		return nil, err
	}

	return &ManagedSession{
		cancel:     cancel,
		done:       done,
		control:    control,
		listener:   ln,
		bootWriter: bootWriter,
		transcript: controlTranscript,
		serialOut:  serialOut,
		cleanup: func() {
			if cfg.OwnNetStack {
				_ = cfg.NetStack.Close()
			}
		},
		dmesg: cfg.Dmesg,
	}, nil
}

func normalizeFreeBSDArm64SessionConfig(cfg freeBSDArm64SessionConfig) freeBSDArm64SessionConfig {
	if cfg.MemoryMB == 0 {
		cfg.MemoryMB = cfg.Spec.MemoryMB
	}
	if cfg.MemoryMB == 0 {
		cfg.MemoryMB = 1024
	}
	if !cfg.Dmesg {
		cfg.Dmesg = cfg.Spec.Dmesg
	}
	return cfg
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
	return transcriptError(err, serialText, controlText)
}

func errorsIsEINTR(err error) bool {
	return errors.Is(err, unix.EINTR)
}

func freeBSDManagedNet(cfg FreeBSDManagedConfig) (*virtio.Net, *netstack.NetStack, bool) {
	if cfg.NetDevice != nil && cfg.NetStack != nil {
		return cfg.NetDevice, cfg.NetStack, false
	}
	netdev, stack := newFreeBSDManagedNet(cfg.GuestIPv4, cfg.GuestMAC)
	return netdev, stack, true
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
	dev := virtio.NewNet(arm64vm.NetBase, arm64vm.NetSize, arm64vm.NetIRQ, mac, freeBSDManagedNetBackend{iface: iface})
	dev.DisableMergeRX = true
	dev.HeaderLength = 12
	iface.AttachVirtioBackend(func(frame []byte) error {
		copied := append([]byte(nil), frame...)
		go func() {
			_ = dev.EnqueueRxPacketOwned(copied)
		}()
		return nil
	})
	return dev, stack
}

func runFreeBSDArm64ManagedVM(ctx context.Context, vm *VM, uart *serial.UART8250, pci *Arm64PCIHost, netdev *virtio.Net, rng *virtio.RNG, serialOut *vmruntime.SerialTranscript) error {
	runtime.LockOSThread()
	// The caller owns a dedicated goroutine; terminate its vCPU thread when it exits.
	vm.SetVCPUTID(unix.Gettid())
	defer vm.SetVCPUTID(0)
	cancelDone := make(chan struct{})
	defer close(cancelDone)
	go func() {
		select {
		case <-ctx.Done():
			vm.RequestImmediateExit()
		case <-cancelDone:
		}
	}()

	var answeredMountroot atomic.Bool
	go func() {
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				serial := serialOut.String()
				if !answeredMountroot.Load() && strings.Contains(serial, "mountroot>") {
					answeredMountroot.Store(true)
					_ = uart.InjectRXBytes([]byte("ufs:/dev/nda0\n"))
				}
			}
		}
	}()

	var exit Exit
	for step := 0; ; step++ {
		if err := ctx.Err(); err != nil {
			pc, _ := vm.GetPC()
			return fmt.Errorf("%w at pc=%#x", err, pc)
		}
		if err := vm.RunInterruptible(&exit); err != nil {
			if errorsIsEINTR(err) {
				continue
			}
			return fmt.Errorf("run step %d: %w", step, err)
		}
		switch exit.Reason {
		case ExitMMIO:
			if err := handleFreeBSDArm64MMIO(vm, uart, pci, netdev, rng, exit.MMIO); err != nil {
				return err
			}
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

func handleFreeBSDArm64MMIO(vm *VM, uart *serial.UART8250, pci *Arm64PCIHost, netdev *virtio.Net, rng *virtio.RNG, mmio MMIOExit) error {
	if uart.Contains(mmio.Addr, int(mmio.Len)) {
		return handleUARTExit(vm, uart, mmio)
	}
	if handled, err := pci.HandleMMIO(vm, mmio); handled || err != nil {
		return err
	}
	if netdev != nil && netdev.Contains(mmio.Addr, int(mmio.Len)) {
		return handleMMIODevice(vm, netdev, mmio)
	}
	if rng != nil && rng.Contains(mmio.Addr, int(mmio.Len)) {
		return handleMMIODevice(vm, rng, mmio)
	}
	return fmt.Errorf("unhandled FreeBSD mmio addr=%#x len=%d write=%v", mmio.Addr, mmio.Len, mmio.Write)
}
