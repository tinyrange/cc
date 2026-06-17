//go:build linux && (amd64 || arm64)

package kvm

import (
	"context"
	"fmt"
	"sync"
)

type sessionDone struct {
	ch   chan struct{}
	once sync.Once
	mu   sync.Mutex
	err  error
}

func newSessionDone() *sessionDone {
	return &sessionDone{ch: make(chan struct{})}
}

func (d *sessionDone) finish(err error) {
	if d == nil {
		return
	}
	d.once.Do(func() {
		d.mu.Lock()
		d.err = err
		d.mu.Unlock()
		close(d.ch)
	})
}

func (d *sessionDone) wait() error {
	if d == nil {
		return nil
	}
	<-d.ch
	return d.result()
}

func (d *sessionDone) result() error {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.err
}

func (d *sessionDone) done() <-chan struct{} {
	if d == nil {
		return nil
	}
	return d.ch
}

func sessionExitError(err error) error {
	if err != nil {
		return fmt.Errorf("managed session exited: %w", err)
	}
	return fmt.Errorf("managed session exited")
}

func (s *ManagedSession) waitForTranscript(ctx context.Context, start int, predicate func(string) bool) (string, error) {
	if s == nil || s.transcript == nil {
		return "", fmt.Errorf("managed session is not running")
	}
	waitCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	type result struct {
		segment string
		err     error
	}
	resultCh := make(chan result, 1)
	go func() {
		segment, err := s.transcript.WaitFor(waitCtx, start, predicate)
		resultCh <- result{segment: segment, err: err}
	}()

	select {
	case res := <-resultCh:
		return res.segment, res.err
	case <-s.done.done():
		return "", sessionExitError(s.done.result())
	case <-ctx.Done():
		return "", ctx.Err()
	}
}
