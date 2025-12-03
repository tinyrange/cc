package arm64

import (
	"errors"
	"fmt"
	"math"

	"github.com/tinyrange/cc/internal/fdt"
	"github.com/tinyrange/cc/internal/hv"
)

const (
	dtbAlignment    = 0x8
	initrdAlignment = 0x1000
	stackGuardBytes = 0x2000
)

// BootOptions describes how the ARM64 kernel should be placed into guest RAM.
type BootOptions struct {
	Cmdline string

	Initrd        []byte
	InitrdGPA     uint64
	DeviceTreeGPA uint64
	StackTopGPA   uint64

	NumCPUs         int
	UART            *UARTConfig
	GIC             *GICConfig
	DeviceTreeNodes []fdt.Node
}

func (o BootOptions) withDefaults() BootOptions {
	out := o
	if out.NumCPUs <= 0 {
		out.NumCPUs = 1
	}
	return out
}

// UARTConfig describes an optional ns16550-compatible console.
type UARTConfig struct {
	Base     uint64
	Size     uint64
	ClockHz  uint32
	RegShift uint32
	BaudRate uint32
}

// BootPlan captures the derived addresses needed to enter the kernel.
type BootPlan struct {
	EntryGPA      uint64
	StackTopGPA   uint64
	DeviceTreeGPA uint64
}

type InterruptSpec struct {
	Type  uint32
	Num   uint32
	Flags uint32
}

func (s InterruptSpec) isZero() bool {
	return s == (InterruptSpec{})
}

type GICVersion int

const (
	GICVersionUnknown GICVersion = iota
	GICVersion2
	GICVersion3
)

type GICConfig struct {
	Version              GICVersion
	DistributorBase      uint64
	DistributorSize      uint64
	RedistributorBase    uint64
	RedistributorSize    uint64
	CpuInterfaceBase     uint64
	CpuInterfaceSize     uint64
	MaintenanceInterrupt InterruptSpec
}

const (
	defaultGICDistributorBase          = 0x08000000
	defaultGICDistributorSize          = 0x00010000
	defaultGICRedistributorBase        = 0x080a0000
	defaultGICRedistributorSize        = 0x00020000
	defaultGICCpuInterfaceBase         = 0x08010000
	defaultGICCpuInterfaceSize         = 0x00002000
	gicDefaultPhandle           uint32 = 1
)

var defaultGICMaintenanceInterrupt = InterruptSpec{Type: 1, Num: 9, Flags: 0x4}

// DefaultGICConfig returns the standard tinyrange GIC description.
func DefaultGICConfig() GICConfig {
	return GICConfig{
		Version:              GICVersion3,
		DistributorBase:      defaultGICDistributorBase,
		DistributorSize:      defaultGICDistributorSize,
		RedistributorBase:    defaultGICRedistributorBase,
		RedistributorSize:    defaultGICRedistributorSize,
		CpuInterfaceBase:     defaultGICCpuInterfaceBase,
		CpuInterfaceSize:     defaultGICCpuInterfaceSize,
		MaintenanceInterrupt: defaultGICMaintenanceInterrupt,
	}
}

func (c *GICConfig) withDefaults() GICConfig {
	if c == nil {
		return DefaultGICConfig()
	}
	out := *c
	if out.Version == GICVersionUnknown {
		out.Version = GICVersion3
	}
	if out.DistributorBase == 0 {
		out.DistributorBase = defaultGICDistributorBase
	}
	if out.DistributorSize == 0 {
		out.DistributorSize = defaultGICDistributorSize
	}
	if out.RedistributorBase == 0 {
		out.RedistributorBase = defaultGICRedistributorBase
	}
	if out.RedistributorSize == 0 {
		out.RedistributorSize = defaultGICRedistributorSize
	}
	if out.CpuInterfaceBase == 0 {
		out.CpuInterfaceBase = defaultGICCpuInterfaceBase
	}
	if out.CpuInterfaceSize == 0 {
		out.CpuInterfaceSize = defaultGICCpuInterfaceSize
	}
	if out.MaintenanceInterrupt.isZero() {
		out.MaintenanceInterrupt = defaultGICMaintenanceInterrupt
	}
	return out
}

// Prepare loads the kernel payload and supporting blobs into guest RAM and
// derives the state required to enter the kernel.
func (k *KernelImage) Prepare(vm hv.VirtualMachine, opts BootOptions) (*BootPlan, error) {
	if vm == nil || vm.MemorySize() == 0 {
		return nil, errors.New("arm64 prepare requires a virtual machine")
	}
	if k == nil || len(k.Payload()) == 0 {
		return nil, errors.New("arm64 kernel payload is empty")
	}

	opts = opts.withDefaults()

	memStart := vm.MemoryBase()
	memSize := vm.MemorySize()
	memEnd := memStart + memSize

	base := alignUp(memStart, imageLoadAlignment)
	loadAddr := base + k.Header.TextOffset
	if loadAddr < memStart {
		return nil, fmt.Errorf("arm64 kernel load address %#x below RAM base %#x", loadAddr, memStart)
	}

	payload := k.Payload()
	kernelEnd := loadAddr + uint64(len(payload))
	if kernelEnd > memEnd {
		return nil, fmt.Errorf("arm64 kernel [%#x, %#x) outside RAM [%#x, %#x)", loadAddr, kernelEnd, memStart, memEnd)
	}

	if err := writeGuest(vm, loadAddr, payload); err != nil {
		return nil, fmt.Errorf("write arm64 kernel payload: %w", err)
	}

	var initrdStart, initrdEnd uint64
	if len(opts.Initrd) > 0 {
		initrdStart = opts.InitrdGPA
		if initrdStart == 0 {
			initrdStart = alignUp(kernelEnd, initrdAlignment)
		}
		initrdEnd = initrdStart + uint64(len(opts.Initrd))
		if initrdStart < memStart || initrdEnd > memEnd {
			return nil, fmt.Errorf("initrd [%#x, %#x) outside RAM [%#x, %#x)", initrdStart, initrdEnd, memStart, memEnd)
		}
		if err := writeGuest(vm, initrdStart, opts.Initrd); err != nil {
			return nil, fmt.Errorf("write initrd: %w", err)
		}
	}

	dtbConfig := deviceTreeConfig{
		MemoryBase:  memStart,
		MemorySize:  memSize,
		NumCPUs:     opts.NumCPUs,
		Cmdline:     opts.Cmdline,
		InitrdStart: initrdStart,
		InitrdEnd:   initrdEnd,
		UART:        opts.UART,
		GIC:         opts.GIC,
		Devices:     opts.DeviceTreeNodes,
	}
	dtb, err := buildDeviceTree(dtbConfig)
	if err != nil {
		return nil, fmt.Errorf("build device tree: %w", err)
	}

	dtbAddr := opts.DeviceTreeGPA
	if dtbAddr == 0 {
		allocBase := kernelEnd
		if initrdEnd > allocBase {
			allocBase = initrdEnd
		}
		dtbAddr = alignUp(allocBase, dtbAlignment)
	}
	dtbEnd := dtbAddr + uint64(len(dtb))
	if dtbAddr < memStart || dtbEnd > memEnd {
		return nil, fmt.Errorf("device tree [%#x, %#x) outside RAM [%#x, %#x)", dtbAddr, dtbEnd, memStart, memEnd)
	}
	if err := writeGuest(vm, dtbAddr, dtb); err != nil {
		return nil, fmt.Errorf("write device tree: %w", err)
	}

	stackTop := opts.StackTopGPA
	if stackTop == 0 {
		stackTop = alignDown(memEnd, 16)
	}
	if stackTop <= dtbEnd+stackGuardBytes {
		return nil, fmt.Errorf("stack top %#x overlaps device tree ending at %#x", stackTop, dtbEnd)
	}

	entry, err := k.Header.EntryPoint(base)
	if err != nil {
		return nil, fmt.Errorf("arm64 entry point: %w", err)
	}

	return &BootPlan{
		EntryGPA:      entry,
		StackTopGPA:   stackTop,
		DeviceTreeGPA: dtbAddr,
	}, nil
}

// ConfigureVCPU programs the first vCPU for entry into the Linux kernel.
func (p *BootPlan) ConfigureVCPU(vcpu hv.VirtualCPU) error {
	if p == nil {
		return errors.New("arm64 boot plan is nil")
	}
	if vcpu == nil {
		return errors.New("arm64 configure requires a vCPU")
	}
	if p.DeviceTreeGPA == 0 {
		return errors.New("arm64 device tree GPA is zero")
	}

	regs := map[hv.Register]hv.RegisterValue{
		hv.RegisterARM64Pc:     hv.Register64(p.EntryGPA),
		hv.RegisterARM64Sp:     hv.Register64(p.StackTopGPA),
		hv.RegisterARM64X0:     hv.Register64(p.DeviceTreeGPA),
		hv.RegisterARM64X1:     hv.Register64(0),
		hv.RegisterARM64X2:     hv.Register64(0),
		hv.RegisterARM64X3:     hv.Register64(0),
		hv.RegisterARM64Pstate: hv.Register64(defaultPstateBits),
	}
	if err := vcpu.SetRegisters(regs); err != nil {
		return fmt.Errorf("set arm64 registers: %w", err)
	}
	return nil
}

const (
	pstateModeEL1h    = 0x5
	pstateDF          = 0x200
	pstateAF          = 0x100
	pstateIF          = 0x80
	pstateFF          = 0x40
	defaultPstateBits = pstateModeEL1h | pstateDF | pstateAF | pstateIF | pstateFF
)

type deviceTreeConfig struct {
	MemoryBase  uint64
	MemorySize  uint64
	NumCPUs     int
	Cmdline     string
	InitrdStart uint64
	InitrdEnd   uint64
	UART        *UARTConfig
	GIC         *GICConfig
	Devices     []fdt.Node
}

func buildDeviceTree(cfg deviceTreeConfig) ([]byte, error) {
	if cfg.MemorySize == 0 {
		return nil, errors.New("device tree requires non-zero RAM size")
	}
	if cfg.NumCPUs <= 0 {
		return nil, errors.New("device tree requires at least one CPU")
	}

	gicCfg := DefaultGICConfig()
	if cfg.GIC != nil {
		gicCfg = cfg.GIC.withDefaults()
	}

	root := fdt.Node{
		Name: "",
		Properties: map[string]fdt.Property{
			"#address-cells":   {U32: []uint32{2}},
			"#size-cells":      {U32: []uint32{2}},
			"compatible":       {Strings: []string{"tinyrange,cc-arm64", "tinyrange,cc"}},
			"model":            {Strings: []string{"tinyrange-cc"}},
			"interrupt-parent": {U32: []uint32{gicDefaultPhandle}},
		},
	}

	cpus := fdt.Node{
		Name: "cpus",
		Properties: map[string]fdt.Property{
			"#address-cells": {U32: []uint32{2}},
			"#size-cells":    {U32: []uint32{0}},
		},
	}
	for cpu := 0; cpu < cfg.NumCPUs; cpu++ {
		cpuNode := fdt.Node{
			Name: fmt.Sprintf("cpu@%d", cpu),
			Properties: map[string]fdt.Property{
				"device_type":   {Strings: []string{"cpu"}},
				"compatible":    {Strings: []string{"arm,armv8"}},
				"reg":           {U64: []uint64{uint64(cpu)}},
				"enable-method": {Strings: []string{"psci"}},
			},
		}
		cpus.Children = append(cpus.Children, cpuNode)
	}
	root.Children = append(root.Children, cpus)

	memoryNode := fdt.Node{
		Name: fmt.Sprintf("memory@%x", cfg.MemoryBase),
		Properties: map[string]fdt.Property{
			"device_type": {Strings: []string{"memory"}},
			"reg":         {U64: []uint64{cfg.MemoryBase, cfg.MemorySize}},
		},
	}
	root.Children = append(root.Children, memoryNode)

	var aliasesProps map[string]fdt.Property
	stdoutAlias := ""
	stdoutBaud := uint32(0)
	if cfg.UART != nil {
		if cfg.UART.Size == 0 {
			return nil, errors.New("uart config requires non-zero size")
		}
		serialNode := fdt.Node{
			Name: fmt.Sprintf("serial@%x", cfg.UART.Base),
			Properties: map[string]fdt.Property{
				"compatible":   {Strings: []string{"ns16550a"}},
				"reg":          {U64: []uint64{cfg.UART.Base, cfg.UART.Size}},
				"reg-io-width": {U32: []uint32{1}},
				"status":       {Strings: []string{"okay"}},
			},
		}
		if cfg.UART.ClockHz != 0 {
			serialNode.Properties["clock-frequency"] = fdt.Property{U32: []uint32{cfg.UART.ClockHz}}
		}
		if cfg.UART.RegShift != 0 {
			serialNode.Properties["reg-shift"] = fdt.Property{U32: []uint32{cfg.UART.RegShift}}
		}
		root.Children = append(root.Children, serialNode)

		serialPath := fmt.Sprintf("/%s", serialNode.Name)
		aliasesProps = map[string]fdt.Property{
			"serial0": {Strings: []string{serialPath}},
		}
		stdoutAlias = "serial0"
		stdoutBaud = cfg.UART.BaudRate
	}

	if len(aliasesProps) > 0 {
		root.Children = append(root.Children, fdt.Node{Name: "aliases", Properties: aliasesProps})
	}

	chosenProps := map[string]fdt.Property{}
	if cfg.Cmdline != "" {
		chosenProps["bootargs"] = fdt.Property{Strings: []string{cfg.Cmdline}}
	}
	if cfg.InitrdEnd > cfg.InitrdStart {
		chosenProps["linux,initrd-start"] = fdt.Property{U64: []uint64{cfg.InitrdStart}}
		chosenProps["linux,initrd-end"] = fdt.Property{U64: []uint64{cfg.InitrdEnd}}
	}
	if stdoutAlias != "" {
		baud := stdoutBaud
		if baud == 0 {
			baud = 115200
		}
		stdout := fmt.Sprintf("%s:%dn8", stdoutAlias, baud)
		chosenProps["stdout-path"] = fdt.Property{Strings: []string{stdout}}
		chosenProps["linux,stdout-path"] = fdt.Property{Strings: []string{stdout}}
	}
	root.Children = append(root.Children, fdt.Node{Name: "chosen", Properties: chosenProps})

	root.Children = append(root.Children, fdt.Node{
		Name: "psci",
		Properties: map[string]fdt.Property{
			"compatible": {Strings: []string{"arm,psci-0.2", "arm,psci"}},
			"method":     {Strings: []string{"hvc"}},
		},
	})

	timerInterrupts := []uint32{1, 13, 4, 1, 14, 4, 1, 11, 4, 1, 10, 4}
	root.Children = append(root.Children, fdt.Node{
		Name: "timer",
		Properties: map[string]fdt.Property{
			"compatible": {Strings: []string{"arm,armv8-timer"}},
			"always-on":  {Flag: true},
			"interrupts": {U32: timerInterrupts},
		},
	})

	gicNode := fdt.Node{
		Name: fmt.Sprintf("interrupt-controller@%x", gicCfg.DistributorBase),
		Properties: map[string]fdt.Property{
			"#interrupt-cells":     {U32: []uint32{3}},
			"#address-cells":       {U32: []uint32{2}},
			"#size-cells":          {U32: []uint32{2}},
			"interrupt-controller": {Flag: true},
			"phandle":              {U32: []uint32{gicDefaultPhandle}},
			"linux,phandle":        {U32: []uint32{gicDefaultPhandle}},
			"interrupts": {U32: []uint32{
				gicCfg.MaintenanceInterrupt.Type,
				gicCfg.MaintenanceInterrupt.Num,
				gicCfg.MaintenanceInterrupt.Flags,
			}},
		},
	}
	switch gicCfg.Version {
	case GICVersion2:
		gicNode.Properties["compatible"] = fdt.Property{Strings: []string{"arm,gic-400"}}
		gicNode.Properties["reg"] = fdt.Property{U64: []uint64{
			gicCfg.DistributorBase, gicCfg.DistributorSize,
			gicCfg.CpuInterfaceBase, gicCfg.CpuInterfaceSize,
		}}
	default:
		gicNode.Properties["compatible"] = fdt.Property{Strings: []string{"arm,gic-v3"}}
		gicNode.Properties["reg"] = fdt.Property{U64: []uint64{
			gicCfg.DistributorBase, gicCfg.DistributorSize,
			gicCfg.RedistributorBase, gicCfg.RedistributorSize,
		}}
	}
	root.Children = append(root.Children, gicNode)

	root.Children = append(root.Children, cfg.Devices...)

	return fdt.Build(root)
}

func alignUp(value, align uint64) uint64 {
	if align == 0 {
		return value
	}
	mask := align - 1
	return (value + mask) &^ mask
}

func alignDown(value, align uint64) uint64 {
	if align == 0 {
		return value
	}
	mask := align - 1
	return value &^ mask
}

func writeGuest(vm hv.VirtualMachine, guestAddr uint64, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	memStart := vm.MemoryBase()
	memEnd := memStart + vm.MemorySize()
	if guestAddr < memStart || guestAddr+uint64(len(data)) > memEnd {
		return fmt.Errorf("guest address range [%#x, %#x) outside RAM [%#x, %#x)", guestAddr, guestAddr+uint64(len(data)), memStart, memEnd)
	}
	if guestAddr > math.MaxInt64 {
		return fmt.Errorf("guest address %#x out of host range", guestAddr)
	}
	if _, err := vm.WriteAt(data, int64(guestAddr)); err != nil {
		return fmt.Errorf("write guest memory at %#x: %w", guestAddr, err)
	}
	return nil
}
