package nvme

import (
	"encoding/binary"
	"testing"
	"time"
)

type parallelDisk struct {
	*testDisk
	entered chan int64
	gates   map[int64]chan struct{}
}

func (d *parallelDisk) ConcurrentReads() bool { return true }
func (d *parallelDisk) ReadAt(p []byte, off int64) (int, error) {
	d.entered <- off
	if gate := d.gates[off]; gate != nil {
		<-gate
	}
	return d.testDisk.ReadAt(p, off)
}
func parallelFixture(t *testing.T, offsets ...int64) (*Controller, *parallelDisk, boundedNVMeMemory) {
	t.Helper()
	d := &parallelDisk{testDisk: newTestDisk(64 << 20), entered: make(chan int64, 16), gates: make(map[int64]chan struct{})}
	for _, off := range offsets {
		d.gates[off] = make(chan struct{})
	}
	m := boundedNVMeMemory{newTestMemory(4 << 20)}
	c := NewController(d)
	c.Attach(m, &testIRQ{})
	c.EnableAsyncIO(nil)
	c.cc = 1
	c.queues[1] = &queue{id: 1, size: 8, sqAddr: 0x1000, cqAddr: 0x2000, cqID: 1, cqPhase: true}
	t.Cleanup(func() {
		for _, ch := range d.gates {
			select {
			case <-ch:
			default:
				close(ch)
			}
		}
		c.CloseIO()
	})
	return c, d, m
}
func enteredReads(t *testing.T, d *parallelDisk, n int) {
	t.Helper()
	for range n {
		select {
		case <-d.entered:
		case <-time.After(2 * time.Second):
			t.Fatal("parallel reads did not dispatch")
		}
	}
}
func TestParallelNVMeOutOfOrderReadAndBarrier(t *testing.T) {
	c, d, m := parallelFixture(t, 0, 512)
	d.data[0] = 11
	d.data[512] = 22
	submitAsync(t, c, m, 0, command{opcode: ioRead, nsid: 1, cid: 1, prp1: 0x4000})
	submitAsync(t, c, m, 1, command{opcode: ioRead, nsid: 1, cid: 2, prp1: 0x5000, cdw10: 1})
	enteredReads(t, d, 2)
	// A write followed by a read must wait for BOTH earlier reads.
	m.data[0x6000] = 33
	submitAsync(t, c, m, 2, command{opcode: ioWrite, nsid: 1, cid: 3, prp1: 0x6000})
	submitAsync(t, c, m, 3, command{opcode: ioFlush, nsid: 1, cid: 4})
	submitAsync(t, c, m, 4, command{opcode: ioRead, nsid: 1, cid: 5, prp1: 0x7000})
	close(d.gates[512])
	readyNVMe(t, c.IOReady())
	ownerReturns(t, func() error { return c.PollIO(false) })
	if m.data[0x5000] != 22 || m.data[0x4000] != 0 || c.queues[1].sqHead != 2 {
		t.Fatal("blocked read prevented unrelated completion, or write overtook read")
	}
	if binary.LittleEndian.Uint16(m.data[0x200c:]) != 2 || binary.LittleEndian.Uint16(m.data[0x2008:]) != 2 {
		t.Fatal("out-of-order completion CID/SQHD")
	}
	close(d.gates[0])
	if err := c.PollIO(true); err != nil {
		t.Fatal(err)
	}
	if m.data[0x4000] != 11 || m.data[0x7000] != 33 || c.queues[1].cqPending != 5 {
		t.Fatal("read/write/flush ordering lost")
	}
	if binary.LittleEndian.Uint16(m.data[0x2018:]) < 2 {
		t.Fatal("SQHD regressed")
	}
}
func TestParallelNVMeSharedCQReservationsAndWorkerBound(t *testing.T) {
	for _, slots := range []uint16{2, 8} {
		t.Run(string(rune('0'+slots)), func(t *testing.T) {
			c, d, m := parallelFixture(t, 0, 512, 1024, 1536, 2048)
			cq := &queue{id: 2, size: slots, cqAddr: 0x2000, cqPhase: true}
			c.queues[2] = cq
			c.queues[1].cqID = 2
			// A second SQ shares CQ2; reservations must span both queues.
			c.queues[3] = &queue{id: 3, size: 8, sqAddr: 0x3000, cqID: 2}
			for i := 0; i < 5; i++ {
				cmd := command{opcode: ioRead, nsid: 1, cid: uint16(i + 1), prp1: uint64(0x4000 + i*0x1000), cdw10: uint32(i)}
				if i%2 == 0 {
					writeIOCommand(m.testMemory, 0x1000, i/2, cmd)
					mustWriteMMIO(t, c, doorbellBase+8, uint64(i/2+1))
				} else {
					writeIOCommand(m.testMemory, 0x3000, i/2, cmd)
					mustWriteMMIO(t, c, doorbellBase+24, uint64(i/2+1))
				}
			}
			limit := min(int(slots), 4)
			enteredReads(t, d, limit)
			if len(c.async.active) != limit {
				t.Fatal("worker/CQ bound violated")
			}
			for _, ch := range d.gates {
				close(ch)
			}
			if err := c.PollIO(true); err != nil {
				t.Fatal(err)
			}
			if slots == 2 && (cq.cqPending != 2 || c.IOReady() != nil) {
				t.Fatal("CQ overrun")
			}
			if slots == 8 && cq.cqPending != 5 {
				t.Fatal("queued read lost")
			}
			if slots == 2 {
				mustWriteMMIO(t, c, doorbellBase+20, 1)
				if err := c.PollIO(true); err != nil {
					t.Fatal(err)
				}
				if cq.cqPending != 2 {
					t.Fatal("CQ acknowledgement did not restart")
				}
			}
		})
	}
}
func TestParallelNVMeResetJoinsEveryOldRead(t *testing.T) {
	c, d, m := parallelFixture(t, 0, 512)
	submitAsync(t, c, m, 0, command{opcode: ioRead, nsid: 1, prp1: 0x4000})
	submitAsync(t, c, m, 1, command{opcode: ioRead, nsid: 1, prp1: 0x5000, cdw10: 1})
	enteredReads(t, d, 2)
	mustWriteMMIO(t, c, regCC, 0)
	mustWriteMMIO(t, c, regAQA, 7|7<<16)
	mustWriteMMIO(t, c, regASQ, 0x9000)
	mustWriteMMIO(t, c, regACQ, 0xa000)
	mustWriteMMIO(t, c, regCC, 1)
	c.queues[1] = &queue{id: 1, size: 8, sqAddr: 0x1000, cqAddr: 0x2000, cqID: 1, cqPhase: true}
	m.data[0x6000] = 44
	submitAsync(t, c, m, 0, command{opcode: ioWrite, nsid: 1, cid: 7, prp1: 0x6000})
	close(d.gates[512])
	readyNVMe(t, c.IOReady())
	if err := c.PollIO(false); err != nil {
		t.Fatal(err)
	}
	if c.queues[1].sqHead != 0 || m.data[0x5000] != 0 {
		t.Fatal("new generation overtook old read or stale DMA")
	}
	close(d.gates[0])
	if err := c.PollIO(true); err != nil {
		t.Fatal(err)
	}
	if m.data[0x4000] != 0 || d.data[0] != 44 || c.queues[1].cqPending != 1 {
		t.Fatal("reset isolation lost")
	}
}

func TestParallelNVMeTransferBudgetAndShutdown(t *testing.T) {
	c, d, m := parallelFixture(t, 0, 16<<20, 32<<20)
	m.testMemory.data = make([]byte, 40<<20)
	// Two maximum-budget halves with disjoint DMA and PRP lists.
	for i := 0; i < 2; i++ {
		list := 0x3000 + i*0x8000
		start := 0x20000 + i*(16<<20)
		// Linked PRP pages: 511 data entries and one next-page entry per page.
		remaining := 4095
		page := list
		entry := 0
		address := start + 4096
		for remaining > 0 {
			if entry == 511 && remaining > 1 {
				binary.LittleEndian.PutUint64(m.data[page+entry*8:], uint64(page+4096))
				page += 4096
				entry = 0
				continue
			}
			binary.LittleEndian.PutUint64(m.data[page+entry*8:], uint64(address))
			address += 4096
			entry++
			remaining--
		}
		submitAsync(t, c, m, i, command{opcode: ioRead, nsid: 1, cid: uint16(i + 1), prp1: uint64(start), prp2: uint64(list), cdw10: uint32(i * (16 << 20) / 512), cdw12: 32767})
	}
	enteredReads(t, d, 2)
	submitAsync(t, c, m, 2, command{opcode: ioRead, nsid: 1, cid: 3, prp1: 0x10000, cdw10: (32 << 20) / 512})
	if c.queues[1].sqHead != 2 {
		t.Fatal("32 MiB budget exceeded")
	}
	mustWriteMMIO(t, c, regCC, 1|1<<14)
	if c.csts>>2&3 != 1 {
		t.Fatal("shutdown completed with outstanding reads")
	}
	close(d.gates[0])
	readyNVMe(t, c.IOReady())
	if err := c.PollIO(false); err != nil {
		t.Fatal(err)
	}
	enteredReads(t, d, 1)
	if c.queues[1].sqHead != 3 || c.csts>>2&3 != 1 {
		t.Fatal("freed budget did not restart read, or premature shutdown")
	}
	close(d.gates[16<<20])
	close(d.gates[32<<20])
	if err := c.PollIO(true); err != nil {
		t.Fatal(err)
	}
	if c.csts>>2&3 != 2 || c.queues[1].cqPending != 3 {
		t.Fatal("shutdown did not join all reads")
	}
}
func TestParallelNVMeCloseJoinsEveryWake(t *testing.T) {
	c, d, m := parallelFixture(t, 0, 512)
	wakeEntered := make(chan struct{}, 2)
	wakeRelease := make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-wakeRelease:
		default:
			close(wakeRelease)
		}
	})
	c.async.wake = func() { wakeEntered <- struct{}{}; <-wakeRelease }
	submitAsync(t, c, m, 0, command{opcode: ioRead, nsid: 1, prp1: 0x4000})
	submitAsync(t, c, m, 1, command{opcode: ioRead, nsid: 1, prp1: 0x5000, cdw10: 1})
	enteredReads(t, d, 2)
	closed := make(chan struct{})
	go func() { c.CloseIO(); close(closed) }()
	close(d.gates[0])
	readyNVMe(t, wakeEntered)
	select {
	case <-closed:
		t.Fatal("close returned before all reads/wakes")
	default:
	}
	close(d.gates[512])
	readyNVMe(t, wakeEntered)
	select {
	case <-closed:
		t.Fatal("close returned while wake target active")
	default:
	}
	close(wakeRelease)
	readyNVMe(t, closed)
	if m.data[0x4000] != 0 || m.data[0x5000] != 0 || c.IOReady() != nil {
		t.Fatal("close delivered DMA")
	}
}
