package boot

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/tinyrange/cc/internal/hv"
)

const (
	e820EntrySize             = 20
	e820MaxEntries            = 128
	typeOfLoaderUnknown uint8 = 0xff
	canUseHeapFlag      uint8 = 1 << 7
)

// E820Entry describes a single BIOS e820 memory map entry.
type E820Entry struct {
	Addr uint64
	Size uint64
	Type uint32
}

// BuildZeroPage populates the boot_params and supporting command line in guest
// memory according to the Linux x86_64 boot protocol.
func (k *KernelImage) BuildZeroPage(vm hv.VirtualMachine, zeroPageGPA, loadAddr uint64, cmdline string, cmdlineGPA uint64, initrdGPA uint64, initrdSize uint32, e820 []E820Entry) error {
	if vm == nil {
		return errors.New("memory mapping is nil")
	}
	if zeroPageGPA < vm.MemoryBase() {
		return fmt.Errorf("zero page GPA %#x below memory base %#x", zeroPageGPA, vm.MemoryBase())
	}
	zpOff := int(zeroPageGPA - vm.MemoryBase())
	if zpOff < 0 || zpOff+zeroPageSize > int(vm.MemorySize()) {
		return fmt.Errorf("zero page GPA %#x outside allocated memory", zeroPageGPA)
	}

	zp := make([]byte, zeroPageSize)

	if len(k.HeaderBytes) > zeroPageSize-setupHeaderOffset {
		return errors.New("setup header larger than zero page space")
	}
	if len(k.HeaderBytes) > 0 {
		copy(zp[setupHeaderOffset:], k.HeaderBytes)
	}

	binary.LittleEndian.PutUint16(zp[setupHeaderBootFlagOffset:], 0xaa55)
	copy(zp[setupHeaderHeaderOffset:], []byte(headerMagic))
	binary.LittleEndian.PutUint16(zp[protocolVersionOffset:], k.Header.ProtocolVersion)
	zp[loadFlagsOffset] = k.Header.LoadFlags
	binary.LittleEndian.PutUint32(zp[kernelAlignmentOffset:], k.Header.KernelAlignment)
	zp[relocatableKernelOffset] = k.Header.RelocatableKernel
	zp[minAlignmentOffset] = k.Header.MinAlignment
	binary.LittleEndian.PutUint16(zp[xloadflagsOffset:], k.Header.XLoadFlags)
	binary.LittleEndian.PutUint32(zp[cmdlineSizeOffset:], k.Header.CmdlineSize)
	binary.LittleEndian.PutUint32(zp[initrdAddrMaxOffset:], k.Header.InitrdAddrMax)
	binary.LittleEndian.PutUint64(zp[prefAddressOffset:], k.Header.PrefAddress)
	binary.LittleEndian.PutUint32(zp[initSizeOffset:], k.Header.InitSize)

	zp[typeOfLoaderOffset] = typeOfLoaderUnknown

	loadFlags := zp[loadFlagsOffset] | canUseHeapFlag
	zp[loadFlagsOffset] = loadFlags

	heapEnd := uint16(0x9800)
	if loadFlags&0x1 != 0 {
		heapEnd = 0xe000
	}
	binary.LittleEndian.PutUint16(zp[heapEndPtrOffset:], heapEnd-0x200)

	if loadAddr > 0xffffffff {
		return fmt.Errorf("load address %#x exceeds 32-bit range", loadAddr)
	}
	binary.LittleEndian.PutUint32(zp[code32StartOffset:], uint32(loadAddr))

	// Command line pointer (32-bit low, ext pointer high for >=4G).
	binary.LittleEndian.PutUint32(zp[cmdLinePtrOffset:], uint32(cmdlineGPA))
	binary.LittleEndian.PutUint32(zp[zeroPageExtCmdLinePtr:], uint32(cmdlineGPA>>32))

	if initrdSize > 0 {
		if initrdGPA == 0 {
			return errors.New("non-zero initrd size but GPA is zero")
		}
		binary.LittleEndian.PutUint32(zp[ramdiskImageOffset:], uint32(initrdGPA))
		binary.LittleEndian.PutUint32(zp[ramdiskSizeOffset:], initrdSize)
		binary.LittleEndian.PutUint32(zp[zeroPageExtRamDiskImage:], uint32(initrdGPA>>32))
		binary.LittleEndian.PutUint32(zp[zeroPageExtRamDiskSize:], uint32(uint64(initrdSize)>>32))
	}

	if k.Header.CmdlineSize != 0 && len(cmdline) > int(k.Header.CmdlineSize) {
		return fmt.Errorf("command line length %d exceeds kernel limit %d", len(cmdline), k.Header.CmdlineSize)
	}
	if err := placeCmdline(vm, cmdlineGPA, cmdline); err != nil {
		return err
	}

	if len(e820) == 0 {
		return errors.New("e820 map must contain at least one entry")
	}
	if len(e820) > e820MaxEntries {
		return fmt.Errorf("too many e820 entries (%d > %d)", len(e820), e820MaxEntries)
	}
	zp[zeroPageE820Entries] = byte(len(e820))

	for idx, ent := range e820 {
		base := zeroPageE820Table + idx*e820EntrySize
		if base+e820EntrySize > zeroPageSize {
			return errors.New("e820 table exceeds zero page size")
		}
		binary.LittleEndian.PutUint64(zp[base:], ent.Addr)
		binary.LittleEndian.PutUint64(zp[base+8:], ent.Size)
		binary.LittleEndian.PutUint32(zp[base+16:], ent.Type)
	}

	if _, err := vm.WriteAt(zp, int64(zpOff)); err != nil {
		return fmt.Errorf("WriteAt zero page: %w", err)
	}
	return nil
}

func placeCmdline(vm hv.VirtualMachine, cmdlineGPA uint64, cmdline string) error {
	if cmdlineGPA < vm.MemoryBase() {
		return fmt.Errorf("cmdline GPA %#x below memory base %#x", cmdlineGPA, vm.MemoryBase())
	}
	offset := int(cmdlineGPA - vm.MemoryBase())
	need := len(cmdline) + 1
	if offset < 0 || offset+need > int(vm.MemorySize()) {
		return fmt.Errorf("command line does not fit in guest memory (need %d bytes)", need)
	}
	cmdlineBytes := append([]byte(cmdline), 0)
	if _, err := vm.WriteAt(cmdlineBytes, int64(offset)); err != nil {
		return fmt.Errorf("WriteAt command line: %w", err)
	}
	return nil
}
