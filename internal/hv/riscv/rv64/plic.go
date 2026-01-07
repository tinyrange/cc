package rv64

import (
	"sync"
)

// PLIC register offsets
const (
	PLICPriorityBase  = 0x000000 // Priority registers (1024 sources)
	PLICPendingBase   = 0x001000 // Pending bits
	PLICEnableBase    = 0x002000 // Enable bits per context
	PLICThresholdBase = 0x200000 // Threshold and claim per context
)

// PLIC context offsets (per-hart, per-mode)
const (
	PLICContextStride = 0x1000
)

// Maximum number of interrupt sources
const PLICMaxSources = 1024

// PLIC implements the Platform Level Interrupt Controller
type PLIC struct {
	cpu *CPU
	mu  sync.Mutex

	// Priority for each source (0-7, 0 = disabled)
	priority [PLICMaxSources]uint32

	// Pending bits (1 bit per source)
	pending [PLICMaxSources / 32]uint32

	// Enable bits per context
	// For simplicity, we only support 2 contexts: M-mode and S-mode
	enable [2][PLICMaxSources / 32]uint32

	// Threshold per context
	threshold [2]uint32

	// Claimed interrupt per context
	claimed [2]uint32
}

// NewPLIC creates a new PLIC
func NewPLIC(cpu *CPU) *PLIC {
	return &PLIC{
		cpu: cpu,
	}
}

// Size implements Device
func (p *PLIC) Size() uint64 {
	return PLICSize
}

// Read implements Device
func (p *PLIC) Read(offset uint64, size int) (uint64, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	switch {
	case offset < PLICPendingBase:
		// Priority registers
		source := offset / 4
		if source < PLICMaxSources {
			return uint64(p.priority[source]), nil
		}

	case offset >= PLICPendingBase && offset < PLICEnableBase:
		// Pending bits
		word := (offset - PLICPendingBase) / 4
		if word < uint64(len(p.pending)) {
			return uint64(p.pending[word]), nil
		}

	case offset >= PLICEnableBase && offset < PLICThresholdBase:
		// Enable bits
		relOffset := offset - PLICEnableBase
		context := relOffset / 0x80
		word := (relOffset % 0x80) / 4
		if context < 2 && word < uint64(len(p.enable[0])) {
			return uint64(p.enable[context][word]), nil
		}

	case offset >= PLICThresholdBase:
		// Threshold and claim
		relOffset := offset - PLICThresholdBase
		context := relOffset / PLICContextStride
		regOffset := relOffset % PLICContextStride

		if context < 2 {
			switch regOffset {
			case 0: // Threshold
				return uint64(p.threshold[context]), nil
			case 4: // Claim
				return uint64(p.claim(int(context))), nil
			}
		}
	}

	return 0, nil
}

// Write implements Device
func (p *PLIC) Write(offset uint64, size int, value uint64) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	switch {
	case offset < PLICPendingBase:
		// Priority registers
		source := offset / 4
		if source < PLICMaxSources && source > 0 { // Source 0 is reserved
			p.priority[source] = uint32(value) & 7 // 3 bits
		}

	case offset >= PLICEnableBase && offset < PLICThresholdBase:
		// Enable bits
		relOffset := offset - PLICEnableBase
		context := relOffset / 0x80
		word := (relOffset % 0x80) / 4
		if context < 2 && word < uint64(len(p.enable[0])) {
			p.enable[context][word] = uint32(value)
		}

	case offset >= PLICThresholdBase:
		// Threshold and complete
		relOffset := offset - PLICThresholdBase
		context := relOffset / PLICContextStride
		regOffset := relOffset % PLICContextStride

		if context < 2 {
			switch regOffset {
			case 0: // Threshold
				p.threshold[context] = uint32(value) & 7
			case 4: // Complete
				p.complete(int(context), uint32(value))
			}
		}
	}

	p.updateInterrupt()
	return nil
}

// SetPending sets an interrupt as pending
func (p *PLIC) SetPending(source uint32, pending bool) {
	if source == 0 || source >= PLICMaxSources {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	word := source / 32
	bit := source % 32

	if pending {
		p.pending[word] |= 1 << bit
	} else {
		p.pending[word] &^= 1 << bit
	}

	p.updateInterrupt()
}

// claim claims the highest priority pending interrupt for a context
func (p *PLIC) claim(context int) uint32 {
	if context >= 2 {
		return 0
	}

	var bestSource uint32
	var bestPriority uint32

	for source := uint32(1); source < PLICMaxSources; source++ {
		word := source / 32
		bit := source % 32

		// Check if pending and enabled
		if (p.pending[word]&(1<<bit)) == 0 {
			continue
		}
		if (p.enable[context][word]&(1<<bit)) == 0 {
			continue
		}

		// Check priority against threshold
		priority := p.priority[source]
		if priority <= p.threshold[context] {
			continue
		}

		// Find highest priority (lower number = higher priority in some implementations,
		// but RISC-V PLIC uses higher number = higher priority)
		if priority > bestPriority {
			bestPriority = priority
			bestSource = source
		}
	}

	if bestSource != 0 {
		// Clear pending bit
		word := bestSource / 32
		bit := bestSource % 32
		p.pending[word] &^= 1 << bit
		p.claimed[context] = bestSource
	}

	p.updateInterrupt()
	return bestSource
}

// complete signals completion of interrupt handling
func (p *PLIC) complete(context int, source uint32) {
	if context >= 2 || source == 0 || source >= PLICMaxSources {
		return
	}

	// Clear the claimed status
	if p.claimed[context] == source {
		p.claimed[context] = 0
	}

	p.updateInterrupt()
}

// updateInterrupt updates the external interrupt pending bits
func (p *PLIC) updateInterrupt() {
	// Check M-mode context (context 0)
	mInt := p.hasPendingInterrupt(0)
	if mInt {
		p.cpu.Mip |= MipMEIP
	} else {
		p.cpu.Mip &^= MipMEIP
	}

	// Check S-mode context (context 1)
	sInt := p.hasPendingInterrupt(1)
	if sInt {
		p.cpu.Mip |= MipSEIP
	} else {
		p.cpu.Mip &^= MipSEIP
	}
}

// hasPendingInterrupt checks if there's a pending interrupt above threshold
func (p *PLIC) hasPendingInterrupt(context int) bool {
	if context >= 2 {
		return false
	}

	for source := uint32(1); source < PLICMaxSources; source++ {
		word := source / 32
		bit := source % 32

		// Check if pending and enabled
		if (p.pending[word]&(1<<bit)) == 0 {
			continue
		}
		if (p.enable[context][word]&(1<<bit)) == 0 {
			continue
		}

		// Check priority against threshold
		if p.priority[source] > p.threshold[context] {
			return true
		}
	}

	return false
}

var _ Device = (*PLIC)(nil)
