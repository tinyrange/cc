package chipset

import (
	"fmt"
	"sync"

	"github.com/tinyrange/cc/internal/hv"
)

const resetControlPort = 0x10

// ResetControlPort emulates the legacy reset control register exposed at I/O
// port 0x10 on some PC-compatible chipsets.
type ResetControlPort struct {
	mu   sync.Mutex
	last byte
}

func NewResetControlPort() *ResetControlPort {
	return &ResetControlPort{}
}

func (p *ResetControlPort) Init(vm hv.VirtualMachine) error {
	return nil
}

func (p *ResetControlPort) IOPorts() []uint16 {
	return []uint16{resetControlPort}
}

func (p *ResetControlPort) ReadIOPort(port uint16, data []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := range data {
		data[i] = p.last
	}
	return nil
}

func (p *ResetControlPort) WriteIOPort(port uint16, data []byte) error {
	if len(data) == 0 {
		return fmt.Errorf("reset control: empty write")
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Store the last written byte so reads have a defined value, even though
	// guests typically never sample it after requesting a reset.
	p.last = data[len(data)-1]

	// The legacy reset control register treats bit 1 as the reset trigger.
	if data[0]&0x02 == 0 {
		return nil
	}

	return hv.ErrGuestRequestedReboot
}

var _ hv.X86IOPortDevice = (*ResetControlPort)(nil)
