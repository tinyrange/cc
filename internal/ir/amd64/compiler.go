package amd64

import (
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"math"
	"sort"

	"github.com/tinyrange/cc/internal/asm"
	"github.com/tinyrange/cc/internal/asm/amd64"
	"github.com/tinyrange/cc/internal/ir"
)

const stackAlignment = 16

var paramRegisters = []asm.Variable{amd64.RDI, amd64.RSI, amd64.RDX, amd64.RCX, amd64.R8, amd64.R9}

var initialFreeRegisters = []asm.Variable{
	amd64.RAX,
	amd64.RCX,
	amd64.RDX,
	amd64.R8,
	amd64.R9,
	amd64.R10,
	amd64.R11,
}

var syscallArgRegisters = []asm.Variable{
	amd64.RDI,
	amd64.RSI,
	amd64.RDX,
	amd64.R10,
	amd64.R8,
	amd64.R9,
}

type compiler struct {
	method       ir.Method
	fragments    asm.Group
	varOffsets   map[string]int32
	frameSize    int32
	freeRegs     []asm.Variable
	usedRegs     map[asm.Variable]bool
	paramIndex   int
	labels       map[string]asm.Label
	labelCounter int
	slotStack    []ir.StackSlotContext
}

func Compile(method ir.Method) (asm.Fragment, error) {
	c, err := newCompiler(method)
	if err != nil {
		return nil, err
	}
	if c.frameSize > 0 {
		c.emit(amd64.AddRegImm(amd64.Reg64(amd64.RSP), -c.frameSize))
	}
	if err := c.compileBlock(ir.Block(method)); err != nil {
		return nil, err
	}
	if c.frameSize > 0 {
		c.emit(amd64.AddRegImm(amd64.Reg64(amd64.RSP), c.frameSize))
	}
	c.emit(amd64.Ret())
	return c.fragments, nil
}

func MustCompile(method ir.Method) asm.Fragment {
	frag, err := Compile(method)
	if err != nil {
		panic(err)
	}
	return frag
}

func newCompiler(method ir.Method) (*compiler, error) {
	vars := make(map[string]struct{})
	collectVariables(ir.Block(method), vars)
	delete(vars, "")

	names := make([]string, 0, len(vars))
	for name := range vars {
		names = append(names, name)
	}
	sort.Strings(names)

	offsets := make(map[string]int32, len(names))
	for idx, name := range names {
		offsets[name] = int32(idx) * 8
	}

	frameSize := alignTo(int32(len(names))*8, stackAlignment)

	return &compiler{
		method:     method,
		varOffsets: offsets,
		frameSize:  frameSize,
		freeRegs:   append([]asm.Variable(nil), initialFreeRegisters...),
		usedRegs:   make(map[asm.Variable]bool),
		labels:     make(map[string]asm.Label),
	}, nil
}

func syscallPreferredRegisters(index int, argRegs []asm.Variable) []asm.Variable {
	if index < 0 || index >= len(argRegs) {
		return nil
	}

	avoid := make(map[asm.Variable]bool, index)
	for i := 0; i < index; i++ {
		avoid[argRegs[i]] = true
	}

	target := argRegs[index]
	base := []asm.Variable{
		target,
		amd64.R11,
		amd64.RCX,
		amd64.R9,
		amd64.R8,
		amd64.RDX,
		amd64.R10,
		amd64.RSI,
		amd64.RDI,
	}

	preferred := make([]asm.Variable, 0, len(base))
	seen := make(map[asm.Variable]bool, len(base))

	for _, reg := range base {
		if seen[reg] || avoid[reg] {
			seen[reg] = true
			continue
		}
		preferred = append(preferred, reg)
		seen[reg] = true
	}

	for _, reg := range base {
		if seen[reg] {
			continue
		}
		preferred = append(preferred, reg)
		seen[reg] = true
	}

	return preferred
}

func (c *compiler) emit(frags ...asm.Fragment) {
	c.fragments = append(c.fragments, frags...)
}

func (c *compiler) compileBlock(block ir.Block) error {
	for _, frag := range block {
		if err := c.compileFragment(frag); err != nil {
			return err
		}
	}
	return nil
}

func (c *compiler) compileFragment(f ir.Fragment) error {
	switch frag := f.(type) {
	case nil:
		return nil
	case ir.Block:
		return c.compileBlock(frag)
	case ir.Method:
		return c.compileBlock(ir.Block(frag))
	case ir.DeclareParam:
		return c.compileDeclareParam(string(frag))
	case ir.AssignFragment:
		return c.compileAssign(frag)
	case ir.SyscallFragment:
		_, err := c.compileSyscall(frag, false)
		return err
	case ir.StackSlotFragment:
		return c.compileStackSlot(frag)
	case ir.IfFragment:
		return c.compileIf(frag)
	case ir.GotoFragment:
		return c.compileGoto(frag)
	case ir.LabelFragment:
		return c.compileLabelBlock(frag)
	case ir.Label:
		return c.compileLabel(frag)
	case ir.ReturnFragment:
		return c.compileReturn(frag)
	case ir.PrintfFragment:
		return c.compilePrintf(frag)
	case ir.CallFragment:
		return c.compileCall(frag)
	case ir.ConstantBytesFragment:
		c.emit(amd64.LoadConstantBytes(frag.Target, frag.Data))
		return nil
	case ir.ConstantPointerFragment:
		return fmt.Errorf("ir: constant pointer fragment cannot be emitted directly")
	case asm.Fragment:
		c.emit(frag)
		return nil
	default:
		return fmt.Errorf("ir: unsupported fragment %T", f)
	}
}

func (c *compiler) compileDeclareParam(name string) error {
	if name == "" {
		return fmt.Errorf("ir: empty parameter name")
	}
	if _, ok := c.varOffsets[name]; !ok {
		return fmt.Errorf("ir: parameter %q not referenced", name)
	}
	if c.paramIndex >= len(paramRegisters) {
		return fmt.Errorf("ir: too many parameters (max %d)", len(paramRegisters))
	}
	reg := paramRegisters[c.paramIndex]
	c.paramIndex++

	mem, err := c.stackSlotMem(name)
	if err != nil {
		return err
	}
	c.emit(amd64.MovToMemory(mem, amd64.Reg64(reg)))
	c.freeReg(reg)
	return nil
}

func (c *compiler) compileAssign(assign ir.AssignFragment) error {
	switch src := assign.Src.(type) {
	case ir.SyscallFragment:
		reg, err := c.compileSyscall(src, true)
		if err != nil {
			return err
		}
		if err := c.storeValue(assign.Dst, reg, ir.Width64); err != nil {
			return err
		}
		c.freeReg(reg)
		return nil
	default:
		reg, width, err := c.evalValue(assign.Src)
		if err != nil {
			return err
		}
		if err := c.storeValue(assign.Dst, reg, width); err != nil {
			return err
		}
		c.freeReg(reg)
		return nil
	}
}

func (c *compiler) compileIf(f ir.IfFragment) error {
	trueLabel := c.newInternalLabel("if_true")
	endLabel := c.newInternalLabel("if_end")
	falseLabel := endLabel
	if f.Otherwise != nil {
		falseLabel = c.newInternalLabel("if_else")
	}

	if err := c.emitConditionJump(f.Cond, trueLabel, falseLabel); err != nil {
		return err
	}

	if f.Otherwise != nil {
		c.emit(asm.MarkLabel(falseLabel))
		if err := c.compileFragment(f.Otherwise); err != nil {
			return err
		}
		c.emit(amd64.Jump(endLabel))
	}

	c.emit(asm.MarkLabel(trueLabel))
	if err := c.compileFragment(f.Then); err != nil {
		return err
	}
	c.emit(asm.MarkLabel(endLabel))
	return nil
}

func (c *compiler) compileGoto(g ir.GotoFragment) error {
	name, err := extractLabelName(g.Label)
	if err != nil {
		return err
	}
	c.emit(amd64.Jump(c.namedLabel(name)))
	return nil
}

func (c *compiler) compileCall(f ir.CallFragment) error {
	targetReg, _, err := c.evalValue(f.Target)
	if err != nil {
		return err
	}
	c.emit(amd64.CallReg(amd64.Reg64(targetReg)))
	c.freeReg(targetReg)
	if f.Result != "" {
		mem, err := c.stackSlotMem(string(f.Result))
		if err != nil {
			return err
		}
		c.emit(amd64.MovToMemory(mem, amd64.Reg64(amd64.RAX)))
	}
	return nil
}

func (c *compiler) compileLabelBlock(f ir.LabelFragment) error {
	c.emit(asm.MarkLabel(c.namedLabel(string(f.Label))))
	return c.compileBlock(f.Block)
}

func (c *compiler) compileLabel(label ir.Label) error {
	c.emit(asm.MarkLabel(c.namedLabel(string(label))))
	return nil
}

func (c *compiler) compileReturn(ret ir.ReturnFragment) error {
	reg, _, err := c.evalValue(ret.Value)
	if err != nil {
		return err
	}
	if reg != amd64.RAX {
		c.emit(amd64.MovReg(amd64.Reg64(amd64.RAX), amd64.Reg64(reg)))
		c.freeReg(reg)
	}
	if c.frameSize > 0 {
		c.emit(amd64.AddRegImm(amd64.Reg64(amd64.RSP), c.frameSize))
	}
	c.emit(amd64.Ret())
	c.freeReg(amd64.RAX)
	return nil
}

func (c *compiler) compileStackSlot(slot ir.StackSlotFragment) error {
	if slot.Size <= 0 {
		return fmt.Errorf("ir: stack slot size must be positive")
	}
	if len(slot.Chunks) == 0 {
		return fmt.Errorf("ir: stack slot has no backing storage")
	}
	baseName := slot.Chunks[0]
	baseOffset, ok := c.varOffsets[baseName]
	if !ok {
		return fmt.Errorf("ir: unknown stack slot chunk %q", baseName)
	}

	c.slotStack = append(c.slotStack, ir.StackSlotContext{
		Id:   slot.Id,
		Base: baseOffset,
		Size: slot.Size,
	})

	if slot.Body != nil {
		if err := c.compileFragment(slot.Body); err != nil {
			c.slotStack = c.slotStack[:len(c.slotStack)-1]
			return err
		}
	}
	c.slotStack = c.slotStack[:len(c.slotStack)-1]

	return nil
}

func (c *compiler) storeValue(dst ir.Fragment, reg asm.Variable, width ir.ValueWidth) error {
	srcReg := regForWidth(reg, width)
	switch dest := dst.(type) {
	case ir.Var:
		mem, err := c.stackSlotMem(string(dest))
		if err != nil {
			return err
		}
		c.emit(amd64.MovToMemory(mem, amd64.Reg64(reg)))
		return nil
	case ir.I32Var:
		mem, err := c.stackSlotMem(string(dest.Name))
		if err != nil {
			return err
		}
		c.emit(amd64.MovToMemory(mem, amd64.Reg32(reg)))
		return nil
	case ir.StackSlotMemFragment:
		offset, err := c.stackSlotOffset(dest.SlotID, dest.Disp)
		if err != nil {
			return err
		}
		mem := amd64.Mem(amd64.Reg64(amd64.RSP)).WithDisp(offset)
		switch width {
		case ir.Width8:
			c.emit(amd64.MovToMemory(mem, amd64.Reg8(reg)))
		case ir.Width32:
			c.emit(amd64.MovToMemory(mem, amd64.Reg32(reg)))
		default:
			c.emit(amd64.MovToMemory(mem, amd64.Reg64(reg)))
		}
		return nil
	case ir.MemVar:
		baseReg, err := c.loadVar64(string(dest.Base))
		if err != nil {
			return err
		}
		disp, err := c.resolveDisp(dest.Disp)
		if err != nil {
			c.freeReg(baseReg)
			return err
		}
		mem := amd64.Mem(amd64.Reg64(baseReg)).WithDisp(disp)
		switch width {
		case ir.Width8:
			c.emit(amd64.MovToMemory(mem, amd64.Reg8(reg)))
		case ir.Width32:
			c.emit(amd64.MovToMemory(mem, amd64.Reg32(reg)))
		default:
			c.emit(amd64.MovToMemory(mem, amd64.Reg64(reg)))
		}
		c.freeReg(baseReg)
		return nil
	case ir.GlobalMem:
		baseReg, err := c.loadGlobalAddress(dest.Name)
		if err != nil {
			return err
		}
		disp, err := c.resolveDisp(dest.Disp)
		if err != nil {
			c.freeReg(baseReg)
			return err
		}
		mem := amd64.Mem(amd64.Reg64(baseReg)).WithDisp(disp)
		switch width {
		case ir.Width8:
			c.emit(amd64.MovToMemory(mem, amd64.Reg8(reg)))
		case ir.Width32:
			c.emit(amd64.MovToMemory(mem, amd64.Reg32(reg)))
		default:
			c.emit(amd64.MovToMemory(mem, amd64.Reg64(reg)))
		}
		c.freeReg(baseReg)
		return nil
	case asm.Variable:
		c.emit(amd64.MovReg(amd64.Reg64(dest), srcReg))
		return nil
	case amd64.Reg:
		c.emit(amd64.MovReg(dest, srcReg))
		return nil
	default:
		return fmt.Errorf("ir: cannot assign to %T", dst)
	}
}

func regForWidth(reg asm.Variable, width ir.ValueWidth) amd64.Reg {
	switch width {
	case ir.Width8:
		return amd64.Reg8(reg)
	case ir.Width16:
		return amd64.Reg16(reg)
	case ir.Width32:
		return amd64.Reg32(reg)
	default:
		return amd64.Reg64(reg)
	}
}

func (c *compiler) emitConditionJump(cond ir.Condition, trueLabel, falseLabel asm.Label) error {
	switch cv := cond.(type) {
	case ir.IsNegativeCondition:
		reg, _, err := c.evalValue(cv.Value)
		if err != nil {
			return err
		}
		c.emit(amd64.TestZero(reg))
		c.emit(amd64.JumpIfNegative(trueLabel))
		c.freeReg(reg)
		c.emit(amd64.Jump(falseLabel))
		return nil
	case ir.IsZeroCondition:
		reg, _, err := c.evalValue(cv.Value)
		if err != nil {
			return err
		}
		c.emit(amd64.TestZero(reg))
		c.emit(amd64.JumpIfZero(trueLabel))
		c.freeReg(reg)
		c.emit(amd64.Jump(falseLabel))
		return nil
	case ir.CompareCondition:
		leftReg, _, err := c.evalValue(cv.Left)
		if err != nil {
			return err
		}
		rightReg, _, err := c.evalValue(cv.Right)
		if err != nil {
			c.freeReg(leftReg)
			return err
		}
		c.emit(amd64.CmpRegReg(amd64.Reg64(leftReg), amd64.Reg64(rightReg)))
		c.freeReg(rightReg)
		c.freeReg(leftReg)
		switch cv.Kind {
		case ir.CompareEqual:
			c.emit(amd64.JumpIfEqual(trueLabel))
		case ir.CompareNotEqual:
			c.emit(amd64.JumpIfNotEqual(trueLabel))
		case ir.CompareLess:
			c.emit(amd64.JumpIfLess(trueLabel))
		case ir.CompareLessOrEqual:
			c.emit(amd64.JumpIfLess(trueLabel))
			c.emit(amd64.JumpIfEqual(trueLabel))
		case ir.CompareGreater:
			c.emit(amd64.JumpIfGreater(trueLabel))
		case ir.CompareGreaterOrEqual:
			c.emit(amd64.JumpIfGreater(trueLabel))
			c.emit(amd64.JumpIfEqual(trueLabel))
		default:
			return fmt.Errorf("ir: unsupported comparison kind %d", cv.Kind)
		}
		c.emit(amd64.Jump(falseLabel))
		return nil
	default:
		return fmt.Errorf("ir: unsupported condition %T", cond)
	}
}

func (c *compiler) compileSyscall(sc ir.SyscallFragment, needResult bool) (asm.Variable, error) {
	wasRAXUsed := c.usedRegs[amd64.RAX]
	c.reserveReg(amd64.RAX)

	argRegs := syscallArgRegisters

	args := make([]asm.Value, len(sc.Args))
	regs := make([]asm.Variable, 0, len(sc.Args))
	for idx, arg := range sc.Args {
		switch a := arg.(type) {
		case ir.Var:
			preferred := syscallPreferredRegisters(idx, argRegs)
			reg, err := c.loadVar64Prefer(string(a), preferred...)
			if err != nil {
				return 0, err
			}
			args[idx] = asm.Variable(reg)
			regs = append(regs, reg)
		case ir.ConstantPointerFragment:
			preferred := syscallPreferredRegisters(idx, argRegs)
			reg, err := c.allocRegPrefer(preferred...)
			if err != nil {
				for _, r := range regs {
					c.freeReg(r)
				}
				return 0, err
			}
			c.emit(amd64.LoadAddress(amd64.Reg64(reg), a.Target))
			args[idx] = asm.Variable(reg)
			regs = append(regs, reg)
		case string:
			val := asm.String(a)
			args[idx] = val
		default:
			if imm, ok := toInt64(a); ok {
				args[idx] = asm.Immediate(imm)
				continue
			}
			return 0, fmt.Errorf("ir: unsupported syscall argument %T", arg)
		}
	}

	c.emit(amd64.Syscall(sc.Num, args...))

	for _, reg := range regs {
		c.freeReg(reg)
	}

	if needResult {
		return amd64.RAX, nil
	}

	if !wasRAXUsed {
		c.freeReg(amd64.RAX)
	}
	return 0, nil
}

func (c *compiler) compilePrintf(p ir.PrintfFragment) error {
	values := make([]asm.Value, 0, len(p.Args))
	regs := make([]asm.Variable, 0, len(p.Args))

	for _, arg := range p.Args {
		reg, width, err := c.evalValue(arg)
		if err != nil {
			for _, r := range regs {
				c.freeReg(r)
			}
			return err
		}

		var val asm.Value
		switch width {
		case ir.Width32:
			val = amd64.Reg32(reg)
		default:
			val = amd64.Reg64(reg)
		}

		values = append(values, val)
		regs = append(regs, reg)
	}

	c.emit(amd64.Printf(p.Format, values...))

	for _, reg := range regs {
		c.freeReg(reg)
	}
	return nil
}

func (c *compiler) evalValue(expr ir.Fragment) (asm.Variable, ir.ValueWidth, error) {
	switch v := expr.(type) {
	case ir.Var:
		reg, err := c.loadVar64(string(v))
		if err != nil {
			return 0, 0, err
		}
		return reg, ir.Width64, nil
	case ir.I32Var:
		reg, err := c.allocReg()
		if err != nil {
			return 0, 0, err
		}
		mem, err := c.stackSlotMem(string(v.Name))
		if err != nil {
			c.freeReg(reg)
			return 0, 0, err
		}
		c.emit(amd64.MovFromMemory(amd64.Reg32(reg), mem))
		return reg, ir.Width32, nil
	case ir.I8Var:
		reg, err := c.allocReg()
		if err != nil {
			return 0, 0, err
		}
		mem, err := c.stackSlotMem(string(v.Name))
		if err != nil {
			c.freeReg(reg)
			return 0, 0, err
		}
		c.emit(amd64.MovZX8(amd64.Reg64(reg), mem))
		return reg, ir.Width8, nil
	case ir.MemVar:
		base, err := c.loadVar64(string(v.Base))
		if err != nil {
			return 0, 0, err
		}
		disp, err := c.resolveDisp(v.Disp)
		if err != nil {
			c.freeReg(base)
			return 0, 0, err
		}
		mem := amd64.Mem(amd64.Reg64(base)).WithDisp(disp)
		width := v.Width
		if width == 0 {
			width = ir.Width64
		}
		switch width {
		case ir.Width8:
			c.emit(amd64.MovZX8(amd64.Reg64(base), mem))
		case ir.Width16:
			c.emit(amd64.MovZX16(amd64.Reg64(base), mem))
		case ir.Width32:
			c.emit(amd64.MovFromMemory(amd64.Reg32(base), mem))
		default:
			c.emit(amd64.MovFromMemory(amd64.Reg64(base), mem))
		}
		return base, ir.Width64, nil
	case ir.GlobalMem:
		base, err := c.loadGlobalAddress(v.Name)
		if err != nil {
			return 0, 0, err
		}
		disp, err := c.resolveDisp(v.Disp)
		if err != nil {
			c.freeReg(base)
			return 0, 0, err
		}
		mem := amd64.Mem(amd64.Reg64(base)).WithDisp(disp)
		width := v.Width
		if width == 0 {
			width = ir.Width64
		}
		switch width {
		case ir.Width8:
			c.emit(amd64.MovZX8(amd64.Reg64(base), mem))
		case ir.Width16:
			c.emit(amd64.MovZX16(amd64.Reg64(base), mem))
		case ir.Width32:
			c.emit(amd64.MovFromMemory(amd64.Reg32(base), mem))
		default:
			c.emit(amd64.MovFromMemory(amd64.Reg64(base), mem))
		}
		return base, ir.Width64, nil
	case ir.StackSlotMemFragment:
		offset, err := c.stackSlotOffset(v.SlotID, v.Disp)
		if err != nil {
			return 0, 0, err
		}
		reg, err := c.allocReg()
		if err != nil {
			return 0, 0, err
		}
		mem := amd64.Mem(amd64.Reg64(amd64.RSP)).WithDisp(offset)
		c.emit(amd64.MovFromMemory(amd64.Reg64(reg), mem))
		return reg, ir.Width64, nil
	case ir.StackSlotPtrFragment:
		offset, err := c.stackSlotOffset(v.SlotID, v.Disp)
		if err != nil {
			return 0, 0, err
		}
		reg, err := c.allocRegPrefer(amd64.RAX)
		if err != nil {
			return 0, 0, err
		}
		c.emit(amd64.MovReg(amd64.Reg64(reg), amd64.Reg64(amd64.RSP)))
		if offset != 0 {
			c.emit(amd64.AddRegImm(amd64.Reg64(reg), offset))
		}
		return reg, ir.Width64, nil
	case ir.Int64:
		reg, err := c.allocRegPrefer(amd64.RAX)
		if err != nil {
			return 0, 0, err
		}
		c.emit(amd64.MovImmediate(amd64.Reg64(reg), int64(v)))
		return reg, ir.Width64, nil
	case ir.Int32:
		reg, err := c.allocRegPrefer(amd64.RAX)
		if err != nil {
			return 0, 0, err
		}
		c.emit(amd64.MovImmediate(amd64.Reg32(reg), int64(v)))
		return reg, ir.Width32, nil
	case ir.Int16:
		reg, err := c.allocRegPrefer(amd64.RAX)
		if err != nil {
			return 0, 0, err
		}
		c.emit(amd64.MovImmediate(amd64.Reg16(reg), int64(v)))
		return reg, ir.Width16, nil
	case ir.Int8:
		reg, err := c.allocRegPrefer(amd64.RAX)
		if err != nil {
			return 0, 0, err
		}
		c.emit(amd64.MovImmediate(amd64.Reg8(reg), int64(v)))
		return reg, ir.Width8, nil
	case ir.SyscallFragment:
		reg, err := c.compileSyscall(v, true)
		if err != nil {
			return 0, 0, err
		}
		return reg, ir.Width64, nil
	case ir.OpFragment:
		return c.evalOp(v)
	case ir.ConstantPointerFragment:
		reg, err := c.allocRegPrefer(amd64.RAX)
		if err != nil {
			return 0, 0, err
		}
		c.emit(amd64.LoadAddress(amd64.Reg64(reg), v.Target))
		return reg, ir.Width64, nil
	case ir.MethodPointerFragment:
		reg, err := c.allocRegPrefer(amd64.RAX)
		if err != nil {
			return 0, 0, err
		}
		c.emit(amd64.MovImmediate(amd64.Reg64(reg), int64(methodPointerPlaceholder(v.Name))))
		return reg, ir.Width64, nil
	case ir.GlobalPointerFragment:
		reg, err := c.allocRegPrefer(amd64.RAX)
		if err != nil {
			return 0, 0, err
		}
		c.emit(amd64.MovImmediate(amd64.Reg64(reg), int64(globalPointerPlaceholder(v.Name))))
		return reg, ir.Width64, nil
	default:
		if imm, ok := toInt64(v); ok {
			reg, err := c.allocRegPrefer(amd64.RAX)
			if err != nil {
				return 0, 0, err
			}
			c.emit(amd64.MovImmediate(amd64.Reg64(reg), imm))
			return reg, ir.Width64, nil
		}
		return 0, 0, fmt.Errorf("ir: unsupported expression %T", expr)
	}
}

func (c *compiler) evalOp(op ir.OpFragment) (asm.Variable, ir.ValueWidth, error) {
	switch op.Kind {
	case ir.OpAdd:
		leftReg, _, err := c.evalValue(op.Left)
		if err != nil {
			return 0, 0, err
		}
		rightReg, _, err := c.evalValue(op.Right)
		if err != nil {
			c.freeReg(leftReg)
			return 0, 0, err
		}
		c.emit(amd64.AddRegReg(amd64.Reg64(leftReg), amd64.Reg64(rightReg)))
		c.freeReg(rightReg)
		return leftReg, ir.Width64, nil
	case ir.OpSub:
		leftReg, _, err := c.evalValue(op.Left)
		if err != nil {
			return 0, 0, err
		}
		rightReg, _, err := c.evalValue(op.Right)
		if err != nil {
			c.freeReg(leftReg)
			return 0, 0, err
		}
		c.emit(amd64.SubRegReg(amd64.Reg64(leftReg), amd64.Reg64(rightReg)))
		c.freeReg(rightReg)
		return leftReg, ir.Width64, nil
	case ir.OpShr:
		leftReg, _, err := c.evalValue(op.Left)
		if err != nil {
			return 0, 0, err
		}
		shift, ok := toInt64(op.Right)
		if !ok {
			c.freeReg(leftReg)
			return 0, 0, fmt.Errorf("ir: shift amount must be immediate")
		}
		if shift < 0 || shift > math.MaxUint8 {
			c.freeReg(leftReg)
			return 0, 0, fmt.Errorf("ir: shift amount %d out of range", shift)
		}
		c.emit(amd64.ShrRegImm(amd64.Reg64(leftReg), uint8(shift)))
		return leftReg, ir.Width64, nil
	case ir.OpShl:
		leftReg, _, err := c.evalValue(op.Left)
		if err != nil {
			return 0, 0, err
		}
		shift, ok := toInt64(op.Right)
		if !ok {
			c.freeReg(leftReg)
			return 0, 0, fmt.Errorf("ir: shift amount must be immediate")
		}
		if shift < 0 || shift > math.MaxUint8 {
			c.freeReg(leftReg)
			return 0, 0, fmt.Errorf("ir: shift amount %d out of range", shift)
		}
		c.emit(amd64.ShlRegImm(amd64.Reg64(leftReg), uint8(shift)))
		return leftReg, ir.Width64, nil
	case ir.OpAnd:
		leftReg, _, err := c.evalValue(op.Left)
		if err != nil {
			return 0, 0, err
		}
		if imm, ok := toInt64(op.Right); ok {
			if imm >= math.MinInt32 && imm <= math.MaxInt32 {
				c.emit(amd64.AndRegImm(amd64.Reg64(leftReg), int32(imm)))
				return leftReg, ir.Width64, nil
			}
		}
		rightReg, _, err := c.evalValue(op.Right)
		if err != nil {
			c.freeReg(leftReg)
			return 0, 0, err
		}
		c.emit(amd64.AndRegReg(amd64.Reg64(leftReg), amd64.Reg64(rightReg)))
		c.freeReg(rightReg)
		return leftReg, ir.Width64, nil
	default:
		return 0, 0, fmt.Errorf("ir: unsupported op kind %d", op.Kind)
	}
}

func (c *compiler) stackSlotMem(name string) (amd64.Memory, error) {
	offset, ok := c.varOffsets[name]
	if !ok {
		return amd64.Memory{}, fmt.Errorf("ir: unknown variable %q", name)
	}
	return amd64.Mem(amd64.Reg64(amd64.RSP)).WithDisp(offset), nil
}

func (c *compiler) stackSlotOffset(slotID uint64, disp ir.Fragment) (int32, error) {
	for i := len(c.slotStack) - 1; i >= 0; i-- {
		ctx := c.slotStack[i]
		if ctx.Id == slotID {
			dispVal, err := c.resolveDisp(disp)
			if err != nil {
				return 0, err
			}
			return ctx.Base + dispVal, nil
		}
	}
	return 0, fmt.Errorf("ir: stack slot %d is not active", slotID)
}

func (c *compiler) loadVar64(name string) (asm.Variable, error) {
	reg, err := c.allocReg()
	if err != nil {
		return 0, err
	}
	mem, err := c.stackSlotMem(name)
	if err != nil {
		c.freeReg(reg)
		return 0, err
	}
	c.emit(amd64.MovFromMemory(amd64.Reg64(reg), mem))
	return reg, nil
}

func (c *compiler) loadVar64Prefer(name string, preferred ...asm.Variable) (asm.Variable, error) {
	reg, err := c.allocRegPrefer(preferred...)
	if err != nil {
		return 0, err
	}
	mem, err := c.stackSlotMem(name)
	if err != nil {
		c.freeReg(reg)
		return 0, err
	}
	c.emit(amd64.MovFromMemory(amd64.Reg64(reg), mem))
	return reg, nil
}

func (c *compiler) loadGlobalAddress(name string) (asm.Variable, error) {
	reg, err := c.allocRegPrefer(amd64.RAX)
	if err != nil {
		return 0, err
	}
	c.emit(amd64.MovImmediate(amd64.Reg64(reg), int64(globalPointerPlaceholder(name))))
	return reg, nil
}

func (c *compiler) resolveDisp(d ir.Fragment) (int32, error) {
	if d == nil {
		return 0, nil
	}
	value, ok := toInt64(d)
	if !ok {
		return 0, fmt.Errorf("ir: displacement must be constant, got %T", d)
	}
	if value < math.MinInt32 || value > math.MaxInt32 {
		return 0, fmt.Errorf("ir: displacement %d out of range", value)
	}
	return int32(value), nil
}

func (c *compiler) allocReg() (asm.Variable, error) {
	if n := len(c.freeRegs); n > 0 {
		reg := c.freeRegs[n-1]
		c.freeRegs = c.freeRegs[:n-1]
		c.usedRegs[reg] = true
		return reg, nil
	}
	return 0, fmt.Errorf("ir: register exhaustion")
}

func (c *compiler) allocRegPrefer(preferred ...asm.Variable) (asm.Variable, error) {
	for _, reg := range preferred {
		if c.reserveReg(reg) {
			return reg, nil
		}
	}
	return c.allocReg()
}

func (c *compiler) reserveReg(reg asm.Variable) bool {
	if c.usedRegs[reg] {
		return false
	}
	for idx, candidate := range c.freeRegs {
		if candidate == reg {
			c.freeRegs = append(c.freeRegs[:idx], c.freeRegs[idx+1:]...)
			c.usedRegs[reg] = true
			return true
		}
	}
	return false
}

func (c *compiler) freeReg(reg asm.Variable) {
	if reg == amd64.RSP {
		return
	}
	if !c.usedRegs[reg] {
		return
	}
	delete(c.usedRegs, reg)
	c.freeRegs = append(c.freeRegs, reg)
}

func (c *compiler) namedLabel(name string) asm.Label {
	if lbl, ok := c.labels[name]; ok {
		return lbl
	}
	lbl := asm.Label(name)
	c.labels[name] = lbl
	return lbl
}

func (c *compiler) newInternalLabel(prefix string) asm.Label {
	c.labelCounter++
	return asm.Label(fmt.Sprintf(".ir_%s_%d", prefix, c.labelCounter))
}

func collectVariables(f ir.Fragment, vars map[string]struct{}) {
	switch v := f.(type) {
	case nil:
	case ir.Block:
		for _, inner := range v {
			collectVariables(inner, vars)
		}
	case ir.Method:
		collectVariables(ir.Block(v), vars)
	case ir.Var:
		vars[string(v)] = struct{}{}
	case ir.I32Var:
		vars[string(v.Name)] = struct{}{}
	case ir.I8Var:
		vars[string(v.Name)] = struct{}{}
	case ir.MemVar:
		vars[string(v.Base)] = struct{}{}
		if v.Disp != nil {
			collectVariables(v.Disp, vars)
		}
	case ir.GlobalMem:
		if v.Disp != nil {
			collectVariables(v.Disp, vars)
		}
	case ir.DeclareParam:
		vars[string(v)] = struct{}{}
	case ir.AssignFragment:
		collectVariables(v.Dst, vars)
		collectVariables(v.Src, vars)
	case ir.SyscallFragment:
		for _, arg := range v.Args {
			collectVariables(arg, vars)
		}
	case ir.IfFragment:
		collectConditionVars(v.Cond, vars)
		collectVariables(v.Then, vars)
		if v.Otherwise != nil {
			collectVariables(v.Otherwise, vars)
		}
	case ir.GotoFragment:
	case ir.LabelFragment:
		collectVariables(v.Block, vars)
	case ir.Label:
	case ir.ReturnFragment:
		collectVariables(v.Value, vars)
	case ir.PrintfFragment:
		for _, arg := range v.Args {
			collectVariables(arg, vars)
		}
	case ir.StackSlotFragment:
		for _, name := range v.Chunks {
			vars[name] = struct{}{}
		}
		if v.Body != nil {
			collectVariables(v.Body, vars)
		}
	case ir.StackSlotMemFragment:
		if v.Disp != nil {
			collectVariables(v.Disp, vars)
		}
	case ir.StackSlotPtrFragment:
		if v.Disp != nil {
			collectVariables(v.Disp, vars)
		}
	case ir.ConstantBytesFragment:
		// constants do not reference stack variables
	case ir.ConstantPointerFragment:
		// pointer fragments also do not reference stack variables
	default:
	}
}

func collectConditionVars(cond ir.Condition, vars map[string]struct{}) {
	switch cv := cond.(type) {
	case ir.IsNegativeCondition:
		collectVariables(cv.Value, vars)
	case ir.IsZeroCondition:
		collectVariables(cv.Value, vars)
	default:
	}
}

func alignTo(value, boundary int32) int32 {
	if boundary <= 0 {
		return value
	}
	mask := boundary - 1
	return (value + mask) &^ mask
}

func toInt64(v any) (int64, bool) {
	switch value := v.(type) {
	case ir.Int64:
		return int64(value), true
	case int:
		return int64(value), true
	case int8:
		return int64(value), true
	case int16:
		return int64(value), true
	case int32:
		return int64(value), true
	case int64:
		return value, true
	case uint:
		return int64(value), true
	case uint8:
		return int64(value), true
	case uint16:
		return int64(value), true
	case uint32:
		return int64(value), true
	case uint64:
		if value > math.MaxInt64 {
			return 0, false
		}
		return int64(value), true
	case uintptr:
		return int64(value), true
	default:
		return 0, false
	}
}

func extractLabelName(label ir.Fragment) (string, error) {
	switch v := label.(type) {
	case ir.Label:
		if v == "" {
			return "", fmt.Errorf("ir: empty label")
		}
		return string(v), nil
	default:
		return "", fmt.Errorf("ir: unsupported label fragment %T", label)
	}
}

const standaloneAlignment = 16

func BuildStandaloneProgram(p *ir.Program) (asm.Program, error) {
	entry, ok := p.Methods[p.Entrypoint]
	if !ok {
		return asm.Program{}, fmt.Errorf("entrypoint method %q not found", p.Entrypoint)
	}

	globalNames := make([]string, 0, len(p.Globals))
	globalSpecs := make(map[string]normalizedGlobal, len(p.Globals))
	for name, cfg := range p.Globals {
		spec, err := normalizeGlobalConfig(name, cfg)
		if err != nil {
			return asm.Program{}, err
		}
		globalNames = append(globalNames, name)
		globalSpecs[name] = spec
	}
	sort.Strings(globalNames)

	entryFrag, err := Compile(entry)
	if err != nil {
		return asm.Program{}, fmt.Errorf("failed to compile entrypoint method %q: %w", p.Entrypoint, err)
	}

	entryProg, err := amd64.EmitProgram(entryFrag)
	if err != nil {
		return asm.Program{}, fmt.Errorf("failed to compile entrypoint method %q: %w", p.Entrypoint, err)
	}

	type methodProgram struct {
		name string
		prog asm.Program
	}

	ordered := make([]methodProgram, 0, len(p.Methods))
	ordered = append(ordered, methodProgram{name: p.Entrypoint, prog: entryProg})

	otherNames := make([]string, 0, len(p.Methods)-1)
	for name := range p.Methods {
		if name == p.Entrypoint {
			continue
		}
		otherNames = append(otherNames, name)
	}
	sort.Strings(otherNames)

	for _, name := range otherNames {
		method := p.Methods[name]
		frag, err := Compile(method)
		if err != nil {
			return asm.Program{}, fmt.Errorf("failed to compile method %q: %w", name, err)
		}
		prog, err := amd64.EmitProgram(frag)
		if err != nil {
			return asm.Program{}, fmt.Errorf("failed to compile method %q: %w", name, err)
		}
		ordered = append(ordered, methodProgram{name: name, prog: prog})
	}

	type layout struct {
		name    string
		token   uint64
		start   int
		size    int
		bssSize int
		relocs  []int
		bssBase int
	}

	finalCode := make([]byte, 0)
	finalRelocs := make([]int, 0)
	layouts := make([]layout, 0, len(ordered))

	for _, method := range ordered {
		prog := method.prog
		aligned := align(len(finalCode), standaloneAlignment)
		if pad := aligned - len(finalCode); pad > 0 {
			finalCode = append(finalCode, make([]byte, pad)...)
		}
		start := len(finalCode)
		code := prog.RelocatedCopy(uintptr(start))
		finalCode = append(finalCode, code...)
		relocs := prog.Relocations()
		for _, rel := range relocs {
			finalRelocs = append(finalRelocs, start+rel)
		}
		layouts = append(layouts, layout{
			name:    method.name,
			token:   methodPointerPlaceholder(method.name),
			start:   start,
			size:    len(code),
			bssSize: prog.BSSSize(),
			relocs:  relocs,
		})
	}

	tokenToStart := make(map[uint64]int, len(layouts))
	for _, layout := range layouts {
		if _, exists := tokenToStart[layout.token]; exists {
			return asm.Program{}, fmt.Errorf("duplicate method token for %q", layout.name)
		}
		tokenToStart[layout.token] = layout.start
	}

	pointerRelocs := make([]int, 0)
	for idx := 0; idx+8 <= len(finalCode); idx++ {
		value := binary.LittleEndian.Uint64(finalCode[idx:])
		start, ok := tokenToStart[value]
		if !ok {
			continue
		}
		binary.LittleEndian.PutUint64(finalCode[idx:], uint64(start))
		pointerRelocs = append(pointerRelocs, idx)
		idx += 7
	}

	if len(pointerRelocs) > 0 {
		finalRelocs = append(finalRelocs, pointerRelocs...)
	}

	globalBSSBase := align(len(finalCode), standaloneAlignment)
	bssCursor := 0
	for idx := range layouts {
		if layouts[idx].bssSize == 0 {
			continue
		}
		bssCursor = align(bssCursor, standaloneAlignment)
		layouts[idx].bssBase = globalBSSBase + bssCursor
		bssCursor += layouts[idx].bssSize
	}

	for _, layout := range layouts {
		if layout.bssSize == 0 {
			continue
		}
		localBSSStart := uint64(layout.size)
		globalBSSStart := uint64(layout.bssBase)
		for _, rel := range layout.relocs {
			pos := layout.start + rel
			if pos+8 > len(finalCode) {
				return asm.Program{}, fmt.Errorf("bss relocation position %d out of range", pos)
			}
			val := binary.LittleEndian.Uint64(finalCode[pos:])
			if val < uint64(layout.start) {
				return asm.Program{}, fmt.Errorf("relocation value %#x precedes routine start %#x", val, layout.start)
			}
			local := val - uint64(layout.start)
			if local < localBSSStart {
				continue
			}
			offset := local - localBSSStart
			binary.LittleEndian.PutUint64(finalCode[pos:], globalBSSStart+offset)
		}
	}

	globalOffsets := make(map[string]int, len(globalNames))
	if len(globalNames) > 0 {
		for _, name := range globalNames {
			spec := globalSpecs[name]
			bssCursor = align(bssCursor, spec.align)
			globalOffsets[name] = bssCursor
			bssCursor += spec.size
		}
	}

	globalTokenToAddr := make(map[uint64]uint64, len(globalOffsets))
	for _, name := range globalNames {
		offset := globalOffsets[name]
		token := globalPointerPlaceholder(name)
		if _, exists := globalTokenToAddr[token]; exists {
			return asm.Program{}, fmt.Errorf("duplicate global token for %q", name)
		}
		globalTokenToAddr[token] = uint64(globalBSSBase + offset)
	}

	globalPointerRelocs := make([]int, 0)
	for idx := 0; idx+8 <= len(finalCode); idx++ {
		value := binary.LittleEndian.Uint64(finalCode[idx:])
		if addr, ok := globalTokenToAddr[value]; ok {
			binary.LittleEndian.PutUint64(finalCode[idx:], addr)
			globalPointerRelocs = append(globalPointerRelocs, idx)
			idx += 7
			continue
		}
		if isGlobalPointerToken(value) {
			return asm.Program{}, fmt.Errorf("reference to undefined global token %#x", value)
		}
	}
	finalRelocs = append(finalRelocs, globalPointerRelocs...)

	finalBSSSize := (globalBSSBase - len(finalCode)) + bssCursor
	if finalBSSSize < 0 {
		finalBSSSize = 0
	}

	return asm.NewProgram(finalCode, finalRelocs, finalBSSSize), nil
}

func align(value, boundary int) int {
	if boundary <= 0 {
		return value
	}
	mask := boundary - 1
	return (value + mask) &^ mask
}

const (
	methodPointerPrefix = 0x5ead000000000000
	methodPointerMask   = 0x0000ffffffffffff
	globalPointerPrefix = 0x5eae000000000000
	globalPointerMask   = 0x0000ffffffffffff
)

func methodPointerPlaceholder(name string) uint64 {
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(name))
	return methodPointerPrefix | (hash.Sum64() & methodPointerMask)
}

func globalPointerPlaceholder(name string) uint64 {
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(name))
	return globalPointerPrefix | (hash.Sum64() & globalPointerMask)
}

func isGlobalPointerToken(value uint64) bool {
	return value&^globalPointerMask == globalPointerPrefix
}

type normalizedGlobal struct {
	size  int
	align int
}

func normalizeGlobalConfig(name string, cfg ir.GlobalConfig) (normalizedGlobal, error) {
	size := cfg.Size
	if size <= 0 {
		size = 8
	}
	align := cfg.Align
	if align <= 0 {
		align = 8
	}
	if align&(align-1) != 0 {
		return normalizedGlobal{}, fmt.Errorf("ir: global %q alignment %d is not a power of two", name, align)
	}
	return normalizedGlobal{size: size, align: align}, nil
}
