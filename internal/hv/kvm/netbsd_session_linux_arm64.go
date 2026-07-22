//go:build linux && arm64

package kvm

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"runtime"
	"strings"
	"time"

	"golang.org/x/sys/unix"
	"j5.nz/cc/client"
	"j5.nz/cc/internal/arm64vm"
	"j5.nz/cc/internal/fdt"
	"j5.nz/cc/internal/managed/machine"
	netbsdarm64 "j5.nz/cc/internal/netbsd/boot/arm64"
	"j5.nz/cc/internal/netstack"
	"j5.nz/cc/internal/nvme"
	"j5.nz/cc/internal/serial"
	"j5.nz/cc/internal/virtio"
	"j5.nz/cc/internal/vmruntime"
)

type NetBSDManagedConfig struct {
	Kernel    []byte
	Root      virtio.BlockBackend
	MemoryMB  uint64
	Dmesg     bool
	GuestIPv4 net.IP
	GuestMAC  net.HardwareAddr
	NetDevice *virtio.Net
	NetStack  *netstack.NetStack
}

func StartNetBSDManagedSession(ctx context.Context, cfg NetBSDManagedConfig, onEvent func(client.BootEvent) error) (*ManagedSession, error) {
	cfg, err := normalizeNetBSDManagedConfig(cfg)
	if err != nil {
		return nil, err
	}

	netdev, stack, ownStack := netBSDManagedNet(cfg)
	return startNetBSDArm64ManagedSession(ctx, netBSDArm64SessionConfig{
		Spec: machine.Spec{
			Guest:    "NetBSD",
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

type netBSDArm64SessionConfig struct {
	Spec        machine.Spec
	Kernel      []byte
	Root        virtio.BlockBackend
	MemoryMB    uint64
	Dmesg       bool
	NetDevice   *virtio.Net
	NetStack    *netstack.NetStack
	OwnNetStack bool
}

const netBSDArm64LowMemoryAliasSize = 128 << 20

func startNetBSDArm64ManagedSession(ctx context.Context, cfg netBSDArm64SessionConfig, onEvent func(client.BootEvent) error) (*ManagedSession, error) {
	cfg = normalizeNetBSDArm64SessionConfig(cfg)
	if cfg.NetDevice == nil || cfg.NetStack == nil {
		return nil, fmt.Errorf("NetBSD network device and stack are required")
	}
	if err := emitManagedBootStatus(onEvent, "starting VM"); err != nil {
		return nil, err
	}
	ln, err := cfg.NetStack.ListenInternal("tcp", fmt.Sprintf(":%d", bsdControlPort))
	if err != nil {
		if cfg.OwnNetStack {
			cfg.NetStack.Close()
		}
		return nil, fmt.Errorf("listen NetBSD control tcp: %w", err)
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

	controlTranscript := vmruntime.NewSerialTranscript()
	control, connected, acceptErrCh := acceptBSDControlConnections(ln, controlTranscript)

	kvmVM, err = NewVM()
	if err != nil {
		cleanupStartup()
		return nil, err
	}
	mem, err := kvmVM.MapAnonymousMemory(arm64vm.MemorySizeBytes(cfg.MemoryMB), netBSDArm64MemoryBase)
	if err != nil {
		cleanupStartup()
		return nil, fmt.Errorf("map guest memory: %w", err)
	}
	if len(mem) >= netBSDArm64LowMemoryAliasSize {
		if _, err := kvmVM.MapAnonymousMemorySlot(1, netBSDArm64LowMemoryAliasSize, 0); err != nil {
			cleanupStartup()
			return nil, fmt.Errorf("map NetBSD low memory window: %w", err)
		}
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
	cfg.NetDevice.LegacyMMIO = true
	cfg.NetDevice.Attach(kvmVM, kvmVM)
	rng := virtio.NewRNG(arm64vm.RNGBase, arm64vm.RNGSize, arm64vm.RNGIRQ)
	rng.LegacyMMIO = true
	rng.Attach(kvmVM, kvmVM)

	plan, err := netbsdarm64.PrepareBoot(mem, cfg.Kernel, netbsdarm64.BootOptions{
		MemoryBase: netBSDArm64MemoryBase,
		MemorySize: arm64vm.MemorySizeBytes(cfg.MemoryMB),
		NumCPUs:    1,
		Console:    true,
		BootArgs:   "root=ld4a",
		ExtraNodes: []fdt.Node{
			pci.DeviceTreeNode(),
			cfg.NetDevice.DeviceTreeNode(),
			rng.DeviceTreeNode(),
		},
	})
	if err != nil {
		cleanupStartup()
		return nil, fmt.Errorf("prepare NetBSD boot: %w", err)
	}
	if err := setupNetBSDArm64Registers(kvmVM, plan); err != nil {
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
		done.finish(runNetBSDArm64ManagedVM(runCtx, vmForRun, uart, pci, cfg.NetDevice, rng, serialOut))
	}()

	select {
	case err := <-acceptErrCh:
		cleanupStartup()
		return nil, netBSDStartupError(err, serialOut.String(), controlTranscript.String())
	case <-connected:
	case <-done.done():
		err := done.result()
		_ = control.Close()
		cleanupStartup()
		return nil, netBSDStartupError(err, serialOut.String(), controlTranscript.String())
	case <-ctx.Done():
		err := fmt.Errorf("NetBSD guest did not connect to control TCP port %d before startup deadline: %w", bsdControlPort, ctx.Err())
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
		return nil, netBSDStartupError(err, serialOut.String(), controlTranscript.String())
	}

	if _, err := controlTranscript.WaitFor(ctx, 0, func(text string) bool {
		return strings.Contains(text, vmruntime.InstanceReadyMarker) || vmruntime.HasFatalBootText(text)
	}); err != nil {
		_ = control.Close()
		cleanupStartup()
		err = fmt.Errorf("NetBSD control connection did not report ready marker %q: %w", vmruntime.InstanceReadyMarker, err)
		return nil, netBSDStartupError(err, serialOut.String(), controlTranscript.String())
	}
	if vmruntime.HasFatalBootText(controlTranscript.String()) {
		_ = control.Close()
		cleanupStartup()
		return nil, netBSDStartupError(fmt.Errorf("NetBSD guest reported boot failure"), serialOut.String(), controlTranscript.String())
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

func normalizeNetBSDArm64SessionConfig(cfg netBSDArm64SessionConfig) netBSDArm64SessionConfig {
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

func netBSDStartupError(err error, serialText, controlText string) error {
	return transcriptError(err, serialText, controlText)
}

func netBSDManagedNet(cfg NetBSDManagedConfig) (*virtio.Net, *netstack.NetStack, bool) {
	if cfg.NetDevice != nil && cfg.NetStack != nil {
		return cfg.NetDevice, cfg.NetStack, false
	}
	netdev, stack := newNetBSDManagedNet(cfg.GuestIPv4, cfg.GuestMAC)
	return netdev, stack, true
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
	iface, _ := stack.AttachNetworkInterface()
	dev := virtio.NewNet(arm64vm.NetBase, arm64vm.NetSize, arm64vm.NetIRQ, mac, netBSDManagedNetBackend{iface: iface})
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

func runNetBSDArm64ManagedVM(ctx context.Context, vm *VM, uart *serial.UART8250, pci *Arm64PCIHost, netdev *virtio.Net, rng *virtio.RNG, serialOut *vmruntime.SerialTranscript) error {
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
			pc, _ := vm.GetPC()
			esr, _ := vm.GetArm64SysReg(3, 0, 5, 2, 0)
			far, _ := vm.GetArm64SysReg(3, 0, 6, 0, 0)
			elr, _ := vm.GetArm64SysReg(3, 0, 4, 0, 1)
			spsr, _ := vm.GetArm64SysReg(3, 0, 4, 0, 0)
			return fmt.Errorf("run step %d at pc=%#x esr_el1=%#x far_el1=%#x elr_el1=%#x spsr_el1=%#x: %w", step, pc, esr, far, elr, spsr, err)
		}
		switch exit.Reason {
		case ExitMMIO:
			if err := handleNetBSDArm64MMIO(vm, uart, pci, netdev, rng, exit.MMIO); err != nil {
				return err
			}
		case ExitShutdown:
			return fmt.Errorf("NetBSD guest shut down\nserial:\n%s", serialOut.String())
		case ExitSystemEvent:
			return fmt.Errorf("unexpected system event %d\nserial:\n%s", exit.SystemEvent, serialOut.String())
		case ExitArmNISV:
			pc, _ := vm.GetPC()
			return fmt.Errorf("NetBSD arm64 no-ISV exit at pc=%#x esr_iss=%#x fault_ipa=%#x\nserial:\n%s", pc, exit.ArmNISV.ESRISS, exit.ArmNISV.FaultIPA, serialOut.String())
		default:
			pc, _ := vm.GetPC()
			return fmt.Errorf("unexpected exit reason %d at pc=%#x\nserial:\n%s", exit.Reason, pc, serialOut.String())
		}
	}
}

func handleNetBSDArm64MMIO(vm *VM, uart *serial.UART8250, pci *Arm64PCIHost, netdev *virtio.Net, rng *virtio.RNG, mmio MMIOExit) error {
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
	return fmt.Errorf("unhandled NetBSD mmio addr=%#x len=%d write=%v", mmio.Addr, mmio.Len, mmio.Write)
}
