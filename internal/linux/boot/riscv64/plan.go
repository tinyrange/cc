package riscv64

import (
	"fmt"

	"github.com/tinyrange/cc/internal/fdt"
	"github.com/tinyrange/cc/internal/hv"
)

const (
	// DefaultKernelBase is where the kernel is loaded (after OpenSBI)
	DefaultKernelBase uint64 = 0x80200000

	// DefaultDTBBase is where the device tree is placed
	DefaultDTBBase uint64 = 0x82000000

	// DefaultInitrdBase is where the initramfs is placed
	DefaultInitrdBase uint64 = 0x84000000
)

// BootOptions configures how the kernel is loaded.
type BootOptions struct {
	Cmdline         string
	Initrd          []byte
	NumCPUs         int
	DeviceTreeNodes []fdt.Node
}

// BootPlan describes how to boot the kernel.
type BootPlan struct {
	KernelBase  uint64
	KernelSize  uint64
	DTBBase     uint64
	DTBSize     uint64
	InitrdBase  uint64
	InitrdSize  uint64
	EntryPoint  uint64
}

// ConfigureVCPU sets up the vCPU for booting Linux.
func (p *BootPlan) ConfigureVCPU(vcpu hv.VirtualCPU) error {
	// Set a0 = hart id (0)
	// Set a1 = DTB address
	// Set PC = kernel entry point
	regs := map[hv.Register]hv.RegisterValue{
		hv.RegisterRISCVX10: hv.Register64(0),           // a0 = hartid
		hv.RegisterRISCVX11: hv.Register64(p.DTBBase),   // a1 = dtb pointer
		hv.RegisterRISCVPc:  hv.Register64(p.EntryPoint), // PC = kernel entry
	}

	return vcpu.SetRegisters(regs)
}

// Prepare loads the kernel and prepares for booting.
func (k *KernelImage) Prepare(vm hv.VirtualMachine, opts BootOptions) (*BootPlan, error) {
	if vm == nil {
		return nil, fmt.Errorf("vm is nil")
	}

	memBase := vm.MemoryBase()
	memSize := vm.MemorySize()

	// Calculate addresses
	kernelBase := DefaultKernelBase
	if kernelBase < memBase || kernelBase >= memBase+memSize {
		kernelBase = memBase + 0x200000 // 2MB offset from RAM base
	}

	dtbBase := DefaultDTBBase
	initrdBase := DefaultInitrdBase

	// Load kernel
	kernelData := k.Payload()
	if len(kernelData) == 0 {
		return nil, fmt.Errorf("kernel payload is empty")
	}

	if _, err := vm.WriteAt(kernelData, int64(kernelBase)); err != nil {
		return nil, fmt.Errorf("write kernel to guest memory: %w", err)
	}

	// Generate device tree
	fdtData := generateFDT(memBase, memSize, opts.Cmdline, initrdBase, uint64(len(opts.Initrd)), opts.NumCPUs, opts.DeviceTreeNodes)
	if _, err := vm.WriteAt(fdtData, int64(dtbBase)); err != nil {
		return nil, fmt.Errorf("write DTB to guest memory: %w", err)
	}

	// Load initrd if present
	if len(opts.Initrd) > 0 {
		if _, err := vm.WriteAt(opts.Initrd, int64(initrdBase)); err != nil {
			return nil, fmt.Errorf("write initrd to guest memory: %w", err)
		}
	}

	return &BootPlan{
		KernelBase:  kernelBase,
		KernelSize:  uint64(len(kernelData)),
		DTBBase:     dtbBase,
		DTBSize:     uint64(len(fdtData)),
		InitrdBase:  initrdBase,
		InitrdSize:  uint64(len(opts.Initrd)),
		EntryPoint:  kernelBase,
	}, nil
}

// generateFDT creates the device tree for RISC-V Linux boot.
func generateFDT(memBase, memSize uint64, cmdline string, initrdStart, initrdSize uint64, numCPUs int, additionalNodes []fdt.Node) []byte {
	builder := fdt.NewBuilder()

	// Root node
	builder.BeginNode("")
	builder.AddPropertyU32("#address-cells", 2)
	builder.AddPropertyU32("#size-cells", 2)
	builder.AddPropertyString("compatible", "riscv-virtio")
	builder.AddPropertyString("model", "riscv-virtio,qemu")

	// Chosen node
	builder.BeginNode("chosen")
	builder.AddPropertyString("bootargs", cmdline)
	builder.AddPropertyString("stdout-path", "/soc/serial@10000000")
	if initrdSize > 0 {
		builder.AddPropertyU64("linux,initrd-start", initrdStart)
		builder.AddPropertyU64("linux,initrd-end", initrdStart+initrdSize)
	}
	builder.EndNode()

	// CPUs node
	builder.BeginNode("cpus")
	builder.AddPropertyU32("#address-cells", 1)
	builder.AddPropertyU32("#size-cells", 0)
	builder.AddPropertyU32("timebase-frequency", 10000000) // 10 MHz

	if numCPUs <= 0 {
		numCPUs = 1
	}

	for i := 0; i < numCPUs; i++ {
		builder.BeginNode(fmt.Sprintf("cpu@%d", i))
		builder.AddPropertyString("device_type", "cpu")
		builder.AddPropertyU32("reg", uint32(i))
		builder.AddPropertyString("status", "okay")
		builder.AddPropertyString("compatible", "riscv")
		builder.AddPropertyString("riscv,isa", "rv64imafdc_zicsr_zifencei")
		builder.AddPropertyString("mmu-type", "riscv,sv48")

		// CPU interrupt controller
		builder.BeginNode("interrupt-controller")
		builder.AddPropertyU32("#interrupt-cells", 1)
		builder.AddPropertyEmpty("interrupt-controller")
		builder.AddPropertyString("compatible", "riscv,cpu-intc")
		builder.AddPropertyU32("phandle", uint32(i+1))
		builder.EndNode()

		builder.EndNode() // cpu@N
	}
	builder.EndNode() // cpus

	// Memory node
	builder.BeginNode(fmt.Sprintf("memory@%x", memBase))
	builder.AddPropertyString("device_type", "memory")
	builder.AddPropertyU64Pair("reg", memBase, memSize)
	builder.EndNode()

	// SOC node
	builder.BeginNode("soc")
	builder.AddPropertyU32("#address-cells", 2)
	builder.AddPropertyU32("#size-cells", 2)
	builder.AddPropertyStringList("compatible", []string{"simple-bus"})
	builder.AddPropertyEmpty("ranges")

	// CLINT (Core Local Interruptor)
	builder.BeginNode("clint@2000000")
	builder.AddPropertyStringList("compatible", []string{"sifive,clint0", "riscv,clint0"})
	builder.AddPropertyU64Pair("reg", 0x02000000, 0x0000c000)
	// interrupts-extended: one pair per CPU (software int 3, timer int 7)
	var intExt []uint32
	for i := 0; i < numCPUs; i++ {
		intExt = append(intExt, uint32(i+1), 3, uint32(i+1), 7)
	}
	builder.AddPropertyU32Array("interrupts-extended", intExt)
	builder.EndNode()

	// PLIC (Platform Level Interrupt Controller)
	builder.BeginNode("plic@c000000")
	builder.AddPropertyString("compatible", "sifive,plic-1.0.0")
	builder.AddPropertyU32("#interrupt-cells", 1)
	builder.AddPropertyEmpty("interrupt-controller")
	builder.AddPropertyU64Pair("reg", 0x0c000000, 0x04000000)
	// interrupts-extended: external int (9) and supervisor external (11)
	var plicIntExt []uint32
	for i := 0; i < numCPUs; i++ {
		plicIntExt = append(plicIntExt, uint32(i+1), 9, uint32(i+1), 11)
	}
	builder.AddPropertyU32Array("interrupts-extended", plicIntExt)
	builder.AddPropertyU32("riscv,ndev", 127)
	builder.AddPropertyU32("phandle", uint32(numCPUs+1))
	builder.EndNode()

	// UART
	plicPhandle := uint32(numCPUs + 1)
	builder.BeginNode("serial@10000000")
	builder.AddPropertyString("compatible", "ns16550a")
	builder.AddPropertyU64Pair("reg", 0x10000000, 0x00001000)
	builder.AddPropertyU32("clock-frequency", 3686400)
	builder.AddPropertyU32("interrupts", 10)
	builder.AddPropertyU32("interrupt-parent", plicPhandle)
	builder.EndNode()

	// Add any additional nodes (e.g., virtio devices)
	for _, node := range additionalNodes {
		addFDTNode(builder, node)
	}

	builder.EndNode() // soc
	builder.EndNode() // root

	return builder.Build()
}

// addFDTNode adds an fdt.Node to the builder recursively.
func addFDTNode(builder *fdt.Builder, node fdt.Node) {
	builder.BeginNode(node.Name)
	for name, prop := range node.Properties {
		switch {
		case len(prop.Strings) > 0:
			if len(prop.Strings) == 1 {
				builder.AddPropertyString(name, prop.Strings[0])
			} else {
				builder.AddPropertyStringList(name, prop.Strings)
			}
		case len(prop.U32) > 0:
			builder.AddPropertyU32Array(name, prop.U32)
		case len(prop.U64) > 0:
			for i := 0; i < len(prop.U64); i += 2 {
				if i+1 < len(prop.U64) {
					builder.AddPropertyU64Pair(name, prop.U64[i], prop.U64[i+1])
				}
			}
		case len(prop.Bytes) > 0:
			builder.AddPropertyBytes(name, prop.Bytes)
		case prop.Flag:
			builder.AddPropertyEmpty(name)
		default:
			builder.AddPropertyEmpty(name)
		}
	}
	for _, child := range node.Children {
		addFDTNode(builder, child)
	}
	builder.EndNode()
}
