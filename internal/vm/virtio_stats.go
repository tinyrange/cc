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
	for i, fsdev := range fsdevs {
		if fsdev == nil {
			continue
		}
		deviceCurrent, deviceHighWater, devicePhysical, deviceErr := fsdev.BackingUsage()
		metadataCurrent, metadataHighWater := fsdev.BackingMetadataUsage()
		deviceCurrent += metadataCurrent
		deviceHighWater += metadataHighWater
		current += deviceCurrent
		highWater += deviceHighWater
		physical += devicePhysical
		if deviceErr != nil {
			errs = append(errs, fmt.Errorf("virtio-fs device %d: %w", i, deviceErr))
		}
	}
	return current, highWater, physical, errors.Join(errs...)
}
