package chipset

import (
	"encoding/binary"
	"fmt"
	"sync"

	"github.com/tinyrange/cc/internal/hv"
)

const (
	// IOAPICBaseAddress is the legacy MMIO base for the first IO-APIC.
	IOAPICBaseAddress uint64 = 0xFEC00000

	ioapicRegisterWindowSize = 0x20
	// ioapicMMIOMask           = ioapicRegisterWindowSize - 1 // Unused

	ioapicRegisterSelect = 0x00
	ioapicRegisterData   = 0x10

	ioapicIDRegister           = 0x00
	ioapicVersionRegister      = 0x01
	ioapicArbitrationRegister  = 0x02
	ioapicRedirectionTableBase = 0x10

	ioapicVersion = 0x11
)

const (
	deliveryModeFixed          = 0x0
	deliveryModeLowestPriority = 0x1
)

// Redirection bits that the guest is permitted to write.
const redirectionWriteMask uint64 = 0xFFFF0000000000FF |
	(0x7 << 8) | // delivery mode
	(1 << 11) | // destination mode
	(1 << 13) | // polarity
	(1 << 15) | // trigger mode
	(1 << 16) // mask bit

// IOAPIC emulates the legacy x86 IO-APIC found at 0xFEC00000.
type IOAPIC struct {
	mu sync.Mutex

	entries []irqRedirection
	index   uint8
	id      uint8

	routing IoApicRouting
	stats   ioapicStats
}

// Init implements hv.Device.
func (i *IOAPIC) Init(vm hv.VirtualMachine) error {
	_ = vm
	return nil
}

// IoApicRouting allows the IO-APIC to notify the rest of the VMM when an
// interrupt should be injected into a vCPU.
type IoApicRouting interface {
	// Assert requests an interrupt injection.
	// vector: The IDT vector (0-255).
	// dest: The target CPU ID or APIC ID.
	// destMode: 0 for Physical, 1 for Logical.
	// deliveryMode: 0 for Fixed, 1 for LowestPriority, etc.
	// level: true when the redirection entry is configured for level-triggered delivery.
	Assert(vector uint8, dest uint8, destMode uint8, deliveryMode uint8, level bool)
}

// IoApicRoutingFunc adapts a simple function to IoApicRouting.
type IoApicRoutingFunc func(vector uint8, dest uint8, destMode uint8, deliveryMode uint8, level bool)

// Assert implements IoApicRouting.
func (f IoApicRoutingFunc) Assert(vector uint8, dest uint8, destMode uint8, deliveryMode uint8, level bool) {
	if f != nil {
		f(vector, dest, destMode, deliveryMode, level)
	}
}

type noopIoApicRouting struct{}

func (noopIoApicRouting) Assert(uint8, uint8, uint8, uint8, bool) {}

// NewIOAPIC builds an IO-APIC exposing numEntries redirection slots.
func NewIOAPIC(numEntries int) *IOAPIC {
	if numEntries <= 0 {
		numEntries = 24
	}
	entries := make([]irqRedirection, numEntries)
	for i := range entries {
		entries[i] = newIRQRedirection()
	}
	return &IOAPIC{
		entries: entries,
		routing: noopIoApicRouting{},
		stats: ioapicStats{
			perIRQ: make([]uint64, numEntries),
		},
	}
}

// SetRouting overrides the destination used when an interrupt fires.
func (i *IOAPIC) SetRouting(r IoApicRouting) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if r == nil {
		i.routing = noopIoApicRouting{}
	} else {
		i.routing = r
	}
}

// HandleEOI clears remote-IRR for any line that was targeting the supplied
// vector and re-evaluates pending level-triggered interrupts.
func (i *IOAPIC) HandleEOI(vector uint32) {
	i.mu.Lock()
	defer i.mu.Unlock()
	for line := range i.entries {
		entry := &i.entries[line]
		if entry.redirection.vector() == uint8(vector) {
			entry.redirection.setRemoteIRR(false)
			entry.evaluate(i.routing, &i.stats, uint8(line), false)
		}
	}
}

// SetIRQ changes the level of a given IO-APIC input pin.
func (i *IOAPIC) SetIRQ(line uint32, high bool) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if line >= uint32(len(i.entries)) {
		return
	}
	idx := int(line)
	entry := &i.entries[idx]
	if high {
		entry.assert(i.routing, &i.stats, uint8(line))
	} else {
		entry.deassert()
	}
}

// MMIORegions implements hv.MemoryMappedIODevice.
func (i *IOAPIC) MMIORegions() []hv.MMIORegion {
	return []hv.MMIORegion{
		{Address: IOAPICBaseAddress, Size: ioapicRegisterWindowSize},
	}
}

// ReadMMIO implements hv.MemoryMappedIODevice.
func (i *IOAPIC) ReadMMIO(addr uint64, data []byte) error {
	// fmt.Fprintf(os.Stderr, "ioapic: read addr=%#x size=%d\n", addr, len(data))
	if !i.inRange(addr, uint64(len(data))) {
		return fmt.Errorf("ioapic: read outside MMIO window: 0x%x", addr)
	}

	offset := addr - IOAPICBaseAddress
	var value uint32

	i.mu.Lock()
	switch offset {
	case ioapicRegisterSelect:
		value = uint32(i.index)
	case ioapicRegisterData:
		value = i.readRegister(i.index)
	default:
		i.mu.Unlock()
		return fmt.Errorf("ioapic: invalid read offset 0x%x", offset)
	}
	i.mu.Unlock()

	buf := make([]byte, 8)
	binary.LittleEndian.PutUint32(buf, value)
	copy(data, buf[:min(len(data), 8)])
	return nil
}

// WriteMMIO implements hv.MemoryMappedIODevice.
func (i *IOAPIC) WriteMMIO(addr uint64, data []byte) error {
	if !i.inRange(addr, uint64(len(data))) {
		return fmt.Errorf("ioapic: write outside MMIO window: 0x%x", addr)
	}
	offset := addr - IOAPICBaseAddress

	i.mu.Lock()
	defer i.mu.Unlock()

	switch offset {
	case ioapicRegisterSelect:
		if len(data) == 0 {
			return fmt.Errorf("ioapic: empty write to select register")
		}
		// If data is larger than 1 byte, we take the LSB (Little Endian).
		i.index = data[0]
	case ioapicRegisterData:
		if len(data) != 4 && len(data) != 8 {
			return fmt.Errorf("ioapic: invalid data register write size %d", len(data))
		}
		value := binary.LittleEndian.Uint32(data)
		i.writeRegister(i.index, value)
	default:
		return fmt.Errorf("ioapic: invalid write offset 0x%x", offset)
	}
	return nil
}

func (i *IOAPIC) readRegister(index uint8) uint32 {
	switch {
	case index == ioapicIDRegister:
		return encodeIoApicID(i.id)
	case index == ioapicVersionRegister:
		return encodeIoApicVersion(uint8(len(i.entries) - 1))
	case index == ioapicArbitrationRegister:
		return 0
	case index >= ioapicRedirectionTableBase:
		return i.readRedirection(index - ioapicRedirectionTableBase)
	default:
		return 0
	}
}

func (i *IOAPIC) writeRegister(index uint8, value uint32) {
	switch {
	case index == ioapicIDRegister:
		i.id = decodeIoApicID(value)
	case index == ioapicVersionRegister, index == ioapicArbitrationRegister:
		// Read-only in hardware, ignore.
	case index >= ioapicRedirectionTableBase:
		i.writeRedirection(index-ioapicRedirectionTableBase, value)
	}
}

func (i *IOAPIC) readRedirection(index uint8) uint32 {
	entry := i.entryForIndex(index)
	if entry == nil {
		return 0
	}
	raw := entry.redirection.raw()
	if index&1 == 1 {
		return uint32(raw >> 32)
	}
	return uint32(raw & 0xffffffff)
}

func (i *IOAPIC) writeRedirection(index uint8, value uint32) {
	entry := i.entryForIndex(index)
	if entry == nil {
		return
	}

	raw := entry.redirection.raw()
	val := uint64(value)
	lowMask := redirectionWriteMask & 0xffffffff
	highMask := redirectionWriteMask & 0xffffffff00000000
	line := uint8(index / 2)

	wasMasked := entry.redirection.masked()

	if index&1 == 1 {
		raw &= ^highMask
		raw |= (val << 32) & highMask
	} else {
		raw &= ^lowMask
		raw |= val & lowMask
	}
	entry.redirection.setRaw(raw)

	isMasked := entry.redirection.masked()

	// If the line was masked and is now unmasked, and the line is currently High,
	// we must treat this as a rising edge for Edge-Triggered interrupts.
	// Without this, Serial ports waiting for an interrupt will hang forever.
	forceEdge := wasMasked && !isMasked && entry.lineLevel

	entry.evaluate(i.routing, &i.stats, line, forceEdge)
}

func (i *IOAPIC) entryForIndex(index uint8) *irqRedirection {
	n := int(index / 2)
	if n < 0 || n >= len(i.entries) {
		return nil
	}
	return &i.entries[n]
}

func (i *IOAPIC) inRange(addr uint64, size uint64) bool {
	if addr < IOAPICBaseAddress {
		return false
	}
	end := addr + size
	windowEnd := IOAPICBaseAddress + ioapicRegisterWindowSize
	return end <= windowEnd
}

type irqRedirection struct {
	redirection redirectionEntry
	lineLevel   bool
}

func newIRQRedirection() irqRedirection {
	return irqRedirection{
		redirection: newRedirectionEntry(),
	}
}

func (r *irqRedirection) assert(router IoApicRouting, stats *ioapicStats, line uint8) {
	edge := !r.lineLevel
	r.lineLevel = true
	r.evaluate(router, stats, line, edge)
}

func (r *irqRedirection) deassert() {
	r.lineLevel = false
	r.redirection.setRemoteIRR(false)
}

func (r *irqRedirection) evaluate(router IoApicRouting, stats *ioapicStats, line uint8, edge bool) {
	if r.redirection.masked() {
		return
	}
	isLevel := r.redirection.isLevelCapable()
	switch {
	case isLevel && (!r.lineLevel || r.redirection.remoteIRR()):
		return
	case !isLevel && !edge:
		return
	}

	r.redirection.setRemoteIRR(isLevel)
	stats.interrupts++
	if int(line) < len(stats.perIRQ) {
		stats.perIRQ[line]++
	}

	destMode := uint8(0) // Physical
	if r.redirection.destinationModeLogical() {
		destMode = 1
	}

	router.Assert(
		r.redirection.vector(),
		r.redirection.destination(),
		destMode,
		r.redirection.deliveryMode(),
		isLevel,
	)
}

type redirectionEntry struct {
	value uint64
}

func newRedirectionEntry() redirectionEntry {
	var value uint64
	value |= 1 << 11 // destination mode logical
	value |= 1 << 16 // masked by default
	return redirectionEntry{value: value}
}

func (r redirectionEntry) raw() uint64 {
	return r.value
}

func (r *redirectionEntry) setRaw(value uint64) {
	r.value = value
}

// destination returns bits 56-63 (Destination Field)
func (r redirectionEntry) destination() uint8 {
	return uint8((r.value >> 56) & 0xFF)
}

func (r redirectionEntry) vector() uint8 {
	return uint8(r.value & 0xff)
}

func (r redirectionEntry) deliveryMode() uint8 {
	return uint8((r.value >> 8) & 0x7)
}

func (r redirectionEntry) masked() bool {
	return (r.value>>16)&1 == 1
}

func (r redirectionEntry) remoteIRR() bool {
	return (r.value>>14)&1 == 1
}

func (r *redirectionEntry) setRemoteIRR(val bool) {
	if val {
		r.value |= 1 << 14
	} else {
		r.value &^= 1 << 14
	}
}

func (r redirectionEntry) triggerModeLevel() bool {
	return (r.value>>15)&1 == 1
}

func (r redirectionEntry) destinationModeLogical() bool {
	return (r.value>>11)&1 == 1
}

func (r redirectionEntry) isLevelCapable() bool {
	if !r.triggerModeLevel() {
		return false
	}
	mode := r.deliveryMode()
	return mode == deliveryModeFixed || mode == deliveryModeLowestPriority
}

type ioapicStats struct {
	interrupts uint64
	perIRQ     []uint64
}

func encodeIoApicID(id uint8) uint32 {
	return uint32(id&0x0f) << 24
}

func decodeIoApicID(value uint32) uint8 {
	return uint8((value >> 24) & 0x0f)
}

// Snapshot support ----------------------------------------------------------

type ioapicEntrySnapshot struct {
	Value     uint64
	LineLevel bool
}

type ioapicSnapshot struct {
	Index   uint8
	ID      uint8
	Entries []ioapicEntrySnapshot
}

func (i *IOAPIC) DeviceId() string { return "ioapic" }

func (i *IOAPIC) CaptureSnapshot() (hv.DeviceSnapshot, error) {
	i.mu.Lock()
	defer i.mu.Unlock()

	snap := &ioapicSnapshot{
		Index:   i.index,
		ID:      i.id,
		Entries: make([]ioapicEntrySnapshot, len(i.entries)),
	}

	for idx, entry := range i.entries {
		snap.Entries[idx] = ioapicEntrySnapshot{
			Value:     entry.redirection.raw(),
			LineLevel: entry.lineLevel,
		}
	}

	return snap, nil
}

func (i *IOAPIC) RestoreSnapshot(snap hv.DeviceSnapshot) error {
	data, ok := snap.(*ioapicSnapshot)
	if !ok {
		return fmt.Errorf("ioapic: invalid snapshot type")
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	if len(data.Entries) != len(i.entries) {
		return fmt.Errorf("ioapic: snapshot entry count mismatch: got %d, want %d", len(data.Entries), len(i.entries))
	}

	i.index = data.Index
	i.id = data.ID

	for idx, entry := range data.Entries {
		i.entries[idx].redirection.setRaw(entry.Value)
		i.entries[idx].lineLevel = entry.LineLevel
	}

	return nil
}

var _ hv.DeviceSnapshotter = (*IOAPIC)(nil)

func encodeIoApicVersion(maxEntry uint8) uint32 {
	val := uint32(ioapicVersion)
	val |= uint32(maxEntry) << 16
	return val
}

// min avoids importing math for ints.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

var (
	_ hv.MemoryMappedIODevice = (*IOAPIC)(nil)
	_ hv.Device               = (*IOAPIC)(nil)
)
