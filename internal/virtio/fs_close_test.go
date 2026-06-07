package virtio

import "testing"

func TestVirtioFSCloseStopsKickPollerAndDeassertsIRQ(t *testing.T) {
	t.Setenv("CCX3_VIRTIOFS_KICK_POLL", "1")

	fsdev := NewFS(0x1000, 0x1000, 44, "root", NewPassthroughFS(t.TempDir(), nil))
	mem := &testGuestMemory{data: make([]byte, 0x10000)}
	irq := &testIRQController{}
	fsdev.Attach(mem, irq)

	fsdev.mu.Lock()
	fsdev.interruptStatus = fsInterruptVring
	if err := fsdev.updateIRQLocked(); err != nil {
		fsdev.mu.Unlock()
		t.Fatalf("raise IRQ before close: %v", err)
	}
	fsdev.startKickPollerLocked()
	fsdev.mu.Unlock()

	if !irq.level {
		t.Fatal("IRQ was not raised before close")
	}
	if err := fsdev.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if irq.irq != 44 || irq.level {
		t.Fatalf("IRQ after Close() = (%d, %t), want (44, false)", irq.irq, irq.level)
	}

	fsdev.mu.Lock()
	defer fsdev.mu.Unlock()
	if fsdev.mem != nil {
		t.Fatal("Close() left guest memory attached")
	}
	if fsdev.irq != nil {
		t.Fatal("Close() left IRQ controller attached")
	}
	if fsdev.kickPollActive {
		t.Fatal("Close() left kick poller active")
	}
}
