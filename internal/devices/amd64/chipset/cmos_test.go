package chipset

import (
	"sync"
	"testing"
	"time"
)

func TestCMOSReturnsCurrentTime(t *testing.T) {
	fixed := time.Date(2024, time.March, 14, 15, 9, 26, 0, time.UTC)
	cmos := NewCMOS(nil,
		WithCMOSClock(func() time.Time { return fixed }),
		WithCMOSTimerFactory(func(time.Duration, func()) timerHandle { return &manualTimer{} }),
	)

	if err := cmos.Init(nil); err != nil {
		t.Fatalf("init cmos: %v", err)
	}

	sec := readCMOSRegister(t, cmos, cmosRegSeconds)
	min := readCMOSRegister(t, cmos, cmosRegMinutes)
	hour := readCMOSRegister(t, cmos, cmosRegHours)
	day := readCMOSRegister(t, cmos, cmosRegDayOfMonth)
	month := readCMOSRegister(t, cmos, cmosRegMonth)
	year := readCMOSRegister(t, cmos, cmosRegYear)
	century := readCMOSRegister(t, cmos, cmosRegCentury)

	if bcdToUint(sec) != 26 || bcdToUint(min) != 9 || bcdToUint(hour&0x7F) != 15 {
		t.Fatalf("unexpected time BCD: h=%02x m=%02x s=%02x", hour, min, sec)
	}
	if bcdToUint(day) != 14 || bcdToUint(month) != 3 {
		t.Fatalf("unexpected date BCD: day=%02x month=%02x", day, month)
	}
	if bcdToUint(year) != 24 || bcdToUint(century) != 20 {
		t.Fatalf("unexpected year/century: year=%02x century=%02x", year, century)
	}
}

func TestCMOSUpdateInterrupt(t *testing.T) {
	var mu sync.Mutex
	var calls []struct {
		line  uint8
		level bool
	}
	sink := IRQLineFunc(func(line uint8, level bool) {
		mu.Lock()
		calls = append(calls, struct {
			line  uint8
			level bool
		}{line: line, level: level})
		mu.Unlock()
	})

	factory := &manualTimerFactory{}
	cmos := NewCMOS(sink,
		WithCMOSTimerFactory(factory.Factory),
		WithCMOSClock(func() time.Time { return time.Unix(0, 0) }),
	)
	if err := cmos.Init(nil); err != nil {
		t.Fatalf("init cmos: %v", err)
	}

	writeCMOSRegister(t, cmos, cmosRegStatusB, statusB24HourMode|statusBUpdateEnable)

	if len(factory.timers) != 1 {
		t.Fatalf("expected rtc timer, got %d", len(factory.timers))
	}

	factory.timers[0].Fire()

	mu.Lock()
	if len(calls) == 0 || calls[len(calls)-1].line != 8 || !calls[len(calls)-1].level {
		t.Fatalf("expected irq8 high, calls=%v", calls)
	}
	mu.Unlock()

	_ = readCMOSRegister(t, cmos, cmosRegStatusC)

	mu.Lock()
	if len(calls) == 0 || calls[len(calls)-1].line != 8 || calls[len(calls)-1].level {
		t.Fatalf("expected irq8 low after ack, calls=%v", calls)
	}
	mu.Unlock()
}

func readCMOSRegister(t *testing.T, c *CMOS, reg byte) byte {
	t.Helper()
	if err := c.WriteIOPort(cmosAddrPort, []byte{reg}); err != nil {
		t.Fatalf("write addr: %v", err)
	}
	buf := []byte{0}
	if err := c.ReadIOPort(cmosDataPort, buf); err != nil {
		t.Fatalf("read data: %v", err)
	}
	return buf[0]
}

func writeCMOSRegister(t *testing.T, c *CMOS, reg byte, value byte) {
	t.Helper()
	if err := c.WriteIOPort(cmosAddrPort, []byte{reg}); err != nil {
		t.Fatalf("write addr: %v", err)
	}
	if err := c.WriteIOPort(cmosDataPort, []byte{value}); err != nil {
		t.Fatalf("write data: %v", err)
	}
}

func bcdToUint(v byte) int {
	return int((v>>4)*10 + (v & 0x0F))
}
