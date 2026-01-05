package chipset

import (
	"testing"
	"time"
)

// dummyTimerFactory avoids spinning goroutines during tests.
func dummyTimerFactory(_ time.Duration, _ func()) timerHandle { return nil }

func TestCMOSStatusCClearsOnRead(t *testing.T) {
	c := NewCMOS(nil, WithCMOSClock(func() time.Time { return time.Unix(0, 0) }), WithCMOSTimerFactory(dummyTimerFactory))

	c.mu.Lock()
	c.cmos[cmosRegStatusC] = statusCIrqUpdate | statusCIrqFlag
	c.mu.Unlock()

	// Select status C register then read.
	if err := c.WriteIOPort(nil, cmosAddrPort, []byte{cmosRegStatusC}); err != nil {
		t.Fatalf("write addr: %v", err)
	}
	buf := []byte{0}
	if err := c.ReadIOPort(nil, cmosDataPort, buf); err != nil {
		t.Fatalf("read status C: %v", err)
	}
	if buf[0]&statusCIrqUpdate == 0 {
		t.Fatalf("expected update flag set in read value, got 0x%02x", buf[0])
	}
	if buf[0]&statusCIrqFlag == 0 {
		t.Fatalf("expected IRQ flag set in read value, got 0x%02x", buf[0])
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cmos[cmosRegStatusC] != 0 {
		t.Fatalf("expected status C cleared after read, got 0x%02x", c.cmos[cmosRegStatusC])
	}
}

func TestCMOSTimeRegistersBCDEncoding(t *testing.T) {
	now := time.Date(2023, 1, 2, 3, 4, 5, 0, time.UTC)
	c := NewCMOS(nil, WithCMOSClock(func() time.Time { return now }), WithCMOSTimerFactory(dummyTimerFactory))

	readReg := func(idx byte) byte {
		if err := c.WriteIOPort(nil, cmosAddrPort, []byte{idx}); err != nil {
			t.Fatalf("write addr: %v", err)
		}
		buf := []byte{0}
		if err := c.ReadIOPort(nil, cmosDataPort, buf); err != nil {
			t.Fatalf("read data: %v", err)
		}
		return buf[0]
	}

	if sec := readReg(cmosRegSeconds); sec != toBCD(5) {
		t.Fatalf("seconds BCD mismatch: got 0x%02x want 0x%02x", sec, toBCD(5))
	}
	if minute := readReg(cmosRegMinutes); minute != toBCD(4) {
		t.Fatalf("minutes BCD mismatch: got 0x%02x want 0x%02x", minute, toBCD(4))
	}
	if hour := readReg(cmosRegHours); hour != toBCD(3) {
		t.Fatalf("hours BCD mismatch: got 0x%02x want 0x%02x", hour, toBCD(3))
	}
}

func TestCMOSAlarmMatch(t *testing.T) {
	now := time.Date(2023, 1, 2, 3, 4, 0, 0, time.UTC)
	c := NewCMOS(nil, WithCMOSClock(func() time.Time { return now }), WithCMOSTimerFactory(func(d time.Duration, cb func()) timerHandle {
		return timerHandleFunc(func() {})
	}))

	// Enable alarm IRQ.
	c.mu.Lock()
	c.cmos[cmosRegStatusB] |= statusBAlarmEnable | statusBBinaryMode
	c.mu.Unlock()

	// Program alarm to fire at 03:04:05.
	for _, reg := range []struct {
		addr byte
		val  byte
	}{
		{cmosRegHoursAlarm, 3},
		{cmosRegMinutesAlarm, 4},
		{cmosRegSecondsAlarm, 5},
	} {
		if err := c.WriteIOPort(nil, cmosAddrPort, []byte{reg.addr}); err != nil {
			t.Fatalf("write addr: %v", err)
		}
		if err := c.WriteIOPort(nil, cmosDataPort, []byte{reg.val}); err != nil {
			t.Fatalf("write data: %v", err)
		}
	}

	// Move time to match.
	c.mu.Lock()
	c.now = func() time.Time { return time.Date(2023, 1, 2, 3, 4, 5, 0, time.UTC) }
	c.mu.Unlock()
	c.scheduleAlarmLocked()

	if err := c.WriteIOPort(nil, cmosAddrPort, []byte{cmosRegStatusC}); err != nil {
		t.Fatalf("select status C: %v", err)
	}
	buf := []byte{0}
	if err := c.ReadIOPort(nil, cmosDataPort, buf); err != nil {
		t.Fatalf("read status C: %v", err)
	}
	if buf[0]&statusCIrqAlarm == 0 {
		t.Fatalf("expected alarm flag set, got 0x%02x", buf[0])
	}
}
