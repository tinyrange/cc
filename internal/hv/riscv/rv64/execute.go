package rv64

import (
	"fmt"
)

// Opcode constants
const (
	OpLoad     = 0b0000011 // I-type loads
	OpLoadFP   = 0b0000111 // FP loads
	OpMiscMem  = 0b0001111 // FENCE
	OpOpImm    = 0b0010011 // I-type ALU
	OpAuipc    = 0b0010111 // U-type
	OpOpImm32  = 0b0011011 // I-type ALU 32-bit
	OpStore    = 0b0100011 // S-type stores
	OpStoreFP  = 0b0100111 // FP stores
	OpAMO      = 0b0101111 // Atomics
	OpOp       = 0b0110011 // R-type ALU
	OpLui      = 0b0110111 // U-type
	OpOp32     = 0b0111011 // R-type ALU 32-bit
	OpMadd     = 0b1000011 // FP fused multiply-add
	OpMsub     = 0b1000111 // FP fused multiply-sub
	OpNmsub    = 0b1001011 // FP negated fused multiply-sub
	OpNmadd    = 0b1001111 // FP negated fused multiply-add
	OpOpFP     = 0b1010011 // FP operations
	OpBranch   = 0b1100011 // B-type branches
	OpJalr     = 0b1100111 // I-type jump
	OpJal      = 0b1101111 // J-type jump
	OpSystem   = 0b1110011 // System instructions
)

// Instruction field extraction
func opcode(insn uint32) uint32   { return insn & 0x7f }
func rd(insn uint32) uint32       { return (insn >> 7) & 0x1f }
func funct3(insn uint32) uint32   { return (insn >> 12) & 0x7 }
func rs1(insn uint32) uint32      { return (insn >> 15) & 0x1f }
func rs2(insn uint32) uint32      { return (insn >> 20) & 0x1f }
func rs3(insn uint32) uint32      { return (insn >> 27) & 0x1f }
func funct7(insn uint32) uint32   { return (insn >> 25) & 0x7f }
func funct2(insn uint32) uint32   { return (insn >> 25) & 0x3 }

// Immediate extraction
func immI(insn uint32) int64 {
	return signExtend(uint64(insn>>20), 12)
}

func immS(insn uint32) int64 {
	imm := (insn >> 7) & 0x1f
	imm |= ((insn >> 25) & 0x7f) << 5
	return signExtend(uint64(imm), 12)
}

func immB(insn uint32) int64 {
	imm := ((insn >> 8) & 0xf) << 1
	imm |= ((insn >> 25) & 0x3f) << 5
	imm |= ((insn >> 7) & 0x1) << 11
	imm |= ((insn >> 31) & 0x1) << 12
	return signExtend(uint64(imm), 13)
}

func immU(insn uint32) int64 {
	return signExtend(uint64(insn&0xfffff000), 32)
}

func immJ(insn uint32) int64 {
	imm := ((insn >> 21) & 0x3ff) << 1
	imm |= ((insn >> 20) & 0x1) << 11
	imm |= ((insn >> 12) & 0xff) << 12
	imm |= ((insn >> 31) & 0x1) << 20
	return signExtend(uint64(imm), 21)
}

// shamt extracts the shift amount for 64-bit shifts
func shamt(insn uint32) uint32 {
	return (insn >> 20) & 0x3f
}

// shamt32 extracts the shift amount for 32-bit shifts
func shamt32(insn uint32) uint32 {
	return (insn >> 20) & 0x1f
}

// Execute executes a single instruction and returns the new PC
func (cpu *CPU) Execute(insn uint32) error {
	op := opcode(insn)

	switch op {
	case OpLui:
		return cpu.execLui(insn)
	case OpAuipc:
		return cpu.execAuipc(insn)
	case OpJal:
		return cpu.execJal(insn)
	case OpJalr:
		return cpu.execJalr(insn)
	case OpBranch:
		return cpu.execBranch(insn)
	case OpLoad:
		return cpu.execLoad(insn)
	case OpStore:
		return cpu.execStore(insn)
	case OpOpImm:
		return cpu.execOpImm(insn)
	case OpOpImm32:
		return cpu.execOpImm32(insn)
	case OpOp:
		return cpu.execOp(insn)
	case OpOp32:
		return cpu.execOp32(insn)
	case OpMiscMem:
		return cpu.execMiscMem(insn)
	case OpSystem:
		return cpu.execSystem(insn)
	case OpAMO:
		return cpu.execAMO(insn)
	case OpLoadFP:
		return cpu.execLoadFP(insn)
	case OpStoreFP:
		return cpu.execStoreFP(insn)
	case OpOpFP:
		return cpu.execOpFP(insn)
	case OpMadd, OpMsub, OpNmsub, OpNmadd:
		return cpu.execFMA(insn, op)
	default:
		return Exception(CauseIllegalInsn, uint64(insn))
	}
}

// LUI - Load Upper Immediate
func (cpu *CPU) execLui(insn uint32) error {
	cpu.WriteReg(rd(insn), uint64(immU(insn)))
	return nil
}

// AUIPC - Add Upper Immediate to PC
func (cpu *CPU) execAuipc(insn uint32) error {
	cpu.WriteReg(rd(insn), uint64(int64(cpu.PC)+immU(insn)))
	return nil
}

// JAL - Jump and Link
func (cpu *CPU) execJal(insn uint32) error {
	target := uint64(int64(cpu.PC) + immJ(insn))
	cpu.WriteReg(rd(insn), cpu.PC+4)
	cpu.PC = target
	return nil
}

// JALR - Jump and Link Register
func (cpu *CPU) execJalr(insn uint32) error {
	target := uint64(int64(cpu.ReadReg(rs1(insn)))+immI(insn)) & ^uint64(1)
	cpu.WriteReg(rd(insn), cpu.PC+4)
	cpu.PC = target
	return nil
}

// Branch instructions
func (cpu *CPU) execBranch(insn uint32) error {
	r1 := cpu.ReadReg(rs1(insn))
	r2 := cpu.ReadReg(rs2(insn))
	f3 := funct3(insn)

	var taken bool
	switch f3 {
	case 0b000: // BEQ
		taken = r1 == r2
	case 0b001: // BNE
		taken = r1 != r2
	case 0b100: // BLT
		taken = int64(r1) < int64(r2)
	case 0b101: // BGE
		taken = int64(r1) >= int64(r2)
	case 0b110: // BLTU
		taken = r1 < r2
	case 0b111: // BGEU
		taken = r1 >= r2
	default:
		return Exception(CauseIllegalInsn, uint64(insn))
	}

	if taken {
		cpu.PC = uint64(int64(cpu.PC) + immB(insn))
	}
	// If not taken, PC will be incremented by Step
	return nil
}

// Load instructions
func (cpu *CPU) execLoad(insn uint32) error {
	addr := uint64(int64(cpu.ReadReg(rs1(insn))) + immI(insn))
	f3 := funct3(insn)

	var val uint64
	var err error

	switch f3 {
	case 0b000: // LB
		v, e := cpu.Bus.Read8(addr)
		val, err = uint64(int8(v)), e
	case 0b001: // LH
		v, e := cpu.Bus.Read16(addr)
		val, err = uint64(int16(v)), e
	case 0b010: // LW
		v, e := cpu.Bus.Read32(addr)
		val, err = uint64(int32(v)), e
	case 0b011: // LD
		val, err = cpu.Bus.Read64(addr)
	case 0b100: // LBU
		v, e := cpu.Bus.Read8(addr)
		val, err = uint64(v), e
	case 0b101: // LHU
		v, e := cpu.Bus.Read16(addr)
		val, err = uint64(v), e
	case 0b110: // LWU
		v, e := cpu.Bus.Read32(addr)
		val, err = uint64(v), e
	default:
		return Exception(CauseIllegalInsn, uint64(insn))
	}

	if err != nil {
		return Exception(CauseLoadAccessFault, addr)
	}

	cpu.WriteReg(rd(insn), val)
	return nil
}

// Store instructions
func (cpu *CPU) execStore(insn uint32) error {
	addr := uint64(int64(cpu.ReadReg(rs1(insn))) + immS(insn))
	val := cpu.ReadReg(rs2(insn))
	f3 := funct3(insn)

	var err error
	switch f3 {
	case 0b000: // SB
		err = cpu.Bus.Write8(addr, uint8(val))
	case 0b001: // SH
		err = cpu.Bus.Write16(addr, uint16(val))
	case 0b010: // SW
		err = cpu.Bus.Write32(addr, uint32(val))
	case 0b011: // SD
		err = cpu.Bus.Write64(addr, val)
	default:
		return Exception(CauseIllegalInsn, uint64(insn))
	}

	if err != nil {
		return Exception(CauseStoreAccessFault, addr)
	}

	return nil
}

// Immediate ALU operations
func (cpu *CPU) execOpImm(insn uint32) error {
	r1 := cpu.ReadReg(rs1(insn))
	imm := immI(insn)
	f3 := funct3(insn)
	sh := shamt(insn)

	var val uint64
	switch f3 {
	case 0b000: // ADDI
		val = uint64(int64(r1) + imm)
	case 0b001: // SLLI
		val = r1 << sh
	case 0b010: // SLTI
		if int64(r1) < imm {
			val = 1
		}
	case 0b011: // SLTIU
		if r1 < uint64(imm) {
			val = 1
		}
	case 0b100: // XORI
		val = r1 ^ uint64(imm)
	case 0b101: // SRLI/SRAI
		if (insn>>30)&1 == 1 {
			val = uint64(int64(r1) >> sh) // SRAI
		} else {
			val = r1 >> sh // SRLI
		}
	case 0b110: // ORI
		val = r1 | uint64(imm)
	case 0b111: // ANDI
		val = r1 & uint64(imm)
	default:
		return Exception(CauseIllegalInsn, uint64(insn))
	}

	cpu.WriteReg(rd(insn), val)
	return nil
}

// 32-bit Immediate ALU operations
func (cpu *CPU) execOpImm32(insn uint32) error {
	r1 := uint32(cpu.ReadReg(rs1(insn)))
	imm := int32(immI(insn))
	f3 := funct3(insn)
	sh := shamt32(insn)

	var val int32
	switch f3 {
	case 0b000: // ADDIW
		val = int32(r1) + imm
	case 0b001: // SLLIW
		val = int32(r1 << sh)
	case 0b101: // SRLIW/SRAIW
		if (insn>>30)&1 == 1 {
			val = int32(r1) >> sh // SRAIW
		} else {
			val = int32(r1 >> sh) // SRLIW
		}
	default:
		return Exception(CauseIllegalInsn, uint64(insn))
	}

	cpu.WriteReg(rd(insn), uint64(val))
	return nil
}

// Register-Register ALU operations
func (cpu *CPU) execOp(insn uint32) error {
	r1 := cpu.ReadReg(rs1(insn))
	r2 := cpu.ReadReg(rs2(insn))
	f3 := funct3(insn)
	f7 := funct7(insn)

	var val uint64

	if f7 == 0b0000001 {
		// M extension
		return cpu.execOpM(insn, r1, r2, f3)
	}

	switch f3 {
	case 0b000: // ADD/SUB
		if f7 == 0b0100000 {
			val = uint64(int64(r1) - int64(r2)) // SUB
		} else {
			val = uint64(int64(r1) + int64(r2)) // ADD
		}
	case 0b001: // SLL
		val = r1 << (r2 & 0x3f)
	case 0b010: // SLT
		if int64(r1) < int64(r2) {
			val = 1
		}
	case 0b011: // SLTU
		if r1 < r2 {
			val = 1
		}
	case 0b100: // XOR
		val = r1 ^ r2
	case 0b101: // SRL/SRA
		if f7 == 0b0100000 {
			val = uint64(int64(r1) >> (r2 & 0x3f)) // SRA
		} else {
			val = r1 >> (r2 & 0x3f) // SRL
		}
	case 0b110: // OR
		val = r1 | r2
	case 0b111: // AND
		val = r1 & r2
	default:
		return Exception(CauseIllegalInsn, uint64(insn))
	}

	cpu.WriteReg(rd(insn), val)
	return nil
}

// M extension operations
func (cpu *CPU) execOpM(insn uint32, r1, r2 uint64, f3 uint32) error {
	var val uint64

	switch f3 {
	case 0b000: // MUL
		val = uint64(int64(r1) * int64(r2))
	case 0b001: // MULH
		hi, _ := mulh64(int64(r1), int64(r2))
		val = uint64(hi)
	case 0b010: // MULHSU
		hi, _ := mulhsu64(int64(r1), r2)
		val = uint64(hi)
	case 0b011: // MULHU
		hi, _ := mulhu64(r1, r2)
		val = hi
	case 0b100: // DIV
		if r2 == 0 {
			val = ^uint64(0)
		} else if r1 == uint64(1<<63) && r2 == ^uint64(0) {
			val = r1 // overflow case
		} else {
			val = uint64(int64(r1) / int64(r2))
		}
	case 0b101: // DIVU
		if r2 == 0 {
			val = ^uint64(0)
		} else {
			val = r1 / r2
		}
	case 0b110: // REM
		if r2 == 0 {
			val = r1
		} else if r1 == uint64(1<<63) && r2 == ^uint64(0) {
			val = 0 // overflow case
		} else {
			val = uint64(int64(r1) % int64(r2))
		}
	case 0b111: // REMU
		if r2 == 0 {
			val = r1
		} else {
			val = r1 % r2
		}
	default:
		return Exception(CauseIllegalInsn, uint64(insn))
	}

	cpu.WriteReg(rd(insn), val)
	return nil
}

// 32-bit Register-Register ALU operations
func (cpu *CPU) execOp32(insn uint32) error {
	r1 := uint32(cpu.ReadReg(rs1(insn)))
	r2 := uint32(cpu.ReadReg(rs2(insn)))
	f3 := funct3(insn)
	f7 := funct7(insn)

	var val int32

	if f7 == 0b0000001 {
		// M extension 32-bit
		return cpu.execOp32M(insn, r1, r2, f3)
	}

	switch f3 {
	case 0b000: // ADDW/SUBW
		if f7 == 0b0100000 {
			val = int32(r1) - int32(r2) // SUBW
		} else {
			val = int32(r1) + int32(r2) // ADDW
		}
	case 0b001: // SLLW
		val = int32(r1 << (r2 & 0x1f))
	case 0b101: // SRLW/SRAW
		if f7 == 0b0100000 {
			val = int32(r1) >> (r2 & 0x1f) // SRAW
		} else {
			val = int32(r1 >> (r2 & 0x1f)) // SRLW
		}
	default:
		return Exception(CauseIllegalInsn, uint64(insn))
	}

	cpu.WriteReg(rd(insn), uint64(val))
	return nil
}

// M extension 32-bit operations
func (cpu *CPU) execOp32M(insn uint32, r1, r2 uint32, f3 uint32) error {
	var val int32

	switch f3 {
	case 0b000: // MULW
		val = int32(r1) * int32(r2)
	case 0b100: // DIVW
		if r2 == 0 {
			val = -1
		} else if r1 == uint32(1<<31) && r2 == ^uint32(0) {
			val = int32(r1) // overflow case
		} else {
			val = int32(r1) / int32(r2)
		}
	case 0b101: // DIVUW
		if r2 == 0 {
			val = -1
		} else {
			val = int32(r1 / r2)
		}
	case 0b110: // REMW
		if r2 == 0 {
			val = int32(r1)
		} else if r1 == uint32(1<<31) && r2 == ^uint32(0) {
			val = 0 // overflow case
		} else {
			val = int32(r1) % int32(r2)
		}
	case 0b111: // REMUW
		if r2 == 0 {
			val = int32(r1)
		} else {
			val = int32(r1 % r2)
		}
	default:
		return Exception(CauseIllegalInsn, uint64(insn))
	}

	cpu.WriteReg(rd(insn), uint64(val))
	return nil
}

// FENCE instructions
func (cpu *CPU) execMiscMem(insn uint32) error {
	f3 := funct3(insn)

	switch f3 {
	case 0b000: // FENCE
		// No-op in single-threaded emulator
	case 0b001: // FENCE.I
		// No-op (instruction cache flush)
	default:
		return Exception(CauseIllegalInsn, uint64(insn))
	}

	return nil
}

// Helper for 64-bit unsigned multiply high
func mulhu64(a, b uint64) (uint64, uint64) {
	const mask32 = 0xFFFFFFFF
	a0 := a & mask32
	a1 := a >> 32
	b0 := b & mask32
	b1 := b >> 32

	p0 := a0 * b0
	p1 := a0 * b1
	p2 := a1 * b0
	p3 := a1 * b1

	carry := ((p0 >> 32) + (p1 & mask32) + (p2 & mask32)) >> 32
	hi := p3 + (p1 >> 32) + (p2 >> 32) + carry
	lo := a * b

	return hi, lo
}

// Helper for 64-bit signed multiply high
func mulh64(a, b int64) (int64, uint64) {
	negResult := (a < 0) != (b < 0)
	ua := uint64(a)
	ub := uint64(b)
	if a < 0 {
		ua = uint64(-a)
	}
	if b < 0 {
		ub = uint64(-b)
	}

	hi, lo := mulhu64(ua, ub)

	if negResult {
		// Negate 128-bit result
		lo = ^lo + 1
		hi = ^hi
		if lo == 0 {
			hi++
		}
	}

	return int64(hi), lo
}

// Helper for 64-bit signed*unsigned multiply high
func mulhsu64(a int64, b uint64) (int64, uint64) {
	negResult := a < 0
	ua := uint64(a)
	if a < 0 {
		ua = uint64(-a)
	}

	hi, lo := mulhu64(ua, b)

	if negResult {
		// Negate 128-bit result
		lo = ^lo + 1
		hi = ^hi
		if lo == 0 {
			hi++
		}
	}

	return int64(hi), lo
}

// System instructions (ECALL, EBREAK, CSR, etc.)
func (cpu *CPU) execSystem(insn uint32) error {
	f3 := funct3(insn)
	csr := uint16(insn >> 20)
	rdReg := rd(insn)
	rs1Reg := rs1(insn)

	if f3 == 0 {
		// ECALL, EBREAK, etc.
		switch insn {
		case 0x00000073: // ECALL
			return cpu.handleEcall()
		case 0x00100073: // EBREAK
			return Exception(CauseBreakpoint, cpu.PC)
		case 0x30200073: // MRET
			return cpu.handleMret()
		case 0x10200073: // SRET
			return cpu.handleSret()
		case 0x10500073: // WFI
			cpu.WFI = true
			return nil
		default:
			// SFENCE.VMA and other privileged instructions
			if (insn >> 25) == 0b0001001 {
				// SFENCE.VMA - no-op for now
				return nil
			}
			return Exception(CauseIllegalInsn, uint64(insn))
		}
	}

	// CSR instructions
	var writeVal uint64
	var doWrite bool

	rs1Val := cpu.ReadReg(rs1Reg)
	if f3 >= 5 {
		// Immediate forms use rs1 field as immediate
		rs1Val = uint64(rs1Reg)
	}

	// Read CSR first
	csrVal, err := cpu.csrRead(csr)
	if err != nil {
		return err
	}

	switch f3 & 3 {
	case 1: // CSRRW(I)
		writeVal = rs1Val
		doWrite = true
	case 2: // CSRRS(I)
		writeVal = csrVal | rs1Val
		doWrite = rs1Reg != 0
	case 3: // CSRRC(I)
		writeVal = csrVal & ^rs1Val
		doWrite = rs1Reg != 0
	default:
		return Exception(CauseIllegalInsn, uint64(insn))
	}

	// Write CSR if needed
	if doWrite {
		if err := cpu.csrWrite(csr, writeVal); err != nil {
			return err
		}
	}

	// Write result to rd
	cpu.WriteReg(rdReg, csrVal)
	return nil
}

// handleEcall handles environment calls
func (cpu *CPU) handleEcall() error {
	switch cpu.Priv {
	case PrivUser:
		return Exception(CauseEcallFromU, 0)
	case PrivSupervisor:
		return Exception(CauseEcallFromS, 0)
	case PrivMachine:
		return Exception(CauseEcallFromM, 0)
	default:
		return fmt.Errorf("invalid privilege level: %d", cpu.Priv)
	}
}

// handleMret handles machine-mode return
func (cpu *CPU) handleMret() error {
	if cpu.Priv < PrivMachine {
		return Exception(CauseIllegalInsn, 0)
	}

	// Restore privilege level from MPP
	mpp := (cpu.Mstatus >> MstatusMPPShift) & 3
	cpu.Priv = uint8(mpp)

	// Restore MIE from MPIE
	if cpu.Mstatus&MstatusMPIE != 0 {
		cpu.Mstatus |= MstatusMIE
	} else {
		cpu.Mstatus &^= MstatusMIE
	}

	// Set MPIE to 1
	cpu.Mstatus |= MstatusMPIE

	// Set MPP to U (or S if U-mode not supported)
	cpu.Mstatus &^= MstatusMPP

	// Jump to mepc
	cpu.PC = cpu.Mepc

	return nil
}

// handleSret handles supervisor-mode return
func (cpu *CPU) handleSret() error {
	if cpu.Priv < PrivSupervisor {
		return Exception(CauseIllegalInsn, 0)
	}

	// Restore privilege level from SPP
	spp := (cpu.Mstatus >> MstatusSPPShift) & 1
	if spp == 1 {
		cpu.Priv = PrivSupervisor
	} else {
		cpu.Priv = PrivUser
	}

	// Restore SIE from SPIE
	if cpu.Mstatus&MstatusSPIE != 0 {
		cpu.Mstatus |= MstatusSIE
	} else {
		cpu.Mstatus &^= MstatusSIE
	}

	// Set SPIE to 1
	cpu.Mstatus |= MstatusSPIE

	// Set SPP to 0
	cpu.Mstatus &^= MstatusSPP

	// Jump to sepc
	cpu.PC = cpu.Sepc

	return nil
}
