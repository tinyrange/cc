package amd64

import (
	"encoding/binary"
	"fmt"
	"math"
	"sync/atomic"

	"github.com/tinyrange/cc/internal/asm"
	"github.com/tinyrange/cc/internal/linux/defs"
	amd64defs "github.com/tinyrange/cc/internal/linux/defs/amd64"
)

const (
	RAX asm.Variable = iota
	RBX
	RCX
	RDX
	RSI
	RDI
	RSP
	RBP
	R8
	R9
	R10
	R11
	R12
	R13
	R14
	R15
)

type loadConstant struct {
	data   []byte
	target asm.Variable
}

type loadPointerArray struct {
	target asm.Variable
	values []asm.Variable
}

func LoadConstantString(target asm.Variable, s string) asm.Fragment {
	return LoadConstantBytes(target, []byte(s))
}

func LoadConstantBytes(target asm.Variable, data []byte) asm.Fragment {
	return &loadConstant{
		data:   append([]byte(nil), data...),
		target: target,
	}
}

type reserveZero struct {
	target asm.Variable
	size   int
}

func ReserveZero(target asm.Variable, size int) asm.Fragment {
	return &reserveZero{
		target: target,
		size:   size,
	}
}

func (r *reserveZero) Emit(ctx asm.Context) error {
	if r.size < 0 {
		return fmt.Errorf("reserve zero requires non-negative size")
	}
	ctx.AddZeroConstant(r.target, r.size)
	return nil
}

func LoadPointerArray(target asm.Variable, values []asm.Variable) asm.Fragment {
	return &loadPointerArray{
		target: target,
		values: append([]asm.Variable(nil), values...),
	}
}

func Move(dst, src asm.Variable) asm.Fragment {
	return &moveRegister{dst: dst, src: src}
}

type moveRegister struct {
	dst asm.Variable
	src asm.Variable
}

type jump struct {
	label asm.Label
	kind  jumpKind
}

type jumpRaw struct {
	value asm.Variable
}

type call struct {
	label asm.Label
}

type testZero struct {
	reg asm.Variable
}

type ret struct {
}

type syscall struct {
	number defs.Syscall
	args   []asm.Value
}

func Syscall(number defs.Syscall, args ...asm.Value) asm.Fragment {
	return &syscall{
		number: number,
		args:   args,
	}
}

func UseRegister(v asm.Variable) asm.Value {
	return asm.Register(v)
}

func SyscallWrite(fd asm.Value, buf asm.Value, count asm.Value) asm.Fragment {
	return &syscall{
		number: defs.SYS_WRITE,
		args:   []asm.Value{fd, buf, count},
	}
}

func SyscallWriteString(fd asm.Value, s string) asm.Fragment {
	return SyscallWrite(fd, asm.LiteralBytes([]byte(s)), asm.Immediate(len(s)))
}

func Print(s string) asm.Fragment {
	saved := []asm.Variable{RAX, RDI, RSI, RDX, RCX, R11, R15}
	const regWidth = 8
	stackSize := int32(len(saved) * regWidth)

	frags := make([]asm.Fragment, 0, len(saved)*2+3)
	frags = append(frags, AddRegImm(Reg64(RSP), -stackSize))

	for idx, reg := range saved {
		offset := int32(idx * regWidth)
		frags = append(frags, MovToMemory(Mem(Reg64(RSP)).WithDisp(offset), Reg64(reg)))
	}

	frags = append(frags, writeStdoutWithKmsgFallback(
		asm.LiteralBytes([]byte(s)),
		asm.Immediate(len(s)),
	))

	for idx := len(saved) - 1; idx >= 0; idx-- {
		offset := int32(idx * regWidth)
		reg := saved[idx]
		frags = append(frags, MovFromMemory(Reg64(reg), Mem(Reg64(RSP)).WithDisp(offset)))
	}

	frags = append(frags, AddRegImm(Reg64(RSP), stackSize))
	return asm.Group(frags)
}

var printfLabelCounter uint64

func writeStdoutWithKmsgFallback(buf asm.Value, count asm.Value) asm.Fragment {
	id := atomic.AddUint64(&printfLabelCounter, 1)
	errLabel := asm.Label(fmt.Sprintf("__printf_write_err_%d", id))
	doneLabel := asm.Label(fmt.Sprintf("__printf_write_done_%d", id))

	return asm.Group{
		SyscallWrite(asm.Immediate(1), buf, count),
		TestZero(RAX),
		JumpIfNegative(errLabel),
		Jump(doneLabel),
		asm.MarkLabel(errLabel),
		Syscall(defs.SYS_OPENAT,
			asm.Immediate(amd64defs.AT_FDCWD),
			asm.String("/dev/kmsg"),
			asm.Immediate(amd64defs.O_WRONLY),
			asm.Immediate(0),
		),
		TestZero(RAX),
		JumpIfNegative(doneLabel),
		MovReg(Reg64(R15), Reg64(RAX)),
		SyscallWriteString(UseRegister(R15), "printf error: using fallback kmsg\n"),
		SyscallWrite(UseRegister(R15), buf, count),
		Syscall(defs.SYS_CLOSE, UseRegister(R15)),
		asm.MarkLabel(doneLabel),
	}
}

// Printf writes a formatted string to stdout.
// Format only supports %x for hexadecimal output.
func Printf(format string, args ...asm.Value) asm.Fragment {
	const (
		regWidth   = 8
		bufferSize = 32
		hexDigits  = 16
	)

	type formatPart struct {
		text     string
		argIndex int // -1 for literal text
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
			panic("asm.Printf: trailing % at end of format string")
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
			panic(fmt.Sprintf("asm.Printf: unsupported format specifier %%%c", next))
		}
	}
	flushLiteral()

	if placeholderCount != len(args) {
		panic(fmt.Sprintf("asm.Printf: argument count mismatch: format expects %d args, got %d", placeholderCount, len(args)))
	}

	if len(parts) == 0 {
		return asm.Group{}
	}
	saved := []asm.Variable{RAX, RDI, RSI, RDX, RCX, R8, R9, R10, R11, R12, R13, R14, R15}
	regAreaSize := len(saved) * regWidth
	totalStack := int32(regAreaSize + bufferSize)
	bufferOffset := int32(regAreaSize)
	savedOffsets := make(map[asm.Variable]int32, len(saved))

	frags := make([]asm.Fragment, 0, len(parts)*8+len(saved)*2+8)
	frags = append(frags, AddRegImm(Reg64(RSP), -totalStack))
	for idx, reg := range saved {
		offset := int32(idx * regWidth)
		savedOffsets[reg] = offset
		frags = append(frags, MovToMemory(Mem(Reg64(RSP)).WithDisp(offset), Reg64(reg)))
	}

	frags = append(frags,
		MovReg(Reg64(R12), Reg64(RSP)),
		AddRegImm(Reg64(R12), bufferOffset),
	)

	loadArg := func(val asm.Value) []asm.Fragment {
		switch v := val.(type) {
		case asm.Immediate:
			return []asm.Fragment{MovImmediate(Reg64(R8), int64(v))}
		case int:
			return []asm.Fragment{MovImmediate(Reg64(R8), int64(v))}
		case int32:
			return []asm.Fragment{MovImmediate(Reg64(R8), int64(v))}
		case int64:
			return []asm.Fragment{MovImmediate(Reg64(R8), v)}
		case uint64:
			return []asm.Fragment{MovImmediate(Reg64(R8), int64(v))}
		case Reg:
			if offset, ok := savedOffsets[v.id]; ok {
				return []asm.Fragment{MovFromMemory(Reg64(R8), Mem(Reg64(RSP)).WithDisp(offset))}
			}
			switch v.size {
			case size64:
				return []asm.Fragment{MovReg(Reg64(R8), Reg64(v.id))}
			case size32:
				return []asm.Fragment{MovReg(Reg32(R8), Reg32(v.id))}
			default:
				panic("asm.Printf: only 32- or 64-bit registers supported for %x")
			}
		case asm.Variable:
			if offset, ok := savedOffsets[v]; ok {
				return []asm.Fragment{MovFromMemory(Reg64(R8), Mem(Reg64(RSP)).WithDisp(offset))}
			}
			return []asm.Fragment{MovReg(Reg64(R8), Reg64(v))}
		case asm.Register:
			if offset, ok := savedOffsets[asm.Variable(v)]; ok {
				return []asm.Fragment{MovFromMemory(Reg64(R8), Mem(Reg64(RSP)).WithDisp(offset))}
			}
			return []asm.Fragment{MovReg(Reg64(R8), Reg64(asm.Variable(v)))}
		default:
			panic(fmt.Sprintf("asm.Printf: unsupported argument type %T", v))
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
		loopLabel := asm.Label(fmt.Sprintf("__printf_hex_loop_%d", id))
		lessLabel := asm.Label(fmt.Sprintf("__printf_hex_less_%d", id))
		storeLabel := asm.Label(fmt.Sprintf("__printf_hex_store_%d", id))
		trimLoopLabel := asm.Label(fmt.Sprintf("__printf_hex_trim_loop_%d", id))
		trimDoneLabel := asm.Label(fmt.Sprintf("__printf_hex_trim_done_%d", id))

		frags = append(frags,
			MovReg(Reg64(R9), Reg64(R12)),
			AddRegImm(Reg64(R9), hexDigits-1),
			MovImmediate(Reg64(R13), hexDigits),
			asm.MarkLabel(loopLabel),
			MovReg(Reg64(R10), Reg64(R8)),
			AndRegImm(Reg64(R10), 0xF),
			CmpRegImm(Reg64(R10), 10),
			JumpIfLess(lessLabel),
			AddRegImm(Reg64(R10), int32('a'-10)),
			Jump(storeLabel),
			asm.MarkLabel(lessLabel),
			AddRegImm(Reg64(R10), int32('0')),
			asm.MarkLabel(storeLabel),
			MovToMemory(Mem(Reg64(R9)), Reg8(R10)),
			ShrRegImm(Reg64(R8), 4),
			AddRegImm(Reg64(R9), -1),
			AddRegImm(Reg64(R13), -1),
			CmpRegImm(Reg64(R13), 0),
			JumpIfGreater(loopLabel),

			MovImmediate(Reg64(R14), 0),
			asm.MarkLabel(trimLoopLabel),
			CmpRegImm(Reg64(R14), hexDigits-1),
			JumpIfEqual(trimDoneLabel),
			MovZX8(Reg64(R10), MemIndex(Reg64(R12), Reg64(R14), 1)),
			CmpRegImm(Reg64(R10), int32('0')),
			JumpIfNotEqual(trimDoneLabel),
			AddRegImm(Reg64(R14), 1),
			Jump(trimLoopLabel),
			asm.MarkLabel(trimDoneLabel),
			MovReg(Reg64(R9), Reg64(R12)),
			AddRegReg(Reg64(R9), Reg64(R14)),
			MovImmediate(Reg64(R13), hexDigits),
			SubRegReg(Reg64(R13), Reg64(R14)),
			writeStdoutWithKmsgFallback(UseRegister(R9), UseRegister(R13)),
		)
	}

	for idx := len(saved) - 1; idx >= 0; idx-- {
		offset := int32(idx * regWidth)
		reg := saved[idx]
		frags = append(frags, MovFromMemory(Reg64(reg), Mem(Reg64(RSP)).WithDisp(offset)))
	}
	frags = append(frags, AddRegImm(Reg64(RSP), totalStack))

	return asm.Group(frags)
}

func Ret() asm.Fragment {
	return &ret{}
}

func JumpRaw(value asm.Variable) asm.Fragment {
	return &jumpRaw{value: value}
}

func Jump(label asm.Label) asm.Fragment {
	return &jump{label: label, kind: jumpAlways}
}

func JumpIfZero(label asm.Label) asm.Fragment {
	return &jump{label: label, kind: jumpEqual}
}

func JumpIfNegative(label asm.Label) asm.Fragment {
	return &jump{label: label, kind: jumpSign}
}

func Call(label asm.Label) asm.Fragment {
	return &call{label: label}
}

func TestZero(reg asm.Variable) asm.Fragment {
	return &testZero{reg: reg}
}

func (l *loadConstant) Emit(ctx asm.Context) error {
	ctx.AddConstant(l.target, l.data)
	return nil
}

func (m *moveRegister) Emit(ctx asm.Context) error {
	if m.dst == m.src {
		return nil
	}
	bytes, err := encodeMovRegReg(Reg64(m.dst), Reg64(m.src))
	if err != nil {
		return err
	}
	ctx.EmitBytes(bytes)
	return nil
}

func (l *loadPointerArray) Emit(_ctx asm.Context) error {
	ctx := _ctx.(*Context)
	offset := len(ctx.constData)
	for idx, value := range l.values {
		loc, ok := ctx.ConstantLocation(value)
		if !ok {
			return fmt.Errorf("no data bound to asm.Variable %d", value)
		}
		ctx.constData = append(ctx.constData, make([]byte, 8)...)
		ctx.addDataPatch(offset+idx*8, sectionConst, loc)
	}
	ctx.constData = append(ctx.constData, make([]byte, 8)...) // null terminator
	ctx.constLocations[l.target] = constantLocation{
		section: sectionConst,
		offset:  offset,
	}
	return nil
}

func (t *testZero) Emit(ctx asm.Context) error {
	bytes, err := encodeTestRegRegSized(Reg64(t.reg), Reg64(t.reg))
	if err != nil {
		return err
	}
	ctx.EmitBytes(bytes)
	return nil
}

func (r *ret) Emit(ctx asm.Context) error {
	ctx.EmitBytes(encodeRet())
	return nil
}

func (j *jumpRaw) Emit(_ctx asm.Context) error {
	ctx := _ctx.(*Context)
	bytes, err := ctx.encodeJumpReg(j.value)
	if err != nil {
		return err
	}
	ctx.EmitBytes(bytes)
	return nil
}

func (j *jump) Emit(_ctx asm.Context) error {
	ctx := _ctx.(*Context)
	pos := ctx.emitJump(j.kind)
	ctx.jumps = append(ctx.jumps, jumpPatch{label: j.label, pos: pos})
	return nil
}

func (c *call) Emit(_ctx asm.Context) error {
	ctx := _ctx.(*Context)
	ctx.EmitBytes([]byte{0xE8, 0, 0, 0, 0})
	pos := len(ctx.text) - 4
	ctx.calls = append(ctx.calls, callPatch{label: c.label, pos: pos})
	return nil
}

func (s *syscall) Emit(_ctx asm.Context) error {
	ctx := _ctx.(*Context)

	num, ok := amd64defs.SyscallMap[s.number]
	if !ok {
		return fmt.Errorf("amd64 asm: unknown syscall number %d", s.number)
	}

	if err := appendMovImmediate(ctx, RAX, int64(num)); err != nil {
		return err
	}

	argRegs := []asm.Variable{RDI, RSI, RDX, R10, R8, R9}
	if len(s.args) > len(argRegs) {
		return fmt.Errorf("too many syscall arguments: %d", len(s.args))
	}

	for idx, arg := range s.args {
		reg := argRegs[idx]
		switch v := arg.(type) {
		case asm.Immediate:
			if err := appendMovImmediate(ctx, reg, int64(v)); err != nil {
				return err
			}
		case asm.Variable:
			if loc, ok := ctx.ConstantLocation(v); ok {
				pos, err := appendMovDataPointer(ctx, reg, loc.offset)
				if err != nil {
					return err
				}
				ctx.addTextPatch(pos, loc)
				continue
			}
			if err := moveRegisterValue(ctx, reg, v); err != nil {
				return err
			}
		case asm.Register:
			if err := moveRegisterValue(ctx, reg, asm.Variable(v)); err != nil {
				return err
			}
		case asm.LiteralValue:
			offset := ctx.literalOffset(v)
			pos, err := appendMovDataPointer(ctx, reg, offset)
			if err != nil {
				return err
			}
			ctx.addTextPatch(pos, constantLocation{section: sectionLiteral, offset: offset})
		default:
			return fmt.Errorf("unsupported syscall argument %T", arg)
		}
	}

	ctx.EmitBytes(syscallOpcode())
	return nil
}

func EmitProgram(fragment asm.Fragment) (asm.Program, error) {
	ctx := newContext()
	if err := fragment.Emit(ctx); err != nil {
		return asm.Program{}, err
	}
	return ctx.finalize()
}

func EmitBytes(fragment asm.Fragment) ([]byte, error) {
	prog, err := EmitProgram(fragment)
	if err != nil {
		return nil, err
	}
	return prog.Bytes(), nil
}

type Context struct {
	text           []byte
	constData      []byte
	literalData    []byte
	constLocations map[asm.Variable]constantLocation
	patches        []patch
	literals       map[dataKey]int
	labels         map[asm.Label]int
	jumps          []jumpPatch
	calls          []callPatch
	bssSize        int
}

type patch struct {
	inText     bool
	pos        int
	target     constantLocation
	ptrSection dataSection
}

type dataKey struct {
	key      string
	zeroTerm bool
}

type jumpPatch struct {
	label asm.Label
	pos   int
}

type callPatch struct {
	label asm.Label
	pos   int
}

type dataSection int

const (
	sectionLiteral dataSection = iota
	sectionConst
	sectionBSS
)

type constantLocation struct {
	section dataSection
	offset  int
}

type jumpKind int

const (
	jumpAlways jumpKind = iota
	jumpEqual
	jumpSign
	jumpNotEqual
	jumpNotZero
	jumpAboveOrEqual
	jumpBelowOrEqual
	jumpAbove
	jumpLess
	jumpGreater
)

func newContext() *Context {
	return &Context{
		constLocations: make(map[asm.Variable]constantLocation),
		literals:       make(map[dataKey]int),
		labels:         make(map[asm.Label]int),
	}
}

func (c *Context) GetLabel(label asm.Label) (int, bool) {
	pos, ok := c.labels[label]
	return pos, ok
}

func (c *Context) SetLabel(label asm.Label) {
	c.labels[label] = len(c.text)
}

func (c *Context) AddConstant(target asm.Variable, data []byte) {
	offset := len(c.constData)
	c.constLocations[target] = constantLocation{
		section: sectionConst,
		offset:  offset,
	}
	c.constData = append(c.constData, data...)
}

func (c *Context) AddZeroConstant(target asm.Variable, size int) {
	if size < 0 {
		panic("AddZeroConstant: negative size")
	}
	const bssAlign = 16
	offset := alignTo(c.bssSize, bssAlign)
	c.constLocations[target] = constantLocation{
		section: sectionBSS,
		offset:  offset,
	}
	c.bssSize = offset + size
}

func (c *Context) EmitBytes(code []byte) {
	c.text = append(c.text, code...)
}

func (c *Context) ConstantLocation(v asm.Variable) (constantLocation, bool) {
	loc, ok := c.constLocations[v]
	return loc, ok
}

func (c *Context) addTextPatch(pos int, target constantLocation) {
	c.patches = append(c.patches, patch{inText: true, pos: pos, target: target})
}

func (c *Context) addDataPatch(pos int, ptrSection dataSection, target constantLocation) {
	c.patches = append(c.patches, patch{
		inText:     false,
		pos:        pos,
		target:     target,
		ptrSection: ptrSection,
	})
}

func appendMovImmediate(ctx *Context, reg asm.Variable, value int64) error {
	const (
		maxUint32 = (1 << 32) - 1
		minInt32  = -1 << 31
	)
	switch {
	case value >= 0 && value <= int64(maxUint32):
		bytes, err := encodeMovRegImm32(reg, uint32(value))
		if err != nil {
			return err
		}
		ctx.EmitBytes(bytes)
		return nil
	case value >= minInt32 && value < 0:
		bytes, err := encodeMovRegImm32Sign(reg, uint32(value))
		if err != nil {
			return err
		}
		ctx.EmitBytes(bytes)
		return nil
	}
	bytes, _, err := encodeMovRegImm64(reg, uint64(value))
	if err != nil {
		return err
	}
	ctx.EmitBytes(bytes)
	return nil
}

func appendMovDataPointer(ctx *Context, reg asm.Variable, offset int) (int, error) {
	bytes, immIdx, err := encodeMovRegImm64(reg, 0)
	if err != nil {
		return 0, err
	}
	pos := len(ctx.text) + immIdx
	ctx.EmitBytes(bytes)
	return pos, nil
}

func appendMovDataPointerReg(ctx *Context, reg Reg, offset int) (int, error) {
	if reg.size != size64 {
		return 0, fmt.Errorf("data pointer load requires 64-bit register")
	}
	bytes, immIdx, err := encodeMovRegImm64(reg.id, 0)
	if err != nil {
		return 0, err
	}
	pos := len(ctx.text) + immIdx
	ctx.EmitBytes(bytes)
	return pos, nil
}

func moveRegisterValue(ctx *Context, dst, src asm.Variable) error {
	if dst == src {
		return nil
	}
	bytes, err := encodeMovRegReg(Reg64(dst), Reg64(src))
	if err != nil {
		return err
	}
	ctx.EmitBytes(bytes)
	return nil
}

func alignTo(value, boundary int) int {
	if boundary <= 0 {
		return value
	}
	mask := boundary - 1
	return (value + mask) &^ mask
}

func (c *Context) finalize() (asm.Program, error) {
	const align = 8
	if rem := len(c.text) % align; rem != 0 {
		padding := align - rem
		c.text = append(c.text, make([]byte, padding)...)
	}

	textLen := len(c.text)
	literalLen := len(c.literalData)
	constLen := len(c.constData)

	dataBase := len(c.text)
	finalData := make([]byte, 0, literalLen+constLen)
	finalData = append(finalData, c.literalData...)
	finalData = append(finalData, c.constData...)

	relocations := make([]int, 0, len(c.patches))

	sectionBase := func(section dataSection) (int, error) {
		switch section {
		case sectionLiteral:
			return 0, nil
		case sectionConst:
			return literalLen, nil
		case sectionBSS:
			return literalLen + constLen, nil
		default:
			return 0, fmt.Errorf("unknown data section %d", section)
		}
	}

	for _, p := range c.patches {
		targetBase, err := sectionBase(p.target.section)
		if err != nil {
			return asm.Program{}, err
		}
		absolute := dataBase + targetBase + p.target.offset
		if p.inText {
			if p.pos+8 > len(c.text) {
				return asm.Program{}, fmt.Errorf("text patch position out of range")
			}
			binary.LittleEndian.PutUint64(c.text[p.pos:p.pos+8], uint64(absolute))
			relocations = append(relocations, p.pos)
			continue
		}
		ptrBase, err := sectionBase(p.ptrSection)
		if err != nil {
			return asm.Program{}, err
		}
		if p.ptrSection == sectionBSS {
			return asm.Program{}, fmt.Errorf("cannot place pointer in BSS")
		}
		index := ptrBase + p.pos
		if index+8 > len(finalData) {
			return asm.Program{}, fmt.Errorf("data patch position out of range")
		}
		binary.LittleEndian.PutUint64(finalData[index:index+8], uint64(absolute))
		relocations = append(relocations, textLen+index)
	}

	for _, j := range c.jumps {
		target, ok := c.labels[j.label]
		if !ok {
			return asm.Program{}, fmt.Errorf("undefined label %q", j.label)
		}
		rel := target - (j.pos + 4)
		if rel < math.MinInt32 || rel > math.MaxInt32 {
			return asm.Program{}, fmt.Errorf("jump to label %q out of range", j.label)
		}
		binary.LittleEndian.PutUint32(c.text[j.pos:j.pos+4], uint32(int32(rel)))
	}

	for _, cpatch := range c.calls {
		target, ok := c.labels[cpatch.label]
		if !ok {
			return asm.Program{}, fmt.Errorf("undefined label %q", cpatch.label)
		}
		rel := target - (cpatch.pos + 4)
		if rel < math.MinInt32 || rel > math.MaxInt32 {
			return asm.Program{}, fmt.Errorf("call to label %q out of range", cpatch.label)
		}
		binary.LittleEndian.PutUint32(c.text[cpatch.pos:cpatch.pos+4], uint32(int32(rel)))
	}

	code := append(c.text, finalData...)
	return asm.NewProgram(code, relocations, c.bssSize), nil
}

func (c *Context) literalOffset(l asm.LiteralValue) int {
	key := dataKey{key: string(l.Data), zeroTerm: l.ZeroTerm}
	if offset, ok := c.literals[key]; ok {
		return offset
	}
	offset := len(c.literalData)
	c.literalData = append(c.literalData, l.Data...)
	if l.ZeroTerm {
		c.literalData = append(c.literalData, 0)
	}
	c.literals[key] = offset
	return offset
}

type registerCode struct {
	code     byte
	high     bool
	needsRex bool
}

func regInfo(v asm.Variable) (registerCode, error) {
	switch v {
	case RAX:
		return registerCode{code: 0, high: false}, nil
	case RBX:
		return registerCode{code: 3, high: false}, nil
	case RCX:
		return registerCode{code: 1, high: false}, nil
	case RDX:
		return registerCode{code: 2, high: false}, nil
	case RSI:
		return registerCode{code: 6, high: false, needsRex: true}, nil
	case RDI:
		return registerCode{code: 7, high: false, needsRex: true}, nil
	case RSP:
		return registerCode{code: 4, high: false, needsRex: true}, nil
	case RBP:
		return registerCode{code: 5, high: false, needsRex: true}, nil
	case R8:
		return registerCode{code: 0, high: true, needsRex: true}, nil
	case R9:
		return registerCode{code: 1, high: true, needsRex: true}, nil
	case R10:
		return registerCode{code: 2, high: true, needsRex: true}, nil
	case R11:
		return registerCode{code: 3, high: true, needsRex: true}, nil
	case R12:
		return registerCode{code: 4, high: true, needsRex: true}, nil
	case R13:
		return registerCode{code: 5, high: true, needsRex: true}, nil
	case R14:
		return registerCode{code: 6, high: true, needsRex: true}, nil
	case R15:
		return registerCode{code: 7, high: true, needsRex: true}, nil
	default:
		return registerCode{}, fmt.Errorf("unsupported register %d", v)
	}
}

func encodeMovRegImm32(reg asm.Variable, value uint32) ([]byte, error) {
	info, err := regInfo(reg)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, 6)
	if prefix := rexPrefix(false, false, false, info.high); prefix != 0 {
		out = append(out, prefix)
	}
	out = append(out, 0xB8+info.code)
	var imm [4]byte
	binary.LittleEndian.PutUint32(imm[:], value)
	out = append(out, imm[:]...)
	return out, nil
}

func encodeMovRegImm32Sign(reg asm.Variable, value uint32) ([]byte, error) {
	info, err := regInfo(reg)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, 7)
	if prefix := rexPrefix(true, false, false, info.high); prefix != 0 {
		out = append(out, prefix)
	}
	out = append(out, 0xC7)
	modrm := byte(0xC0 | info.code)
	out = append(out, modrm)
	var imm [4]byte
	binary.LittleEndian.PutUint32(imm[:], value)
	out = append(out, imm[:]...)
	return out, nil
}

func encodeMovRegImm64(reg asm.Variable, value uint64) ([]byte, int, error) {
	info, err := regInfo(reg)
	if err != nil {
		return nil, 0, err
	}
	out := make([]byte, 0, 12)
	if prefix := rexPrefix(true, false, false, info.high); prefix != 0 {
		out = append(out, prefix)
	}
	out = append(out, 0xB8+info.code)
	immPos := len(out)
	var imm [8]byte
	binary.LittleEndian.PutUint64(imm[:], value)
	out = append(out, imm[:]...)
	return out, immPos, nil
}

func (c *Context) encodeJumpReg(reg asm.Variable) ([]byte, error) {
	info, err := regInfo(reg)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, 3)
	if prefix := rexPrefix(false, false, false, info.high); prefix != 0 {
		out = append(out, prefix)
	}
	out = append(out, 0xFF)
	modrm := byte(0xE0 | info.code)
	out = append(out, modrm)
	return out, nil
}

func (c *Context) emitJump(kind jumpKind) int {
	switch kind {
	case jumpAlways:
		c.text = append(c.text, 0xE9)
		pos := len(c.text)
		c.text = append(c.text, 0, 0, 0, 0)
		return pos
	case jumpEqual:
		c.text = append(c.text, 0x0F, 0x84)
		pos := len(c.text)
		c.text = append(c.text, 0, 0, 0, 0)
		return pos
	case jumpNotEqual, jumpNotZero:
		c.text = append(c.text, 0x0F, 0x85)
		pos := len(c.text)
		c.text = append(c.text, 0, 0, 0, 0)
		return pos
	case jumpAboveOrEqual:
		c.text = append(c.text, 0x0F, 0x83)
		pos := len(c.text)
		c.text = append(c.text, 0, 0, 0, 0)
		return pos
	case jumpBelowOrEqual:
		c.text = append(c.text, 0x0F, 0x86)
		pos := len(c.text)
		c.text = append(c.text, 0, 0, 0, 0)
		return pos
	case jumpAbove:
		c.text = append(c.text, 0x0F, 0x87)
		pos := len(c.text)
		c.text = append(c.text, 0, 0, 0, 0)
		return pos
	case jumpLess:
		c.text = append(c.text, 0x0F, 0x8C)
		pos := len(c.text)
		c.text = append(c.text, 0, 0, 0, 0)
		return pos
	case jumpGreater:
		c.text = append(c.text, 0x0F, 0x8F)
		pos := len(c.text)
		c.text = append(c.text, 0, 0, 0, 0)
		return pos
	case jumpSign:
		c.text = append(c.text, 0x0F, 0x88)
		pos := len(c.text)
		c.text = append(c.text, 0, 0, 0, 0)
		return pos
	default:
		panic(fmt.Sprintf("unsupported jump kind %d", kind))
	}
}

func encodeRet() []byte {
	return []byte{0xC3}
}

func encodeTestRegReg(dst, src asm.Variable) ([]byte, error) {
	return encodeTestRegRegSized(Reg64(dst), Reg64(src))
}

func encodeXorRegReg(dst, src asm.Variable) ([]byte, error) {
	return encodeXorRegRegSized(Reg64(dst), Reg64(src))
}

func rexPrefix(w, r, x, b bool) byte {
	if !w && !r && !x && !b {
		return 0
	}
	prefix := byte(0x40)
	if w {
		prefix |= 0x08
	}
	if r {
		prefix |= 0x04
	}
	if x {
		prefix |= 0x02
	}
	if b {
		prefix |= 0x01
	}
	return prefix
}

func syscallOpcode() []byte {
	return []byte{0x0F, 0x05}
}
