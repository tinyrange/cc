package rv64

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync/atomic"
)

// ErrHalt is returned when the machine is halted
var ErrHalt = errors.New("machine halted")

// Machine represents a complete RV64GC system
type Machine struct {
	CPU   *CPU
	Bus   *Bus
	MMU   *MMU
	CLINT *CLINT
	PLIC  *PLIC
	UART  *UART

	// Debug output
	DebugOutput io.Writer

	// Halt flag
	halted atomic.Bool

	// Stop on write to address 0
	stopOnZero bool

	// Instruction count for yielding
	instructionCount uint64
}

// NewMachine creates a new RV64GC machine
func NewMachine(ramSize uint64, output io.Writer, input io.Reader) *Machine {
	bus := NewBus(ramSize)

	cpu := NewCPU(bus)
	mmu := NewMMU(cpu)
	clint := NewCLINT(cpu)
	plic := NewPLIC(cpu)
	uart := NewUART(output, input)

	// Add devices to bus
	bus.AddDevice(CLINTBase, clint)
	bus.AddDevice(PLICBase, plic)
	bus.AddDevice(UARTBase, uart)

	return &Machine{
		CPU:   cpu,
		Bus:   bus,
		MMU:   mmu,
		CLINT: clint,
		PLIC:  plic,
		UART:  uart,
	}
}

// Reset resets the machine to initial state
func (m *Machine) Reset() {
	m.CPU.Reset()
	m.MMU.FlushTLB()
	m.halted.Store(false)
}

// SetPC sets the program counter
func (m *Machine) SetPC(pc uint64) {
	m.CPU.PC = pc
}

// GetPC gets the program counter
func (m *Machine) GetPC() uint64 {
	return m.CPU.PC
}

// SetStopOnZero enables halting when writing to address 0
func (m *Machine) SetStopOnZero(enable bool) {
	m.stopOnZero = enable
}

// LoadBytes loads data into memory at the given physical address
func (m *Machine) LoadBytes(addr uint64, data []byte) error {
	return m.Bus.LoadBytes(addr, data)
}

// MemoryBase returns the base address of RAM
func (m *Machine) MemoryBase() uint64 {
	return m.Bus.RAMBase
}

// MemorySize returns the size of RAM
func (m *Machine) MemorySize() uint64 {
	return m.Bus.RAM.Size()
}

// Step executes a single instruction
func (m *Machine) Step() error {
	// Check for pending interrupts
	if !m.CPU.WFI {
		if pending, cause := m.CPU.CheckInterrupt(); pending {
			m.CPU.HandleTrap(cause, 0)
			return nil
		}
	} else {
		// WFI - check if we should wake up
		if pending, _ := m.CPU.CheckInterrupt(); pending {
			m.CPU.WFI = false
		} else {
			return nil // Still waiting
		}
	}

	// Translate instruction address
	pc := m.CPU.PC
	paddr, err := m.MMU.TranslateFetch(pc)
	if err != nil {
		if exc, ok := err.(ExceptionError); ok {
			m.CPU.HandleTrap(exc.Cause, pc)
			return nil
		}
		return err
	}

	// Fetch instruction
	insn, err := m.Bus.Fetch(paddr)
	if err != nil {
		m.CPU.HandleTrap(CauseInsnAccessFault, pc)
		return nil
	}

	// Check for compressed instruction
	isCompressed := (insn & 0x3) != 0x3
	if isCompressed {
		// Expand compressed instruction
		expanded, err := m.CPU.ExpandCompressed(uint16(insn))
		if err != nil {
			if exc, ok := err.(ExceptionError); ok {
				m.CPU.HandleTrap(exc.Cause, pc)
				return nil
			}
			return err
		}
		insn = expanded
	}

	// Save old PC for exception handling
	oldPC := m.CPU.PC

	// Execute instruction
	err = m.executeWithMMU(insn)
	if err != nil {
		if exc, ok := err.(ExceptionError); ok {
			m.CPU.PC = oldPC

			// Check for ecall from S-mode - handle as SBI call
			if exc.Cause == CauseEcallFromS {
				if err := m.HandleSBI(); err != nil {
					return err
				}
				// Advance PC past ecall instruction
				m.CPU.PC += 4
				return nil
			}

			m.CPU.HandleTrap(exc.Cause, exc.Tval)
			return nil
		}
		return err
	}

	// If PC wasn't changed by a jump, increment it
	if m.CPU.PC == oldPC {
		if isCompressed {
			m.CPU.PC += 2
		} else {
			m.CPU.PC += 4
		}
	}

	// Update counters
	m.CPU.Cycle++
	m.CPU.Instret++
	m.instructionCount++

	return nil
}

// executeWithMMU executes an instruction with MMU translation for memory ops
func (m *Machine) executeWithMMU(insn uint32) error {
	// Wrap bus operations with MMU translation
	op := opcode(insn)

	switch op {
	case OpLoad:
		return m.execLoadMMU(insn)
	case OpStore:
		return m.execStoreMMU(insn)
	case OpAMO:
		return m.execAMOMMU(insn)
	case OpLoadFP:
		return m.execLoadFPMMU(insn)
	case OpStoreFP:
		return m.execStoreFPMMU(insn)
	default:
		return m.CPU.Execute(insn)
	}
}

// execLoadMMU executes load with MMU
func (m *Machine) execLoadMMU(insn uint32) error {
	vaddr := uint64(int64(m.CPU.ReadReg(rs1(insn))) + immI(insn))
	paddr, err := m.MMU.TranslateRead(vaddr)
	if err != nil {
		if exc, ok := err.(ExceptionError); ok {
			exc.Tval = vaddr
			return exc
		}
		return err
	}

	f3 := funct3(insn)
	var val uint64

	switch f3 {
	case 0b000: // LB
		v, e := m.Bus.Read8(paddr)
		if e != nil {
			return Exception(CauseLoadAccessFault, vaddr)
		}
		val = uint64(int8(v))
	case 0b001: // LH
		v, e := m.Bus.Read16(paddr)
		if e != nil {
			return Exception(CauseLoadAccessFault, vaddr)
		}
		val = uint64(int16(v))
	case 0b010: // LW
		v, e := m.Bus.Read32(paddr)
		if e != nil {
			return Exception(CauseLoadAccessFault, vaddr)
		}
		val = uint64(int32(v))
	case 0b011: // LD
		v, e := m.Bus.Read64(paddr)
		if e != nil {
			return Exception(CauseLoadAccessFault, vaddr)
		}
		val = v
	case 0b100: // LBU
		v, e := m.Bus.Read8(paddr)
		if e != nil {
			return Exception(CauseLoadAccessFault, vaddr)
		}
		val = uint64(v)
	case 0b101: // LHU
		v, e := m.Bus.Read16(paddr)
		if e != nil {
			return Exception(CauseLoadAccessFault, vaddr)
		}
		val = uint64(v)
	case 0b110: // LWU
		v, e := m.Bus.Read32(paddr)
		if e != nil {
			return Exception(CauseLoadAccessFault, vaddr)
		}
		val = uint64(v)
	default:
		return Exception(CauseIllegalInsn, uint64(insn))
	}

	m.CPU.WriteReg(rd(insn), val)
	return nil
}

// execStoreMMU executes store with MMU
func (m *Machine) execStoreMMU(insn uint32) error {
	vaddr := uint64(int64(m.CPU.ReadReg(rs1(insn))) + immS(insn))
	paddr, err := m.MMU.TranslateWrite(vaddr)
	if err != nil {
		if exc, ok := err.(ExceptionError); ok {
			exc.Tval = vaddr
			return exc
		}
		return err
	}

	// Check for stop on zero
	if m.stopOnZero && paddr == 0 {
		m.halted.Store(true)
		return ErrHalt
	}

	val := m.CPU.ReadReg(rs2(insn))
	f3 := funct3(insn)

	var writeErr error
	switch f3 {
	case 0b000: // SB
		writeErr = m.Bus.Write8(paddr, uint8(val))
	case 0b001: // SH
		writeErr = m.Bus.Write16(paddr, uint16(val))
	case 0b010: // SW
		writeErr = m.Bus.Write32(paddr, uint32(val))
	case 0b011: // SD
		writeErr = m.Bus.Write64(paddr, val)
	default:
		return Exception(CauseIllegalInsn, uint64(insn))
	}

	if writeErr != nil {
		return Exception(CauseStoreAccessFault, vaddr)
	}

	return nil
}

// execAMOMMU executes atomic operations with MMU
func (m *Machine) execAMOMMU(insn uint32) error {
	vaddr := m.CPU.ReadReg(rs1(insn))
	paddr, err := m.MMU.TranslateWrite(vaddr)
	if err != nil {
		if exc, ok := err.(ExceptionError); ok {
			exc.Tval = vaddr
			return exc
		}
		return err
	}

	// Temporarily swap bus address translation
	origBus := m.CPU.Bus
	m.CPU.Bus = &translatedBus{bus: m.Bus, paddr: paddr, vaddr: vaddr}
	defer func() { m.CPU.Bus = origBus }()

	return m.CPU.execAMO(insn)
}

// translatedBus wraps Bus to use a pre-translated address
type translatedBus struct {
	bus   *Bus
	paddr uint64
	vaddr uint64
}

func (t *translatedBus) Read(addr uint64, size int) (uint64, error) {
	return t.bus.Read(t.paddr, size)
}

func (t *translatedBus) Write(addr uint64, size int, value uint64) error {
	return t.bus.Write(t.paddr, size, value)
}

func (t *translatedBus) Read8(addr uint64) (uint8, error)   { return t.bus.Read8(t.paddr) }
func (t *translatedBus) Read16(addr uint64) (uint16, error) { return t.bus.Read16(t.paddr) }
func (t *translatedBus) Read32(addr uint64) (uint32, error) { return t.bus.Read32(t.paddr) }
func (t *translatedBus) Read64(addr uint64) (uint64, error) { return t.bus.Read64(t.paddr) }
func (t *translatedBus) Write8(addr uint64, value uint8) error {
	return t.bus.Write8(t.paddr, value)
}
func (t *translatedBus) Write16(addr uint64, value uint16) error {
	return t.bus.Write16(t.paddr, value)
}
func (t *translatedBus) Write32(addr uint64, value uint32) error {
	return t.bus.Write32(t.paddr, value)
}
func (t *translatedBus) Write64(addr uint64, value uint64) error {
	return t.bus.Write64(t.paddr, value)
}

// execLoadFPMMU executes FP load with MMU
func (m *Machine) execLoadFPMMU(insn uint32) error {
	vaddr := uint64(int64(m.CPU.ReadReg(rs1(insn))) + immI(insn))
	paddr, err := m.MMU.TranslateRead(vaddr)
	if err != nil {
		if exc, ok := err.(ExceptionError); ok {
			exc.Tval = vaddr
			return exc
		}
		return err
	}

	rdReg := rd(insn)
	f3 := funct3(insn)

	switch f3 {
	case 0b010: // FLW
		val, err := m.Bus.Read32(paddr)
		if err != nil {
			return Exception(CauseLoadAccessFault, vaddr)
		}
		m.CPU.F[rdReg] = f32ToU64(u64ToF32(uint64(val)))
		m.CPU.setFS(3)

	case 0b011: // FLD
		val, err := m.Bus.Read64(paddr)
		if err != nil {
			return Exception(CauseLoadAccessFault, vaddr)
		}
		m.CPU.F[rdReg] = val
		m.CPU.setFS(3)

	default:
		return Exception(CauseIllegalInsn, uint64(insn))
	}

	return nil
}

// execStoreFPMMU executes FP store with MMU
func (m *Machine) execStoreFPMMU(insn uint32) error {
	vaddr := uint64(int64(m.CPU.ReadReg(rs1(insn))) + immS(insn))
	paddr, err := m.MMU.TranslateWrite(vaddr)
	if err != nil {
		if exc, ok := err.(ExceptionError); ok {
			exc.Tval = vaddr
			return exc
		}
		return err
	}

	rs2Reg := rs2(insn)
	f3 := funct3(insn)

	switch f3 {
	case 0b010: // FSW
		val := uint32(m.CPU.F[rs2Reg])
		if err := m.Bus.Write32(paddr, val); err != nil {
			return Exception(CauseStoreAccessFault, vaddr)
		}

	case 0b011: // FSD
		if err := m.Bus.Write64(paddr, m.CPU.F[rs2Reg]); err != nil {
			return Exception(CauseStoreAccessFault, vaddr)
		}

	default:
		return Exception(CauseIllegalInsn, uint64(insn))
	}

	return nil
}

// Run runs the machine until halted or context cancelled
func (m *Machine) Run(ctx context.Context, yieldAfter int64) error {
	if yieldAfter <= 0 {
		yieldAfter = 100000
	}

	for {
		// Check context
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Update timer
		m.CLINT.Tick()

		// Run a batch of instructions
		for i := int64(0); i < yieldAfter; i++ {
			err := m.Step()
			if err != nil {
				if errors.Is(err, ErrHalt) {
					return ErrHalt
				}
				return fmt.Errorf("step error at PC=0x%x: %w", m.CPU.PC, err)
			}
		}
	}
}

// Halt stops the machine
func (m *Machine) Halt() {
	m.halted.Store(true)
}

// IsHalted returns true if the machine is halted
func (m *Machine) IsHalted() bool {
	return m.halted.Load()
}

// AddDevice adds a device to the bus
func (m *Machine) AddDevice(base uint64, dev Device) {
	m.Bus.AddDevice(base, dev)
}

// ReadAt reads from guest physical memory
func (m *Machine) ReadAt(p []byte, off int64) (int, error) {
	addr := uint64(off)
	for i := range p {
		val, err := m.Bus.Read8(addr + uint64(i))
		if err != nil {
			return i, err
		}
		p[i] = val
	}
	return len(p), nil
}

// WriteAt writes to guest physical memory
func (m *Machine) WriteAt(p []byte, off int64) (int, error) {
	addr := uint64(off)
	for i, b := range p {
		if err := m.Bus.Write8(addr+uint64(i), b); err != nil {
			return i, err
		}
	}
	return len(p), nil
}
