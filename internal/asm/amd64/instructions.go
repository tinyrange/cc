package amd64

import (
	"fmt"

	"github.com/tinyrange/cc/internal/asm"
)

func MovImmediate(dst Reg, value int64) asm.Fragment {
	return fragmentFunc(func(ctx asm.Context) error {
		bytes, err := encodeMovRegImm(dst, value)
		if err != nil {
			return err
		}
		ctx.EmitBytes(bytes)
		return nil
	})
}

func MovReg(dst, src Reg) asm.Fragment {
	return fragmentFunc(func(ctx asm.Context) error {
		bytes, err := encodeMovRegReg(dst, src)
		if err != nil {
			return err
		}
		ctx.EmitBytes(bytes)
		return nil
	})
}

func MovToMemory(mem Memory, src Reg) asm.Fragment {
	return fragmentFunc(func(ctx asm.Context) error {
		bytes, err := encodeMovMemReg(mem, src)
		if err != nil {
			return err
		}
		ctx.EmitBytes(bytes)
		return nil
	})
}

func MovFromMemory(dst Reg, mem Memory) asm.Fragment {
	return fragmentFunc(func(ctx asm.Context) error {
		bytes, err := encodeMovRegMem(dst, mem)
		if err != nil {
			return err
		}
		ctx.EmitBytes(bytes)
		return nil
	})
}

func CallReg(target Reg) asm.Fragment {
	return fragmentFunc(func(ctx asm.Context) error {
		bytes, err := encodeCallReg(target)
		if err != nil {
			return err
		}
		ctx.EmitBytes(bytes)
		return nil
	})
}

func MovZX8(dst Reg, mem Memory) asm.Fragment {
	return fragmentFunc(func(ctx asm.Context) error {
		bytes, err := encodeMovZXRegMem(dst, mem, size8)
		if err != nil {
			return err
		}
		ctx.EmitBytes(bytes)
		return nil
	})
}

func MovZX16(dst Reg, mem Memory) asm.Fragment {
	return fragmentFunc(func(ctx asm.Context) error {
		bytes, err := encodeMovZXRegMem(dst, mem, size16)
		if err != nil {
			return err
		}
		ctx.EmitBytes(bytes)
		return nil
	})
}

func MovStoreImm8(mem Memory, value byte) asm.Fragment {
	return fragmentFunc(func(ctx asm.Context) error {
		bytes, err := encodeMovMemImm8(mem, value)
		if err != nil {
			return err
		}
		ctx.EmitBytes(bytes)
		return nil
	})
}

func AddRegImm(reg Reg, value int32) asm.Fragment {
	return fragmentFunc(func(ctx asm.Context) error {
		bytes, err := encodeAddRegImm(reg, value)
		if err != nil {
			return err
		}
		ctx.EmitBytes(bytes)
		return nil
	})
}

func AddRegReg(dst, src Reg) asm.Fragment {
	return fragmentFunc(func(ctx asm.Context) error {
		bytes, err := encodeAddRegReg(dst, src)
		if err != nil {
			return err
		}
		ctx.EmitBytes(bytes)
		return nil
	})
}

func SubRegReg(dst, src Reg) asm.Fragment {
	return fragmentFunc(func(ctx asm.Context) error {
		bytes, err := encodeSubRegReg(dst, src)
		if err != nil {
			return err
		}
		ctx.EmitBytes(bytes)
		return nil
	})
}

func OrRegReg(dst, src Reg) asm.Fragment {
	return fragmentFunc(func(ctx asm.Context) error {
		bytes, err := encodeOrRegReg(dst, src)
		if err != nil {
			return err
		}
		ctx.EmitBytes(bytes)
		return nil
	})
}

func CmpRegImm(reg Reg, value int32) asm.Fragment {
	return fragmentFunc(func(ctx asm.Context) error {
		bytes, err := encodeCmpRegImm(reg, value)
		if err != nil {
			return err
		}
		ctx.EmitBytes(bytes)
		return nil
	})
}

func CmpRegReg(dst, src Reg) asm.Fragment {
	return fragmentFunc(func(ctx asm.Context) error {
		bytes, err := encodeCmpRegReg(dst, src)
		if err != nil {
			return err
		}
		ctx.EmitBytes(bytes)
		return nil
	})
}

func AndRegReg(dst, src Reg) asm.Fragment {
	return fragmentFunc(func(ctx asm.Context) error {
		bytes, err := encodeAndRegReg(dst, src)
		if err != nil {
			return err
		}
		ctx.EmitBytes(bytes)
		return nil
	})
}

func AndRegImm(reg Reg, value int32) asm.Fragment {
	return fragmentFunc(func(ctx asm.Context) error {
		bytes, err := encodeAndRegImm(reg, value)
		if err != nil {
			return err
		}
		ctx.EmitBytes(bytes)
		return nil
	})
}

func OrRegImm(reg Reg, value int32) asm.Fragment {
	return fragmentFunc(func(ctx asm.Context) error {
		bytes, err := encodeOrRegImm(reg, value)
		if err != nil {
			return err
		}
		ctx.EmitBytes(bytes)
		return nil
	})
}

func XorRegReg(dst, src Reg) asm.Fragment {
	return fragmentFunc(func(ctx asm.Context) error {
		bytes, err := encodeXorRegRegSized(dst, src)
		if err != nil {
			return err
		}
		ctx.EmitBytes(bytes)
		return nil
	})
}

func ImulRegImm(dst, src Reg, value int32) asm.Fragment {
	return fragmentFunc(func(ctx asm.Context) error {
		bytes, err := encodeImulRegImm(dst, src, value)
		if err != nil {
			return err
		}
		ctx.EmitBytes(bytes)
		return nil
	})
}

func Hlt() asm.Fragment {
	return fragmentFunc(func(ctx asm.Context) error {
		ctx.EmitBytes(encodeHlt())
		return nil
	})
}

func OutDXAL() asm.Fragment {
	return fragmentFunc(func(ctx asm.Context) error {
		ctx.EmitBytes(encodeOutDXAL())
		return nil
	})
}

func Rdmsr() asm.Fragment {
	return fragmentFunc(func(ctx asm.Context) error {
		ctx.EmitBytes(encodeRdmsr())
		return nil
	})
}

func ShrRegImm(reg Reg, count uint8) asm.Fragment {
	return fragmentFunc(func(ctx asm.Context) error {
		bytes, err := encodeShrRegImm(reg, count)
		if err != nil {
			return err
		}
		ctx.EmitBytes(bytes)
		return nil
	})
}

func ShlRegImm(reg Reg, count uint8) asm.Fragment {
	return fragmentFunc(func(ctx asm.Context) error {
		bytes, err := encodeShlRegImm(reg, count)
		if err != nil {
			return err
		}
		ctx.EmitBytes(bytes)
		return nil
	})
}

func Wrmsr() asm.Fragment {
	return fragmentFunc(func(ctx asm.Context) error {
		ctx.EmitBytes(encodeWrmsr())
		return nil
	})
}

func LoadAddress(dst Reg, variable asm.Variable) asm.Fragment {
	return fragmentFunc(func(_ctx asm.Context) error {
		ctx := _ctx.(*Context)
		if dst.size != size64 {
			return fmt.Errorf("load address requires 64-bit register")
		}
		loc, ok := ctx.ConstantLocation(variable)
		if !ok {
			return fmt.Errorf("no constant bound to variable %d", variable)
		}
		pos, err := appendMovDataPointerReg(ctx, dst, loc.offset)
		if err != nil {
			return err
		}
		ctx.addTextPatch(pos, loc)
		return nil
	})
}

func JumpIfNotEqual(label asm.Label) asm.Fragment {
	return &jump{label: label, kind: jumpNotEqual}
}

func JumpIfNotZero(label asm.Label) asm.Fragment {
	return &jump{label: label, kind: jumpNotZero}
}

func JumpIfAboveOrEqual(label asm.Label) asm.Fragment {
	return &jump{label: label, kind: jumpAboveOrEqual}
}

func JumpIfBelowOrEqual(label asm.Label) asm.Fragment {
	return &jump{label: label, kind: jumpBelowOrEqual}
}

func JumpIfEqual(label asm.Label) asm.Fragment {
	return &jump{label: label, kind: jumpEqual}
}

func JumpIfLess(label asm.Label) asm.Fragment {
	return &jump{label: label, kind: jumpLess}
}

func JumpIfAbove(label asm.Label) asm.Fragment {
	return &jump{label: label, kind: jumpAbove}
}

func JumpIfGreater(label asm.Label) asm.Fragment {
	return &jump{label: label, kind: jumpGreater}
}
