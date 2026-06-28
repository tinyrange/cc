//go:build darwin && arm64

package hvf

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
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

type bsdManagedConfig struct {
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
	NVMeRoot            bool
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
	return startBSDManagedSession(ctx, bsdManagedConfig{
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
		NVMeRoot:    darwinArm64NVMeRootEnabled(),
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
	return startBSDManagedSession(ctx, bsdManagedConfig{
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
		NVMeRoot:            darwinArm64NVMeRootEnabled(),
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
	return startBSDManagedSession(ctx, bsdManagedConfig{
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
		NVMeRoot:           darwinArm64NVMeRootEnabled(),
		NetBSDVirtioQuirks: true,
	}, onEvent)
}

const netBSDArm64MemoryBase = 0x40000000

func startBSDManagedSession(ctx context.Context, cfg bsdManagedConfig, onEvent func(client.BootEvent) error) (*ManagedSession, error) {
	if err := emitManagedBootStatus(onEvent, "starting VM"); err != nil {
		return nil, err
	}
	ln, err := cfg.NetStack.ListenInternal("tcp", fmt.Sprintf(":%d", bsdControlPort))
	if err != nil {
		return nil, fmt.Errorf("listen %s control tcp: %w", cfg.GuestName, err)
	}

	var vm *VM
	var cancel context.CancelFunc
	var bootWriter *bootEventWriter
	cleanupStartup := func() {
		if cancel != nil {
			cancel()
		}
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
	controlTranscript := newSerialTranscript()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			acceptErrCh <- err
			return
		}
		connCh <- conn
		_, _ = io.Copy(controlTranscript, conn)
	}()

	vm, err = NewVMWithOptions(ctx, VMOptions{CPUs: 1})
	if err != nil {
		cleanupStartup()
		return nil, err
	}
	memorySize := arm64vm.MemorySizeBytes(cfg.MemoryMB)
	memoryBase := cfg.MemoryBase
	if memoryBase == 0 {
		memoryBase = arm64vm.MemoryBase
	}
	mem, err := vm.MapAnonymousMemory(uintptr(memorySize), IPA(memoryBase), hvMemoryRead|hvMemoryWrite|hvMemoryExec)
	if err != nil {
		cleanupStartup()
		return nil, fmt.Errorf("map guest memory: %w", err)
	}

	serialOut := newSerialTranscript()
	var serialWriter io.Writer = serialOut
	if onEvent != nil {
		bootWriter = newBootEventWriter(onEvent)
		serialWriter = io.MultiWriter(serialOut, bootWriter)
	}
	uart := serial.NewUART8250(arm64vm.DefaultUARTBase, arm64vm.DefaultUARTRegShift, serialWriter)
	uart.AttachIRQ(vm, arm64vm.UARTSPI)
	block := virtio.NewBlock(arm64vm.ShareFSBase, arm64vm.RootFSSize, arm64vm.ShareFSIRQ, cfg.Root)
	block.DisableSizeMax = cfg.FreeBSDVirtioQuirks
	block.LegacyMMIO = cfg.NetBSDVirtioQuirks
	block.Attach(vm, vm)
	var pci *hvfPCIHost
	var nvmeBlock *nvme.Controller
	if cfg.NVMeRoot {
		nvmeBlock = nvme.NewController(cfg.Root)
		nvmeBlock.Attach(vm, vm)
		pci = newHVFPCIHost(newHVFNVMePCIDevice(1, arm64vm.NVMeBase, arm64vm.NVMeIRQ, nvmeBlock))
	}
	if cfg.FreeBSDVirtioQuirks {
		cfg.NetDevice.DisableMergeRX = true
		cfg.NetDevice.HeaderLength = 12
	}
	if cfg.NetBSDVirtioQuirks {
		cfg.NetDevice.DisableMergeRX = true
		cfg.NetDevice.LegacyMMIO = true
	}
	cfg.NetDevice.Attach(vm, vm)
	rng := virtio.NewRNG(arm64vm.RNGBase, arm64vm.RNGSize, arm64vm.RNGIRQ)
	rng.LegacyMMIO = cfg.NetBSDVirtioQuirks
	rng.Attach(vm, vm)
	rootNodes := bsdRootBlockNodes(block, pci)

	switch cfg.BootKind {
	case "openbsd":
		plan, err := openbsdarm64.PrepareBoot(mem, cfg.Kernel, openbsdarm64.BootOptions{
			MemoryBase: memoryBase,
			MemorySize: memorySize,
			NumCPUs:    1,
			GICVersion: openbsdarm64.GICVersionV3,
			Console:    true,
			ExtraNodes: append(rootNodes, cfg.NetDevice.DeviceTreeNode(), rng.DeviceTreeNode()),
		})
		if err != nil {
			cleanupStartup()
			return nil, fmt.Errorf("prepare OpenBSD boot: %w", err)
		}
		if err := setupOpenBSDBootState(vm, plan); err != nil {
			cleanupStartup()
			return nil, err
		}
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
	default:
		cleanupStartup()
		return nil, fmt.Errorf("unsupported BSD boot kind %q", cfg.BootKind)
	}

	runCtx, runCancel := context.WithCancel(context.Background())
	cancel = runCancel
	done := newManagedSessionDone()
	closeDone := make(chan struct{})
	vmForRun := vm
	vm = nil
	go func() {
		defer close(closeDone)
		defer vmForRun.Close()
		done.finish(runBSDManagedVM(runCtx, vmForRun, cfg.GuestName, uart, block, pci, cfg.NetDevice, rng, serialOut))
	}()

	var control net.Conn
	select {
	case err := <-acceptErrCh:
		cleanupStartup()
		return nil, bsdStartupError(err, serialOut.String(), controlTranscript.String())
	case conn := <-connCh:
		control = conn
	case <-done.done():
		cleanupStartup()
		return nil, bsdStartupError(done.result(), serialOut.String(), controlTranscript.String())
	case <-ctx.Done():
		err := fmt.Errorf("%s guest did not connect to control TCP port %d before startup deadline: %w", cfg.GuestName, bsdControlPort, ctx.Err())
		cancel()
		select {
		case <-done.done():
			if runErr := done.result(); runErr != nil {
				err = fmt.Errorf("%w; VM run result: %v", err, runErr)
			}
		case <-time.After(time.Second):
		}
		cleanupStartup()
		return nil, bsdStartupError(err, serialOut.String(), controlTranscript.String())
	}

	if _, err := controlTranscript.WaitFor(ctx, 0, containsReadyOrFatal); err != nil {
		_ = control.Close()
		cleanupStartup()
		err = fmt.Errorf("%s control connection did not report ready marker %q: %w", cfg.GuestName, vmruntime.InstanceReadyMarker, err)
		return nil, bsdStartupError(err, serialOut.String(), controlTranscript.String())
	}
	if vmruntime.HasFatalBootText(controlTranscript.String()) {
		_ = control.Close()
		cleanupStartup()
		return nil, bsdStartupError(fmt.Errorf("%s guest reported boot failure", cfg.GuestName), serialOut.String(), controlTranscript.String())
	}
	if err := emitManagedBootStatus(onEvent, "guest ready"); err != nil {
		_ = control.Close()
		cleanupStartup()
		return nil, err
	}

	return &ManagedSession{
		cancel:     cancel,
		done:       done,
		closeDone:  closeDone,
		control:    control,
		listener:   ln,
		bootWriter: bootWriter,
		transcript: controlTranscript,
		serialOut:  serialOut,
		cleanup: func() {
			if cfg.OwnNetStack && cfg.NetStack != nil {
				cfg.NetStack.Close()
			}
		},
		dmesg: cfg.Dmesg,
	}, nil
}

func openBSDManagedNet(netdev *virtio.Net, stack *netstack.NetStack, guestIPv4 net.IP, mac net.HardwareAddr) (*virtio.Net, *netstack.NetStack, bool) {
	return newBSDManagedNet(netdev, stack, guestIPv4, mac, false, 0, false)
}

func freeBSDManagedNet(netdev *virtio.Net, stack *netstack.NetStack, guestIPv4 net.IP, mac net.HardwareAddr) (*virtio.Net, *netstack.NetStack, bool) {
	return newBSDManagedNet(netdev, stack, guestIPv4, mac, true, 12, false)
}

func netBSDManagedNet(netdev *virtio.Net, stack *netstack.NetStack, guestIPv4 net.IP, mac net.HardwareAddr) (*virtio.Net, *netstack.NetStack, bool) {
	return newBSDManagedNet(netdev, stack, guestIPv4, mac, true, 0, true)
}

type bsdManagedNetBackend struct {
	iface *netstack.NetworkInterface
}

func (b bsdManagedNetBackend) HandleTxPacket(packet []byte) error {
	return b.iface.DeliverGuestPacket(packet, true)
}

func newBSDManagedNet(netdev *virtio.Net, stack *netstack.NetStack, guestIPv4 net.IP, mac net.HardwareAddr, disableMergeRX bool, headerLength int, legacyMMIO bool) (*virtio.Net, *netstack.NetStack, bool) {
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
			_ = netdev.EnqueueRxPacketOwned(copied)
		}()
		return nil
	})
	return netdev, stack, true
}

func setupOpenBSDBootState(vm *VM, plan *openbsdarm64.BootPlan) error {
	if err := vm.SetReg(hvRegPC, plan.EntryGPA); err != nil {
		return fmt.Errorf("set PC: %w", err)
	}
	if err := vm.SetReg(hvRegCPSR, arm64vm.DefaultPStateBits); err != nil {
		return fmt.Errorf("set CPSR: %w", err)
	}
	if err := vm.SetReg(hvRegX0, plan.KernelEndGPA); err != nil {
		return fmt.Errorf("set X0: %w", err)
	}
	if err := vm.SetReg(hvRegX1, 0); err != nil {
		return fmt.Errorf("set X1: %w", err)
	}
	if err := vm.SetReg(hvRegX2, plan.DeviceTreeGPA); err != nil {
		return fmt.Errorf("set X2: %w", err)
	}
	return nil
}

func setupFreeBSDBootState(vm *VM, plan *freebsdarm64.BootPlan) error {
	if err := vm.SetReg(hvRegPC, plan.EntryGPA); err != nil {
		return fmt.Errorf("set PC: %w", err)
	}
	if err := vm.SetReg(hvRegCPSR, arm64vm.DefaultPStateBits); err != nil {
		return fmt.Errorf("set CPSR: %w", err)
	}
	if err := vm.SetReg(hvRegX0, plan.DeviceTreeGPA); err != nil {
		return fmt.Errorf("set X0: %w", err)
	}
	for _, reg := range []Reg{hvRegX1, hvRegX2, hvRegX3} {
		if err := vm.SetReg(reg, 0); err != nil {
			return fmt.Errorf("clear reg %d: %w", reg, err)
		}
	}
	return nil
}

func setupNetBSDBootState(vm *VM, plan *netbsdarm64.BootPlan) error {
	if err := vm.SetReg(hvRegPC, plan.EntryGPA); err != nil {
		return fmt.Errorf("set PC: %w", err)
	}
	if err := vm.SetReg(hvRegCPSR, 0x40000000|arm64vm.DefaultPStateBits); err != nil {
		return fmt.Errorf("set CPSR: %w", err)
	}
	if err := vm.SetReg(hvRegX0, plan.DeviceTreeGPA); err != nil {
		return fmt.Errorf("set X0: %w", err)
	}
	for _, reg := range []Reg{hvRegX1, hvRegX2, hvRegX3} {
		if err := vm.SetReg(reg, 0); err != nil {
			return fmt.Errorf("clear reg %d: %w", reg, err)
		}
	}
	return nil
}

func runBSDManagedVM(ctx context.Context, vm *VM, guestName string, uart *serial.UART8250, block *virtio.Block, pci *hvfPCIHost, netdev *virtio.Net, rng *virtio.RNG, serialOut *serialTranscript) error {
	cancelDone := make(chan struct{})
	defer close(cancelDone)
	go func() {
		select {
		case <-ctx.Done():
			_ = vm.CancelRun()
		case <-cancelDone:
		}
	}()

	go answerBSDPrompts(ctx, guestName, uart, serialOut, pci != nil)

	runner := newVMRunManager(vm)
	for {
		if err := ctx.Err(); err != nil {
			pc, _ := vm.GetProgramCounter()
			return fmt.Errorf("%w at pc=%#x", err, pc)
		}
		runRes, err, stalled := runner.Run(ctx, persistentRunSlice(false, false))
		if stalled {
			continue
		}
		if err != nil {
			return fmt.Errorf("run %s VM: %w", guestName, err)
		}
		if runRes == nil || runRes.exit == nil {
			return fmt.Errorf("%s vcpu returned nil exit info", guestName)
		}
		exitInfo := runRes.exit
		vcpuIndex := runRes.index
		if exitInfo.Reason == hvExitReasonVTimerActivated {
			if err := injectVirtualTimerPPI(vm, vcpuIndex); err != nil {
				return fmt.Errorf("inject virtual timer ppi: %w", err)
			}
			continue
		}
		if exitInfo.Reason == hvExitReasonCanceled {
			continue
		}
		if exitInfo.Reason != hvExitReasonException {
			return fmt.Errorf("unexpected exit reason %v\nserial:\n%s", exitInfo.Reason, serialOut.String())
		}
		switch DecodeExceptionClass(exitInfo.Exception.Syndrome) {
		case ExceptionClassDataAbortLowerEL:
			if err := handleContainerDataAbort(ctx, vm, vcpuIndex, uart, nil, rng, nil, nil, netdev, exitInfo); err != nil {
				addr := uint64(exitInfo.Exception.PhysicalAddress)
				if pci != nil && pci.Contains(addr, 1) {
					if err := handleBSDPCIDataAbort(vm, vcpuIndex, pci, exitInfo); err != nil {
						return err
					}
					continue
				}
				if !block.Contains(addr, 1) {
					return err
				}
				if err := handleBSDBlockDataAbort(vm, vcpuIndex, block, exitInfo); err != nil {
					return err
				}
			}
		case ExceptionClassSystemRegister:
			handled, err := vm.HandleSystemInstructionForVCPU(vcpuIndex, exitInfo.Exception.Syndrome)
			if err != nil {
				return err
			}
			if !handled {
				pc, _ := vm.GetProgramCounterForVCPU(vcpuIndex)
				info, _ := DecodeSystemInstruction(exitInfo.Exception.Syndrome)
				return fmt.Errorf("unsupported system instruction trap pc=%#x syndrome=%#x op0=%d op1=%d op2=%d crn=%d crm=%d rt=%d read=%t\nserial:\n%s",
					pc, exitInfo.Exception.Syndrome, info.Op0, info.Op1, info.Op2, info.CRn, info.CRm, info.RawRt, info.Read, serialOut.String())
			}
		case ExceptionClassHVC64:
			halt, err := handleContainerHVC(vm, vcpuIndex)
			if err != nil {
				return err
			}
			if halt {
				return fmt.Errorf("%s guest halted while instance was running\nserial:\n%s", guestName, serialOut.String())
			}
		default:
			pc, _ := vm.GetProgramCounterForVCPU(vcpuIndex)
			return fmt.Errorf("unexpected exception class %#x pc=%#x syndrome=%#x physical=%#x\nserial:\n%s",
				DecodeExceptionClass(exitInfo.Exception.Syndrome), pc, exitInfo.Exception.Syndrome, uint64(exitInfo.Exception.PhysicalAddress), serialOut.String())
		}
	}
}

func bsdRootBlockNodes(block *virtio.Block, pci *hvfPCIHost) []fdt.Node {
	if pci != nil {
		return []fdt.Node{pci.DeviceTreeNode()}
	}
	return []fdt.Node{block.DeviceTreeNode()}
}

func darwinArm64NVMeRootEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("CC_DARWIN_ARM64_NVME_ROOT"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func handleBSDPCIDataAbort(vm *VM, vcpuIndex int, pci *hvfPCIHost, exitInfo *VcpuExit) error {
	info, err := DecodeDataAbort(exitInfo.Exception.Syndrome)
	if err != nil {
		return err
	}
	addr := uint64(exitInfo.Exception.PhysicalAddress)
	if !pci.Contains(addr, info.SizeBytes) {
		return fmt.Errorf("unhandled PCI MMIO access addr=%#x size=%d write=%v", addr, info.SizeBytes, info.Write)
	}
	if info.Write {
		value, err := readAbortValue(vm, vcpuIndex, info)
		if err != nil {
			return err
		}
		if err := pci.Write(addr, info.SizeBytes, value); err != nil {
			return err
		}
	} else {
		value, err := pci.Read(addr, info.SizeBytes)
		if err != nil {
			return err
		}
		if err := writeAbortValue(vm, vcpuIndex, info, value); err != nil {
			return err
		}
	}
	return vm.AdvanceProgramCounterForVCPU(vcpuIndex)
}

func handleBSDBlockDataAbort(vm *VM, vcpuIndex int, block *virtio.Block, exitInfo *VcpuExit) error {
	info, err := DecodeDataAbort(exitInfo.Exception.Syndrome)
	if err != nil {
		return err
	}
	addr := uint64(exitInfo.Exception.PhysicalAddress)
	if !block.Contains(addr, info.SizeBytes) {
		return fmt.Errorf("unhandled MMIO access addr=%#x size=%d write=%v", addr, info.SizeBytes, info.Write)
	}
	if info.Write {
		value, err := readAbortValue(vm, vcpuIndex, info)
		if err != nil {
			return err
		}
		if err := block.Write(addr, info.SizeBytes, value); err != nil {
			return err
		}
	} else {
		value, err := block.Read(addr, info.SizeBytes)
		if err != nil {
			return err
		}
		if err := writeAbortValue(vm, vcpuIndex, info, value); err != nil {
			return err
		}
	}
	return vm.AdvanceProgramCounterForVCPU(vcpuIndex)
}

func answerBSDPrompts(ctx context.Context, guestName string, uart *serial.UART8250, serialOut *serialTranscript, nvmeRoot bool) {
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
			serial := serialOut.String()
			if strings.EqualFold(guestName, "OpenBSD") {
				if !answeredRoot.Load() && strings.Contains(serial, "root device:") {
					answeredRoot.Store(true)
					_ = uart.InjectRXBytes([]byte("sd0a\n"))
				}
				if answeredRoot.Load() && !answeredSwap.Load() && strings.Contains(serial, "swap device") {
					answeredSwap.Store(true)
					_ = uart.InjectRXBytes([]byte("\n"))
				}
				continue
			}
			if strings.EqualFold(guestName, "NetBSD") {
				if !answeredRoot.Load() && strings.Contains(serial, "root device:") {
					answeredRoot.Store(true)
					_ = uart.InjectRXBytes([]byte("ld4a\n"))
				}
				continue
			}
			if !answeredMountroot.Load() && strings.Contains(serial, "mountroot>") {
				answeredMountroot.Store(true)
				root := "ufs:/dev/vtbd0\n"
				if nvmeRoot {
					root = "ufs:/dev/nda0\n"
				}
				_ = uart.InjectRXBytes([]byte(root))
			}
		}
	}
}
