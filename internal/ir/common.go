package ir

import (
	"fmt"

	"github.com/tinyrange/cc/internal/linux/defs"
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

type CompareKind int

const (
	CompareEqual CompareKind = iota
	CompareNotEqual
	CompareLess
	CompareLessOrEqual
	CompareGreater
	CompareGreaterOrEqual
)

type CompareCondition struct {
	Kind  CompareKind
	Left  Fragment
	Right Fragment
}

func IsEqual(left, right any) Condition {
	return CompareCondition{
		Kind:  CompareEqual,
		Left:  asFragment(left),
		Right: asFragment(right),
	}
}

func IsNotEqual(left, right any) Condition {
	return CompareCondition{
		Kind:  CompareNotEqual,
		Left:  asFragment(left),
		Right: asFragment(right),
	}
}

func IsLessThan(left, right any) Condition {
	return CompareCondition{
		Kind:  CompareLess,
		Left:  asFragment(left),
		Right: asFragment(right),
	}
}

func IsLessOrEqual(left, right any) Condition {
	return CompareCondition{
		Kind:  CompareLessOrEqual,
		Left:  asFragment(left),
		Right: asFragment(right),
	}
}

func IsGreaterThan(left, right any) Condition {
	return CompareCondition{
		Kind:  CompareGreater,
		Left:  asFragment(left),
		Right: asFragment(right),
	}
}

func IsGreaterOrEqual(left, right any) Condition {
	return CompareCondition{
		Kind:  CompareGreaterOrEqual,
		Left:  asFragment(left),
		Right: asFragment(right),
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

type ValueWidth uint8

const (
	Width8  ValueWidth = 8
	Width16 ValueWidth = 16
	Width32 ValueWidth = 32
	Width64 ValueWidth = 64
)

type I32Var struct {
	Name Var
}

type I8Var struct {
	Name Var
}

func (v Var) As32() Fragment {
	return I32Var{Name: v}
}

func (v Var) As8() Fragment {
	return I8Var{Name: v}
}

type MemVar struct {
	Base  Var
	Disp  Fragment
	Width ValueWidth
}

func (m MemVar) WithDisp(disp any) Fragment {
	m.Disp = asFragment(disp)
	return m
}

func (m MemVar) withWidth(width ValueWidth) MemVar {
	m.Width = width
	return m
}

func (m MemVar) As8() MemVar {
	return m.withWidth(Width8)
}

func (m MemVar) As16() MemVar {
	return m.withWidth(Width16)
}

func (m MemVar) As32() MemVar {
	return m.withWidth(Width32)
}

func (v Var) AsMem() MemoryFragment {
	return MemVar{Base: v, Width: Width64}
}

func (g GlobalVar) AsMem() MemoryFragment {
	return g.Mem()
}

// Mem exposes a typed memory reference so width helpers (As8/As16/As32) may be
// chained without losing the underlying memVar type.
func (v Var) Mem() MemVar {
	return MemVar{Base: v, Width: Width64}
}

// MemWithDisp is equivalent to Mem().WithDisp(disp) but preserves the memVar
// type so callers can chain width conversions.
func (v Var) MemWithDisp(disp any) MemVar {
	return MemVar{Base: v, Width: Width64, Disp: asFragment(disp)}
}

type GlobalMem struct {
	Name  string
	Disp  Fragment
	Width ValueWidth
}

func (m GlobalMem) WithDisp(disp any) Fragment {
	m.Disp = asFragment(disp)
	return m
}

func (m GlobalMem) withWidth(width ValueWidth) GlobalMem {
	m.Width = width
	return m
}

func (m GlobalMem) As8() GlobalMem {
	return m.withWidth(Width8)
}

func (m GlobalMem) As16() GlobalMem {
	return m.withWidth(Width16)
}

func (m GlobalMem) As32() GlobalMem {
	return m.withWidth(Width32)
}

// Mem exposes a typed global memory reference so width helpers may be chained.
func (g GlobalVar) Mem() GlobalMem {
	return GlobalMem{Name: string(g), Width: Width64}
}

// MemWithDisp is equivalent to Mem().WithDisp(disp) while retaining the
// concrete type for chaining width helpers.
func (g GlobalVar) MemWithDisp(disp any) GlobalMem {
	return GlobalMem{Name: string(g), Width: Width64, Disp: asFragment(disp)}
}

type Label string

type SyscallFragment struct {
	Num  defs.Syscall
	Args []Fragment
}

func Syscall(num defs.Syscall, args ...any) Fragment {
	argsFragments := make([]Fragment, 0, len(args))
	for _, arg := range args {
		argsFragments = append(argsFragments, asFragment(arg))
	}
	return SyscallFragment{Num: num, Args: argsFragments}
}

type ReturnFragment struct {
	Value Fragment
}

func Return(value any) Fragment {
	return ReturnFragment{Value: asFragment(value)}
}

type PrintfFragment struct {
	Format string
	Args   []Fragment
}

func Printf(format string, args ...any) Fragment {
	argFragments := make([]Fragment, 0, len(args))
	for _, arg := range args {
		argFragments = append(argFragments, asFragment(arg))
	}
	return PrintfFragment{Format: format, Args: argFragments}
}

type AssignFragment struct {
	Dst Fragment
	Src Fragment
}

func Assign(dst Fragment, src Fragment) Fragment {
	return AssignFragment{Dst: dst, Src: src}
}

type IfFragment struct {
	Cond      Condition
	Then      Fragment
	Otherwise Fragment
}

func If(cond Condition, then Fragment, otherwise ...Fragment) Fragment {
	if len(otherwise) > 0 {
		return IfFragment{Cond: cond, Then: then, Otherwise: otherwise[0]}
	}
	return IfFragment{Cond: cond, Then: then}
}

type GotoFragment struct {
	Label Fragment
}

func Goto(label Fragment) Fragment {
	return GotoFragment{Label: label}
}

type CallFragment struct {
	Target Fragment
	Result Var
}

// Call emits an indirect call to the provided target value. When result is
// specified the callee's return value (RAX) is stored into that variable.
func Call(target any, result ...Var) Fragment {
	var res Var
	if len(result) > 0 {
		res = result[0]
	}
	return CallFragment{
		Target: asFragment(target),
		Result: res,
	}
}

type LabelFragment struct {
	Label Label
	Block Block
}

func DeclareLabel(label Label, block Block) Fragment {
	return LabelFragment{Label: label, Block: block}
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
	OpOr
	OpXor
)

type OpFragment struct {
	Kind  OpKind
	Left  Fragment
	Right Fragment
}

type StackSlotContext struct {
	Id   uint64
	Base int32
	Size int64
}

func Op(kind OpKind, left, right Fragment) Fragment {
	return OpFragment{Kind: kind, Left: left, Right: right}
}

type IsNegativeCondition struct {
	Value Fragment
}

func IsNegative(value Fragment) Condition {
	return IsNegativeCondition{Value: value}
}

type IsZeroCondition struct {
	Value Fragment
}

func IsZero(value Fragment) Condition {
	return IsZeroCondition{Value: value}
}

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

var (
	_ Fragment = Block(nil)
	_ Fragment = DeclareParam("")
	_ Fragment = Var("")
	_ Fragment = Label("")
	_ Fragment = SyscallFragment{}
	_ Fragment = AssignFragment{}
	_ Fragment = IfFragment{}
	_ Fragment = GotoFragment{}
	_ Fragment = Method(nil)
	_ Fragment = ReturnFragment{}
	_ Fragment = PrintfFragment{}
	_ Fragment = MethodPointerFragment{}
	_ Fragment = GlobalPointerFragment{}
)
