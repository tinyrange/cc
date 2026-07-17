package virtio

import (
	"encoding/binary"
	"testing"
)

func TestVirtioFSKickPollingRequiresExplicitOptIn(t *testing.T) {
	t.Setenv("CCX3_VIRTIOFS_KICK_POLL", "")
	if resolveVirtioFSKickPoll() {
		t.Fatal("virtio-fs kick polling enabled without explicit opt-in")
	}

	t.Setenv("CCX3_VIRTIOFS_KICK_POLL", "true")
	if !resolveVirtioFSKickPoll() {
		t.Fatal("virtio-fs kick polling not enabled by explicit opt-in")
	}

	t.Setenv("CCX3_VIRTIOFS_KICK_POLL", "invalid")
	if resolveVirtioFSKickPoll() {
		t.Fatal("invalid virtio-fs kick polling setting enabled polling")
	}
}

func TestVirtioFSCompletionUsesCurrentInterruptPreference(t *testing.T) {
	mem := make(testGuestMemory, 0x2000)
	dev := NewFS(0, 0x1000, 11, "root", nil)
	dev.Attach(mem, &testIRQ{})
	q := &dev.queues[fsQueueRequest]
	q.size = 8
	q.ready = true
	q.availAddr = 0x1000
	q.usedIdx = 2

	// A request may be harvested while the driver is polling and suppressing
	// interrupts, then complete after the driver has gone to sleep.
	binary.LittleEndian.PutUint16(mem[q.availAddr:], fsAvailNoInterrupt)
	interrupt, err := dev.shouldInterruptCompletionLocked(q, 1)
	if err != nil {
		t.Fatal(err)
	}
	if interrupt {
		t.Fatal("completion ignored the driver's current interrupt suppression")
	}
	binary.LittleEndian.PutUint16(mem[q.availAddr:], 0)
	interrupt, err = dev.shouldInterruptCompletionLocked(q, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !interrupt {
		t.Fatal("completion retained a stale interrupt-suppressed decision")
	}
}

func TestVirtioFSPokeRaisesVringIRQ(t *testing.T) {
	irq := &recordingIRQ{}
	fs := &FS{IRQ: 18, irq: irq}
	if err := fs.Poke(); err != nil {
		t.Fatalf("Poke: %v", err)
	}
	if len(irq.levels) != 1 || !irq.levels[0] {
		t.Fatalf("IRQ levels = %v, want [true]", irq.levels)
	}
	if !fs.irqHigh || fs.interruptStatus&fsInterruptVring == 0 {
		t.Fatalf("vring IRQ was not raised: high=%t status=%#x", fs.irqHigh, fs.interruptStatus)
	}
}

func TestVirtioFSPollReportsUnsupportedWithoutNotifications(t *testing.T) {
	const unique = 42
	req := make([]byte, fuseInHeaderSize+32)
	binary.LittleEndian.PutUint32(req[4:8], fusePoll)
	binary.LittleEndian.PutUint64(req[8:16], unique)

	reply, err := (&FS{}).dispatchFUSE(req)
	if err != nil {
		t.Fatal(err)
	}
	if reply.unique != unique || reply.errno != -linuxENOSYS || len(reply.extra) != 0 {
		t.Fatalf("POLL reply = unique %d errno %d extra %x", reply.unique, reply.errno, reply.extra)
	}
}
