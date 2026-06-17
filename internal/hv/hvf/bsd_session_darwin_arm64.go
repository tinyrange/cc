//go:build darwin && arm64

package hvf

import (
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"time"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/arm64vm"
	"j5.nz/cc/internal/fdt"
	freebsdarm64 "j5.nz/cc/internal/freebsd/boot/arm64"
	"j5.nz/cc/internal/netstack"
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

type bsdManagedConfig struct {
	GuestName           string
	Kernel              []byte
	Root                virtio.BlockBackend
	MemoryMB            uint64
	Dmesg               bool
	NetDevice           *virtio.Net
	NetStack            *netstack.NetStack
	PrepareOpenBSD      bool
	FreeBSDVirtioQuirks bool
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
	if cfg.NetDevice == nil || cfg.NetStack == nil {
		return nil, fmt.Errorf("OpenBSD network device and stack are required")
	}
	return startBSDManagedSession(ctx, bsdManagedConfig{
		GuestName:      "OpenBSD",
		Kernel:         cfg.Kernel,
		Root:           cfg.Root,
		MemoryMB:       cfg.MemoryMB,
		Dmesg:          cfg.Dmesg,
		NetDevice:      cfg.NetDevice,
		NetStack:       cfg.NetStack,
		PrepareOpenBSD: true,
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
	if cfg.NetDevice == nil || cfg.NetStack == nil {
		return nil, fmt.Errorf("FreeBSD network device and stack are required")
	}
	return startBSDManagedSession(ctx, bsdManagedConfig{
		GuestName:           "FreeBSD",
		Kernel:              cfg.Kernel,
		Root:                cfg.Root,
		MemoryMB:            cfg.MemoryMB,
		Dmesg:               cfg.Dmesg,
		NetDevice:           cfg.NetDevice,
		NetStack:            cfg.NetStack,
		FreeBSDVirtioQuirks: true,
	}, onEvent)
}

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
	mem, err := vm.MapAnonymousMemory(uintptr(memorySize), IPA(arm64vm.MemoryBase), hvMemoryRead|hvMemoryWrite|hvMemoryExec)
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
	block.Attach(vm, vm)
	if cfg.FreeBSDVirtioQuirks {
		cfg.NetDevice.DisableMergeRX = true
		cfg.NetDevice.HeaderLength = 12
	}
	cfg.NetDevice.Attach(vm, vm)
	rng := virtio.NewRNG(arm64vm.RNGBase, arm64vm.RNGSize, arm64vm.RNGIRQ)
	rng.Attach(vm, vm)

	if cfg.PrepareOpenBSD {
		plan, err := openbsdarm64.PrepareBoot(mem, cfg.Kernel, openbsdarm64.BootOptions{
			MemoryBase: arm64vm.MemoryBase,
			MemorySize: memorySize,
			NumCPUs:    1,
			GICVersion: openbsdarm64.GICVersionV3,
			Console:    true,
			ExtraNodes: []fdt.Node{block.DeviceTreeNode(), cfg.NetDevice.DeviceTreeNode(), rng.DeviceTreeNode()},
		})
		if err != nil {
			cleanupStartup()
			return nil, fmt.Errorf("prepare OpenBSD boot: %w", err)
		}
		if err := setupOpenBSDBootState(vm, plan); err != nil {
			cleanupStartup()
			return nil, err
		}
	} else {
		plan, err := freebsdarm64.PrepareBoot(mem, cfg.Kernel, freebsdarm64.BootOptions{
			MemoryBase: arm64vm.MemoryBase,
			MemorySize: memorySize,
			NumCPUs:    1,
			GICVersion: freebsdarm64.GICVersionV3,
			Console:    true,
			ExtraNodes: []fdt.Node{block.DeviceTreeNode(), cfg.NetDevice.DeviceTreeNode(), rng.DeviceTreeNode()},
		})
		if err != nil {
			cleanupStartup()
			return nil, fmt.Errorf("prepare FreeBSD boot: %w", err)
		}
		if err := setupFreeBSDBootState(vm, plan); err != nil {
			cleanupStartup()
			return nil, err
		}
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
		done.finish(runBSDManagedVM(runCtx, vmForRun, cfg.GuestName, uart, block, cfg.NetDevice, rng, serialOut))
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
		dmesg:      cfg.Dmesg,
	}, nil
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

func runBSDManagedVM(ctx context.Context, vm *VM, guestName string, uart *serial.UART8250, block *virtio.Block, netdev *virtio.Net, rng *virtio.RNG, serialOut *serialTranscript) error {
	cancelDone := make(chan struct{})
	defer close(cancelDone)
	go func() {
		select {
		case <-ctx.Done():
			_ = vm.CancelRun()
		case <-cancelDone:
		}
	}()

	go answerBSDPrompts(ctx, guestName, uart, serialOut)

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
				if !block.Contains(uint64(exitInfo.Exception.PhysicalAddress), 1) {
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

func answerBSDPrompts(ctx context.Context, guestName string, uart *serial.UART8250, serialOut *serialTranscript) {
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
			if !answeredMountroot.Load() && strings.Contains(serial, "mountroot>") {
				answeredMountroot.Store(true)
				_ = uart.InjectRXBytes([]byte("ufs:/dev/vtbd0\n"))
			}
		}
	}
}
