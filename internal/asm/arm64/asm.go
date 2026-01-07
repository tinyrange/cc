package arm64

import (
	"fmt"
	"sync/atomic"

	"github.com/tinyrange/cc/internal/asm"
	"github.com/tinyrange/cc/internal/linux/defs"
	arm64defs "github.com/tinyrange/cc/internal/linux/defs/arm64"
)

func requireContext(ctx asm.Context) (*Context, error) {
	if c, ok := ctx.(*Context); ok {
		return c, nil
	}
	return nil, fmt.Errorf("arm64 asm: unsupported context %T", ctx)
}

// LoadConstantBytes binds the provided data to the named constant variable.
func LoadConstantBytes(target asm.Variable, data []byte) asm.Fragment {
	return fragmentFunc(func(ctx asm.Context) error {
		c, err := requireContext(ctx)
		if err != nil {
			return err
		}
		c.AddConstant(target, data)
		return nil
	})
}

// ReserveZero allocates zero-initialized space in the BSS section.
func ReserveZero(target asm.Variable, size int) asm.Fragment {
	return fragmentFunc(func(ctx asm.Context) error {
		c, err := requireContext(ctx)
		if err != nil {
			return err
		}
		c.AddZeroConstant(target, size)
		return nil
	})
}

// LoadAddress loads the absolute address of the provided constant into dst.
func LoadAddress(dst Reg, constant asm.Variable) asm.Fragment {
	return fragmentFunc(func(ctx asm.Context) error {
		if err := dst.validate(); err != nil {
			return err
		}
		c, err := requireContext(ctx)
		if err != nil {
			return err
		}
		loc, ok := c.ConstantLocation(constant)
		if !ok {
			return fmt.Errorf("arm64 asm: constant %v not defined", constant)
		}
		return loadPointerToLocation(c, dst, loc)
	})
}

func MovImmediate(dst Reg, value int64) asm.Fragment {
	return fragmentFunc(func(ctx asm.Context) error {
		if err := dst.validate(); err != nil {
			return err
		}
		c, err := requireContext(ctx)
		if err != nil {
			return err
		}
		switch dst.size {
		case size64:
			return emitMovImmediate(c, dst, uint64(value))
		case size32:
			return emitMovImmediate(c, Reg64(dst.id), uint64(uint32(value)))
		case size16:
			return emitMovImmediate(c, Reg64(dst.id), uint64(uint16(value)))
		case size8:
			return emitMovImmediate(c, Reg64(dst.id), uint64(uint8(value)))
		default:
			return fmt.Errorf("arm64 asm: unsupported immediate width %d", dst.size)
		}
	})
}

func emitMovImmediate(c *Context, dst Reg, value uint64) error {
	first := true
	for shift := uint32(0); shift < 64; shift += 16 {
		chunk := uint16((value >> shift) & 0xFFFF)
		if first {
			word, err := encodeMovz(dst, chunk, shift)
			if err != nil {
				return err
			}
			c.emit32(word)
			first = false
			continue
		}
		if chunk == 0 {
			continue
		}
		word, err := encodeMovk(dst, chunk, shift)
		if err != nil {
			return err
		}
		c.emit32(word)
	}
	if first {
		word, err := encodeMovz(dst, 0, 0)
		if err != nil {
			return err
		}
		c.emit32(word)
	}
	return nil
}

func MovReg(dst, src Reg) asm.Fragment {
	return fragmentFunc(func(ctx asm.Context) error {
		if err := dst.validate(); err != nil {
			return err
		}
		if err := src.validate(); err != nil {
			return err
		}
		c, err := requireContext(ctx)
		if err != nil {
			return err
		}
		word, err := encodeMoveReg(dst, src)
		if err != nil {
			return err
		}
		c.emit32(word)
		return nil
	})
}

// MovRegFromSP copies the stack pointer into dst. ARM64 treats the SP
// register differently from general-purpose registers, so MOV cannot use it
// as a source operand.
func MovRegFromSP(dst Reg) asm.Fragment {
	return fragmentFunc(func(ctx asm.Context) error {
		if err := dst.validate(); err != nil {
			return err
		}
		c, err := requireContext(ctx)
		if err != nil {
			return err
		}
		word, err := encodeAddImm64(dst, Reg64(SP), 0)
		if err != nil {
			return err
		}
		c.emit32(word)
		return nil
	})
}

func AddRegImm(dst Reg, value int32) asm.Fragment {
	return fragmentFunc(func(ctx asm.Context) error {
		if err := dst.validate(); err != nil {
			return err
		}
		c, err := requireContext(ctx)
		if err != nil {
			return err
		}
		return emitAddRegImm(c, dst, value)
	})
}

func AddRegReg(dst, src Reg) asm.Fragment {
	return fragmentFunc(func(ctx asm.Context) error {
		if err := dst.validate(); err != nil {
			return err
		}
		if err := src.validate(); err != nil {
			return err
		}
		c, err := requireContext(ctx)
		if err != nil {
			return err
		}
		word, err := encodeAddReg64(dst, dst, src)
		if err != nil {
			return err
		}
		c.emit32(word)
		return nil
	})
}

func SubRegReg(dst, src Reg) asm.Fragment {
	return fragmentFunc(func(ctx asm.Context) error {
		if err := dst.validate(); err != nil {
			return err
		}
		if err := src.validate(); err != nil {
			return err
		}
		c, err := requireContext(ctx)
		if err != nil {
			return err
		}
		word, err := encodeSubReg64(dst, dst, src)
		if err != nil {
			return err
		}
		c.emit32(word)
		return nil
	})
}

func CmpRegReg(left, right Reg) asm.Fragment {
	return fragmentFunc(func(ctx asm.Context) error {
		if err := left.validate(); err != nil {
			return err
		}
		if err := right.validate(); err != nil {
			return err
		}
		c, err := requireContext(ctx)
		if err != nil {
			return err
		}
		word, err := encodeCmpReg64(left, right)
		if err != nil {
			return err
		}
		c.emit32(word)
		return nil
	})
}

func CmpRegImm(reg Reg, value int32) asm.Fragment {
	return fragmentFunc(func(ctx asm.Context) error {
		if value < 0 {
			return fmt.Errorf("arm64 asm: CmpRegImm negative immediates not supported")
		}
		if err := reg.validate(); err != nil {
			return err
		}
		if value > 0xFFF {
			return fmt.Errorf("arm64 asm: CmpRegImm immediate too large")
		}
		c, err := requireContext(ctx)
		if err != nil {
			return err
		}
		word, err := encodeCmpImm64(reg, uint16(value))
		if err != nil {
			return err
		}
		c.emit32(word)
		return nil
	})
}

func TestZero(reg Reg) asm.Fragment {
	return fragmentFunc(func(ctx asm.Context) error {
		if err := reg.validate(); err != nil {
			return err
		}
		c, err := requireContext(ctx)
		if err != nil {
			return err
		}
		word, err := encodeTestZero(reg)
		if err != nil {
			return err
		}
		c.emit32(word)
		return nil
	})
}

func ShlRegImm(reg Reg, amount uint32) asm.Fragment {
	return shiftRegImm(reg, amount, false)
}

func ShrRegImm(reg Reg, amount uint32) asm.Fragment {
	return shiftRegImm(reg, amount, true)
}

func shiftRegImm(reg Reg, amount uint32, right bool) asm.Fragment {
	return fragmentFunc(func(ctx asm.Context) error {
		if err := reg.validate(); err != nil {
			return err
		}
		c, err := requireContext(ctx)
		if err != nil {
			return err
		}
		word, err := encodeLogicalShift(reg, reg, amount, right)
		if err != nil {
			return err
		}
		c.emit32(word)
		return nil
	})
}

func AndRegReg(dst, src Reg) asm.Fragment {
	return fragmentFunc(func(ctx asm.Context) error {
		if err := dst.validate(); err != nil {
			return err
		}
		if err := src.validate(); err != nil {
			return err
		}
		c, err := requireContext(ctx)
		if err != nil {
			return err
		}
		word, err := encodeAndReg(dst, dst, src)
		if err != nil {
			return err
		}
		c.emit32(word)
		return nil
	})
}

func OrRegReg(dst, src Reg) asm.Fragment {
	return fragmentFunc(func(ctx asm.Context) error {
		if err := dst.validate(); err != nil {
			return err
		}
		if err := src.validate(); err != nil {
			return err
		}
		c, err := requireContext(ctx)
		if err != nil {
			return err
		}
		word, err := encodeOrrReg(dst, dst, src)
		if err != nil {
			return err
		}
		c.emit32(word)
		return nil
	})
}

func XorRegReg(dst, src Reg) asm.Fragment {
	return fragmentFunc(func(ctx asm.Context) error {
		if err := dst.validate(); err != nil {
			return err
		}
		if err := src.validate(); err != nil {
			return err
		}
		c, err := requireContext(ctx)
		if err != nil {
			return err
		}
		word, err := encodeEorReg(dst, dst, src)
		if err != nil {
			return err
		}
		c.emit32(word)
		return nil
	})
}

func MovToMemory64(mem Memory, src Reg) asm.Fragment {
	return storeHelper(mem, src, literal64)
}

func MovToMemory32(mem Memory, src Reg) asm.Fragment {
	return storeHelper(mem, src, literal32)
}

func MovToMemory8(mem Memory, src Reg) asm.Fragment {
	return storeHelper(mem, src, literal8)
}

func storeHelper(mem Memory, src Reg, width literalWidth) asm.Fragment {
	return fragmentFunc(func(ctx asm.Context) error {
		if err := src.validate(); err != nil {
			return err
		}
		c, err := requireContext(ctx)
		if err != nil {
			return err
		}
		word, err := encodeLoadStoreUnsigned(src, mem, width, true)
		if err != nil {
			return err
		}
		c.emit32(word)
		return nil
	})
}

func MovFromMemory64(dst Reg, mem Memory) asm.Fragment {
	return loadHelper(dst, mem, literal64)
}

func MovFromMemory32(dst Reg, mem Memory) asm.Fragment {
	return loadHelper(dst, mem, literal32)
}

func MovFromMemory8(dst Reg, mem Memory) asm.Fragment {
	return loadHelper(dst, mem, literal8)
}

func loadHelper(dst Reg, mem Memory, width literalWidth) asm.Fragment {
	return fragmentFunc(func(ctx asm.Context) error {
		if err := dst.validate(); err != nil {
			return err
		}
		c, err := requireContext(ctx)
		if err != nil {
			return err
		}
		word, err := encodeLoadStoreUnsigned(dst, mem, width, false)
		if err != nil {
			return err
		}
		c.emit32(word)
		return nil
	})
}

func MovToMemory(mem Memory, src Reg) asm.Fragment {
	switch src.size {
	case size64:
		return storeHelper(mem, src, literal64)
	case size32:
		return storeHelper(mem, src, literal32)
	case size16:
		return storeHelper(mem, Reg32(src.id), literal16)
	case size8:
		return storeHelper(mem, Reg32(src.id), literal8)
	default:
		return fragmentFunc(func(asm.Context) error {
			return fmt.Errorf("arm64 asm: unsupported store width %d", src.size)
		})
	}
}

func MovFromMemory(dst Reg, mem Memory) asm.Fragment {
	switch dst.size {
	case size64:
		return loadHelper(dst, mem, literal64)
	case size32:
		return loadHelper(dst, mem, literal32)
	case size16:
		return loadHelper(Reg32(dst.id), mem, literal16)
	case size8:
		return loadHelper(Reg32(dst.id), mem, literal8)
	default:
		return fragmentFunc(func(asm.Context) error {
			return fmt.Errorf("arm64 asm: unsupported load width %d", dst.size)
		})
	}
}

func MovZX8(dst Reg, mem Memory) asm.Fragment {
	return loadHelper(Reg32(dst.id), mem, literal8)
}

func MovZX16(dst Reg, mem Memory) asm.Fragment {
	return loadHelper(Reg32(dst.id), mem, literal16)
}

func Jump(label asm.Label) asm.Fragment {
	return fragmentFunc(func(ctx asm.Context) error {
		c, err := requireContext(ctx)
		if err != nil {
			return err
		}
		c.emitBranch(label)
		return nil
	})
}

func JumpIfEqual(label asm.Label) asm.Fragment    { return condJump(label, condEQ) }
func JumpIfNotEqual(label asm.Label) asm.Fragment { return condJump(label, condNE) }
func JumpIfZero(label asm.Label) asm.Fragment     { return condJump(label, condEQ) }
func JumpIfNotZero(label asm.Label) asm.Fragment  { return condJump(label, condNE) }
func JumpIfGreater(label asm.Label) asm.Fragment  { return condJump(label, condGT) }
func JumpIfGreaterOrEqual(label asm.Label) asm.Fragment {
	return condJump(label, condGE)
}
func JumpIfLess(label asm.Label) asm.Fragment { return condJump(label, condLT) }
func JumpIfLessOrEqual(label asm.Label) asm.Fragment {
	return condJump(label, condLE)
}
func JumpIfNegative(label asm.Label) asm.Fragment { return condJump(label, condMI) }

func condJump(label asm.Label, cond condition) asm.Fragment {
	return fragmentFunc(func(ctx asm.Context) error {
		c, err := requireContext(ctx)
		if err != nil {
			return err
		}
		c.emitCondBranch(label, cond)
		return nil
	})
}

func Call(label asm.Label) asm.Fragment {
	return fragmentFunc(func(ctx asm.Context) error {
		c, err := requireContext(ctx)
		if err != nil {
			return err
		}
		c.emitCall(label)
		return nil
	})
}

func CallReg(target Reg) asm.Fragment {
	return fragmentFunc(func(ctx asm.Context) error {
		if err := target.validate(); err != nil {
			return err
		}
		c, err := requireContext(ctx)
		if err != nil {
			return err
		}
		word := uint32(0xD63F0000 | (uint32(target.id) << 5))
		c.emit32(word)
		return nil
	})
}

func Ret() asm.Fragment {
	return fragmentFunc(func(ctx asm.Context) error {
		c, err := requireContext(ctx)
		if err != nil {
			return err
		}
		c.emit32(0xD65F03C0)
		return nil
	})
}

// ISB emits an Instruction Synchronization Barrier.
// This is required after modifying code in memory before executing it.
func ISB() asm.Fragment {
	return fragmentFunc(func(ctx asm.Context) error {
		c, err := requireContext(ctx)
		if err != nil {
			return err
		}
		// ISB SY: 0xD5033FDF
		c.emit32(0xD5033FDF)
		return nil
	})
}

// DSB emits a Data Synchronization Barrier (full system).
// This ensures all memory operations complete before continuing.
func DSB() asm.Fragment {
	return fragmentFunc(func(ctx asm.Context) error {
		c, err := requireContext(ctx)
		if err != nil {
			return err
		}
		// DSB SY: 0xD5033F9F
		c.emit32(0xD5033F9F)
		return nil
	})
}

func Syscall(number defs.Syscall, args ...asm.Value) asm.Fragment {
	return &syscallFragment{number: number, args: args}
}

func Hvc() asm.Fragment {
	return fragmentFunc(func(ctx asm.Context) error {
		c, err := requireContext(ctx)
		if err != nil {
			return err
		}
		c.emit32(0xD4000002) // hvc #0
		return nil
	})
}

func UseRegister(v asm.Variable) asm.Value {
	return asm.Register(v)
}

func SetVectorBase(src Reg) asm.Fragment {
	return fragmentFunc(func(ctx asm.Context) error {
		if err := src.validate(); err != nil {
			return err
		}
		c, err := requireContext(ctx)
		if err != nil {
			return err
		}
		word, err := encodeMSR(systemRegVBAR, src)
		if err != nil {
			return err
		}
		c.emit32(word)
		return nil
	})
}

func SyscallWrite(fd asm.Value, buf asm.Value, count asm.Value) asm.Fragment {
	return Syscall(defs.SYS_WRITE, fd, buf, count)
}

func SyscallWriteString(fd asm.Value, s string) asm.Fragment {
	return SyscallWrite(fd, asm.LiteralBytes([]byte(s)), asm.Immediate(len(s)))
}

type syscallFragment struct {
	number defs.Syscall
	args   []asm.Value
}

func (s *syscallFragment) Emit(ctx asm.Context) error {
	c, err := requireContext(ctx)
	if err != nil {
		return err
	}
	num, ok := arm64defs.SyscallMap[s.number]
	if !ok {
		return fmt.Errorf("arm64 asm: unknown syscall number %d", s.number)
	}
	if err := emitMovImmediate(c, Reg64(X8), uint64(num)); err != nil {
		return err
	}
	argRegs := []Reg{
		Reg64(X0),
		Reg64(X1),
		Reg64(X2),
		Reg64(X3),
		Reg64(X4),
		Reg64(X5),
	}
	if len(s.args) > len(argRegs) {
		return fmt.Errorf("arm64 asm: too many syscall arguments (%d)", len(s.args))
	}
	for idx, value := range s.args {
		if err := moveValueIntoReg(c, argRegs[idx], value); err != nil {
			return err
		}
	}
	c.emit32(0xD4000001) // svc #0
	return nil
}

func loadPointerToLocation(c *Context, dst Reg, loc constantLocation) error {
	literalOffset := c.addPointerLiteral(loc)
	word, err := encodeLiteralLoad(dst, literal64)
	if err != nil {
		return err
	}
	pos := c.emit32(word)
	c.addLiteralLoad(pos, literalOffset, literal64)
	return nil
}

func moveValueIntoReg(c *Context, dst Reg, value asm.Value) error {
	switch v := value.(type) {
	case asm.Immediate:
		return emitMovImmediate(c, dst, uint64(int64(v)))
	case int:
		return emitMovImmediate(c, dst, uint64(int64(v)))
	case int32:
		return emitMovImmediate(c, dst, uint64(int64(v)))
	case int64:
		return emitMovImmediate(c, dst, uint64(v))
	case uint64:
		return emitMovImmediate(c, dst, v)
	case asm.Variable:
		if loc, ok := c.ConstantLocation(v); ok {
			return loadPointerToLocation(c, dst, loc)
		}
		return moveRegisterValue(c, asm.Variable(dst.id), v)
	case asm.Register:
		return moveRegisterValue(c, asm.Variable(dst.id), asm.Variable(v))
	case asm.LiteralValue:
		offset := c.literalOffset(v)
		loc := constantLocation{section: sectionLiteral, offset: offset}
		return loadPointerToLocation(c, dst, loc)
	default:
		return fmt.Errorf("arm64 asm: unsupported value %T", value)
	}
}

func moveRegisterValue(c *Context, dst, src asm.Variable) error {
	if dst == src {
		return nil
	}
	word, err := encodeMoveReg(Reg64(dst), Reg64(src))
	if err != nil {
		return err
	}
	c.emit32(word)
	return nil
}

func emitAddRegImm(c *Context, reg Reg, value int32) error {
	if value == 0 {
		return nil
	}
	remaining := value
	for remaining != 0 {
		var chunk int32
		if remaining > 0 {
			if remaining > 0xFFF {
				chunk = 0xFFF
			} else {
				chunk = remaining
			}
			word, err := encodeAddImm64(reg, reg, uint16(chunk))
			if err != nil {
				return err
			}
			c.emit32(word)
			remaining -= chunk
			continue
		}
		if remaining < -0xFFF {
			chunk = -0xFFF
		} else {
			chunk = remaining
		}
		word, err := encodeSubImm64(reg, reg, uint16(-chunk))
		if err != nil {
			return err
		}
		c.emit32(word)
		remaining -= chunk
	}
	return nil
}

var printfLabelCounter uint64

const (
	atFdcwd = -100
	oWronly = 1
)

func writeStdoutWithKmsgFallback(buf asm.Value, count asm.Value) asm.Fragment {
	id := atomic.AddUint64(&printfLabelCounter, 1)
	errLabel := asm.Label(fmt.Sprintf("__arm64_printf_write_err_%d", id))
	doneLabel := asm.Label(fmt.Sprintf("__arm64_printf_write_done_%d", id))
	fdReg := Reg64(X23)

	return asm.Group{
		SyscallWrite(asm.Immediate(1), buf, count),
		TestZero(Reg64(X0)),
		JumpIfNegative(errLabel),
		Jump(doneLabel),
		asm.MarkLabel(errLabel),
		Syscall(defs.SYS_OPENAT,
			asm.Immediate(atFdcwd),
			asm.String("/dev/kmsg"),
			asm.Immediate(oWronly),
			asm.Immediate(0),
		),
		TestZero(Reg64(X0)),
		JumpIfNegative(doneLabel),
		MovReg(fdReg, Reg64(X0)),
		SyscallWriteString(UseRegister(fdReg.id), "printf error: using fallback kmsg\n"),
		SyscallWrite(UseRegister(fdReg.id), buf, count),
		Syscall(defs.SYS_CLOSE, UseRegister(fdReg.id)),
		asm.MarkLabel(doneLabel),
	}
}

// Printf writes a formatted string (currently only supporting %x) to stdout.
func Printf(format string, args ...asm.Value) asm.Fragment {
	const (
		regWidth   = 8
		bufferSize = 32
		hexDigits  = 16
	)

	type formatPart struct {
		text     string
		argIndex int
	}

	parts := make([]formatPart, 0)
	literal := make([]byte, 0, len(format))
	flushLiteral := func() {
		if len(literal) == 0 {
			return
		}
		parts = append(parts, formatPart{text: string(literal), argIndex: -1})
		literal = literal[:0]
	}

	placeholderCount := 0
	for i := 0; i < len(format); i++ {
		ch := format[i]
		if ch != '%' {
			literal = append(literal, ch)
			continue
		}
		if i+1 >= len(format) {
			panic("arm64 asm.Printf: trailing % at end of format string")
		}
		next := format[i+1]
		switch next {
		case '%':
			literal = append(literal, '%')
			i++
		case 'x':
			flushLiteral()
			parts = append(parts, formatPart{argIndex: placeholderCount})
			placeholderCount++
			i++
		default:
			panic(fmt.Sprintf("arm64 asm.Printf: unsupported format specifier %%%c", next))
		}
	}
	flushLiteral()

	if placeholderCount != len(args) {
		panic(fmt.Sprintf("arm64 asm.Printf: format expects %d args, got %d", placeholderCount, len(args)))
	}
	if len(parts) == 0 {
		return asm.Group{}
	}

	saved := []asm.Variable{
		X0, X1, X2, X3, X4, X5, X6, X7,
		X8, X9, X10, X11, X12, X13, X14,
		X15, X16, X17, X18, X19, X20, X21, X22, X23,
		X29, X30,
	}

	// Stack Alignment Check:
	// 26 registers * 8 bytes = 208 bytes.
	// 208 + 32 (buffer) = 240 bytes.
	// 240 is divisible by 16 (required for SP alignment on ARM64).
	regAreaSize := len(saved) * regWidth
	totalStack := int32(regAreaSize + bufferSize)
	bufferOffset := int32(regAreaSize)
	savedOffsets := make(map[asm.Variable]int32, len(saved))

	frags := make([]asm.Fragment, 0, len(parts)*10+len(saved)*2+8)

	// Allocate stack space
	frags = append(frags, AddRegImm(Reg64(SP), -totalStack))

	// Save registers
	for idx, reg := range saved {
		offset := int32(idx * regWidth)
		savedOffsets[reg] = offset
		frags = append(frags, MovToMemory(Mem(Reg64(SP)).WithDisp(offset), Reg64(reg)))
	}

	// Setup Frame Pointer (optional but recommended for debugging/unwinding)
	// Effectively: MOV FP, SP (after adjusting for where FP is saved)
	// If you want strict ABI compliance, you would set X29 to point to the saved X29 location here.

	bufferReg := Reg64(X20)
	valueReg := Reg64(X8)
	tmp0 := Reg64(X9)
	tmp1 := Reg64(X10)
	tmp2 := Reg64(X11)
	tmp3 := Reg64(X12)
	tmp4 := Reg64(X13)
	indexReg := Reg64(X14)

	frags = append(frags,
		MovRegFromSP(bufferReg),
		AddRegImm(bufferReg, bufferOffset),
	)

	loadArg := func(val asm.Value) []asm.Fragment {
		switch v := val.(type) {
		case asm.Immediate:
			return []asm.Fragment{MovImmediate(valueReg, int64(v))}
		case int:
			return []asm.Fragment{MovImmediate(valueReg, int64(v))}
		case int32:
			return []asm.Fragment{MovImmediate(valueReg, int64(v))}
		case int64:
			return []asm.Fragment{MovImmediate(valueReg, v)}
		case uint64:
			return []asm.Fragment{MovImmediate(valueReg, int64(v))}
		case Reg:
			if offset, ok := savedOffsets[v.id]; ok {
				return []asm.Fragment{MovFromMemory(valueReg, Mem(Reg64(SP)).WithDisp(offset))}
			}
			return []asm.Fragment{MovReg(valueReg, Reg64(v.id))}
		case asm.Variable:
			if offset, ok := savedOffsets[v]; ok {
				return []asm.Fragment{MovFromMemory(valueReg, Mem(Reg64(SP)).WithDisp(offset))}
			}
			return []asm.Fragment{MovReg(valueReg, Reg64(v))}
		case asm.Register:
			id := asm.Variable(v)
			if offset, ok := savedOffsets[id]; ok {
				return []asm.Fragment{MovFromMemory(valueReg, Mem(Reg64(SP)).WithDisp(offset))}
			}
			return []asm.Fragment{MovReg(valueReg, Reg64(id))}
		default:
			panic(fmt.Sprintf("arm64 asm.Printf: unsupported argument type %T", v))
		}
	}

	for _, part := range parts {
		if part.argIndex == -1 {
			if len(part.text) == 0 {
				continue
			}
			frags = append(frags, writeStdoutWithKmsgFallback(
				asm.LiteralBytes([]byte(part.text)),
				asm.Immediate(len(part.text)),
			))
			continue
		}

		arg := args[part.argIndex]
		frags = append(frags, loadArg(arg)...)

		id := atomic.AddUint64(&printfLabelCounter, 1)
		loopLabel := asm.Label(fmt.Sprintf("__arm64_printf_hex_loop_%d", id))
		storeLabel := asm.Label(fmt.Sprintf("__arm64_printf_hex_store_%d", id))
		lessLabel := asm.Label(fmt.Sprintf("__arm64_printf_hex_less_%d", id))
		trimLoop := asm.Label(fmt.Sprintf("__arm64_printf_trim_loop_%d", id))
		trimDone := asm.Label(fmt.Sprintf("__arm64_printf_trim_done_%d", id))

		frags = append(frags,
			MovReg(tmp0, bufferReg),
			AddRegImm(tmp0, hexDigits-1),
			MovImmediate(tmp3, hexDigits),
			asm.MarkLabel(loopLabel),
			MovReg(tmp1, valueReg),
			MovImmediate(tmp2, 0xF),
			AndRegReg(tmp1, tmp2),
			CmpRegImm(tmp1, 10),
			JumpIfLess(lessLabel),
			MovImmediate(tmp2, int64('a'-10)),
			AddRegReg(tmp1, tmp2),
			Jump(storeLabel),
			asm.MarkLabel(lessLabel),
			MovImmediate(tmp2, int64('0')),
			AddRegReg(tmp1, tmp2),
			asm.MarkLabel(storeLabel),
			MovToMemory8(Mem(tmp0), Reg32(tmp1.id)),
			ShrRegImm(valueReg, 4),
			AddRegImm(tmp0, -1),
			AddRegImm(tmp3, -1),
			CmpRegImm(tmp3, 0),
			JumpIfGreater(loopLabel),

			MovImmediate(indexReg, 0),
			asm.MarkLabel(trimLoop),
			CmpRegImm(indexReg, hexDigits-1),
			JumpIfEqual(trimDone),
		)

		// Load current byte into tmp1.
		frags = append(frags,
			MovReg(tmp0, bufferReg),
			MovReg(tmp2, indexReg),
			AddRegReg(tmp0, tmp2),
			MovZX8(tmp1, Mem(tmp0)),
			CmpRegImm(tmp1, int32('0')),
			JumpIfNotEqual(trimDone),
			AddRegImm(indexReg, 1),
			Jump(trimLoop),
			asm.MarkLabel(trimDone),
			MovReg(tmp0, bufferReg),
			MovReg(tmp2, indexReg),
			AddRegReg(tmp0, tmp2),
			MovImmediate(tmp3, hexDigits),
			MovReg(tmp4, indexReg),
			SubRegReg(tmp3, tmp4),
			writeStdoutWithKmsgFallback(UseRegister(tmp0.id), UseRegister(tmp3.id)),
		)
	}

	// Restore registers
	for idx := len(saved) - 1; idx >= 0; idx-- {
		offset := int32(idx * regWidth)
		reg := saved[idx]
		frags = append(frags, MovFromMemory(Reg64(reg), Mem(Reg64(SP)).WithDisp(offset)))
	}

	// Deallocate stack
	frags = append(frags, AddRegImm(Reg64(SP), totalStack))

	return asm.Group(frags)
}
