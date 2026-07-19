package vmruntime

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/managed/protocol"
)

const (
	InstanceReadyMarker   = protocol.ReadyMarker
	InitDurationMarker    = "__CCX3_INIT_MS__:"
	ExecTimingMarker      = protocol.TimingMarkerPrefix
	CommandBeginMarker    = protocol.BeginMarkerPrefix
	CommandOutputMarker   = protocol.OutputMarkerPrefix
	CommandErrorMarker    = protocol.ErrorMarkerPrefix
	CommandControlMarker  = protocol.ControlMarkerPrefix
	CommandUsageMarker    = protocol.UsageMarkerPrefix
	CommandExitMarkerPref = protocol.ExitMarkerPrefix
)

type SerialTranscript struct {
	mu       sync.Mutex
	buf      []byte
	file     *os.File
	path     string
	size     int
	closed   bool
	spillErr error
}

const (
	serialTranscriptMemoryBytes = 1 << 20
	serialTranscriptReadBytes   = 256 << 10
)

func NewSerialTranscript() *SerialTranscript {
	s := &SerialTranscript{}
	runtime.SetFinalizer(s, (*SerialTranscript).finalize)
	return s
}

func (s *SerialTranscript) Write(data []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, os.ErrClosed
	}
	if s.file == nil && s.size+len(data) <= serialTranscriptMemoryBytes {
		s.buf = append(s.buf, data...)
		s.size += len(data)
		return len(data), nil
	}
	if s.file == nil {
		if err := s.openSpillLocked(); err != nil {
			s.spillErr = err
			return 0, err
		}
	}
	n, err := s.file.WriteAt(data, int64(s.size))
	s.size += n
	return n, err
}

func (s *SerialTranscript) openSpillLocked() error {
	dir, err := os.UserCacheDir()
	if err != nil || dir == "" {
		dir = os.TempDir()
	}
	dir = filepath.Join(dir, "cc", "transcripts")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "serial-*")
	if err != nil {
		return err
	}
	if len(s.buf) > 0 {
		if _, err := f.Write(s.buf); err != nil {
			_ = f.Close()
			_ = os.Remove(f.Name())
			return err
		}
	}
	s.file, s.path, s.buf = f, f.Name(), nil
	if err := os.Remove(s.path); err == nil {
		s.path = ""
	}
	return nil
}

func (s *SerialTranscript) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.size
}

func (s *SerialTranscript) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, _ := s.readLocked(0, s.size)
	return string(data)
}

// ReadFrom returns only transcript bytes at and after offset. Callers which
// follow a live serial stream must not repeatedly materialize the entire boot
// transcript: guest output can be arbitrarily large, and doing so lets bulk
// output delay unrelated command lifecycle and cancellation events.
func (s *SerialTranscript) ReadFrom(offset int) (string, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if offset < 0 {
		offset = 0
	}
	if offset > s.size {
		offset = s.size
	}
	end := min(s.size, offset+serialTranscriptReadBytes)
	data, _ := s.readLocked(offset, end)
	return string(data), end
}

func (s *SerialTranscript) readLocked(start, end int) ([]byte, error) {
	if s.closed {
		return nil, os.ErrClosed
	}
	if start < 0 {
		start = 0
	}
	if end > s.size {
		end = s.size
	}
	if start >= end {
		return nil, nil
	}
	if s.file == nil {
		return append([]byte(nil), s.buf[start:end]...), s.spillErr
	}
	data := make([]byte, end-start)
	n, err := s.file.ReadAt(data, int64(start))
	if errors.Is(err, io.EOF) && n == len(data) {
		err = nil
	}
	return data[:n], err
}

func (s *SerialTranscript) WaitFor(ctx context.Context, start int, predicate func(string) bool) (string, error) {
	for {
		s.mu.Lock()
		data, err := s.readLocked(start, s.size)
		s.mu.Unlock()
		if err != nil {
			return "", err
		}
		text := string(data)
		if predicate(text) {
			return text, nil
		}

		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (s *SerialTranscript) Close() error {
	if s == nil {
		return nil
	}
	runtime.SetFinalizer(s, nil)
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	f, path := s.file, s.path
	s.file, s.path, s.buf, s.size = nil, "", nil, 0
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

func (s *SerialTranscript) finalize() {
	_ = s.Close()
}

type BootEventWriter struct {
	ch       chan string
	done     chan struct{}
	callback func(client.BootEvent) error
	closeMu  sync.Mutex
	closed   bool
	errMu    sync.Mutex
	err      error
}

func NewBootEventWriter(callback func(client.BootEvent) error) *BootEventWriter {
	w := &BootEventWriter{
		ch:       make(chan string, 128),
		done:     make(chan struct{}),
		callback: callback,
	}
	go func() {
		defer close(w.done)
		for chunk := range w.ch {
			if w.callback == nil || chunk == "" {
				continue
			}
			if err := w.callback(client.BootEvent{Kind: "serial", Data: chunk}); err != nil {
				w.errMu.Lock()
				if w.err == nil {
					w.err = err
				}
				w.errMu.Unlock()
				return
			}
		}
	}()
	return w
}

func (w *BootEventWriter) Write(data []byte) (int, error) {
	if len(data) == 0 {
		return 0, nil
	}
	w.closeMu.Lock()
	if w.closed {
		w.closeMu.Unlock()
		return len(data), nil
	}
	select {
	case w.ch <- string(append([]byte(nil), data...)):
	default:
	}
	w.closeMu.Unlock()
	return len(data), nil
}

func (w *BootEventWriter) Close() error {
	w.closeMu.Lock()
	if !w.closed {
		w.closed = true
		close(w.ch)
	}
	w.closeMu.Unlock()
	<-w.done
	w.errMu.Lock()
	defer w.errMu.Unlock()
	return w.err
}

func HasFatalBootText(text string) bool {
	fatalMarkers := []string{
		"ccx3-init-fatal:",
		"Kernel panic",
		"kernel panic",
		"panic: ",
		"not syncing",
		"reboot: System halted",
	}
	for _, marker := range fatalMarkers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func ParseInitDurationMarker(text string) (int, bool) {
	idx := strings.LastIndex(text, InitDurationMarker)
	if idx < 0 {
		return 0, false
	}
	rest := text[idx+len(InitDurationMarker):]
	end := strings.IndexByte(rest, '\n')
	if end >= 0 {
		rest = rest[:end]
	}
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return 0, false
	}
	ms, err := strconv.Atoi(rest)
	if err != nil {
		return 0, false
	}
	return ms, true
}
