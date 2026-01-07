package rv64

// Compressed instruction field extraction
func cOp(insn uint16) uint16     { return insn & 0x3 }
func cFunct3(insn uint16) uint16 { return (insn >> 13) & 0x7 }

// C.ADDI4SPN, C.LW, C.LD, C.SW, C.SD register fields (3-bit, mapped to x8-x15)
func cRd_(insn uint16) uint32  { return uint32(((insn >> 2) & 0x7) + 8) }
func cRs1_(insn uint16) uint32 { return uint32(((insn >> 7) & 0x7) + 8) }
func cRs2_(insn uint16) uint32 { return uint32(((insn >> 2) & 0x7) + 8) }

// C.LWSP, C.SDSP, etc. register fields (full 5-bit)
func cRd(insn uint16) uint32  { return uint32((insn >> 7) & 0x1f) }
func cRs1(insn uint16) uint32 { return uint32((insn >> 7) & 0x1f) }
func cRs2(insn uint16) uint32 { return uint32((insn >> 2) & 0x1f) }

// Expand a 16-bit compressed instruction to 32-bit
func (cpu *CPU) ExpandCompressed(insn uint16) (uint32, error) {
	op := cOp(insn)
	funct3 := cFunct3(insn)

	switch op {
	case 0b00: // Quadrant 0
		return cpu.expandQ0(insn, funct3)
	case 0b01: // Quadrant 1
		return cpu.expandQ1(insn, funct3)
	case 0b10: // Quadrant 2
		return cpu.expandQ2(insn, funct3)
	default:
		return 0, Exception(CauseIllegalInsn, uint64(insn))
	}
}

// expandQ0 expands quadrant 0 compressed instructions
func (cpu *CPU) expandQ0(insn uint16, funct3 uint16) (uint32, error) {
	switch funct3 {
	case 0b000: // C.ADDI4SPN
		// nzuimm[5:4|9:6|2|3] = insn[12:5]
		imm := ((uint32(insn) >> 6) & 0x1) << 2
		imm |= ((uint32(insn) >> 5) & 0x1) << 3
		imm |= ((uint32(insn) >> 11) & 0x3) << 4
		imm |= ((uint32(insn) >> 7) & 0xf) << 6
		if imm == 0 {
			return 0, Exception(CauseIllegalInsn, uint64(insn))
		}
		rd := cRd_(insn)
		// addi rd', x2, nzuimm -> addi rd', x2, imm
		return (imm << 20) | (2 << 15) | (0b000 << 12) | (rd << 7) | 0b0010011, nil

	case 0b001: // C.FLD (RV64)
		// uimm[5:3|7:6] = insn[12:10|6:5]
		imm := ((uint32(insn) >> 10) & 0x7) << 3
		imm |= ((uint32(insn) >> 5) & 0x3) << 6
		rs1 := cRs1_(insn)
		rd := cRd_(insn)
		// fld rd', offset(rs1')
		return (imm << 20) | (rs1 << 15) | (0b011 << 12) | (rd << 7) | 0b0000111, nil

	case 0b010: // C.LW
		// uimm[5:3|2|6] = insn[12:10|6|5]
		imm := ((uint32(insn) >> 6) & 0x1) << 2
		imm |= ((uint32(insn) >> 10) & 0x7) << 3
		imm |= ((uint32(insn) >> 5) & 0x1) << 6
		rs1 := cRs1_(insn)
		rd := cRd_(insn)
		// lw rd', offset(rs1')
		return (imm << 20) | (rs1 << 15) | (0b010 << 12) | (rd << 7) | 0b0000011, nil

	case 0b011: // C.LD (RV64)
		// uimm[5:3|7:6] = insn[12:10|6:5]
		imm := ((uint32(insn) >> 10) & 0x7) << 3
		imm |= ((uint32(insn) >> 5) & 0x3) << 6
		rs1 := cRs1_(insn)
		rd := cRd_(insn)
		// ld rd', offset(rs1')
		return (imm << 20) | (rs1 << 15) | (0b011 << 12) | (rd << 7) | 0b0000011, nil

	case 0b101: // C.FSD (RV64)
		// uimm[5:3|7:6] = insn[12:10|6:5]
		imm := ((uint32(insn) >> 10) & 0x7) << 3
		imm |= ((uint32(insn) >> 5) & 0x3) << 6
		rs1 := cRs1_(insn)
		rs2 := cRs2_(insn)
		// fsd rs2', offset(rs1')
		immHi := (imm >> 5) & 0x7f
		immLo := imm & 0x1f
		return (immHi << 25) | (rs2 << 20) | (rs1 << 15) | (0b011 << 12) | (immLo << 7) | 0b0100111, nil

	case 0b110: // C.SW
		// uimm[5:3|2|6] = insn[12:10|6|5]
		imm := ((uint32(insn) >> 6) & 0x1) << 2
		imm |= ((uint32(insn) >> 10) & 0x7) << 3
		imm |= ((uint32(insn) >> 5) & 0x1) << 6
		rs1 := cRs1_(insn)
		rs2 := cRs2_(insn)
		// sw rs2', offset(rs1')
		immHi := (imm >> 5) & 0x7f
		immLo := imm & 0x1f
		return (immHi << 25) | (rs2 << 20) | (rs1 << 15) | (0b010 << 12) | (immLo << 7) | 0b0100011, nil

	case 0b111: // C.SD (RV64)
		// uimm[5:3|7:6] = insn[12:10|6:5]
		imm := ((uint32(insn) >> 10) & 0x7) << 3
		imm |= ((uint32(insn) >> 5) & 0x3) << 6
		rs1 := cRs1_(insn)
		rs2 := cRs2_(insn)
		// sd rs2', offset(rs1')
		immHi := (imm >> 5) & 0x7f
		immLo := imm & 0x1f
		return (immHi << 25) | (rs2 << 20) | (rs1 << 15) | (0b011 << 12) | (immLo << 7) | 0b0100011, nil
	}

	return 0, Exception(CauseIllegalInsn, uint64(insn))
}

// expandQ1 expands quadrant 1 compressed instructions
func (cpu *CPU) expandQ1(insn uint16, funct3 uint16) (uint32, error) {
	switch funct3 {
	case 0b000: // C.NOP / C.ADDI
		rd := cRd(insn)
		// imm[5|4:0] = insn[12|6:2]
		imm := uint32(insn>>2) & 0x1f
		if (insn>>12)&1 != 0 {
			imm |= 0xffffffe0 // Sign extend
		}
		if rd == 0 {
			// C.NOP -> addi x0, x0, 0
			return 0b0010011, nil
		}
		// C.ADDI -> addi rd, rd, imm
		return (imm << 20) | (rd << 15) | (0b000 << 12) | (rd << 7) | 0b0010011, nil

	case 0b001: // C.ADDIW (RV64)
		rd := cRd(insn)
		if rd == 0 {
			return 0, Exception(CauseIllegalInsn, uint64(insn))
		}
		// imm[5|4:0] = insn[12|6:2]
		imm := uint32(insn>>2) & 0x1f
		if (insn>>12)&1 != 0 {
			imm |= 0xffffffe0 // Sign extend
		}
		// C.ADDIW -> addiw rd, rd, imm
		return (imm << 20) | (rd << 15) | (0b000 << 12) | (rd << 7) | 0b0011011, nil

	case 0b010: // C.LI
		rd := cRd(insn)
		// imm[5|4:0] = insn[12|6:2]
		imm := uint32(insn>>2) & 0x1f
		if (insn>>12)&1 != 0 {
			imm |= 0xffffffe0 // Sign extend
		}
		// C.LI -> addi rd, x0, imm
		return (imm << 20) | (0 << 15) | (0b000 << 12) | (rd << 7) | 0b0010011, nil

	case 0b011: // C.ADDI16SP / C.LUI
		rd := cRd(insn)
		if rd == 2 {
			// C.ADDI16SP
			// nzimm[9|4|6|8:7|5] = insn[12|6|5|4:3|2]
			imm := ((uint32(insn) >> 2) & 0x1) << 5
			imm |= ((uint32(insn) >> 3) & 0x3) << 7
			imm |= ((uint32(insn) >> 5) & 0x1) << 6
			imm |= ((uint32(insn) >> 6) & 0x1) << 4
			if (insn>>12)&1 != 0 {
				imm |= 0xfffffc00 // Sign extend from bit 9
			}
			if imm == 0 {
				return 0, Exception(CauseIllegalInsn, uint64(insn))
			}
			// C.ADDI16SP -> addi x2, x2, nzimm
			return (imm << 20) | (2 << 15) | (0b000 << 12) | (2 << 7) | 0b0010011, nil
		} else {
			// C.LUI
			if rd == 0 {
				return 0, Exception(CauseIllegalInsn, uint64(insn))
			}
			// nzimm[17|16:12] = insn[12|6:2]
			imm := (uint32(insn>>2) & 0x1f) << 12
			if (insn>>12)&1 != 0 {
				imm |= 0xfffe0000 // Sign extend from bit 17
			}
			if imm == 0 {
				return 0, Exception(CauseIllegalInsn, uint64(insn))
			}
			// C.LUI -> lui rd, nzimm[17:12]
			return (imm & 0xfffff000) | (rd << 7) | 0b0110111, nil
		}

	case 0b100: // C.SRLI, C.SRAI, C.ANDI, C.SUB, C.XOR, C.OR, C.AND, C.SUBW, C.ADDW
		funct2 := (insn >> 10) & 0x3
		rd := cRs1_(insn) // Note: rd' = rs1'
		switch funct2 {
		case 0b00: // C.SRLI
			// shamt[5|4:0] = insn[12|6:2]
			shamt := uint32(insn>>2) & 0x1f
			if (insn>>12)&1 != 0 {
				shamt |= 0x20
			}
			// C.SRLI -> srli rd', rd', shamt
			return (shamt << 20) | (rd << 15) | (0b101 << 12) | (rd << 7) | 0b0010011, nil

		case 0b01: // C.SRAI
			// shamt[5|4:0] = insn[12|6:2]
			shamt := uint32(insn>>2) & 0x1f
			if (insn>>12)&1 != 0 {
				shamt |= 0x20
			}
			// C.SRAI -> srai rd', rd', shamt
			return (0b010000<<25 | shamt<<20) | (rd << 15) | (0b101 << 12) | (rd << 7) | 0b0010011, nil

		case 0b10: // C.ANDI
			// imm[5|4:0] = insn[12|6:2]
			imm := uint32(insn>>2) & 0x1f
			if (insn>>12)&1 != 0 {
				imm |= 0xffffffe0 // Sign extend
			}
			// C.ANDI -> andi rd', rd', imm
			return (imm << 20) | (rd << 15) | (0b111 << 12) | (rd << 7) | 0b0010011, nil

		case 0b11:
			rs2 := cRs2_(insn)
			funct1 := (insn >> 12) & 0x1
			funct2b := (insn >> 5) & 0x3
			if funct1 == 0 {
				switch funct2b {
				case 0b00: // C.SUB
					return (0b0100000 << 25) | (rs2 << 20) | (rd << 15) | (0b000 << 12) | (rd << 7) | 0b0110011, nil
				case 0b01: // C.XOR
					return (rs2 << 20) | (rd << 15) | (0b100 << 12) | (rd << 7) | 0b0110011, nil
				case 0b10: // C.OR
					return (rs2 << 20) | (rd << 15) | (0b110 << 12) | (rd << 7) | 0b0110011, nil
				case 0b11: // C.AND
					return (rs2 << 20) | (rd << 15) | (0b111 << 12) | (rd << 7) | 0b0110011, nil
				}
			} else {
				switch funct2b {
				case 0b00: // C.SUBW (RV64)
					return (0b0100000 << 25) | (rs2 << 20) | (rd << 15) | (0b000 << 12) | (rd << 7) | 0b0111011, nil
				case 0b01: // C.ADDW (RV64)
					return (rs2 << 20) | (rd << 15) | (0b000 << 12) | (rd << 7) | 0b0111011, nil
				}
			}
		}
		return 0, Exception(CauseIllegalInsn, uint64(insn))

	case 0b101: // C.J
		// imm[11|4|9:8|10|6|7|3:1|5] = insn[12|11|10:9|8|7|6|5:3|2]
		imm := ((uint32(insn) >> 2) & 0x1) << 5
		imm |= ((uint32(insn) >> 3) & 0x7) << 1
		imm |= ((uint32(insn) >> 6) & 0x1) << 7
		imm |= ((uint32(insn) >> 7) & 0x1) << 6
		imm |= ((uint32(insn) >> 8) & 0x1) << 10
		imm |= ((uint32(insn) >> 9) & 0x3) << 8
		imm |= ((uint32(insn) >> 11) & 0x1) << 4
		if (insn>>12)&1 != 0 {
			imm |= 0xfffff800 // Sign extend from bit 11
		}
		// C.J -> jal x0, offset
		// J-type: imm[20|10:1|11|19:12]
		jimm := ((imm >> 12) & 0xff) << 12 // imm[19:12]
		jimm |= ((imm >> 11) & 0x1) << 20  // imm[20]
		jimm |= ((imm >> 1) & 0x3ff) << 21 // imm[10:1]
		jimm |= ((imm >> 11) & 0x1) << 31  // Sign bit
		return (jimm & 0xfffff000) | (0 << 7) | 0b1101111, nil

	case 0b110: // C.BEQZ
		rs1 := cRs1_(insn)
		// imm[8|4:3|7:6|2:1|5] = insn[12|11:10|6:5|4:3|2]
		imm := ((uint32(insn) >> 2) & 0x1) << 5
		imm |= ((uint32(insn) >> 3) & 0x3) << 1
		imm |= ((uint32(insn) >> 5) & 0x3) << 6
		imm |= ((uint32(insn) >> 10) & 0x3) << 3
		if (insn>>12)&1 != 0 {
			imm |= 0xffffff00 // Sign extend from bit 8
		}
		// C.BEQZ -> beq rs1', x0, offset
		// B-type: imm[12|10:5] rs2 rs1 funct3 imm[4:1|11]
		bimm := ((imm >> 11) & 0x1) << 31 // imm[12]
		bimm |= ((imm >> 5) & 0x3f) << 25 // imm[10:5]
		bimm |= ((imm >> 1) & 0xf) << 8   // imm[4:1]
		bimm |= ((imm >> 11) & 0x1) << 7  // imm[11]
		return bimm | (0 << 20) | (rs1 << 15) | (0b000 << 12) | 0b1100011, nil

	case 0b111: // C.BNEZ
		rs1 := cRs1_(insn)
		// Same encoding as C.BEQZ
		imm := ((uint32(insn) >> 2) & 0x1) << 5
		imm |= ((uint32(insn) >> 3) & 0x3) << 1
		imm |= ((uint32(insn) >> 5) & 0x3) << 6
		imm |= ((uint32(insn) >> 10) & 0x3) << 3
		if (insn>>12)&1 != 0 {
			imm |= 0xffffff00 // Sign extend from bit 8
		}
		// C.BNEZ -> bne rs1', x0, offset
		bimm := ((imm >> 11) & 0x1) << 31
		bimm |= ((imm >> 5) & 0x3f) << 25
		bimm |= ((imm >> 1) & 0xf) << 8
		bimm |= ((imm >> 11) & 0x1) << 7
		return bimm | (0 << 20) | (rs1 << 15) | (0b001 << 12) | 0b1100011, nil
	}

	return 0, Exception(CauseIllegalInsn, uint64(insn))
}

// expandQ2 expands quadrant 2 compressed instructions
func (cpu *CPU) expandQ2(insn uint16, funct3 uint16) (uint32, error) {
	switch funct3 {
	case 0b000: // C.SLLI
		rd := cRd(insn)
		if rd == 0 {
			return 0, Exception(CauseIllegalInsn, uint64(insn))
		}
		// shamt[5|4:0] = insn[12|6:2]
		shamt := uint32(insn>>2) & 0x1f
		if (insn>>12)&1 != 0 {
			shamt |= 0x20
		}
		// C.SLLI -> slli rd, rd, shamt
		return (shamt << 20) | (rd << 15) | (0b001 << 12) | (rd << 7) | 0b0010011, nil

	case 0b001: // C.FLDSP (RV64)
		rd := cRd(insn)
		// uimm[5|4:3|8:6] = insn[12|6:5|4:2]
		imm := ((uint32(insn) >> 2) & 0x7) << 6
		imm |= ((uint32(insn) >> 5) & 0x3) << 3
		imm |= ((uint32(insn) >> 12) & 0x1) << 5
		// C.FLDSP -> fld rd, offset(x2)
		return (imm << 20) | (2 << 15) | (0b011 << 12) | (rd << 7) | 0b0000111, nil

	case 0b010: // C.LWSP
		rd := cRd(insn)
		if rd == 0 {
			return 0, Exception(CauseIllegalInsn, uint64(insn))
		}
		// uimm[5|4:2|7:6] = insn[12|6:4|3:2]
		imm := ((uint32(insn) >> 2) & 0x3) << 6
		imm |= ((uint32(insn) >> 4) & 0x7) << 2
		imm |= ((uint32(insn) >> 12) & 0x1) << 5
		// C.LWSP -> lw rd, offset(x2)
		return (imm << 20) | (2 << 15) | (0b010 << 12) | (rd << 7) | 0b0000011, nil

	case 0b011: // C.LDSP (RV64)
		rd := cRd(insn)
		if rd == 0 {
			return 0, Exception(CauseIllegalInsn, uint64(insn))
		}
		// uimm[5|4:3|8:6] = insn[12|6:5|4:2]
		imm := ((uint32(insn) >> 2) & 0x7) << 6
		imm |= ((uint32(insn) >> 5) & 0x3) << 3
		imm |= ((uint32(insn) >> 12) & 0x1) << 5
		// C.LDSP -> ld rd, offset(x2)
		return (imm << 20) | (2 << 15) | (0b011 << 12) | (rd << 7) | 0b0000011, nil

	case 0b100: // C.JR, C.MV, C.EBREAK, C.JALR, C.ADD
		rs1 := cRs1(insn)
		rs2 := cRs2(insn)
		if (insn>>12)&1 == 0 {
			if rs2 == 0 {
				// C.JR
				if rs1 == 0 {
					return 0, Exception(CauseIllegalInsn, uint64(insn))
				}
				// C.JR -> jalr x0, rs1, 0
				return (rs1 << 15) | (0b000 << 12) | (0 << 7) | 0b1100111, nil
			} else {
				// C.MV
				// C.MV -> add rd, x0, rs2
				return (rs2 << 20) | (0 << 15) | (0b000 << 12) | (rs1 << 7) | 0b0110011, nil
			}
		} else {
			if rs2 == 0 {
				if rs1 == 0 {
					// C.EBREAK
					return 0x00100073, nil
				}
				// C.JALR
				// C.JALR -> jalr x1, rs1, 0
				return (rs1 << 15) | (0b000 << 12) | (1 << 7) | 0b1100111, nil
			} else {
				// C.ADD
				// C.ADD -> add rd, rd, rs2
				return (rs2 << 20) | (rs1 << 15) | (0b000 << 12) | (rs1 << 7) | 0b0110011, nil
			}
		}

	case 0b101: // C.FSDSP (RV64)
		rs2 := cRs2(insn)
		// uimm[5:3|8:6] = insn[12:10|9:7]
		imm := ((uint32(insn) >> 7) & 0x7) << 6
		imm |= ((uint32(insn) >> 10) & 0x7) << 3
		// C.FSDSP -> fsd rs2, offset(x2)
		immHi := (imm >> 5) & 0x7f
		immLo := imm & 0x1f
		return (immHi << 25) | (rs2 << 20) | (2 << 15) | (0b011 << 12) | (immLo << 7) | 0b0100111, nil

	case 0b110: // C.SWSP
		rs2 := cRs2(insn)
		// uimm[5:2|7:6] = insn[12:9|8:7]
		imm := ((uint32(insn) >> 7) & 0x3) << 6
		imm |= ((uint32(insn) >> 9) & 0xf) << 2
		// C.SWSP -> sw rs2, offset(x2)
		immHi := (imm >> 5) & 0x7f
		immLo := imm & 0x1f
		return (immHi << 25) | (rs2 << 20) | (2 << 15) | (0b010 << 12) | (immLo << 7) | 0b0100011, nil

	case 0b111: // C.SDSP (RV64)
		rs2 := cRs2(insn)
		// uimm[5:3|8:6] = insn[12:10|9:7]
		imm := ((uint32(insn) >> 7) & 0x7) << 6
		imm |= ((uint32(insn) >> 10) & 0x7) << 3
		// C.SDSP -> sd rs2, offset(x2)
		immHi := (imm >> 5) & 0x7f
		immLo := imm & 0x1f
		return (immHi << 25) | (rs2 << 20) | (2 << 15) | (0b011 << 12) | (immLo << 7) | 0b0100011, nil
	}

	return 0, Exception(CauseIllegalInsn, uint64(insn))
}
