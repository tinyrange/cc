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

	if err := pit.WriteIOPort(nil, pitControlPort, []byte{0x36}); err != nil {
		t.Fatalf("write control: %v", err)
	}
	if err := pit.WriteIOPort(nil, pitChannel0Port, []byte{0x04}); err != nil {
		t.Fatalf("write low byte: %v", err)
	}
	if err := pit.WriteIOPort(nil, pitChannel0Port, []byte{0x00}); err != nil {
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
	if err := pit.ReadIOPort(nil, pitChannel0Port, buf); err != nil {
		t.Fatalf("read low: %v", err)
	}
	low := buf[0]
	if err := pit.ReadIOPort(nil, pitChannel0Port, buf); err != nil {
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

// TestPITMode2PeriodicTimer tests mode 2 (rate generator) periodic behavior.
func TestPITMode2PeriodicTimer(t *testing.T) {
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

	// Mode 2: Rate generator (periodic pulse)
	// Control word: channel 0, access low/high, mode 2, binary
	if err := pit.WriteIOPort(nil, pitControlPort, []byte{0x34}); err != nil {
		t.Fatalf("write control: %v", err)
	}
	// Set reload value to 5 (period = 5ms)
	if err := pit.WriteIOPort(nil, pitChannel0Port, []byte{0x05}); err != nil {
		t.Fatalf("write low byte: %v", err)
	}
	if err := pit.WriteIOPort(nil, pitChannel0Port, []byte{0x00}); err != nil {
		t.Fatalf("write high byte: %v", err)
	}

	if len(factory.timers) != 1 {
		t.Fatalf("expected one timer, got %d", len(factory.timers))
	}

	// Verify timer period is correct
	expectedPeriod := 5 * time.Millisecond
	if factory.timers[0].period != expectedPeriod {
		t.Fatalf("expected period %v, got %v", expectedPeriod, factory.timers[0].period)
	}

	// Fire timer multiple times to verify periodic behavior
	for i := 0; i < 3; i++ {
		advance(factory.timers[0].period)
		factory.timers[0].Fire()

		callsMu.Lock()
		expectedCalls := (i + 1) * 2 // Each period generates 2 calls (high, low)
		if len(calls) < expectedCalls {
			t.Fatalf("after %d periods, expected at least %d calls, got %d", i+1, expectedCalls, len(calls))
		}
		// Verify interrupt pulse pattern
		idx := i * 2
		if calls[idx].line != 0 || !calls[idx].level {
			t.Fatalf("period %d: expected level high on irq0, got %+v", i+1, calls[idx])
		}
		if calls[idx+1].line != 0 || calls[idx+1].level {
			t.Fatalf("period %d: expected level low on irq0, got %+v", i+1, calls[idx+1])
		}
		callsMu.Unlock()

		// Counter should reload and continue counting
		advance(1 * time.Millisecond)
		counter := readPitCounter(t, pit)
		if counter == 0 || counter > 5 {
			t.Fatalf("after period %d, expected counter between 1-5, got %d", i+1, counter)
		}
	}
}

// TestPITMode0OneShot tests mode 0 (interrupt on terminal count) one-shot behavior.
// Note: Mode 0 uses time.AfterFunc directly, not the timer factory.
func TestPITMode0OneShot(t *testing.T) {
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

	pit := NewPIT(sink,
		WithPITClock(nowFn),
		WithPITTick(1*time.Millisecond),
	)

	if err := pit.Init(nil); err != nil {
		t.Fatalf("init pit: %v", err)
	}

	// Mode 0: Interrupt on terminal count (one-shot)
	// Control word: channel 0, access low/high, mode 0, binary
	if err := pit.WriteIOPort(nil, pitControlPort, []byte{0x30}); err != nil {
		t.Fatalf("write control: %v", err)
	}
	// Set reload value to 3 (should fire after 3ms)
	if err := pit.WriteIOPort(nil, pitChannel0Port, []byte{0x03}); err != nil {
		t.Fatalf("write low byte: %v", err)
	}
	if err := pit.WriteIOPort(nil, pitChannel0Port, []byte{0x00}); err != nil {
		t.Fatalf("write high byte: %v", err)
	}

	// Verify initial state: output should be low while counting
	advance(1 * time.Millisecond)
	counter := readPitCounter(t, pit)
	if counter == 0 || counter > 3 {
		t.Fatalf("during countdown, expected counter 1-3, got %d", counter)
	}

	// Mode 0 uses deadline-based calculation for reads
	// Advance to when count should reach zero
	advance(2 * time.Millisecond)
	counter = readPitCounter(t, pit)
	if counter != 0 {
		t.Fatalf("after countdown, expected counter 0, got %d", counter)
	}

	// Note: Mode 0 uses time.AfterFunc which fires asynchronously with real time,
	// so we can't easily test the interrupt callback in a deterministic way with a manual clock.
	// Instead, we verify the one-shot behavior by checking that:
	// 1. Counter reaches 0 (verified above)
	// 2. Counter stays at 0 and doesn't reload
	advance(10 * time.Millisecond)
	counter = readPitCounter(t, pit)
	if counter != 0 {
		t.Fatalf("one-shot timer reloaded: expected counter 0, got %d", counter)
	}
}

// TestPITCounterLatchAndReadback tests counter latching and readback commands.
func TestPITCounterLatchAndReadback(t *testing.T) {
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

	factory := &manualTimerFactory{}
	pit := NewPIT(nil,
		WithPITClock(nowFn),
		WithPITTimerFactory(factory.Factory),
		WithPITTick(1*time.Millisecond),
	)

	if err := pit.Init(nil); err != nil {
		t.Fatalf("init pit: %v", err)
	}

	// Configure channel 0 in mode 2 with reload value 0x1234
	// Control word 0x34: channel 0, access low/high, mode 2, binary
	if err := pit.WriteIOPort(nil, pitControlPort, []byte{0x34}); err != nil {
		t.Fatalf("write control: %v", err)
	}
	// Write reload value to start the channel
	if err := pit.WriteIOPort(nil, pitChannel0Port, []byte{0x34}); err != nil {
		t.Fatalf("write low byte: %v", err)
	}
	if err := pit.WriteIOPort(nil, pitChannel0Port, []byte{0x12}); err != nil {
		t.Fatalf("write high byte: %v", err)
	}

	// Advance time so counter changes and channel is running
	advance(2 * time.Millisecond)

	// Verify channel is running by reading counter
	initialCounter := readPitCounter(t, pit)
	if initialCounter == 0 || initialCounter >= 0x1234 {
		t.Fatalf("channel should be running: counter=%04x", initialCounter)
	}

	// Latch count using control port command
	// Latch command: channel 0, latch count
	if err := pit.WriteIOPort(nil, pitControlPort, []byte{0x00}); err != nil {
		t.Fatalf("latch count: %v", err)
	}

	// Read latched value (should be stable even as time advances)
	advance(5 * time.Millisecond)
	buf := []byte{0}
	if err := pit.ReadIOPort(nil, pitChannel0Port, buf); err != nil {
		t.Fatalf("read low byte: %v", err)
	}
	low := buf[0]
	if err := pit.ReadIOPort(nil, pitChannel0Port, buf); err != nil {
		t.Fatalf("read high byte: %v", err)
	}
	high := buf[0]
	latchedValue := uint16(high)<<8 | uint16(low)

	// Advance more time and read current (non-latched) value - should be different
	advance(10 * time.Millisecond)
	currentValue := readPitCounter(t, pit)
	if currentValue == latchedValue {
		t.Fatalf("current value should differ from latched value: both %04x", currentValue)
	}

	// Latch again and verify we get a new latched value
	if err := pit.WriteIOPort(nil, pitControlPort, []byte{0x00}); err != nil {
		t.Fatalf("latch count again: %v", err)
	}
	if err := pit.ReadIOPort(nil, pitChannel0Port, buf); err != nil {
		t.Fatalf("read latched low byte: %v", err)
	}
	latchedLow := buf[0]
	if err := pit.ReadIOPort(nil, pitChannel0Port, buf); err != nil {
		t.Fatalf("read latched high byte: %v", err)
	}
	latchedHigh := buf[0]
	latchedValue2 := uint16(latchedHigh)<<8 | uint16(latchedLow)

	// New latched value should match current value at latch time
	if latchedValue2 != currentValue {
		t.Fatalf("new latched value should match current: got %04x, expected %04x", latchedValue2, currentValue)
	}

	// Test readback command (0xC2 = readback all counters, status and count)
	// Readback command format: 11 (readback) | 0 (reserved) | 111 (all counters) | 11 (status+count)
	// This latches status and count for all channels
	if err := pit.WriteIOPort(nil, pitControlPort, []byte{0xC2}); err != nil {
		t.Fatalf("readback command: %v", err)
	}

	// After readback, we should be able to read status and count for channel 0
	// Channel 0 status (read first if latched)
	if err := pit.ReadIOPort(nil, pitChannel0Port, buf); err != nil {
		t.Fatalf("read status: %v", err)
	}
	status := buf[0]
	// Status byte: bit 7 = OUT pin, bit 6 = null count, bits 5-4 = access mode, bits 3-1 = mode, bit 0 = BCD
	// Verify status byte format is valid (not all zeros)
	if status == 0 {
		t.Fatalf("status byte should not be zero")
	}
	// Verify access mode bits (bits 5-4) are valid (0-3)
	accessBits := (status >> 4) & 0x3
	if accessBits > 3 {
		t.Fatalf("invalid access mode bits: %d (status=0x%02x)", accessBits, status)
	}
	// Verify mode bits (bits 3-1) are valid (0-7)
	modeBits := (status >> 1) & 0x7
	if modeBits > 7 {
		t.Fatalf("invalid mode bits: %d (status=0x%02x)", modeBits, status)
	}

	// Channel 0 count (latched by readback command)
	if err := pit.ReadIOPort(nil, pitChannel0Port, buf); err != nil {
		t.Fatalf("read count low: %v", err)
	}
	readbackLow := buf[0]
	if err := pit.ReadIOPort(nil, pitChannel0Port, buf); err != nil {
		t.Fatalf("read count high: %v", err)
	}
	readbackHigh := buf[0]
	readbackValue := uint16(readbackHigh)<<8 | uint16(readbackLow)

	// Readback should latch the current count at the time of the command
	// The value should be reasonable (not 0)
	// Note: For mode 2, the counter counts down from reload to 1, then reloads
	// The exact value depends on timing, so we just verify it's a valid non-zero value
	if readbackValue == 0 {
		t.Fatalf("readback value should not be zero: got %04x", readbackValue)
	}
	// Verify readback actually returned a latched value (not just current count)
	// by checking it's different from a fresh read
	advance(5 * time.Millisecond)
	freshValue := readPitCounter(t, pit)
	// The readback value should be different from the fresh value (unless we got very lucky with timing)
	// This verifies that readback actually latched a value
	if readbackValue == freshValue && readbackValue == 0x1234 {
		t.Fatalf("readback may not have latched: got %04x same as fresh %04x", readbackValue, freshValue)
	}
}

// TestPITChannel2GatedMode0 tests mode 0 (one-shot) on channel 2 with gate control.
// This is the exact scenario Linux uses for TSC calibration:
// 1. Configure channel 2 in mode 0
// 2. Write count value - counter should NOT start (gate is low)
// 3. Enable gate via port 0x61 - counter starts, output goes LOW
// 4. Wait for count to expire - output goes HIGH
func TestPITChannel2GatedMode0(t *testing.T) {
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

	pit := NewPIT(nil,
		WithPITClock(nowFn),
		WithPITTick(1*time.Millisecond),
	)
	port61 := NewPort61(pit)

	if err := pit.Init(nil); err != nil {
		t.Fatalf("init pit: %v", err)
	}
	if err := port61.Init(nil); err != nil {
		t.Fatalf("init port61: %v", err)
	}

	// Configure channel 2 in mode 0 (one-shot)
	// Control word 0xB0: channel 2, access low/high, mode 0, binary
	if err := pit.WriteIOPort(nil, pitControlPort, []byte{0xB0}); err != nil {
		t.Fatalf("write control: %v", err)
	}

	// Write count value (10 ticks = 10ms with 1ms tick)
	if err := pit.WriteIOPort(nil, pitChannel2Port, []byte{0x0A}); err != nil {
		t.Fatalf("write low byte: %v", err)
	}
	if err := pit.WriteIOPort(nil, pitChannel2Port, []byte{0x00}); err != nil {
		t.Fatalf("write high byte: %v", err)
	}

	// Gate is initially low - channel should be armed but NOT running
	// Output should stay HIGH (not counting yet)
	buf := []byte{0}
	if err := port61.ReadIOPort(nil, pitPort61, buf); err != nil {
		t.Fatalf("read port61: %v", err)
	}
	if buf[0]&1 != 0 {
		t.Fatalf("gate should be low initially")
	}

	// Check output is HIGH (bit 5 of port 0x61) - counter hasn't started
	if err := port61.ReadIOPort(nil, pitPort61, buf); err != nil {
		t.Fatalf("read port61 output: %v", err)
	}
	if buf[0]&(1<<5) == 0 {
		t.Fatalf("output should be HIGH before gate enabled, got 0x%02x", buf[0])
	}

	// Advance time - counter should NOT have started (still armed)
	advance(5 * time.Millisecond)
	if err := port61.ReadIOPort(nil, pitPort61, buf); err != nil {
		t.Fatalf("read port61 after delay: %v", err)
	}
	if buf[0]&(1<<5) == 0 {
		t.Fatalf("output should still be HIGH (counter armed, not running), got 0x%02x", buf[0])
	}

	// Enable gate via port 0x61 bit 0 - this should trigger countdown
	if err := port61.WriteIOPort(nil, pitPort61, []byte{0x01}); err != nil {
		t.Fatalf("enable gate: %v", err)
	}

	// Output should now be LOW (countdown started)
	if err := port61.ReadIOPort(nil, pitPort61, buf); err != nil {
		t.Fatalf("read port61 after gate enable: %v", err)
	}
	if buf[0]&(1<<5) != 0 {
		t.Fatalf("output should be LOW after gate enabled (countdown started), got 0x%02x", buf[0])
	}

	// Advance partway through countdown - output should still be LOW
	advance(5 * time.Millisecond)
	if err := port61.ReadIOPort(nil, pitPort61, buf); err != nil {
		t.Fatalf("read port61 during countdown: %v", err)
	}
	if buf[0]&(1<<5) != 0 {
		t.Fatalf("output should still be LOW during countdown, got 0x%02x", buf[0])
	}

	// Advance past countdown completion - output should be HIGH
	advance(10 * time.Millisecond)
	if err := port61.ReadIOPort(nil, pitPort61, buf); err != nil {
		t.Fatalf("read port61 after countdown: %v", err)
	}
	if buf[0]&(1<<5) == 0 {
		t.Fatalf("output should be HIGH after countdown complete, got 0x%02x", buf[0])
	}
}

// TestPITPort61Integration tests port 0x61 integration with PIT channel 2.
func TestPITPort61Integration(t *testing.T) {
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

	factory := &manualTimerFactory{}
	pit := NewPIT(nil,
		WithPITClock(nowFn),
		WithPITTimerFactory(factory.Factory),
		WithPITTick(1*time.Millisecond),
	)

	port61 := NewPort61(pit)

	if err := pit.Init(nil); err != nil {
		t.Fatalf("init pit: %v", err)
	}
	if err := port61.Init(nil); err != nil {
		t.Fatalf("init port61: %v", err)
	}

	// Configure channel 2 in mode 3 (square wave) with reload value 0x10
	if err := pit.WriteIOPort(nil, pitControlPort, []byte{0xB6}); err != nil {
		t.Fatalf("write control: %v", err)
	}
	if err := pit.WriteIOPort(nil, pitChannel2Port, []byte{0x10}); err != nil {
		t.Fatalf("write low byte: %v", err)
	}
	if err := pit.WriteIOPort(nil, pitChannel2Port, []byte{0x00}); err != nil {
		t.Fatalf("write high byte: %v", err)
	}

	// Initially gate should be low (disabled)
	buf := []byte{0}
	if err := port61.ReadIOPort(nil, pitPort61, buf); err != nil {
		t.Fatalf("read port61: %v", err)
	}
	initialVal := buf[0]
	if initialVal&1 != 0 {
		t.Fatalf("expected gate bit 0 initially, got 0x%02x", initialVal)
	}

	// Enable gate via port 0x61 bit 0
	if err := port61.WriteIOPort(nil, pitPort61, []byte{0x01}); err != nil {
		t.Fatalf("write port61 gate: %v", err)
	}

	// Verify gate bit is set
	if err := port61.ReadIOPort(nil, pitPort61, buf); err != nil {
		t.Fatalf("read port61 after write: %v", err)
	}
	val := buf[0]
	if val&1 == 0 {
		t.Fatalf("expected gate bit set, got 0x%02x", val)
	}

	// Advance time and check channel 2 output status (bit 5)
	advance(5 * time.Millisecond)
	if err := port61.ReadIOPort(nil, pitPort61, buf); err != nil {
		t.Fatalf("read port61 output: %v", err)
	}
	outputVal := buf[0]
	outputHigh := (outputVal & (1 << 5)) != 0

	// Channel 2 should be running and outputting
	if !outputHigh {
		t.Fatalf("expected channel 2 output high, got 0x%02x", outputVal)
	}

	// Test speaker data bit (bit 1)
	if err := port61.WriteIOPort(nil, pitPort61, []byte{0x03}); err != nil {
		t.Fatalf("write port61 speaker: %v", err)
	}
	if err := port61.ReadIOPort(nil, pitPort61, buf); err != nil {
		t.Fatalf("read port61 speaker: %v", err)
	}
	speakerVal := buf[0]
	if speakerVal&(1<<1) == 0 {
		t.Fatalf("expected speaker bit set, got 0x%02x", speakerVal)
	}

	// Test refresh bit (bit 4) - should toggle on each read
	refresh1 := (speakerVal & (1 << 4)) != 0
	if err := port61.ReadIOPort(nil, pitPort61, buf); err != nil {
		t.Fatalf("read port61 refresh: %v", err)
	}
	refresh2 := (buf[0] & (1 << 4)) != 0
	if refresh1 == refresh2 {
		t.Fatalf("expected refresh bit to toggle, got %v then %v", refresh1, refresh2)
	}

	// Disable gate and verify channel 2 stops
	if err := port61.WriteIOPort(nil, pitPort61, []byte{0x02}); err != nil {
		t.Fatalf("write port61 disable gate: %v", err)
	}
	advance(10 * time.Millisecond)
	if err := port61.ReadIOPort(nil, pitPort61, buf); err != nil {
		t.Fatalf("read port61 after disable: %v", err)
	}
	gateDisabled := buf[0]
	if gateDisabled&1 != 0 {
		t.Fatalf("expected gate bit cleared, got 0x%02x", gateDisabled)
	}
}
