package vmruntime

import (
	"bytes"
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	"j5.nz/cc/client"
)

const (
	InstanceReadyMarker   = "__CCX3_READY__"
	InitDurationMarker    = "__CCX3_INIT_MS__:"
	ExecTimingMarker      = "__CCX3_TIMING__:"
	CommandBeginMarker    = "__CCX3_BEGIN__:"
	CommandOutputMarker   = "__CCX3_OUT__:"
	CommandErrorMarker    = "__CCX3_ERR__:"
	CommandUsageMarker    = "__CCX3_USAGE__:"
	CommandExitMarkerPref = "__CCX3_EXIT__:"
)

type SerialTranscript struct {
	mu   sync.Mutex
	cond *sync.Cond
	buf  bytes.Buffer
}

func NewSerialTranscript() *SerialTranscript {
	s := &SerialTranscript{}
	s.cond = sync.NewCond(&s.mu)
	return s
}

func (s *SerialTranscript) Write(data []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, err := s.buf.Write(data)
	s.cond.Broadcast()
	return n, err
}

func (s *SerialTranscript) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Len()
}

func (s *SerialTranscript) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

func (s *SerialTranscript) WaitFor(ctx context.Context, start int, predicate func(string) bool) (string, error) {
	for {
		s.mu.Lock()
		if start <= s.buf.Len() {
			text := s.buf.String()[start:]
			if predicate(text) {
				s.mu.Unlock()
				return text, nil
			}
		}
		s.mu.Unlock()

		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		time.Sleep(5 * time.Millisecond)
	}
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
