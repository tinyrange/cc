package chipset

import (
	"sync"
	"testing"
	"time"
)

func TestPITChannel0GeneratesInterrupts(t *testing.T) {
	now := time.Unix(0, 0)
	var nowMu sync.Mutex
	nowFn := func() time.Time {
		nowMu.Lock()
		defer nowMu.Unlock()
		return now
	}
	advance := func(d time.Duration) {
		nowMu.Lock()
		now = now.Add(d)
		nowMu.Unlock()
	}

	var callsMu sync.Mutex
	var calls []struct {
		line  uint8
		level bool
	}
	sink := IRQLineFunc(func(line uint8, level bool) {
		callsMu.Lock()
		calls = append(calls, struct {
			line  uint8
			level bool
		}{line: line, level: level})
		callsMu.Unlock()
	})

	factory := &manualTimerFactory{}
	pit := NewPIT(sink,
		WithPITClock(nowFn),
		WithPITTimerFactory(factory.Factory),
		WithPITTick(1*time.Millisecond),
	)

	if err := pit.Init(nil); err != nil {
		t.Fatalf("init pit: %v", err)
	}

	if err := pit.WriteIOPort(pitControlPort, []byte{0x36}); err != nil {
		t.Fatalf("write control: %v", err)
	}
	if err := pit.WriteIOPort(pitChannel0Port, []byte{0x04}); err != nil {
		t.Fatalf("write low byte: %v", err)
	}
	if err := pit.WriteIOPort(pitChannel0Port, []byte{0x00}); err != nil {
		t.Fatalf("write high byte: %v", err)
	}

	if len(factory.timers) != 1 {
		t.Fatalf("expected one timer, got %d", len(factory.timers))
	}

	initial := readPitCounter(t, pit)
	if initial != 4 {
		t.Fatalf("expected counter 4, got %d", initial)
	}

	advance(factory.timers[0].period)
	factory.timers[0].Fire()

	callsMu.Lock()
	if len(calls) < 2 {
		t.Fatalf("expected irq pulse, got %d calls", len(calls))
	}
	if calls[0].line != 0 || !calls[0].level {
		t.Fatalf("expected level high on irq0, got %+v", calls[0])
	}
	if calls[1].line != 0 || calls[1].level {
		t.Fatalf("expected level low on irq0, got %+v", calls[1])
	}
	callsMu.Unlock()

	advance(2 * time.Millisecond)
	next := readPitCounter(t, pit)
	if next >= initial {
		t.Fatalf("expected counter to decrease, had %d then %d", initial, next)
	}
}

func readPitCounter(t *testing.T, pit *PIT) uint16 {
	t.Helper()
	buf := []byte{0}
	if err := pit.ReadIOPort(pitChannel0Port, buf); err != nil {
		t.Fatalf("read low: %v", err)
	}
	low := buf[0]
	if err := pit.ReadIOPort(pitChannel0Port, buf); err != nil {
		t.Fatalf("read high: %v", err)
	}
	high := buf[0]
	return uint16(high)<<8 | uint16(low)
}

type manualTimer struct {
	period  time.Duration
	cb      func()
	stopped bool
}

func (m *manualTimer) Stop() {
	m.stopped = true
}

func (m *manualTimer) Fire() {
	if m.stopped || m.cb == nil {
		return
	}
	m.cb()
}

type manualTimerFactory struct {
	timers []*manualTimer
}

func (m *manualTimerFactory) Factory(period time.Duration, cb func()) timerHandle {
	timer := &manualTimer{period: period, cb: cb}
	m.timers = append(m.timers, timer)
	return timer
}
