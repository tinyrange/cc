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
	irq        irqLine
	irqLine    uint8
	irqAssert  bool
	timer      timerHandle
	timerMaker timerFactory
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
func WithCMOSIRQLine(line uint8) CMOSOption {
	return func(c *CMOS) {
		c.irqLine = line
	}
}

// NewCMOS constructs an RTC device connected to the supplied IRQ sink.
func NewCMOS(irq irqLine, opts ...CMOSOption) *CMOS {
	c := &CMOS{
		now:        time.Now,
		irq:        irq,
		irqLine:    8,
		timerMaker: defaultTimerFactory,
	}
	if c.irq == nil {
		c.irq = noopIRQLine{}
	}
	c.cmos[cmosRegStatusA] = 0x20
	c.cmos[cmosRegStatusB] = statusB24HourMode
	c.cmos[cmosRegStatusD] = 0x80
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Init implements hv.Device.
func (c *CMOS) Init(vm hv.VirtualMachine) error {
	_ = vm
	c.mu.Lock()
	defer c.mu.Unlock()
	c.startTimerLocked()
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
			c.irq.SetIRQ(c.irqLine, false)
			c.irqAssert = false
		}
		return value
	}
	return c.cmos[idx]
}

func (c *CMOS) writeRegisterLocked(idx byte, value byte) {
	switch idx {
	case cmosRegStatusA:
		c.cmos[idx] = value &^ (1 << 7)
	case cmosRegStatusB:
		c.cmos[idx] = value
		c.refreshIRQLineLocked()
	case cmosRegStatusC, cmosRegStatusD:
		// Read-only
	case cmosRegSeconds, cmosRegMinutes, cmosRegHours,
		cmosRegWeekday, cmosRegDayOfMonth, cmosRegMonth,
		cmosRegYear, cmosRegCentury:
		c.cmos[idx] = value
	default:
		c.cmos[idx] = value
	}
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

func (c *CMOS) handleUpdateTick() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cmos[cmosRegStatusC] |= statusCIrqUpdate
	c.refreshIRQLineLocked()
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

	if c.irq == nil {
		return
	}
	if active && !c.irqAssert {
		c.irq.SetIRQ(c.irqLine, true)
		c.irqAssert = true
	} else if !active && c.irqAssert {
		c.irq.SetIRQ(c.irqLine, false)
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

var _ hv.X86IOPortDevice = (*CMOS)(nil)
var _ hv.Device = (*CMOS)(nil)
