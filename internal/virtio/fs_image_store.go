package virtio

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
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
	locations    map[uint64]uint64
	free         []uint64
	freeSet      map[uint64]struct{}
	current      uint64
	highWater    uint64
	reclaimErr   error
	closed       bool
	refs         int
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
	return &imageDataStore{dir: dir, refs: 1, locations: make(map[uint64]uint64), freeSet: make(map[uint64]struct{})}
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
	s.nextLocation++
	location := s.nextLocation
	s.locations[location] = offset
	return location, nil
}

func (s *imageDataStore) readPage(location uint64, dst []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file == nil {
		clear(dst)
		return nil
	}
	offset, ok := s.locations[location]
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
	offset, ok := s.locations[location]
	if !ok {
		return os.ErrNotExist
	}
	_, err := s.file.WriteAt(data, int64(offset+pageOffset))
	return err
}

func (s *imageDataStore) releasePage(location uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	offset, exists := s.locations[location]
	if !exists {
		return
	}
	delete(s.locations, location)
	s.free = append(s.free, offset)
	s.freeSet[offset] = struct{}{}
	if s.current >= imageDataPageSize {
		s.current -= imageDataPageSize
	}
	if s.file != nil {
		reclaimErr := reclaimFileRange(s.file, int64(offset), int64(imageDataPageSize))
		if reclaimErr != nil && !errors.Is(reclaimErr, errRangeReclaimUnsupported) && s.reclaimErr == nil {
			s.reclaimErr = reclaimErr
		}
		// Tail coalescing is portable and returns all blocks after a large
		// recently-created file is deleted even where hole punching is absent.
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
		if errors.Is(reclaimErr, errRangeReclaimUnsupported) && len(s.freeSet) > len(s.locations) && s.next >= 8<<20 {
			if err := s.compactLocked(); err != nil && s.reclaimErr == nil {
				s.reclaimErr = err
			}
		}
	}
}

var errRangeReclaimUnsupported = errors.New("backing filesystem does not support range reclamation")

func (s *imageDataStore) compactLocked() error {
	if s.file == nil || len(s.locations) == 0 {
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
	newLocations := make(map[uint64]uint64, len(s.locations))
	var page [imageDataPageSize]byte
	var next uint64
	for location, oldOffset := range s.locations {
		n, readErr := s.file.ReadAt(page[:], int64(oldOffset))
		if readErr != nil && !(errors.Is(readErr, io.EOF) && n == len(page)) {
			cleanup()
			return readErr
		}
		if _, err := f.WriteAt(page[:], int64(next)); err != nil {
			cleanup()
			return err
		}
		newLocations[location] = next
		next += imageDataPageSize
	}
	oldFile, oldPath := s.file, s.path
	s.file, s.path, s.locations, s.next = f, path, newLocations, next
	s.free = nil
	s.freeSet = make(map[uint64]struct{})
	if err := oldFile.Close(); err != nil {
		return err
	}
	if oldPath != "" {
		if err := os.Remove(oldPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func (s *imageDataStore) usage() (current, highWater uint64, reclaimErr error) {
	if s == nil {
		return 0, 0, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.current, s.highWater, s.reclaimErr
}

func (s *imageDataStore) sync() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file == nil {
		return nil
	}
	return s.file.Sync()
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
	f, path := s.file, s.path
	s.file, s.path, s.locations, s.free, s.freeSet = nil, "", nil, nil, nil
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
	return errors.Join(errs...)
}
