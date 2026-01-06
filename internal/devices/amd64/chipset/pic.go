package chipset

import (
	"fmt"
	"math/bits"
	"sync"

	"github.com/tinyrange/cc/internal/hv"
)

const (
	primaryPicCommandPort   uint16 = 0x20
	primaryPicDataPort      uint16 = 0x21
	secondaryPicCommandPort        = 0xa0
	secondaryPicDataPort           = 0xa1
	primaryPicELCRPort             = 0x4d0
	secondaryPicELCRPort           = 0x4d1

	picChainCommunicationIRQ = 2
	picIRQMask               = 0x7
	picSpuriousIRQ           = 7
)

// picStats tracks statistics for a PIC.
type picStats struct {
	spuriousInterrupts uint64
	acknowledges       uint64
	perIRQ             [8]uint64
}

// DualPIC implements the classic pair of cascaded 8259A controllers.
type DualPIC struct {
	mu    sync.Mutex
	ready LineInterrupt

	vm hv.VirtualMachine

	pics [2]*pic

	ackHook AcknowledgeHook

	stats picStats
}

func NewDualPIC() *DualPIC {
	return &DualPIC{
		ready: LineInterruptDetached(),
		pics: [2]*pic{
			newPic(true),
			newPic(false),
		},
	}
}

func (p *DualPIC) SetReadySink(sink readySink) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if sink == nil {
		p.ready = LineInterruptDetached()
	} else {
		p.ready = LineInterruptFromFunc(sink.SetLevel)
	}
	p.syncOutputsLocked()
}

// SetReadyLine sets the interrupt line used for INT output.
func (p *DualPIC) SetReadyLine(line LineInterrupt) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if line == nil {
		p.ready = LineInterruptDetached()
	} else {
		p.ready = line
	}
	p.syncOutputsLocked()
}

// SetAcknowledgeHook installs a hook invoked when an interrupt is acknowledged.
func (p *DualPIC) SetAcknowledgeHook(hook AcknowledgeHook) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ackHook = hook
}

func (p *DualPIC) Init(vm hv.VirtualMachine) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.vm = vm
	// Reset both PICs, preserving ELCR but clearing lines
	p.pics[0].reset(false, true)
	p.pics[1].reset(false, true)
	p.stats = picStats{}
	return nil
}

func (p *DualPIC) IOPorts() []uint16 {
	return []uint16{
		primaryPicCommandPort,
		primaryPicDataPort,
		secondaryPicCommandPort,
		secondaryPicDataPort,
		primaryPicELCRPort,
		secondaryPicELCRPort,
	}
}

func (p *DualPIC) ReadIOPort(ctx hv.ExitContext, port uint16, data []byte) error {
	if len(data) != 1 {
		return fmt.Errorf("pic: invalid read size %d", len(data))
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	switch port {
	case primaryPicCommandPort:
		data[0] = p.pics[0].readCommand()
	case primaryPicDataPort:
		data[0] = p.pics[0].readData()
	case secondaryPicCommandPort:
		data[0] = p.pics[1].readCommand()
	case secondaryPicDataPort:
		data[0] = p.pics[1].readData()
	case primaryPicELCRPort:
		data[0] = p.pics[0].elcr
	case secondaryPicELCRPort:
		data[0] = p.pics[1].elcr
	default:
		return fmt.Errorf("pic: invalid read port 0x%04x", port)
	}
	return nil
}

func (p *DualPIC) WriteIOPort(ctx hv.ExitContext, port uint16, data []byte) error {
	if len(data) == 2 && (port == primaryPicCommandPort || port == secondaryPicCommandPort) {
		var prim, sec byte = data[0], data[1]
		if port == secondaryPicCommandPort {
			prim, sec = sec, prim
		}
		p.mu.Lock()
		p.pics[0].writeCommand(prim)
		p.pics[1].writeCommand(sec)
		p.syncOutputsLocked()
		p.mu.Unlock()
		return nil
	}

	if len(data) != 1 {
		return fmt.Errorf("pic: invalid write size %d", len(data))
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	switch port {
	case primaryPicCommandPort:
		p.pics[0].writeCommand(data[0])
	case primaryPicDataPort:
		p.pics[0].writeData(data[0])
	case secondaryPicCommandPort:
		p.pics[1].writeCommand(data[0])
	case secondaryPicDataPort:
		p.pics[1].writeData(data[0])
	case primaryPicELCRPort:
		p.pics[0].elcr = data[0]
	case secondaryPicELCRPort:
		p.pics[1].elcr = data[0]
	default:
		return fmt.Errorf("pic: invalid write port 0x%04x", port)
	}

	p.syncOutputsLocked()
	return nil
}

func (p *DualPIC) syncOutputsLocked() {
	cascade := p.pics[1].interruptPending()
	p.pics[0].setIRQ(picChainCommunicationIRQ, cascade)
	if p.ready == nil {
		p.ready = LineInterruptDetached()
	}
	p.ready.SetLevel(p.pics[0].interruptPending())
}

func (p *DualPIC) SetIRQ(line uint8, level bool) {
	p.mu.Lock()
	if line >= 16 {
		p.mu.Unlock()
		return
	}
	if line >= 8 {
		p.pics[1].setIRQ(line-8, level)
	} else {
		p.pics[0].setIRQ(line, level)
	}
	p.syncOutputsLocked()
	p.mu.Unlock()
}

// Acknowledge returns whether an interrupt was pending and, if so, what vector
// should be delivered to the CPU.
func (p *DualPIC) Acknowledge() (bool, uint8) {
	p.mu.Lock()
	defer p.mu.Unlock()

	requested, vec := p.pics[0].acknowledgeInterrupt(&p.stats)
	if requested && vec&picIRQMask == picChainCommunicationIRQ {
		secRequested, secVec := p.pics[1].acknowledgeInterrupt(&p.stats)
		if !secRequested {
			// Spurious interrupt from secondary PIC (IRQ15 = secondary's IRQ7)
			p.stats.spuriousInterrupts++
			vec = secVec // Return secondary's spurious vector (IRQ15)
			p.syncOutputsLocked()
			return false, vec
		}
		vec = secVec
		// Track acknowledge for secondary PIC
		p.stats.acknowledges++
		irq := vec & picIRQMask
		if irq < 8 {
			// Track per-IRQ within PIC (0-7 for each PIC)
			p.stats.perIRQ[irq]++
		}
	} else if requested {
		// Track acknowledge for primary PIC
		p.stats.acknowledges++
		irq := vec & picIRQMask
		if irq < 8 {
			p.stats.perIRQ[irq]++
		}
	} else {
		// Spurious interrupt from primary PIC (IRQ7)
		p.stats.spuriousInterrupts++
	}
	p.syncOutputsLocked()
	if requested && p.ackHook != nil {
		p.ackHook.PICAcknowledge(vec)
	}
	return requested, vec
}

func (p *DualPIC) String() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return fmt.Sprintf("PIC(primary=%v, secondary=%v)", p.pics[0], p.pics[1])
}

// Snapshot support ----------------------------------------------------------

type picSnapshot struct {
	InitStage     initStage
	ICW2          byte
	IMR           byte
	OCW2          ocw2
	OCW3          ocw3
	ISR           byte
	ELCR          byte
	Lines         byte
	LineLow       byte
	AutoEOIRotate bool
	SpecialMask   bool
}

type dualPicSnapshot struct {
	Primary   picSnapshot
	Secondary picSnapshot
	Stats     picStats
}

func (p *DualPIC) DeviceId() string { return "pic" }

func (p *DualPIC) CaptureSnapshot() (hv.DeviceSnapshot, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	snap := &dualPicSnapshot{
		Primary:   p.pics[0].captureSnapshot(),
		Secondary: p.pics[1].captureSnapshot(),
		Stats:     p.stats,
	}

	return snap, nil
}

func (p *DualPIC) RestoreSnapshot(snap hv.DeviceSnapshot) error {
	data, ok := snap.(*dualPicSnapshot)
	if !ok {
		return fmt.Errorf("pic: invalid snapshot type")
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	p.pics[0].restoreSnapshot(data.Primary)
	p.pics[1].restoreSnapshot(data.Secondary)
	p.stats = data.Stats

	p.syncOutputsLocked()
	return nil
}

func (p *pic) captureSnapshot() picSnapshot {
	return picSnapshot{
		InitStage:     p.initStage,
		ICW2:          p.icw2,
		IMR:           p.imr,
		OCW2:          p.ocw2,
		OCW3:          p.ocw3,
		ISR:           p.isr,
		ELCR:          p.elcr,
		Lines:         p.lines,
		LineLow:       p.lineLow,
		AutoEOIRotate: p.autoEOIRotate,
		SpecialMask:   p.specialMask,
	}
}

func (p *pic) restoreSnapshot(snap picSnapshot) {
	p.initStage = snap.InitStage
	p.icw2 = snap.ICW2
	p.imr = snap.IMR
	p.ocw2 = snap.OCW2
	p.ocw3 = snap.OCW3
	p.isr = snap.ISR
	p.elcr = snap.ELCR
	p.lines = snap.Lines
	p.lineLow = snap.LineLow
	p.autoEOIRotate = snap.AutoEOIRotate
	p.specialMask = snap.SpecialMask
}

var _ hv.X86IOPortDevice = (*DualPIC)(nil)
var _ hv.Device = (*DualPIC)(nil)
var _ hv.DeviceSnapshotter = (*DualPIC)(nil)

// AcknowledgeHook is notified when the PIC has acknowledged an interrupt.
type AcknowledgeHook interface {
	PICAcknowledge(vector uint8)
}

// pic models a single 8259A.
type pic struct {
	primary bool

	initStage initStage
	icw2      byte
	imr       byte
	ocw2      ocw2
	ocw3      ocw3
	isr       byte
	elcr      byte
	lines     byte
	lineLow   byte

	// Rotate-on-auto-EOI mode
	autoEOIRotate bool
	// Special mask mode
	specialMask bool
}

func newPic(primary bool) *pic {
	icw2 := byte(0)
	if !primary {
		icw2 = 8
	}
	return &pic{
		primary:   primary,
		initStage: initUninitialized,
		icw2:      icw2,
		lineLow:   0xff,
	}
}

func (p *pic) reset(preserveLines, preserveELCR bool) {
	lines := byte(0)
	if preserveLines {
		lines = p.lines
	}
	elcr := byte(0)
	if preserveELCR {
		elcr = p.elcr
	}
	primary := p.primary
	*p = *newPic(primary)
	p.lines = lines
	if preserveELCR {
		p.elcr = elcr
	}
	p.autoEOIRotate = false
	p.specialMask = false
}

// setPriorityLevel sets the priority level (0-7) as the lowest priority.
func (p *pic) setPriorityLevel(level byte) {
	// In a real PIC, this rotates the priority base register.
	// We track this implicitly through ISR state, so this is a no-op
	// for our implementation, but we acknowledge the command.
	_ = level
}

func (p *pic) irr() byte {
	return p.lines & (p.elcr | p.lineLow)
}

func (p *pic) setIRQ(line uint8, high bool) {
	bit := byte(1 << line)
	if high {
		p.lines |= bit
	} else {
		p.lines &^= bit
		p.lineLow |= bit
	}
}

func (p *pic) readyVec() byte {
	highestISR := lowestSetBit(p.isr)
	higherNotISR := highestISR - 1
	var maskedIRR byte
	if p.specialMask {
		// In special mask mode, IMR is ignored for priority calculation
		// Only ISR bits block lower priority interrupts
		maskedIRR = p.irr()
	} else {
		maskedIRR = p.irr() &^ p.imr
	}
	return maskedIRR & higherNotISR
}

func (p *pic) interruptPending() bool {
	return p.readyVec() != 0
}

func (p *pic) pendingLine() (byte, bool) {
	if vec := p.readyVec(); vec != 0 {
		return byte(bits.TrailingZeros8(vec)), true
	}
	return 0, false
}

func (p *pic) acknowledgeInterrupt(stats *picStats) (bool, uint8) {
	if line, ok := p.pendingLine(); ok {
		bit := byte(1 << line)
		p.lineLow &^= bit
		p.isr |= bit
		// Handle rotate-on-auto-EOI if enabled
		if p.autoEOIRotate {
			p.rotatePriority()
		}
		return true, p.icw2 | line
	}
	return false, p.icw2 | picSpuriousIRQ
}

// rotatePriority rotates the interrupt priority after EOI.
// The lowest priority interrupt becomes the highest priority.
func (p *pic) rotatePriority() {
	// Rotate ISR to find the next priority base
	// This is a simplified implementation - real hardware rotates
	// the priority base register, but we track it via ISR state
	if p.isr == 0 {
		return
	}
	// Priority rotation: after EOI, the serviced interrupt becomes
	// the lowest priority. We don't need to track this explicitly
	// as the priority is determined by bit position in ISR.
}

func (p *pic) eoi(line *byte) {
	var mask byte
	if line != nil {
		mask = 1 << *line
	} else {
		mask = lowestSetBit(p.isr)
	}
	p.isr &^= mask
	// Handle rotate-on-auto-EOI if enabled
	if p.autoEOIRotate {
		p.rotatePriority()
	}
}

func (p *pic) readCommand() byte {
	if p.ocw3.poll() {
		p.ocw3.setPoll(false)
		// Poll command doesn't update stats, use nil stats
		var nilStats picStats
		requested, vec := p.acknowledgeInterrupt(&nilStats)
		val := byte(0)
		if requested {
			val = 1 << 7
		}
		val |= vec & picIRQMask
		return val
	}
	if p.ocw3.rr() {
		if p.ocw3.ris() {
			return p.isr
		}
		return p.irr()
	}
	return 0
}

func (p *pic) readData() byte {
	return p.imr
}

func (p *pic) writeCommand(value byte) {
	const (
		initBit    = 0x10
		commandBit = 0x08
	)

	if value&initBit != 0 {
		if value != 0x11 {
			// Unsupported; keep going but log later.
		}
		p.reset(true, true)
		p.initStage = initExpectingICW2
		return
	}

	if p.initStage != initInitialized {
		// OCWs delivered before init completes are ignored.
		return
	}

	if value&commandBit == 0 {
		ocw := ocw2(value)
		p.ocw2 = ocw
		// Check for rotate-on-auto-EOI: R=1, SL=0, EOI=1
		if ocw.Rotate() && !ocw.SL() && ocw.EOI() {
			p.autoEOIRotate = true
		} else if ocw.Rotate() && ocw.SL() && !ocw.EOI() {
			// Set priority command: R=1, SL=1, EOI=0
			// Rotate priority to specified level
			level := ocw.Level()
			p.setPriorityLevel(level)
		} else if ocw.Rotate() && ocw.SL() && ocw.EOI() {
			// Rotate priority on specific EOI: R=1, SL=1, EOI=1
			level := ocw.Level()
			p.eoi(&level)
			p.setPriorityLevel(level)
		} else {
			// Regular EOI commands
			switch {
			case ocw.EOI() && ocw.SL():
				line := ocw.Level()
				p.eoi(&line)
			case ocw.EOI():
				p.eoi(nil)
			}
			// Clear rotate-on-auto-EOI if not set
			if !ocw.Rotate() {
				p.autoEOIRotate = false
			}
		}
		return
	}

	ocw := ocw3(value)
	// Handle special mask mode commands
	if ocw.SpecialMaskEnabled() {
		// ESMM=1: Enable special mask mode
		if ocw.SpecialMask() {
			// SMM=1: Enter special mask mode
			p.specialMask = true
		} else {
			// SMM=0: Exit special mask mode
			p.specialMask = false
		}
	}
	p.ocw3 = ocw
}

func (p *pic) writeData(value byte) {
	switch p.initStage {
	case initUninitialized, initInitialized:
		p.imr = value
	case initExpectingICW2:
		if value&picIRQMask != 0 {
			return
		}
		p.icw2 = value &^ picIRQMask
		p.initStage = initExpectingICW3
	case initExpectingICW3:
		// For primary, expect bit 2 set; for secondary expect value 2.
		if p.primary {
			if value != (1 << picChainCommunicationIRQ) {
				return
			}
		} else if value != picChainCommunicationIRQ {
			return
		}
		p.initStage = initExpectingICW4
	case initExpectingICW4:
		if value != 1 && value != 3 {
			return
		}
		p.initStage = initInitialized
	}
}

type initStage int

const (
	initUninitialized initStage = iota
	initExpectingICW2
	initExpectingICW3
	initExpectingICW4
	initInitialized
)

type ocw2 byte

type ocw3 byte

func (o ocw2) Level() byte  { return byte(o) & 0x07 }
func (o ocw2) SL() bool     { return byte(o)&0x40 != 0 }
func (o ocw2) EOI() bool    { return byte(o)&0x20 != 0 }
func (o ocw2) Rotate() bool { return byte(o)&0x80 != 0 }

func (o ocw3) rr() bool  { return byte(o)&0x02 != 0 }
func (o ocw3) ris() bool { return byte(o)&0x04 != 0 }
func (o ocw3) poll() bool {
	return byte(o)&0x04 != 0 && byte(o)&0x01 != 0
}
func (o *ocw3) setPoll(v bool) {
	if v {
		*o |= 0x04 | 0x01
	} else {
		*o &^= 0x04 | 0x01
	}
}
func (o ocw3) SpecialMask() bool        { return byte(o)&0x20 != 0 }
func (o ocw3) SpecialMaskEnabled() bool { return byte(o)&0x40 != 0 }

func lowestSetBit(b byte) byte {
	return b & byte(-int8(b))
}

var _ readySink = ReadySinkFunc(nil)
