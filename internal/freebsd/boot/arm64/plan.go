package arm64

import (
	"bytes"
	"debug/elf"
	"errors"
	"fmt"
	"io"

	"j5.nz/cc/internal/fdt"
)

const (
	kernelBaseVA = 0xffff000000000000
	pageSize     = 4096
	dtbAlignment = 8

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

	entry, kernelEndGPA, err := loadELF(memory, kernelFile, opts.MemoryBase)
	if err != nil {
		return nil, err
	}

	dtb, err := buildDeviceTree(deviceTreeConfig{
		MemoryBase: opts.MemoryBase,
		MemorySize: opts.MemorySize,
		NumCPUs:    opts.NumCPUs,
		GICVersion: opts.GICVersion,
		UART:       opts.UART,
		Console:    opts.Console,
		ExtraNodes: append([]fdt.Node(nil), opts.ExtraNodes...),
	})
	if err != nil {
		return nil, err
	}

	dtbAddr := alignUp(kernelEndGPA, dtbAlignment)
	dtbOff := dtbAddr - opts.MemoryBase
	if dtbAddr < opts.MemoryBase || dtbOff+uint64(len(dtb)) > uint64(len(memory)) {
		return nil, fmt.Errorf("device tree does not fit in guest RAM")
	}
	copy(memory[dtbOff:dtbOff+uint64(len(dtb))], dtb)

	return &BootPlan{
		EntryGPA:      entry,
		KernelEndGPA:  kernelEndGPA,
		DeviceTreeGPA: dtbAddr,
		DeviceTree:    dtb,
	}, nil
}

func loadELF(memory []byte, kernel []byte, memoryBase uint64) (uint64, uint64, error) {
	f, err := elf.NewFile(bytes.NewReader(kernel))
	if err != nil {
		return 0, 0, fmt.Errorf("parse FreeBSD arm64 kernel ELF: %w", err)
	}
	defer f.Close()
	if f.FileHeader.Class != elf.ELFCLASS64 || f.FileHeader.Data != elf.ELFDATA2LSB || f.FileHeader.Machine != elf.EM_AARCH64 {
		return 0, 0, fmt.Errorf("unsupported FreeBSD arm64 kernel ELF class=%v data=%v machine=%v", f.FileHeader.Class, f.FileHeader.Data, f.FileHeader.Machine)
	}
	if f.Entry < kernelBaseVA {
		return 0, 0, fmt.Errorf("unsupported FreeBSD arm64 kernel entry %#x", f.Entry)
	}

	var kernelEndGPA uint64
	for _, prog := range f.Progs {
		if prog.Type != elf.PT_LOAD || prog.Memsz == 0 {
			continue
		}
		if prog.Vaddr < kernelBaseVA {
			continue
		}
		gpa := memoryBase + (prog.Vaddr - kernelBaseVA)
		off := gpa - memoryBase
		if gpa < memoryBase || off+prog.Memsz > uint64(len(memory)) {
			return 0, 0, fmt.Errorf("kernel segment vaddr=%#x memsz=%#x exceeds guest memory %#x", prog.Vaddr, prog.Memsz, len(memory))
		}
		seg := memory[off : off+prog.Memsz]
		clear(seg)
		if prog.Filesz != 0 {
			if _, err := io.ReadFull(prog.Open(), seg[:prog.Filesz]); err != nil {
				return 0, 0, fmt.Errorf("read kernel segment at %#x: %w", prog.Vaddr, err)
			}
		}
		if end := alignUp(gpa+prog.Memsz, pageSize); end > kernelEndGPA {
			kernelEndGPA = end
		}
	}
	if kernelEndGPA == 0 {
		return 0, 0, errors.New("FreeBSD arm64 kernel has no loadable segments")
	}
	entryGPA := memoryBase + (f.Entry - kernelBaseVA)
	return entryGPA, kernelEndGPA, nil
}

type deviceTreeConfig struct {
	MemoryBase uint64
	MemorySize uint64
	NumCPUs    int
	GICVersion GICVersion
	UART       *UARTConfig
	Console    bool
	ExtraNodes []fdt.Node
}

func buildDeviceTree(cfg deviceTreeConfig) ([]byte, error) {
	root := fdt.Node{
		Name: "",
		Properties: map[string]fdt.Property{
			"#address-cells":         {U32: []uint32{2}},
			"#size-cells":            {U32: []uint32{2}},
			"compatible":             {Strings: []string{"ccx3,arm64"}},
			"freebsd,dts-version":    {Strings: []string{"freebsd,15.1"}},
			"model":                  {Strings: []string{"ccx3-arm64"}},
			"interrupt-parent":       {U32: []uint32{gicDefaultPhandle}},
			"serial-number":          {Strings: []string{"ccx3-arm64"}},
			"freebsd,loader-envp":    {Strings: []string{""}},
			"freebsd,loader-version": {Strings: []string{"cc"}},
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
		cpus.Children = append(cpus.Children, fdt.Node{
			Name: fmt.Sprintf("cpu@%d", cpu),
			Properties: map[string]fdt.Property{
				"device_type":   {Strings: []string{"cpu"}},
				"compatible":    {Strings: []string{"arm,armv8"}},
				"reg":           {U64: []uint64{uint64(cpu)}},
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
				"compatible":       {Strings: []string{"ns16550a"}},
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
				"bootargs": {Strings: []string{"FreeBSD: -h"}},
			},
		}
		if cfg.Console {
			chosen.Properties["stdout-path"] = fdt.Property{Strings: []string{fmt.Sprintf("serial0:%dn8", cfg.UART.BaudRate)}}
		}
		root.Children = append(root.Children, chosen)
	}

	root.Children = append(root.Children, fdt.Node{
		Name: "psci",
		Properties: map[string]fdt.Property{
			"compatible": {Strings: []string{"arm,psci-0.2", "arm,psci"}},
			"method":     {Strings: []string{"hvc"}},
		},
	})
	root.Children = append(root.Children, fdt.Node{
		Name: "timer",
		Properties: map[string]fdt.Property{
			"compatible":      {Strings: []string{"arm,armv8-timer"}},
			"always-on":       {Flag: true},
			"interrupt-names": {Strings: []string{"sec-phys", "phys", "virt", "hyp-phys"}},
			"interrupts":      {U32: []uint32{1, 13, 4, 1, 14, 4, 1, 11, 4, 1, 10, 4}},
		},
	})
	root.Children = append(root.Children, fdt.Node{
		Name: fmt.Sprintf("interrupt-controller@%x", defaultGICDistributorBase),
		Properties: map[string]fdt.Property{
			"#interrupt-cells":     {U32: []uint32{3}},
			"#address-cells":       {U32: []uint32{2}},
			"#size-cells":          {U32: []uint32{2}},
			"interrupt-controller": {Flag: true},
			"interrupts":           {U32: []uint32{1, 9, 0xF04}},
			"phandle":              {U32: []uint32{gicDefaultPhandle}},
			"linux,phandle":        {U32: []uint32{gicDefaultPhandle}},
		},
	})
	gicProps := root.Children[len(root.Children)-1].Properties
	if cfg.GICVersion == GICVersionV3 {
		redistributorSize := uint64(defaultGICRedistributorSize) * uint64(cfg.NumCPUs)
		gicProps["compatible"] = fdt.Property{Strings: []string{"arm,gic-v3"}}
		gicProps["reg"] = fdt.Property{U64: []uint64{
			defaultGICDistributorBase, defaultGICDistributorSize,
			defaultGICRedistributorBase, redistributorSize,
		}}
	} else {
		gicProps["compatible"] = fdt.Property{Strings: []string{"arm,gic-400"}}
		gicProps["reg"] = fdt.Property{U64: []uint64{
			defaultGICDistributorBase, defaultGICDistributorSize,
			defaultGICv2CPUInterfaceBase, defaultGICv2CPUInterfaceSize,
		}}
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
