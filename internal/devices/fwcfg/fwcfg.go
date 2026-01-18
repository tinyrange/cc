// Package fwcfg implements the QEMU fw_cfg device for firmware configuration.
package fwcfg

import (
	"encoding/binary"
	"fmt"
	"log/slog"
	"sort"
	"sync"

	"github.com/tinyrange/cc/internal/chipset"
	"github.com/tinyrange/cc/internal/hv"
)

// fw_cfg MMIO register offsets (MMIO transport)
const (
	FW_CFG_DATA     = 0x00 // Data register (8-bit read, multi-byte read)
	FW_CFG_SELECTOR = 0x08 // Selector register (16-bit write)
	FW_CFG_DMA_ADDR = 0x10 // DMA address register (64-bit write, big-endian)
)

// fw_cfg selectors
const (
	FW_CFG_SIGNATURE   = 0x0000
	FW_CFG_ID          = 0x0001
	FW_CFG_UUID        = 0x0002
	FW_CFG_RAM_SIZE    = 0x0003
	FW_CFG_NOGRAPHIC   = 0x0004
	FW_CFG_NB_CPUS     = 0x0005
	FW_CFG_MACHINE_ID  = 0x0006
	FW_CFG_KERNEL_ADDR = 0x0007
	FW_CFG_KERNEL_SIZE = 0x0008
	FW_CFG_KERNEL_CMD  = 0x0009
	FW_CFG_INITRD_ADDR = 0x000a
	FW_CFG_INITRD_SIZE = 0x000b
	FW_CFG_BOOT_MENU   = 0x000e
	FW_CFG_FILE_DIR    = 0x0019
	FW_CFG_FILE_FIRST  = 0x0020
)

// fw_cfg DMA control bits
const (
	FW_CFG_DMA_CTL_ERROR  = 1 << 0
	FW_CFG_DMA_CTL_READ   = 1 << 1
	FW_CFG_DMA_CTL_SKIP   = 1 << 2
	FW_CFG_DMA_CTL_SELECT = 1 << 3
	FW_CFG_DMA_CTL_WRITE  = 1 << 4
)

// fw_cfg ID bits
const (
	FW_CFG_VERSION     = 1 << 0
	FW_CFG_VERSION_DMA = 1 << 1
)

// Default base address and size
const (
	DefaultBase = 0x09020000
	DefaultSize = 0x1000
)

// fwCfgFile represents a file in the fw_cfg directory.
type fwCfgFile struct {
	name     string
	selector uint16
	data     []byte
	onWrite  func(data []byte) error
}

// FwCfgDmaAccess represents the DMA access structure.
type FwCfgDmaAccess struct {
	Control uint32
	Length  uint32
	Address uint64
}

// FwCfg implements the QEMU fw_cfg device.
type FwCfg struct {
	mu sync.Mutex
	vm hv.VirtualMachine

	base uint64
	size uint64

	// Current state
	selector   uint16
	dataOffset uint32

	// DMA state - accumulates high 32 bits until low bits are written
	dmaAddrHigh uint32

	// Files indexed by selector
	files          map[uint16]*fwCfgFile
	filesByName    map[string]*fwCfgFile
	nextFileSelect uint16

	// Pre-computed file directory
	fileDir []byte
}

// New creates a new fw_cfg device at the given base address.
func New(base uint64) *FwCfg {
	f := &FwCfg{
		base:           base,
		size:           DefaultSize,
		files:          make(map[uint16]*fwCfgFile),
		filesByName:    make(map[string]*fwCfgFile),
		nextFileSelect: FW_CFG_FILE_FIRST,
	}
	return f
}

// NewDefault creates a new fw_cfg device at the default base address.
func NewDefault() *FwCfg {
	return New(DefaultBase)
}

// AddFile registers a file with the fw_cfg device.
// The file can be read by the guest using the assigned selector.
func (f *FwCfg) AddFile(name string, data []byte) uint16 {
	return f.AddFileWithCallback(name, data, nil)
}

// AddFileWithCallback registers a file that triggers a callback when written via DMA.
func (f *FwCfg) AddFileWithCallback(name string, data []byte, onWrite func(data []byte) error) uint16 {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Check if file already exists
	if existing, ok := f.filesByName[name]; ok {
		existing.data = data
		existing.onWrite = onWrite
		f.rebuildFileDir()
		return existing.selector
	}

	selector := f.nextFileSelect
	f.nextFileSelect++

	file := &fwCfgFile{
		name:     name,
		selector: selector,
		data:     data,
		onWrite:  onWrite,
	}
	f.files[selector] = file
	f.filesByName[name] = file

	f.rebuildFileDir()
	return selector
}

// rebuildFileDir rebuilds the file directory data structure.
// Must be called with lock held.
func (f *FwCfg) rebuildFileDir() {
	// File directory format:
	// uint32_be count
	// For each file:
	//   uint32_be size
	//   uint16_be selector
	//   uint16_be reserved
	//   char name[56]

	count := len(f.files)
	f.fileDir = make([]byte, 4+count*64)

	binary.BigEndian.PutUint32(f.fileDir[0:4], uint32(count))

	// Sort files by selector for consistent ordering
	var selectors []uint16
	for sel := range f.files {
		selectors = append(selectors, sel)
	}
	sort.Slice(selectors, func(i, j int) bool { return selectors[i] < selectors[j] })

	offset := 4
	for _, sel := range selectors {
		file := f.files[sel]
		binary.BigEndian.PutUint32(f.fileDir[offset:offset+4], uint32(len(file.data)))
		binary.BigEndian.PutUint16(f.fileDir[offset+4:offset+6], file.selector)
		binary.BigEndian.PutUint16(f.fileDir[offset+6:offset+8], 0) // reserved
		// Copy name, truncate to 55 chars + null terminator
		nameBytes := []byte(file.name)
		if len(nameBytes) > 55 {
			nameBytes = nameBytes[:55]
		}
		copy(f.fileDir[offset+8:offset+64], nameBytes)
		offset += 64
	}
}

// Init implements hv.Device.
func (f *FwCfg) Init(vm hv.VirtualMachine) error {
	f.vm = vm
	return nil
}

// Start implements chipset.ChangeDeviceState.
func (f *FwCfg) Start() error {
	return nil
}

// Stop implements chipset.ChangeDeviceState.
func (f *FwCfg) Stop() error {
	return nil
}

// Reset implements chipset.ChangeDeviceState.
func (f *FwCfg) Reset() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.selector = 0
	f.dataOffset = 0
	return nil
}

// SupportsPortIO implements chipset.ChipsetDevice.
func (f *FwCfg) SupportsPortIO() *chipset.PortIOIntercept {
	return nil
}

// SupportsMmio implements chipset.ChipsetDevice.
func (f *FwCfg) SupportsMmio() *chipset.MmioIntercept {
	return &chipset.MmioIntercept{
		Regions: []hv.MMIORegion{
			{
				Address: f.base,
				Size:    f.size,
			},
		},
		Handler: f,
	}
}

// SupportsPollDevice implements chipset.ChipsetDevice.
func (f *FwCfg) SupportsPollDevice() *chipset.PollDevice {
	return nil
}

// ReadMMIO implements chipset.MmioHandler.
func (f *FwCfg) ReadMMIO(ctx hv.ExitContext, addr uint64, data []byte) error {
	if addr < f.base || addr+uint64(len(data)) > f.base+f.size {
		return fmt.Errorf("fwcfg: address 0x%x out of bounds", addr)
	}

	offset := addr - f.base

	switch offset {
	case FW_CFG_DATA, FW_CFG_DATA + 1, FW_CFG_DATA + 2, FW_CFG_DATA + 3,
		FW_CFG_DATA + 4, FW_CFG_DATA + 5, FW_CFG_DATA + 6, FW_CFG_DATA + 7:
		// Multi-byte data read
		err := f.readData(data)
		// slog.Info("fwcfg ReadMMIO data", "offset", fmt.Sprintf("0x%x", offset), "len", len(data), "data", fmt.Sprintf("%x", data))
		return err

	case FW_CFG_SELECTOR:
		f.mu.Lock()
		sel := f.selector
		f.mu.Unlock()
		if len(data) >= 2 {
			binary.LittleEndian.PutUint16(data, sel)
		} else if len(data) == 1 {
			data[0] = byte(sel)
		}
		slog.Info("fwcfg ReadMMIO selector", "selector", fmt.Sprintf("0x%x", sel))
		return nil

	default:
		// Unknown register, return zeros
		for i := range data {
			data[i] = 0
		}
		slog.Info("fwcfg ReadMMIO unknown", "offset", fmt.Sprintf("0x%x", offset))
		return nil
	}
}

// WriteMMIO implements chipset.MmioHandler.
func (f *FwCfg) WriteMMIO(ctx hv.ExitContext, addr uint64, data []byte) error {
	if addr < f.base || addr+uint64(len(data)) > f.base+f.size {
		return fmt.Errorf("fwcfg: address 0x%x out of bounds", addr)
	}

	offset := addr - f.base
	slog.Info("fwcfg WriteMMIO", "offset", fmt.Sprintf("0x%x", offset), "len", len(data))

	switch offset {
	case FW_CFG_SELECTOR:
		if len(data) >= 2 {
			f.mu.Lock()
			f.selector = binary.LittleEndian.Uint16(data)
			f.dataOffset = 0
			slog.Info("fwcfg selector set", "selector", fmt.Sprintf("0x%x", f.selector))
			f.mu.Unlock()
		}
		return nil

	case FW_CFG_DMA_ADDR:
		// 64-bit DMA address (big-endian) or high 32 bits
		slog.Info("fwcfg DMA_ADDR write", "len", len(data), "data", fmt.Sprintf("%x", data))
		if len(data) == 8 {
			dmaAddr := binary.BigEndian.Uint64(data)
			slog.Info("fwcfg DMA_ADDR 64-bit", "addr", fmt.Sprintf("0x%x", dmaAddr))
			return f.handleDma(dmaAddr)
		} else if len(data) == 4 {
			// High 32 bits - store and wait for low bits
			f.mu.Lock()
			f.dmaAddrHigh = binary.BigEndian.Uint32(data)
			f.mu.Unlock()
			slog.Info("fwcfg DMA_ADDR high 32-bits stored", "high", fmt.Sprintf("0x%x", f.dmaAddrHigh))
			return nil
		}
		return nil

	case FW_CFG_DMA_ADDR + 4:
		// Low 32 bits of DMA address - combine with high bits and trigger DMA
		if len(data) == 4 {
			f.mu.Lock()
			high := f.dmaAddrHigh
			f.mu.Unlock()
			low := binary.BigEndian.Uint32(data)
			dmaAddr := (uint64(high) << 32) | uint64(low)
			slog.Info("fwcfg DMA_ADDR+4 low 32-bits, triggering DMA", "high", fmt.Sprintf("0x%x", high), "low", fmt.Sprintf("0x%x", low), "addr", fmt.Sprintf("0x%x", dmaAddr))
			return f.handleDma(dmaAddr)
		}
		return nil

	default:
		slog.Info("fwcfg WriteMMIO unknown", "offset", fmt.Sprintf("0x%x", offset))
		return nil
	}
}

// readData reads data from the currently selected item.
func (f *FwCfg) readData(data []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	itemData := f.getSelectedData()
	for i := range data {
		if f.dataOffset < uint32(len(itemData)) {
			data[i] = itemData[f.dataOffset]
			f.dataOffset++
		} else {
			data[i] = 0
		}
	}
	return nil
}

// getSelectedData returns the data for the currently selected item.
// Must be called with lock held.
func (f *FwCfg) getSelectedData() []byte {
	switch f.selector {
	case FW_CFG_SIGNATURE:
		return []byte("QEMU")

	case FW_CFG_ID:
		// Return version with DMA support
		var buf [4]byte
		binary.LittleEndian.PutUint32(buf[:], FW_CFG_VERSION|FW_CFG_VERSION_DMA)
		return buf[:]

	case FW_CFG_FILE_DIR:
		return f.fileDir

	default:
		// Check for file
		if file, ok := f.files[f.selector]; ok {
			return file.data
		}
		return nil
	}
}

// handleDma handles a DMA request.
func (f *FwCfg) handleDma(dmaAddr uint64) error {
	if f.vm == nil {
		return fmt.Errorf("fwcfg: VM not initialized")
	}

	// Read DMA access structure (16 bytes, big-endian)
	var buf [16]byte
	n, err := f.vm.ReadAt(buf[:], int64(dmaAddr))
	if err != nil || n != 16 {
		return fmt.Errorf("fwcfg: failed to read DMA structure: %v", err)
	}

	dma := FwCfgDmaAccess{
		Control: binary.BigEndian.Uint32(buf[0:4]),
		Length:  binary.BigEndian.Uint32(buf[4:8]),
		Address: binary.BigEndian.Uint64(buf[8:16]),
	}

	slog.Info("fwcfg DMA", "control", fmt.Sprintf("0x%x", dma.Control), "length", dma.Length, "address", fmt.Sprintf("0x%x", dma.Address))

	f.mu.Lock()

	// Handle select
	if dma.Control&FW_CFG_DMA_CTL_SELECT != 0 {
		newSelector := uint16(dma.Control >> 16)
		slog.Info("fwcfg DMA select", "selector", fmt.Sprintf("0x%x", newSelector))
		f.selector = newSelector
		f.dataOffset = 0
	}

	var resultControl uint32 = 0

	if dma.Control&FW_CFG_DMA_CTL_READ != 0 {
		// Read operation
		itemData := f.getSelectedData()
		length := dma.Length
		remaining := uint32(0)
		if f.dataOffset < uint32(len(itemData)) {
			remaining = uint32(len(itemData)) - f.dataOffset
		}
		if length > remaining {
			length = remaining
		}

		if length > 0 {
			f.mu.Unlock()
			_, err := f.vm.WriteAt(itemData[f.dataOffset:f.dataOffset+length], int64(dma.Address))
			f.mu.Lock()
			if err != nil {
				resultControl = FW_CFG_DMA_CTL_ERROR
			} else {
				f.dataOffset += length
			}
		}

		// Zero-fill remaining requested bytes
		if dma.Length > length {
			zeros := make([]byte, dma.Length-length)
			f.mu.Unlock()
			f.vm.WriteAt(zeros, int64(dma.Address+uint64(length)))
			f.mu.Lock()
		}
	} else if dma.Control&FW_CFG_DMA_CTL_WRITE != 0 {
		// Write operation (for ramfb etc.)
		slog.Info("fwcfg DMA write", "selector", fmt.Sprintf("0x%x", f.selector), "length", dma.Length)
		if file, ok := f.files[f.selector]; ok && file.onWrite != nil {
			writeData := make([]byte, dma.Length)
			f.mu.Unlock()
			n, err := f.vm.ReadAt(writeData, int64(dma.Address))
			f.mu.Lock()
			if err != nil || n != int(dma.Length) {
				slog.Info("fwcfg DMA write read error", "err", err, "n", n)
				resultControl = FW_CFG_DMA_CTL_ERROR
			} else {
				slog.Info("fwcfg DMA write calling handler", "file", file.name, "data_len", len(writeData))
				f.mu.Unlock()
				if err := file.onWrite(writeData); err != nil {
					slog.Info("fwcfg DMA write handler error", "err", err)
					resultControl = FW_CFG_DMA_CTL_ERROR
				}
				f.mu.Lock()
			}
		} else {
			slog.Info("fwcfg DMA write no handler", "selector", fmt.Sprintf("0x%x", f.selector))
		}
	} else if dma.Control&FW_CFG_DMA_CTL_SKIP != 0 {
		// Skip operation
		f.dataOffset += dma.Length
	}

	f.mu.Unlock()

	// Write result (clear control, set error if needed)
	var resultBuf [4]byte
	binary.BigEndian.PutUint32(resultBuf[:], resultControl)
	_, err = f.vm.WriteAt(resultBuf[:], int64(dmaAddr))
	return err
}

// Base returns the MMIO base address.
func (f *FwCfg) Base() uint64 {
	return f.base
}

// Size returns the MMIO region size.
func (f *FwCfg) Size() uint64 {
	return f.size
}

var (
	_ hv.Device                 = (*FwCfg)(nil)
	_ chipset.ChipsetDevice     = (*FwCfg)(nil)
	_ chipset.MmioHandler       = (*FwCfg)(nil)
	_ chipset.ChangeDeviceState = (*FwCfg)(nil)
)
