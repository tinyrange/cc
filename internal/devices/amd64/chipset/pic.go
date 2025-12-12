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

// DualPIC implements the classic pair of cascaded 8259A controllers.
type DualPIC struct {
	mu    sync.Mutex
	ready LineInterrupt

	vm hv.VirtualMachine

	pics [2]*pic

	ackHook AcknowledgeHook
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

func (p *DualPIC) ReadIOPort(port uint16, data []byte) error {
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

func (p *DualPIC) WriteIOPort(port uint16, data []byte) error {
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

	requested, vec := p.pics[0].acknowledgeInterrupt()
	if requested && vec&picIRQMask == picChainCommunicationIRQ {
		secRequested, secVec := p.pics[1].acknowledgeInterrupt()
		if !secRequested {
			panic("secondary PIC reported ready but returned spurious IRQ")
		}
		vec = secVec
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

var _ hv.X86IOPortDevice = (*DualPIC)(nil)
var _ hv.Device = (*DualPIC)(nil)

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
	ocw3      ocw3
	isr       byte
	elcr      byte
	lines     byte
	lineLow   byte
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
	return (p.irr() &^ p.imr) & higherNotISR
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

func (p *pic) acknowledgeInterrupt() (bool, uint8) {
	if line, ok := p.pendingLine(); ok {
		bit := byte(1 << line)
		p.lineLow &^= bit
		p.isr |= bit
		return true, p.icw2 | line
	}
	return false, p.icw2 | picSpuriousIRQ
}

func (p *pic) eoi(line *byte) {
	var mask byte
	if line != nil {
		mask = 1 << *line
	} else {
		mask = lowestSetBit(p.isr)
	}
	p.isr &^= mask
}

func (p *pic) readCommand() byte {
	if p.ocw3.poll() {
		p.ocw3.setPoll(false)
		requested, vec := p.acknowledgeInterrupt()
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
		switch {
		case ocw.EOI() && ocw.SL():
			line := ocw.Level()
			p.eoi(&line)
		case ocw.EOI():
			p.eoi(nil)
		}
		return
	}

	ocw := ocw3(value)
	if ocw.SpecialMaskEnabled() || ocw.SpecialMask() {
		return
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

func (o ocw2) Level() byte { return byte(o) & 0x07 }
func (o ocw2) SL() bool    { return byte(o)&0x40 != 0 }
func (o ocw2) EOI() bool   { return byte(o)&0x20 != 0 }

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
