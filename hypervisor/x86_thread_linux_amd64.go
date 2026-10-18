//go:build linux && amd64

package hypervisor

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"

	"golang.org/x/sys/unix"
)

type x86ThreadRequest struct {
	run   func() error
	reply chan error
}

// x86Thread owns one vCPU for its entire lifetime, not just KVM_RUN. In
// particular, completing an I/O instruction or inspecting registers must not
// transfer the vCPU to the device coordinator's host thread.
type x86Thread struct {
	mu     sync.Mutex
	jobs   chan x86ThreadRequest
	done   chan struct{}
	tid    atomic.Int32
	closed bool
}

func newX86Thread() *x86Thread {
	t := &x86Thread{jobs: make(chan x86ThreadRequest), done: make(chan struct{})}
	ready := make(chan struct{})
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(t.done)
		t.tid.Store(int32(unix.Gettid()))
		defer t.tid.Store(0)
		close(ready)
		for request := range t.jobs {
			request.reply <- request.run()
		}
	}()
	<-ready
	return t
}

func (t *x86Thread) call(fn func() error) error {
	// RunUntil callbacks already execute on the owner and may call CPU APIs.
	// A locked owner thread cannot be lent to another goroutine while blocked.
	if tid := t.tid.Load(); tid != 0 && int(tid) == unix.Gettid() {
		return fn()
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return fmt.Errorf("vCPU owner is closed")
	}
	reply := make(chan error, 1)
	t.jobs <- x86ThreadRequest{fn, reply}
	return <-reply
}

// The machine owner joins execution before releasing its vCPU threads.
func (t *x86Thread) close() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.closed {
		t.closed = true
		close(t.jobs)
	}
	<-t.done
}
