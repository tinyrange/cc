//go:build windows && arm64

package whp

import (
	"bytes"
	"context"
	"debug/elf"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/arm64vm"
	"j5.nz/cc/internal/fdt"
	freebsdarm64 "j5.nz/cc/internal/freebsd/boot/arm64"
	netbsdarm64 "j5.nz/cc/internal/netbsd/boot/arm64"
	"j5.nz/cc/internal/netstack"
	"j5.nz/cc/internal/nvme"
	openbsdarm64 "j5.nz/cc/internal/openbsd/boot/arm64"
	"j5.nz/cc/internal/serial"
	"j5.nz/cc/internal/virtio"
	"j5.nz/cc/internal/vmruntime"
)

const bsdControlPort = 10777

const (
	openBSDWHPCNTVOverflowInterrupt = 20
	openBSDWHPVirtualTimerPPI       = 4
	openBSDWHPGICLPIIntIDBits       = 1
	openBSDWHPMINGICMSILPIBits      = 14
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

type bsdARM64SessionConfig struct {
	GuestName           string
	Kernel              []byte
	Root                virtio.BlockBackend
	MemoryMB            uint64
	Dmesg               bool
	NetDevice           *virtio.Net
	NetStack            *netstack.NetStack
	OwnNetStack         bool
	BootKind            string
	MemoryBase          uint64
	BootArgs            string
	FreeBSDVirtioQuirks bool
	NetBSDVirtioQuirks  bool
}

func StartOpenBSDManagedSession(ctx context.Context, cfg OpenBSDManagedConfig, onEvent func(client.BootEvent) error) (*ManagedSession, error) {
	if len(cfg.Kernel) == 0 {
		return nil, fmt.Errorf("OpenBSD kernel is required")
	}
	if cfg.Root == nil {
		return nil, fmt.Errorf("OpenBSD root filesystem is required")
	}
	if cfg.MemoryMB == 0 {
		cfg.MemoryMB = 768
	}
	netdev, stack, ownStack := openBSDManagedNet(cfg.NetDevice, cfg.NetStack, cfg.GuestIPv4, cfg.GuestMAC)
	return startBSDARM64ManagedSession(ctx, bsdARM64SessionConfig{
		GuestName:   "OpenBSD",
		Kernel:      cfg.Kernel,
		Root:        cfg.Root,
		MemoryMB:    cfg.MemoryMB,
		Dmesg:       cfg.Dmesg,
		NetDevice:   netdev,
		NetStack:    stack,
		OwnNetStack: ownStack,
		BootKind:    "openbsd",
		MemoryBase:  arm64vm.MemoryBase,
	}, onEvent)
}

func StartFreeBSDManagedSession(ctx context.Context, cfg FreeBSDManagedConfig, onEvent func(client.BootEvent) error) (*ManagedSession, error) {
	if len(cfg.Kernel) == 0 {
		return nil, fmt.Errorf("FreeBSD kernel is required")
	}
	if cfg.Root == nil {
		return nil, fmt.Errorf("FreeBSD root filesystem is required")
	}
	if cfg.MemoryMB == 0 {
		cfg.MemoryMB = 1024
	}
	netdev, stack, ownStack := freeBSDManagedNet(cfg.NetDevice, cfg.NetStack, cfg.GuestIPv4, cfg.GuestMAC)
	return startBSDARM64ManagedSession(ctx, bsdARM64SessionConfig{
		GuestName:           "FreeBSD",
		Kernel:              cfg.Kernel,
		Root:                cfg.Root,
		MemoryMB:            cfg.MemoryMB,
		Dmesg:               cfg.Dmesg,
		NetDevice:           netdev,
		NetStack:            stack,
		OwnNetStack:         ownStack,
		BootKind:            "freebsd",
		MemoryBase:          arm64vm.MemoryBase,
		FreeBSDVirtioQuirks: true,
	}, onEvent)
}

func StartNetBSDManagedSession(ctx context.Context, cfg NetBSDManagedConfig, onEvent func(client.BootEvent) error) (*ManagedSession, error) {
	if len(cfg.Kernel) == 0 {
		return nil, fmt.Errorf("NetBSD kernel is required")
	}
	if cfg.Root == nil {
		return nil, fmt.Errorf("NetBSD root filesystem is required")
	}
	if cfg.MemoryMB == 0 {
		cfg.MemoryMB = 1024
	}
	netdev, stack, ownStack := netBSDManagedNet(cfg.NetDevice, cfg.NetStack, cfg.GuestIPv4, cfg.GuestMAC)
	return startBSDARM64ManagedSession(ctx, bsdARM64SessionConfig{
		GuestName:          "NetBSD",
		Kernel:             cfg.Kernel,
		Root:               cfg.Root,
		MemoryMB:           cfg.MemoryMB,
		Dmesg:              cfg.Dmesg,
		NetDevice:          netdev,
		NetStack:           stack,
		OwnNetStack:        ownStack,
		BootKind:           "netbsd",
		MemoryBase:         netBSDArm64MemoryBase,
		NetBSDVirtioQuirks: true,
	}, onEvent)
}

const netBSDArm64MemoryBase = 0x40000000

func startBSDARM64ManagedSession(ctx context.Context, cfg bsdARM64SessionConfig, onEvent func(client.BootEvent) error) (*ManagedSession, error) {
	traceStart := time.Now()
	trace := os.Getenv("CC_WHP_BSD_TIMING") != ""
	tracef := func(format string, args ...any) {
		if trace {
			_, _ = fmt.Fprintf(os.Stderr, "whp-bsd %s +%s: %s\n", cfg.GuestName, time.Since(traceStart).Round(time.Millisecond), fmt.Sprintf(format, args...))
		}
	}
	if cfg.NetDevice == nil || cfg.NetStack == nil {
		return nil, fmt.Errorf("%s network device and stack are required", cfg.GuestName)
	}
	if err := emitManagedBootStatus(onEvent, "starting VM"); err != nil {
		return nil, err
	}
	ln, err := cfg.NetStack.ListenInternal("tcp", fmt.Sprintf(":%d", bsdControlPort))
	if err != nil {
		if cfg.OwnNetStack {
			cfg.NetStack.Close()
		}
		return nil, fmt.Errorf("listen %s control tcp: %w", cfg.GuestName, err)
	}

	var (
		vm         *VM
		bootWriter *vmruntime.BootEventWriter
	)
	cleanupStartup := func() {
		_ = ln.Close()
		if cfg.OwnNetStack && cfg.NetStack != nil {
			cfg.NetStack.Close()
		}
		if bootWriter != nil {
			_ = bootWriter.Close()
		}
		if vm != nil {
			_ = vm.Close()
		}
	}

	connCh := make(chan net.Conn, 1)
	acceptErrCh := make(chan error, 1)
	controlTranscript := vmruntime.NewSerialTranscript()
	go acceptBSDControlConnection(ln, controlTranscript, connCh, acceptErrCh)

	memorySize := arm64vm.MemorySizeBytes(cfg.MemoryMB)
	memoryBase := cfg.MemoryBase
	if memoryBase == 0 {
		memoryBase = arm64vm.MemoryBase
	}
	vmOpts := VMOptions{}
	enableOpenBSDMSI := false
	if cfg.BootKind == "openbsd" {
		vmOpts.CNTVOverflowInterrupt = openBSDWHPCNTVOverflowInterrupt
		lpiBits, err := getCapability[uint32](capabilityCodeGicLpiIntIDBits)
		if err == nil && lpiBits >= openBSDWHPMINGICMSILPIBits {
			vmOpts.GICLPIIntIDBits = lpiBits
			enableOpenBSDMSI = true
		} else {
			vmOpts.GICLPIIntIDBits = openBSDWHPGICLPIIntIDBits
		}
		tracef("openbsd lpiBits=%d enableMSI=%t", vmOpts.GICLPIIntIDBits, enableOpenBSDMSI)
	}
	vm, err = NewVMWithOptions(memorySize, memoryBase, vmOpts)
	if err != nil {
		cleanupStartup()
		return nil, err
	}
	tracef("vm created")
	mem := vm.Memory()

	serialOut := vmruntime.NewSerialTranscript()
	var serialWriter io.Writer = serialOut
	if os.Getenv("CC_WHP_BSD_SERIAL") != "" {
		serialWriter = io.MultiWriter(serialWriter, os.Stderr)
	}
	if onEvent != nil {
		bootWriter = vmruntime.NewBootEventWriter(onEvent)
		serialWriter = io.MultiWriter(serialOut, bootWriter)
		if os.Getenv("CC_WHP_BSD_SERIAL") != "" {
			serialWriter = io.MultiWriter(serialWriter, os.Stderr)
		}
	}
	uart := serial.NewUART8250(arm64vm.DefaultUARTBase, arm64vm.DefaultUARTRegShift, serialWriter)
	uart.AttachIRQ(vm, arm64vm.UARTSPI)

	nvmeBlock := nvme.NewController(cfg.Root)
	nvmeBlock.Attach(vm, vm)
	nvmePCI := newArm64NVMePCIDevice(1, arm64vm.NVMeBase, arm64vm.NVMeIRQ, nvmeBlock)
	pci := newArm64PCIHost(nvmePCI)
	if enableOpenBSDMSI {
		nvmePCI.MSI = true
		pci.msiParent = openbsdarm64.DefaultGICITSPhandle
	}

	if cfg.FreeBSDVirtioQuirks {
		cfg.NetDevice.DisableMergeRX = true
		cfg.NetDevice.HeaderLength = 12
	}
	if cfg.NetBSDVirtioQuirks {
		cfg.NetDevice.DisableMergeRX = true
		cfg.NetDevice.LegacyMMIO = true
	}
	if cfg.BootKind == "openbsd" {
		cfg.NetDevice.NoTXIRQ = true
		cfg.NetDevice.NoTXUsed = true
	}
	cfg.NetDevice.AsyncTX = true
	cfg.NetDevice.AsyncTXDelay = 10 * time.Millisecond
	cfg.NetDevice.Attach(vm, vm)
	rng := virtio.NewRNG(arm64vm.RNGBase, arm64vm.RNGSize, arm64vm.RNGIRQ)
	rng.LegacyMMIO = cfg.NetBSDVirtioQuirks
	rng.Attach(vm, vm)
	rtc := arm64vm.NewPL031(arm64vm.RTCBase, arm64vm.RTCSize, time.Now())
	rootNodes := []fdt.Node{pci.DeviceTreeNode(), rtc.DeviceTreeNode()}

	switch cfg.BootKind {
	case "openbsd":
		plan, err := openbsdarm64.PrepareBoot(mem, cfg.Kernel, openbsdarm64.BootOptions{
			MemoryBase:                      memoryBase,
			MemorySize:                      memorySize,
			NumCPUs:                         1,
			GICVersion:                      openbsdarm64.GICVersionV3,
			Console:                         true,
			VirtualTimerPPI:                 openBSDWHPVirtualTimerPPI,
			EnableGICITS:                    enableOpenBSDMSI,
			UsePMRShiftForInterruptPriority: true,
			DisableNVMeINTxMasking:          true,
			ExtraNodes:                      append(rootNodes, cfg.NetDevice.DeviceTreeNode(), rng.DeviceTreeNode()),
		})
		if err != nil {
			cleanupStartup()
			return nil, fmt.Errorf("prepare OpenBSD boot: %w", err)
		}
		if err := setupOpenBSDBootState(vm, plan); err != nil {
			cleanupStartup()
			return nil, err
		}
		tracef("boot state prepared")
	case "freebsd":
		plan, err := freebsdarm64.PrepareBoot(mem, cfg.Kernel, freebsdarm64.BootOptions{
			MemoryBase: memoryBase,
			MemorySize: memorySize,
			NumCPUs:    1,
			GICVersion: freebsdarm64.GICVersionV3,
			Console:    true,
			ExtraNodes: append(rootNodes, cfg.NetDevice.DeviceTreeNode(), rng.DeviceTreeNode()),
		})
		if err != nil {
			cleanupStartup()
			return nil, fmt.Errorf("prepare FreeBSD boot: %w", err)
		}
		if err := setupFreeBSDBootState(vm, plan); err != nil {
			cleanupStartup()
			return nil, err
		}
		tracef("boot state prepared")
	case "netbsd":
		bootArgs := cfg.BootArgs
		if bootArgs == "" {
			bootArgs = "root=ld4a"
		}
		plan, err := netbsdarm64.PrepareBoot(mem, cfg.Kernel, netbsdarm64.BootOptions{
			MemoryBase: memoryBase,
			MemorySize: memorySize,
			NumCPUs:    1,
			GICVersion: netbsdarm64.GICVersionV3,
			Console:    true,
			BootArgs:   bootArgs,
			ExtraNodes: append(rootNodes, cfg.NetDevice.DeviceTreeNode(), rng.DeviceTreeNode()),
		})
		if err != nil {
			cleanupStartup()
			return nil, fmt.Errorf("prepare NetBSD boot: %w", err)
		}
		if err := setupNetBSDBootState(vm, plan); err != nil {
			cleanupStartup()
			return nil, err
		}
		tracef("boot state prepared")
	default:
		cleanupStartup()
		return nil, fmt.Errorf("unsupported BSD boot kind %q", cfg.BootKind)
	}

	runCtx, cancel := context.WithCancel(context.Background())
	doneCh := make(chan error, 1)
	vmForRun := vm
	vm = nil
	go func() {
		err := runBSDARM64ManagedVM(runCtx, cfg.GuestName, vmForRun, uart, pci, cfg.NetDevice, rng, rtc, serialOut, newWHPPCSampler(cfg.GuestName, cfg.Kernel))
		err = errors.Join(err, vmForRun.Close())
		doneCh <- err
	}()
	tracef("vm run loop started")

	stopStartedVM := func() {
		cancel()
		_ = ln.Close()
		if bootWriter != nil {
			_ = bootWriter.Close()
		}
		select {
		case <-doneCh:
		case <-time.After(time.Second):
		}
		if cfg.OwnNetStack && cfg.NetStack != nil {
			cfg.NetStack.Close()
		}
	}

	var control net.Conn
	select {
	case err := <-acceptErrCh:
		stopStartedVM()
		return nil, bsdStartupError(err, serialOut.String(), controlTranscript.String())
	case conn := <-connCh:
		control = conn
		tracef("control connected")
	case err := <-doneCh:
		cancel()
		_ = ln.Close()
		if bootWriter != nil {
			_ = bootWriter.Close()
		}
		if cfg.OwnNetStack && cfg.NetStack != nil {
			cfg.NetStack.Close()
		}
		return nil, bsdStartupError(err, serialOut.String(), controlTranscript.String())
	case <-ctx.Done():
		err := fmt.Errorf("%s guest did not connect to control TCP port %d before startup deadline: %w", cfg.GuestName, bsdControlPort, ctx.Err())
		cancel()
		select {
		case runErr := <-doneCh:
			if runErr != nil {
				err = fmt.Errorf("%w; VM run result: %v", err, runErr)
			}
		case <-time.After(time.Second):
		}
		_ = ln.Close()
		if bootWriter != nil {
			_ = bootWriter.Close()
		}
		if cfg.OwnNetStack && cfg.NetStack != nil {
			cfg.NetStack.Close()
		}
		return nil, bsdStartupError(err, serialOut.String(), controlTranscript.String())
	}

	if _, err := controlTranscript.WaitFor(ctx, 0, containsReadyOrFatal); err != nil {
		_ = control.Close()
		stopStartedVM()
		err = fmt.Errorf("%s control connection did not report ready marker %q: %w", cfg.GuestName, vmruntime.InstanceReadyMarker, err)
		return nil, bsdStartupError(err, serialOut.String(), controlTranscript.String())
	}
	if vmruntime.HasFatalBootText(controlTranscript.String()) {
		_ = control.Close()
		stopStartedVM()
		return nil, bsdStartupError(fmt.Errorf("%s guest reported boot failure", cfg.GuestName), serialOut.String(), controlTranscript.String())
	}
	tracef("ready marker received")
	if err := emitManagedBootStatus(onEvent, "guest ready"); err != nil {
		_ = control.Close()
		stopStartedVM()
		return nil, err
	}

	var cleanup func()
	if cfg.OwnNetStack && cfg.NetStack != nil {
		cleanup = func() {
			_ = cfg.NetStack.Close()
		}
	}
	return &ManagedSession{
		cancel:     cancel,
		doneCh:     doneCh,
		control:    control,
		listener:   ln,
		bootWriter: bootWriter,
		transcript: controlTranscript,
		serialOut:  serialOut,
		cleanup:    cleanup,
		dmesg:      cfg.Dmesg,
	}, nil
}

func openBSDManagedNet(netdev *virtio.Net, stack *netstack.NetStack, guestIPv4 net.IP, mac net.HardwareAddr) (*virtio.Net, *netstack.NetStack, bool) {
	return newBSDManagedNet(netdev, stack, guestIPv4, mac, false, 0, false, 50*time.Millisecond)
}

func freeBSDManagedNet(netdev *virtio.Net, stack *netstack.NetStack, guestIPv4 net.IP, mac net.HardwareAddr) (*virtio.Net, *netstack.NetStack, bool) {
	return newBSDManagedNet(netdev, stack, guestIPv4, mac, true, 12, false, 0)
}

func netBSDManagedNet(netdev *virtio.Net, stack *netstack.NetStack, guestIPv4 net.IP, mac net.HardwareAddr) (*virtio.Net, *netstack.NetStack, bool) {
	return newBSDManagedNet(netdev, stack, guestIPv4, mac, true, 0, true, 0)
}

func acceptBSDControlConnection(ln net.Listener, transcript *vmruntime.SerialTranscript, connCh chan<- net.Conn, errCh chan<- error) {
	const candidateReadyTimeout = time.Second
	buf := make([]byte, 32*1024)

	for {
		conn, err := ln.Accept()
		if err != nil {
			errCh <- err
			return
		}
		start := transcript.Len()
		_ = conn.SetReadDeadline(time.Now().Add(candidateReadyTimeout))

		for {
			n, readErr := conn.Read(buf)
			if n > 0 {
				_, _ = transcript.Write(buf[:n])
				text := transcript.String()
				if start <= len(text) && containsReadyOrFatal(text[start:]) {
					_ = conn.SetReadDeadline(time.Time{})
					connCh <- conn
					for readErr == nil {
						n, readErr = conn.Read(buf)
						if n > 0 {
							_, _ = transcript.Write(buf[:n])
						}
					}
					return
				}
			}
			if readErr != nil {
				_ = conn.Close()
				break
			}
		}
	}
}

type bsdManagedNetBackend struct {
	iface *netstack.NetworkInterface
}

func (b bsdManagedNetBackend) HandleTxPacket(packet []byte) error {
	return b.iface.DeliverGuestPacket(packet, true)
}

func newBSDManagedNet(netdev *virtio.Net, stack *netstack.NetStack, guestIPv4 net.IP, mac net.HardwareAddr, disableMergeRX bool, headerLength int, legacyMMIO bool, rxDelay time.Duration) (*virtio.Net, *netstack.NetStack, bool) {
	if netdev != nil && stack != nil {
		return netdev, stack, false
	}
	if guestIPv4 == nil {
		guestIPv4 = net.IPv4(10, 42, 0, 2)
	}
	if mac == nil {
		mac = net.HardwareAddr{0x02, 0x42, 0x0a, 0x2a, 0x00, 0x02}
	}
	stack = netstack.New(slog.Default())
	_ = stack.SetGuestMAC(mac)
	_ = stack.SetGuestIPv4(guestIPv4)
	iface, _ := stack.AttachNetworkInterface()
	netdev = virtio.NewNet(arm64vm.NetBase, arm64vm.NetSize, arm64vm.NetIRQ, mac, bsdManagedNetBackend{iface: iface})
	netdev.DisableMergeRX = disableMergeRX
	netdev.HeaderLength = headerLength
	netdev.LegacyMMIO = legacyMMIO
	iface.AttachVirtioBackend(func(frame []byte) error {
		copied := append([]byte(nil), frame...)
		go func() {
			if rxDelay > 0 {
				time.Sleep(rxDelay)
			}
			if err := netdev.EnqueueRxPacketOwned(copied); err != nil {
				slog.Warn("enqueue BSD RX packet", "error", err)
			}
		}()
		return nil
	})
	return netdev, stack, true
}

func setupOpenBSDBootState(vm *VM, plan *openbsdarm64.BootPlan) error {
	if err := vm.SetPC(plan.EntryGPA); err != nil {
		return fmt.Errorf("set PC: %w", err)
	}
	if err := vm.SetPState(arm64vm.DefaultPStateBits); err != nil {
		return fmt.Errorf("set PSTATE: %w", err)
	}
	if err := vm.SetX(0, plan.KernelEndGPA); err != nil {
		return fmt.Errorf("set X0: %w", err)
	}
	if err := vm.SetX(1, 0); err != nil {
		return fmt.Errorf("set X1: %w", err)
	}
	if err := vm.SetX(2, plan.DeviceTreeGPA); err != nil {
		return fmt.Errorf("set X2: %w", err)
	}
	return nil
}

func setupFreeBSDBootState(vm *VM, plan *freebsdarm64.BootPlan) error {
	if err := vm.SetPC(plan.EntryGPA); err != nil {
		return fmt.Errorf("set PC: %w", err)
	}
	if err := vm.SetPState(arm64vm.DefaultPStateBits); err != nil {
		return fmt.Errorf("set PSTATE: %w", err)
	}
	if err := vm.SetX(0, plan.DeviceTreeGPA); err != nil {
		return fmt.Errorf("set X0: %w", err)
	}
	for reg := 1; reg <= 3; reg++ {
		if err := vm.SetX(reg, 0); err != nil {
			return fmt.Errorf("clear X%d: %w", reg, err)
		}
	}
	return nil
}

func setupNetBSDBootState(vm *VM, plan *netbsdarm64.BootPlan) error {
	if err := vm.SetPC(plan.EntryGPA); err != nil {
		return fmt.Errorf("set PC: %w", err)
	}
	if err := vm.SetPState(0x40000000 | arm64vm.DefaultPStateBits); err != nil {
		return fmt.Errorf("set PSTATE: %w", err)
	}
	if err := vm.SetX(0, plan.DeviceTreeGPA); err != nil {
		return fmt.Errorf("set X0: %w", err)
	}
	for reg := 1; reg <= 3; reg++ {
		if err := vm.SetX(reg, 0); err != nil {
			return fmt.Errorf("clear X%d: %w", reg, err)
		}
	}
	return nil
}

func runBSDARM64ManagedVM(ctx context.Context, guestName string, vm *VM, uart *serial.UART8250, pci *arm64PCIHost, netdev *virtio.Net, rng *virtio.RNG, rtc *arm64vm.PL031, serialOut *vmruntime.SerialTranscript, sampler *whpPCSampler) error {
	go answerBSDPrompts(ctx, guestName, uart, serialOut)
	if sampler != nil {
		defer sampler.dump("final")
	}
	trace := os.Getenv("CC_WHP_BSD_TIMING") != ""
	traceStart := time.Now()
	lastTrace := traceStart
	var nextSample time.Time
	if sampler != nil {
		nextSample = traceStart.Add(sampler.interval)
	}
	var exits, mmioExits, canceledExits uint64
	for step := 0; ; step++ {
		if err := ctx.Err(); err != nil {
			pc, _ := vm.GetPC()
			return fmt.Errorf("%w at pc=%#x", err, pc)
		}
		var exit Exit
		runCtx := ctx
		var cancel context.CancelFunc
		if sampler != nil {
			timeout := time.Until(nextSample)
			if timeout <= 0 {
				timeout = time.Nanosecond
			}
			runCtx, cancel = context.WithTimeout(ctx, timeout)
		}
		err := vm.RunInterruptible(runCtx, &exit)
		now := time.Now()
		if cancel != nil {
			cancel()
		}
		if err != nil {
			if sampler != nil && errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
				sampler.recordCurrentPC(vm, now)
				nextSample = sampler.nextAfter(nextSample, now)
				continue
			}
			if ctx.Err() != nil {
				pc, _ := vm.GetPC()
				return fmt.Errorf("%w at pc=%#x", ctx.Err(), pc)
			}
			return fmt.Errorf("run %s step %d: %w\nserial:\n%s", guestName, step, err, serialOut.String())
		}
		if sampler != nil && !now.Before(nextSample) {
			lr, _ := vm.getRegister(registerX(30))
			if exit.PC != 0 {
				sampler.record(exit.PC, lr, now)
			} else {
				sampler.recordCurrentPC(vm, now)
			}
			nextSample = sampler.nextAfter(nextSample, now)
		}
		if trace {
			exits++
			if now.Sub(lastTrace) >= 5*time.Second {
				_, _ = fmt.Fprintf(os.Stderr, "whp-bsd %s run +%s: exits=%d mmio=%d canceled=%d serial=%d\n",
					guestName, now.Sub(traceStart).Round(time.Millisecond), exits, mmioExits, canceledExits, serialOut.Len())
				lastTrace = now
			}
		}
		switch exit.Reason {
		case runVPExitReasonUnmappedGPA, runVPExitReasonGPAIntercept:
			if trace {
				mmioExits++
			}
			if err := handleBSDARM64MMIO(vm, uart, pci, netdev, rng, rtc, exit.MMIO); err != nil {
				return fmt.Errorf("handle %s mmio addr=%#x len=%d write=%v pc=%#x: %w\nserial:\n%s",
					guestName, exit.MMIO.Addr, exit.MMIO.Len, exit.MMIO.Write, exit.MMIO.PC, err, serialOut.String())
			}
		case runVPExitReasonCanceled:
			if trace {
				canceledExits++
			}
		case runVPExitReasonArm64Reset:
			return fmt.Errorf("%s guest requested arm64 reset\nserial:\n%s", guestName, serialOut.String())
		default:
			pc, _ := vm.GetPC()
			return fmt.Errorf("unexpected %s exit %s at pc=%#x\nserial:\n%s", guestName, exit.Reason, pc, serialOut.String())
		}
	}
}

type whpPCSampler struct {
	guestName string
	interval  time.Duration
	start     time.Time
	lastDump  time.Time
	symbols   []whpPCSymbol
	counts    map[string]uint64
	callers   map[string]uint64
	rawCounts map[uint64]uint64
	total     uint64
}

type whpPCSymbol struct {
	name string
	addr uint64
	size uint64
}

func newWHPPCSampler(guestName string, kernel []byte) *whpPCSampler {
	if os.Getenv("CC_WHP_PC_SAMPLE") == "" {
		return nil
	}
	now := time.Now()
	return &whpPCSampler{
		guestName: guestName,
		interval:  100 * time.Millisecond,
		start:     now,
		lastDump:  now,
		symbols:   whpKernelSymbols(kernel),
		counts:    make(map[string]uint64),
		callers:   make(map[string]uint64),
		rawCounts: make(map[uint64]uint64),
	}
}

func whpKernelSymbols(kernel []byte) []whpPCSymbol {
	f, err := elf.NewFile(bytes.NewReader(kernel))
	if err != nil {
		return nil
	}
	raw, err := f.Symbols()
	if err != nil {
		return nil
	}
	symbols := make([]whpPCSymbol, 0, len(raw))
	for _, sym := range raw {
		if sym.Value == 0 || sym.Size == 0 || elf.ST_TYPE(sym.Info) != elf.STT_FUNC {
			continue
		}
		symbols = append(symbols, whpPCSymbol{name: sym.Name, addr: sym.Value, size: sym.Size})
	}
	sort.Slice(symbols, func(i, j int) bool {
		return symbols[i].addr < symbols[j].addr
	})
	return symbols
}

func (s *whpPCSampler) nextAfter(next, now time.Time) time.Time {
	for !next.After(now) {
		next = next.Add(s.interval)
	}
	return next
}

func (s *whpPCSampler) recordCurrentPC(vm *VM, now time.Time) {
	if pc, err := vm.GetPC(); err == nil {
		lr, _ := vm.getRegister(registerX(30))
		s.record(pc, lr, now)
	}
}

func (s *whpPCSampler) record(pc, lr uint64, now time.Time) {
	if s == nil {
		return
	}
	pcDesc := s.describe(pc)
	s.total++
	s.rawCounts[pc]++
	s.counts[pcDesc]++
	if lr != 0 {
		s.callers[fmt.Sprintf("%s <- %s", pcDesc, s.describe(lr))]++
	}
	if now.Sub(s.lastDump) >= 5*time.Second {
		s.dump("sample")
		s.lastDump = now
	}
}

func (s *whpPCSampler) describe(pc uint64) string {
	if len(s.symbols) == 0 {
		return fmt.Sprintf("%#x", pc)
	}
	i := sort.Search(len(s.symbols), func(i int) bool {
		return s.symbols[i].addr > pc
	})
	if i == 0 {
		return fmt.Sprintf("%#x", pc)
	}
	sym := s.symbols[i-1]
	if pc >= sym.addr+sym.size {
		return fmt.Sprintf("%#x", pc)
	}
	return fmt.Sprintf("%s+%#x", sym.name, pc-sym.addr)
}

func (s *whpPCSampler) dump(label string) {
	if s == nil || s.total == 0 {
		return
	}
	type entry struct {
		name  string
		count uint64
	}
	entries := make([]entry, 0, len(s.counts))
	for name, count := range s.counts {
		entries = append(entries, entry{name: name, count: count})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].count == entries[j].count {
			return entries[i].name < entries[j].name
		}
		return entries[i].count > entries[j].count
	})
	limit := 12
	if len(entries) < limit {
		limit = len(entries)
	}
	_, _ = fmt.Fprintf(os.Stderr, "whp-pc-sample %s %s +%s total=%d\n", s.guestName, label, time.Since(s.start).Round(time.Millisecond), s.total)
	for i := 0; i < limit; i++ {
		_, _ = fmt.Fprintf(os.Stderr, "whp-pc-sample %s %5d %5.1f%% %s\n", s.guestName, entries[i].count, float64(entries[i].count)*100/float64(s.total), entries[i].name)
	}
	callerEntries := make([]entry, 0, len(s.callers))
	for name, count := range s.callers {
		callerEntries = append(callerEntries, entry{name: name, count: count})
	}
	sort.Slice(callerEntries, func(i, j int) bool {
		if callerEntries[i].count == callerEntries[j].count {
			return callerEntries[i].name < callerEntries[j].name
		}
		return callerEntries[i].count > callerEntries[j].count
	})
	if len(callerEntries) < limit {
		limit = len(callerEntries)
	}
	for i := 0; i < limit; i++ {
		_, _ = fmt.Fprintf(os.Stderr, "whp-pc-sample %s caller %5d %5.1f%% %s\n", s.guestName, callerEntries[i].count, float64(callerEntries[i].count)*100/float64(s.total), callerEntries[i].name)
	}
}

func handleBSDARM64MMIO(vm *VM, uart *serial.UART8250, pci *arm64PCIHost, netdev *virtio.Net, rng *virtio.RNG, rtc *arm64vm.PL031, mmio MMIOExit) error {
	if pci != nil && pci.Contains(mmio.Addr, int(mmio.Len)) {
		if mmio.Write {
			if err := pci.Write(mmio.Addr, int(mmio.Len), mmioValue(mmio)); err != nil {
				return err
			}
			return vm.CompleteMMIOWrite(mmio)
		}
		value, err := pci.Read(mmio.Addr, int(mmio.Len))
		if err != nil {
			return err
		}
		return vm.CompleteMMIORead(mmio, value)
	}
	if rtc != nil && rtc.Contains(mmio.Addr, int(mmio.Len)) {
		if mmio.Write {
			if err := rtc.Write(mmio.Addr, int(mmio.Len), mmioValue(mmio)); err != nil {
				return err
			}
			return vm.CompleteMMIOWrite(mmio)
		}
		value, err := rtc.Read(mmio.Addr, int(mmio.Len))
		if err != nil {
			return err
		}
		return vm.CompleteMMIORead(mmio, value)
	}
	return handleBootMMIO(vm, uart, nil, nil, rng, netdev, mmio, nil)
}

func answerBSDPrompts(ctx context.Context, guestName string, uart *serial.UART8250, serialOut *vmruntime.SerialTranscript) {
	var answeredRoot atomic.Bool
	var answeredSwap atomic.Bool
	var answeredMountroot atomic.Bool
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			serialText := serialOut.String()
			if strings.EqualFold(guestName, "OpenBSD") {
				if !answeredRoot.Load() && strings.Contains(serialText, "root device:") {
					answeredRoot.Store(true)
					_ = uart.InjectRXBytes([]byte("sd0a\n"))
				}
				if answeredRoot.Load() && !answeredSwap.Load() && strings.Contains(serialText, "swap device") {
					answeredSwap.Store(true)
					_ = uart.InjectRXBytes([]byte("\n"))
				}
				continue
			}
			if strings.EqualFold(guestName, "NetBSD") {
				if !answeredRoot.Load() && strings.Contains(serialText, "root device:") {
					answeredRoot.Store(true)
					_ = uart.InjectRXBytes([]byte("ld4a\n"))
				}
				continue
			}
			if !answeredMountroot.Load() && strings.Contains(serialText, "mountroot>") {
				answeredMountroot.Store(true)
				select {
				case <-ctx.Done():
					return
				case <-time.After(100 * time.Millisecond):
				}
				_ = uart.InjectRXBytes([]byte("ufs:/dev/nda0\n"))
			}
		}
	}
}

func containsReadyOrFatal(text string) bool {
	return strings.Contains(text, vmruntime.InstanceReadyMarker) || vmruntime.HasFatalBootText(text)
}

func bsdStartupError(err error, serialText, controlText string) error {
	return transcriptError(err, serialText, controlText)
}
