package rv64

import (
	"math"
)

// Floating point rounding modes
const (
	RoundNearestEven = 0
	RoundToZero      = 1
	RoundDown        = 2
	RoundUp          = 3
	RoundNearestMax  = 4
	RoundDynamic     = 7
)

// Floating point exception flags
const (
	FlagNX = 1 << 0 // Inexact
	FlagUF = 1 << 1 // Underflow
	FlagOF = 1 << 2 // Overflow
	FlagDZ = 1 << 3 // Divide by zero
	FlagNV = 1 << 4 // Invalid operation
)

// Helper functions for float conversion
func f32ToU64(f float32) uint64 {
	bits := math.Float32bits(f)
	// NaN-boxing: upper bits are all 1s
	return 0xffffffff00000000 | uint64(bits)
}

func u64ToF32(val uint64) float32 {
	// Check NaN-boxing
	if (val >> 32) != 0xffffffff {
		return float32(math.NaN())
	}
	return math.Float32frombits(uint32(val))
}

func f64ToU64(f float64) uint64 {
	return math.Float64bits(f)
}

func u64ToF64(val uint64) float64 {
	return math.Float64frombits(val)
}

// execLoadFP executes floating point load instructions
func (cpu *CPU) execLoadFP(insn uint32) error {
	addr := uint64(int64(cpu.ReadReg(rs1(insn))) + immI(insn))
	rdReg := rd(insn)
	f3 := funct3(insn)

	switch f3 {
	case 0b010: // FLW
		val, err := cpu.Bus.Read32(addr)
		if err != nil {
			return Exception(CauseLoadAccessFault, addr)
		}
		cpu.F[rdReg] = f32ToU64(math.Float32frombits(val))
		cpu.setFS(3) // Dirty

	case 0b011: // FLD
		val, err := cpu.Bus.Read64(addr)
		if err != nil {
			return Exception(CauseLoadAccessFault, addr)
		}
		cpu.F[rdReg] = val
		cpu.setFS(3) // Dirty

	default:
		return Exception(CauseIllegalInsn, uint64(insn))
	}

	cpu.PC += 4
	return nil
}

// execStoreFP executes floating point store instructions
func (cpu *CPU) execStoreFP(insn uint32) error {
	addr := uint64(int64(cpu.ReadReg(rs1(insn))) + immS(insn))
	rs2Reg := rs2(insn)
	f3 := funct3(insn)

	switch f3 {
	case 0b010: // FSW
		val := uint32(cpu.F[rs2Reg])
		if err := cpu.Bus.Write32(addr, val); err != nil {
			return Exception(CauseStoreAccessFault, addr)
		}

	case 0b011: // FSD
		if err := cpu.Bus.Write64(addr, cpu.F[rs2Reg]); err != nil {
			return Exception(CauseStoreAccessFault, addr)
		}

	default:
		return Exception(CauseIllegalInsn, uint64(insn))
	}

	cpu.PC += 4
	return nil
}

// execOpFP executes floating point operations
func (cpu *CPU) execOpFP(insn uint32) error {
	f7 := funct7(insn)
	f3 := funct3(insn)
	rdReg := rd(insn)
	rs1Reg := rs1(insn)
	rs2Reg := rs2(insn)
	rm := f3 // rounding mode

	// Use dynamic rounding mode if specified
	if rm == RoundDynamic {
		rm = uint32(cpu.Frm)
	}

	// Determine precision from funct7
	isDouble := (f7 & 1) == 1

	switch f7 >> 2 {
	case 0b00000: // FADD
		if isDouble {
			a := u64ToF64(cpu.F[rs1Reg])
			b := u64ToF64(cpu.F[rs2Reg])
			cpu.F[rdReg] = f64ToU64(a + b)
		} else {
			a := u64ToF32(cpu.F[rs1Reg])
			b := u64ToF32(cpu.F[rs2Reg])
			cpu.F[rdReg] = f32ToU64(a + b)
		}
		cpu.setFS(3)

	case 0b00001: // FSUB
		if isDouble {
			a := u64ToF64(cpu.F[rs1Reg])
			b := u64ToF64(cpu.F[rs2Reg])
			cpu.F[rdReg] = f64ToU64(a - b)
		} else {
			a := u64ToF32(cpu.F[rs1Reg])
			b := u64ToF32(cpu.F[rs2Reg])
			cpu.F[rdReg] = f32ToU64(a - b)
		}
		cpu.setFS(3)

	case 0b00010: // FMUL
		if isDouble {
			a := u64ToF64(cpu.F[rs1Reg])
			b := u64ToF64(cpu.F[rs2Reg])
			cpu.F[rdReg] = f64ToU64(a * b)
		} else {
			a := u64ToF32(cpu.F[rs1Reg])
			b := u64ToF32(cpu.F[rs2Reg])
			cpu.F[rdReg] = f32ToU64(a * b)
		}
		cpu.setFS(3)

	case 0b00011: // FDIV
		if isDouble {
			a := u64ToF64(cpu.F[rs1Reg])
			b := u64ToF64(cpu.F[rs2Reg])
			cpu.F[rdReg] = f64ToU64(a / b)
		} else {
			a := u64ToF32(cpu.F[rs1Reg])
			b := u64ToF32(cpu.F[rs2Reg])
			cpu.F[rdReg] = f32ToU64(a / b)
		}
		cpu.setFS(3)

	case 0b01011: // FSQRT
		if isDouble {
			a := u64ToF64(cpu.F[rs1Reg])
			cpu.F[rdReg] = f64ToU64(math.Sqrt(a))
		} else {
			a := u64ToF32(cpu.F[rs1Reg])
			cpu.F[rdReg] = f32ToU64(float32(math.Sqrt(float64(a))))
		}
		cpu.setFS(3)

	case 0b00100: // FSGNJ, FSGNJN, FSGNJX
		if isDouble {
			a := cpu.F[rs1Reg]
			b := cpu.F[rs2Reg]
			signA := a & (1 << 63)
			signB := b & (1 << 63)
			switch f3 {
			case 0b000: // FSGNJ
				cpu.F[rdReg] = (a &^ (1 << 63)) | signB
			case 0b001: // FSGNJN
				cpu.F[rdReg] = (a &^ (1 << 63)) | (^signB & (1 << 63))
			case 0b010: // FSGNJX
				cpu.F[rdReg] = (a &^ (1 << 63)) | (signA ^ signB)
			default:
				return Exception(CauseIllegalInsn, uint64(insn))
			}
		} else {
			a := uint32(cpu.F[rs1Reg])
			b := uint32(cpu.F[rs2Reg])
			signA := a & (1 << 31)
			signB := b & (1 << 31)
			var result uint32
			switch f3 {
			case 0b000: // FSGNJ
				result = (a &^ (1 << 31)) | signB
			case 0b001: // FSGNJN
				result = (a &^ (1 << 31)) | (^signB & (1 << 31))
			case 0b010: // FSGNJX
				result = (a &^ (1 << 31)) | (signA ^ signB)
			default:
				return Exception(CauseIllegalInsn, uint64(insn))
			}
			cpu.F[rdReg] = f32ToU64(math.Float32frombits(result))
		}
		cpu.setFS(3)

	case 0b00101: // FMIN, FMAX
		if isDouble {
			a := u64ToF64(cpu.F[rs1Reg])
			b := u64ToF64(cpu.F[rs2Reg])
			var result float64
			if f3 == 0b000 {
				result = math.Min(a, b)
			} else {
				result = math.Max(a, b)
			}
			cpu.F[rdReg] = f64ToU64(result)
		} else {
			a := u64ToF32(cpu.F[rs1Reg])
			b := u64ToF32(cpu.F[rs2Reg])
			var result float32
			if f3 == 0b000 {
				result = float32(math.Min(float64(a), float64(b)))
			} else {
				result = float32(math.Max(float64(a), float64(b)))
			}
			cpu.F[rdReg] = f32ToU64(result)
		}
		cpu.setFS(3)

	case 0b10100: // FEQ, FLT, FLE
		var result uint64
		if isDouble {
			a := u64ToF64(cpu.F[rs1Reg])
			b := u64ToF64(cpu.F[rs2Reg])
			switch f3 {
			case 0b010: // FEQ
				if a == b {
					result = 1
				}
			case 0b001: // FLT
				if a < b {
					result = 1
				}
			case 0b000: // FLE
				if a <= b {
					result = 1
				}
			default:
				return Exception(CauseIllegalInsn, uint64(insn))
			}
		} else {
			a := u64ToF32(cpu.F[rs1Reg])
			b := u64ToF32(cpu.F[rs2Reg])
			switch f3 {
			case 0b010: // FEQ
				if a == b {
					result = 1
				}
			case 0b001: // FLT
				if a < b {
					result = 1
				}
			case 0b000: // FLE
				if a <= b {
					result = 1
				}
			default:
				return Exception(CauseIllegalInsn, uint64(insn))
			}
		}
		cpu.WriteReg(rdReg, result)

	case 0b11000: // FCVT.W.S/D, FCVT.WU.S/D, FCVT.L.S/D, FCVT.LU.S/D
		var result int64
		if isDouble {
			a := u64ToF64(cpu.F[rs1Reg])
			switch rs2Reg {
			case 0b00000: // FCVT.W.D
				result = int64(int32(a))
			case 0b00001: // FCVT.WU.D
				result = int64(int32(uint32(a)))
			case 0b00010: // FCVT.L.D
				result = int64(a)
			case 0b00011: // FCVT.LU.D
				result = int64(uint64(a))
			default:
				return Exception(CauseIllegalInsn, uint64(insn))
			}
		} else {
			a := u64ToF32(cpu.F[rs1Reg])
			switch rs2Reg {
			case 0b00000: // FCVT.W.S
				result = int64(int32(a))
			case 0b00001: // FCVT.WU.S
				result = int64(int32(uint32(a)))
			case 0b00010: // FCVT.L.S
				result = int64(a)
			case 0b00011: // FCVT.LU.S
				result = int64(uint64(a))
			default:
				return Exception(CauseIllegalInsn, uint64(insn))
			}
		}
		cpu.WriteReg(rdReg, uint64(result))

	case 0b11010: // FCVT.S/D.W, FCVT.S/D.WU, FCVT.S/D.L, FCVT.S/D.LU
		if isDouble {
			var result float64
			switch rs2Reg {
			case 0b00000: // FCVT.D.W
				result = float64(int32(cpu.ReadReg(rs1Reg)))
			case 0b00001: // FCVT.D.WU
				result = float64(uint32(cpu.ReadReg(rs1Reg)))
			case 0b00010: // FCVT.D.L
				result = float64(int64(cpu.ReadReg(rs1Reg)))
			case 0b00011: // FCVT.D.LU
				result = float64(cpu.ReadReg(rs1Reg))
			default:
				return Exception(CauseIllegalInsn, uint64(insn))
			}
			cpu.F[rdReg] = f64ToU64(result)
		} else {
			var result float32
			switch rs2Reg {
			case 0b00000: // FCVT.S.W
				result = float32(int32(cpu.ReadReg(rs1Reg)))
			case 0b00001: // FCVT.S.WU
				result = float32(uint32(cpu.ReadReg(rs1Reg)))
			case 0b00010: // FCVT.S.L
				result = float32(int64(cpu.ReadReg(rs1Reg)))
			case 0b00011: // FCVT.S.LU
				result = float32(cpu.ReadReg(rs1Reg))
			default:
				return Exception(CauseIllegalInsn, uint64(insn))
			}
			cpu.F[rdReg] = f32ToU64(result)
		}
		cpu.setFS(3)

	case 0b11100: // FMV.X.W/D, FCLASS
		if f3 == 0b000 {
			// FMV.X.W/D
			if isDouble {
				cpu.WriteReg(rdReg, cpu.F[rs1Reg])
			} else {
				cpu.WriteReg(rdReg, uint64(int32(cpu.F[rs1Reg])))
			}
		} else if f3 == 0b001 {
			// FCLASS
			var result uint64
			if isDouble {
				f := u64ToF64(cpu.F[rs1Reg])
				result = classifyF64(f)
			} else {
				f := u64ToF32(cpu.F[rs1Reg])
				result = classifyF32(f)
			}
			cpu.WriteReg(rdReg, result)
		} else {
			return Exception(CauseIllegalInsn, uint64(insn))
		}

	case 0b11110: // FMV.W/D.X
		if isDouble {
			cpu.F[rdReg] = cpu.ReadReg(rs1Reg)
		} else {
			cpu.F[rdReg] = f32ToU64(math.Float32frombits(uint32(cpu.ReadReg(rs1Reg))))
		}
		cpu.setFS(3)

	case 0b01000: // FCVT.S.D / FCVT.D.S
		if isDouble {
			// FCVT.D.S
			f := u64ToF32(cpu.F[rs1Reg])
			cpu.F[rdReg] = f64ToU64(float64(f))
		} else {
			// FCVT.S.D
			f := u64ToF64(cpu.F[rs1Reg])
			cpu.F[rdReg] = f32ToU64(float32(f))
		}
		cpu.setFS(3)

	default:
		return Exception(CauseIllegalInsn, uint64(insn))
	}

	cpu.PC += 4
	return nil
}

// execFMA executes fused multiply-add operations
func (cpu *CPU) execFMA(insn uint32, op uint32) error {
	rdReg := rd(insn)
	rs1Reg := rs1(insn)
	rs2Reg := rs2(insn)
	rs3Reg := rs3(insn)
	fmt := funct2(insn) & 1

	if fmt == 1 {
		// Double precision
		a := u64ToF64(cpu.F[rs1Reg])
		b := u64ToF64(cpu.F[rs2Reg])
		c := u64ToF64(cpu.F[rs3Reg])
		var result float64
		switch op {
		case OpMadd: // FMADD
			result = a*b + c
		case OpMsub: // FMSUB
			result = a*b - c
		case OpNmsub: // FNMSUB
			result = -(a*b) + c
		case OpNmadd: // FNMADD
			result = -(a*b) - c
		}
		cpu.F[rdReg] = f64ToU64(result)
	} else {
		// Single precision
		a := u64ToF32(cpu.F[rs1Reg])
		b := u64ToF32(cpu.F[rs2Reg])
		c := u64ToF32(cpu.F[rs3Reg])
		var result float32
		switch op {
		case OpMadd: // FMADD
			result = a*b + c
		case OpMsub: // FMSUB
			result = a*b - c
		case OpNmsub: // FNMSUB
			result = -(a*b) + c
		case OpNmadd: // FNMADD
			result = -(a*b) - c
		}
		cpu.F[rdReg] = f32ToU64(result)
	}

	cpu.setFS(3)
	cpu.PC += 4
	return nil
}

// setFS sets the floating point status in mstatus
func (cpu *CPU) setFS(state uint64) {
	cpu.Mstatus = (cpu.Mstatus &^ MstatusFS) | (state << MstatusFSShift)
	if state == 3 {
		cpu.Mstatus |= MstatusSD
	}
}

// classifyF32 returns the FCLASS result for a single-precision float
func classifyF32(f float32) uint64 {
	bits := math.Float32bits(f)
	sign := bits >> 31
	exp := (bits >> 23) & 0xff
	frac := bits & 0x7fffff

	if exp == 0xff {
		if frac != 0 {
			if (frac & (1 << 22)) != 0 {
				return 1 << 9 // quiet NaN
			}
			return 1 << 8 // signaling NaN
		}
		if sign != 0 {
			return 1 << 0 // -infinity
		}
		return 1 << 7 // +infinity
	}

	if exp == 0 {
		if frac == 0 {
			if sign != 0 {
				return 1 << 3 // -0
			}
			return 1 << 4 // +0
		}
		if sign != 0 {
			return 1 << 2 // negative subnormal
		}
		return 1 << 5 // positive subnormal
	}

	if sign != 0 {
		return 1 << 1 // negative normal
	}
	return 1 << 6 // positive normal
}

// classifyF64 returns the FCLASS result for a double-precision float
func classifyF64(f float64) uint64 {
	bits := math.Float64bits(f)
	sign := bits >> 63
	exp := (bits >> 52) & 0x7ff
	frac := bits & 0xfffffffffffff

	if exp == 0x7ff {
		if frac != 0 {
			if (frac & (1 << 51)) != 0 {
				return 1 << 9 // quiet NaN
			}
			return 1 << 8 // signaling NaN
		}
		if sign != 0 {
			return 1 << 0 // -infinity
		}
		return 1 << 7 // +infinity
	}

	if exp == 0 {
		if frac == 0 {
			if sign != 0 {
				return 1 << 3 // -0
			}
			return 1 << 4 // +0
		}
		if sign != 0 {
			return 1 << 2 // negative subnormal
		}
		return 1 << 5 // positive subnormal
	}

	if sign != 0 {
		return 1 << 1 // negative normal
	}
	return 1 << 6 // positive normal
}
