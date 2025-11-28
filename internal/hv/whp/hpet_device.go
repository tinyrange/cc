//go:build windows && amd64

package whp

import (
	"fmt"
	"sync"
	"time"

	"github.com/tinyrange/cc/internal/hv"
)

const (
	hpetClockPeriodFemtoseconds = 10_000_000 // 10ns -> 100 MHz
	hpetVendorID                = 0x8086
	hpetNumberTimers            = 1

	hpetRegisterCapabilities    = 0x000
	hpetRegisterConfig          = 0x010
	hpetRegisterInterruptStatus = 0x020
	hpetRegisterMainCounter     = 0x0F0
	hpetRegisterLength          = 0x008
)

const nsToFemtoseconds = 1_000_000 // 1ns = 1e6 fs

type hpetDevice struct {
	base uint64

	mu            sync.Mutex
	generalConfig uint64

	counter    uint64
	lastUpdate time.Time
	enabled    bool
}

func newHPETDevice(base uint64) *hpetDevice {
	return &hpetDevice{
		base:       base,
		lastUpdate: time.Now(),
	}
}

func (d *hpetDevice) MMIORegions() []hv.MMIORegion {
	return []hv.MMIORegion{
		{
			Address: d.base,
			Size:    0x1000,
		},
	}
}

func (d *hpetDevice) ReadMMIO(addr uint64, data []byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	offset := addr - d.base
	if offset+uint64(len(data)) > 0x1000 {
		return fmt.Errorf("hpet: read out of range offset=0x%x size=%d", offset, len(data))
	}

	for i := range data {
		b, err := d.byteAtLocked(offset + uint64(i))
		if err != nil {
			return err
		}
		data[i] = b
	}
	return nil
}

func (d *hpetDevice) WriteMMIO(addr uint64, data []byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	offset := addr - d.base
	if offset+uint64(len(data)) > 0x1000 {
		return fmt.Errorf("hpet: write out of range offset=0x%x size=%d", offset, len(data))
	}

	for i := range data {
		if err := d.writeByteLocked(offset+uint64(i), data[i]); err != nil {
			return err
		}
	}
	return nil
}

func (d *hpetDevice) Init(vm hv.VirtualMachine) error {
	return nil
}

func (d *hpetDevice) byteAtLocked(offset uint64) (byte, error) {
	switch {
	case offset >= hpetRegisterCapabilities && offset < hpetRegisterCapabilities+hpetRegisterLength:
		value := d.capabilities()
		return byte(value >> (8 * (offset - hpetRegisterCapabilities))), nil
	case offset >= hpetRegisterConfig && offset < hpetRegisterConfig+hpetRegisterLength:
		return byte(d.generalConfig >> (8 * (offset - hpetRegisterConfig))), nil
	case offset >= hpetRegisterInterruptStatus && offset < hpetRegisterInterruptStatus+hpetRegisterLength:
		return 0, nil
	case offset >= hpetRegisterMainCounter && offset < hpetRegisterMainCounter+hpetRegisterLength:
		value := d.currentCounterLocked()
		return byte(value >> (8 * (offset - hpetRegisterMainCounter))), nil
	default:
		return 0, nil
	}
}

func (d *hpetDevice) writeByteLocked(offset uint64, value byte) error {
	switch {
	case offset >= hpetRegisterConfig && offset < hpetRegisterConfig+hpetRegisterLength:
		shift := 8 * (offset - hpetRegisterConfig)
		mask := uint64(0xFF) << shift
		newConfig := (d.generalConfig & ^mask) | (uint64(value) << shift)
		return d.setGeneralConfigLocked(newConfig)
	case offset >= hpetRegisterMainCounter && offset < hpetRegisterMainCounter+hpetRegisterLength:
		shift := 8 * (offset - hpetRegisterMainCounter)
		mask := uint64(0xFF) << shift
		d.counter = (d.counter & ^mask) | (uint64(value) << shift)
		d.lastUpdate = time.Now()
		return nil
	default:
		// silently ignore unsupported regions
		return nil
	}
}

func (d *hpetDevice) setGeneralConfigLocked(config uint64) error {
	const supportedMask = uint64(0x3)

	d.updateCounterLocked()

	prevEnabled := d.enabled
	d.generalConfig = config & supportedMask
	d.enabled = d.generalConfig&1 == 1

	if d.enabled && !prevEnabled {
		d.lastUpdate = time.Now()
	}

	if !d.enabled && prevEnabled {
		// ensure counter is consistent when disabled
		d.lastUpdate = time.Now()
	}
	return nil
}

func (d *hpetDevice) currentCounterLocked() uint64 {
	d.updateCounterLocked()
	return d.counter
}

func (d *hpetDevice) updateCounterLocked() {
	if !d.enabled {
		return
	}

	now := time.Now()
	elapsed := now.Sub(d.lastUpdate)
	if elapsed <= 0 {
		return
	}

	ticks := (uint64(elapsed.Nanoseconds()) * nsToFemtoseconds) / hpetClockPeriodFemtoseconds
	if ticks == 0 {
		return
	}

	d.counter += ticks
	d.lastUpdate = now
}

func (d *hpetDevice) capabilities() uint64 {
	const (
		counter64Bit = 1 << 13
		legacyRoute  = 1 << 15
	)

	cap := uint64(hpetClockPeriodFemtoseconds)
	cap |= uint64(hpetNumberTimers-1) << 8
	cap |= counter64Bit | legacyRoute
	cap |= uint64(hpetVendorID) << 16

	return cap
}

var _ hv.MemoryMappedIODevice = (*hpetDevice)(nil)
