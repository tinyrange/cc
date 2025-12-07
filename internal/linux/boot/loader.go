package boot

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime"
	"strings"

	"github.com/tinyrange/cc/internal/acpi"
	"github.com/tinyrange/cc/internal/asm/amd64"
	"github.com/tinyrange/cc/internal/asm/arm64"
	chipset "github.com/tinyrange/cc/internal/devices/amd64/chipset"
	amd64input "github.com/tinyrange/cc/internal/devices/amd64/input"
	"github.com/tinyrange/cc/internal/devices/amd64/pci"
	amd64serial "github.com/tinyrange/cc/internal/devices/amd64/serial"
	"github.com/tinyrange/cc/internal/devices/hpet"
	"github.com/tinyrange/cc/internal/devices/serial"
	"github.com/tinyrange/cc/internal/devices/virtio"
	"github.com/tinyrange/cc/internal/fdt"
	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/ir"
	_ "github.com/tinyrange/cc/internal/ir/amd64"
	amd64boot "github.com/tinyrange/cc/internal/linux/boot/amd64"
	arm64boot "github.com/tinyrange/cc/internal/linux/boot/arm64"
)

type bootPlan interface {
	ConfigureVCPU(vcpu hv.VirtualCPU) error
}

const (
	amd64ACPITablesSize = 0x10000
	amd64StackGuard     = 0x1000

	arm64UARTMMIOBase = 0x09000000
	arm64UARTRegShift = 0
	arm64UARTBaudRate = 115200

	hpetBaseAddress = 0xFED00000
)

const (
	armGICInterruptTypeSPI = 0
	armGICInterruptTypePPI = 1

	armKVMIRQTypeShift = 24
	armKVMIRQTypeSPI   = 1
	armKVMIRQTypePPI   = 2
)

var arm64UARTInterrupt = arm64boot.InterruptSpec{
	Type:  armGICInterruptTypeSPI,
	Num:   33, // Matches qemu-virt UART
	Flags: 0x4,
}

var arm64UARTIRQLine = armInterruptLine(arm64UARTInterrupt)

func armInterruptLine(spec arm64boot.InterruptSpec) uint32 {
	var irqType uint32
	switch spec.Type {
	case armGICInterruptTypeSPI:
		irqType = armKVMIRQTypeSPI
	case armGICInterruptTypePPI:
		irqType = armKVMIRQTypePPI
	default:
		panic(fmt.Sprintf("unsupported GIC interrupt type %d", spec.Type))
	}
	return (irqType << armKVMIRQTypeShift) | (spec.Num & 0xFFFF)
}

type programRunner struct {
	loader *LinuxLoader
	linux  io.ReaderAt
}

// Run implements hv.RunConfig.
func (p *programRunner) Run(ctx context.Context, vcpu hv.VirtualCPU) error {
	if err := p.loader.plan.ConfigureVCPU(vcpu); err != nil {
		return fmt.Errorf("configure vCPU: %w", err)
	}

	for {
		if err := vcpu.Run(ctx); err != nil {
			if errors.Is(err, hv.ErrVMHalted) {
				return nil
			}
			if errors.Is(err, hv.ErrGuestRequestedReboot) {
				return nil
			}
			return fmt.Errorf("run vCPU: %w", err)
		}
	}
}

var (
	_ hv.RunConfig = &programRunner{}
)

type convertCRLF struct {
	io.Writer
}

func (c *convertCRLF) Write(p []byte) (n int, err error) {
	var converted []byte
	for i := range p {
		if p[i] == '\n' {
			converted = append(converted, '\r')
		}
		converted = append(converted, p[i])
	}
	return c.Writer.Write(converted)
}

type LinuxLoader struct {
	NumCPUs int
	MemSize uint64
	MemBase uint64

	GetCmdline         func(arch hv.CpuArchitecture) ([]string, error)
	GetInit            func(arch hv.CpuArchitecture) (*ir.Program, error)
	GetKernel          func() (io.ReaderAt, int64, error)
	GetSystemMap       func() (io.ReaderAt, error)
	CreateVM           func(vm hv.VirtualMachine) error
	CreateVMWithMemory func(vm hv.VirtualMachine) error

	SerialStdout io.Writer

	Devices []hv.DeviceTemplate

	AdditionalFiles []InitFile

	plan         bootPlan
	kernelReader io.ReaderAt
}

func (l *LinuxLoader) ConfigureVCPU(vcpu hv.VirtualCPU) error {
	if l.plan == nil {
		return errors.New("linux loader not loaded")
	}

	return l.plan.ConfigureVCPU(vcpu)
}

// OnCreateVCPU implements hv.VMCallbacks.
func (l *LinuxLoader) OnCreateVCPU(vCpu hv.VirtualCPU) error {
	return nil
}

// OnCreateVM implements hv.VMCallbacks.
func (l *LinuxLoader) OnCreateVM(vm hv.VirtualMachine) error {
	if l.CreateVM != nil {
		return l.CreateVM(vm)
	}

	return nil
}

// OnCreateVMWithMemory implements hv.VMCallbacks.
func (l *LinuxLoader) OnCreateVMWithMemory(vm hv.VirtualMachine) error {
	if l.CreateVMWithMemory != nil {
		return l.CreateVMWithMemory(vm)
	}
	return nil
}

// implements hv.VMConfig.
func (l *LinuxLoader) CPUCount() int               { return l.NumCPUs }
func (l *LinuxLoader) Callbacks() hv.VMCallbacks   { return l }
func (l *LinuxLoader) Loader() hv.VMLoader         { return l }
func (l *LinuxLoader) MemoryBase() uint64          { return l.MemBase }
func (l *LinuxLoader) MemorySize() uint64          { return l.MemSize }
func (l *LinuxLoader) NeedsInterruptSupport() bool { return true }

// Load implements hv.VMLoader.
func (l *LinuxLoader) Load(vm hv.VirtualMachine) error {
	if l.GetKernel == nil {
		return errors.New("linux loader missing kernel provider")
	}

	kernelReader, kernelSize, err := l.GetKernel()
	if err != nil {
		return fmt.Errorf("get kernel: %w", err)
	}

	l.kernelReader = kernelReader

	arch := vm.Hypervisor().Architecture()

	initPayload, err := l.buildInitPayload(arch)
	if err != nil {
		return err
	}

	files := []InitFile{
		{Path: "/init", Data: initPayload, Mode: os.FileMode(0o755)},
		// add /dev/mem as /mem
		{Path: "/mem", Data: nil, Mode: os.FileMode(0o600), DevMajor: 1, DevMinor: 1},
	}
	files = append(files, l.AdditionalFiles...)
	initrd, err := buildInitramfs(files)
	if err != nil {
		return fmt.Errorf("build initramfs: %w", err)
	}

	cmdline, err := l.GetCmdline(arch)
	if err != nil {
		return fmt.Errorf("get cmdline: %w", err)
	}

	cmdlineBase := append([]string(nil), cmdline...)
	var virtioCmdline []string
	var virtioNodes []fdt.Node
	allocator := NewGSIAllocator(16, []uint32{0, 1, 2, 4, 8, 9, 10})
	for idx, dev := range l.Devices {
		// Opportunistically assign GSIs to devices that haven't chosen one.
		switch d := dev.(type) {
		case virtio.ConsoleTemplate:
			if d.IRQLine == 0 {
				d.IRQLine = allocator.Allocate()
				l.Devices[idx] = d
			}
		case virtio.FSTemplate:
			if d.IRQLine == 0 {
				d.IRQLine = allocator.Allocate()
				l.Devices[idx] = d
			}
		}
	}

	for _, dev := range l.Devices {
		if vdev, ok := dev.(virtio.VirtioMMIODevice); ok {
			params, err := vdev.GetLinuxCommandLineParam()
			if err != nil {
				return fmt.Errorf("get virtio mmio device linux cmdline param: %w", err)
			}
			virtioCmdline = append(virtioCmdline, params...)
			nodes, err := vdev.DeviceTreeNodes()
			if err != nil {
				return fmt.Errorf("get virtio mmio device tree nodes: %w", err)
			}
			virtioNodes = append(virtioNodes, nodes...)
		}
	}

	switch arch {
	case hv.ArchitectureX86_64:
		cmdline := append(cmdlineBase, virtioCmdline...)
		cmdlineStr := strings.Join(cmdline, " ")
		return l.loadAMD64(vm, kernelReader, kernelSize, cmdlineStr, initrd)
	case hv.ArchitectureARM64:
		cmdlineStr := strings.Join(cmdlineBase, " ")
		return l.loadARM64(vm, kernelReader, kernelSize, cmdlineStr, initrd, virtioNodes)
	case hv.ArchitectureRISCV64:
		return fmt.Errorf("linux loader for riscv64 is not implemented yet (pending kernel/initrd support)")
	default:
		return fmt.Errorf("unsupported architecture: %v", arch)
	}
}

func (l *LinuxLoader) buildInitPayload(arch hv.CpuArchitecture) ([]byte, error) {
	if l.GetInit == nil {
		return nil, errors.New("linux loader missing init program provider")
	}

	initProg, err := l.GetInit(arch)
	if err != nil {
		return nil, fmt.Errorf("get init program: %w", err)
	}
	if initProg == nil {
		return nil, fmt.Errorf("init program for %s is nil", arch)
	}

	fac, err := ir.BuildStandaloneProgramForArch(arch, initProg)
	if err != nil {
		return nil, fmt.Errorf("build init program: %w", err)
	}

	switch arch {
	case hv.ArchitectureX86_64:
		payload, err := amd64.StandaloneELF(fac)
		if err != nil {
			return nil, fmt.Errorf("build init payload ELF: %w", err)
		}
		return payload, nil
	case hv.ArchitectureARM64:
		payload, err := arm64.StandaloneELF(fac)
		if err != nil {
			return nil, fmt.Errorf("build init payload ELF: %w", err)
		}

		return payload, nil
	case hv.ArchitectureRISCV64:
		return nil, fmt.Errorf("init payload for riscv64 is not implemented yet")
	default:
		return nil, fmt.Errorf("unsupported architecture for init payload: %s", arch)
	}
}

func (l *LinuxLoader) loadAMD64(vm hv.VirtualMachine, kernelReader io.ReaderAt, kernelSize int64, cmdline string, initrd []byte) error {
	kernelImage, err := amd64boot.LoadKernel(kernelReader, kernelSize)
	if err != nil {
		return fmt.Errorf("load kernel: %w", err)
	}

	numCPUs := l.NumCPUs
	if numCPUs <= 0 {
		numCPUs = 1
	}

	memBase := vm.MemoryBase()
	memSize := vm.MemorySize()
	if memSize <= amd64ACPITablesSize {
		return fmt.Errorf("guest memory (%d bytes) too small for ACPI tables", memSize)
	}
	tablesBase := memBase + memSize - amd64ACPITablesSize

	e820 := amd64boot.DefaultE820Map(memBase, memBase+memSize)
	e820, err = reserveE820Region(e820, tablesBase, amd64ACPITablesSize)
	if err != nil {
		return fmt.Errorf("reserve ACPI tables in e820 map: %w", err)
	}

	opts := amd64boot.BootOptions{
		Cmdline: cmdline,
		Initrd:  initrd,
		E820:    e820,
	}

	if len(initrd) > 0 {
		reserveTop := tablesBase
		initrdSize := uint64(len(initrd))
		guard := uint64(amd64StackGuard)

		if reserveTop <= memBase+guard || initrdSize >= reserveTop-memBase {
			return fmt.Errorf("not enough space to place initrd below ACPI tables")
		}

		top := reserveTop - guard
		if top <= memBase || top < initrdSize {
			return fmt.Errorf("not enough space for initrd (size %d) with guard below ACPI tables", initrdSize)
		}

		opts.InitrdGPA = alignDown(top-initrdSize, 0x1000)
	} else {
		stackTop := tablesBase - amd64StackGuard
		if stackTop <= memBase {
			return fmt.Errorf("insufficient space for stack below ACPI tables")
		}
		opts.StackTopGPA = alignDown(stackTop, 0x10)
	}

	plan, err := kernelImage.Prepare(vm, opts)
	if err != nil {
		return fmt.Errorf("prepare kernel: %w", err)
	}
	l.plan = plan

	consoleSerial := amd64serial.NewSerial16550(0x3F8, 4, &convertCRLF{l.SerialStdout})
	if err := vm.AddDevice(consoleSerial); err != nil {
		return fmt.Errorf("add serial device: %w", err)
	}

	auxSerial := amd64serial.NewSerial16550(0x2F8, 3, io.Discard)
	if err := vm.AddDevice(auxSerial); err != nil {
		return fmt.Errorf("add aux serial device: %w", err)
	}

	if err := vm.AddDevice(pci.NewHostBridge()); err != nil {
		return fmt.Errorf("add pci host bridge: %w", err)
	}

	pic := chipset.NewDualPIC()
	if err := vm.AddDevice(pic); err != nil {
		return fmt.Errorf("add dual PIC: %w", err)
	}

	setter, _ := vm.(interface {
		SetIRQ(uint32, bool) error
	})
	irqForwarder := chipset.IRQLineFunc(func(line uint8, level bool) {
		if setter == nil {
			return
		}
		if err := setter.SetIRQ(uint32(line), level); err != nil {
			slog.Warn("set IRQ line", "line", line, "level", level, "err", err)
		}
	})

	if err := vm.AddDevice(chipset.NewPIT(irqForwarder)); err != nil {
		return fmt.Errorf("add PIT: %w", err)
	}

	if err := vm.AddDevice(chipset.NewCMOS(irqForwarder)); err != nil {
		return fmt.Errorf("add CMOS/RTC: %w", err)
	}

	if setter != nil {
		if err := vm.AddDevice(hpet.New(hpetBaseAddress, setter)); err != nil {
			return fmt.Errorf("add HPET device: %w", err)
		}
	}

	if err := vm.AddDevice(chipset.NewResetControlPort()); err != nil {
		return fmt.Errorf("add reset control port: %w", err)
	}

	var legacyPorts []uint16
	for _, rng := range []struct {
		start uint16
		end   uint16
	}{
		{0x0, 0xf},
		{0x11, 0x1f},
		{0x80, 0x8f},
		{0xBD, 0xBD}, // scratch port
		{0x2e8, 0x2ef},
		{0x3e8, 0x3ef},
		{0xbb00, 0xbbff},
	} {
		for port := rng.start; port <= rng.end; port++ {
			legacyPorts = append(legacyPorts, port)
		}
	}

	legacy := hv.SimpleX86IOPortDevice{
		Ports: legacyPorts,
		ReadFunc: func(port uint16, data []byte) error {
			if port == 0x12 {
				return hv.ErrGuestRequestedReboot
			}
			// slog.Info("legacy port read", "port", fmt.Sprintf("0x%04x", port), "size", len(data))
			for i := range data {
				data[i] = 0
			}
			return nil
		},
		WriteFunc: func(port uint16, data []byte) error {
			// slog.Info("legacy port write", "port", fmt.Sprintf("0x%04x", port), "size", len(data), "data", data)
			return nil
		},
	}
	if err := vm.AddDevice(legacy); err != nil {
		return fmt.Errorf("add legacy port stub: %w", err)
	}

	if err := vm.AddDevice(amd64input.NewI8042()); err != nil {
		return fmt.Errorf("add i8042 controller: %w", err)
	}

	for _, dev := range l.Devices {
		if err := vm.AddDeviceFromTemplate(dev); err != nil {
			return fmt.Errorf("add device from template: %w", err)
		}
	}

	if err := acpi.Install(vm, acpi.Config{
		MemoryBase: memBase,
		MemorySize: memSize,
		TablesBase: tablesBase,
		TablesSize: amd64ACPITablesSize,
		NumCPUs:    numCPUs,
		IOAPIC: acpi.IOAPICConfig{
			ID:      0,
			Address: uint32(chipset.IOAPICBaseAddress),
			GSIBase: 0,
		},
		HPET: &acpi.HPETConfig{
			Address: hpetBaseAddress,
		},
		ISAOverrides: []acpi.InterruptOverride{
			// Legacy ISA routing: IRQ0->GSI2 (already used), IRQ1 keyboard, IRQ4 serial, IRQ8 RTC.
			{Bus: 0, IRQ: 0, GSI: 2, Flags: 0},   // Timer (edge/high)
			{Bus: 0, IRQ: 1, GSI: 1, Flags: 0},   // Keyboard
			{Bus: 0, IRQ: 4, GSI: 4, Flags: 0},   // COM1
			{Bus: 0, IRQ: 8, GSI: 8, Flags: 0x0}, // RTC (edge/high)
		},
	}); err != nil {
		return fmt.Errorf("install ACPI tables: %w", err)
	}

	return nil
}

func reserveE820Region(entries []amd64boot.E820Entry, base, size uint64) ([]amd64boot.E820Entry, error) {
	if size == 0 {
		return entries, nil
	}
	end := base + size

	var out []amd64boot.E820Entry
	var reserved bool

	for _, ent := range entries {
		entEnd := ent.Addr + ent.Size
		if end <= ent.Addr || base >= entEnd {
			out = append(out, ent)
			continue
		}

		if base > ent.Addr {
			out = append(out, amd64boot.E820Entry{
				Addr: ent.Addr,
				Size: base - ent.Addr,
				Type: ent.Type,
			})
		}

		resStart := base
		if resStart < ent.Addr {
			resStart = ent.Addr
		}
		resEnd := end
		if resEnd > entEnd {
			resEnd = entEnd
		}

		if resEnd > resStart {
			out = append(out, amd64boot.E820Entry{
				Addr: resStart,
				Size: resEnd - resStart,
				Type: 2, // Reserved
			})
			reserved = true
		}

		if resEnd < entEnd {
			out = append(out, amd64boot.E820Entry{
				Addr: resEnd,
				Size: entEnd - resEnd,
				Type: ent.Type,
			})
		}
	}

	if !reserved {
		return entries, fmt.Errorf("reserved region [%#x, %#x) outside e820 map", base, end)
	}

	return out, nil
}

func alignDown(value, align uint64) uint64 {
	if align == 0 {
		return value
	}
	return value &^ (align - 1)
}

func (l *LinuxLoader) loadARM64(vm hv.VirtualMachine, kernelReader io.ReaderAt, kernelSize int64, cmdline string, initrd []byte, deviceTree []fdt.Node) error {
	kernelImage, err := arm64boot.LoadKernel(kernelReader, kernelSize)
	if err != nil {
		return fmt.Errorf("load kernel: %w", err)
	}

	numCPUs := l.NumCPUs
	if numCPUs <= 0 {
		numCPUs = 1
	}

	gicConfig, err := detectArm64GICConfig(vm)
	if err != nil {
		return fmt.Errorf("detect GIC config: %w", err)
	}

	plan, err := kernelImage.Prepare(vm, arm64boot.BootOptions{
		Cmdline: cmdline,
		Initrd:  initrd,
		NumCPUs: numCPUs,
		UART: &arm64boot.UARTConfig{
			Base:      arm64UARTMMIOBase,
			Size:      serial.UART8250MMIOSize,
			ClockHz:   serial.UART8250DefaultClock,
			RegShift:  arm64UARTRegShift,
			BaudRate:  arm64UARTBaudRate,
			Interrupt: arm64UARTInterrupt,
		},
		GIC:             gicConfig,
		DeviceTreeNodes: deviceTree,
	})
	if err != nil {
		return fmt.Errorf("prepare kernel: %w", err)
	}
	l.plan = plan

	uartDev := serial.NewUART8250MMIO(arm64UARTMMIOBase, arm64UARTRegShift, arm64UARTIRQLine, &convertCRLF{l.SerialStdout})
	if err := vm.AddDevice(uartDev); err != nil {
		return fmt.Errorf("add arm64 uart device: %w", err)
	}

	for _, dev := range l.Devices {
		if err := vm.AddDeviceFromTemplate(dev); err != nil {
			return fmt.Errorf("add device from template: %w", err)
		}
	}

	return nil
}

func detectArm64GICConfig(vm hv.VirtualMachine) (*arm64boot.GICConfig, error) {
	if vm == nil {
		return nil, errors.New("vm is nil")
	}

	config := arm64boot.DefaultGICConfig()

	if provider, ok := vm.(hv.Arm64GICProvider); ok {
		if info, ok := provider.Arm64GICInfo(); ok {
			if ver := convertArm64GICVersion(info.Version); ver != arm64boot.GICVersionUnknown {
				config.Version = ver
			}
			if info.DistributorBase != 0 {
				config.DistributorBase = info.DistributorBase
			}
			if info.DistributorSize != 0 {
				config.DistributorSize = info.DistributorSize
			}
			if info.RedistributorBase != 0 {
				config.RedistributorBase = info.RedistributorBase
			}
			if info.RedistributorSize != 0 {
				config.RedistributorSize = info.RedistributorSize
			}
			if info.CpuInterfaceBase != 0 {
				config.CpuInterfaceBase = info.CpuInterfaceBase
			}
			if info.CpuInterfaceSize != 0 {
				config.CpuInterfaceSize = info.CpuInterfaceSize
			}
			if info.ItsBase != 0 {
				config.ItsBase = info.ItsBase
			}
			if info.ItsSize != 0 {
				config.ItsSize = info.ItsSize
			}
			if info.MaintenanceInterrupt != (hv.Arm64Interrupt{}) {
				config.MaintenanceInterrupt = arm64boot.InterruptSpec{
					Type:  info.MaintenanceInterrupt.Type,
					Num:   info.MaintenanceInterrupt.Num,
					Flags: info.MaintenanceInterrupt.Flags,
				}
			}
		}
	}

	if runtime.GOOS == "windows" && config.Version == arm64boot.GICVersion3 {
		base, err := queryArm64GICRBase(vm)
		if err != nil {
			return nil, fmt.Errorf("query GIC redistributor base: %w", err)
		}
		if base != 0 {
			config.RedistributorBase = base
		}
	}

	return &config, nil
}

func convertArm64GICVersion(ver hv.Arm64GICVersion) arm64boot.GICVersion {
	switch ver {
	case hv.Arm64GICVersion2:
		return arm64boot.GICVersion2
	case hv.Arm64GICVersion3:
		return arm64boot.GICVersion3
	default:
		return arm64boot.GICVersionUnknown
	}
}

func queryArm64GICRBase(vm hv.VirtualMachine) (uint64, error) {
	var base uint64
	err := vm.VirtualCPUCall(0, func(cpu hv.VirtualCPU) error {
		regs := map[hv.Register]hv.RegisterValue{
			hv.RegisterARM64GicrBase: hv.Register64(0),
		}
		if err := cpu.GetRegisters(regs); err != nil {
			return fmt.Errorf("get GICR base register: %w", err)
		}
		value, ok := regs[hv.RegisterARM64GicrBase].(hv.Register64)
		if !ok {
			return fmt.Errorf("unexpected register value type %T for GICR base", regs[hv.RegisterARM64GicrBase])
		}
		base = uint64(value)
		return nil
	})
	if err != nil {
		return 0, err
	}
	return base, nil
}

func (l *LinuxLoader) RunConfig() (hv.RunConfig, error) {
	loader := &programRunner{loader: l, linux: l.kernelReader}

	return loader, nil
}

var (
	_ hv.VMLoader    = &LinuxLoader{}
	_ hv.VMConfig    = &LinuxLoader{}
	_ hv.VMCallbacks = &LinuxLoader{}
)
