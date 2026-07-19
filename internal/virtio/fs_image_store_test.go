package virtio

import (
	"bytes"
	"testing"
)

func TestImageDataStorePortableCompactionPreservesLiveLocations(t *testing.T) {
	store := newImageDataStore()
	defer store.close()
	locations := make([]uint64, 3)
	for i := range locations {
		page := bytes.Repeat([]byte{byte(i + 1)}, int(imageDataPageSize))
		location, err := store.allocatePage(page)
		if err != nil {
			t.Fatal(err)
		}
		locations[i] = location
	}
	store.releasePage(locations[0])
	store.releasePage(locations[1])
	store.mu.Lock()
	err := store.compactLocked()
	next := store.next
	store.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if next != imageDataPageSize {
		t.Fatalf("compacted backing size = %d, want %d", next, imageDataPageSize)
	}
	page := make([]byte, imageDataPageSize)
	if err := store.readPage(locations[2], page); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(page, bytes.Repeat([]byte{3}, len(page))) {
		t.Fatal("live page changed during compaction")
	}
}

func TestImageDataStoreBatchesReleasedPagesAndAccountsImmediately(t *testing.T) {
	store := newImageDataStore()
	defer store.close()
	locations := make([]uint64, 256)
	for i := range locations {
		location, err := store.allocatePage([]byte{byte(i)})
		if err != nil {
			t.Fatal(err)
		}
		locations[i] = location
	}
	for _, location := range locations {
		store.releasePage(location)
	}
	current, highWater, err := store.usage()
	if current != 0 || highWater != uint64(len(locations))*imageDataPageSize || err != nil {
		t.Fatalf("usage after logical release = %d, %d, %v", current, highWater, err)
	}
	if err := store.sync(); err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	next := store.next
	store.mu.Unlock()
	if next != 0 {
		t.Fatalf("backing length after batched reclaim = %d, want 0", next)
	}
}
