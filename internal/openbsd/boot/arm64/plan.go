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
	kernelBaseVA  = 0xffffff8000000000
	pageSize      = 4096
	dtbAlignment  = 8
	dtbLoadOffset = 0x100000

	DefaultUARTBase     = 0x09000000
	DefaultUARTSize     = 0x1000
	DefaultUARTClockHz  = 1843200
	DefaultUARTRegShift = 0
	DefaultUARTBaudRate = 115200

	defaultGICDistributorBase           = 0x08000000
	defaultGICDistributorSize           = 0x00010000
	defaultGICv2CPUInterfaceBase        = 0x08010000
	defaultGICv2CPUInterfaceSize        = 0x00002000
	defaultGICITSBase                   = 0x08080000
	defaultGICITSSize                   = 0x00020000
	defaultGICRedistributorBase         = 0x080a0000
	defaultGICRedistributorSize         = 0x00020000
	gicDefaultPhandle            uint32 = 1
	gicITSDefaultPhandle         uint32 = 2
)

const DefaultGICITSPhandle = gicITSDefaultPhandle

type GICVersion int

const (
	GICVersionDefault GICVersion = iota
	GICVersionV2
	GICVersionV3
)

type BootOptions struct {
	MemoryBase                      uint64
	MemorySize                      uint64
	NumCPUs                         int
	GICVersion                      GICVersion
	UART                            *UARTConfig
	Console                         bool
	VirtualTimerPPI                 uint32
	EnableGICITS                    bool
	UsePMRShiftForInterruptPriority bool
	DisableNVMeINTxMasking          bool
	ExtraNodes                      []fdt.Node
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
	KernelEndVA   uint64
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

	entry, kernelEndVA, kernelEndGPA, err := loadELF(memory, kernelFile, opts.MemoryBase)
	if err != nil {
		return nil, err
	}
	if opts.UsePMRShiftForInterruptPriority {
		if err := patchAGINTCPriorityShift(memory, kernelFile); err != nil {
			return nil, err
		}
	}
	if opts.DisableNVMeINTxMasking {
		if err := patchNVMeINTxMasking(memory, kernelFile); err != nil {
			return nil, err
		}
	}
	dtb, err := buildDeviceTree(deviceTreeConfig{
		MemoryBase:      opts.MemoryBase,
		MemorySize:      opts.MemorySize,
		NumCPUs:         opts.NumCPUs,
		GICVersion:      opts.GICVersion,
		UART:            opts.UART,
		Console:         opts.Console,
		VirtualTimerPPI: opts.VirtualTimerPPI,
		EnableGICITS:    opts.EnableGICITS,
		ExtraNodes:      append([]fdt.Node(nil), opts.ExtraNodes...),
	})
	if err != nil {
		return nil, err
	}

	dtbAddr := alignUp(opts.MemoryBase+dtbLoadOffset, dtbAlignment)
	dtbOff := dtbAddr - opts.MemoryBase
	if dtbAddr < opts.MemoryBase || dtbOff+uint64(len(dtb)) > uint64(len(memory)) || dtbOff+uint64(len(dtb)) > 0x200000 {
		return nil, fmt.Errorf("device tree does not fit in guest RAM")
	}
	copy(memory[dtbOff:dtbOff+uint64(len(dtb))], dtb)

	return &BootPlan{
		EntryGPA:      entry,
		KernelEndVA:   kernelEndVA,
		KernelEndGPA:  kernelEndGPA,
		DeviceTreeGPA: dtbAddr,
		DeviceTree:    dtb,
	}, nil
}

func loadELF(memory []byte, kernel []byte, memoryBase uint64) (uint64, uint64, uint64, error) {
	f, err := elf.NewFile(bytes.NewReader(kernel))
	if err != nil {
		return 0, 0, 0, fmt.Errorf("parse OpenBSD arm64 kernel ELF: %w", err)
	}
	defer f.Close()
	if f.FileHeader.Class != elf.ELFCLASS64 || f.FileHeader.Data != elf.ELFDATA2LSB || f.FileHeader.Machine != elf.EM_AARCH64 {
		return 0, 0, 0, fmt.Errorf("unsupported OpenBSD arm64 kernel ELF class=%v data=%v machine=%v", f.FileHeader.Class, f.FileHeader.Data, f.FileHeader.Machine)
	}
	if f.Entry < kernelBaseVA {
		return 0, 0, 0, fmt.Errorf("unsupported OpenBSD arm64 kernel entry %#x", f.Entry)
	}

	var kernelEndVA uint64
	for _, prog := range f.Progs {
		if prog.Type != elf.PT_LOAD {
			continue
		}
		if prog.Vaddr < kernelBaseVA || prog.Memsz == 0 {
			continue
		}
		gpa := memoryBase + (prog.Vaddr - kernelBaseVA)
		off := gpa - memoryBase
		if gpa < memoryBase || off+prog.Memsz > uint64(len(memory)) {
			return 0, 0, 0, fmt.Errorf("kernel segment vaddr=%#x memsz=%#x exceeds guest memory %#x", prog.Vaddr, prog.Memsz, len(memory))
		}
		seg := memory[off : off+prog.Memsz]
		clear(seg)
		if prog.Filesz != 0 {
			if _, err := io.ReadFull(prog.Open(), seg[:prog.Filesz]); err != nil {
				return 0, 0, 0, fmt.Errorf("read kernel segment at %#x: %w", prog.Vaddr, err)
			}
		}
		if end := alignUp(prog.Vaddr+prog.Memsz, pageSize); end > kernelEndVA {
			kernelEndVA = end
		}
	}
	if kernelEndVA == 0 {
		return 0, 0, 0, errors.New("OpenBSD arm64 kernel has no loadable segments")
	}
	entryGPA := memoryBase + (f.Entry - kernelBaseVA)
	kernelEndGPA := memoryBase + (kernelEndVA - kernelBaseVA)
	return entryGPA, kernelEndVA, kernelEndGPA, nil
}

func patchAGINTCPriorityShift(memory []byte, kernel []byte) error {
	f, err := elf.NewFile(bytes.NewReader(kernel))
	if err != nil {
		return fmt.Errorf("parse OpenBSD kernel ELF for agintc priority patch: %w", err)
	}
	syms, err := f.Symbols()
	if err != nil {
		return fmt.Errorf("read OpenBSD kernel symbols for agintc priority patch: %w", err)
	}
	if err := patchAGINTCFunctionPriorityShift(memory, syms, "agintc_set_priority", 96); err != nil {
		return err
	}
	if err := patchAGINTCFunctionPriorityShift(memory, syms, "agintc_calc_irq", 768); err != nil {
		return err
	}
	return nil
}

func patchAGINTCFunctionPriorityShift(memory []byte, syms []elf.Symbol, name string, scanSize uint64) error {
	var symbol *elf.Symbol
	for i := range syms {
		if syms[i].Name == name {
			symbol = &syms[i]
			break
		}
	}
	if symbol == nil {
		return fmt.Errorf("OpenBSD kernel missing %s symbol", name)
	}
	if symbol.Value < kernelBaseVA {
		return fmt.Errorf("OpenBSD %s value %#x below kernel base %#x", name, symbol.Value, uint64(kernelBaseVA))
	}
	start := symbol.Value - kernelBaseVA
	if start >= uint64(len(memory)) {
		return fmt.Errorf("OpenBSD %s offset %#x exceeds guest memory %#x", name, start, len(memory))
	}

	code := memory[start:min(start+scanSize, uint64(len(memory)))]
	old := []byte{0x0a, 0x30, 0x43, 0xb9}     // ldr w10, [x0, #816]  ; sc_prio_shift
	newInst := []byte{0x0a, 0x34, 0x43, 0xb9} // ldr w10, [x0, #820]  ; sc_pmr_shift
	idx := bytes.Index(code, old)
	if idx < 0 {
		return fmt.Errorf("OpenBSD %s patch instruction not found", name)
	}
	if bytes.Index(code[idx+1:], old) >= 0 {
		return fmt.Errorf("OpenBSD %s patch instruction is ambiguous", name)
	}
	copy(code[idx:], newInst)
	return nil
}

func patchNVMeINTxMasking(memory []byte, kernel []byte) error {
	f, err := elf.NewFile(bytes.NewReader(kernel))
	if err != nil {
		return fmt.Errorf("parse OpenBSD kernel ELF for nvme intx patch: %w", err)
	}
	syms, err := f.Symbols()
	if err != nil {
		return fmt.Errorf("read OpenBSD kernel symbols for nvme intx patch: %w", err)
	}
	symbol, err := openBSDKernelSymbol(syms, "nvme_intr_intx")
	if err != nil {
		return err
	}
	start := symbol.Value - kernelBaseVA
	if start >= uint64(len(memory)) {
		return fmt.Errorf("OpenBSD nvme_intr_intx offset %#x exceeds guest memory %#x", start, len(memory))
	}
	code := memory[start:min(start+192, uint64(len(memory)))]
	nop := []byte{0x1f, 0x20, 0x03, 0xd5}
	patterns := [][]byte{
		{
			0x00, 0x2c, 0x40, 0xf9, // ldr x0, [x0, #88]
			0x82, 0x01, 0x80, 0x52, // mov w2, #12 ; NVME_INTMS
			0x61, 0x32, 0x40, 0xf9, // ldr x1, [x19, #96]
			0x23, 0x00, 0x80, 0x52, // mov w3, #1
			0x08, 0x1c, 0x40, 0xf9, // ldr x8, [x0, #56]
			0x00, 0x01, 0x3f, 0xd6, // blr x8
		},
		{
			0x68, 0x86, 0x45, 0xa9, // ldp x8, x1, [x19, #88]
			0x09, 0x00, 0x14, 0x2a, // orr w9, w0, w20
			0x3f, 0x01, 0x00, 0x71, // cmp w9, #0
			0x02, 0x02, 0x80, 0x52, // mov w2, #16 ; NVME_INTMC
			0x23, 0x00, 0x80, 0x52, // mov w3, #1
			0xf3, 0x07, 0x9f, 0x1a, // cset w19, ne
			0x09, 0x1d, 0x40, 0xf9, // ldr x9, [x8, #56]
			0xe0, 0x03, 0x08, 0xaa, // mov x0, x8
			0x20, 0x01, 0x3f, 0xd6, // blr x9
		},
	}
	for _, pattern := range patterns {
		idx := bytes.Index(code, pattern)
		if idx < 0 {
			return fmt.Errorf("OpenBSD nvme_intr_intx patch pattern not found")
		}
		if bytes.Index(code[idx+1:], pattern) >= 0 {
			return fmt.Errorf("OpenBSD nvme_intr_intx patch pattern is ambiguous")
		}
		copy(code[idx+len(pattern)-4:], nop)
	}
	return nil
}

func openBSDKernelSymbol(syms []elf.Symbol, name string) (*elf.Symbol, error) {
	for i := range syms {
		if syms[i].Name != name {
			continue
		}
		if syms[i].Value < kernelBaseVA {
			return nil, fmt.Errorf("OpenBSD %s value %#x below kernel base %#x", name, syms[i].Value, uint64(kernelBaseVA))
		}
		return &syms[i], nil
	}
	return nil, fmt.Errorf("OpenBSD kernel missing %s symbol", name)
}

type deviceTreeConfig struct {
	MemoryBase      uint64
	MemorySize      uint64
	NumCPUs         int
	GICVersion      GICVersion
	UART            *UARTConfig
	Console         bool
	VirtualTimerPPI uint32
	EnableGICITS    bool
	ExtraNodes      []fdt.Node
}

func buildDeviceTree(cfg deviceTreeConfig) ([]byte, error) {
	root := fdt.Node{
		Name: "",
		Properties: map[string]fdt.Property{
			"#address-cells":   {U32: []uint32{2}},
			"#size-cells":      {U32: []uint32{2}},
			"compatible":       {Strings: []string{"ccx3,arm64"}},
			"model":            {Strings: []string{"ccx3-arm64"}},
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
		if cfg.UART.Interrupt != (InterruptSpec{}) {
			serialNode.Properties["interrupts"] = fdt.Property{U32: []uint32{
				cfg.UART.Interrupt.Type, cfg.UART.Interrupt.Num, cfg.UART.Interrupt.Flags,
			}}
			serialNode.Properties["interrupt-parent"] = fdt.Property{U32: []uint32{gicDefaultPhandle}}
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
				"bootargs": {Strings: []string{"bsd -"}},
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
			"compatible": {Strings: []string{"arm,psci-0.2", "arm,psci"}},
			"method":     {Strings: []string{"hvc"}},
		},
	})
	virtualTimerPPI := cfg.VirtualTimerPPI
	if virtualTimerPPI == 0 {
		virtualTimerPPI = 11
	}
	root.Children = append(root.Children, fdt.Node{
		Name: "timer",
		Properties: map[string]fdt.Property{
			"compatible": {Strings: []string{"arm,armv8-timer"}},
			"always-on":  {Flag: true},
			"interrupts": {U32: []uint32{1, 13, 4, 1, 14, 4, 1, virtualTimerPPI, 4, 1, 10, 4}},
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
		if cfg.EnableGICITS {
			root.Children[len(root.Children)-1].Children = append(root.Children[len(root.Children)-1].Children, fdt.Node{
				Name: fmt.Sprintf("msi-controller@%x", defaultGICITSBase),
				Properties: map[string]fdt.Property{
					"compatible":     {Strings: []string{"arm,gic-v3-its"}},
					"msi-controller": {Flag: true},
					"#msi-cells":     {U32: []uint32{1}},
					"phandle":        {U32: []uint32{gicITSDefaultPhandle}},
					"linux,phandle":  {U32: []uint32{gicITSDefaultPhandle}},
					"reg":            {U64: []uint64{defaultGICITSBase, defaultGICITSSize}},
				},
			})
		}
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
