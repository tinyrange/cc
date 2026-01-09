package ir

import (
	"fmt"
	"sync/atomic"

	"github.com/tinyrange/cc/internal/asm"
	"github.com/tinyrange/cc/internal/linux/defs"
)

// SyscallCheckedConfig describes a syscall invocation that assigns the result
// to a variable and branches to an error handler when the result is negative.
type SyscallCheckedConfig struct {
	// Result stores the syscall return value for later use. It must be a Var so
	// the helper can emit the common negative-result check.
	Result Var
	// Number is the syscall number (for example linux.SYS_OPENAT).
	Number defs.Syscall
	// Args provides the syscall arguments. Each element must be compatible with
	// Syscall (Var, string, or int-convertible value).
	Args []any
	// OnError runs when the syscall result is negative. Use a Block to sequence
	// multiple fragments or Goto to jump to an error label.
	OnError Fragment
}

// SyscallChecked emits the canonical syscall + errno handling pattern used by
// the init runtime. It expands to:
//
//	result = syscall(number, args...)
//	if result < 0 {
//	  onError
//	}
//
// The helper panics if required fields are missing so mistakes surface early
// during method construction.
func SyscallChecked(cfg SyscallCheckedConfig) Fragment {
	if cfg.Result == "" {
		panic("ir: SyscallChecked requires a result variable")
	}
	if cfg.OnError == nil {
		panic("ir: SyscallChecked requires an error handler fragment")
	}

	block := Block{
		Assign(cfg.Result, Syscall(cfg.Number, cfg.Args...)),
		If(IsNegative(cfg.Result), cfg.OnError),
	}
	return block
}

const DefaultStageResultShift = 32

var (
	helperVarCounter uint64
	stackSlotCounter uint64
)

type ConstantBytesFragment struct {
	Target asm.Variable
	Data   []byte
}

// ConstantBytesConfig describes how a constant byte slice should be bound to an
// assembler variable and optionally expose metadata to the IR method.
type ConstantBytesConfig struct {
	// Target selects the asm variable that will reference the constant. Required.
	Target asm.Variable
	// Data provides the literal contents to bind.
	Data []byte
	// ZeroTerminate appends a trailing zero byte when true (unless already
	// present) so the constant can be treated as a C string.
	ZeroTerminate bool
	// Length receives the length of Data before any zero terminator is appended
	// when set. Useful for methods that need the literal size separately.
	Length Var
	// TotalLength receives the full length after optional zero termination when
	// set. Callers can differentiate between the raw length and the encoded size.
	TotalLength Var
	// Pointer optionally receives the address of the constant block. This allows
	// callers to reuse the data without re-encoding string literals.
	Pointer Var
}

func newHelperVar(prefix string) Var {
	id := atomic.AddUint64(&helperVarCounter, 1)
	return Var(fmt.Sprintf("__ir_%s_%d", prefix, id))
}

// LoadConstantBytes mirrors asm.LoadConstantBytes by binding the provided byte
// slice to the supplied constant variable within the emitted program.
func LoadConstantBytes(target asm.Variable, data []byte) Fragment {
	return LoadConstantBytesConfig(ConstantBytesConfig{
		Target: target,
		Data:   data,
	})
}

// LoadConstantBytesConfig binds a byte slice to an asm variable and exposes
// optional metadata such as zero termination and length tracking.
func LoadConstantBytesConfig(cfg ConstantBytesConfig) Fragment {
	if cfg.Target == 0 {
		panic("ir: LoadConstantBytesConfig requires a target variable")
	}
	raw := append([]byte(nil), cfg.Data...)
	originalLen := len(raw)

	if cfg.ZeroTerminate {
		needTerm := true
		if len(raw) > 0 && raw[len(raw)-1] == 0 {
			needTerm = false
		}
		if needTerm {
			raw = append(raw, 0)
		}
	}

	frags := Block{
		ConstantBytesFragment{
			Target: cfg.Target,
			Data:   raw,
		},
	}

	if cfg.Length != "" {
		frags = append(frags, Assign(cfg.Length, Int64(int64(originalLen))))
	}
	if cfg.TotalLength != "" {
		frags = append(frags, Assign(cfg.TotalLength, Int64(int64(len(raw)))))
	}
	if cfg.Pointer != "" {
		frags = append(frags, Assign(cfg.Pointer, ConstantPointer(cfg.Target)))
	}

	return frags
}

type ConstantPointerFragment struct {
	Target asm.Variable
}

// ConstantPointer loads the address of a constant declared via LoadConstantBytes.
func ConstantPointer(variable asm.Variable) Fragment {
	return ConstantPointerFragment{Target: variable}
}

type MethodPointerFragment struct {
	Name string
}

// MethodPointer returns a placeholder for the entry address of the named IR method.
// The actual pointer is patched once the standalone program is linked, so the name
// must correspond to a method included in the Program.
func MethodPointer(name string) Fragment {
	if name == "" {
		panic("ir: MethodPointer requires a method name")
	}
	return MethodPointerFragment{Name: name}
}

// CallMethod emits an indirect call to another IR method.
func CallMethod(name string, result ...Var) Fragment {
	return Call(MethodPointer(name), result...)
}

type GlobalPointerFragment struct {
	Name string
}

// Pointer returns a placeholder pointing at the base address of the global
// variable. The linker resolves the final address once the standalone program
// layout is finalized.
func (g GlobalVar) Pointer() Fragment {
	if g == "" {
		panic("ir: global pointer requires a name")
	}
	return GlobalPointerFragment{Name: string(g)}
}

// AssignGlobal stores value into the provided global variable. It behaves like
// Assign(g.AsMem(), value) but documents the intent at the call site.
func AssignGlobal(g GlobalVar, value any) Fragment {
	return Assign(g.AsMem(), value)
}

// StageResultStoreConfig describes how to split a stage-encoded uint64 into
// the low detail portion and the high stage tag and store them in memory.
type StageResultStoreConfig struct {
	// Base points at the destination region (for example Var("page").AsMem()).
	Base MemoryFragment
	// Offset is the byte displacement for the detail (low 32-bit) field.
	Offset int64
	// Value is the encoded result (detail | stage<<shift) to store.
	Value Var
	// Scratch optionally provides a variable to receive the shifted stage. When
	// omitted the helper allocates an internal temporary.
	Scratch Var
	// Shift controls how many bits to shift Value right to obtain the stage. If
	// zero the helper uses defaultStageResultShift.
	Shift int64
}

// WriteStageResult emits the canonical pattern used by init to store a
// stage-encoded result: copy the low 32 bits to Base+Offset and the stage to
// Base+Offset+4. The function panics if required fields are missing so
// mistakes appear during method construction.
func WriteStageResult(cfg StageResultStoreConfig) Fragment {
	if cfg.Base == nil {
		panic("ir: WriteStageResult requires a base memory reference")
	}
	if cfg.Value == "" {
		panic("ir: WriteStageResult requires a value variable")
	}
	shift := cfg.Shift
	if shift <= 0 {
		shift = DefaultStageResultShift
	}
	scratch := cfg.Scratch
	if scratch == "" {
		scratch = newHelperVar("stage_hi")
	}

	low := cfg.Base.WithDisp(cfg.Offset)
	high := cfg.Base.WithDisp(cfg.Offset + 4)

	return Block{
		Assign(low, cfg.Value.As32()),
		Assign(scratch, Op(OpShr, cfg.Value, Int64(shift))),
		Assign(high, scratch.As32()),
	}
}

// StackSlotConfig describes a temporary stack reservation that should outlive
// a loop body. The helper subtracts Size bytes (rounded up to the requested
// alignment and the ABI stack alignment), exposes the slot via the provided
// StackSlot helper, compiles Body, and restores the stack pointer once the
// fragment completes.
type StackSlotConfig struct {
	// Size is the number of bytes required by the caller. Must be > 0.
	Size int64
	// Body builds the fragment that will run while the stack slot is active.
	Body func(StackSlot) Fragment
}

// StackSlot allows callers to address the reserved memory without having to
// juggle raw stack pointer arithmetic.
type StackSlot struct {
	id   uint64
	size int64
}

// Base returns a MemoryFragment covering the start of the slot.
func (s StackSlot) Base() MemoryFragment {
	return StackSlotMemFragment{SlotID: s.id}
}

// At returns a MemoryFragment located at the provided displacement from the
// slot base.
func (s StackSlot) At(disp any) MemoryFragment {
	return StackSlotMemFragment{SlotID: s.id, Disp: asFragment(disp)}
}

// Pointer returns the address of the slot base.
func (s StackSlot) Pointer() Fragment {
	return StackSlotPtrFragment{SlotID: s.id}
}

// PointerWithDisp returns the address of the slot plus the provided
// displacement.
func (s StackSlot) PointerWithDisp(disp any) Fragment {
	return StackSlotPtrFragment{SlotID: s.id, Disp: asFragment(disp)}
}

// Size reports the total number of bytes reserved for the slot.
func (s StackSlot) Size() int64 {
	return s.size
}

type StackSlotFragment struct {
	Id     uint64
	Size   int64
	Body   Fragment
	Chunks []string
}

// WithStackSlot creates a temporary stack allocation for the duration of Body.
// Callers should avoid returning from inside Body so the cleanup sequence runs.
func WithStackSlot(cfg StackSlotConfig) Fragment {
	if cfg.Size <= 0 {
		panic("ir: WithStackSlot requires a positive size")
	}
	if cfg.Body == nil {
		panic("ir: WithStackSlot requires a body builder")
	}
	id := atomic.AddUint64(&stackSlotCounter, 1)
	chunkBytes := int64(8)
	chunks := int((cfg.Size + chunkBytes - 1) / chunkBytes)
	if chunks == 0 {
		chunks = 1
	}
	totalSize := int64(chunks) * chunkBytes
	names := make([]string, chunks)
	for i := 0; i < chunks; i++ {
		names[i] = fmt.Sprintf("__ir_slot_%d_%04d", id, i)
	}
	slot := StackSlot{id: id, size: totalSize}
	return StackSlotFragment{
		Id:     id,
		Size:   totalSize,
		Body:   cfg.Body(slot),
		Chunks: names,
	}
}

type StackSlotMemFragment struct {
	SlotID uint64
	Disp   Fragment
	Width  ValueWidth // Width of memory access (0 means 64-bit/default)
}

func (m StackSlotMemFragment) WithDisp(disp any) Fragment {
	return StackSlotMemFragment{SlotID: m.SlotID, Disp: asFragment(disp), Width: m.Width}
}

// As8 returns a copy with 8-bit width.
func (m StackSlotMemFragment) As8() StackSlotMemFragment {
	return StackSlotMemFragment{SlotID: m.SlotID, Disp: m.Disp, Width: Width8}
}

// As16 returns a copy with 16-bit width.
func (m StackSlotMemFragment) As16() StackSlotMemFragment {
	return StackSlotMemFragment{SlotID: m.SlotID, Disp: m.Disp, Width: Width16}
}

// As32 returns a copy with 32-bit width.
func (m StackSlotMemFragment) As32() StackSlotMemFragment {
	return StackSlotMemFragment{SlotID: m.SlotID, Disp: m.Disp, Width: Width32}
}

type StackSlotPtrFragment struct {
	SlotID uint64
	Disp   Fragment
}
