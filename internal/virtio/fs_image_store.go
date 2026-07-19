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
	mu     sync.Mutex
	dir    string
	file   *os.File
	path   string
	next   uint64
	free   []uint64
	closed bool
	refs   int
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
	return &imageDataStore{dir: dir, refs: 1}
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
	if n := len(s.free); n > 0 {
		offset = s.free[n-1]
		s.free = s.free[:n-1]
	} else {
		offset = s.next
		s.next += imageDataPageSize
	}
	var page [imageDataPageSize]byte
	copy(page[:], data)
	if _, err := s.file.WriteAt(page[:], int64(offset)); err != nil {
		s.free = append(s.free, offset)
		return 0, err
	}
	return offset, nil
}

func (s *imageDataStore) readPage(offset uint64, dst []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file == nil {
		clear(dst)
		return nil
	}
	n, err := s.file.ReadAt(dst, int64(offset))
	if errors.Is(err, io.EOF) && n == len(dst) {
		err = nil
	}
	return err
}

func (s *imageDataStore) writeAt(offset, pageOffset uint64, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file == nil {
		return os.ErrNotExist
	}
	_, err := s.file.WriteAt(data, int64(offset+pageOffset))
	return err
}

func (s *imageDataStore) releasePage(offset uint64) {
	s.mu.Lock()
	if !s.closed {
		s.free = append(s.free, offset)
	}
	s.mu.Unlock()
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
	s.file, s.path, s.free = nil, "", nil
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
