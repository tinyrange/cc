package amd64

import (
	"debug/elf"
	"errors"
	"fmt"
	"io"
	"math"
)

const (
	defaultELFCmdlineSize  = 4096
	defaultELFInitrdMax    = 0x37ffffff
	defaultELFKernelAlign  = 0x200000
	elf64EntryFlagXLF64Bit = 0x1
)

func loadELFKernel(kernel io.ReaderAt) (*KernelImage, error) {
	f, err := elf.NewFile(kernel)
	if err != nil {
		return nil, fmt.Errorf("open elf kernel: %w", err)
	}
	defer f.Close()

	if f.Machine != elf.EM_X86_64 {
		return nil, fmt.Errorf("unsupported ELF machine %d (want x86_64)", f.Machine)
	}
	if len(f.Progs) == 0 {
		return nil, errors.New("ELF kernel has no program headers")
	}

	var segments []elfSegment
	var minPhys uint64
	var maxPhys uint64
	var maxAlign uint64
	for _, prog := range f.Progs {
		if prog.Type != elf.PT_LOAD {
			continue
		}
		if prog.Memsz == 0 {
			continue
		}
		if prog.Filesz > prog.Memsz {
			return nil, fmt.Errorf("ELF segment file size %#x exceeds mem size %#x", prog.Filesz, prog.Memsz)
		}
		if prog.Filesz > uint64(math.MaxInt) {
			return nil, fmt.Errorf("ELF segment file size %#x exceeds host limits", prog.Filesz)
		}
		if prog.Memsz > uint64(math.MaxInt) {
			return nil, fmt.Errorf("ELF segment mem size %#x exceeds host limits", prog.Memsz)
		}
		data := make([]byte, int(prog.Filesz))
		if prog.Filesz > 0 {
			if _, err := prog.ReadAt(data, 0); err != nil {
				return nil, fmt.Errorf("read ELF segment @%#x: %w", prog.Off, err)
			}
		}
		seg := elfSegment{
			physAddr: prog.Paddr,
			fileSize: prog.Filesz,
			memSize:  prog.Memsz,
			data:     data,
		}
		segments = append(segments, seg)
		if minPhys == 0 || prog.Paddr < minPhys {
			minPhys = prog.Paddr
		}
		if end := prog.Paddr + prog.Memsz; end > maxPhys {
			maxPhys = end
		}
		if prog.Align > maxAlign {
			maxAlign = prog.Align
		}
	}

	if len(segments) == 0 {
		return nil, errors.New("ELF kernel has no loadable segments")
	}

	if maxAlign == 0 {
		maxAlign = defaultELFKernelAlign
	}
	kernelAlign := maxAlign
	if kernelAlign > math.MaxUint32 {
		kernelAlign = defaultELFKernelAlign
	}

	if minPhys == 0 {
		// Linux kernels are normally linked away from zero. Bail out instead of
		// trying to guess a relocation scheme.
		return nil, errors.New("ELF kernel min physical address is zero")
	}

	if maxPhys <= minPhys {
		return nil, fmt.Errorf("invalid ELF kernel span [%#x, %#x)", minPhys, maxPhys)
	}

	span := maxPhys - minPhys
	if span > math.MaxUint32 {
		return nil, fmt.Errorf("ELF kernel span %#x exceeds 4GiB limit", span)
	}

	entry := f.Entry
	if entry == 0 {
		return nil, errors.New("ELF kernel entry point is zero")
	}
	if entry < minPhys || entry >= maxPhys {
		return nil, fmt.Errorf("ELF entry %#x outside loaded span [%#x, %#x)", entry, minPhys, maxPhys)
	}

	header := SetupHeader{
		ProtocolVersion:   0x020b,
		LoadFlags:         0x1, // LOADED_HIGH
		InitrdAddrMax:     defaultELFInitrdMax,
		KernelAlignment:   uint32(kernelAlign),
		RelocatableKernel: 0,
		XLoadFlags:        elf64EntryFlagXLF64Bit,
		CmdlineSize:       defaultELFCmdlineSize,
		PrefAddress:       minPhys,
		InitSize:          uint32(span),
	}
	if f.Type == elf.ET_DYN {
		header.RelocatableKernel = 1
	}

	return &KernelImage{
		format:      kernelFormatELF,
		Header:      header,
		elfSegments: segments,
		elfEntry:    entry,
		elfMinPhys:  minPhys,
		elfMaxPhys:  maxPhys,
	}, nil
}
