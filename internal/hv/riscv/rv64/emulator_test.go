package rv64

import (
	"bytes"
	"context"
	"testing"
	"time"
)

func TestBasicExecution(t *testing.T) {
	// Create a machine with 1MB RAM
	output := &bytes.Buffer{}
	m := NewMachine(1024*1024, output, nil)

	// Simple program that writes "Hi" to UART and halts
	// lui a0, 0x10000    # UART base
	// li a1, 'H'
	// sb a1, 0(a0)
	// li a1, 'i'
	// sb a1, 0(a0)
	// li a1, '\n'
	// sb a1, 0(a0)
	// # Write to address 0 to halt
	// li a0, 0
	// sw zero, 0(a0)

	code := []uint32{
		0x10000537, // lui a0, 0x10000
		0x04800593, // li a1, 'H' (addi a1, zero, 0x48)
		0x00b50023, // sb a1, 0(a0)
		0x06900593, // li a1, 'i' (addi a1, zero, 0x69)
		0x00b50023, // sb a1, 0(a0)
		0x00a00593, // li a1, '\n' (addi a1, zero, 0x0a)
		0x00b50023, // sb a1, 0(a0)
		0x00000513, // li a0, 0
		0x00052023, // sw zero, 0(a0)
	}

	// Load program at RAM base
	for i, insn := range code {
		addr := RAMBase + uint64(i*4)
		m.Bus.Write32(addr, insn)
	}

	// Set PC to RAM base
	m.SetPC(RAMBase)

	// Enable stop on zero
	m.SetStopOnZero(true)

	// Run
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := m.Run(ctx, 100)
	if err != ErrHalt {
		t.Fatalf("expected ErrHalt, got %v", err)
	}

	// Check output
	expected := "Hi\n"
	if output.String() != expected {
		t.Fatalf("expected output %q, got %q", expected, output.String())
	}
}

func TestALUOperations(t *testing.T) {
	output := &bytes.Buffer{}
	m := NewMachine(1024*1024, output, nil)

	// Test ADD, SUB, AND, OR, XOR
	// li a0, 10
	// li a1, 3
	// add a2, a0, a1    # a2 = 13
	// sub a3, a0, a1    # a3 = 7
	// and a4, a0, a1    # a4 = 2
	// or a5, a0, a1     # a5 = 11
	// xor a6, a0, a1    # a6 = 9
	// # Halt
	// li t0, 0
	// sw zero, 0(t0)

	code := []uint32{
		0x00a00513, // li a0, 10
		0x00300593, // li a1, 3
		0x00b50633, // add a2, a0, a1
		0x40b506b3, // sub a3, a0, a1
		0x00b57733, // and a4, a0, a1
		0x00b567b3, // or a5, a0, a1
		0x00b54833, // xor a6, a0, a1
		0x00000293, // li t0, 0
		0x0002a023, // sw zero, 0(t0)
	}

	for i, insn := range code {
		addr := RAMBase + uint64(i*4)
		m.Bus.Write32(addr, insn)
	}

	m.SetPC(RAMBase)
	m.SetStopOnZero(true)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := m.Run(ctx, 100)
	if err != ErrHalt {
		t.Fatalf("expected ErrHalt, got %v", err)
	}

	// Check register values
	if m.CPU.X[12] != 13 {
		t.Errorf("a2 (add): expected 13, got %d", m.CPU.X[12])
	}
	if m.CPU.X[13] != 7 {
		t.Errorf("a3 (sub): expected 7, got %d", m.CPU.X[13])
	}
	if m.CPU.X[14] != 2 {
		t.Errorf("a4 (and): expected 2, got %d", m.CPU.X[14])
	}
	if m.CPU.X[15] != 11 {
		t.Errorf("a5 (or): expected 11, got %d", m.CPU.X[15])
	}
	if m.CPU.X[16] != 9 {
		t.Errorf("a6 (xor): expected 9, got %d", m.CPU.X[16])
	}
}

func TestBranches(t *testing.T) {
	output := &bytes.Buffer{}
	m := NewMachine(1024*1024, output, nil)

	// Test BEQ branch
	// li a0, 5
	// li a1, 5
	// li a2, 0       # result
	// beq a0, a1, equal
	// li a2, 1       # should be skipped
	// equal:
	// addi a2, a2, 10 # a2 = 10
	// # Halt
	// li t0, 0
	// sw zero, 0(t0)

	code := []uint32{
		0x00500513, // li a0, 5
		0x00500593, // li a1, 5
		0x00000613, // li a2, 0
		0x00b50463, // beq a0, a1, +8 (skip next insn)
		0x00100613, // li a2, 1 (skipped)
		0x00a60613, // addi a2, a2, 10
		0x00000293, // li t0, 0
		0x0002a023, // sw zero, 0(t0)
	}

	for i, insn := range code {
		addr := RAMBase + uint64(i*4)
		m.Bus.Write32(addr, insn)
	}

	m.SetPC(RAMBase)
	m.SetStopOnZero(true)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := m.Run(ctx, 100)
	if err != ErrHalt {
		t.Fatalf("expected ErrHalt, got %v", err)
	}

	if m.CPU.X[12] != 10 {
		t.Errorf("a2: expected 10, got %d", m.CPU.X[12])
	}
}

func TestMultiplyDivide(t *testing.T) {
	output := &bytes.Buffer{}
	m := NewMachine(1024*1024, output, nil)

	// Test MUL, DIV, REM
	code := []uint32{
		0x00700513, // li a0, 7
		0x00300593, // li a1, 3
		0x02b50633, // mul a2, a0, a1 (7*3=21)
		0x02b546b3, // div a3, a0, a1 (7/3=2)
		0x02b56733, // rem a4, a0, a1 (7%3=1)
		0x00000293, // li t0, 0
		0x0002a023, // sw zero, 0(t0)
	}

	for i, insn := range code {
		addr := RAMBase + uint64(i*4)
		m.Bus.Write32(addr, insn)
	}

	m.SetPC(RAMBase)
	m.SetStopOnZero(true)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := m.Run(ctx, 100)
	if err != ErrHalt {
		t.Fatalf("expected ErrHalt, got %v", err)
	}

	if m.CPU.X[12] != 21 {
		t.Errorf("a2 (mul): expected 21, got %d", m.CPU.X[12])
	}
	if m.CPU.X[13] != 2 {
		t.Errorf("a3 (div): expected 2, got %d", m.CPU.X[13])
	}
	if m.CPU.X[14] != 1 {
		t.Errorf("a4 (rem): expected 1, got %d", m.CPU.X[14])
	}
}

func TestSBICall(t *testing.T) {
	output := &bytes.Buffer{}
	m := NewMachine(4*1024, output, nil)
	m.SetupForLinux(0, 0, RAMBase)

	// Write a small program that makes an SBI putchar call
	// a7 = 1 (legacy putchar), a0 = 'H' (72)
	code := []uint32{
		0x04800513, // li a0, 72 ('H')
		0x00100893, // li a7, 1  (SBI_LEGACY_PUTCHAR)
		0x00000073, // ecall
		0x04900513, // li a0, 73 ('I')
		0x00000073, // ecall
		0x10500073, // wfi
	}

	for i, insn := range code {
		m.Bus.Write32(RAMBase+uint64(i*4), insn)
	}

	// Run a few steps
	for i := 0; i < 20; i++ {
		if err := m.Step(); err != nil {
			if err == ErrHalt {
				break
			}
			t.Logf("Step %d error: %v", i, err)
		}
		t.Logf("Step %d: PC=0x%x, a0=0x%x, a7=0x%x", i, m.CPU.PC, m.CPU.X[10], m.CPU.X[17])
	}

	t.Logf("Output: %q", output.String())
	if output.String() != "HI" {
		t.Errorf("Expected 'HI', got %q", output.String())
	}
}

func TestCompressedInstructions(t *testing.T) {
	output := &bytes.Buffer{}
	m := NewMachine(1024*1024, output, nil)

	// Test some compressed instructions
	// c.li a0, 5       (0x4515)
	// c.addi a0, 3     (0x050d) - this adds 3 to a0
	// c.mv a1, a0      (0x85aa)
	// # Halt using full instruction
	// li t0, 0
	// sw zero, 0(t0)

	// Write 16-bit and 32-bit instructions
	m.Bus.Write16(RAMBase+0, 0x4515)     // c.li a0, 5
	m.Bus.Write16(RAMBase+2, 0x050d)     // c.addi a0, 3
	m.Bus.Write16(RAMBase+4, 0x85aa)     // c.mv a1, a0
	m.Bus.Write32(RAMBase+6, 0x00000293) // li t0, 0
	m.Bus.Write32(RAMBase+10, 0x0002a023) // sw zero, 0(t0)

	m.SetPC(RAMBase)
	m.SetStopOnZero(true)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := m.Run(ctx, 100)
	if err != ErrHalt {
		t.Fatalf("expected ErrHalt, got %v", err)
	}

	if m.CPU.X[10] != 8 {
		t.Errorf("a0: expected 8, got %d", m.CPU.X[10])
	}
	if m.CPU.X[11] != 8 {
		t.Errorf("a1: expected 8, got %d", m.CPU.X[11])
	}
}

func TestFDTGeneration(t *testing.T) {
	m := NewMachine(64*1024*1024, nil, nil)
	fdt := GenerateFDT(m, "console=ttyS0")

	// Check magic number
	if len(fdt) < 4 {
		t.Fatal("FDT too short")
	}

	magic := uint32(fdt[0])<<24 | uint32(fdt[1])<<16 | uint32(fdt[2])<<8 | uint32(fdt[3])
	if magic != FDTMagic {
		t.Errorf("FDT magic: expected 0x%08x, got 0x%08x", FDTMagic, magic)
	}

	t.Logf("FDT size: %d bytes", len(fdt))
}
