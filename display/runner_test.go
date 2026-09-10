package display

import (
	"context"
	"image"
	"sync/atomic"
	"testing"
	"time"
)

type serialSession struct {
	Session
	executing atomic.Bool
	keys      atomic.Int32
	overlap   atomic.Bool
}

func (s *serialSession) Key(uint16, bool) error {
	if s.executing.Load() {
		s.overlap.Store(true)
	}
	s.keys.Add(1)
	return nil
}
func (s *serialSession) Snapshot(image.Rectangle, uint64, bool) FramebufferUpdate {
	if s.executing.Load() {
		s.overlap.Store(true)
	}
	return FramebufferUpdate{Width: 640, Height: 480}
}

func TestRunnerInterruptsExecutionForInputAndResumesDuringPresentation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	s := &serialSession{}
	started := make(chan struct{}, 16)
	r := StartRunner(ctx, s, func(ctx context.Context) error {
		s.executing.Store(true)
		select {
		case started <- struct{}{}:
		default:
		}
		<-ctx.Done()
		s.executing.Store(false)
		return nil
	})
	defer r.Close()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("VM did not start")
	}
	if err := r.Key(30, true); err != nil {
		t.Fatal(err)
	}
	frame := r.Snapshot(image.Rectangle{}, 0, false)
	if frame.Width != 640 || s.keys.Load() != 1 || s.overlap.Load() {
		t.Fatal("display access overlapped execution or lost input")
	}
	// No presentation callback or sleep is needed to allow execution to resume.
	for !s.executing.Load() {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("VM remained paused during presentation")
		}
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if err := r.Key(30, false); err == nil {
		t.Fatal("input accepted after close")
	}
}
