package nvme

import (
	"errors"
	"fmt"
	"io"
	"sync"
)

// Async I/O permits up to four reads for backends explicitly supporting it.
// Writes and flushes remain global barriers. Active buffers total at most
// 32 MiB. Queue, DMA and IRQ ownership stays with the caller.
// The worker never holds the controller lock or accesses guest memory.
type asyncIO struct {
	wake       func()
	active     []*backingIO
	notify     chan struct{}
	notifyMu   sync.Mutex
	notified   bool
	limit      int
	buffer     []byte
	generation uint64
	closed     bool
	nextQueue  uint16
}

type dmaSpan struct {
	address uint64
	size    int
}
type backingIO struct {
	ready, done chan struct{}
	sq, cq      *queue
	cmd         command
	generation  uint64
	spans       []dmaSpan
	data        []byte
	offset      int64
	status      uint16
}

// EnableAsyncIO opts into worker-backed I/O. Call before enabling the controller.
// wake must only wake native execution, not mutate the controller or guest RAM.
// The owner must service IOReady with PollIO, and join workers with CloseIO before
// releasing the backend, memory, or wake target. Synchronous callers are unchanged.
func (c *Controller) EnableAsyncIO(wake func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.async == nil {
		limit := 1
		if b, ok := c.backend.(interface{ ConcurrentReads() bool }); ok && b.ConcurrentReads() {
			limit = 4
		}
		c.async = &asyncIO{wake: wake, notify: make(chan struct{}), limit: limit}
	}
}

// IOReady is nil when idle; closure means backing I/O is ready for delivery.
func (c *Controller) IOReady() <-chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.async == nil || len(c.async.active) == 0 {
		return nil
	}
	c.async.notifyMu.Lock()
	defer c.async.notifyMu.Unlock()
	return c.async.notify
}

// CloseIO prevents further dispatch, joins backing I/O and its wake, and discards
// any undelivered DMA/completion. An in-flight disk write cannot be canceled.
func (c *Controller) CloseIO() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.async == nil {
		return
	}
	c.async.closed = true
	c.async.generation++
	for _, j := range c.async.active {
		<-j.done
	}
	c.async.active = nil
	c.async.buffer = nil
}

// PollIO delivers completed data and CQ/IRQ changes on the caller's device loop.
// wait drains submitted work (for pause/stop); false never waits for backing I/O.
func (c *Controller) PollIO(wait bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	a := c.async
	if a == nil || a.closed {
		return nil
	}
	for {
		// Consume the notification before scanning: a concurrent completion
		// either appears in this scan or closes the next wait channel.
		a.notifyMu.Lock()
		if a.notified {
			a.notify = make(chan struct{})
			a.notified = false
		}
		notify := a.notify
		a.notifyMu.Unlock()
		index := -1
		for i, j := range a.active {
			select {
			case <-j.ready:
				index = i
			default:
			}
			if index >= 0 {
				break
			}
		}
		if index < 0 {
			if err := c.scheduleIOLocked(); err != nil {
				return err
			}
			if !wait || len(a.active) == 0 {
				return nil
			}
			<-notify
			continue
		}
		j := a.active[index]
		<-j.done // Join wake before releasing its native target.
		copy(a.active[index:], a.active[index+1:])
		a.active[len(a.active)-1] = nil
		a.active = a.active[:len(a.active)-1]
		if cap(j.data) > cap(a.buffer) {
			a.buffer = j.data
		}

		if j.generation == a.generation && c.queues[j.sq.id] == j.sq && c.queues[j.cq.id] == j.cq {
			if j.status == 0 {
				if j.cmd.opcode == ioRead {
					off := 0
					for _, span := range j.spans {
						if err := c.mem.WriteIPA(span.address, j.data[off:off+span.size]); err != nil {
							return err
						}
						off += span.size
					}
					c.recordIOLocked("read", len(j.data))
				} else if j.cmd.opcode == ioWrite {
					c.recordIOLocked("write", len(j.data))
				}
			}
			if err := c.writeCompletionLocked(j.cq, j.sq.id, j.sq.sqHead, j.cmd.cid, 0, j.status); err != nil {
				return err
			}
			if err := c.updateIRQLocked(true); err != nil {
				return err
			}
		}
		if err := c.scheduleIOLocked(); err != nil {
			return err
		}
	}
}

func (c *Controller) scheduleIOLocked() error {
	a := c.async
	if a == nil || a.closed || len(a.active) >= a.limit || c.cc&1 == 0 {
		return nil
	}
	// A reset must join the old generation before dispatching new work.
	used := 0
	for _, j := range a.active {
		if j.generation != a.generation || j.cmd.opcode != ioRead {
			return nil
		}
		used += cap(j.data)
	}
	// Round-robin ready queues, counting undelivered jobs as CQ reservations.
	var chosen *queue
	pending := false
	for id, q := range c.queues {
		if id == adminQueueID || q.size == 0 || q.sqHead == q.sqTail {
			continue
		}
		pending = true
		cq := c.queues[q.cqID]
		if cq == nil {
			return fmt.Errorf("nvme completion queue %d missing", q.cqID)
		}
		reserved := int(cq.cqPending)
		for _, j := range a.active {
			if j.cq == cq {
				reserved++
			}
		}
		if reserved >= int(cq.size) {
			continue
		}
		if chosen == nil || uint16(id-a.nextQueue) < uint16(chosen.id-a.nextQueue) {
			chosen = q
		}
	}
	if chosen == nil {
		if !pending && len(a.active) == 0 && c.cc&(3<<14) != 0 {
			c.csts = (c.csts &^ (3 << 2)) | 2<<2
		}
		return nil
	}
	q := chosen
	raw := c.queueSubmissionEntry(q, q.sqHead)
	if raw == nil {
		var err error
		raw, err = c.mem.ReadIPA(q.sqAddr+uint64(q.sqHead)*64, 64)
		if err != nil {
			return err
		}
	}
	j := &backingIO{ready: make(chan struct{}), done: make(chan struct{}), sq: q,
		cq: c.queues[q.cqID], cmd: parseCommand(raw), generation: a.generation}
	cmd := j.cmd
	// Never let reads pass a consumed write/flush, or a queue's barrier head.
	if len(a.active) != 0 && cmd.opcode != ioRead {
		return nil
	}
	count := int((cmd.cdw12&0xffff)+1) * blockSize
	if cmd.opcode == ioRead && used+count > 32<<20 {
		return nil
	}
	if c.backend == nil || cmd.nsid != 1 {
		j.status = 1
	} else if cmd.opcode != ioFlush {
		if cmd.opcode != ioRead && cmd.opcode != ioWrite {
			j.status = 1
		} else {
			count := int((cmd.cdw12&0xffff)+1) * blockSize
			lba := uint64(cmd.cdw10) | uint64(cmd.cdw11)<<32
			size := c.backend.Size()
			if size < int64(count) || lba > uint64(size-int64(count))/blockSize {
				j.status = 1
			} else {
				j.offset = int64(lba * blockSize)
				var err error
				j.spans, err = c.snapshotPRPLocked(cmd.prp1, cmd.prp2, count)
				if err != nil {
					return err
				}
				if cap(a.buffer) < count || used+cap(a.buffer) > 32<<20 {
					a.buffer = make([]byte, count)
				}
				j.data = a.buffer[:count]
				a.buffer = nil
				if cmd.opcode == ioWrite {
					off := 0
					for _, span := range j.spans {
						if err := c.transferPage(span.address, j.data[off:off+span.size], false); err != nil {
							return err
						}
						off += span.size
					}
				}
			}
		}
	}
	q.sqHead = (q.sqHead + 1) % q.size
	a.nextQueue = q.id + 1
	a.active = append(a.active, j)
	backend, wake := c.backend, a.wake
	go func() {
		defer close(j.done)
		if j.status == 0 {
			switch cmd.opcode {
			case ioRead:
				n, err := backend.ReadAt(j.data, j.offset)
				if n < 0 || n > len(j.data) || (err != nil && !errors.Is(err, io.EOF)) {
					j.status = 1
				} else {
					clear(j.data[n:])
				}
			case ioWrite:
				n, err := backend.WriteAt(j.data, j.offset)
				if err != nil || n != len(j.data) {
					j.status = 1
				}
			case ioFlush:
				// BlockBackend has no flush method: like the synchronous controller, flush
				// is an ordering barrier after all earlier backing writes have returned.
			}
		}
		close(j.ready)
		a.notifyMu.Lock()
		if !a.notified {
			close(a.notify)
			a.notified = true
		}
		a.notifyMu.Unlock()
		if wake != nil {
			wake()
		}
	}()
	return c.scheduleIOLocked()
}

func (c *Controller) snapshotPRPLocked(prp1, prp2 uint64, count int) ([]dmaSpan, error) {
	if prp1 == 0 {
		return nil, fmt.Errorf("nvme command missing PRP1")
	}
	page := c.pageSz
	if page == 0 {
		page = 4096
	}
	spans := make([]dmaSpan, 0, 1+count/int(page))
	add := func(addr uint64, size int) error {
		if addr == 0 || addr+uint64(size) < addr {
			return fmt.Errorf("invalid nvme DMA address %#x", addr)
		}
		// Validate mappings before dispatch. No borrowed RAM is given to the worker.
		if mem, ok := c.mem.(guestMemorySlicer); ok {
			if _, err := mem.SliceIPA(addr, size); err != nil {
				return err
			}
		} else if _, err := c.mem.ReadIPA(addr, size); err != nil {
			return err
		}
		spans = append(spans, dmaSpan{addr, size})
		return nil
	}
	first := min(count, int(page-prp1%page))
	if err := add(prp1, first); err != nil {
		return nil, err
	}
	remaining := count - first
	if remaining > 0 {
		if remaining <= int(page) {
			if prp2%page != 0 {
				return nil, fmt.Errorf("unaligned nvme PRP2")
			}
			if err := add(prp2, remaining); err != nil {
				return nil, err
			}
		} else {
			if prp2%8 != 0 {
				return nil, fmt.Errorf("unaligned nvme PRP list")
			}
			if err := c.walkPRPListLocked(prp2, remaining, func(addr uint64, size int) error {
				if addr%page != 0 {
					return fmt.Errorf("unaligned nvme PRP page")
				}
				return add(addr, size)
			}); err != nil {
				return nil, err
			}
		}
	}
	return spans, nil
}
