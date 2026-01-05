package chipset

import "github.com/tinyrange/cc/internal/hv"

// Port61 implements the legacy port 0x61 speaker/timer gate register.
type Port61 struct {
	pit *PIT

	gate        bool
	speakerData bool
	refresh     bool
}

func NewPort61(pit *PIT) *Port61 {
	return &Port61{
		pit: pit,
	}
}

func (p *Port61) Init(vm hv.VirtualMachine) error {
	_ = vm
	return nil
}

func (p *Port61) IOPorts() []uint16 { return []uint16{pitPort61} }

func (p *Port61) ReadIOPort(ctx hv.ExitContext, port uint16, data []byte) error {
	if len(data) != 1 {
		return hv.ErrInterrupted
	}
	if port != pitPort61 {
		return hv.ErrInterrupted
	}

	var val byte
	if p.gate {
		val |= 1 << 0
	}
	if p.speakerData {
		val |= 1 << 1
	}
	if p.refresh {
		val |= 1 << 4
	}
	if p.pit != nil {
		if p.pit.Channel2OutputHigh() {
			val |= 1 << 5
		}
	}

	// Toggle refresh bit each read to simulate periodic toggling.
	p.refresh = !p.refresh
	data[0] = val
	return nil
}

func (p *Port61) WriteIOPort(ctx hv.ExitContext, port uint16, data []byte) error {
	if len(data) != 1 {
		return hv.ErrInterrupted
	}
	if port != pitPort61 {
		return hv.ErrInterrupted
	}

	val := data[0]
	p.gate = val&1 != 0
	p.speakerData = val&(1<<1) != 0

	if p.pit != nil {
		p.pit.SetChannel2Gate(p.gate)
	}

	return nil
}

var (
	_ hv.Device          = (*Port61)(nil)
	_ hv.X86IOPortDevice = (*Port61)(nil)
)
