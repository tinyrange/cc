package nvme

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"
)

// GuestMemory may perform a multi-byte write in any internal order. Model a
// transport that copies high addresses first while the guest polls the phase.
type observingCompletionMemory struct {
	*testMemory
	address   uint64
	phase     uint16
	want      [16]byte
	premature bool
	observed  bool
}

func (m *observingCompletionMemory) WriteIPA(addr uint64, data []byte) error {
	for i := len(data) - 1; i >= 0; i-- {
		m.data[int(addr)+i] = data[i]
		entry := m.data[m.address : m.address+16]
		if binary.LittleEndian.Uint16(entry[14:])&1 == m.phase {
			m.observed = true
			for j := range m.want {
				if entry[j] != m.want[j] {
					m.premature = true
				}
			}
		}
	}
	return nil
}
func TestCompletionPhasePublishesOnlyAfterBody(t *testing.T) {
	mem := &observingCompletionMemory{testMemory: newTestMemory(0x4000), address: 0x1000}
	ctrl := NewController(newTestDisk(4096))
	ctrl.Attach(mem, &testIRQ{})
	q := &queue{id: 1, size: 1, cqAddr: mem.address, cqPhase: true}
	for sequence := uint16(0); sequence < 3; sequence++ {
		result := uint32(0x12340000) + uint32(sequence)
		cid := uint16(0x3456) + sequence
		var want [16]byte
		binary.LittleEndian.PutUint32(want[:], result)
		binary.LittleEndian.PutUint16(want[8:], 42)
		binary.LittleEndian.PutUint16(want[10:], 7)
		binary.LittleEndian.PutUint16(want[12:], cid)
		status := uint16(0x180) + sequence
		mem.phase = 0
		if q.cqPhase {
			mem.phase = 1
		}
		binary.LittleEndian.PutUint16(want[14:], status<<1|mem.phase)
		copy(mem.want[:], want[:])
		mem.premature, mem.observed = false, false
		if err := ctrl.writeCompletionLocked(q, 7, 42, cid, result, status); err != nil {
			t.Fatal(err)
		}
		if mem.premature || !mem.observed {
			t.Fatalf("completion phase published before body: sequence=%d premature=%t observed=%t", sequence, mem.premature, mem.observed)
		}
		if got := binary.LittleEndian.Uint16(mem.data[mem.address+14:]); got != status<<1|mem.phase {
			t.Fatalf("wrong status: %#x", got)
		}
		q.advanceCompletionHeadLocked(0)
	}
}

func TestMappedCompletionReleasePublishesBodyAcrossPhaseWrap(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	mem := newTestMemory(0x4000)
	ctrl := NewController(newTestDisk(4096))
	ctrl.Attach(mem, &testIRQ{})
	entry := mem.data[0x1000:0x1010]
	q := &queue{id: 1, size: 1, cqAddr: 0x1000, cqMem: entry, cqPhase: true}
	ack := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		var first error
		for sequence := uint16(0); sequence < 1000; sequence++ {
			var last [4]byte
			phase := uint16(1 - sequence%2)
			for {
				binary.NativeEndian.PutUint32(last[:], atomic.LoadUint32((*uint32)(unsafe.Pointer(&entry[12]))))
				if binary.LittleEndian.Uint16(last[2:])&1 == phase {
					break
				}
				select {
				case <-ctx.Done():
					done <- ctx.Err()
					return
				default:
					runtime.Gosched()
				}
			}
			if first == nil && (binary.LittleEndian.Uint32(entry[:4]) != uint32(0x12340000)+uint32(sequence) || binary.LittleEndian.Uint16(entry[8:]) != sequence || binary.LittleEndian.Uint16(entry[10:]) != 7 || binary.LittleEndian.Uint16(last[:]) != 0x3456+sequence) {
				first = fmt.Errorf("guest observed new phase with stale body at %d", sequence)
			}
			select {
			case ack <- struct{}{}:
			case <-ctx.Done():
				done <- ctx.Err()
				return
			}
		}
		done <- first
	}()
	for sequence := uint16(0); sequence < 1000; sequence++ {
		if err := ctrl.writeCompletionLocked(q, 7, sequence, 0x3456+sequence, uint32(0x12340000)+uint32(sequence), 0); err != nil {
			t.Fatal(err)
		}
		select {
		case <-ack:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		q.advanceCompletionHeadLocked(0)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestMappedCompletionRejectsUnalignedHostPublication(t *testing.T) {
	mem := newTestMemory(0x4000)
	ctrl := NewController(newTestDisk(4096))
	ctrl.Attach(mem, &testIRQ{})
	var words [8]uint32
	buffer := unsafe.Slice((*byte)(unsafe.Pointer(&words[0])), 32)
	q := &queue{id: 1, size: 1, cqMem: buffer[1:17], cqPhase: true}
	if err := ctrl.writeCompletionLocked(q, 7, 0, 1, 1, 0); err == nil {
		t.Fatal("accepted unaligned mapped completion word")
	}
	if q.cqTail != 0 || q.cqPending != 0 || !bytes.Equal(buffer, make([]byte, 32)) {
		t.Fatal("failed publication changed queue ownership")
	}
}
