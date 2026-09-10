package display

import (
	"context"
	"image"
	"sync"
)

// Runner serializes VM execution and display access on a worker. Presentation
// does not pause the VM: a session request interrupts advance only long enough
// to copy a frame or submit input. advance must return when its context ends.
// The underlying session's Snapshot result must own its pixels.
type Runner struct {
	session  Session
	ctx      context.Context
	cancel   context.CancelFunc
	requests chan func()
	done     chan struct{}
	mu       sync.Mutex
	wake     context.CancelFunc
	err      error
}

func StartRunner(ctx context.Context, session Session, advance func(context.Context) error) *Runner {
	ctx, cancel := context.WithCancel(ctx)
	r := &Runner{session: session, ctx: ctx, cancel: cancel, requests: make(chan func(), 64), done: make(chan struct{})}
	go func() {
		defer close(r.done)
		for ctx.Err() == nil {
			step, stop := context.WithCancel(ctx)
			r.mu.Lock()
			r.wake = stop
			r.mu.Unlock()
			for {
				select {
				case request := <-r.requests:
					request()
				default:
					goto drained
				}
			}
		drained:
			err := advance(step)
			stop()
			if err != nil {
				r.mu.Lock()
				r.err = err
				r.mu.Unlock()
				return
			}
		}
	}()
	return r
}

func (r *Runner) call(fn func()) bool {
	complete := make(chan struct{})
	select {
	case r.requests <- func() { fn(); close(complete) }:
	case <-r.done:
		return false
	}
	r.mu.Lock()
	if r.wake != nil {
		r.wake()
	}
	r.mu.Unlock()
	select {
	case <-complete:
		return true
	case <-r.done:
		return false
	}
}
func (r *Runner) Err() error       { r.mu.Lock(); defer r.mu.Unlock(); return r.err }
func (r *Runner) Close() error     { r.cancel(); <-r.done; return r.Err() }
func (r *Runner) Size() (w, h int) { r.call(func() { w, h = r.session.Size() }); return }
func (r *Runner) Snapshot(rect image.Rectangle, since uint64, incremental bool) (frame FramebufferUpdate) {
	r.call(func() { frame = r.session.Snapshot(rect, since, incremental) })
	return
}
func (r *Runner) Changed() <-chan struct{} { return nil }
func (r *Runner) Resize(w, h int) (err error) {
	if !r.call(func() { err = r.session.Resize(w, h) }) {
		return r.closedError()
	}
	return
}
func (r *Runner) Key(code uint16, down bool) (err error) {
	if !r.call(func() { err = r.session.Key(code, down) }) {
		return r.closedError()
	}
	return
}
func (r *Runner) Pointer(x, y uint32, buttons, previous uint8) (err error) {
	if !r.call(func() { err = r.session.Pointer(x, y, buttons, previous) }) {
		return r.closedError()
	}
	return
}
func (r *Runner) Scroll(x, y int32) (err error) {
	if !r.call(func() {
		if s, ok := r.session.(HighResolutionScroller); ok {
			err = s.Scroll(x, y)
		}
	}) {
		return r.closedError()
	}
	return
}
func (r *Runner) SetClipboard(text string) { r.call(func() { r.session.SetClipboard(text) }) }
func (r *Runner) GuestClipboard() (text string, generation uint64) {
	r.call(func() { text, generation = r.session.GuestClipboard() })
	return
}
func (r *Runner) closedError() error {
	if err := r.Err(); err != nil {
		return err
	}
	return context.Canceled
}
