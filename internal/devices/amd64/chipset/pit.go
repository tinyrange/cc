package chipset

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/tinyrange/cc/internal/hv"
)

const (
	pitChannel0Port uint16 = 0x40
	pitChannel1Port uint16 = 0x41
	pitChannel2Port uint16 = 0x42
	pitControlPort  uint16 = 0x43
	pitPort61              = 0x61

	pitInputFrequency = 1193182
)

var pitTickDuration = time.Second / pitInputFrequency

// VmTime models a virtual time source for the PIT.
type VmTime interface {
	Now() time.Duration
	SetTimeout(deadline time.Duration)
	CancelTimeout()
}

// PIT emulates the legacy 8254 programmable interval timer used by x86 PCs.
type PIT struct {
	mu sync.Mutex

	now          func() time.Time
	tick         time.Duration
	timers       [3]*pitChannel
	irq          irqLine
	timerFactory timerFactory

	vmTime VmTime

	debugCh2Reads int
	debugCh0Ticks int
	debugCh0Arms  int
	debugCh2High  byte
}

// PITOption customises the PIT instance, mainly for tests.
type PITOption func(*PIT)

// WithPITClock overrides the time base used to compute counter state.
func WithPITClock(now func() time.Time) PITOption {
	return func(p *PIT) {
		if now != nil {
			p.now = now
		}
	}
}

// WithPITTick overrides the duration of a single PIT tick.
func WithPITTick(d time.Duration) PITOption {
	return func(p *PIT) {
		if d > 0 {
			p.tick = d
		}
	}
}

// WithPITTimerFactory injects a custom periodic timer factory (used in tests).
func WithPITTimerFactory(factory func(time.Duration, func()) timerHandle) PITOption {
	return func(p *PIT) {
		if factory != nil {
			p.timerFactory = factory
		}
	}
}

// NewPIT builds a programmable interval timer backed by the supplied IRQ sink.
func NewPIT(irq irqLine, opts ...PITOption) *PIT {
	pit := &PIT{
		now:          time.Now,
		tick:         pitTickDuration,
		irq:          irq,
		timerFactory: defaultTimerFactory,
	}
	if pit.irq == nil {
		pit.irq = noopIRQLine{}
	}
	for i := range pit.timers {
		pit.timers[i] = newPitChannel()
	}
	for _, opt := range opts {
		opt(pit)
	}
	return pit
}

// Init implements hv.Device.
func (p *PIT) Init(vm hv.VirtualMachine) error {
	_ = vm
	return nil
}

// Poll is a placeholder hook to integrate PIT with a poll-based scheduler.
// Future work will migrate timer delivery away from real time tickers.
func (p *PIT) Poll(context.Context) error {
	if p.vmTime != nil {
		// Placeholder: hook VmTime to drive virtual deadlines.
	}
	return nil
}

// SetChannel2Gate sets the gate input for channel 2 (used by port 0x61).
func (p *PIT) SetChannel2Gate(high bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.timers[2].setGate(high)
}

// Channel2OutputHigh reports the OUT state of channel 2.
func (p *PIT) Channel2OutputHigh() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	ch2 := p.timers[2]
	_ = ch2.currentCount(p.now(), p.tick)
	return ch2.outputHigh
}

// IOPorts implements hv.X86IOPortDevice.
func (p *PIT) IOPorts() []uint16 {
	return []uint16{pitChannel0Port, pitChannel1Port, pitChannel2Port, pitControlPort}
}

// ReadIOPort implements hv.X86IOPortDevice.
func (p *PIT) ReadIOPort(port uint16, data []byte) error {
	if len(data) != 1 {
		return fmt.Errorf("pit: invalid read size %d", len(data))
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	switch port {
	case pitChannel0Port, pitChannel1Port, pitChannel2Port:
		idx := int(port - pitChannel0Port)
		value := p.timers[idx].read(p.now(), p.tick)
		data[0] = value
	case pitControlPort:
		data[0] = 0xFF
	default:
		return fmt.Errorf("pit: invalid read port 0x%04x", port)
	}
	return nil
}

// WriteIOPort implements hv.X86IOPortDevice.
func (p *PIT) WriteIOPort(port uint16, data []byte) error {
	if len(data) != 1 {
		return fmt.Errorf("pit: invalid write size %d", len(data))
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	switch port {
	case pitChannel0Port, pitChannel1Port, pitChannel2Port:
		idx := int(port - pitChannel0Port)
		if p.timers[idx].write(p.now(), p.tick, data[0]) && idx == 0 {
			if p.debugCh0Arms < 4 {
				p.debugCh0Arms++
				ch := p.timers[0]
				fmt.Printf("pit: ch0 arm reload=%04x mode=%d tick=%v\n", ch.reload, ch.control.mode, p.tick)
			}
			p.armChannel0Locked()
		}
	case pitControlPort:
		p.writeControlLocked(data[0])
	default:
		return fmt.Errorf("pit: invalid write port 0x%04x", port)
	}
	return nil
}

func (p *PIT) writeControlLocked(value byte) {
	selectField := (value >> 6) & 0x3
	if selectField == 0x3 {
		p.handleReadBackLocked(value)
		return
	}

	idx := int(selectField)
	access := pitAccessMode((value >> 4) & 0x3)
	mode := pitMode((value >> 1) & 0x7)
	bcd := value&0x1 == 1
	if mode == pitMode6Alt {
		mode = pitMode2
	} else if mode == pitMode7Alt {
		mode = pitMode3
	}

	if access == pitAccessLatch {
		p.timers[idx].latchCount(p.now(), p.tick)
		return
	}

	p.timers[idx].setControl(access, mode, bcd)
	if idx == 0 {
		p.disarmChannel0Locked()
	}
}

func (p *PIT) handleReadBackLocked(value byte) {
	command := readBackCommand(value)
	selections := []bool{command.counter0(), command.counter1(), command.counter2()}
	for idx, sel := range selections {
		if !sel {
			continue
		}
		if command.status() {
			p.timers[idx].latchStatus()
		}
		if command.count() {
			p.timers[idx].latchCount(p.now(), p.tick)
		}
	}
}

func (p *PIT) armChannel0Locked() {
	ch := p.timers[0]
	p.disarmChannel0Locked()
	if !ch.running {
		return
	}
	counts := ch.effectiveReload()
	if counts == 0 {
		return
	}
	period := time.Duration(counts) * p.tick
	if period <= 0 {
		return
	}
	if ch.control.mode == pitMode0 {
		timer := time.AfterFunc(period, func() { p.handleChannel0OneShot() })
		ch.timer = timerHandleFunc(func() {
			timer.Stop()
		})
		return
	}
	if ch.control.mode == pitMode4 {
		// Mode 4: Software triggered strobe (one-shot pulse)
		timer := time.AfterFunc(period, func() { p.handleChannel0Strobe() })
		ch.timer = timerHandleFunc(func() {
			timer.Stop()
		})
		return
	}
	if ch.control.mode == pitMode3 {
		// Mode 3: Square wave generator - 50% duty cycle
		// For even counts: high for n/2, low for n/2
		// For odd counts: high for (n+1)/2, low for (n-1)/2
		// Output toggles every half period, interrupt on falling edge
		ch.outputHigh = true
		ch.squareWaveHigh = true
		// Use full period timer, toggle happens at half period internally
		handle := p.timerFactory(period, func() { p.handleChannel0SquareWavePeriod() })
		ch.timer = handle
		return
	}
	// Mode 2: Rate generator (periodic pulse)
	handle := p.timerFactory(period, func() { p.handleChannel0Tick() })
	ch.timer = handle
}

func (p *PIT) disarmChannel0Locked() {
	ch := p.timers[0]
	if ch.timer != nil {
		ch.timer.Stop()
		ch.timer = nil
	}
}

func (p *PIT) handleChannel0Tick() {
	p.mu.Lock()
	defer p.mu.Unlock()

	ch := p.timers[0]
	if !ch.running || ch.reload == 0 {
		return
	}
	// Mode 2: Rate generator - output pulses low for one tick at end of period
	ch.lastReload = p.now()
	ch.outputHigh = false
	if p.debugCh0Ticks < 8 {
		p.debugCh0Ticks++
		fmt.Printf("pit: ch0 tick (mode2) reload=%04x outputHigh=%v\n", ch.reload, ch.outputHigh)
	}
	p.raiseIRQLocked(0)
	// Output goes high again immediately (next tick will be high)
	ch.outputHigh = true
}

func (p *PIT) handleChannel0SquareWavePeriod() {
	p.mu.Lock()
	defer p.mu.Unlock()

	ch := p.timers[0]
	if !ch.running || ch.reload == 0 {
		return
	}
	counts := ch.effectiveReload()
	if counts == 0 {
		return
	}
	period := time.Duration(counts) * p.tick
	halfPeriod := period / 2
	if counts%2 == 1 {
		halfPeriod = time.Duration((counts+1)/2) * p.tick
	}

	// Check if we're at the half-period point (falling edge - generate interrupt)
	elapsed := p.now().Sub(ch.lastReload)
	if elapsed >= halfPeriod {
		// Falling edge - output goes low, generate interrupt
		ch.squareWaveHigh = false
		ch.outputHigh = false
		p.raiseIRQLocked(0)
	} else {
		// Rising edge - output goes high
		ch.squareWaveHigh = true
		ch.outputHigh = true
	}
	ch.lastReload = p.now()
}

func (p *PIT) handleChannel0Strobe() {
	p.mu.Lock()
	defer p.mu.Unlock()

	ch := p.timers[0]
	// Mode 4: Software triggered strobe - pulse low for one tick
	ch.outputHigh = false
	ch.running = false
	p.disarmChannel0Locked()
	p.raiseIRQLocked(0)
	// Output goes high again immediately
	ch.outputHigh = true
}

func (p *PIT) handleChannel0OneShot() {
	p.mu.Lock()
	defer p.mu.Unlock()

	ch := p.timers[0]
	ch.running = false
	ch.outputHigh = true
	p.disarmChannel0Locked()
	p.raiseIRQLocked(0)
}

func (p *PIT) raiseIRQLocked(line uint8) {
	if p.irq == nil {
		return
	}
	p.irq.SetIRQ(line, true)
	p.irq.SetIRQ(line, false)
}

type pitAccessMode uint8

const (
	pitAccessLatch   pitAccessMode = 0
	pitAccessLow     pitAccessMode = 1
	pitAccessHigh    pitAccessMode = 2
	pitAccessLowHigh pitAccessMode = 3
)

type pitMode uint8

const (
	pitMode0    pitMode = 0
	pitMode1    pitMode = 1
	pitMode2    pitMode = 2
	pitMode3    pitMode = 3
	pitMode4    pitMode = 4
	pitMode5    pitMode = 5
	pitMode6Alt pitMode = 6
	pitMode7Alt pitMode = 7
)

type pitChannel struct {
	control pitControl

	pendingValue uint16
	expectHigh   bool

	reload     uint16
	lastReload time.Time
	running    bool
	nullCount  bool

	outputHigh bool
	gate       bool

	timer timerHandle

	countLatched     bool
	countLatchHigh   bool
	countLatchValue  uint16
	statusLatched    bool
	statusLatchValue byte
	readHigh         bool
	latchedReadValue uint16

	deadline time.Time

	// Mode 3 (square wave) state
	squareWaveHigh bool
}

type pitControl struct {
	access pitAccessMode
	mode   pitMode
	bcd    bool
}

func newPitChannel() *pitChannel {
	return &pitChannel{
		control:    pitControl{access: pitAccessLowHigh, mode: pitMode3},
		nullCount:  true,
		outputHigh: true,
	}
}

func (ch *pitChannel) setControl(access pitAccessMode, mode pitMode, bcd bool) {
	ch.control = pitControl{access: access, mode: mode, bcd: bcd}
	ch.pendingValue = 0
	ch.expectHigh = false
	ch.readHigh = false
	ch.countLatched = false
	ch.statusLatched = false
	ch.nullCount = true
	ch.running = false
	ch.outputHigh = true
	ch.deadline = time.Time{}
	ch.squareWaveHigh = false
}

func (ch *pitChannel) write(now time.Time, tick time.Duration, value byte) bool {
	switch ch.control.access {
	case pitAccessLow:
		ch.pendingValue = uint16(value)
		ch.expectHigh = false
	case pitAccessHigh:
		ch.pendingValue = uint16(value) << 8
		ch.expectHigh = false
	case pitAccessLowHigh:
		if !ch.expectHigh {
			ch.pendingValue = (ch.pendingValue & 0xFF00) | uint16(value)
			ch.expectHigh = true
			return false
		}
		ch.pendingValue = (uint16(value) << 8) | (ch.pendingValue & 0x00FF)
		ch.expectHigh = false
	default:
		return false
	}

	ch.reload = ch.pendingValue
	ch.lastReload = now
	ch.running = true
	ch.nullCount = false
	ch.readHigh = false
	ch.countLatched = false
	ch.statusLatched = false
	ch.deadline = time.Time{}
	switch ch.control.mode {
	case pitMode0:
		// Mode 0: One-shot countdown to zero; record deadline for faster reads.
		ch.deadline = now.Add(time.Duration(ch.effectiveReload()) * tick)
		// OUT goes low shortly after loading the count while the timer runs.
		ch.outputHigh = false
	case pitMode2:
		// Mode 2: Rate generator - output stays high during countdown
		ch.outputHigh = true
	case pitMode3:
		// Mode 3: Square wave - output starts high
		ch.outputHigh = true
		ch.squareWaveHigh = true
	case pitMode4:
		// Mode 4: Software triggered strobe - output stays high until count expires
		ch.outputHigh = true
	default:
		ch.outputHigh = true
	}
	_ = tick
	return true
}

func (ch *pitChannel) read(now time.Time, tick time.Duration) byte {
	if ch.statusLatched {
		ch.statusLatched = false
		return ch.statusLatchValue
	}

	value, latched := ch.nextReadableValue(now, tick)

	switch ch.control.access {
	case pitAccessLow:
		if !latched {
			ch.readHigh = false
		}
		return byte(value)
	case pitAccessHigh:
		if !latched {
			ch.readHigh = false
		}
		return byte(value >> 8)
	case pitAccessLowHigh:
		if !ch.readHigh {
			ch.readHigh = true
			ch.latchedReadValue = value
			return byte(value)
		}
		ch.readHigh = false
		return byte(ch.latchedReadValue >> 8)
	default:
		return byte(value)
	}
}

func (ch *pitChannel) nextReadableValue(now time.Time, tick time.Duration) (uint16, bool) {
	if ch.countLatched {
		value := ch.countLatchValue
		if !ch.countLatchHigh && ch.control.access == pitAccessLowHigh {
			ch.countLatchHigh = true
		} else {
			ch.countLatched = false
			ch.countLatchHigh = false
		}
		return value, true
	}
	return ch.currentCount(now, tick), false
}

func (ch *pitChannel) currentCount(now time.Time, tick time.Duration) uint16 {
	if !ch.running {
		switch ch.control.mode {
		case pitMode0:
			if ch.outputHigh {
				return 0
			}
			return ch.reload
		case pitMode4:
			// Mode 4: After strobe, output is high
			return ch.reload
		default:
			return ch.reload
		}
	}
	if !ch.deadline.IsZero() && ch.control.mode == pitMode0 {
		remaining := ch.deadline.Sub(now)
		if remaining <= 0 {
			ch.outputHigh = true
			ch.running = false
			return 0
		}
		ticks := uint64((remaining + tick - 1) / tick)
		// MODE0 only counts down once; cap at reload.
		if ticks > uint64(ch.reload) {
			ticks = uint64(ch.reload)
		}
		return uint16(ticks)
	}
	elapsed := now.Sub(ch.lastReload)
	if elapsed < 0 {
		elapsed = 0
	}
	ticks := uint64(elapsed / tick)
	period := uint64(ch.effectiveReload())
	if period == 0 {
		return ch.reload
	}
	if ticks >= period {
		switch ch.control.mode {
		case pitMode0:
			ch.outputHigh = true
			ch.running = false
			return 0
		case pitMode2:
			// Mode 2: Output pulses low at end of period, then reloads
			ticks %= period
		case pitMode3:
			// Mode 3: Square wave continues
			ticks %= period
		case pitMode4:
			// Mode 4: One-shot strobe, output goes high after pulse
			ch.outputHigh = true
			ch.running = false
			return ch.reload
		default:
			ticks %= period
		}
	}
	if ticks == 0 {
		return ch.reload
	}
	remaining := period - ticks
	if remaining == 1<<16 {
		return 0
	}
	return uint16(remaining)
}

func (ch *pitChannel) latchCount(now time.Time, tick time.Duration) {
	if ch.countLatched {
		return
	}
	ch.countLatchValue = ch.currentCount(now, tick)
	ch.countLatched = true
	ch.countLatchHigh = false
}

func (ch *pitChannel) latchStatus() {
	ch.statusLatched = true
	ch.statusLatchValue = ch.statusByte()
}

func (ch *pitChannel) statusByte() byte {
	status := byte(0)
	if ch.outputHigh {
		status |= 1 << 7
	}
	if ch.nullCount {
		status |= 1 << 6
	}
	status |= byte(ch.control.access&0x3) << 4
	mode := byte(ch.control.mode)
	if mode == byte(pitMode6Alt) {
		mode = byte(pitMode2)
	} else if mode == byte(pitMode7Alt) {
		mode = byte(pitMode3)
	}
	status |= (mode & 0x7) << 1
	if ch.control.bcd {
		status |= 1
	}
	return status
}

func (ch *pitChannel) toggleOutput() {
	ch.outputHigh = !ch.outputHigh
}

func (ch *pitChannel) setGate(gate bool) {
	ch.gate = gate
}

func (ch *pitChannel) effectiveReload() uint32 {
	if ch.reload == 0 {
		return 1 << 16
	}
	return uint32(ch.reload)
}

type readBackCommand byte

func (c readBackCommand) counter0() bool { return (byte(c)>>1)&1 == 1 }
func (c readBackCommand) counter1() bool { return (byte(c)>>2)&1 == 1 }
func (c readBackCommand) counter2() bool { return (byte(c)>>3)&1 == 1 }
func (c readBackCommand) status() bool   { return (byte(c)>>4)&1 == 1 }
func (c readBackCommand) count() bool    { return (byte(c)>>5)&1 == 1 }

var _ hv.X86IOPortDevice = (*PIT)(nil)
var _ hv.Device = (*PIT)(nil)
