package vmruntime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
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
	mu         sync.Mutex
	buf        []byte
	file       *os.File
	path       string
	base       int
	fileBase   int
	size       int
	tail       []byte
	readers    map[uint64]int
	nextReader uint64
	closed     bool
	spillErr   error
	reclaimErr error
	reclaimAt  int
	staleFiles []*os.File
	stalePaths []string
}

const (
	serialTranscriptMemoryBytes  = 1 << 20
	serialTranscriptReadBytes    = 256 << 10
	serialTranscriptTailBytes    = 64 << 10
	serialTranscriptWaitBytes    = 1 << 20
	serialTranscriptReclaimBytes = 8 << 20
)

type TranscriptReader interface {
	Advance(int)
	Close()
}

type serialTranscriptReader struct {
	transcript *SerialTranscript
	id         uint64
	once       sync.Once
}

func NewSerialTranscript() *SerialTranscript {
	s := &SerialTranscript{readers: make(map[uint64]int)}
	runtime.SetFinalizer(s, (*SerialTranscript).finalize)
	return s
}

func (s *SerialTranscript) Write(data []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, os.ErrClosed
	}
	if s.file == nil && len(s.buf)+len(data) <= serialTranscriptMemoryBytes {
		s.buf = append(s.buf, data...)
		s.size += len(data)
		s.appendTailLocked(data)
		return len(data), nil
	}
	if s.file == nil {
		if err := s.openSpillLocked(); err != nil {
			s.spillErr = err
			return 0, err
		}
	}
	n, err := s.file.WriteAt(data, int64(s.size-s.fileBase))
	s.size += n
	s.appendTailLocked(data[:n])
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
	s.file, s.path, s.buf, s.fileBase = f, f.Name(), nil, s.base
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
	if len(s.tail) == s.size {
		return string(s.tail)
	}
	return fmt.Sprintf("[... %d earlier transcript bytes omitted ...]\n%s", s.size-len(s.tail), s.tail)
}

func (s *SerialTranscript) appendTailLocked(data []byte) {
	if len(data) >= serialTranscriptTailBytes {
		s.tail = append(s.tail[:0], data[len(data)-serialTranscriptTailBytes:]...)
		return
	}
	if drop := len(s.tail) + len(data) - serialTranscriptTailBytes; drop > 0 {
		copy(s.tail, s.tail[drop:])
		s.tail = s.tail[:len(s.tail)-drop]
	}
	s.tail = append(s.tail, data...)
}

// RetainFrom registers a live reader. Releasing it permits transcript storage
// older than every remaining reader to be reclaimed. Offsets remain absolute,
// so concurrent commands can finish in any order without invalidating cursors.
func (s *SerialTranscript) RetainFrom(offset int) func() {
	reader := s.RetainReader(offset)
	return reader.Close
}

// RetainReader registers a movable reader cursor. Streaming parsers advance
// it after copying bytes into their own small framing buffer, which prevents a
// quiet, long-lived command from pinning unrelated later output.
func (s *SerialTranscript) RetainReader(offset int) TranscriptReader {
	if s == nil {
		return &serialTranscriptReader{}
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return &serialTranscriptReader{}
	}
	if offset < s.base {
		offset = s.base
	}
	s.nextReader++
	id := s.nextReader
	s.readers[id] = offset
	s.mu.Unlock()
	return &serialTranscriptReader{transcript: s, id: id}
}

func (r *serialTranscriptReader) Advance(offset int) {
	if r == nil || r.transcript == nil {
		return
	}
	s := r.transcript
	s.mu.Lock()
	if current, ok := s.readers[r.id]; ok && offset > current {
		if offset > s.size {
			offset = s.size
		}
		s.readers[r.id] = offset
		s.discardToReadersLocked()
	}
	s.mu.Unlock()
}

func (r *serialTranscriptReader) Close() {
	if r == nil || r.transcript == nil {
		return
	}
	r.once.Do(func() {
		s := r.transcript
		s.mu.Lock()
		delete(s.readers, r.id)
		s.discardToReadersLocked()
		s.mu.Unlock()
	})
}

func (s *SerialTranscript) discardToReadersLocked() {
	target := s.size
	for _, readerOffset := range s.readers {
		if readerOffset < target {
			target = readerOffset
		}
	}
	s.discardBeforeLocked(target)
}

func (s *SerialTranscript) discardBeforeLocked(offset int) {
	if offset <= s.base {
		return
	}
	if offset > s.size {
		offset = s.size
	}
	if s.file == nil {
		drop := offset - s.base
		if drop >= len(s.buf) {
			s.buf = nil
		} else {
			s.buf = s.buf[drop:]
		}
		s.base = offset
		return
	}
	if offset == s.size {
		s.base = offset
		if offset-s.fileBase < serialTranscriptReclaimBytes {
			return
		}
		if err := s.file.Truncate(0); err != nil {
			s.spillErr = errors.Join(s.spillErr, err)
			return
		}
		s.fileBase = offset
		return
	}
	s.base = offset
	s.compactConsumedPrefixLocked()
}

func (s *SerialTranscript) compactConsumedPrefixLocked() {
	consumed := s.base - s.fileBase
	live := s.size - s.base
	if consumed < serialTranscriptReclaimBytes || consumed < live || s.base-s.reclaimAt < serialTranscriptReclaimBytes {
		return
	}
	s.reclaimAt = s.base
	dir := filepath.Dir(s.file.Name())
	next, err := os.CreateTemp(dir, "serial-reclaim-*")
	if err == nil && live > 0 {
		_, err = io.CopyN(next, io.NewSectionReader(s.file, int64(consumed), int64(live)), int64(live))
	}
	if err != nil {
		if next != nil {
			name := next.Name()
			_ = next.Close()
			_ = os.Remove(name)
		}
		s.reclaimErr = errors.Join(s.reclaimErr, err)
		slog.Warn("serial transcript prefix reclamation failed; preserving the existing spill file", "error", err)
		return
	}
	name := next.Name()
	old, oldPath := s.file, s.path
	s.file = next
	s.fileBase = s.base
	if removeErr := os.Remove(name); removeErr != nil {
		s.path = name
	} else {
		s.path = ""
	}
	if closeErr := old.Close(); closeErr != nil {
		s.reclaimErr = errors.Join(s.reclaimErr, closeErr)
		if _, statErr := old.Stat(); statErr == nil {
			s.staleFiles = append(s.staleFiles, old)
		}
		slog.Warn("serial transcript reclaimed its consumed prefix but could not close the old spill file", "error", closeErr)
	} else if oldPath != "" {
		if removeErr := os.Remove(oldPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			s.reclaimErr = errors.Join(s.reclaimErr, removeErr)
			s.stalePaths = append(s.stalePaths, oldPath)
			slog.Warn("serial transcript reclaimed its consumed prefix but could not remove the old spill file", "path", oldPath, "error", removeErr)
		}
	}
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
	if offset < s.base {
		offset = s.base
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
	if start < s.base {
		start = s.base
	}
	if end > s.size {
		end = s.size
	}
	if start >= end {
		return nil, nil
	}
	if s.file == nil {
		return append([]byte(nil), s.buf[start-s.base:end-s.base]...), s.spillErr
	}
	data := make([]byte, end-start)
	n, err := s.file.ReadAt(data, int64(start-s.fileBase))
	if errors.Is(err, io.EOF) && n == len(data) {
		err = nil
	}
	return data[:n], err
}

func (s *SerialTranscript) WaitFor(ctx context.Context, start int, predicate func(string) bool) (string, error) {
	return s.waitFor(ctx, start, "", false, predicate)
}

// WaitForCommand permits complete compatibility output to be materialized
// only after this command's terminal record is present. An unrelated command
// completing on the shared control transcript must not repeatedly read a
// large active command back from disk.
func (s *SerialTranscript) WaitForCommand(ctx context.Context, start int, id string, predicate func(string) bool) (string, error) {
	return s.waitFor(ctx, start, id, false, predicate)
}

// WaitForCommandEvent waits on the requested command's filtered protocol
// records before command exit. It is intended for bounded lifecycle events
// such as input readiness; complete output waits should use WaitForCommand.
func (s *SerialTranscript) WaitForCommandEvent(ctx context.Context, start int, id string, predicate func(string) bool) (string, error) {
	return s.waitFor(ctx, start, id, true, predicate)
}

func (s *SerialTranscript) waitFor(ctx context.Context, start int, commandID string, beforeExit bool, predicate func(string) bool) (string, error) {
	reader := s.RetainReader(start)
	defer reader.Close()
	var window []byte
	var partialLine []byte
	var commandRecords *SerialTranscript
	if commandID != "" {
		commandRecords = NewSerialTranscript()
		defer commandRecords.Close()
	}
	terminalDirty := false
	offset := start
	dirty := false
	for {
		text, next := s.ReadFrom(offset)
		if next > offset {
			if commandID != "" {
				var terminal bool
				var err error
				partialLine, terminal, err = appendCommandRecords(commandRecords, partialLine, []byte(text), commandID)
				if err != nil {
					return "", err
				}
				terminalDirty = terminalDirty || terminal
			}
			window = append(window, text...)
			if len(window) > serialTranscriptWaitBytes {
				copy(window, window[len(window)-serialTranscriptWaitBytes:])
				window = window[:serialTranscriptWaitBytes]
			}
			offset = next
			reader.Advance(offset)
			dirty = true
			continue
		} else {
			s.mu.Lock()
			err := s.spillErr
			closed := s.closed
			s.mu.Unlock()
			if err != nil {
				return "", err
			}
			if closed {
				return "", os.ErrClosed
			}
		}

		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		// Debounce the predicate until the producer is momentarily quiescent.
		// Managed stdout arrives as many small protocol lines; reparsing the
		// complete command after every line is otherwise quadratic even though
		// the backing transcript itself is streamed.
		time.Sleep(5 * time.Millisecond)
		if s.Len() > offset {
			continue
		}
		if dirty {
			candidate := window
			if commandID != "" {
				if record, terminal := commandRecord(partialLine, commandID); len(record) != 0 && terminal {
					terminalDirty = true
				}
				if !terminalDirty && !beforeExit {
					dirty = false
					continue
				}
				var err error
				candidate, err = commandRecords.materialize()
				if err != nil {
					return "", err
				}
				if record, _ := commandRecord(partialLine, commandID); len(record) != 0 {
					candidate = append(candidate, record...)
				}
				terminalDirty = false
			}
			if predicate(string(candidate)) {
				return string(candidate), nil
			}
		}
		dirty = false
	}
}

func appendCommandRecords(records io.Writer, partial, data []byte, id string) ([]byte, bool, error) {
	partial = append(partial, data...)
	terminalSeen := false
	for {
		newline := bytes.IndexByte(partial, '\n')
		if newline < 0 {
			return partial, terminalSeen, nil
		}
		line := partial[:newline]
		if record, terminal := commandRecord(line, id); len(record) != 0 {
			if _, err := records.Write(record); err != nil {
				return partial, terminalSeen, err
			}
			if _, err := records.Write([]byte{'\n'}); err != nil {
				return partial, terminalSeen, err
			}
			terminalSeen = terminalSeen || terminal
		}
		partial = partial[newline+1:]
	}
}

func commandRecord(line []byte, id string) ([]byte, bool) {
	prefixes := []string{
		CommandBeginMarker + id,
		CommandOutputMarker + id + ":",
		CommandErrorMarker + id + ":",
		CommandControlMarker + id + ":",
		CommandUsageMarker + id + ":",
		ExecTimingMarker + id + ":",
		CommandExitMarkerPref + id + ":",
	}
	for _, prefix := range prefixes {
		if index := bytes.Index(line, []byte(prefix)); index >= 0 {
			return line[index:], prefix == CommandExitMarkerPref+id+":"
		}
	}
	return nil, false
}

func (s *SerialTranscript) materialize() ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out bytes.Buffer
	for offset := s.base; offset < s.size; {
		next := min(s.size, offset+serialTranscriptReadBytes)
		data, err := s.readLocked(offset, next)
		if err != nil {
			return nil, err
		}
		if len(data) == 0 {
			break
		}
		_, _ = out.Write(data)
		offset += len(data)
	}
	return out.Bytes(), nil
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
	f, path, staleFiles, stalePaths := s.file, s.path, s.staleFiles, s.stalePaths
	s.file, s.path, s.buf, s.tail, s.readers, s.size = nil, "", nil, nil, nil, 0
	s.staleFiles, s.stalePaths = nil, nil
	s.mu.Unlock()
	var errs []error
	if f != nil {
		errs = append(errs, f.Close())
	}
	for _, stale := range staleFiles {
		if stale != nil {
			errs = append(errs, stale.Close())
		}
	}
	if path != "" {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	for _, stalePath := range stalePaths {
		if err := os.Remove(stalePath); err != nil && !errors.Is(err, os.ErrNotExist) {
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
