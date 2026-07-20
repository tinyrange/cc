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
	var tracker *virtio.FSBackingUsageTracker
	sharedTracker := true
	devices := 0
	for i, fsdev := range fsdevs {
		if fsdev == nil {
			continue
		}
		devices++
		deviceCurrent, deviceHighWater, devicePhysical, deviceErr := fsdev.BackingUsage()
		deviceTracker := fsdev.BackingUsageTracker()
		if deviceTracker == nil {
			sharedTracker = false
		} else if tracker == nil {
			tracker = deviceTracker
		} else if deviceTracker != tracker {
			sharedTracker = false
		}
		current += deviceCurrent
		if tracker == nil || !sharedTracker {
			highWater = max(highWater, deviceHighWater)
		}
		physical += devicePhysical
		if deviceErr != nil {
			errs = append(errs, fmt.Errorf("virtio-fs device %d: %w", i, deviceErr))
		}
	}
	if devices != 0 && tracker != nil && sharedTracker {
		current, highWater = tracker.Usage()
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
