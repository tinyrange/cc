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
	t.Logf("Loaded kernel at 0x%x, size=%d (0x%x)", kernelBase, len(kernelData), len(kernelData))

	// Verify kernel was loaded correctly at a test address
	testAddr := uint64(0x80c06ad8)
	testOffset := testAddr - kernelBase
	if testOffset < uint64(len(kernelData)) {
		expectedVal := uint16(kernelData[testOffset]) | uint16(kernelData[testOffset+1])<<8
		actualVal, err := m.Bus.Read16(testAddr)
		t.Logf("Kernel verification at 0x%x (offset 0x%x): kernel=0x%04x, bus=0x%04x, err=%v",
			testAddr, testOffset, expectedVal, actualVal, err)
	}

	// Get kernel image_size from header (offset 0x10, 64-bit little-endian)
	kernelImageSize := uint64(kernelData[0x10]) | uint64(kernelData[0x11])<<8 |
		uint64(kernelData[0x12])<<16 | uint64(kernelData[0x13])<<24 |
		uint64(kernelData[0x14])<<32 | uint64(kernelData[0x15])<<40 |
		uint64(kernelData[0x16])<<48 | uint64(kernelData[0x17])<<56
	t.Logf("Kernel image_size from header: 0x%x (%d bytes)", kernelImageSize, kernelImageSize)

	// Generate and load FDT with memory reservations for kernel
	cmdline := "console=hvc0 earlycon"
	dtbBase := uint64(0x82000000)
	reservations := []MemoryReservation{
		{Address: kernelBase, Size: kernelImageSize}, // Reserve kernel memory
		{Address: dtbBase, Size: 0x10000},            // Reserve DTB (64KB should be enough)
	}
	fdt := GenerateFDTWithReservations(m, cmdline, reservations)
	t.Logf("Generated FDT with reservations: kernel=[0x%x, 0x%x), dtb=[0x%x, 0x%x)",
		kernelBase, kernelBase+kernelImageSize, dtbBase, dtbBase+0x10000)
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

	// Skip to instruction 190000 (before trap at ~190093)
	t.Log("Fast forwarding to instruction 190000...")
	for i := 0; i < 190000; i++ {
		if err := m.Step(); err != nil {
			t.Logf("Error at insn %d: %v", i, err)
			break
		}
	}

	// Track function calls and returns
	t.Log("Tracing from instruction 190000:")
	callStack := []uint64{} // Track return addresses
	traceEveryInsn := false
	for i := 190000; i < 195000; i++ {
		// Trace when PC changes from BSS loop
		inBSSLoop := m.CPU.PC >= 0x8020110c && m.CPU.PC <= 0x80201116
		if !inBSSLoop || i == 190000 || i == 191820 {
			traceEveryInsn = true
		} else {
			traceEveryInsn = false
		}

		if traceEveryInsn {
			insn2, _ := m.Bus.Read32(m.CPU.PC)
			// a3=x13, a4=x14 - BSS loop pointers
			t.Logf("  [TRACE] insn %d: PC=0x%x insn=0x%08x a3=0x%x a4=0x%x",
				i, m.CPU.PC, insn2, m.CPU.X[13], m.CPU.X[14])
		}
		pc := m.CPU.PC
		insn, _ := m.Bus.Read32(pc)
		oldRA := m.CPU.X[1]
		oldSP := m.CPU.X[2]
		oldScause := m.CPU.Scause

		// Decode instruction to detect jalr/jal
		opcode := insn & 0x7f
		isCompressed := (insn & 0x3) != 0x3

		// Check for c.jr, c.jalr (compressed returns/calls)
		if isCompressed {
			c := uint16(insn & 0xffff)
			funct4 := (c >> 12) & 0xf
			rd := (c >> 7) & 0x1f
			rs2 := (c >> 2) & 0x1f

			if funct4 == 0x8 && rs2 == 0 && rd != 0 {
				// c.jr rs1 - return
				t.Logf("  insn %d: PC=0x%x c.jr x%d (target=0x%x) - RETURN", i, pc, rd, m.CPU.X[rd])
			} else if funct4 == 0x9 && rs2 == 0 && rd != 0 {
				// c.jalr rs1 - call
				t.Logf("  insn %d: PC=0x%x c.jalr x%d (target=0x%x, ra will be 0x%x) - CALL", i, pc, rd, m.CPU.X[rd], pc+2)
				callStack = append(callStack, pc+2)
			}
		} else if opcode == 0x67 {
			// JALR rd, rs1, imm
			rd := (insn >> 7) & 0x1f
			rs1 := (insn >> 15) & 0x1f
			imm := int32(insn) >> 20
			target := uint64(int64(m.CPU.X[rs1]) + int64(imm))

			if rd == 0 {
				// jalr zero, rs1, imm - pure jump/return
				t.Logf("  insn %d: PC=0x%x jalr x0, x%d, %d (target=0x%x) - RETURN/JUMP", i, pc, rs1, imm, target)
			} else if rd == 1 {
				// jalr ra, rs1, imm - call
				t.Logf("  insn %d: PC=0x%x jalr ra, x%d, %d (target=0x%x, ra will be 0x%x) - CALL", i, pc, rs1, imm, target, pc+4)
				callStack = append(callStack, pc+4)
			}
		} else if opcode == 0x6f {
			// JAL rd, imm - jump and link
			rd := (insn >> 7) & 0x1f
			if rd == 1 {
				// Extract J-type immediate
				imm20 := (insn >> 31) & 1
				imm101 := (insn >> 21) & 0x3ff
				imm11 := (insn >> 20) & 1
				imm1912 := (insn >> 12) & 0xff
				offset := int32((imm20<<20)|(imm1912<<12)|(imm11<<11)|(imm101<<1)) << 11 >> 11
				target := uint64(int64(pc) + int64(offset))
				t.Logf("  insn %d: PC=0x%x jal ra, 0x%x (ra will be 0x%x) - CALL", i, pc, target, pc+4)
				callStack = append(callStack, pc+4)
			}
		}

		// Also detect when RA is loaded from stack (c.ldsp ra, offset(sp))
		if isCompressed {
			c := uint16(insn & 0xffff)
			// c.ldsp: 011 uimm[5] rd uimm[4:3|8:6] 10
			if (c & 0xe003) == 0x6002 {
				rd := (c >> 7) & 0x1f
				if rd == 1 {
					// Loading ra from stack
					uimm5 := (c >> 12) & 1
					uimm43 := (c >> 5) & 3
					uimm86 := (c >> 2) & 7
					offset := (uimm5 << 5) | (uimm43 << 3) | (uimm86 << 6)
					addr := m.CPU.X[2] + uint64(offset)
					val, _ := m.Bus.Read64(addr)
					t.Logf("  insn %d: PC=0x%x c.ldsp ra, %d(sp) - LOAD RA from [0x%x] = 0x%x", i, pc, offset, addr, val)
					// If loading 0, dump the stack for debugging
					if val == 0 {
						t.Logf("  *** LOADING 0 INTO RA! Dumping stack around 0x%x:", addr)
						for off := int64(-64); off <= 64; off += 8 {
							a := addr + uint64(off)
							v, _ := m.Bus.Read64(a)
							marker := "  "
							if off == 0 {
								marker = "=>"
							}
							t.Logf("    %s [0x%x] = 0x%016x", marker, a, v)
						}
					}
				}
			}
		}

		if err := m.Step(); err != nil {
			t.Logf("Error at insn %d: %v", i, err)
			break
		}

		// Detect when scause changes (trap taken)
		if m.CPU.Scause != oldScause && m.CPU.Scause != 0 {
			t.Logf("*** TRAP at insn %d: scause=0x%x, from PC=0x%x to 0x%x, sepc=0x%x",
				i, m.CPU.Scause, pc, m.CPU.PC, m.CPU.Sepc)
		}

		// Show when RA changes to a surprising value
		if m.CPU.X[1] != oldRA {
			if m.CPU.X[1] == 0 {
				t.Logf("  insn %d: RA BECAME 0 (was 0x%x) at PC=0x%x", i, oldRA, pc)
			}
		}

		// Show when SP changes significantly
		if m.CPU.X[2] != oldSP {
			diff := int64(m.CPU.X[2]) - int64(oldSP)
			// Log big changes, or any changes in the critical range
			if (diff < -64 || diff > 64) || (i >= 192300 && i <= 192770) {
				t.Logf("  insn %d: SP changed by %d (0x%x -> 0x%x) at PC=0x%x", i, diff, oldSP, m.CPU.X[2], pc)
			}
		}

		// Stop when we hit the trap handler
		if m.CPU.PC == m.CPU.Stvec && m.CPU.Stvec != 0 {
			t.Logf("  Reached trap handler (stvec=0x%x), stopping trace", m.CPU.Stvec)
			break
		}
	}

	// Add a write watchpoint for the problematic address
	watchAddr := uint64(0x80c06ad8)
	watchSize := uint64(8)
	watchFound := false

	// Wrap the bus to detect writes
	originalRAM := m.Bus.RAM
	type writeRecord struct {
		pc    uint64
		addr  uint64
		value uint64
		size  int
		insn  int64
	}
	var writeHistory []writeRecord

	// Check memory before running
	t.Log("Checking memory at watch address before main loop:")
	val1, _ := m.Bus.Read32(watchAddr)
	val2, _ := m.Bus.Read32(watchAddr + 4)
	t.Logf("  0x%x: 0x%08x 0x%08x", watchAddr, val1, val2)

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
	var enteredTrapHandler bool

	// Track last N instructions for debugging
	type pcRecord struct {
		pc     uint64
		insn   uint32
		scause uint64
		stval  uint64
	}
	pcHistory := make([]pcRecord, 0, 100)

	// Track function calls in main loop (for debugging kernel memory issues)
	mainLoopCallStack := []uint64{}
	mainLoopTraceStart := int64(44400) // Start detailed trace before kernel clears its own code
	mainLoopTraceEnabled := false      // Set to true to enable detailed tracing
	_ = mainLoopCallStack              // May not be used depending on trace

	// Helper to check if address range overlaps watch range
	checkWatch := func(addr uint64, size int) bool {
		return addr < watchAddr+watchSize && addr+uint64(size) > watchAddr
	}
	_ = checkWatch      // Silence unused warning if not used below
	_ = originalRAM     // Silence unused warning
	_ = watchFound      // Silence unused warning
	_ = writeHistory    // Silence unused warning

	// Store original value at watch address
	origWatchVal, _ := m.Bus.Read64(watchAddr)
	t.Logf("Original value at watch address 0x%x: 0x%016x", watchAddr, origWatchVal)

	for insnCount = 0; insnCount < maxInsns; insnCount++ {
		// Update timer occasionally
		if insnCount%1000 == 0 {
			m.CLINT.Tick()
		}

		// Check if watch address was modified
		if !watchFound {
			currVal, _ := m.Bus.Read64(watchAddr)
			if currVal != origWatchVal {
				t.Logf("*** WATCH ADDRESS 0x%x MODIFIED at insn %d ***", watchAddr, insnCount)
				t.Logf("    Old value: 0x%016x, New value: 0x%016x", origWatchVal, currVal)
				t.Logf("    PC: 0x%x, RA: 0x%x, SP: 0x%x", m.CPU.PC, m.CPU.X[1], m.CPU.X[2])
				t.Logf("    x5(t0): 0x%x, x10(a0): 0x%x, x11(a1): 0x%x, x13(a3): 0x%x",
					m.CPU.X[5], m.CPU.X[10], m.CPU.X[11], m.CPU.X[13])
				t.Log("    Last 50 instructions:")
				start := len(pcHistory) - 50
				if start < 0 {
					start = 0
				}
				for i := start; i < len(pcHistory); i++ {
					rec := pcHistory[i]
					t.Logf("      0x%x: 0x%08x", rec.pc, rec.insn)
				}
				watchFound = true
				origWatchVal = currVal // Track further changes
			}
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

		// Detailed call/return tracing before the watch triggers
		if mainLoopTraceEnabled && insnCount >= mainLoopTraceStart && insnCount < mainLoopTraceStart+500 {
			pc := m.CPU.PC
			insn, _ := m.Bus.Read32(pc)
			opcode := insn & 0x7f
			isCompressed := (insn & 0x3) != 0x3

			if isCompressed {
				c := uint16(insn & 0xffff)
				funct4 := (c >> 12) & 0xf
				rd := (c >> 7) & 0x1f
				rs2 := (c >> 2) & 0x1f

				if funct4 == 0x8 && rs2 == 0 && rd != 0 {
					t.Logf("  [MAIN %d] PC=0x%x c.jr x%d (to 0x%x) - RETURN", insnCount, pc, rd, m.CPU.X[rd])
				} else if funct4 == 0x9 && rs2 == 0 && rd != 0 {
					t.Logf("  [MAIN %d] PC=0x%x c.jalr x%d (to 0x%x, ra=0x%x) - CALL", insnCount, pc, rd, m.CPU.X[rd], pc+2)
					mainLoopCallStack = append(mainLoopCallStack, pc+2)
				}
			} else if opcode == 0x67 {
				rd := (insn >> 7) & 0x1f
				rs1 := (insn >> 15) & 0x1f
				imm := int32(insn) >> 20
				target := uint64(int64(m.CPU.X[rs1]) + int64(imm))

				if rd == 0 {
					t.Logf("  [MAIN %d] PC=0x%x jalr x0, x%d, %d (to 0x%x) - RETURN", insnCount, pc, rs1, imm, target)
				} else if rd == 1 {
					t.Logf("  [MAIN %d] PC=0x%x jalr ra, x%d, %d (to 0x%x, ra=0x%x) - CALL", insnCount, pc, rs1, imm, target, pc+4)
					mainLoopCallStack = append(mainLoopCallStack, pc+4)
				}
			} else if opcode == 0x6f {
				rd := (insn >> 7) & 0x1f
				if rd == 1 {
					imm20 := (insn >> 31) & 1
					imm101 := (insn >> 21) & 0x3ff
					imm11 := (insn >> 20) & 1
					imm1912 := (insn >> 12) & 0xff
					offset := int32((imm20<<20)|(imm1912<<12)|(imm11<<11)|(imm101<<1)) << 11 >> 11
					target := uint64(int64(pc) + int64(offset))
					t.Logf("  [MAIN %d] PC=0x%x jal ra, 0x%x (ra=0x%x) - CALL", insnCount, pc, target, pc+4)
					mainLoopCallStack = append(mainLoopCallStack, pc+4)
				}
			}

			// Show a0 when it might be setting up a memset call
			if m.CPU.X[10] >= 0x80c06000 && m.CPU.X[10] <= 0x80c07000 {
				t.Logf("  [MAIN %d] PC=0x%x a0=0x%x a1=0x%x a2=0x%x (potential memset range)",
					insnCount, pc, m.CPU.X[10], m.CPU.X[11], m.CPU.X[12])
			}

			// Show when x15 is in the interesting range (before memset call)
			if insnCount < 44520 && m.CPU.X[15] >= 0x80c06000 && m.CPU.X[15] <= 0x80c07000 {
				t.Logf("  [MAIN %d] PC=0x%x x15=0x%x insn=0x%08x",
					insnCount, pc, m.CPU.X[15], insn)
			}
		}

		// Record PC history
		oldPC := m.CPU.PC
		oldScause := m.CPU.Scause

		// Get current instruction for history
		if len(pcHistory) >= 100 {
			pcHistory = pcHistory[1:]
		}
		if insn, err := m.Bus.Read32(oldPC); err == nil {
			pcHistory = append(pcHistory, pcRecord{pc: oldPC, insn: insn, scause: m.CPU.Scause, stval: m.CPU.Stval})
		}

		// Detect first entry into trap handler (stvec area)
		stvec := m.CPU.Stvec
		if !enteredTrapHandler && stvec != 0 && (oldPC == stvec || oldPC == stvec+4) {
			enteredTrapHandler = true
			t.Logf("FIRST ENTRY into trap handler at insn %d: PC=0x%x, stvec=0x%x", insnCount, oldPC, stvec)
			t.Logf("  scause=0x%x, sepc=0x%x, stval=0x%x", m.CPU.Scause, m.CPU.Sepc, m.CPU.Stval)
			t.Logf("  a0=0x%x, a1=0x%x, ra=0x%x, sp=0x%x", m.CPU.X[10], m.CPU.X[11], m.CPU.X[1], m.CPU.X[2])
			t.Log("  Last 30 instructions before trap handler entry:")
			start := len(pcHistory) - 30
			if start < 0 {
				start = 0
			}
			for i := start; i < len(pcHistory); i++ {
				rec := pcHistory[i]
				t.Logf("    0x%x: 0x%08x (scause=0x%x)", rec.pc, rec.insn, rec.scause)
			}
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

	// Show memory around kernel return address (0x80201148)
	t.Log("\nKernel code at return address 0x80201148:")
	for i := 0; i < 20; i++ {
		addr := uint64(0x80201140 + i*4)
		val, err := m.Bus.Read32(addr)
		if err != nil {
			break
		}
		t.Logf("  0x%08x: %08x", addr, val)
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
