package virtio

import "sync"

// FSBackingUsageTracker observes the sum of a VM's virtio-fs backing stores at
// request boundaries. Component peaks cannot be combined after the fact: only
// observing the shared current sum preserves a real aggregate high-water mark.
type FSBackingUsageTracker struct {
	mu        sync.Mutex
	backends  []FSBackend
	current   uint64
	highWater uint64
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
	var current uint64
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
	}
	t.mu.Lock()
	t.current = current
	if current > t.highWater {
		t.highWater = current
	}
	t.mu.Unlock()
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
