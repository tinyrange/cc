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
