//go:build linux && amd64

package kvm

import (
	"context"
	"fmt"
	"io"
	"net"
	"strings"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/amd64vm"
	"j5.nz/cc/internal/managed/machine"
	"j5.nz/cc/internal/netstack"
	"j5.nz/cc/internal/nvme"
	"j5.nz/cc/internal/serial"
	"j5.nz/cc/internal/virtio"
	"j5.nz/cc/internal/vmruntime"
)

const bsdControlPort = 10777

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
	OwnNetStack bool
	NetPCIDev   uint8
	NetIOBase   uint16
	NetIRQ      uint8
	Prepare     func(vm *VM, mem []byte) error
	Run         func(ctx context.Context, vm *VM, uart *serial.UART8250, pci *PCIBus, serialOut *vmruntime.SerialTranscript) error
}

func startBSDPCManagedSession(ctx context.Context, cfg bsdPCSessionConfig, onEvent func(client.BootEvent) error) (*ManagedSession, error) {
	cfg = normalizeBSDPCSessionConfig(cfg)
	if cfg.NetDevice == nil || cfg.NetStack == nil {
		return nil, fmt.Errorf("%s network device and stack are required", cfg.GuestName)
	}
	if cfg.Prepare == nil {
		return nil, fmt.Errorf("%s boot prepare hook is required", cfg.GuestName)
	}
	if cfg.Run == nil {
		return nil, fmt.Errorf("%s run hook is required", cfg.GuestName)
	}
	controlPort := bsdSessionControlPort(cfg)
	ln, err := cfg.NetStack.ListenInternal("tcp", fmt.Sprintf(":%d", controlPort))
	if err != nil {
		if cfg.OwnNetStack {
			cfg.NetStack.Close()
		}
		return nil, fmt.Errorf("listen %s control tcp: %w", cfg.GuestName, err)
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

	pci := attachBSDPCDevices(kvmVM, cfg.Root, cfg.ExtraBlocks, cfg.NetDevice, cfg.NetPCIDev, cfg.NetIOBase, cfg.NetIRQ)

	if err := cfg.Prepare(kvmVM, mem); err != nil {
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
		done.finish(cfg.Run(runCtx, vmForRun, uart, pci, serialOut))
	}()

	var control net.Conn
	select {
	case err := <-acceptErrCh:
		cleanupStartup()
		return nil, bsdStartupError(err, serialOut.String(), controlTranscript.String())
	case conn := <-connCh:
		control = conn
	case <-done.done():
		err := done.result()
		cleanupStartup()
		return nil, bsdStartupError(err, serialOut.String(), controlTranscript.String())
	case <-ctx.Done():
		cleanupStartup()
		err := fmt.Errorf("%s guest did not connect to control TCP port %d before startup deadline: %w", cfg.GuestName, controlPort, ctx.Err())
		return nil, bsdStartupError(err, serialOut.String(), controlTranscript.String())
	}

	if _, err := controlTranscript.WaitFor(ctx, 0, func(text string) bool {
		return strings.Contains(text, vmruntime.InstanceReadyMarker) || vmruntime.HasFatalBootText(text)
	}); err != nil {
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

func attachBSDPCDevices(vm *VM, root virtio.BlockBackend, extraBlocks []virtio.BlockBackend, netdev *virtio.Net, netPCIDev uint8, netIOBase uint16, netIRQ uint8) *PCIBus {
	var pciDevices []*PCIDevice
	block := nvme.NewController(root)
	block.Attach(vm, vm)
	pciDevices = append(pciDevices, NewNVMePCIDevice(1, 0xfeb00000, 10, block))
	for i, backend := range extraBlocks {
		if backend == nil {
			continue
		}
		dev := uint8(2 + i)
		irq := uint8(11 + i)
		extraBlock := nvme.NewController(backend)
		extraBlock.Attach(vm, vm)
		pciDevices = append(pciDevices, NewNVMePCIDevice(dev, 0xfeb00000+uint64(i+1)*0x10000, irq, extraBlock))
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
	netdev.Attach(vm, vm)
	pciDevices = append(pciDevices, NewVirtioNetPCIDevice(netPCIDev, netIOBase, netIRQ, netdev))
	return NewPCIBus(pciDevices...)
}

func bsdStartupError(err error, serialText, controlText string) error {
	return transcriptError(err, serialText, controlText)
}
