package ir

import (
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"math"
	"sort"

	asm "github.com/tinyrange/cc/internal/asm/amd64"
)

type Fragment interface{}

type MemoryFragment interface {
	Fragment
	WithDisp(disp any) Fragment
}

func asFragment(v any) Fragment {
	if f, ok := v.(Fragment); ok {
		return f
	}
	panic(fmt.Sprintf("cannot convert %T to Fragment", v))
}

type Condition interface {
	Fragment
}

type compareKind int

const (
	compareEqual compareKind = iota
	compareNotEqual
	compareLess
	compareLessOrEqual
	compareGreater
	compareGreaterOrEqual
)

type compareCondition struct {
	kind  compareKind
	left  Fragment
	right Fragment
}

func IsEqual(left, right any) Condition {
	return compareCondition{
		kind:  compareEqual,
		left:  asFragment(left),
		right: asFragment(right),
	}
}

func IsNotEqual(left, right any) Condition {
	return compareCondition{
		kind:  compareNotEqual,
		left:  asFragment(left),
		right: asFragment(right),
	}
}

func IsLessThan(left, right any) Condition {
	return compareCondition{
		kind:  compareLess,
		left:  asFragment(left),
		right: asFragment(right),
	}
}

func IsLessOrEqual(left, right any) Condition {
	return compareCondition{
		kind:  compareLessOrEqual,
		left:  asFragment(left),
		right: asFragment(right),
	}
}

func IsGreaterThan(left, right any) Condition {
	return compareCondition{
		kind:  compareGreater,
		left:  asFragment(left),
		right: asFragment(right),
	}
}

func IsGreaterOrEqual(left, right any) Condition {
	return compareCondition{
		kind:  compareGreaterOrEqual,
		left:  asFragment(left),
		right: asFragment(right),
	}
}

type Method []Fragment

type Block []Fragment

type DeclareParam string

type Int64 int64
type Int32 int32
type Int16 int16
type Int8 int8

type Var string

type GlobalVar string

// Global declares a reference to a program-level variable. The returned handle
// can be used with AssignGlobal or treated as a memory reference via AsMem.
func Global(name string) GlobalVar {
	if name == "" {
		panic("ir: global name must be non-empty")
	}
	return GlobalVar(name)
}

// Name returns the string identifier backing the global. Useful when wiring
// globals into Program.Globals.
func (g GlobalVar) Name() string {
	return string(g)
}

type i32Var struct {
	name Var
}

type i8Var struct {
	name Var
}

func (v Var) As32() Fragment {
	return i32Var{name: v}
}

func (v Var) As8() Fragment {
	return i8Var{name: v}
}

type memVar struct {
	base  Var
	disp  Fragment
	width valueWidth
}

func (m memVar) WithDisp(disp any) Fragment {
	m.disp = asFragment(disp)
	return m
}

func (m memVar) withWidth(width valueWidth) memVar {
	m.width = width
	return m
}

func (m memVar) As8() memVar {
	return m.withWidth(width8)
}

func (m memVar) As16() memVar {
	return m.withWidth(width16)
}

func (m memVar) As32() memVar {
	return m.withWidth(width32)
}

func (v Var) AsMem() MemoryFragment {
	return memVar{base: v, width: width64}
}

func (g GlobalVar) AsMem() MemoryFragment {
	return g.Mem()
}

// Mem exposes a typed memory reference so width helpers (As8/As16/As32) may be
// chained without losing the underlying memVar type.
func (v Var) Mem() memVar {
	return memVar{base: v, width: width64}
}

// MemWithDisp is equivalent to Mem().WithDisp(disp) but preserves the memVar
// type so callers can chain width conversions.
func (v Var) MemWithDisp(disp any) memVar {
	return memVar{base: v, width: width64, disp: asFragment(disp)}
}

type globalMem struct {
	name  string
	disp  Fragment
	width valueWidth
}

func (m globalMem) WithDisp(disp any) Fragment {
	m.disp = asFragment(disp)
	return m
}

func (m globalMem) withWidth(width valueWidth) globalMem {
	m.width = width
	return m
}

func (m globalMem) As8() globalMem {
	return m.withWidth(width8)
}

func (m globalMem) As16() globalMem {
	return m.withWidth(width16)
}

func (m globalMem) As32() globalMem {
	return m.withWidth(width32)
}

// Mem exposes a typed global memory reference so width helpers may be chained.
func (g GlobalVar) Mem() globalMem {
	return globalMem{name: string(g), width: width64}
}

// MemWithDisp is equivalent to Mem().WithDisp(disp) while retaining the
// concrete type for chaining width helpers.
func (g GlobalVar) MemWithDisp(disp any) globalMem {
	return globalMem{name: string(g), width: width64, disp: asFragment(disp)}
}

type Label string

type syscallFragment struct {
	num  int64
	args []Fragment
}

func Syscall(num int64, args ...any) Fragment {
	argsFragments := make([]Fragment, 0, len(args))
	for _, arg := range args {
		argsFragments = append(argsFragments, asFragment(arg))
	}
	return syscallFragment{num: num, args: argsFragments}
}

type returnFragment struct {
	value Fragment
}

func Return(value any) Fragment {
	return returnFragment{value: asFragment(value)}
}

type printfFragment struct {
	format string
	args   []Fragment
}

func Printf(format string, args ...any) Fragment {
	argFragments := make([]Fragment, 0, len(args))
	for _, arg := range args {
		argFragments = append(argFragments, asFragment(arg))
	}
	return printfFragment{format: format, args: argFragments}
}

type assignFragment struct {
	dst Fragment
	src Fragment
}

func Assign(dst Fragment, src Fragment) Fragment {
	return assignFragment{dst: dst, src: src}
}

type ifFragment struct {
	cond      Condition
	then      Fragment
	otherwise Fragment
}

func If(cond Condition, then Fragment, otherwise ...Fragment) Fragment {
	if len(otherwise) > 0 {
		return ifFragment{cond: cond, then: then, otherwise: otherwise[0]}
	}
	return ifFragment{cond: cond, then: then}
}

type gotoFragment struct {
	label Fragment
}

func Goto(label Fragment) Fragment {
	return gotoFragment{label: label}
}

type callFragment struct {
	target Fragment
	result Var
}

// Call emits an indirect call to the provided target value. When result is
// specified the callee's return value (RAX) is stored into that variable.
func Call(target any, result ...Var) Fragment {
	var res Var
	if len(result) > 0 {
		res = result[0]
	}
	return callFragment{
		target: asFragment(target),
		result: res,
	}
}

type labelFragment struct {
	label Label
	block Block
}

func DeclareLabel(label Label, block Block) Fragment {
	return labelFragment{label: label, block: block}
}

type OpKind int

const (
	OpInvalid OpKind = iota
	OpAdd
	OpSub
	OpMul
	OpDiv
	OpShr
	OpShl
	OpAnd
)

type opFragment struct {
	kind  OpKind
	left  Fragment
	right Fragment
}

type stackSlotContext struct {
	id   uint64
	base int32
	size int64
}

func Op(kind OpKind, left, right Fragment) Fragment {
	return opFragment{kind: kind, left: left, right: right}
}

type isNegativeCondition struct {
	value Fragment
}

func IsNegative(value Fragment) Condition {
	return isNegativeCondition{value: value}
}

type isZeroCondition struct {
	value Fragment
}

func IsZero(value Fragment) Condition {
	return isZeroCondition{value: value}
}

const stackAlignment = 16

var paramRegisters = []asm.Variable{asm.RDI, asm.RSI, asm.RDX, asm.RCX, asm.R8, asm.R9}

var initialFreeRegisters = []asm.Variable{
	asm.RAX,
	asm.RCX,
	asm.RDX,
	asm.R8,
	asm.R9,
	asm.R10,
	asm.R11,
}

var syscallArgRegisters = []asm.Variable{
	asm.RDI,
	asm.RSI,
	asm.RDX,
	asm.R10,
	asm.R8,
	asm.R9,
}

type valueWidth uint8

const (
	width8  valueWidth = 8
	width16 valueWidth = 16
	width32 valueWidth = 32
	width64 valueWidth = 64
)

type compiler struct {
	method       Method
	fragments    asm.Group
	varOffsets   map[string]int32
	frameSize    int32
	freeRegs     []asm.Variable
	usedRegs     map[asm.Variable]bool
	paramIndex   int
	labels       map[string]asm.Label
	labelCounter int
	slotStack    []stackSlotContext
}

func Compile(method Method) (asm.Fragment, error) {
	c, err := newCompiler(method)
	if err != nil {
		return nil, err
	}
	if c.frameSize > 0 {
		c.emit(asm.AddRegImm(asm.Reg64(asm.RSP), -c.frameSize))
	}
	if err := c.compileBlock(Block(method)); err != nil {
		return nil, err
	}
	if c.frameSize > 0 {
		c.emit(asm.AddRegImm(asm.Reg64(asm.RSP), c.frameSize))
	}
	c.emit(asm.Ret())
	return c.fragments, nil
}

func (m Method) Compile() (asm.Fragment, error) {
	return Compile(m)
}

func MustCompile(method Method) asm.Fragment {
	frag, err := Compile(method)
	if err != nil {
		panic(err)
	}
	return frag
}

func newCompiler(method Method) (*compiler, error) {
	vars := make(map[string]struct{})
	collectVariables(Block(method), vars)
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
		asm.R11,
		asm.RCX,
		asm.R9,
		asm.R8,
		asm.RDX,
		asm.R10,
		asm.RSI,
		asm.RDI,
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

func (c *compiler) compileBlock(block Block) error {
	for _, frag := range block {
		if err := c.compileFragment(frag); err != nil {
			return err
		}
	}
	return nil
}

func (c *compiler) compileFragment(f Fragment) error {
	switch frag := f.(type) {
	case nil:
		return nil
	case Block:
		return c.compileBlock(frag)
	case Method:
		return c.compileBlock(Block(frag))
	case DeclareParam:
		return c.compileDeclareParam(string(frag))
	case assignFragment:
		return c.compileAssign(frag)
	case syscallFragment:
		_, err := c.compileSyscall(frag, false)
		return err
	case stackSlotFragment:
		return c.compileStackSlot(frag)
	case ifFragment:
		return c.compileIf(frag)
	case gotoFragment:
		return c.compileGoto(frag)
	case labelFragment:
		return c.compileLabelBlock(frag)
	case Label:
		return c.compileLabel(frag)
	case returnFragment:
		return c.compileReturn(frag)
	case printfFragment:
		return c.compilePrintf(frag)
	case callFragment:
		return c.compileCall(frag)
	case constantBytesFragment:
		c.emit(asm.LoadConstantBytes(frag.target, frag.data))
		return nil
	case constantPointerFragment:
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
	c.emit(asm.MovToMemory(mem, asm.Reg64(reg)))
	c.freeReg(reg)
	return nil
}

func (c *compiler) compileAssign(assign assignFragment) error {
	switch src := assign.src.(type) {
	case syscallFragment:
		reg, err := c.compileSyscall(src, true)
		if err != nil {
			return err
		}
		if err := c.storeValue(assign.dst, reg, width64); err != nil {
			return err
		}
		c.freeReg(reg)
		return nil
	default:
		reg, width, err := c.evalValue(assign.src)
		if err != nil {
			return err
		}
		if err := c.storeValue(assign.dst, reg, width); err != nil {
			return err
		}
		c.freeReg(reg)
		return nil
	}
}

func (c *compiler) compileIf(f ifFragment) error {
	trueLabel := c.newInternalLabel("if_true")
	endLabel := c.newInternalLabel("if_end")
	falseLabel := endLabel
	if f.otherwise != nil {
		falseLabel = c.newInternalLabel("if_else")
	}

	if err := c.emitConditionJump(f.cond, trueLabel, falseLabel); err != nil {
		return err
	}

	if f.otherwise != nil {
		c.emit(asm.MarkLabel(falseLabel))
		if err := c.compileFragment(f.otherwise); err != nil {
			return err
		}
		c.emit(asm.Jump(endLabel))
	}

	c.emit(asm.MarkLabel(trueLabel))
	if err := c.compileFragment(f.then); err != nil {
		return err
	}
	c.emit(asm.MarkLabel(endLabel))
	return nil
}

func (c *compiler) compileGoto(g gotoFragment) error {
	name, err := extractLabelName(g.label)
	if err != nil {
		return err
	}
	c.emit(asm.Jump(c.namedLabel(name)))
	return nil
}

func (c *compiler) compileCall(f callFragment) error {
	targetReg, _, err := c.evalValue(f.target)
	if err != nil {
		return err
	}
	c.emit(asm.CallReg(asm.Reg64(targetReg)))
	c.freeReg(targetReg)
	if f.result != "" {
		mem, err := c.stackSlotMem(string(f.result))
		if err != nil {
			return err
		}
		c.emit(asm.MovToMemory(mem, asm.Reg64(asm.RAX)))
	}
	return nil
}

func (c *compiler) compileLabelBlock(f labelFragment) error {
	c.emit(asm.MarkLabel(c.namedLabel(string(f.label))))
	return c.compileBlock(f.block)
}

func (c *compiler) compileLabel(label Label) error {
	c.emit(asm.MarkLabel(c.namedLabel(string(label))))
	return nil
}

func (c *compiler) compileReturn(ret returnFragment) error {
	reg, _, err := c.evalValue(ret.value)
	if err != nil {
		return err
	}
	if reg != asm.RAX {
		c.emit(asm.MovReg(asm.Reg64(asm.RAX), asm.Reg64(reg)))
		c.freeReg(reg)
	}
	if c.frameSize > 0 {
		c.emit(asm.AddRegImm(asm.Reg64(asm.RSP), c.frameSize))
	}
	c.emit(asm.Ret())
	c.freeReg(asm.RAX)
	return nil
}

func (c *compiler) compileStackSlot(slot stackSlotFragment) error {
	if slot.size <= 0 {
		return fmt.Errorf("ir: stack slot size must be positive")
	}
	if len(slot.chunks) == 0 {
		return fmt.Errorf("ir: stack slot has no backing storage")
	}
	baseName := slot.chunks[0]
	baseOffset, ok := c.varOffsets[baseName]
	if !ok {
		return fmt.Errorf("ir: unknown stack slot chunk %q", baseName)
	}

	c.slotStack = append(c.slotStack, stackSlotContext{
		id:   slot.id,
		base: baseOffset,
		size: slot.size,
	})

	if slot.body != nil {
		if err := c.compileFragment(slot.body); err != nil {
			c.slotStack = c.slotStack[:len(c.slotStack)-1]
			return err
		}
	}
	c.slotStack = c.slotStack[:len(c.slotStack)-1]

	return nil
}

func (c *compiler) storeValue(dst Fragment, reg asm.Variable, width valueWidth) error {
	srcReg := regForWidth(reg, width)
	switch dest := dst.(type) {
	case Var:
		mem, err := c.stackSlotMem(string(dest))
		if err != nil {
			return err
		}
		c.emit(asm.MovToMemory(mem, asm.Reg64(reg)))
		return nil
	case i32Var:
		mem, err := c.stackSlotMem(string(dest.name))
		if err != nil {
			return err
		}
		c.emit(asm.MovToMemory(mem, asm.Reg32(reg)))
		return nil
	case stackSlotMemFragment:
		offset, err := c.stackSlotOffset(dest.slotID, dest.disp)
		if err != nil {
			return err
		}
		mem := asm.Mem(asm.Reg64(asm.RSP)).WithDisp(offset)
		switch width {
		case width8:
			c.emit(asm.MovToMemory(mem, asm.Reg8(reg)))
		case width32:
			c.emit(asm.MovToMemory(mem, asm.Reg32(reg)))
		default:
			c.emit(asm.MovToMemory(mem, asm.Reg64(reg)))
		}
		return nil
	case memVar:
		baseReg, err := c.loadVar64(string(dest.base))
		if err != nil {
			return err
		}
		disp, err := c.resolveDisp(dest.disp)
		if err != nil {
			c.freeReg(baseReg)
			return err
		}
		mem := asm.Mem(asm.Reg64(baseReg)).WithDisp(disp)
		switch width {
		case width8:
			c.emit(asm.MovToMemory(mem, asm.Reg8(reg)))
		case width32:
			c.emit(asm.MovToMemory(mem, asm.Reg32(reg)))
		default:
			c.emit(asm.MovToMemory(mem, asm.Reg64(reg)))
		}
		c.freeReg(baseReg)
		return nil
	case globalMem:
		baseReg, err := c.loadGlobalAddress(dest.name)
		if err != nil {
			return err
		}
		disp, err := c.resolveDisp(dest.disp)
		if err != nil {
			c.freeReg(baseReg)
			return err
		}
		mem := asm.Mem(asm.Reg64(baseReg)).WithDisp(disp)
		switch width {
		case width8:
			c.emit(asm.MovToMemory(mem, asm.Reg8(reg)))
		case width32:
			c.emit(asm.MovToMemory(mem, asm.Reg32(reg)))
		default:
			c.emit(asm.MovToMemory(mem, asm.Reg64(reg)))
		}
		c.freeReg(baseReg)
		return nil
	case asm.Variable:
		c.emit(asm.MovReg(asm.Reg64(dest), srcReg))
		return nil
	case asm.Reg:
		c.emit(asm.MovReg(dest, srcReg))
		return nil
	default:
		return fmt.Errorf("ir: cannot assign to %T", dst)
	}
}

func regForWidth(reg asm.Variable, width valueWidth) asm.Reg {
	switch width {
	case width8:
		return asm.Reg8(reg)
	case width16:
		return asm.Reg16(reg)
	case width32:
		return asm.Reg32(reg)
	default:
		return asm.Reg64(reg)
	}
}

func (c *compiler) emitConditionJump(cond Condition, trueLabel, falseLabel asm.Label) error {
	switch cv := cond.(type) {
	case isNegativeCondition:
		reg, _, err := c.evalValue(cv.value)
		if err != nil {
			return err
		}
		c.emit(asm.TestZero(reg))
		c.emit(asm.JumpIfNegative(trueLabel))
		c.freeReg(reg)
		c.emit(asm.Jump(falseLabel))
		return nil
	case isZeroCondition:
		reg, _, err := c.evalValue(cv.value)
		if err != nil {
			return err
		}
		c.emit(asm.TestZero(reg))
		c.emit(asm.JumpIfZero(trueLabel))
		c.freeReg(reg)
		c.emit(asm.Jump(falseLabel))
		return nil
	case compareCondition:
		leftReg, _, err := c.evalValue(cv.left)
		if err != nil {
			return err
		}
		rightReg, _, err := c.evalValue(cv.right)
		if err != nil {
			c.freeReg(leftReg)
			return err
		}
		c.emit(asm.CmpRegReg(asm.Reg64(leftReg), asm.Reg64(rightReg)))
		c.freeReg(rightReg)
		c.freeReg(leftReg)
		switch cv.kind {
		case compareEqual:
			c.emit(asm.JumpIfEqual(trueLabel))
		case compareNotEqual:
			c.emit(asm.JumpIfNotEqual(trueLabel))
		case compareLess:
			c.emit(asm.JumpIfLess(trueLabel))
		case compareLessOrEqual:
			c.emit(asm.JumpIfLess(trueLabel))
			c.emit(asm.JumpIfEqual(trueLabel))
		case compareGreater:
			c.emit(asm.JumpIfGreater(trueLabel))
		case compareGreaterOrEqual:
			c.emit(asm.JumpIfGreater(trueLabel))
			c.emit(asm.JumpIfEqual(trueLabel))
		default:
			return fmt.Errorf("ir: unsupported comparison kind %d", cv.kind)
		}
		c.emit(asm.Jump(falseLabel))
		return nil
	default:
		return fmt.Errorf("ir: unsupported condition %T", cond)
	}
}

func (c *compiler) compileSyscall(sc syscallFragment, needResult bool) (asm.Variable, error) {
	wasRAXUsed := c.usedRegs[asm.RAX]
	c.reserveReg(asm.RAX)

	argRegs := syscallArgRegisters

	args := make([]asm.Value, len(sc.args))
	regs := make([]asm.Variable, 0, len(sc.args))
	for idx, arg := range sc.args {
		switch a := arg.(type) {
		case Var:
			preferred := syscallPreferredRegisters(idx, argRegs)
			reg, err := c.loadVar64Prefer(string(a), preferred...)
			if err != nil {
				return 0, err
			}
			args[idx] = asm.Variable(reg)
			regs = append(regs, reg)
		case constantPointerFragment:
			preferred := syscallPreferredRegisters(idx, argRegs)
			reg, err := c.allocRegPrefer(preferred...)
			if err != nil {
				for _, r := range regs {
					c.freeReg(r)
				}
				return 0, err
			}
			c.emit(asm.LoadAddress(asm.Reg64(reg), a.target))
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

	c.emit(asm.Syscall(int(sc.num), args...))

	for _, reg := range regs {
		c.freeReg(reg)
	}

	if needResult {
		return asm.RAX, nil
	}

	if !wasRAXUsed {
		c.freeReg(asm.RAX)
	}
	return 0, nil
}

func (c *compiler) compilePrintf(p printfFragment) error {
	values := make([]asm.Value, 0, len(p.args))
	regs := make([]asm.Variable, 0, len(p.args))

	for _, arg := range p.args {
		reg, width, err := c.evalValue(arg)
		if err != nil {
			for _, r := range regs {
				c.freeReg(r)
			}
			return err
		}

		var val asm.Value
		switch width {
		case width32:
			val = asm.Reg32(reg)
		default:
			val = asm.Reg64(reg)
		}

		values = append(values, val)
		regs = append(regs, reg)
	}

	c.emit(asm.Printf(p.format, values...))

	for _, reg := range regs {
		c.freeReg(reg)
	}
	return nil
}

func (c *compiler) evalValue(expr Fragment) (asm.Variable, valueWidth, error) {
	switch v := expr.(type) {
	case Var:
		reg, err := c.loadVar64(string(v))
		if err != nil {
			return 0, 0, err
		}
		return reg, width64, nil
	case i32Var:
		reg, err := c.allocReg()
		if err != nil {
			return 0, 0, err
		}
		mem, err := c.stackSlotMem(string(v.name))
		if err != nil {
			c.freeReg(reg)
			return 0, 0, err
		}
		c.emit(asm.MovFromMemory(asm.Reg32(reg), mem))
		return reg, width32, nil
	case i8Var:
		reg, err := c.allocReg()
		if err != nil {
			return 0, 0, err
		}
		mem, err := c.stackSlotMem(string(v.name))
		if err != nil {
			c.freeReg(reg)
			return 0, 0, err
		}
		c.emit(asm.MovZX8(asm.Reg64(reg), mem))
		return reg, width8, nil
	case memVar:
		base, err := c.loadVar64(string(v.base))
		if err != nil {
			return 0, 0, err
		}
		disp, err := c.resolveDisp(v.disp)
		if err != nil {
			c.freeReg(base)
			return 0, 0, err
		}
		mem := asm.Mem(asm.Reg64(base)).WithDisp(disp)
		width := v.width
		if width == 0 {
			width = width64
		}
		switch width {
		case width8:
			c.emit(asm.MovZX8(asm.Reg64(base), mem))
		case width16:
			c.emit(asm.MovZX16(asm.Reg64(base), mem))
		case width32:
			c.emit(asm.MovFromMemory(asm.Reg32(base), mem))
		default:
			c.emit(asm.MovFromMemory(asm.Reg64(base), mem))
		}
		return base, width64, nil
	case globalMem:
		base, err := c.loadGlobalAddress(v.name)
		if err != nil {
			return 0, 0, err
		}
		disp, err := c.resolveDisp(v.disp)
		if err != nil {
			c.freeReg(base)
			return 0, 0, err
		}
		mem := asm.Mem(asm.Reg64(base)).WithDisp(disp)
		width := v.width
		if width == 0 {
			width = width64
		}
		switch width {
		case width8:
			c.emit(asm.MovZX8(asm.Reg64(base), mem))
		case width16:
			c.emit(asm.MovZX16(asm.Reg64(base), mem))
		case width32:
			c.emit(asm.MovFromMemory(asm.Reg32(base), mem))
		default:
			c.emit(asm.MovFromMemory(asm.Reg64(base), mem))
		}
		return base, width64, nil
	case stackSlotMemFragment:
		offset, err := c.stackSlotOffset(v.slotID, v.disp)
		if err != nil {
			return 0, 0, err
		}
		reg, err := c.allocReg()
		if err != nil {
			return 0, 0, err
		}
		mem := asm.Mem(asm.Reg64(asm.RSP)).WithDisp(offset)
		c.emit(asm.MovFromMemory(asm.Reg64(reg), mem))
		return reg, width64, nil
	case stackSlotPtrFragment:
		offset, err := c.stackSlotOffset(v.slotID, v.disp)
		if err != nil {
			return 0, 0, err
		}
		reg, err := c.allocRegPrefer(asm.RAX)
		if err != nil {
			return 0, 0, err
		}
		c.emit(asm.MovReg(asm.Reg64(reg), asm.Reg64(asm.RSP)))
		if offset != 0 {
			c.emit(asm.AddRegImm(asm.Reg64(reg), offset))
		}
		return reg, width64, nil
	case Int64:
		reg, err := c.allocRegPrefer(asm.RAX)
		if err != nil {
			return 0, 0, err
		}
		c.emit(asm.MovImmediate(asm.Reg64(reg), int64(v)))
		return reg, width64, nil
	case Int32:
		reg, err := c.allocRegPrefer(asm.RAX)
		if err != nil {
			return 0, 0, err
		}
		c.emit(asm.MovImmediate(asm.Reg32(reg), int64(v)))
		return reg, width32, nil
	case Int16:
		reg, err := c.allocRegPrefer(asm.RAX)
		if err != nil {
			return 0, 0, err
		}
		c.emit(asm.MovImmediate(asm.Reg16(reg), int64(v)))
		return reg, width16, nil
	case Int8:
		reg, err := c.allocRegPrefer(asm.RAX)
		if err != nil {
			return 0, 0, err
		}
		c.emit(asm.MovImmediate(asm.Reg8(reg), int64(v)))
		return reg, width8, nil
	case syscallFragment:
		reg, err := c.compileSyscall(v, true)
		if err != nil {
			return 0, 0, err
		}
		return reg, width64, nil
	case opFragment:
		return c.evalOp(v)
	case constantPointerFragment:
		reg, err := c.allocRegPrefer(asm.RAX)
		if err != nil {
			return 0, 0, err
		}
		c.emit(asm.LoadAddress(asm.Reg64(reg), v.target))
		return reg, width64, nil
	case methodPointerFragment:
		reg, err := c.allocRegPrefer(asm.RAX)
		if err != nil {
			return 0, 0, err
		}
		c.emit(asm.MovImmediate(asm.Reg64(reg), int64(methodPointerPlaceholder(v.name))))
		return reg, width64, nil
	case globalPointerFragment:
		reg, err := c.allocRegPrefer(asm.RAX)
		if err != nil {
			return 0, 0, err
		}
		c.emit(asm.MovImmediate(asm.Reg64(reg), int64(globalPointerPlaceholder(v.name))))
		return reg, width64, nil
	default:
		if imm, ok := toInt64(v); ok {
			reg, err := c.allocRegPrefer(asm.RAX)
			if err != nil {
				return 0, 0, err
			}
			c.emit(asm.MovImmediate(asm.Reg64(reg), imm))
			return reg, width64, nil
		}
		return 0, 0, fmt.Errorf("ir: unsupported expression %T", expr)
	}
}

func (c *compiler) evalOp(op opFragment) (asm.Variable, valueWidth, error) {
	switch op.kind {
	case OpAdd:
		leftReg, _, err := c.evalValue(op.left)
		if err != nil {
			return 0, 0, err
		}
		rightReg, _, err := c.evalValue(op.right)
		if err != nil {
			c.freeReg(leftReg)
			return 0, 0, err
		}
		c.emit(asm.AddRegReg(asm.Reg64(leftReg), asm.Reg64(rightReg)))
		c.freeReg(rightReg)
		return leftReg, width64, nil
	case OpSub:
		leftReg, _, err := c.evalValue(op.left)
		if err != nil {
			return 0, 0, err
		}
		rightReg, _, err := c.evalValue(op.right)
		if err != nil {
			c.freeReg(leftReg)
			return 0, 0, err
		}
		c.emit(asm.SubRegReg(asm.Reg64(leftReg), asm.Reg64(rightReg)))
		c.freeReg(rightReg)
		return leftReg, width64, nil
	case OpShr:
		leftReg, _, err := c.evalValue(op.left)
		if err != nil {
			return 0, 0, err
		}
		shift, ok := toInt64(op.right)
		if !ok {
			c.freeReg(leftReg)
			return 0, 0, fmt.Errorf("ir: shift amount must be immediate")
		}
		if shift < 0 || shift > math.MaxUint8 {
			c.freeReg(leftReg)
			return 0, 0, fmt.Errorf("ir: shift amount %d out of range", shift)
		}
		c.emit(asm.ShrRegImm(asm.Reg64(leftReg), uint8(shift)))
		return leftReg, width64, nil
	case OpShl:
		leftReg, _, err := c.evalValue(op.left)
		if err != nil {
			return 0, 0, err
		}
		shift, ok := toInt64(op.right)
		if !ok {
			c.freeReg(leftReg)
			return 0, 0, fmt.Errorf("ir: shift amount must be immediate")
		}
		if shift < 0 || shift > math.MaxUint8 {
			c.freeReg(leftReg)
			return 0, 0, fmt.Errorf("ir: shift amount %d out of range", shift)
		}
		c.emit(asm.ShlRegImm(asm.Reg64(leftReg), uint8(shift)))
		return leftReg, width64, nil
	case OpAnd:
		leftReg, _, err := c.evalValue(op.left)
		if err != nil {
			return 0, 0, err
		}
		if imm, ok := toInt64(op.right); ok {
			if imm >= math.MinInt32 && imm <= math.MaxInt32 {
				c.emit(asm.AndRegImm(asm.Reg64(leftReg), int32(imm)))
				return leftReg, width64, nil
			}
		}
		rightReg, _, err := c.evalValue(op.right)
		if err != nil {
			c.freeReg(leftReg)
			return 0, 0, err
		}
		c.emit(asm.AndRegReg(asm.Reg64(leftReg), asm.Reg64(rightReg)))
		c.freeReg(rightReg)
		return leftReg, width64, nil
	default:
		return 0, 0, fmt.Errorf("ir: unsupported op kind %d", op.kind)
	}
}

func (c *compiler) stackSlotMem(name string) (asm.Memory, error) {
	offset, ok := c.varOffsets[name]
	if !ok {
		return asm.Memory{}, fmt.Errorf("ir: unknown variable %q", name)
	}
	return asm.Mem(asm.Reg64(asm.RSP)).WithDisp(offset), nil
}

func (c *compiler) stackSlotOffset(slotID uint64, disp Fragment) (int32, error) {
	for i := len(c.slotStack) - 1; i >= 0; i-- {
		ctx := c.slotStack[i]
		if ctx.id == slotID {
			dispVal, err := c.resolveDisp(disp)
			if err != nil {
				return 0, err
			}
			return ctx.base + dispVal, nil
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
	c.emit(asm.MovFromMemory(asm.Reg64(reg), mem))
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
	c.emit(asm.MovFromMemory(asm.Reg64(reg), mem))
	return reg, nil
}

func (c *compiler) loadGlobalAddress(name string) (asm.Variable, error) {
	reg, err := c.allocRegPrefer(asm.RAX)
	if err != nil {
		return 0, err
	}
	c.emit(asm.MovImmediate(asm.Reg64(reg), int64(globalPointerPlaceholder(name))))
	return reg, nil
}

func (c *compiler) resolveDisp(d Fragment) (int32, error) {
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
	if reg == asm.RSP {
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

func collectVariables(f Fragment, vars map[string]struct{}) {
	switch v := f.(type) {
	case nil:
	case Block:
		for _, inner := range v {
			collectVariables(inner, vars)
		}
	case Method:
		collectVariables(Block(v), vars)
	case Var:
		vars[string(v)] = struct{}{}
	case i32Var:
		vars[string(v.name)] = struct{}{}
	case i8Var:
		vars[string(v.name)] = struct{}{}
	case memVar:
		vars[string(v.base)] = struct{}{}
		if v.disp != nil {
			collectVariables(v.disp, vars)
		}
	case globalMem:
		if v.disp != nil {
			collectVariables(v.disp, vars)
		}
	case DeclareParam:
		vars[string(v)] = struct{}{}
	case assignFragment:
		collectVariables(v.dst, vars)
		collectVariables(v.src, vars)
	case syscallFragment:
		for _, arg := range v.args {
			collectVariables(arg, vars)
		}
	case ifFragment:
		collectConditionVars(v.cond, vars)
		collectVariables(v.then, vars)
		if v.otherwise != nil {
			collectVariables(v.otherwise, vars)
		}
	case gotoFragment:
	case labelFragment:
		collectVariables(v.block, vars)
	case Label:
	case returnFragment:
		collectVariables(v.value, vars)
	case printfFragment:
		for _, arg := range v.args {
			collectVariables(arg, vars)
		}
	case stackSlotFragment:
		for _, name := range v.chunks {
			vars[name] = struct{}{}
		}
		if v.body != nil {
			collectVariables(v.body, vars)
		}
	case stackSlotMemFragment:
		if v.disp != nil {
			collectVariables(v.disp, vars)
		}
	case stackSlotPtrFragment:
		if v.disp != nil {
			collectVariables(v.disp, vars)
		}
	case constantBytesFragment:
		// constants do not reference stack variables
	case constantPointerFragment:
		// pointer fragments also do not reference stack variables
	default:
	}
}

func collectConditionVars(cond Condition, vars map[string]struct{}) {
	switch cv := cond.(type) {
	case isNegativeCondition:
		collectVariables(cv.value, vars)
	case isZeroCondition:
		collectVariables(cv.value, vars)
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
	case Int64:
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

func extractLabelName(label Fragment) (string, error) {
	switch v := label.(type) {
	case Label:
		if v == "" {
			return "", fmt.Errorf("ir: empty label")
		}
		return string(v), nil
	default:
		return "", fmt.Errorf("ir: unsupported label fragment %T", label)
	}
}

var (
	_ Fragment = Block(nil)
	_ Fragment = DeclareParam("")
	_ Fragment = Var("")
	_ Fragment = Label("")
	_ Fragment = syscallFragment{}
	_ Fragment = assignFragment{}
	_ Fragment = ifFragment{}
	_ Fragment = gotoFragment{}
	_ Fragment = Method(nil)
	_ Fragment = returnFragment{}
	_ Fragment = printfFragment{}
	_ Fragment = methodPointerFragment{}
	_ Fragment = globalPointerFragment{}
)

type GlobalConfig struct {
	// Size controls how many bytes are reserved for the variable. Defaults to 8.
	Size int
	// Align controls the byte alignment for the variable. Defaults to 8 and must
	// be a power of two.
	Align int
}

type Program struct {
	Entrypoint string
	Methods    map[string]Method
	Globals    map[string]GlobalConfig
}

type normalizedGlobal struct {
	size  int
	align int
}

func normalizeGlobalConfig(name string, cfg GlobalConfig) (normalizedGlobal, error) {
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

const standaloneAlignment = 16

func (p *Program) buildStandaloneProgram() (asm.Program, error) {
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

	entryFrag, err := entry.Compile()
	if err != nil {
		return asm.Program{}, fmt.Errorf("failed to compile entrypoint method %q: %w", p.Entrypoint, err)
	}

	entryProg, err := asm.EmitProgram(entryFrag)
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
		frag, err := method.Compile()
		if err != nil {
			return asm.Program{}, fmt.Errorf("failed to compile method %q: %w", name, err)
		}
		prog, err := asm.EmitProgram(frag)
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

// CompileStandaloneProgram compiles p into a standalone assembly program
// without wrapping it in an ELF container.
func (p *Program) CompileStandaloneProgram() (asm.Program, error) {
	return p.buildStandaloneProgram()
}

func (p *Program) CompileELF() ([]byte, error) {
	program, err := p.buildStandaloneProgram()
	if err != nil {
		return nil, err
	}

	return program.StandaloneELF()
}
