package rv64

// execAMO executes atomic memory operations
func (cpu *CPU) execAMO(insn uint32) error {
	f3 := funct3(insn)
	f5 := funct7(insn) >> 2 // Top 5 bits of funct7

	addr := cpu.ReadReg(rs1(insn))
	rs2Val := cpu.ReadReg(rs2(insn))

	// Check alignment
	switch f3 {
	case 0b010: // 32-bit
		if addr&3 != 0 {
			return Exception(CauseStoreAddrMisaligned, addr)
		}
		return cpu.execAMO32(insn, addr, rs2Val, f5)
	case 0b011: // 64-bit
		if addr&7 != 0 {
			return Exception(CauseStoreAddrMisaligned, addr)
		}
		return cpu.execAMO64(insn, addr, rs2Val, f5)
	default:
		return Exception(CauseIllegalInsn, uint64(insn))
	}
}

// execAMO32 executes 32-bit atomic operations
func (cpu *CPU) execAMO32(insn uint32, addr uint64, rs2Val uint64, f5 uint32) error {
	rdReg := rd(insn)

	switch f5 {
	case 0b00010: // LR.W
		val, err := cpu.Bus.Read32(addr)
		if err != nil {
			return Exception(CauseLoadAccessFault, addr)
		}
		cpu.WriteReg(rdReg, uint64(int32(val)))
		cpu.Reservation = addr
		cpu.ReservationValid = true
		cpu.PC += 4
		return nil

	case 0b00011: // SC.W
		if !cpu.ReservationValid || cpu.Reservation != addr {
			cpu.WriteReg(rdReg, 1) // Failure
			cpu.PC += 4
			return nil
		}
		if err := cpu.Bus.Write32(addr, uint32(rs2Val)); err != nil {
			return Exception(CauseStoreAccessFault, addr)
		}
		cpu.WriteReg(rdReg, 0) // Success
		cpu.ReservationValid = false
		cpu.PC += 4
		return nil

	default:
		// Other AMO operations
		oldVal, err := cpu.Bus.Read32(addr)
		if err != nil {
			return Exception(CauseLoadAccessFault, addr)
		}

		var newVal uint32
		switch f5 {
		case 0b00001: // AMOSWAP.W
			newVal = uint32(rs2Val)
		case 0b00000: // AMOADD.W
			newVal = oldVal + uint32(rs2Val)
		case 0b00100: // AMOXOR.W
			newVal = oldVal ^ uint32(rs2Val)
		case 0b01100: // AMOAND.W
			newVal = oldVal & uint32(rs2Val)
		case 0b01000: // AMOOR.W
			newVal = oldVal | uint32(rs2Val)
		case 0b10000: // AMOMIN.W
			if int32(oldVal) < int32(rs2Val) {
				newVal = oldVal
			} else {
				newVal = uint32(rs2Val)
			}
		case 0b10100: // AMOMAX.W
			if int32(oldVal) > int32(rs2Val) {
				newVal = oldVal
			} else {
				newVal = uint32(rs2Val)
			}
		case 0b11000: // AMOMINU.W
			if oldVal < uint32(rs2Val) {
				newVal = oldVal
			} else {
				newVal = uint32(rs2Val)
			}
		case 0b11100: // AMOMAXU.W
			if oldVal > uint32(rs2Val) {
				newVal = oldVal
			} else {
				newVal = uint32(rs2Val)
			}
		default:
			return Exception(CauseIllegalInsn, uint64(insn))
		}

		if err := cpu.Bus.Write32(addr, newVal); err != nil {
			return Exception(CauseStoreAccessFault, addr)
		}
		cpu.WriteReg(rdReg, uint64(int32(oldVal)))
		cpu.PC += 4
		return nil
	}
}

// execAMO64 executes 64-bit atomic operations
func (cpu *CPU) execAMO64(insn uint32, addr uint64, rs2Val uint64, f5 uint32) error {
	rdReg := rd(insn)

	switch f5 {
	case 0b00010: // LR.D
		val, err := cpu.Bus.Read64(addr)
		if err != nil {
			return Exception(CauseLoadAccessFault, addr)
		}
		cpu.WriteReg(rdReg, val)
		cpu.Reservation = addr
		cpu.ReservationValid = true
		cpu.PC += 4
		return nil

	case 0b00011: // SC.D
		if !cpu.ReservationValid || cpu.Reservation != addr {
			cpu.WriteReg(rdReg, 1) // Failure
			cpu.PC += 4
			return nil
		}
		if err := cpu.Bus.Write64(addr, rs2Val); err != nil {
			return Exception(CauseStoreAccessFault, addr)
		}
		cpu.WriteReg(rdReg, 0) // Success
		cpu.ReservationValid = false
		cpu.PC += 4
		return nil

	default:
		// Other AMO operations
		oldVal, err := cpu.Bus.Read64(addr)
		if err != nil {
			return Exception(CauseLoadAccessFault, addr)
		}

		var newVal uint64
		switch f5 {
		case 0b00001: // AMOSWAP.D
			newVal = rs2Val
		case 0b00000: // AMOADD.D
			newVal = oldVal + rs2Val
		case 0b00100: // AMOXOR.D
			newVal = oldVal ^ rs2Val
		case 0b01100: // AMOAND.D
			newVal = oldVal & rs2Val
		case 0b01000: // AMOOR.D
			newVal = oldVal | rs2Val
		case 0b10000: // AMOMIN.D
			if int64(oldVal) < int64(rs2Val) {
				newVal = oldVal
			} else {
				newVal = rs2Val
			}
		case 0b10100: // AMOMAX.D
			if int64(oldVal) > int64(rs2Val) {
				newVal = oldVal
			} else {
				newVal = rs2Val
			}
		case 0b11000: // AMOMINU.D
			if oldVal < rs2Val {
				newVal = oldVal
			} else {
				newVal = rs2Val
			}
		case 0b11100: // AMOMAXU.D
			if oldVal > rs2Val {
				newVal = oldVal
			} else {
				newVal = rs2Val
			}
		default:
			return Exception(CauseIllegalInsn, uint64(insn))
		}

		if err := cpu.Bus.Write64(addr, newVal); err != nil {
			return Exception(CauseStoreAccessFault, addr)
		}
		cpu.WriteReg(rdReg, oldVal)
		cpu.PC += 4
		return nil
	}
}
