package boot

import (
	"errors"
	"fmt"

	"github.com/tinyrange/cc/internal/hv"
)

// bootOptions parameterise how the kernel is placed into guest RAM.
type bootOptions struct {
	// Cmdline is the kernel command line (without trailing NUL).
	Cmdline string
	// LoadAddr explicitly sets the payload load GPA. If zero, a default is
	// chosen using the kernel's preferred address and alignment.
	LoadAddr uint64
	// Initrd holds the initramfs image to copy into guest RAM.
	Initrd []byte
	// InitrdGPA overrides where the initramfs is copied in guest RAM. If zero,
	// a location is chosen automatically.
	InitrdGPA uint64
	// ZeroPageGPA is where the 4 KiB boot_params ("zero page") will be written.
	// Default: 0x00090000.
	ZeroPageGPA uint64
	// CmdlineGPA is where the NUL-terminated command line will be placed.
	// Default: ZeroPageGPA + 0x1000.
	CmdlineGPA uint64
	// StackTopGPA sets the initial RSP. If zero, it defaults near the top of
	// guest RAM (RAM end - 4 KiB) aligned down to 16 bytes.
	StackTopGPA uint64
	// PagingBase is the GPA used as scratch for the identity-mapped paging
	// structures. Default: 0x00020000.
	PagingBase uint64
	// AddressSpaceGiB controls how much memory (in GiB) is identity mapped by
	// SetupLongMode. Default: 4 GiB.
	AddressSpaceGiB int
	// E820 overrides the BIOS memory map. If empty, a single RAM entry covering
	// the entire allocated memory region is used.
	E820 []E820Entry
}

func (o *bootOptions) withDefaults() bootOptions {
	out := *o
	if out.ZeroPageGPA == 0 {
		out.ZeroPageGPA = 0x00090000
	}
	if out.CmdlineGPA == 0 {
		out.CmdlineGPA = out.ZeroPageGPA + uint64(zeroPageSize)
	}
	if out.PagingBase == 0 {
		out.PagingBase = 0x00020000
	}
	if out.AddressSpaceGiB == 0 {
		out.AddressSpaceGiB = 4
	}
	return out
}

const stackGuardBytes = 0x1000

func defaultE820Map(memStart, memEnd uint64) []E820Entry {
	if memEnd <= memStart {
		return nil
	}

	const (
		pageSize        = 0x1000
		isaMemEnd       = 0x0009f000
		biosRegionStart = 0x000f0000
		biosRegionEnd   = 0x00100000
	)

	memStart = alignDown(memStart, pageSize)
	memEnd = alignDown(memEnd, pageSize)
	if memEnd <= memStart {
		return nil
	}

	min := func(a, b uint64) uint64 {
		if a < b {
			return a
		}
		return b
	}
	max := func(a, b uint64) uint64 {
		if a > b {
			return a
		}
		return b
	}

	var entries []E820Entry

	lowEnd := min(memEnd, isaMemEnd)
	if lowEnd > memStart {
		entries = append(entries, E820Entry{
			Addr: memStart,
			Size: lowEnd - memStart,
			Type: 1,
		})
	}

	if memEnd > isaMemEnd {
		reserveStart := max(isaMemEnd, memStart)
		reserveStart = alignDown(reserveStart, pageSize)
		reserveEnd := min(memEnd, biosRegionEnd)
		reserveEnd = alignDown(reserveEnd, pageSize)
		if reserveEnd > reserveStart {
			entries = append(entries, E820Entry{
				Addr: reserveStart,
				Size: reserveEnd - reserveStart,
				Type: 2,
			})
		}
	}

	highStart := max(biosRegionEnd, memStart)
	highStart = alignUp(highStart, pageSize)
	if memEnd > highStart {
		entries = append(entries, E820Entry{
			Addr: highStart,
			Size: memEnd - highStart,
			Type: 1,
		})
	}

	// Ensure the kernel sees at least two entries; otherwise it falls back to
	// legacy heuristics (see append_e820_table).
	if len(entries) < 2 {
		total := memEnd - memStart
		if total == 0 {
			return entries
		}
		split := alignDown(total/2, pageSize)
		if split == 0 || split >= total {
			// Try to keep the higher region at least one page.
			split = alignDown(total-pageSize, pageSize)
		}
		if split == 0 || split >= total {
			// Fallback: no sensible split, just return single entry.
			return []E820Entry{{
				Addr: memStart,
				Size: total,
				Type: 1,
			}}
		}
		first := E820Entry{Addr: memStart, Size: split, Type: 1}
		second := E820Entry{Addr: memStart + split, Size: total - split, Type: 1}
		return []E820Entry{first, second}
	}

	return entries
}

// BootPlan captures the derived addresses required to hand control to the
// kernel. After Prepare completes successfully, ConfigureVCPU can be used to
// program the first vCPU.
type BootPlan struct {
	LoadAddr        uint64
	EntryGPA        uint64
	ZeroPageGPA     uint64
	CmdlineGPA      uint64
	StackTopGPA     uint64
	PagingBase      uint64
	AddressSpaceGiB int
}

// Prepare loads the kernel payload, builds the zero page and returns the
// derived boot parameters. Call ConfigureVCPU to apply the plan to a vCPU.
func (k *KernelImage) Prepare(vm hv.VirtualMachine, opts bootOptions) (*BootPlan, error) {
	if vm == nil || vm.MemorySize() == 0 {
		return nil, errors.New("memory mapping is nil")
	}
	switch k.format {
	case kernelFormatELF:
		return k.prepareELF(vm, opts)
	default:
		return k.prepareBzImage(vm, opts)
	}
}

func (k *KernelImage) prepareBzImage(vm hv.VirtualMachine, opts bootOptions) (*BootPlan, error) {
	opts = opts.withDefaults()
	var initrdAddr uint64
	var initrdSize uint64

	memStart := vm.MemoryBase()
	memEnd := memStart + vm.MemorySize()

	loadAddr := opts.LoadAddr
	if loadAddr == 0 {
		loadAddr = k.DefaultLoadAddress()
	}

	if k.Header.RelocatableKernel != 0 {
		align := uint64(k.Header.KernelAlignment)
		if align == 0 {
			// Default alignment used historically by bzImage loaders.
			align = 0x200000
		}
		loadAddr = alignUp(loadAddr, align)
	} else if k.Header.PrefAddress != 0 {
		loadAddr = k.Header.PrefAddress
	}

	payload := k.Payload()
	coverage := len(payload)
	if init := int(k.Header.InitSize); init > coverage {
		coverage = init
	}
	if loadAddr < memStart || loadAddr+uint64(coverage) > memEnd {
		return nil, fmt.Errorf("kernel load range [%#x, %#x) outside RAM [%#x, %#x)", loadAddr, loadAddr+uint64(coverage), memStart, memEnd)
	}
	if err := k.LoadIntoMemory(vm, loadAddr); err != nil {
		return nil, err
	}

	if len(opts.Initrd) > 0 {
		if uint64(len(opts.Initrd)) > uint64(^uint32(0)) {
			return nil, fmt.Errorf("initrd too large (%d bytes)", len(opts.Initrd))
		}
		initrdSize = uint64(len(opts.Initrd))
		if opts.InitrdGPA != 0 {
			initrdAddr = opts.InitrdGPA
		} else {
			top := memEnd
			if opts.StackTopGPA != 0 {
				top = opts.StackTopGPA
			}
			if top <= memStart+initrdSize {
				return nil, fmt.Errorf("not enough space to place initrd (%d bytes) below %#x", len(opts.Initrd), top)
			}
			initrdAddr = alignDown(top-initrdSize, 0x1000)
		}
		initrdEnd := initrdAddr + initrdSize
		if initrdAddr < memStart || initrdEnd > memEnd {
			return nil, fmt.Errorf("initrd range [%#x, %#x) outside RAM [%#x, %#x)", initrdAddr, initrdEnd, memStart, memEnd)
		}
		if max := uint64(k.Header.InitrdAddrMax); max != 0 && initrdEnd-1 > max {
			return nil, fmt.Errorf("initrd end %#x exceeds kernel limit %#x", initrdEnd-1, max)
		}
		offset := int(initrdAddr - memStart)
		// copy(mem.Data[offset:offset+len(opts.Initrd)], opts.Initrd)
		if _, err := vm.WriteAt(opts.Initrd, int64(offset)); err != nil {
			return nil, fmt.Errorf("write initrd: %w", err)
		}
	}

	e820 := opts.E820
	if len(e820) == 0 {
		e820 = defaultE820Map(memStart, memEnd)
	}

	if err := k.BuildZeroPage(vm, opts.ZeroPageGPA, loadAddr, opts.Cmdline, opts.CmdlineGPA, initrdAddr, uint32(initrdSize), e820); err != nil {
		return nil, err
	}

	stack := opts.StackTopGPA
	if stack == 0 {
		top := memEnd
		if initrdSize > 0 && initrdAddr > memStart {
			top = initrdAddr
		}
		guard := uint64(stackGuardBytes)
		if top <= memStart+guard*2 {
			return nil, fmt.Errorf("not enough space to place stack (top %#x, base %#x)", top, memStart)
		}
		stack = alignDown(top-guard, 0x10)
	}
	if stack < memStart || stack >= memEnd {
		return nil, fmt.Errorf("stack pointer %#x outside RAM [%#x, %#x)", stack, memStart, memEnd)
	}
	if initrdSize > 0 {
		guard := uint64(stackGuardBytes)
		initrdEnd := initrdAddr + initrdSize
		if stack+guard > initrdAddr {
			return nil, fmt.Errorf("stack pointer %#x + guard %#x overlaps initrd [%#x, %#x)", stack, guard, initrdAddr, initrdEnd)
		}
		if stack >= initrdAddr && stack < initrdEnd {
			return nil, fmt.Errorf("stack pointer %#x inside initrd [%#x, %#x)", stack, initrdAddr, initrdEnd)
		}
	}

	return &BootPlan{
		LoadAddr:        loadAddr,
		EntryGPA:        k.EntryPoint(loadAddr),
		ZeroPageGPA:     opts.ZeroPageGPA,
		CmdlineGPA:      opts.CmdlineGPA,
		StackTopGPA:     stack,
		PagingBase:      opts.PagingBase,
		AddressSpaceGiB: opts.AddressSpaceGiB,
	}, nil
}

func (k *KernelImage) prepareELF(vm hv.VirtualMachine, opts bootOptions) (*BootPlan, error) {
	opts = opts.withDefaults()
	if opts.LoadAddr != 0 {
		return nil, errors.New("ELF kernels do not support overriding load address")
	}

	if err := k.LoadIntoMemory(vm, 0); err != nil {
		return nil, err
	}

	memStart := vm.MemoryBase()
	memEnd := memStart + vm.MemorySize()

	entry := k.EntryPoint(0)
	if entry < memStart || entry >= memEnd {
		return nil, fmt.Errorf("kernel entry %#x outside RAM [%#x, %#x)", entry, memStart, memEnd)
	}

	var initrdAddr uint64
	var initrdSize uint64

	if len(opts.Initrd) > 0 {
		if uint64(len(opts.Initrd)) > uint64(^uint32(0)) {
			return nil, fmt.Errorf("initrd too large (%d bytes)", len(opts.Initrd))
		}
		initrdSize = uint64(len(opts.Initrd))
		if opts.InitrdGPA != 0 {
			initrdAddr = opts.InitrdGPA
		} else {
			top := memEnd
			if opts.StackTopGPA != 0 {
				top = opts.StackTopGPA
			}
			if top <= memStart+initrdSize {
				return nil, fmt.Errorf("not enough space to place initrd (%d bytes) below %#x", len(opts.Initrd), top)
			}
			initrdAddr = alignDown(top-initrdSize, 0x1000)
		}
		initrdEnd := initrdAddr + initrdSize
		if initrdAddr < memStart || initrdEnd > memEnd {
			return nil, fmt.Errorf("initrd range [%#x, %#x) outside RAM [%#x, %#x)", initrdAddr, initrdEnd, memStart, memEnd)
		}
		if max := uint64(k.Header.InitrdAddrMax); max != 0 && initrdEnd-1 > max {
			return nil, fmt.Errorf("initrd end %#x exceeds kernel limit %#x", initrdEnd-1, max)
		}
		offset := int(initrdAddr - memStart)
		if _, err := vm.WriteAt(opts.Initrd, int64(offset)); err != nil {
			return nil, fmt.Errorf("write initrd: %w", err)
		}
	}

	e820 := opts.E820
	if len(e820) == 0 {
		e820 = defaultE820Map(memStart, memEnd)
	}

	if err := k.BuildZeroPage(vm, opts.ZeroPageGPA, entry, opts.Cmdline, opts.CmdlineGPA, initrdAddr, uint32(initrdSize), e820); err != nil {
		return nil, err
	}

	stack := opts.StackTopGPA
	if stack == 0 {
		top := memEnd
		if initrdSize > 0 && initrdAddr > memStart {
			top = initrdAddr
		}
		guard := uint64(stackGuardBytes)
		if top <= memStart+guard*2 {
			return nil, fmt.Errorf("not enough space to place stack (top %#x, base %#x)", top, memStart)
		}
		stack = alignDown(top-guard, 0x10)
	}
	if stack < memStart || stack >= memEnd {
		return nil, fmt.Errorf("stack pointer %#x outside RAM [%#x, %#x)", stack, memStart, memEnd)
	}
	if initrdSize > 0 {
		guard := uint64(stackGuardBytes)
		initrdEnd := initrdAddr + initrdSize
		if stack+guard > initrdAddr {
			return nil, fmt.Errorf("stack pointer %#x + guard %#x overlaps initrd [%#x, %#x)", stack, guard, initrdAddr, initrdEnd)
		}
		if stack >= initrdAddr && stack < initrdEnd {
			return nil, fmt.Errorf("stack pointer %#x inside initrd [%#x, %#x)", stack, initrdAddr, initrdEnd)
		}
	}

	return &BootPlan{
		LoadAddr:        k.Header.PrefAddress,
		EntryGPA:        entry,
		ZeroPageGPA:     opts.ZeroPageGPA,
		CmdlineGPA:      opts.CmdlineGPA,
		StackTopGPA:     stack,
		PagingBase:      opts.PagingBase,
		AddressSpaceGiB: opts.AddressSpaceGiB,
	}, nil
}

// ConfigureVCPU programs the supplied vCPU for a 64-bit Linux handoff using the
// prepared memory layout.
func (p *BootPlan) ConfigureVCPU(vcpu hv.VirtualCPU) error {
	if vcpu == nil {
		return errors.New("memory or vcpu is nil")
	}

	amd64Cpu, ok := vcpu.(hv.VirtualCPUAmd64)
	if !ok {
		return errors.New("vcpu is not amd64")
	}

	if err := amd64Cpu.SetLongModeWithSelectors(p.PagingBase, p.AddressSpaceGiB, 0x10, 0x18); err != nil {
		return fmt.Errorf("setup long mode: %w", err)
	}

	if err := vcpu.SetRegisters(map[hv.Register]hv.RegisterValue{
		hv.RegisterAMD64Rip:    hv.Register64(p.EntryGPA),
		hv.RegisterAMD64Rsi:    hv.Register64(p.ZeroPageGPA),
		hv.RegisterAMD64Rsp:    hv.Register64(p.StackTopGPA),
		hv.RegisterAMD64Rax:    hv.Register64(0),
		hv.RegisterAMD64Rbx:    hv.Register64(0),
		hv.RegisterAMD64Rcx:    hv.Register64(0),
		hv.RegisterAMD64Rdx:    hv.Register64(0),
		hv.RegisterAMD64Rdi:    hv.Register64(0),
		hv.RegisterAMD64Rbp:    hv.Register64(0),
		hv.RegisterAMD64Rflags: hv.Register64(0x2), // reserved bit
	}); err != nil {
		return fmt.Errorf("set registers: %w", err)
	}

	return nil
}
