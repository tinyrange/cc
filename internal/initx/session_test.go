package initx

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/tinyrange/cc/internal/ir"
)

type fakeSessionRunner struct {
	bootErr error
	runErr  error

	bootDelay time.Duration
	runDelay  time.Duration

	stdinForwarded bool
}

func (f *fakeSessionRunner) Boot(ctx context.Context) error {
	if f.bootDelay > 0 {
		select {
		case <-time.After(f.bootDelay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return f.bootErr
}

func (f *fakeSessionRunner) StartStdinForwarding() { f.stdinForwarded = true }

func (f *fakeSessionRunner) Run(ctx context.Context, _ *ir.Program) error {
	if f.runDelay > 0 {
		select {
		case <-time.After(f.runDelay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return f.runErr
}

func TestStartSession_BootFailureReturns(t *testing.T) {
	t.Parallel()

	want := errors.New("boot failed")
	r := &fakeSessionRunner{bootErr: want}
	s := startSession(context.Background(), nil, r, &ir.Program{Entrypoint: "main", Methods: map[string]ir.Method{"main": {}}}, SessionConfig{
		BootTimeout: 50 * time.Millisecond,
	})
	if err := s.Wait(); !errors.Is(err, want) {
		t.Fatalf("Wait()=%v, want %v", err, want)
	}
	if r.stdinForwarded {
		t.Fatalf("stdin forwarding happened despite boot failure")
	}
}

func TestStartSession_StopTimeout(t *testing.T) {
	t.Parallel()

	r := &fakeSessionRunner{
		bootDelay: 100 * time.Millisecond,
	}
	s := startSession(context.Background(), nil, r, &ir.Program{Entrypoint: "main", Methods: map[string]ir.Method{"main": {}}}, SessionConfig{
		BootTimeout: 5 * time.Second,
	})

	err := s.Stop(10 * time.Millisecond)
	if err == nil {
		t.Fatalf("Stop() expected timeout error, got nil")
	}
}
