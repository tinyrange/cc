package rv64

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/linux/kernel"
)

func (cpu *CPU) DumpRegisters() string {
	var buf bytes.Buffer

	// ABI register names
	regNames := []string{
		"zero", "ra", "sp", "gp", "tp", "t0", "t1", "t2",
		"s0/fp", "s1", "a0", "a1", "a2", "a3", "a4", "a5",
		"a6", "a7", "s2", "s3", "s4", "s5", "s6", "s7",
		"s8", "s9", "s10", "s11", "t3", "t4", "t5", "t6",
	}

	fmt.Fprintf(&buf, "PC:   0x%016x\n", cpu.PC)
	fmt.Fprintf(&buf, "Priv: %d (", cpu.Priv)
	switch cpu.Priv {
	case PrivMachine:
		buf.WriteString("M-mode)")
	case PrivSupervisor:
		buf.WriteString("S-mode)")
	case PrivUser:
		buf.WriteString("U-mode)")
	default:
		buf.WriteString("unknown)")
	}
	buf.WriteString("\n\n")

	// Integer registers
	fmt.Fprintf(&buf, "Integer Registers:\n")
	for i := 0; i < 32; i += 4 {
		for j := 0; j < 4; j++ {
			reg := i + j
			fmt.Fprintf(&buf, "x%-2d(%-5s) = 0x%016x  ", reg, regNames[reg], cpu.X[reg])
		}
		buf.WriteString("\n")
	}

	// Key CSRs
	fmt.Fprintf(&buf, "\nKey CSRs:\n")
	fmt.Fprintf(&buf, "mstatus:  0x%016x  mtvec:    0x%016x\n", cpu.Mstatus, cpu.Mtvec)
	fmt.Fprintf(&buf, "mepc:     0x%016x  mcause:   0x%016x\n", cpu.Mepc, cpu.Mcause)
	fmt.Fprintf(&buf, "mtval:    0x%016x  mie:      0x%016x\n", cpu.Mtval, cpu.Mie)
	fmt.Fprintf(&buf, "mip:      0x%016x  mideleg:  0x%016x\n", cpu.Mip, cpu.Mideleg)
	fmt.Fprintf(&buf, "medeleg:  0x%016x  mscratch: 0x%016x\n", cpu.Medeleg, cpu.Mscratch)
	fmt.Fprintf(&buf, "sstatus:  0x%016x  stvec:    0x%016x\n", cpu.readSstatus(), cpu.Stvec)
	fmt.Fprintf(&buf, "sepc:     0x%016x  scause:   0x%016x\n", cpu.Sepc, cpu.Scause)
	fmt.Fprintf(&buf, "stval:    0x%016x  satp:     0x%016x\n", cpu.Stval, cpu.Satp)
	fmt.Fprintf(&buf, "sscratch: 0x%016x\n", cpu.Sscratch)
	fmt.Fprintf(&buf, "cycle:    %d  instret:  %d\n", cpu.Cycle, cpu.Instret)

	return buf.String()
}

func TestLinuxKernelBoot(t *testing.T) {
	// Load kernel
	t.Log("Loading RISC-V kernel...")
	k, err := kernel.LoadForArchitecture(hv.ArchitectureRISCV64)
	if err != nil {
		t.Skipf("Could not load kernel (may need network): %v", err)
	}

	kf, err := k.Open()
	if err != nil {
		t.Fatalf("Open kernel: %v", err)
	}

	kSize, err := k.Size()
	if err != nil {
		t.Fatalf("Kernel size: %v", err)
	}
	t.Logf("Kernel size: %d bytes", kSize)

	// Read kernel bytes
	kernelData := make([]byte, kSize)
	n, err := kf.ReadAt(kernelData, 0)
	if err != nil {
		t.Fatalf("Read kernel: %v", err)
	}
	t.Logf("Read %d kernel bytes", n)

	// Check if gzip compressed and decompress
	if len(kernelData) >= 2 && kernelData[0] == 0x1f && kernelData[1] == 0x8b {
		t.Log("Kernel is gzip compressed, decompressing...")
		gzReader, err := gzip.NewReader(bytes.NewReader(kernelData))
		if err != nil {
			t.Fatalf("Create gzip reader: %v", err)
		}
		kernelData, err = io.ReadAll(gzReader)
		gzReader.Close()
		if err != nil {
			t.Fatalf("Decompress kernel: %v", err)
		}
		t.Logf("Decompressed kernel to %d bytes", len(kernelData))
	}

	// Create output buffer
	consoleOutput := &bytes.Buffer{}
	sbiDebug := &bytes.Buffer{}

	// Create machine with 256MB RAM
	ramSize := uint64(256 * 1024 * 1024)
	m := NewMachine(ramSize, consoleOutput, nil)
	m.DebugOutput = sbiDebug
	m.CPU.DebugLog = sbiDebug

	// Load kernel at 0x80200000 (standard RISC-V Linux load address)
	kernelBase := uint64(0x80200000)
	if err := m.LoadBytes(kernelBase, kernelData); err != nil {
		t.Fatalf("Load kernel: %v", err)
	}
	t.Logf("Loaded kernel at 0x%x", kernelBase)

	// Generate and load FDT
	cmdline := "console=ttyS0 earlycon=sbi"
	fdt := GenerateFDT(m, cmdline)
	dtbBase := uint64(0x82000000)
	if err := m.LoadBytes(dtbBase, fdt); err != nil {
		t.Fatalf("Load FDT: %v", err)
	}
	t.Logf("Loaded FDT (%d bytes) at 0x%x", len(fdt), dtbBase)

	// Dump FDT header for debugging
	if len(fdt) >= 40 {
		t.Logf("FDT header: magic=0x%08x totalsize=%d struct_off=%d strings_off=%d",
			uint32(fdt[0])<<24|uint32(fdt[1])<<16|uint32(fdt[2])<<8|uint32(fdt[3]),
			uint32(fdt[4])<<24|uint32(fdt[5])<<16|uint32(fdt[6])<<8|uint32(fdt[7]),
			uint32(fdt[8])<<24|uint32(fdt[9])<<16|uint32(fdt[10])<<8|uint32(fdt[11]),
			uint32(fdt[12])<<24|uint32(fdt[13])<<16|uint32(fdt[14])<<8|uint32(fdt[15]))
	}

	// Set up CPU state for Linux boot with SBI support (MMU disabled, let kernel set it up)
	m.SetupForLinux(0, dtbBase, kernelBase)

	t.Logf("Starting execution at PC=0x%x with a0=%d, a1=0x%x, Priv=%d, SATP=0x%x",
		m.CPU.PC, m.CPU.X[10], m.CPU.X[11], m.CPU.Priv, m.CPU.Satp)

	// Trace first 50 instructions with CSR info
	t.Log("First 50 instructions:")
	for i := 0; i < 50; i++ {
		pc := m.CPU.PC
		insn, _ := m.Bus.Read32(pc)
		oldStvec := m.CPU.Stvec
		oldSatp := m.CPU.Satp
		if err := m.Step(); err != nil {
			t.Logf("  %d: PC=0x%x insn=0x%08x -> error: %v", i, pc, insn, err)
			break
		}
		extra := ""
		if m.CPU.Stvec != oldStvec {
			extra = fmt.Sprintf(" stvec=0x%x", m.CPU.Stvec)
		}
		if m.CPU.Satp != oldSatp {
			extra = fmt.Sprintf(" satp=0x%x", m.CPU.Satp)
		}
		t.Logf("  %d: PC=0x%x insn=0x%08x -> PC=0x%x%s", i, pc, insn, m.CPU.PC, extra)
	}

	// Run for limited instructions
	maxInsns := int64(10000000) // 10M instructions
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	startTime := time.Now()
	var stepErr error
	var insnCount int64
	var lastPC uint64
	var loopCount int
	var lastSatp uint64
	var lastRA uint64 = m.CPU.X[1]

	// Track last N instructions for debugging
	type pcRecord struct {
		pc   uint64
		insn uint32
	}
	pcHistory := make([]pcRecord, 0, 100)

	for insnCount = 0; insnCount < maxInsns; insnCount++ {
		// Update timer occasionally
		if insnCount%1000 == 0 {
			m.CLINT.Tick()
		}

		// Detect infinite loops (but allow some repetition for spin loops)
		if m.CPU.PC == lastPC {
			loopCount++
			if loopCount > 1000 {
				t.Logf("Detected infinite loop at PC=0x%x after %d instructions", m.CPU.PC, insnCount)
				break
			}
		} else {
			loopCount = 0
			lastPC = m.CPU.PC
		}

		// Record PC history
		oldPC := m.CPU.PC
		oldScause := m.CPU.Scause

		// Get current instruction for history
		if len(pcHistory) >= 100 {
			pcHistory = pcHistory[1:]
		}
		if insn, err := m.Bus.Read32(oldPC); err == nil {
			pcHistory = append(pcHistory, pcRecord{pc: oldPC, insn: insn})
		}

		stepErr = m.Step()

		// Check for SATP change (MMU being enabled)
		if m.CPU.Satp != lastSatp {
			t.Logf("SATP changed at insn %d: 0x%x -> 0x%x, PC=0x%x",
				insnCount, lastSatp, m.CPU.Satp, m.CPU.PC)
			lastSatp = m.CPU.Satp
		}

		// Check for RA becoming 0 (likely indicates where the NULL return originates)
		if m.CPU.X[1] != lastRA {
			if m.CPU.X[1] == 0 && lastRA != 0 {
				t.Logf("RA became 0 at insn %d: PC=0x%x (was RA=0x%x)",
					insnCount, m.CPU.PC, lastRA)
			}
			lastRA = m.CPU.X[1]
		}

		// Check for S-mode trap (which is what Linux uses)
		if m.CPU.Scause != oldScause && m.CPU.Scause != 0 {
			t.Logf("S-mode TRAP at insn %d: PC=0x%x -> 0x%x, scause=0x%x, stval=0x%x, sepc=0x%x",
				insnCount, oldPC, m.CPU.PC, m.CPU.Scause, m.CPU.Stval, m.CPU.Sepc)
			// Show instruction at trap point
			if insn, err := m.Bus.Read32(m.CPU.Sepc); err == nil {
				t.Logf("  Instruction at sepc: 0x%08x", insn)
			}
			// Print last 20 instructions
			t.Log("  Last 20 instructions before trap:")
			start := len(pcHistory) - 20
			if start < 0 {
				start = 0
			}
			for i := start; i < len(pcHistory); i++ {
				rec := pcHistory[i]
				t.Logf("    0x%x: 0x%08x", rec.pc, rec.insn)
			}
		}

		if stepErr != nil {
			break
		}

		if ctx.Err() != nil {
			stepErr = ctx.Err()
			break
		}
	}

	elapsed := time.Since(startTime)

	t.Logf("Executed %d instructions in %v (%.2f MIPS)", insnCount, elapsed, float64(insnCount)/elapsed.Seconds()/1e6)

	if stepErr != nil && stepErr != ErrHalt {
		t.Logf("Execution stopped with error: %v", stepErr)
	}

	// Dump registers
	t.Log("\n" + m.CPU.DumpRegisters())

	// Show console output
	if consoleOutput.Len() > 0 {
		t.Logf("Console output (%d bytes):\n%s", consoleOutput.Len(), consoleOutput.String())
	} else {
		t.Log("No console output")
	}

	// Show SBI debug output
	if sbiDebug.Len() > 0 {
		t.Logf("SBI calls (%d bytes):\n%s", sbiDebug.Len(), sbiDebug.String())
	} else {
		t.Log("No SBI calls")
	}

	// Show some memory around PC
	t.Log("\nMemory around PC:")
	pc := m.CPU.PC
	for addr := pc - 16; addr < pc+32; addr += 4 {
		val, err := m.Bus.Read32(addr)
		if err != nil {
			continue
		}
		marker := "  "
		if addr == pc {
			marker = "=>"
		}
		t.Logf("%s 0x%08x: %08x", marker, addr, val)
	}

	// Disassemble kernel entry point
	t.Log("\nKernel header (PE format?):")
	for i := 0; i < 20; i++ {
		addr := kernelBase + uint64(i*4)
		val, err := m.Bus.Read32(addr)
		if err != nil {
			break
		}
		t.Logf("0x%08x: %08x", addr, val)
	}

	// Check for PE header and show text_offset
	if kernelData[0] == 0x4d && kernelData[1] == 0x5a { // MZ
		t.Log("Kernel has PE/COFF header (EFI stub)")
		textOffset := uint64(kernelData[8]) | uint64(kernelData[9])<<8 |
			uint64(kernelData[10])<<16 | uint64(kernelData[11])<<24
		t.Logf("text_offset from header: 0x%x", textOffset)
	}

	// If there was a trap, inspect the faulting instruction
	if m.CPU.Sepc != 0 {
		t.Logf("\nFaulting instruction at sepc=0x%x:", m.CPU.Sepc)
		for i := -4; i <= 8; i += 2 {
			addr := m.CPU.Sepc + uint64(i*2)
			val, err := m.Bus.Read32(addr)
			if err != nil {
				continue
			}
			marker := "  "
			if addr == m.CPU.Sepc {
				marker = "=>"
			}
			// Show both 32-bit word and 16-bit compressed view
			val16, _ := m.Bus.Read16(addr)
			t.Logf("%s 0x%08x: %08x (16-bit: %04x)", marker, addr, val, val16)
		}

		// Check kernel data directly at the faulting offset
		offset := m.CPU.Sepc - kernelBase
		if offset < uint64(len(kernelData)) {
			t.Logf("\nKernel data at offset 0x%x (sepc-kernelBase):", offset)
			start := int(offset) - 8
			if start < 0 {
				start = 0
			}
			end := int(offset) + 16
			if end > len(kernelData) {
				end = len(kernelData)
			}
			for i := start; i < end; i += 2 {
				val := uint16(kernelData[i]) | uint16(kernelData[i+1])<<8
				marker := "  "
				if uint64(i) == offset {
					marker = "=>"
				}
				t.Logf("%s kernel[0x%x]: %04x", marker, i, val)
			}
		}
	}
}
