package virtio

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// imageDataStore keeps writable imageFS pages in a sparse host file instead of
// Go heap allocations. The host VM process can therefore reclaim clean pages,
// and backing-filesystem exhaustion is returned to the guest as an ordinary
// write failure rather than becoming an unbounded host-heap failure.
type imageDataStore struct {
	mu           sync.Mutex
	dir          string
	file         *os.File
	path         string
	next         uint64
	nextLocation uint64
	// locations uses the stable allocation token as a slice index and stores
	// offset+1 (zero means released). Tokens are dense and monotonic, so a map
	// added substantial per-page heap overhead without buying sparsity.
	locations               []uint64
	liveLocations           int
	freeLocations           []uint64
	free                    []uint64
	freeSet                 map[uint64]struct{}
	pendingReclaim          map[uint64]struct{}
	current                 uint64
	highWater               uint64
	reclaimErr              error
	staleCleanupErr         error
	rangeReclaimUnsupported bool
	closed                  bool
	refs                    int
	stale                   []imageDataStaleFile
}

type imageDataStaleFile struct {
	file *os.File
	path string
}

type imageDataReclaimQueue struct {
	mu      sync.Mutex
	pending map[*imageDataStore]struct{}
	wake    chan struct{}
}

var globalImageDataReclaimer = newImageDataReclaimQueue()

func newImageDataReclaimQueue() *imageDataReclaimQueue {
	q := &imageDataReclaimQueue{pending: make(map[*imageDataStore]struct{}), wake: make(chan struct{}, 1)}
	go q.run()
	return q
}

func (q *imageDataReclaimQueue) schedule(store *imageDataStore) {
	q.mu.Lock()
	q.pending[store] = struct{}{}
	q.mu.Unlock()
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

func (q *imageDataReclaimQueue) run() {
	for range q.wake {
		// Gather adjacent 4 KiB releases into filesystem-sized operations.
		time.Sleep(5 * time.Millisecond)
		q.mu.Lock()
		stores := make([]*imageDataStore, 0, len(q.pending))
		for store := range q.pending {
			stores = append(stores, store)
		}
		clear(q.pending)
		q.mu.Unlock()
		for _, store := range stores {
			store.mu.Lock()
			store.flushReclaimLocked()
			store.mu.Unlock()
		}
	}
}

func newImageDataStore() *imageDataStore {
	dir, err := os.UserCacheDir()
	if err != nil || dir == "" {
		dir = os.TempDir()
	}
	dir = filepath.Join(dir, "cc", "imagefs")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		dir = os.TempDir()
	}
	return &imageDataStore{dir: dir, refs: 1, locations: []uint64{0}, freeSet: make(map[uint64]struct{}), pendingReclaim: make(map[uint64]struct{})}
}

func (s *imageDataStore) retain() {
	s.mu.Lock()
	if !s.closed {
		s.refs++
	}
	s.mu.Unlock()
}

func (s *imageDataStore) ensureFileLocked() error {
	if s.closed {
		return os.ErrClosed
	}
	if s.file != nil {
		return nil
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(s.dir, "pages-*")
	if err != nil {
		return err
	}
	s.file = f
	s.path = f.Name()
	// Unix keeps the open inode alive after unlink, which gives us automatic
	// cleanup if ccvm is killed by the very failure this store is meant to
	// contain. Windows keeps the named path and removes it during Close.
	if err := os.Remove(s.path); err == nil {
		s.path = ""
	}
	return nil
}

func (s *imageDataStore) allocatePage(data []byte) (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureFileLocked(); err != nil {
		return 0, err
	}
	var offset uint64
	reused := false
	for len(s.free) > 0 {
		offset = s.free[len(s.free)-1]
		s.free = s.free[:len(s.free)-1]
		if _, ok := s.freeSet[offset]; ok {
			delete(s.freeSet, offset)
			delete(s.pendingReclaim, offset)
			reused = true
			break
		}
	}
	if !reused {
		offset = s.next
		s.next += imageDataPageSize
	}
	var page [imageDataPageSize]byte
	copy(page[:], data)
	if _, err := s.file.WriteAt(page[:], int64(offset)); err != nil {
		s.free = append(s.free, offset)
		s.freeSet[offset] = struct{}{}
		return 0, err
	}
	s.current += imageDataPageSize
	if s.current > s.highWater {
		s.highWater = s.current
	}
	var location uint64
	if len(s.freeLocations) != 0 {
		location = s.freeLocations[len(s.freeLocations)-1]
		s.freeLocations = s.freeLocations[:len(s.freeLocations)-1]
		s.locations[location] = offset + 1
	} else {
		s.nextLocation++
		location = s.nextLocation
		s.locations = append(s.locations, offset+1)
	}
	s.liveLocations++
	return location, nil
}

func (s *imageDataStore) locationOffsetLocked(location uint64) (uint64, bool) {
	if location == 0 || location >= uint64(len(s.locations)) || s.locations[location] == 0 {
		return 0, false
	}
	return s.locations[location] - 1, true
}

func (s *imageDataStore) readPage(location uint64, dst []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file == nil {
		clear(dst)
		return nil
	}
	offset, ok := s.locationOffsetLocked(location)
	if !ok {
		return os.ErrNotExist
	}
	n, err := s.file.ReadAt(dst, int64(offset))
	if errors.Is(err, io.EOF) && n == len(dst) {
		err = nil
	}
	return err
}

func (s *imageDataStore) writeAt(location, pageOffset uint64, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file == nil {
		return os.ErrNotExist
	}
	offset, ok := s.locationOffsetLocked(location)
	if !ok {
		return os.ErrNotExist
	}
	_, err := s.file.WriteAt(data, int64(offset+pageOffset))
	return err
}

func (s *imageDataStore) releasePage(location uint64) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	offset, exists := s.locationOffsetLocked(location)
	if !exists {
		s.mu.Unlock()
		return
	}
	s.locations[location] = 0
	s.liveLocations--
	s.freeLocations = append(s.freeLocations, location)
	s.free = append(s.free, offset)
	s.freeSet[offset] = struct{}{}
	scheduleReclaim := len(s.pendingReclaim) == 0
	s.pendingReclaim[offset] = struct{}{}
	if s.current >= imageDataPageSize {
		s.current -= imageDataPageSize
	}
	s.mu.Unlock()
	if scheduleReclaim {
		globalImageDataReclaimer.schedule(s)
	}
}

func (s *imageDataStore) flushReclaimLocked() {
	if s.file == nil || len(s.pendingReclaim) == 0 {
		return
	}
	offsets := make([]uint64, 0, len(s.pendingReclaim))
	for offset := range s.pendingReclaim {
		if _, free := s.freeSet[offset]; free {
			offsets = append(offsets, offset)
		}
	}
	clear(s.pendingReclaim)

	oldNext := s.next
	for s.next >= imageDataPageSize {
		tail := s.next - imageDataPageSize
		if _, free := s.freeSet[tail]; !free {
			break
		}
		delete(s.freeSet, tail)
		s.next = tail
	}
	if s.next != oldNext {
		if err := s.file.Truncate(int64(s.next)); err != nil && s.reclaimErr == nil {
			s.reclaimErr = err
		}
	}

	sort.Slice(offsets, func(i, j int) bool { return offsets[i] < offsets[j] })
	unsupported := false
	for i := 0; i < len(offsets); {
		start := offsets[i]
		if start >= s.next {
			i++
			continue
		}
		end := start + imageDataPageSize
		i++
		for i < len(offsets) && offsets[i] == end && end < s.next {
			end += imageDataPageSize
			i++
		}
		err := reclaimFileRange(s.file, int64(start), int64(end-start))
		if errors.Is(err, errRangeReclaimUnsupported) {
			unsupported = true
			s.rangeReclaimUnsupported = true
			break
		}
		if err != nil && s.reclaimErr == nil {
			s.reclaimErr = err
		}
	}
	if (unsupported || s.rangeReclaimUnsupported) && shouldCompactPortableImageStore(s.current, s.next) {
		if err := s.compactLocked(); err != nil && s.reclaimErr == nil {
			s.reclaimErr = err
		}
	}
}

func shouldCompactPortableImageStore(live, allocated uint64) bool {
	if allocated < 4<<20 || allocated <= live {
		return false
	}
	dead := allocated - live
	return dead >= (live+1)/2
}

var errRangeReclaimUnsupported = errors.New("backing filesystem does not support range reclamation")

func (s *imageDataStore) compactLocked() error {
	if s.file == nil || s.liveLocations == 0 {
		return nil
	}
	f, err := os.CreateTemp(s.dir, "pages-compact-*")
	if err != nil {
		return err
	}
	path := f.Name()
	if err := os.Remove(path); err == nil {
		path = ""
	}
	cleanup := func() {
		_ = f.Close()
		if path != "" {
			_ = os.Remove(path)
		}
	}
	newLocations := make([]uint64, len(s.locations))
	var page [imageDataPageSize]byte
	var next uint64
	for location, encodedOffset := range s.locations {
		if encodedOffset == 0 {
			continue
		}
		oldOffset := encodedOffset - 1
		n, readErr := s.file.ReadAt(page[:], int64(oldOffset))
		if readErr != nil && !(errors.Is(readErr, io.EOF) && n == len(page)) {
			cleanup()
			return readErr
		}
		if _, err := f.WriteAt(page[:], int64(next)); err != nil {
			cleanup()
			return err
		}
		newLocations[location] = next + 1
		next += imageDataPageSize
	}
	oldFile, oldPath := s.file, s.path
	s.file, s.path, s.locations, s.next = f, path, newLocations, next
	s.free = nil
	s.freeSet = make(map[uint64]struct{})
	s.pendingReclaim = make(map[uint64]struct{})
	// The replacement is live, but cleanup ownership for the old file must not
	// disappear if Windows or an external scanner makes close/removal fail
	// transiently. Retain it for later sync/usage/close retries and report the
	// failure without rolling back the valid compacted store.
	s.stale = append(s.stale, imageDataStaleFile{file: oldFile, path: oldPath})
	s.staleCleanupErr = s.cleanupStaleLocked()
	return nil
}

func (s *imageDataStore) cleanupStaleLocked() error {
	var pending []imageDataStaleFile
	var errs []error
	for _, stale := range s.stale {
		if stale.file != nil {
			if err := stale.file.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
				errs = append(errs, fmt.Errorf("close replaced image backing file: %w", err))
				pending = append(pending, stale)
				continue
			}
			stale.file = nil
		}
		if stale.path != "" {
			if err := os.Remove(stale.path); err != nil && !errors.Is(err, os.ErrNotExist) {
				errs = append(errs, fmt.Errorf("remove replaced image backing file %q: %w", stale.path, err))
				pending = append(pending, stale)
			}
		}
	}
	s.stale = pending
	return errors.Join(errs...)
}

func (s *imageDataStore) usage() (current, highWater, physical uint64, reclaimErr error) {
	if s == nil {
		return 0, 0, 0, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.staleCleanupErr = s.cleanupStaleLocked()
	if s.file != nil {
		physical, reclaimErr = allocatedFileBytes(s.file)
	}
	return s.current, s.highWater, physical, errors.Join(s.reclaimErr, s.staleCleanupErr, reclaimErr)
}

func (s *imageDataStore) metadataUsage() uint64 {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return uint64(cap(s.locations))*8 + uint64(cap(s.freeLocations)+len(s.freeSet)+len(s.pendingReclaim))*16
}

func (s *imageDataStore) sync() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.flushReclaimLocked()
	cleanupErr := s.cleanupStaleLocked()
	s.staleCleanupErr = cleanupErr
	if s.file == nil {
		return cleanupErr
	}
	return errors.Join(cleanupErr, s.file.Sync())
}

func (s *imageDataStore) close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.refs--
	if s.refs > 0 {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.flushReclaimLocked()
	f, path, stale := s.file, s.path, s.stale
	s.file, s.path, s.locations, s.freeLocations, s.free, s.freeSet, s.pendingReclaim, s.stale = nil, "", nil, nil, nil, nil, nil, nil
	s.mu.Unlock()
	var errs []error
	if f != nil {
		errs = append(errs, f.Close())
	}
	if path != "" {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	for _, old := range stale {
		if old.file != nil {
			if err := old.file.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
				errs = append(errs, err)
			}
		}
		if old.path != "" {
			if err := os.Remove(old.path); err != nil && !errors.Is(err, os.ErrNotExist) {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}
