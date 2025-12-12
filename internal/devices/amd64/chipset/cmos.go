package chipset

import (
	"fmt"
	"sync"
	"time"

	"github.com/tinyrange/cc/internal/hv"
)

const (
	cmosAddrPort uint16 = 0x70
	cmosDataPort uint16 = 0x71

	cmosRegSeconds      byte = 0x00
	cmosRegSecondsAlarm byte = 0x01
	cmosRegMinutes      byte = 0x02
	cmosRegMinutesAlarm byte = 0x03
	cmosRegHours        byte = 0x04
	cmosRegHoursAlarm   byte = 0x05
	cmosRegWeekday      byte = 0x06
	cmosRegDayOfMonth   byte = 0x07
	cmosRegMonth        byte = 0x08
	cmosRegYear         byte = 0x09
	cmosRegStatusA      byte = 0x0A
	cmosRegStatusB      byte = 0x0B
	cmosRegStatusC      byte = 0x0C
	cmosRegStatusD      byte = 0x0D
	cmosRegCentury      byte = 0x32
)

const (
	statusBSet             = 1 << 7
	statusBPeriodicEnable  = 1 << 6
	statusBAlarmEnable     = 1 << 5
	statusBUpdateEnable    = 1 << 4
	statusBSquareWave      = 1 << 3
	statusBBinaryMode      = 1 << 2
	statusB24HourMode      = 1 << 1
	statusBDaylightSavings = 1 << 0

	statusCIrqPeriodic = 1 << 6
	statusCIrqAlarm    = 1 << 5
	statusCIrqUpdate   = 1 << 4
	statusCIrqFlag     = 1 << 7
)

// CMOS emulates the MC146818 RTC/CMOS chip.
type CMOS struct {
	mu sync.Mutex

	addr       byte
	nmiMasked  bool
	cmos       [256]byte
	now        func() time.Time
	irqLine    LineInterrupt
	irqAssert  bool
	timer      timerHandle
	timerMaker timerFactory

	periodic timerHandle
	square   timerHandle
	update   timerHandle
	alarm    timerHandle

	uipDeadline time.Time
}

// CMOSOption customises the RTC for tests.
type CMOSOption func(*CMOS)

// WithCMOSClock overrides the time source used for RTC registers.
func WithCMOSClock(now func() time.Time) CMOSOption {
	return func(c *CMOS) {
		if now != nil {
			c.now = now
		}
	}
}

// WithCMOSTimerFactory overrides the periodic timer factory.
func WithCMOSTimerFactory(factory func(time.Duration, func()) timerHandle) CMOSOption {
	return func(c *CMOS) {
		if factory != nil {
			c.timerMaker = factory
		}
	}
}

// WithCMOSIRQLine overrides which IRQ line the RTC uses (defaults to 8).
// NewCMOS constructs an RTC device connected to the supplied IRQ sink.
func NewCMOS(irq irqLine, opts ...CMOSOption) *CMOS {
	c := &CMOS{
		now:        time.Now,
		irqLine:    LineInterruptDetached(),
		timerMaker: defaultTimerFactory,
	}
	c.SetIRQSink(irq)
	c.cmos[cmosRegStatusA] = 0x20
	c.cmos[cmosRegStatusB] = statusB24HourMode
	c.cmos[cmosRegStatusD] = 0x80
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// SetIRQSink configures the IRQ line used for RTC interrupts.
func (c *CMOS) SetIRQSink(sink irqLine) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if sink == nil {
		c.irqLine = LineInterruptDetached()
		return
	}
	c.irqLine = LineInterruptFromFunc(func(level bool) {
		sink.SetIRQ(8, level)
	})
}

// SetIRQLine configures the LineInterrupt used for RTC IRQ delivery.
func (c *CMOS) SetIRQLine(line LineInterrupt) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if line == nil {
		c.irqLine = LineInterruptDetached()
		return
	}
	c.irqLine = line
}

// Init implements hv.Device.
func (c *CMOS) Init(vm hv.VirtualMachine) error {
	_ = vm
	c.mu.Lock()
	defer c.mu.Unlock()
	c.uipDeadline = c.now()
	c.startTimerLocked()
	c.startPeriodicLocked()
	return nil
}

// IOPorts implements hv.X86IOPortDevice.
func (c *CMOS) IOPorts() []uint16 { return []uint16{cmosAddrPort, cmosDataPort} }

// ReadIOPort implements hv.X86IOPortDevice.
func (c *CMOS) ReadIOPort(port uint16, data []byte) error {
	if len(data) != 1 {
		return fmt.Errorf("cmos: invalid read size %d", len(data))
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	switch port {
	case cmosAddrPort:
		data[0] = c.addr
	case cmosDataPort:
		idx := c.addr & 0x7F
		data[0] = c.readRegisterLocked(idx)
	default:
		return fmt.Errorf("cmos: invalid read port 0x%04x", port)
	}
	return nil
}

// WriteIOPort implements hv.X86IOPortDevice.
func (c *CMOS) WriteIOPort(port uint16, data []byte) error {
	if len(data) != 1 {
		return fmt.Errorf("cmos: invalid write size %d", len(data))
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	switch port {
	case cmosAddrPort:
		c.addr = data[0] & 0x7F
		c.nmiMasked = data[0]&0x80 != 0
	case cmosDataPort:
		idx := c.addr & 0x7F
		c.writeRegisterLocked(idx, data[0])
	default:
		return fmt.Errorf("cmos: invalid write port 0x%04x", port)
	}
	return nil
}

func (c *CMOS) readRegisterLocked(idx byte) byte {
	switch idx {
	case cmosRegStatusA:
		statusA := c.cmos[cmosRegStatusA] &^ (1 << 7)
		if c.now().Before(c.uipDeadline) {
			statusA |= 1 << 7
		}
		return statusA
	case cmosRegSeconds, cmosRegMinutes, cmosRegHours,
		cmosRegWeekday, cmosRegDayOfMonth, cmosRegMonth,
		cmosRegYear, cmosRegCentury:
		fields := c.currentTimeFieldsLocked()
		switch idx {
		case cmosRegSeconds:
			return fields.second
		case cmosRegMinutes:
			return fields.minute
		case cmosRegHours:
			return fields.hour
		case cmosRegWeekday:
			return fields.weekday
		case cmosRegDayOfMonth:
			return fields.day
		case cmosRegMonth:
			return fields.month
		case cmosRegYear:
			return fields.year
		case cmosRegCentury:
			return fields.century
		}
	case cmosRegStatusC:
		value := c.cmos[cmosRegStatusC]
		c.cmos[cmosRegStatusC] = 0
		if c.irqAssert {
			c.irqLine.SetLevel(false)
			c.irqAssert = false
		}
		return value
	}
	return c.cmos[idx]
}

func (c *CMOS) writeRegisterLocked(idx byte, value byte) {
	switch idx {
	case cmosRegStatusA:
		// Mask out UIP; emulate oscillator bits (4-6) and rate select (0-3).
		c.cmos[idx] = value &^ (1 << 7)
		c.applyStatusA()
	case cmosRegStatusB:
		// Only bits 0-6 writable; bit 7 (SET) is handled separately.
		c.cmos[idx] = value & 0x7F
		if value&statusBSet != 0 {
			// Freeze time updates.
			if c.timer != nil {
				c.timer.Stop()
				c.timer = nil
			}
			if c.periodic != nil {
				c.periodic.Stop()
				c.periodic = nil
			}
		} else if c.timer == nil {
			c.timer = c.timerMaker(time.Second, func() { c.handleUpdateTick() })
			c.applyStatusA()
		}
		if c.alarm != nil {
			c.alarm.Stop()
			c.alarm = nil
		}
		c.scheduleAlarmLocked()
		c.refreshIRQLineLocked()
	case cmosRegStatusC, cmosRegStatusD:
		// Read-only
	case cmosRegSeconds, cmosRegMinutes, cmosRegHours,
		cmosRegWeekday, cmosRegDayOfMonth, cmosRegMonth,
		cmosRegYear, cmosRegCentury:
		statusB := c.cmos[cmosRegStatusB]
		val := value
		if statusB&statusBBinaryMode == 0 {
			val = fromBCD(value)
		}
		if idx == cmosRegHours && statusB&statusB24HourMode == 0 {
			val = decode12Hour(val)
		}
		c.cmos[idx] = val
		c.scheduleAlarmLocked()
	default:
		c.cmos[idx] = value
	}
}

func (c *CMOS) scheduleAlarmLocked() {
	statusB := c.cmos[cmosRegStatusB]
	if statusB&statusBAlarmEnable == 0 {
		return
	}
	if c.alarm != nil {
		c.alarm.Stop()
		c.alarm = nil
	}

	nowTime := c.now().UTC()
	next, ok := c.nextAlarmTimeLocked(nowTime)
	if !ok {
		return
	}
	delay := next.Sub(nowTime)
	if delay < 0 {
		delay = 0
	}

	c.alarm = c.timerMaker(delay, func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.cmos[cmosRegStatusC] |= statusCIrqAlarm
		c.refreshIRQLineLocked()
		// Reschedule the next occurrence.
		c.scheduleAlarmLocked()
	})
}

func (c *CMOS) nextAlarmTimeLocked(now time.Time) (time.Time, bool) {
	statusB := c.cmos[cmosRegStatusB]
	target := rtcFields{
		second: c.cmos[cmosRegSecondsAlarm],
		minute: c.cmos[cmosRegMinutesAlarm],
		hour:   c.cmos[cmosRegHoursAlarm],
	}
	// Convert to binary for comparison if needed.
	if statusB&statusBBinaryMode == 0 {
		target.second = fromBCD(target.second)
		target.minute = fromBCD(target.minute)
		target.hour = decode12Hour(target.hour)
	}
	start := now.Add(time.Second)
	for i := 0; i < 24*60*60; i++ {
		candidate := start.Add(time.Duration(i) * time.Second)
		h, m, s := candidate.UTC().Clock()
		if target.hour != 0xFF && int(target.hour) != h {
			continue
		}
		if target.minute != 0xFF && int(target.minute) != m {
			continue
		}
		if target.second != 0xFF && int(target.second) != s {
			continue
		}
		return candidate, true
	}
	return time.Time{}, false
}

func (c *CMOS) startTimerLocked() {
	if c.timerMaker == nil {
		c.timerMaker = defaultTimerFactory
	}
	if c.timer != nil {
		c.timer.Stop()
	}
	c.timer = c.timerMaker(time.Second, func() { c.handleUpdateTick() })
}

func (c *CMOS) applyStatusA() {
	dv := (c.cmos[cmosRegStatusA] >> 4) & 0x7
	oscillatorOn := dv != 0

	if !oscillatorOn {
		if c.timer != nil {
			c.timer.Stop()
			c.timer = nil
		}
		if c.periodic != nil {
			c.periodic.Stop()
			c.periodic = nil
		}
		if c.square != nil {
			c.square.Stop()
			c.square = nil
		}
		return
	}

	if c.timer == nil {
		c.timer = c.timerMaker(time.Second, func() { c.handleUpdateTick() })
	}

	// Status A bits 0-3: rate select.
	rateSelect := c.cmos[cmosRegStatusA] & 0x0F
	period := rateToDuration(rateSelect)

	if c.periodic != nil {
		c.periodic.Stop()
		c.periodic = nil
	}
	if period > 0 {
		c.periodic = c.timerMaker(period, func() {
			c.mu.Lock()
			defer c.mu.Unlock()
			if c.cmos[cmosRegStatusB]&statusBPeriodicEnable != 0 {
				c.cmos[cmosRegStatusC] |= statusCIrqPeriodic
				c.refreshIRQLineLocked()
			}
		})
	}

	// Square wave output follows the same rate select when enabled.
	if c.square != nil {
		c.square.Stop()
		c.square = nil
	}
	if c.cmos[cmosRegStatusB]&statusBSquareWave != 0 && period > 0 {
		c.square = c.timerMaker(period, func() {
			// Square wave toggles IRQ line independently of flags.
			c.irqLine.PulseInterrupt()
		})
	}
}

func rateToDuration(rate byte) time.Duration {
	// Follows PC AT RTC rates: rate=6 => 1024Hz, rate=15 => disabled.
	switch rate {
	case 3:
		return time.Second / 8192
	case 4:
		return time.Second / 4096
	case 5:
		return time.Second / 2048
	case 6:
		return time.Second / 1024
	case 7:
		return time.Second / 512
	case 8:
		return time.Second / 256
	case 9:
		return time.Second / 128
	case 10:
		return time.Second / 64
	case 11:
		return time.Second / 32
	case 12:
		return time.Second / 16
	case 13:
		return time.Second / 8
	case 14:
		return time.Second / 4
	case 15:
		return 0
	default:
		return time.Second
	}
}

func (c *CMOS) handleUpdateTick() {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Toggle UIP for a short window before raising update IRQ.
	c.uipDeadline = c.now().Add(2 * time.Millisecond)
	c.cmos[cmosRegStatusC] |= statusCIrqUpdate
	c.scheduleAlarmLocked()
	c.refreshIRQLineLocked()
}

func (c *CMOS) startPeriodicLocked() {
	if c.timerMaker == nil {
		c.timerMaker = defaultTimerFactory
	}
	if c.periodic != nil {
		c.periodic.Stop()
	}
	// Default periodic rate ~1Hz (Status A rate select 6 = 1024Hz in HW; keep simple).
	c.periodic = c.timerMaker(time.Second, func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.cmos[cmosRegStatusC] |= statusCIrqPeriodic
		c.refreshIRQLineLocked()
	})
}

func (c *CMOS) refreshIRQLineLocked() {
	statusB := c.cmos[cmosRegStatusB]
	statusC := c.cmos[cmosRegStatusC]

	active := false
	if statusC&statusCIrqUpdate != 0 && statusB&statusBUpdateEnable != 0 {
		active = true
	}
	if statusC&statusCIrqAlarm != 0 && statusB&statusBAlarmEnable != 0 {
		active = true
	}
	if statusC&statusCIrqPeriodic != 0 && statusB&statusBPeriodicEnable != 0 {
		active = true
	}

	if active {
		statusC |= statusCIrqFlag
	} else {
		statusC &^= statusCIrqFlag
	}
	c.cmos[cmosRegStatusC] = statusC

	if active && !c.irqAssert {
		c.irqLine.SetLevel(true)
		c.irqAssert = true
	} else if !active && c.irqAssert {
		c.irqLine.SetLevel(false)
		c.irqAssert = false
	}
}

func (c *CMOS) currentTimeFieldsLocked() rtcFields {
	if c.cmos[cmosRegStatusB]&statusBSet != 0 {
		return rtcFields{
			second:  c.cmos[cmosRegSeconds],
			minute:  c.cmos[cmosRegMinutes],
			hour:    c.cmos[cmosRegHours],
			weekday: c.cmos[cmosRegWeekday],
			day:     c.cmos[cmosRegDayOfMonth],
			month:   c.cmos[cmosRegMonth],
			year:    c.cmos[cmosRegYear],
			century: c.cmos[cmosRegCentury],
		}
	}

	t := c.now().UTC()
	if c.cmos[cmosRegStatusB]&statusBDaylightSavings != 0 {
		t = t.Add(time.Hour)
	}
	yearFull := t.Year()
	century := yearFull / 100
	year := yearFull % 100

	fields := rtcFields{
		second:  byte(t.Second()),
		minute:  byte(t.Minute()),
		hour:    byte(t.Hour()),
		weekday: byte(t.Weekday()) + 1,
		day:     byte(t.Day()),
		month:   byte(t.Month()),
		year:    byte(year),
		century: byte(century),
	}

	fields.normalize(c.cmos[cmosRegStatusB])
	return fields
}

type rtcFields struct {
	second, minute, hour byte
	weekday, day, month  byte
	year, century        byte
}

func (f *rtcFields) normalize(statusB byte) {
	binaryMode := statusB&statusBBinaryMode != 0
	twentyFour := statusB&statusB24HourMode != 0

	if !twentyFour {
		pm := f.hour >= 12
		hour := f.hour % 12
		if hour == 0 {
			hour = 12
		}
		if pm {
			hour |= 0x80
		}
		if binaryMode {
			f.hour = hour
		} else {
			low := hour &^ 0x80
			encoded := toBCD(low)
			if hour&0x80 != 0 {
				encoded |= 0x80
			}
			f.hour = encoded
		}
	} else if !binaryMode {
		f.hour = toBCD(f.hour)
	}

	if !binaryMode {
		f.second = toBCD(f.second)
		f.minute = toBCD(f.minute)
		f.day = toBCD(f.day)
		f.month = toBCD(f.month)
		f.year = toBCD(f.year)
		f.century = toBCD(f.century)
		f.weekday = toBCD(f.weekday)
	}
}

func toBCD(v byte) byte {
	return ((v / 10) << 4) | (v % 10)
}

func fromBCD(v byte) byte {
	return (v>>4)*10 + (v & 0x0F)
}

func decode12Hour(v byte) byte {
	pm := v&0x80 != 0
	hour := v & 0x7F
	if hour == 12 {
		hour = 0
	}
	if pm {
		hour += 12
	}
	return hour
}

var _ hv.X86IOPortDevice = (*CMOS)(nil)
var _ hv.Device = (*CMOS)(nil)
