package vm

import (
	"errors"
	"fmt"

	"j5.nz/cc/internal/virtio"
)

func virtioFSStats(fsdevs []*virtio.FS) []virtio.FSStats {
	if len(fsdevs) == 0 {
		return nil
	}
	out := make([]virtio.FSStats, 0, len(fsdevs))
	for _, fsdev := range fsdevs {
		if fsdev == nil {
			continue
		}
		out = append(out, fsdev.Stats())
	}
	return out
}

func virtioFSBackingUsage(fsdevs []*virtio.FS) (current, highWater, physical uint64, err error) {
	var errs []error
	providers := 0
	for i, fsdev := range fsdevs {
		if fsdev == nil {
			continue
		}
		deviceCurrent, deviceHighWater, devicePhysical, deviceErr := fsdev.BackingUsage()
		providers++
		current += deviceCurrent
		highWater = max(highWater, deviceHighWater)
		physical += devicePhysical
		if deviceErr != nil {
			errs = append(errs, fmt.Errorf("virtio-fs device %d: %w", i, deviceErr))
		}
	}
	// One device has an exact internally observed peak. For multiple devices,
	// the maximum component peak is a conservative lower bound; summing their
	// independent peaks would invent a state which may never have existed.
	if providers == 0 {
		highWater = 0
	} else {
		highWater = max(highWater, current)
	}
	return current, highWater, physical, errors.Join(errs...)
}

func virtioFSBackingMetadataUsage(fsdevs []*virtio.FS) (current, highWater uint64) {
	for _, fsdev := range fsdevs {
		if fsdev == nil {
			continue
		}
		deviceCurrent, deviceHighWater := fsdev.BackingMetadataUsage()
		current += deviceCurrent
		highWater = max(highWater, deviceHighWater)
	}
	highWater = max(highWater, current)
	return current, highWater
}
