//go:build experimental

package ccvm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"
)

// Machine exposes a minimal wrapper around the existing VirtualMachine so the
// interpreter can be embedded behind higher-level hypervisor interfaces without
// changing the core decode/execute logic.
type Machine struct {
	vm      *VirtualMachine
	memBase uint64
}

// MemoryRegion exposes an exported view of the emulator's memory mapping.
type MemoryRegion interface {
	io.ReaderAt
	io.WriterAt
	Size() int64
}

type mappedRegion struct {
	region memoryRegion
}

func (m mappedRegion) Size() int64 {
	return m.region.Size()
}

func (m mappedRegion) ReadAt(p []byte, off int64) (n int, err error) {
	return m.region.ReadAt(p, off)
}

func (m mappedRegion) WriteAt(p []byte, off int64) (n int, err error) {
	return m.region.WriteAt(p, off)
}

// NewMachine builds a bare RISC-V machine with the requested memory size. The
// main RAM is mapped at RAM_BASE to match the original emulator layout.
func NewMachine(memSize uint64) (*Machine, error) {
	if memSize == 0 {
		return nil, fmt.Errorf("ccvm: memory size must be non-zero")
	}

	vm := &VirtualMachine{
		pc:      RAM_BASE,
		priv:    PRV_M,
		misa:    MCPUID_SUPER | MCPUID_USER | MCPUID_I | MCPUID_M | MCPUID_A | MCPUID_F | MCPUID_D | MCPUID_C,
		curXlen: 64,
		mxl:     2,
		mstatus: 0xa00000000,
		ramSize: memSize,
	}

	vm.plic = &plic{vm: vm}
	vm.clint = &clint{vm: vm}

	vm.ramMap = vm.registerRam(RAM_BASE, rawRegion(make([]byte, memSize)))

	vm.tlbInit()

	return &Machine{
		vm:      vm,
		memBase: RAM_BASE,
	}, nil
}

// MemoryBase reports the base physical address for RAM.
func (m *Machine) MemoryBase() uint64 {
	return m.memBase
}

// MemorySize reports the configured RAM size.
func (m *Machine) MemorySize() uint64 {
	return m.vm.ramSize
}

// ReadAt implements io.ReaderAt by delegating to the underlying VM.
func (m *Machine) ReadAt(p []byte, off int64) (int, error) {
	n, _, err := m.vm.readAt(p, uint64(off))
	return n, err
}

// WriteAt implements io.WriterAt by delegating to the underlying VM.
func (m *Machine) WriteAt(p []byte, off int64) (int, error) {
	n, _, err := m.vm.writeAt(p, uint64(off))
	return n, err
}

// AllocateMemory maps a fresh RAM region at the requested physical address.
func (m *Machine) AllocateMemory(base, size uint64) (MemoryRegion, error) {
	if size == 0 {
		return nil, fmt.Errorf("ccvm: allocation size must be non-zero")
	}
	return mappedRegion{region: m.vm.registerRam(base, rawRegion(make([]byte, size)))}, nil
}

// SetPC updates the program counter.
func (m *Machine) SetPC(pc uint64) {
	m.vm.pc = pc
}

// PC reads the current program counter.
func (m *Machine) PC() uint64 {
	return m.vm.pc
}

// SetRegister writes the integer register at idx (0-31).
func (m *Machine) SetRegister(idx int, value uint64) error {
	if idx < 0 || idx >= len(m.vm.reg) {
		return fmt.Errorf("ccvm: register index %d out of range", idx)
	}
	m.vm.reg[idx] = value
	return nil
}

// Register returns the value of the integer register at idx (0-31).
func (m *Machine) Register(idx int) (uint64, error) {
	if idx < 0 || idx >= len(m.vm.reg) {
		return 0, fmt.Errorf("ccvm: register index %d out of range", idx)
	}
	return m.vm.reg[idx], nil
}

// EnableStopOnZero causes a store to address zero to end execution with
// ErrStopOnZero.
func (m *Machine) EnableStopOnZero() {
	m.vm.stopOnZero = true
}

// Run steps the VM until either ErrStopOnZero is raised, the context is
// cancelled, or another error occurs. A small time.Sleep prevents a busy loop
// when the context repeatedly cancels quickly.
func (m *Machine) Run(ctx context.Context, batchCycles int64) error {
	if batchCycles <= 0 {
		batchCycles = 500000
	}

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		err := m.vm.Step(batchCycles)
		switch {
		case err == nil:
			continue
		case errors.Is(err, ErrStopOnZero):
			return ErrStopOnZero
		default:
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return err
			}
			time.Sleep(time.Millisecond)
			return err
		}
	}
}

// Close is present for symmetry with other hypervisors; nothing to release yet.
func (m *Machine) Close() error {
	return nil
}
