package virtio

import (
	"sync"
	"testing"
	"time"
)

type blockingUsageBackend struct {
	FSBackend
	mu      sync.Mutex
	current uint64
	block   chan struct{}
	started chan struct{}
}

func (b *blockingUsageBackend) BackingUsage() (uint64, uint64, uint64, error) {
	b.mu.Lock()
	current, block, started := b.current, b.block, b.started
	b.mu.Unlock()
	if block != nil {
		select {
		case <-started:
		default:
			close(started)
		}
		<-block
	}
	return current, current, current, nil
}

func (b *blockingUsageBackend) BackingMetadataUsage() (uint64, uint64) {
	return 17, 17
}

func TestFSBackingTrackerStatusSnapshotDoesNotWaitForSampler(t *testing.T) {
	backend := &blockingUsageBackend{current: 11}
	device := NewFS(0, 0, 0, "root", backend)
	tracker := AttachFSBackingUsageTracker([]*FS{device})
	backend.mu.Lock()
	backend.current = 23
	backend.block = make(chan struct{})
	backend.started = make(chan struct{})
	block, started := backend.block, backend.started
	backend.mu.Unlock()

	doneMutation := tracker.TrackMutation()
	doneMutation()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("background telemetry sample did not start")
	}
	statusDone := make(chan FSBackingUsageSnapshot, 1)
	go func() { statusDone <- tracker.Snapshot() }()
	select {
	case snapshot := <-statusDone:
		if snapshot.DataBytes != 11 || snapshot.MetadataBytes != 17 || snapshot.CombinedBytes != 28 {
			t.Fatalf("last coherent snapshot = %+v", snapshot)
		}
	case <-time.After(50 * time.Millisecond):
		t.Fatal("status snapshot waited for blocked host storage")
	}
	close(block)
	deadline := time.Now().Add(time.Second)
	for tracker.Snapshot().DataBytes != 23 {
		if time.Now().After(deadline) {
			t.Fatal("completed background sample was not published")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestFSBackingTrackerPublishesOnlyQuiescentMutationSnapshots(t *testing.T) {
	backend := &blockingUsageBackend{current: 11}
	device := NewFS(0, 0, 0, "root", backend)
	tracker := AttachFSBackingUsageTracker([]*FS{device})
	doneMutation := tracker.TrackMutation()
	backend.mu.Lock()
	backend.current = 23
	backend.mu.Unlock()
	tracker.RequestSample()
	deadline := time.Now().Add(time.Second)
	for tracker.sampling.Load() {
		if time.Now().After(deadline) {
			t.Fatal("active-mutation sample did not return")
		}
		time.Sleep(time.Millisecond)
	}
	if snapshot := tracker.Snapshot(); snapshot.DataBytes != 11 {
		t.Fatalf("active mutation published an intermediate snapshot: %+v", snapshot)
	}
	doneMutation()
	deadline = time.Now().Add(time.Second)
	for tracker.Snapshot().DataBytes != 23 {
		if time.Now().After(deadline) {
			t.Fatal("quiescent mutation snapshot was not published")
		}
		time.Sleep(time.Millisecond)
	}
}
