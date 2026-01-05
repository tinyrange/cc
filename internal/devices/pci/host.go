package pci

import (
	"encoding/binary"
	"fmt"
	"sync"

	"github.com/tinyrange/cc/internal/fdt"
	"github.com/tinyrange/cc/internal/hv"
)

const (
	type0BAROffset = 0x10
	type0BARCount  = 6
	type0BARStride = 4
)

// ConfigSpace models PCI configuration space access for a single bus/device/function tuple.
type ConfigSpace interface {
	ReadConfig(offset uint16, size uint8) (uint32, error)
	WriteConfig(offset uint16, size uint8, value uint32) error
}

// Endpoint represents a PCI function behind the host bridge.
type Endpoint interface {
	ConfigSpace() ConfigSpace
	OnBARReprogram(index int, value uint32) error
}

// BARAllocator reserves address space for BAR windows.
type BARAllocator interface {
	Allocate(io bool, size uint32, align uint32) (uint64, error)
}

type linearAllocator struct {
	base uint64
	size uint64
	next uint64
}

func newLinearAllocator(base, size uint64) *linearAllocator {
	return &linearAllocator{
		base: base,
		size: size,
		next: base,
	}
}

func (a *linearAllocator) Allocate(io bool, size uint32, align uint32) (uint64, error) {
	if io {
		return 0, fmt.Errorf("I/O BARs unsupported")
	}
	if size == 0 {
		return 0, fmt.Errorf("BAR size must be non-zero")
	}
	if align == 0 {
		align = size
	}
	align64 := uint64(align)
	base := (a.next + align64 - 1) &^ (align64 - 1)
	if base < a.base || base+uint64(size) < base || base+uint64(size) > a.base+a.size {
		return 0, fmt.Errorf("PCI MMIO space exhausted")
	}
	a.next = base + uint64(size)
	return base, nil
}

type deviceKey struct {
	bus uint8
	dev uint8
	fn  uint8
}

type deviceSlot struct {
	endpoint Endpoint
	provider ConfigSpace
	barValue [type0BARCount]uint32
	barSize  [type0BARCount]uint32
}

func (s *deviceSlot) onConfigWrite(offset uint16, size uint8, value uint32) (int, uint32, bool) {
	if s == nil || s.endpoint == nil {
		return 0, 0, false
	}
	if size != 4 {
		return 0, 0, false
	}
	if offset < type0BAROffset || offset >= type0BAROffset+type0BARCount*type0BARStride {
		return 0, 0, false
	}
	if offset%type0BARStride != 0 {
		return 0, 0, false
	}
	if value == 0xffff_ffff {
		return 0, 0, false
	}
	index := int((offset - type0BAROffset) / type0BARStride)
	if index < 0 || index >= type0BARCount {
		return 0, 0, false
	}
	s.barValue[index] = value
	return index, value, true
}

// DeviceHandle exposes helper methods for registered endpoints.
type DeviceHandle struct {
	host *HostBridge
	key  deviceKey
}

// AllocateMemoryBAR reserves MMIO space for the supplied BAR index.
func (h *DeviceHandle) AllocateMemoryBAR(index int, size uint32, align uint32) (uint64, error) {
	if h == nil || h.host == nil {
		return 0, fmt.Errorf("pci device handle is nil")
	}
	return h.host.allocateBAR(h.key, index, false, size, align)
}

// AllocateIOBAR reserves legacy I/O space for the supplied BAR index (unsupported on ARM).
func (h *DeviceHandle) AllocateIOBAR(index int, size uint32, align uint32) (uint64, error) {
	return 0, fmt.Errorf("I/O BAR allocation not supported")
}

// HostBridgeConfig describes the MMIO layout for config accesses and BAR windows.
type HostBridgeConfig struct {
	ConfigBase   uint64
	ConfigSize   uint64
	MMIOBase     uint64
	MMIOSize     uint64
	RootVendorID uint16
	RootDeviceID uint16
	MaxBus       uint8
	BARAllocator BARAllocator
}

// HostBridge implements a minimal ECAM-capable PCI root complex.
type HostBridge struct {
	configBase uint64
	configSize uint64

	mmioBase uint64
	mmioSize uint64

	rootVendorID uint16
	rootDeviceID uint16
	maxBus       uint8

	barAllocator BARAllocator

	mu      sync.Mutex
	devices map[deviceKey]*deviceSlot
}

// NewHostBridge constructs a host bridge using the supplied config.
func NewHostBridge(cfg HostBridgeConfig) *HostBridge {
	const (
		defaultConfigSize = 1 << 20 // 1 MiB covers bus 0
		defaultMMIOBase   = 0x20000000
		defaultMMIOSize   = 0x10000000
	)

	h := &HostBridge{
		configBase: cfg.ConfigBase,
		configSize: cfg.ConfigSize,
		mmioBase:   cfg.MMIOBase,
		mmioSize:   cfg.MMIOSize,
		rootVendorID: func() uint16 {
			if cfg.RootVendorID != 0 {
				return cfg.RootVendorID
			}
			return 0x1af4
		}(),
		rootDeviceID: func() uint16 {
			if cfg.RootDeviceID != 0 {
				return cfg.RootDeviceID
			}
			return 0x0001
		}(),
		maxBus:  0,
		devices: make(map[deviceKey]*deviceSlot),
	}
	if h.configSize == 0 {
		h.configSize = defaultConfigSize
	}
	if h.mmioSize == 0 {
		h.mmioSize = defaultMMIOSize
	}
	if h.mmioBase == 0 {
		h.mmioBase = defaultMMIOBase
	}
	if cfg.MaxBus != 0 {
		h.maxBus = cfg.MaxBus
	}
	if cfg.BARAllocator != nil {
		h.barAllocator = cfg.BARAllocator
	} else {
		h.barAllocator = newLinearAllocator(h.mmioBase, h.mmioSize)
	}
	return h
}

// Init implements hv.Device.
func (*HostBridge) Init(hv.VirtualMachine) error {
	return nil
}

// MMIORegions implements hv.MemoryMappedIODevice.
func (h *HostBridge) MMIORegions() []hv.MMIORegion {
	if h.configSize == 0 {
		return nil
	}
	return []hv.MMIORegion{{
		Address: h.configBase,
		Size:    h.configSize,
	}}
}

// ReadMMIO implements hv.MemoryMappedIODevice.
func (h *HostBridge) ReadMMIO(ctx hv.ExitContext, addr uint64, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	offset := addr - h.configBase
	if offset >= h.configSize {
		return fmt.Errorf("pci host bridge: read outside config space %#x", addr)
	}

	remaining := len(data)
	cursor := 0
	curOffset := offset
	for remaining > 0 {
		key, reg, ok := h.decodeConfigAddress(curOffset)
		if !ok {
			data[cursor] = 0xff
			cursor++
			curOffset++
			remaining--
			continue
		}
		chunk := pickConfigAccessSize(reg, remaining)
		value := h.readConfig(key, reg, chunk)
		for i := 0; i < int(chunk); i++ {
			if cursor+i < len(data) {
				data[cursor+i] = byte(value >> (8 * i))
			}
		}
		cursor += int(chunk)
		curOffset += uint64(chunk)
		remaining -= int(chunk)
	}
	return nil
}

// WriteMMIO implements hv.MemoryMappedIODevice.
func (h *HostBridge) WriteMMIO(ctx hv.ExitContext, addr uint64, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	offset := addr - h.configBase
	if offset >= h.configSize {
		return fmt.Errorf("pci host bridge: write outside config space %#x", addr)
	}

	remaining := len(data)
	cursor := 0
	curOffset := offset
	for remaining > 0 {
		key, reg, ok := h.decodeConfigAddress(curOffset)
		if !ok {
			break
		}
		chunk := pickConfigAccessSize(reg, remaining)
		value := uint32(0)
		for i := 0; i < int(chunk); i++ {
			value |= uint32(data[cursor+i]) << (8 * i)
		}
		h.writeConfig(key, reg, chunk, value)
		cursor += int(chunk)
		curOffset += uint64(chunk)
		remaining -= int(chunk)
	}
	return nil
}

func (h *HostBridge) decodeConfigAddress(offset uint64) (deviceKey, uint16, bool) {
	if offset > uint64(^uint16(0)) {
		return deviceKey{}, 0, false
	}
	bus := uint8((offset >> 20) & 0xff)
	device := uint8((offset >> 15) & 0x1f)
	function := uint8((offset >> 12) & 0x7)
	if bus > h.maxBus {
		return deviceKey{}, 0, false
	}
	reg := uint16(offset & 0xfff)
	return deviceKey{bus: bus, dev: device, fn: function}, reg, true
}

func (h *HostBridge) readConfig(key deviceKey, offset uint16, size uint8) uint32 {
	if key.bus == 0 && key.dev == 0 && key.fn == 0 {
		return h.readRootConfig(offset, size)
	}
	provider := h.provider(key)
	if provider == nil {
		return 0xffff_ffff
	}
	value, err := provider.ReadConfig(offset, size)
	if err != nil {
		return 0xffff_ffff
	}
	return maskValue(value, size)
}

func (h *HostBridge) writeConfig(key deviceKey, offset uint16, size uint8, value uint32) {
	if key.bus == 0 && key.dev == 0 && key.fn == 0 {
		return
	}
	provider := h.provider(key)
	if provider == nil {
		return
	}
	if err := provider.WriteConfig(offset, size, value); err != nil {
		return
	}

	var (
		endpoint Endpoint
		barIdx   int
		barValue uint32
		notify   bool
	)

	h.mu.Lock()
	if slot := h.devices[key]; slot != nil {
		barIdx, barValue, notify = slot.onConfigWrite(offset, size, value)
		if notify {
			endpoint = slot.endpoint
		}
	}
	h.mu.Unlock()

	if notify && endpoint != nil {
		_ = endpoint.OnBARReprogram(barIdx, barValue)
	}
}

func (h *HostBridge) readRootConfig(offset uint16, size uint8) uint32 {
	if size == 0 || size > 4 {
		return 0xffff_ffff
	}
	if int(offset)+int(size) > 256 {
		return 0xffff_ffff
	}
	var buf [256]byte
	binary.LittleEndian.PutUint16(buf[0:], h.rootVendorID)
	binary.LittleEndian.PutUint16(buf[2:], h.rootDeviceID)
	buf[0x08] = 0x00
	buf[0x09] = 0x00
	buf[0x0a] = 0x00
	buf[0x0b] = 0x06
	buf[0x0e] = 0x00
	value := uint32(0)
	for i := uint8(0); i < size; i++ {
		value |= uint32(buf[int(offset)+int(i)]) << (8 * i)
	}
	return value
}

// RegisterEndpoint associates an endpoint with the supplied location.
func (h *HostBridge) RegisterEndpoint(bus, device, function uint8, endpoint Endpoint) (*DeviceHandle, error) {
	if endpoint == nil {
		return nil, fmt.Errorf("pci endpoint cannot be nil")
	}
	if bus != 0 {
		return nil, fmt.Errorf("only bus 0 supported (got %d)", bus)
	}
	provider := endpoint.ConfigSpace()
	if provider == nil {
		return nil, fmt.Errorf("endpoint must expose config space")
	}

	key := deviceKey{bus: bus, dev: device, fn: function}
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, exists := h.devices[key]; exists {
		return nil, fmt.Errorf("device already registered at %02x:%02x.%x", bus, device, function)
	}
	h.devices[key] = &deviceSlot{
		endpoint: endpoint,
		provider: provider,
	}
	return &DeviceHandle{host: h, key: key}, nil
}

func (h *HostBridge) provider(key deviceKey) ConfigSpace {
	h.mu.Lock()
	defer h.mu.Unlock()
	if slot := h.devices[key]; slot != nil {
		return slot.provider
	}
	return nil
}

func (h *HostBridge) allocateBAR(key deviceKey, index int, io bool, size uint32, align uint32) (uint64, error) {
	if io {
		return 0, fmt.Errorf("I/O BARs unsupported")
	}
	if index < 0 || index >= type0BARCount {
		return 0, fmt.Errorf("BAR index %d out of range", index)
	}
	if size == 0 {
		return 0, fmt.Errorf("BAR size must be non-zero")
	}
	if h.barAllocator == nil {
		h.barAllocator = newLinearAllocator(h.mmioBase, h.mmioSize)
	}
	base, err := h.barAllocator.Allocate(io, size, align)
	if err != nil {
		return 0, err
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	slot := h.devices[key]
	if slot == nil {
		return 0, fmt.Errorf("device not registered")
	}
	slot.barSize[index] = size
	if base <= uint64(^uint32(0)) {
		slot.barValue[index] = uint32(base)
	} else {
		slot.barValue[index] = uint32(base & 0xffff_ffff)
	}
	return base, nil
}

func maskValue(value uint32, size uint8) uint32 {
	switch size {
	case 1:
		return value & 0xff
	case 2:
		return value & 0xffff
	case 4:
		return value
	default:
		return 0xffff_ffff
	}
}

func pickConfigAccessSize(reg uint16, remaining int) uint8 {
	if reg%4 == 0 && remaining >= 4 {
		return 4
	}
	if reg%2 == 0 && remaining >= 2 {
		return 2
	}
	return 1
}

// DeviceTreeNode returns a device-tree node describing the host bridge.
func (h *HostBridge) DeviceTreeNode() fdt.Node {
	childHigh := uint32(h.mmioBase >> 32)
	childLow := uint32(h.mmioBase & 0xffff_ffff)
	parentHigh := uint32(h.mmioBase >> 32)
	parentLow := uint32(h.mmioBase & 0xffff_ffff)
	sizeHigh := uint32(h.mmioSize >> 32)
	sizeLow := uint32(h.mmioSize & 0xffff_ffff)
	ranges := []uint32{
		0x02000000, childHigh, childLow,
		parentHigh, parentLow,
		sizeHigh, sizeLow,
	}
	return fdt.Node{
		Name: fmt.Sprintf("pcie@%x", h.configBase),
		Properties: map[string]fdt.Property{
			"compatible":           {Strings: []string{"pci-host-ecam-generic"}},
			"device_type":          {Strings: []string{"pci"}},
			"#address-cells":       {U32: []uint32{3}},
			"#size-cells":          {U32: []uint32{2}},
			"linux,pci-probe-only": {U32: []uint32{1}},
			"bus-range":            {U32: []uint32{0, uint32(h.maxBus)}},
			"reg":                  {U64: []uint64{h.configBase, h.configSize}},
			"ranges":               {U32: ranges},
			"linux,pci-domain":     {U32: []uint32{0}},
		},
	}
}

var (
	_ hv.Device               = (*HostBridge)(nil)
	_ hv.MemoryMappedIODevice = (*HostBridge)(nil)
)
