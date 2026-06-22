//go:build windows && amd64

package whp

import (
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"time"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/amd64vm"
	freebsdamd64 "j5.nz/cc/internal/freebsd/boot/amd64"
	"j5.nz/cc/internal/managed/machine"
	netbsdamd64 "j5.nz/cc/internal/netbsd/boot/amd64"
	"j5.nz/cc/internal/netstack"
	openbsdamd64 "j5.nz/cc/internal/openbsd/boot/amd64"
	"j5.nz/cc/internal/serial"
	"j5.nz/cc/internal/virtio"
	"j5.nz/cc/internal/vmruntime"
)

const bsdControlPort = 10777

var processStart = time.Now()

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

type NetBSDManagedConfig = FreeBSDManagedConfig

type bsdPCSessionConfig struct {
	Spec        machine.Spec
	GuestName   string
	Kernel      []byte
	Root        virtio.BlockBackend
	ExtraBlocks []virtio.BlockBackend
	MemoryMB    uint64
	Dmesg       bool
	NetDevice   *virtio.Net
	NetStack    *netstack.NetStack
	NetPCIDev   uint8
	NetIOBase   uint16
	NetIRQ      uint8
	BlockQuirks bsdPCBlockQuirks
	Prepare     func(vm *VM, mem []byte) error
	Input       func(context.Context, *serial.UART8250, *vmruntime.SerialTranscript)
}

type bsdPCBlockQuirks struct {
	DisableSizeMax bool
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
	return startBSDPCManagedSession(ctx, bsdPCSessionConfig{
		Spec: machine.Spec{
			Guest:    "OpenBSD",
			Arch:     "amd64",
			MemoryMB: cfg.MemoryMB,
			Dmesg:    cfg.Dmesg,
			Control:  machine.ControlSpec{Kind: "tcp", Port: bsdControlPort},
			Boot:     machine.BootSpec{Kind: "openbsd"},
			Network:  &machine.NetworkSpec{GuestIPv4: cfg.GuestIPv4.String(), MAC: cfg.GuestMAC.String()},
			Devices: []machine.DeviceSpec{
				{Kind: "virtio-block", Name: "root", Bus: "pci", Slot: 1, IOBase: 0x1000, IRQ: 10},
				{Kind: "virtio-net", Name: "net0", Bus: "pci", Slot: 2, IOBase: 0x1100, IRQ: 11},
			},
		},
		GuestName: "OpenBSD",
		Kernel:    cfg.Kernel,
		Root:      cfg.Root,
		MemoryMB:  cfg.MemoryMB,
		Dmesg:     cfg.Dmesg,
		NetDevice: cfg.NetDevice,
		NetStack:  cfg.NetStack,
		Prepare: func(vm *VM, mem []byte) error {
			plan, err := openbsdamd64.PrepareBoot(mem, cfg.Kernel, openbsdamd64.BootOptions{
				MemorySize: amd64vm.MemorySizeBytes(cfg.MemoryMB),
				BootDev:    openbsdamd64.SCSIBootDev(0, 0),
			})
			if err != nil {
				return fmt.Errorf("prepare OpenBSD boot: %w", err)
			}
			return vm.SetProtectedMode32(plan.EntryGPA, plan.StackGPA)
		},
		Input: answerOpenBSDPrompts,
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
	return startBSDPCManagedSession(ctx, freeNetBSDSessionConfig("FreeBSD", "freebsd", cfg, bsdPCBlockQuirks{DisableSizeMax: true}, func(vm *VM, mem []byte) error {
		plan, err := freebsdamd64.PrepareBoot(mem, cfg.Kernel, freebsdamd64.BootOptions{
			MemorySize: amd64vm.MemorySizeBytes(cfg.MemoryMB),
		})
		if err != nil {
			return fmt.Errorf("prepare FreeBSD boot: %w", err)
		}
		return vm.SetFreeBSDLongMode(plan.EntryGVA, plan.StackGPA, plan.PagingGPA)
	}), onEvent)
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
	return startBSDPCManagedSession(ctx, freeNetBSDSessionConfig("NetBSD", "netbsd", cfg, bsdPCBlockQuirks{}, func(vm *VM, mem []byte) error {
		plan, err := netbsdamd64.PrepareBoot(mem, cfg.Kernel, netbsdamd64.BootOptions{
			MemorySize: amd64vm.MemorySizeBytes(cfg.MemoryMB),
		})
		if err != nil {
			return fmt.Errorf("prepare NetBSD boot: %w", err)
		}
		return vm.SetProtectedMode32(plan.EntryGPA, plan.StackGPA)
	}), onEvent)
}

func freeNetBSDSessionConfig(name, boot string, cfg FreeBSDManagedConfig, quirks bsdPCBlockQuirks, prepare func(*VM, []byte) error) bsdPCSessionConfig {
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
	return bsdPCSessionConfig{
		Spec: machine.Spec{
			Guest:    name,
			Arch:     "amd64",
			MemoryMB: cfg.MemoryMB,
			Dmesg:    cfg.Dmesg,
			Control:  machine.ControlSpec{Kind: "tcp", Port: bsdControlPort},
			Boot:     machine.BootSpec{Kind: boot},
			Network:  &machine.NetworkSpec{GuestIPv4: cfg.GuestIPv4.String(), MAC: cfg.GuestMAC.String()},
			Devices:  devices,
		},
		GuestName:   name,
		Kernel:      cfg.Kernel,
		Root:        cfg.Root,
		ExtraBlocks: cfg.ExtraBlocks,
		MemoryMB:    cfg.MemoryMB,
		Dmesg:       cfg.Dmesg,
		NetDevice:   cfg.NetDevice,
		NetStack:    cfg.NetStack,
		BlockQuirks: quirks,
		Prepare:     prepare,
	}
}

func startBSDPCManagedSession(ctx context.Context, cfg bsdPCSessionConfig, onEvent func(client.BootEvent) error) (*ManagedSession, error) {
	cfg = normalizeBSDPCSessionConfig(cfg)
	if cfg.NetDevice == nil || cfg.NetStack == nil {
		return nil, fmt.Errorf("%s network device and stack are required", cfg.GuestName)
	}
	if cfg.Prepare == nil {
		return nil, fmt.Errorf("%s boot prepare hook is required", cfg.GuestName)
	}
	if err := emitManagedBootStatus(onEvent, "starting VM"); err != nil {
		return nil, err
	}
	ln, err := cfg.NetStack.ListenInternal("tcp", fmt.Sprintf(":%d", bsdSessionControlPort(cfg)))
	if err != nil {
		return nil, fmt.Errorf("listen %s control tcp: %w", cfg.GuestName, err)
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

	vm, err := newBootVM(amd64vm.MemorySizeBytes(cfg.MemoryMB))
	if err != nil {
		_ = ln.Close()
		return nil, err
	}
	var bootWriter *vmruntime.BootEventWriter
	serialOut := vmruntime.NewSerialTranscript()
	var serialWriter io.Writer = serialOut
	if onEvent != nil {
		bootWriter = vmruntime.NewBootEventWriter(onEvent)
		serialWriter = io.MultiWriter(serialOut, bootWriter)
	}
	uart := serial.NewUART8250(amd64vm.COM1Base, 0, serialWriter)
	platform := newBootPlatform(vm, uart)
	platform.AttachPCDevices(NewI8042(), NewCMOSRTC(nil), NewACPIPM())
	pci := attachBSDPCDevices(platform, cfg.Root, cfg.ExtraBlocks, cfg.NetDevice, cfg.NetPCIDev, cfg.NetIOBase, cfg.NetIRQ, cfg.BlockQuirks)
	platform.AttachPCI(pci)
	if err := cfg.Prepare(vm, vm.Memory()); err != nil {
		platform.Close()
		_ = vm.Close()
		_ = ln.Close()
		if bootWriter != nil {
			_ = bootWriter.Close()
		}
		return nil, err
	}
	if err := vm.EnableEmulation(platform); err != nil {
		platform.Close()
		_ = vm.Close()
		_ = ln.Close()
		if bootWriter != nil {
			_ = bootWriter.Close()
		}
		return nil, fmt.Errorf("enable emulation: %w", err)
	}

	runCtx, cancel := context.WithCancel(context.Background())
	doneCh := make(chan error, 1)
	if cfg.Input != nil {
		go cfg.Input(runCtx, uart, serialOut)
	}
	go func() {
		defer vm.Close()
		defer platform.Close()
		doneCh <- runBSDManagedVM(runCtx, cfg.GuestName, vm, platform, serialOut)
	}()

	var control net.Conn
	select {
	case err := <-acceptErrCh:
		cancel()
		_ = ln.Close()
		if bootWriter != nil {
			_ = bootWriter.Close()
		}
		return nil, bsdStartupError(err, serialOut.String(), controlTranscript.String())
	case conn := <-connCh:
		control = conn
	case err := <-doneCh:
		cancel()
		_ = ln.Close()
		if bootWriter != nil {
			_ = bootWriter.Close()
		}
		return nil, bsdStartupError(err, serialOut.String(), controlTranscript.String())
	case <-ctx.Done():
		cancel()
		_ = ln.Close()
		if bootWriter != nil {
			_ = bootWriter.Close()
		}
		return nil, bsdStartupError(fmt.Errorf("%s guest did not connect to control TCP port %d before startup deadline: %w (%s)", cfg.GuestName, bsdSessionControlPort(cfg), ctx.Err(), platform.Summary()), serialOut.String(), controlTranscript.String())
	}

	if _, err := controlTranscript.WaitFor(ctx, 0, func(text string) bool {
		return strings.Contains(text, vmruntime.InstanceReadyMarker) || vmruntime.HasFatalBootText(text)
	}); err != nil {
		cancel()
		_ = control.Close()
		_ = ln.Close()
		if bootWriter != nil {
			_ = bootWriter.Close()
		}
		return nil, bsdStartupError(fmt.Errorf("%s control connection did not report ready marker %q: %w", cfg.GuestName, vmruntime.InstanceReadyMarker, err), serialOut.String(), controlTranscript.String())
	}
	if vmruntime.HasFatalBootText(controlTranscript.String()) {
		cancel()
		_ = control.Close()
		_ = ln.Close()
		if bootWriter != nil {
			_ = bootWriter.Close()
		}
		return nil, bsdStartupError(fmt.Errorf("%s guest reported boot failure", cfg.GuestName), serialOut.String(), controlTranscript.String())
	}
	if err := emitManagedBootStatus(onEvent, "guest ready"); err != nil {
		cancel()
		_ = control.Close()
		_ = ln.Close()
		if bootWriter != nil {
			_ = bootWriter.Close()
		}
		return nil, err
	}

	return &ManagedSession{
		cancel:     cancel,
		doneCh:     doneCh,
		control:    control,
		listener:   ln,
		bootWriter: bootWriter,
		transcript: controlTranscript,
		serialOut:  serialOut,
		platform:   platform,
		dmesg:      cfg.Dmesg,
	}, nil
}

func normalizeBSDPCSessionConfig(cfg bsdPCSessionConfig) bsdPCSessionConfig {
	if cfg.GuestName == "" {
		cfg.GuestName = cfg.Spec.Guest
	}
	if cfg.MemoryMB == 0 {
		cfg.MemoryMB = cfg.Spec.MemoryMB
	}
	if !cfg.Dmesg {
		cfg.Dmesg = cfg.Spec.Dmesg
	}
	for _, dev := range cfg.Spec.Devices {
		if dev.Kind != "virtio-net" && dev.Name != "net0" {
			continue
		}
		if cfg.NetPCIDev == 0 {
			cfg.NetPCIDev = dev.Slot
		}
		if cfg.NetIOBase == 0 {
			cfg.NetIOBase = dev.IOBase
		}
		if cfg.NetIRQ == 0 {
			cfg.NetIRQ = dev.IRQ
		}
	}
	return cfg
}

func bsdSessionControlPort(cfg bsdPCSessionConfig) int {
	if cfg.Spec.Control.Port > 0 {
		return cfg.Spec.Control.Port
	}
	return bsdControlPort
}

func attachBSDPCDevices(platform *bootPlatform, root virtio.BlockBackend, extraBlocks []virtio.BlockBackend, netdev *virtio.Net, netPCIDev uint8, netIOBase uint16, netIRQ uint8, blockQuirks bsdPCBlockQuirks) *PCIBus {
	var pciDevices []*PCIDevice
	block := virtio.NewBlock(0, 0x1000, 10, root)
	block.DisableSizeMax = blockQuirks.DisableSizeMax
	block.Attach(platform.vm, platform)
	pciDevices = append(pciDevices, NewVirtioBlockPCIDevice(1, 0x1000, 10, block))
	for i, backend := range extraBlocks {
		if backend == nil {
			continue
		}
		dev := uint8(2 + i)
		ioBase := uint16(0x1100 + i*0x100)
		irq := uint8(11 + i)
		extraBlock := virtio.NewBlock(0, 0x1000, uint32(irq), backend)
		extraBlock.DisableSizeMax = blockQuirks.DisableSizeMax
		extraBlock.Attach(platform.vm, platform)
		pciDevices = append(pciDevices, NewVirtioBlockPCIDevice(dev, ioBase, irq, extraBlock))
	}
	netIndex := len(pciDevices) + 1
	if netPCIDev == 0 {
		netPCIDev = uint8(netIndex)
	}
	if netIOBase == 0 {
		netIOBase = uint16(0x1000 + netIndex*0x100)
	}
	if netIRQ == 0 {
		netIRQ = uint8(10 + netIndex)
	}
	netdev.IRQ = uint32(netIRQ)
	platform.AttachNet(netdev)
	pciDevices = append(pciDevices, NewVirtioNetPCIDevice(netPCIDev, netIOBase, netIRQ, netdev))
	return NewPCIBus(pciDevices...)
}

func answerOpenBSDPrompts(ctx context.Context, uart *serial.UART8250, serialOut *vmruntime.SerialTranscript) {
	var answeredRoot atomic.Bool
	var answeredSwap atomic.Bool
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
}

func runBSDManagedVM(ctx context.Context, guestName string, vm *VM, platform *bootPlatform, serialOut *vmruntime.SerialTranscript) error {
	for step := 0; ; step++ {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("%w (%s)", err, platform.Summary())
		}
		if err := platform.armPendingIRQWindow(); err != nil {
			return fmt.Errorf("arm pending irq window: %w", err)
		}
		var raw runVPExitContext
		exit, err := vm.runWithCancel(ctx, &raw)
		if err != nil {
			if ctx.Err() != nil {
				return fmt.Errorf("%w (%s)", ctx.Err(), platform.Summary())
			}
			return fmt.Errorf("run step %d: %w", step, err)
		}
		platform.recordExit(exit, &raw)
		switch exit.Reason {
		case runVPExitReasonX64IoPortAccess:
			if err := vm.emulateIO(&raw); err != nil {
				io := raw.ioPortAccess()
				return fmt.Errorf("emulate io at rip=%#x port=%#x: %w\nserial:\n%s", exit.RIP, io.Port, err, serialOut.String())
			}
		case runVPExitReasonMemoryAccess:
			if err := vm.emulateMMIO(&raw); err != nil {
				if handled, skipErr := skipNoopMMIOCacheFlush(vm, exit, raw.memoryAccess()); handled {
					if skipErr != nil {
						return skipErr
					}
					break
				}
				mem := raw.memoryAccess()
				return fmt.Errorf("emulate mmio at rip=%#x gpa=%#x gva=%#x access=%d insn_len=%d insn=% x: %w\nserial:\n%s", exit.RIP, uint64(mem.GPA), mem.GVA, mem.AccessInfo.accessType(), mem.InstructionByteCount, mem.InstructionBytes[:mem.InstructionByteCount], err, serialOut.String())
			}
		case runVPExitReasonX64Halt:
			if !platform.hasPendingIRQ() {
				return fmt.Errorf("%s guest halted\nserial:\n%s\n%s", guestName, serialOut.String(), platform.Summary())
			}
		case runVPExitReasonX64ApicEoi:
			platform.HandleEOI(raw.apicEoi().InterruptVector)
		case runVPExitReasonX64MsrAccess:
			if err := handleMSRAccess(vm, exit, &raw); err != nil {
				return fmt.Errorf("handle msr at rip=%#x: %w\nserial:\n%s", exit.RIP, err, serialOut.String())
			}
		case runVPExitReasonX64InterruptWindow:
		case runVPExitReasonCanceled:
		default:
			return fmt.Errorf("unexpected exit %s at rip=%#x\nserial:\n%s\n%s", exit.Reason, exit.RIP, serialOut.String(), platform.Summary())
		}
		if flushed, err := platform.flushPendingIRQ(&raw); err != nil {
			return fmt.Errorf("flush pending irq after %s at rip=%#x: %w", exit.Reason, exit.RIP, err)
		} else if exit.Reason == runVPExitReasonX64Halt && !flushed {
			return fmt.Errorf("%s guest halted with pending irq blocked\nserial:\n%s\n%s", guestName, serialOut.String(), platform.Summary())
		}
	}
}

func handleMSRAccess(vm *VM, exit Exit, raw *runVPExitContext) error {
	msr := raw.msrAccess()
	nextRIP := exit.RIP + uint64(raw.instructionLength())
	if raw.instructionLength() == 0 {
		nextRIP = exit.RIP + 2
	}
	if msr.AccessInfo.isWrite() {
		return vm.SetRIP(nextRIP)
	}
	value := readMSR(msr.MSRNumber)
	return vm.SetRegisters(map[registerName]uint64{
		registerRax: value & 0xffffffff,
		registerRdx: value >> 32,
		registerRip: nextRIP,
	})
}

func readMSR(msr uint32) uint64 {
	switch msr {
	case 0x10:
		return uint64(time.Since(processStart).Nanoseconds())
	case 0xce:
		return 0
	case 0xe7, 0xe8:
		return 0
	case 0x1a0:
		return 0
	default:
		return 0
	}
}

func skipNoopMMIOCacheFlush(vm *VM, exit Exit, mem *memoryAccessContext) (bool, error) {
	if mem == nil || mem.InstructionByteCount < 3 {
		return false, nil
	}
	insn := mem.InstructionBytes[:mem.InstructionByteCount]
	i := 0
	for i < len(insn) {
		switch insn[i] {
		case 0x66, 0xf2, 0xf3:
			i++
		case 0x40, 0x41, 0x42, 0x43, 0x44, 0x45, 0x46, 0x47, 0x48, 0x49, 0x4a, 0x4b, 0x4c, 0x4d, 0x4e, 0x4f:
			i++
		default:
			goto prefixesDone
		}
	}
prefixesDone:
	if i+2 > len(insn) || insn[i] != 0x0f || insn[i+1] != 0xae {
		return false, nil
	}
	if i+2 >= len(insn) {
		return false, nil
	}
	modrm := insn[i+2]
	reg := (modrm >> 3) & 0x7
	if reg != 7 {
		return false, nil
	}
	length := i + 3
	mod := modrm >> 6
	rm := modrm & 0x7
	if mod != 3 && rm == 4 {
		if length >= len(insn) {
			return false, nil
		}
		sib := insn[length]
		length++
		base := sib & 0x7
		if mod == 0 && base == 5 {
			length += 4
		}
	} else if mod == 0 && rm == 5 {
		length += 4
	}
	switch mod {
	case 1:
		length++
	case 2:
		length += 4
	}
	if length <= 0 || length > int(mem.InstructionByteCount) {
		return false, nil
	}
	if err := vm.SetRIP(exit.RIP + uint64(length)); err != nil {
		return true, fmt.Errorf("skip MMIO cache flush at rip=%#x: %w", exit.RIP, err)
	}
	return true, nil
}

func bsdStartupError(err error, serialText, controlText string) error {
	return transcriptError(err, serialText, controlText)
}
