package amd64

import (
	"errors"
	"fmt"
	"math"

	"github.com/tinyrange/cc/internal/hv"
)

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

// DefaultLoadAddress returns a reasonable load address for the kernel payload.
// Preference order:
//  1. setup_header.pref_address if non-zero
//  2. 1 MiB if LOAD_HIGH flag set
//  3. 64 KiB otherwise
func (k *KernelImage) DefaultLoadAddress() uint64 {
	if k.format == kernelFormatELF {
		if k.Header.PrefAddress != 0 {
			return k.Header.PrefAddress
		}
		return k.elfMinPhys
	}
	if k.Header.PrefAddress != 0 {
		return k.Header.PrefAddress
	}
	if k.Header.LoadFlags&0x1 != 0 {
		return 0x00100000
	}
	return 0x00010000
}

// LoadIntoMemory copies the kernel payload into guest RAM at loadAddr. The
// range [loadAddr, loadAddr+max(init_size, len(payload))) is cleared before the
// payload is copied to satisfy the kernel's expectation of zeroed memory.
func (k *KernelImage) LoadIntoMemory(vm hv.VirtualMachine, loadAddr uint64) error {
	if vm == nil || vm.MemorySize() == 0 {
		return errors.New("memory mapping is nil")
	}
	if k.format == kernelFormatELF {
		return k.loadELFSegments(vm)
	}
	payload := k.Payload()
	memStart := vm.MemoryBase()
	memEnd := memStart + vm.MemorySize()
	if loadAddr < memStart {
		return fmt.Errorf("load address %#x below RAM base %#x", loadAddr, memStart)
	}
	clearLen := len(payload)
	if init := int(k.Header.InitSize); init > clearLen {
		clearLen = init
	}
	if loadAddr+uint64(clearLen) > memEnd {
		return fmt.Errorf("kernel requires %#x bytes at %#x but RAM ends at %#x", clearLen, loadAddr, memEnd)
	}
	if loadAddr > math.MaxInt64 {
		return fmt.Errorf("load address %#x out of host range", loadAddr)
	}
	clear := make([]byte, clearLen)
	if _, err := vm.WriteAt(clear, int64(loadAddr)); err != nil {
		return fmt.Errorf("clear kernel memory: %w", err)
	}
	if _, err := vm.WriteAt(payload, int64(loadAddr)); err != nil {
		return fmt.Errorf("write kernel payload: %w", err)
	}
	return nil
}

// EntryPoint returns the 64-bit entry point GPA when the payload is loaded at
// loadAddr. The Linux boot protocol places the 64-bit entry at load+0x200.
func (k *KernelImage) EntryPoint(loadAddr uint64) uint64 {
	if k.format == kernelFormatELF {
		return k.elfEntry
	}
	return loadAddr + 0x200
}

func (k *KernelImage) loadELFSegments(vm hv.VirtualMachine) error {
	memStart := vm.MemoryBase()
	memEnd := memStart + vm.MemorySize()
	for _, seg := range k.elfSegments {
		if seg.memSize == 0 {
			continue
		}
		start := seg.physAddr
		end := start + seg.memSize
		if start < memStart || end > memEnd {
			return fmt.Errorf("ELF segment [%#x, %#x) outside RAM [%#x, %#x)", start, end, memStart, memEnd)
		}
		if start > math.MaxInt64 {
			return fmt.Errorf("ELF segment start %#x out of host range", start)
		}
		regionLen := int(seg.memSize)
		if uint64(regionLen) != seg.memSize {
			return fmt.Errorf("ELF segment size %#x exceeds host limits", seg.memSize)
		}
		region := make([]byte, regionLen)
		if _, err := vm.WriteAt(region, int64(start)); err != nil {
			return fmt.Errorf("WriteAt zeroing ELF segment memory: %w", err)
		}
		if seg.fileSize > 0 {
			fileSize := int(seg.fileSize)
			if _, err := vm.WriteAt(seg.data[:fileSize], int64(start)); err != nil {
				return fmt.Errorf("WriteAt ELF segment data: %w", err)
			}
		}
	}
	return nil
}
