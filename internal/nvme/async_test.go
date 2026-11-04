package nvme

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"
)

type heldNVMeDisk struct {
	*testDisk
	entered chan byte
	release chan struct{}
	once    sync.Once
	unblock sync.Once
	short   bool
	failure error
}

func (d *heldNVMeDisk) gate(op byte) {
	d.entered <- op
	d.once.Do(func() { <-d.release })
}
func (d *heldNVMeDisk) ReadAt(p []byte, off int64) (int, error) {
	d.gate(ioRead)
	if d.failure != nil {
		return 0, d.failure
	}
	if d.short {
		copy(p, "abc")
		return 3, io.EOF
	}
	return d.testDisk.ReadAt(p, off)
}
func (d *heldNVMeDisk) WriteAt(p []byte, off int64) (int, error) {
	d.gate(ioWrite)
	if d.failure != nil {
		return 0, d.failure
	}
	if d.short {
		return len(p) - 1, nil
	}
	return d.testDisk.WriteAt(p, off)
}
func (d *heldNVMeDisk) allow() { d.unblock.Do(func() { close(d.release) }) }

type boundedNVMeMemory struct{ *testMemory }

func (m boundedNVMeMemory) SliceIPA(addr uint64, size int) ([]byte, error) {
	if size < 0 || addr > uint64(len(m.data)) || uint64(size) > uint64(len(m.data))-addr {
		return nil, fmt.Errorf("DMA outside RAM")
	}
	return m.data[addr : addr+uint64(size)], nil
}

func asyncFixture(t *testing.T) (*Controller, *heldNVMeDisk, boundedNVMeMemory, *testIRQ) {
	t.Helper()
	d := &heldNVMeDisk{testDisk: newTestDisk(4 << 20), entered: make(chan byte, 16), release: make(chan struct{})}
	m := boundedNVMeMemory{newTestMemory(4 << 20)}
	irq := &testIRQ{}
	c := NewController(d)
	c.Attach(m, irq)
	c.EnableAsyncIO(nil)
	c.cc = 1
	c.queues[1] = &queue{id: 1, size: 8, sqAddr: 0x1000, cqAddr: 0x2000, cqID: 1, cqPhase: true, interrupts: true}
	t.Cleanup(func() { d.allow(); c.CloseIO() })
	return c, d, m, irq
}
func readyNVMe(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	if ch == nil {
		t.Fatal("no backing job")
	}
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("backing result not ready")
	}
}
func ownerReturns(t *testing.T, fn func() error) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- fn() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("device owner blocked on backing storage")
	}
}
func submitAsync(t *testing.T, c *Controller, m boundedNVMeMemory, slot int, cmd command) {
	t.Helper()
	writeIOCommand(m.testMemory, 0x1000, slot, cmd)
	ownerReturns(t, func() error { return c.WriteMMIO(doorbellBase+8, 4, uint64((slot+1)%8)) })
}

func TestAsyncNVMeOwnerProgressAndPRPSnapshot(t *testing.T) {
	c, d, m, irq := asyncFixture(t)
	copy(d.data, bytes.Repeat([]byte{0x5a}, 3*4096))
	binary.LittleEndian.PutUint64(m.data[0x3000:], 0x6000)
	binary.LittleEndian.PutUint64(m.data[0x3008:], 0x8000)
	submitAsync(t, c, m, 0, command{opcode: ioRead, nsid: 1, cid: 9, prp1: 0x4000, prp2: 0x3000, cdw12: 23})
	select {
	case <-d.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("worker not started")
	}
	// MMIO and nonblocking device polling must progress while ReadAt is held.
	ownerReturns(t, func() error { _, err := c.ReadMMIO(regCSTS, 4); return err })
	ownerReturns(t, func() error { return c.PollIO(false) })
	// A malicious changed PRP list after dispatch must not redirect read DMA.
	binary.LittleEndian.PutUint64(m.data[0x3000:], 0xa000)
	d.allow()
	readyNVMe(t, c.IOReady())
	if irq.level || !bytes.Equal(m.data[0x4000:0x5000], make([]byte, 4096)) || c.queues[1].cqPending != 0 {
		t.Fatal("worker mutated guest RAM, completion or IRQ")
	}
	if err := c.PollIO(false); err != nil {
		t.Fatal(err)
	}
	for _, addr := range []int{0x4000, 0x6000, 0x8000} {
		if !bytes.Equal(m.data[addr:addr+4096], bytes.Repeat([]byte{0x5a}, 4096)) {
			t.Fatalf("missing DMA at %#x", addr)
		}
	}
	if m.data[0xa000] != 0 || !irq.level || binary.LittleEndian.Uint16(m.data[0x200c:]) != 9 {
		t.Fatal("bad completion/PRP ownership")
	}
}

func TestAsyncNVMeWriteSnapshotFlushOrderAndCQBackpressure(t *testing.T) {
	c, d, m, _ := asyncFixture(t)
	q := c.queues[1]
	q.size = 2
	// Only two CQ slots: third SQ command will wait for an acknowledgement.
	cq := &queue{id: 2, size: 2, cqAddr: 0x2000, cqPhase: true, interrupts: true}
	c.queues[2] = cq
	q.cqID = 2
	q.size = 8
	copy(m.data[0x4000:], bytes.Repeat([]byte{0x37}, 512))
	submitAsync(t, c, m, 0, command{opcode: ioWrite, nsid: 1, cid: 1, prp1: 0x4000})
	<-d.entered
	clear(m.data[0x4000:0x4200])
	submitAsync(t, c, m, 1, command{opcode: ioFlush, nsid: 1, cid: 2})
	submitAsync(t, c, m, 2, command{opcode: ioRead, nsid: 1, cid: 3, prp1: 0x5000})
	mustWriteMMIO(t, c, regCC, 1|1<<14)
	if c.csts>>2&3 != 1 {
		t.Fatal("shutdown completed with backing write still blocked")
	}
	select {
	case <-d.entered:
		t.Fatal("later I/O overtook blocked write")
	default:
	}
	d.allow()
	if err := c.PollIO(true); err != nil {
		t.Fatal(err)
	}
	if d.data[0] != 0x37 || cq.cqPending != 2 || q.sqHead != 2 || c.IOReady() != nil || c.csts>>2&3 != 1 {
		t.Fatal("snapshot, flush ordering, CQ backpressure, or shutdown busy lost")
	}
	// CQ2 head, not SQ1 head. Frees space without a new SQ doorbell.
	mustWriteMMIO(t, c, doorbellBase+2*8+4, 1)
	if err := c.PollIO(true); err != nil {
		t.Fatal(err)
	}
	if m.data[0x5000] != 0x37 || q.sqHead != 3 || cq.cqPending != 2 || c.csts>>2&3 != 2 {
		t.Fatal("ack did not restart pending I/O or finish shutdown")
	}
}

func TestAsyncNVMeResetDropsDMAAndSerializesNewGeneration(t *testing.T) {
	c, d, m, irq := asyncFixture(t)
	d.data[0] = 0x64
	submitAsync(t, c, m, 0, command{opcode: ioRead, nsid: 1, cid: 1, prp1: 0x4000})
	<-d.entered
	ownerReturns(t, func() error { return c.WriteMMIO(regCC, 4, 0) })
	// Re-enable with a new queue while the old ReadAt is blocked.
	mustWriteMMIO(t, c, regAQA, 7|7<<16)
	mustWriteMMIO(t, c, regASQ, 0x9000)
	mustWriteMMIO(t, c, regACQ, 0xa000)
	mustWriteMMIO(t, c, regCC, 1)
	c.queues[1] = &queue{id: 1, size: 8, sqAddr: 0x1000, cqAddr: 0x2000, cqID: 1, cqPhase: true, interrupts: true}
	copy(m.data[0x5000:], bytes.Repeat([]byte{0x77}, 512))
	submitAsync(t, c, m, 0, command{opcode: ioWrite, nsid: 1, cid: 2, prp1: 0x5000})
	select {
	case <-d.entered:
		t.Fatal("new generation ran before old backing I/O joined")
	default:
	}
	d.allow()
	if err := c.PollIO(true); err != nil {
		t.Fatal(err)
	}
	if m.data[0x4000] != 0 || binary.LittleEndian.Uint16(m.data[0x200c:]) != 2 || d.data[0] != 0x77 || !irq.level {
		t.Fatal("reset delivered stale DMA/completion or lost new generation")
	}
}

func TestAsyncNVMeCloseJoinsWakeWithoutDMA(t *testing.T) {
	c, d, m, irq := asyncFixture(t)
	wakeEntered, wakeRelease := make(chan struct{}), make(chan struct{})
	c.async.wake = func() { close(wakeEntered); <-wakeRelease }
	t.Cleanup(func() {
		select {
		case <-wakeRelease:
		default:
			close(wakeRelease)
		}
	})
	submitAsync(t, c, m, 0, command{opcode: ioRead, nsid: 1, prp1: 0x4000})
	d.allow()
	readyNVMe(t, wakeEntered)
	closed := make(chan struct{})
	go func() { c.CloseIO(); close(closed) }()
	select {
	case <-closed:
		t.Fatal("close did not join wake")
	case <-time.After(20 * time.Millisecond):
	}
	close(wakeRelease)
	readyNVMe(t, closed)
	if irq.level || m.data[0x4000] != 0 || c.IOReady() != nil {
		t.Fatal("close delivered DMA or retained job")
	}
}

func TestAsyncNVMeShortAndFailedBackingIO(t *testing.T) {
	for _, tc := range []struct {
		name   string
		op     byte
		short  bool
		err    error
		status uint16
	}{
		{"short read zero fill", ioRead, true, nil, 0},
		{"short write failure", ioWrite, true, nil, 1},
		{"read failure", ioRead, false, errors.New("read failed"), 1},
		{"write failure", ioWrite, false, errors.New("write failed"), 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, d, m, _ := asyncFixture(t)
			d.short, d.failure = tc.short, tc.err
			copy(m.data[0x4000:], bytes.Repeat([]byte{0xff}, 512))
			submitAsync(t, c, m, 0, command{opcode: tc.op, nsid: 1, prp1: 0x4000})
			d.allow()
			if err := c.PollIO(true); err != nil {
				t.Fatal(err)
			}
			if got := binary.LittleEndian.Uint16(m.data[0x200e:]) >> 1; got != tc.status {
				t.Fatalf("status=%d want%d", got, tc.status)
			}
			if tc.op == ioRead && tc.status == 0 && (!bytes.Equal(m.data[0x4000:0x4003], []byte("abc")) || !bytes.Equal(m.data[0x4003:0x4200], make([]byte, 509))) {
				t.Fatal("short read did not zero fill")
			}
			if tc.op == ioRead && tc.status != 0 && m.data[0x4000] != 0xff {
				t.Fatal("failed read overwrote guest RAM")
			}
		})
	}
}

func TestAsyncNVMeInvalidPRPAndLBA(t *testing.T) {
	t.Run("invalid DMA", func(t *testing.T) {
		c, d, m, _ := asyncFixture(t)
		writeIOCommand(m.testMemory, 0x1000, 0, command{opcode: ioRead, nsid: 1, prp1: uint64(len(m.data)) - 16})
		if err := c.WriteMMIO(doorbellBase+8, 4, 1); err == nil {
			t.Fatal("invalid DMA accepted")
		}
		if c.IOReady() != nil {
			t.Fatal("invalid DMA started worker")
		}
		select {
		case <-d.entered:
			t.Fatal("invalid DMA reached disk")
		default:
		}
	})
	t.Run("overflow LBA", func(t *testing.T) {
		c, d, m, _ := asyncFixture(t)
		submitAsync(t, c, m, 0, command{opcode: ioRead, nsid: 1, prp1: 0x4000, cdw10: ^uint32(0), cdw11: ^uint32(0)})
		if err := c.PollIO(true); err != nil {
			t.Fatal(err)
		}
		if binary.LittleEndian.Uint16(m.data[0x200e:])>>1 != 1 {
			t.Fatal("overflow LBA succeeded")
		}
		select {
		case <-d.entered:
			t.Fatal("overflow LBA reached disk")
		default:
		}
	})
}
