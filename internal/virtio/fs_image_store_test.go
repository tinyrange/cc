package virtio

import (
	"bytes"
	"os"
	"path/filepath"
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

func TestImageDataStoreRetainsFailedReplacementCleanupForRetry(t *testing.T) {
	store := newImageDataStore()
	dir := t.TempDir()
	stalePath := filepath.Join(dir, "replaced")
	if err := os.Mkdir(stalePath, 0o700); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(stalePath, "scanner-hold")
	if err := os.WriteFile(child, []byte("hold"), 0o600); err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	store.stale = append(store.stale, imageDataStaleFile{path: stalePath})
	err := store.cleanupStaleLocked()
	retained := len(store.stale)
	store.mu.Unlock()
	if err == nil || retained != 1 {
		t.Fatalf("failed cleanup error=%v retained=%d", err, retained)
	}
	if err := os.Remove(child); err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	err = store.cleanupStaleLocked()
	retained = len(store.stale)
	store.mu.Unlock()
	if err != nil || retained != 0 {
		t.Fatalf("cleanup retry error=%v retained=%d", err, retained)
	}
}

func TestImageDataStoreClearsRecoveredStaleCleanupCondition(t *testing.T) {
	store := newImageDataStore()
	dir := t.TempDir()
	stalePath := filepath.Join(dir, "replaced")
	if err := os.Mkdir(stalePath, 0o700); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(stalePath, "scanner-hold")
	if err := os.WriteFile(child, []byte("hold"), 0o600); err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	store.stale = append(store.stale, imageDataStaleFile{path: stalePath})
	store.mu.Unlock()
	if _, _, _, err := store.usage(); err == nil {
		t.Fatal("active stale cleanup failure was not reported")
	}
	if err := os.Remove(child); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := store.usage(); err != nil {
		t.Fatalf("successful cleanup retry retained a stale current error: %v", err)
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
	current, highWater, _, err := store.usage()
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

func TestPortableImageStoreCompactsBeforeTwofoldAmplification(t *testing.T) {
	page := uint64(imageDataPageSize)
	if !shouldCompactPortableImageStore(1000*page, 1999*page) {
		t.Fatal("nearly twofold physical amplification was retained")
	}
	if shouldCompactPortableImageStore(1000*page, 1499*page) {
		t.Fatal("sub-threshold dead space triggered compaction")
	}
	if shouldCompactPortableImageStore(1<<20, 1792<<10) {
		t.Fatal("small backing file triggered portable compaction")
	}
}
