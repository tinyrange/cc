package arm64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"

	"j5.nz/cc/internal/fdt"
	linuxarm64 "j5.nz/cc/internal/linux/boot/arm64"
)

const (
	dtbAlignment = 8
	dtbOffset    = 0x08000000

	DefaultUARTBase     = 0x09000000
	DefaultUARTSize     = 0x1000
	DefaultUARTClockHz  = 1843200
	DefaultUARTRegShift = 0
	DefaultUARTBaudRate = 115200

	defaultGICDistributorBase           = 0x08000000
	defaultGICDistributorSize           = 0x00010000
	defaultGICv2CPUInterfaceBase        = 0x08010000
	defaultGICv2CPUInterfaceSize        = 0x00002000
	defaultGICRedistributorBase         = 0x080a0000
	defaultGICRedistributorSize         = 0x00020000
	gicDefaultPhandle            uint32 = 1
)

type GICVersion int

const (
	GICVersionDefault GICVersion = iota
	GICVersionV2
	GICVersionV3
)

type BootOptions struct {
	MemoryBase uint64
	MemorySize uint64
	NumCPUs    int
	GICVersion GICVersion
	UART       *UARTConfig
	Console    bool
	BootArgs   string
	ExtraNodes []fdt.Node
}

type UARTConfig struct {
	Base      uint64
	Size      uint64
	ClockHz   uint32
	RegShift  uint32
	BaudRate  uint32
	Interrupt InterruptSpec
}

type InterruptSpec struct {
	Type  uint32
	Num   uint32
	Flags uint32
}

type BootPlan struct {
	EntryGPA      uint64
	KernelGPA     uint64
	KernelEndGPA  uint64
	DeviceTreeGPA uint64
	DeviceTree    []byte
}

func PrepareBoot(memory []byte, kernelFile []byte, opts BootOptions) (*BootPlan, error) {
	if len(memory) == 0 {
		return nil, errors.New("guest memory is empty")
	}
	if len(kernelFile) == 0 {
		return nil, errors.New("kernel file is empty")
	}
	if opts.MemorySize == 0 {
		opts.MemorySize = uint64(len(memory))
	}
	if opts.NumCPUs <= 0 {
		opts.NumCPUs = 1
	}
	if opts.GICVersion == GICVersionDefault {
		opts.GICVersion = GICVersionV2
	}
	if opts.UART == nil {
		opts.UART = &UARTConfig{
			Base:      DefaultUARTBase,
			Size:      DefaultUARTSize,
			ClockHz:   DefaultUARTClockHz,
			RegShift:  DefaultUARTRegShift,
			BaudRate:  DefaultUARTBaudRate,
			Interrupt: InterruptSpec{Type: 0, Num: 33, Flags: 0x4},
		}
	}
	if opts.BootArgs == "" {
		opts.BootArgs = "root=ld0a"
	}

	reader := bytes.NewReader(kernelFile)
	probe, err := linuxarm64.ProbeKernelImage(reader, int64(len(kernelFile)))
	if err != nil {
		return nil, fmt.Errorf("parse NetBSD arm64 kernel image: %w", err)
	}
	image, err := probe.ExtractImage(reader, int64(len(kernelFile)))
	if err != nil {
		return nil, fmt.Errorf("extract NetBSD arm64 kernel image: %w", err)
	}

	base := alignUp(opts.MemoryBase, linuxarm64.ImageLoadAlignment)
	loadAddr := base + probe.Header.TextOffset
	entry, err := probe.Header.EntryPoint(base)
	if err != nil {
		return nil, err
	}
	kernelReserveSize := uint64(len(image))
	if probe.Header.ImageSize > kernelReserveSize {
		kernelReserveSize = probe.Header.ImageSize
	}
	loadOff := loadAddr - opts.MemoryBase
	if loadAddr < opts.MemoryBase || loadOff+kernelReserveSize > uint64(len(memory)) {
		return nil, fmt.Errorf("NetBSD arm64 kernel does not fit in guest RAM")
	}
	copy(memory[loadOff:loadOff+uint64(len(image))], image)
	kernelEndGPA := loadAddr + kernelReserveSize

	dtb, err := buildDeviceTree(deviceTreeConfig{
		MemoryBase: opts.MemoryBase,
		MemorySize: opts.MemorySize,
		NumCPUs:    opts.NumCPUs,
		GICVersion: opts.GICVersion,
		UART:       opts.UART,
		Console:    opts.Console,
		BootArgs:   opts.BootArgs,
		ExtraNodes: append([]fdt.Node(nil), opts.ExtraNodes...),
	})
	if err != nil {
		return nil, err
	}

	dtbAddr := alignUp(opts.MemoryBase+dtbOffset, dtbAlignment)
	dtbOff := dtbAddr - opts.MemoryBase
	if dtbAddr < opts.MemoryBase || dtbOff+uint64(len(dtb)) > uint64(len(memory)) {
		return nil, fmt.Errorf("device tree does not fit in guest RAM")
	}
	copy(memory[dtbOff:dtbOff+uint64(len(dtb))], dtb)
	if err := writeQEMULoaderStub(memory, dtbAddr, entry); err != nil {
		return nil, err
	}

	return &BootPlan{
		EntryGPA:      opts.MemoryBase,
		KernelGPA:     loadAddr,
		KernelEndGPA:  kernelEndGPA,
		DeviceTreeGPA: dtbAddr,
		DeviceTree:    dtb,
	}, nil
}

func writeQEMULoaderStub(memory []byte, dtbAddr, kernelEntry uint64) error {
	const stubSize = 40
	if uint64(len(memory)) < stubSize {
		return fmt.Errorf("guest memory too small for NetBSD loader stub")
	}
	stub := memory[:stubSize]
	binary.LittleEndian.PutUint32(stub[0:4], 0x580000c0)   // ldr x0, .+0x18
	binary.LittleEndian.PutUint32(stub[4:8], 0xaa1f03e1)   // mov x1, xzr
	binary.LittleEndian.PutUint32(stub[8:12], 0xaa1f03e2)  // mov x2, xzr
	binary.LittleEndian.PutUint32(stub[12:16], 0xaa1f03e3) // mov x3, xzr
	binary.LittleEndian.PutUint32(stub[16:20], 0x58000084) // ldr x4, .+0x20
	binary.LittleEndian.PutUint32(stub[20:24], 0xd61f0080) // br x4
	binary.LittleEndian.PutUint64(stub[24:32], dtbAddr)
	binary.LittleEndian.PutUint64(stub[32:40], kernelEntry)
	return nil
}

type deviceTreeConfig struct {
	MemoryBase uint64
	MemorySize uint64
	NumCPUs    int
	GICVersion GICVersion
	UART       *UARTConfig
	Console    bool
	BootArgs   string
	ExtraNodes []fdt.Node
}

func buildDeviceTree(cfg deviceTreeConfig) ([]byte, error) {
	root := fdt.Node{
		Name: "",
		Properties: map[string]fdt.Property{
			"#address-cells":   {U32: []uint32{2}},
			"#size-cells":      {U32: []uint32{2}},
			"compatible":       {Strings: []string{"linux,dummy-virt"}},
			"model":            {Strings: []string{"linux,dummy-virt"}},
			"interrupt-parent": {U32: []uint32{gicDefaultPhandle}},
		},
	}

	cpus := fdt.Node{
		Name: "cpus",
		Properties: map[string]fdt.Property{
			"#address-cells": {U32: []uint32{1}},
			"#size-cells":    {U32: []uint32{0}},
		},
	}
	for cpu := 0; cpu < cfg.NumCPUs; cpu++ {
		cpus.Children = append(cpus.Children, fdt.Node{
			Name: fmt.Sprintf("cpu@%d", cpu),
			Properties: map[string]fdt.Property{
				"device_type":   {Strings: []string{"cpu"}},
				"compatible":    {Strings: []string{"arm,cortex-a57"}},
				"reg":           {U32: []uint32{uint32(cpu)}},
				"enable-method": {Strings: []string{"psci"}},
			},
		})
	}
	root.Children = append(root.Children, cpus)

	root.Children = append(root.Children, fdt.Node{
		Name: fmt.Sprintf("memory@%x", cfg.MemoryBase),
		Properties: map[string]fdt.Property{
			"device_type": {Strings: []string{"memory"}},
			"reg":         {U64: []uint64{cfg.MemoryBase, cfg.MemorySize}},
		},
	})

	if cfg.UART != nil {
		serialNode := fdt.Node{
			Name: fmt.Sprintf("serial@%x", cfg.UART.Base),
			Properties: map[string]fdt.Property{
				"clock-frequency":  {U32: []uint32{cfg.UART.ClockHz}},
				"compatible":       {Strings: []string{"ns16550a", "ns16550"}},
				"current-speed":    {U32: []uint32{cfg.UART.BaudRate}},
				"interrupt-parent": {U32: []uint32{gicDefaultPhandle}},
				"interrupts": {U32: []uint32{
					cfg.UART.Interrupt.Type, cfg.UART.Interrupt.Num, cfg.UART.Interrupt.Flags,
				}},
				"reg":          {U64: []uint64{cfg.UART.Base, cfg.UART.Size}},
				"reg-io-width": {U32: []uint32{1}},
				"status":       {Strings: []string{"okay"}},
			},
		}
		if cfg.UART.RegShift != 0 {
			serialNode.Properties["reg-shift"] = fdt.Property{U32: []uint32{cfg.UART.RegShift}}
		}
		root.Children = append(root.Children, serialNode)
		root.Children = append(root.Children, fdt.Node{
			Name: "aliases",
			Properties: map[string]fdt.Property{
				"serial0": {Strings: []string{fmt.Sprintf("/%s", serialNode.Name)}},
			},
		})
		chosen := fdt.Node{
			Name: "chosen",
			Properties: map[string]fdt.Property{
				"bootargs": {Strings: []string{cfg.BootArgs}},
			},
		}
		if cfg.Console {
			consolePath := fmt.Sprintf("/%s", serialNode.Name)
			chosen.Properties["stdout-path"] = fdt.Property{Strings: []string{consolePath}}
			chosen.Properties["linux,stdout-path"] = fdt.Property{Strings: []string{consolePath}}
		}
		root.Children = append(root.Children, chosen)
	}

	root.Children = append(root.Children, fdt.Node{
		Name: "psci",
		Properties: map[string]fdt.Property{
			"compatible":  {Strings: []string{"arm,psci-1.0", "arm,psci-0.2", "arm,psci"}},
			"cpu_on":      {U32: []uint32{0xc4000003}},
			"cpu_off":     {U32: []uint32{0x84000002}},
			"cpu_suspend": {U32: []uint32{0xc4000001}},
			"method":      {Strings: []string{"hvc"}},
		},
	})
	root.Children = append(root.Children, fdt.Node{
		Name: "apb-pclk",
		Properties: map[string]fdt.Property{
			"#clock-cells":       {U32: []uint32{0}},
			"clock-frequency":    {U32: []uint32{24000000}},
			"clock-output-names": {Strings: []string{"clk24mhz"}},
			"compatible":         {Strings: []string{"fixed-clock"}},
			"phandle":            {U32: []uint32{2}},
		},
	})
	root.Children = append(root.Children, fdt.Node{
		Name: "timer",
		Properties: map[string]fdt.Property{
			"compatible": {Strings: []string{"arm,armv8-timer", "arm,armv7-timer"}},
			"always-on":  {Flag: true},
			"interrupts": {U32: []uint32{1, 13, 0x104, 1, 14, 0x104, 1, 11, 0x104, 1, 10, 0x104}},
		},
	})
	if cfg.GICVersion == GICVersionV3 {
		root.Children = append(root.Children, fdt.Node{
			Name: fmt.Sprintf("interrupt-controller@%x", defaultGICDistributorBase),
			Properties: map[string]fdt.Property{
				"#interrupt-cells":     {U32: []uint32{3}},
				"#address-cells":       {U32: []uint32{2}},
				"#size-cells":          {U32: []uint32{2}},
				"compatible":           {Strings: []string{"arm,gic-v3"}},
				"interrupt-controller": {Flag: true},
				"phandle":              {U32: []uint32{gicDefaultPhandle}},
				"linux,phandle":        {U32: []uint32{gicDefaultPhandle}},
				"ranges":               {Flag: true},
				"reg": {U64: []uint64{
					defaultGICDistributorBase, defaultGICDistributorSize,
					defaultGICRedistributorBase, defaultGICRedistributorSize,
				}},
			},
		})
	} else {
		root.Children = append(root.Children, fdt.Node{
			Name: fmt.Sprintf("interrupt-controller@%x", defaultGICDistributorBase),
			Properties: map[string]fdt.Property{
				"#interrupt-cells":     {U32: []uint32{3}},
				"#address-cells":       {U32: []uint32{2}},
				"#size-cells":          {U32: []uint32{2}},
				"compatible":           {Strings: []string{"arm,cortex-a15-gic"}},
				"interrupt-controller": {Flag: true},
				"phandle":              {U32: []uint32{gicDefaultPhandle}},
				"linux,phandle":        {U32: []uint32{gicDefaultPhandle}},
				"reg": {U64: []uint64{
					defaultGICDistributorBase, defaultGICDistributorSize,
					defaultGICv2CPUInterfaceBase, defaultGICv2CPUInterfaceSize,
				}},
			},
		})
	}
	root.Children = append(root.Children, cfg.ExtraNodes...)

	return fdt.Build(root)
}

func alignUp(value, align uint64) uint64 {
	if align == 0 {
		return value
	}
	mask := align - 1
	return (value + mask) &^ mask
}
