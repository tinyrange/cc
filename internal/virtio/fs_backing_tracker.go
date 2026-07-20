package virtio

import "sync"

// FSBackingUsageTracker observes the sum of a VM's virtio-fs backing stores at
// request boundaries. Component peaks cannot be combined after the fact: only
// observing the shared current sum preserves a real aggregate high-water mark.
type FSBackingUsageTracker struct {
	boundary          sync.Mutex
	mu                sync.Mutex
	backends          []FSBackend
	current           uint64
	highWater         uint64
	metadataCurrent   uint64
	metadataHighWater uint64
	combinedHighWater uint64
}

func AttachFSBackingUsageTracker(devices []*FS) *FSBackingUsageTracker {
	tracker := &FSBackingUsageTracker{}
	for _, device := range devices {
		if device == nil {
			continue
		}
		device.mu.Lock()
		tracker.backends = append(tracker.backends, device.backend)
		device.backingUsageTracker = tracker
		device.mu.Unlock()
	}
	tracker.Sample()
	return tracker
}

func (t *FSBackingUsageTracker) Sample() {
	if t == nil {
		return
	}
	t.boundary.Lock()
	t.sampleLocked()
	t.boundary.Unlock()
}

func (t *FSBackingUsageTracker) sampleLocked() {
	var current, metadata uint64
	for _, backend := range t.backends {
		if provider, ok := backend.(interface{ BackingCurrent() uint64 }); ok {
			current += provider.BackingCurrent()
			continue
		}
		if provider, ok := backend.(interface {
			BackingUsage() (uint64, uint64, uint64, error)
		}); ok {
			value, _, _, _ := provider.BackingUsage()
			current += value
		}
		if provider, ok := backend.(interface{ BackingMetadataUsage() (uint64, uint64) }); ok {
			value, _ := provider.BackingMetadataUsage()
			metadata += value
		}
	}
	t.mu.Lock()
	t.current = current
	t.metadataCurrent = metadata
	if current > t.highWater {
		t.highWater = current
	}
	if metadata > t.metadataHighWater {
		t.metadataHighWater = metadata
	}
	combined := current + metadata
	if combined < current {
		combined = ^uint64(0)
	}
	if combined > t.combinedHighWater {
		t.combinedHighWater = combined
	}
	t.mu.Unlock()
}

// TrackMutation serializes backing mutations across every filesystem attached
// to the VM and samples before releasing that boundary. Aggregate telemetry
// therefore describes a state which really existed instead of a sum assembled
// from different points in time.
func (t *FSBackingUsageTracker) TrackMutation() func() {
	if t == nil {
		return func() {}
	}
	t.boundary.Lock()
	return func() {
		t.sampleLocked()
		t.boundary.Unlock()
	}
}

func (t *FSBackingUsageTracker) Usage() (uint64, uint64) {
	if t == nil {
		return 0, 0
	}
	t.Sample()
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.current, t.highWater
}

func (t *FSBackingUsageTracker) MetadataUsage() (uint64, uint64) {
	if t == nil {
		return 0, 0
	}
	t.Sample()
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.metadataCurrent, t.metadataHighWater
}

func (t *FSBackingUsageTracker) CombinedUsage() (uint64, uint64) {
	if t == nil {
		return 0, 0
	}
	t.Sample()
	t.mu.Lock()
	defer t.mu.Unlock()
	combined := t.current + t.metadataCurrent
	if combined < t.current {
		combined = ^uint64(0)
	}
	return combined, t.combinedHighWater
}
