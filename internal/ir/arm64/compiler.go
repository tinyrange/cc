package arm64

import (
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"math"
	"sort"
	"strings"

	"github.com/tinyrange/cc/internal/asm"
	arm64asm "github.com/tinyrange/cc/internal/asm/arm64"
	"github.com/tinyrange/cc/internal/ir"
)

const stackAlignment = 16

var paramRegisters = []asm.Variable{
	arm64asm.X0,
	arm64asm.X1,
	arm64asm.X2,
	arm64asm.X3,
	arm64asm.X4,
	arm64asm.X5,
	arm64asm.X6,
	arm64asm.X7,
}

var initialFreeRegisters = []asm.Variable{
	arm64asm.X0,
	arm64asm.X1,
	arm64asm.X2,
	arm64asm.X3,
	arm64asm.X4,
	arm64asm.X5,
	arm64asm.X6,
	arm64asm.X7,
	arm64asm.X8,
	arm64asm.X9,
	arm64asm.X10,
	arm64asm.X11,
	arm64asm.X12,
	arm64asm.X13,
	arm64asm.X14,
	arm64asm.X15,
}

var syscallArgRegisters = []asm.Variable{
	arm64asm.X0,
	arm64asm.X1,
	arm64asm.X2,
	arm64asm.X3,
	arm64asm.X4,
	arm64asm.X5,
}

// printfStackUsage is the stack space Printf requires for register saves and buffer.
// This must be reserved when the method contains Printf calls to prevent stack overlap.
// Printf saves 26 registers (208 bytes) plus 32 bytes buffer = 240 bytes total.
const printfStackUsage = 240

type compiler struct {
	method        ir.Method
	fragments     asm.Group
	varOffsets    map[string]int32
	frameSize     int32
	varFrameSize  int32 // Size of variable area (before printf padding)
	freeRegs      []asm.Variable
	usedRegs      map[asm.Variable]bool
	paramIndex    int
	labels        map[string]asm.Label
	labelCounter  int
	slotStack     []ir.StackSlotContext
	epilogueLabel asm.Label // label for epilogue code (used by return statements)
	hasPrintf     bool      // true if method contains Printf calls
}

func Compile(method ir.Method) (asm.Fragment, error) {
	c, err := newCompiler(method)
	if err != nil {
		return nil, err
	}

	// Check if this method makes calls to other methods.
	// If so, we need to save/restore X30 (link register) since BLR overwrites it.
	makesCalls := methodMakesCalls(method)

	// Create epilogue label for return statements to jump to
	c.epilogueLabel = asm.Label(".ir_epilogue")

	// Prologue: save X29/X30 if method makes calls, then allocate frame
	if makesCalls {
		// STP X29, X30, [SP, #-16]! - save frame pointer and link register
		c.emit(arm64asm.StpPreIndex(arm64asm.X29, arm64asm.X30, arm64asm.SP, -16))
	}
	if c.frameSize > 0 {
		c.emit(arm64asm.AddRegImm(arm64asm.Reg64(arm64asm.SP), -c.frameSize))
	}

	if err := c.compileBlock(ir.Block(method)); err != nil {
		return nil, err
	}

	// Epilogue: label for return statements, deallocate frame, restore X29/X30, ret
	c.emit(asm.MarkLabel(c.epilogueLabel))
	if c.frameSize > 0 {
		c.emit(arm64asm.AddRegImm(arm64asm.Reg64(arm64asm.SP), c.frameSize))
	}
	if makesCalls {
		// LDP X29, X30, [SP], #16 - restore frame pointer and link register
		c.emit(arm64asm.LdpPostIndex(arm64asm.X29, arm64asm.X30, arm64asm.SP, 16))
	}
	c.emit(arm64asm.Ret())
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

	varFrameSize := alignTo(int32(len(names))*8, stackAlignment)

	// Check if the method uses Printf. If so, we need extra padding because
	// Printf adjusts SP down by 240 bytes and stores registers there.
	// Without padding, Printf's saved register area can overlap with our variables.
	hasPrintf := methodUsesPrintf(ir.Block(method))
	frameSize := varFrameSize
	if hasPrintf {
		// Add padding equal to Printf's stack usage to prevent overlap.
		// This ensures that even when Printf does SUB SP, SP, #240,
		// its register save area doesn't overlap with our variables.
		frameSize = alignTo(varFrameSize+printfStackUsage, stackAlignment)
	}

	return &compiler{
		method:       method,
		varOffsets:   offsets,
		varFrameSize: varFrameSize,
		frameSize:    frameSize,
		hasPrintf:    hasPrintf,
		freeRegs:     append([]asm.Variable(nil), initialFreeRegisters...),
		usedRegs:     make(map[asm.Variable]bool),
		labels:       make(map[string]asm.Label),
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
		arm64asm.X9,
		arm64asm.X2,
		arm64asm.X7,
		arm64asm.X6,
		arm64asm.X3,
		arm64asm.X8,
		arm64asm.X4,
		arm64asm.X5,
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
	case ir.ISBFragment:
		c.emit(arm64asm.DSB())
		c.emit(arm64asm.ISB())
		return nil
	case ir.CacheFlushFragment:
		return c.compileCacheFlush(frag)
	case ir.ConstantBytesFragment:
		c.emit(arm64asm.LoadConstantBytes(frag.Target, frag.Data))
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
	c.emit(arm64asm.MovToMemory(mem, arm64asm.Reg64(reg)))
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
	case ir.CallFragment:
		// Handle function calls in assignment context
		// Compile the call, then store the result (X0) to the destination
		if err := c.compileCall(src); err != nil {
			return err
		}
		// The call result is in X0, store it to the destination
		if err := c.storeValue(assign.Dst, arm64asm.X0, ir.Width64); err != nil {
			return err
		}
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
		c.emit(arm64asm.Jump(endLabel))
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
	c.emit(arm64asm.Jump(c.namedLabel(name)))
	return nil
}

// callArgRegisters defines the AAPCS64 argument registers.
var callArgRegisters = []asm.Variable{
	arm64asm.X0, arm64asm.X1, arm64asm.X2, arm64asm.X3,
	arm64asm.X4, arm64asm.X5, arm64asm.X6, arm64asm.X7,
}

func (c *compiler) compileCall(f ir.CallFragment) error {
	// Maximum 8 arguments for AAPCS64
	if len(f.Args) > len(callArgRegisters) {
		return fmt.Errorf("ir: too many arguments for function call (max %d, got %d)", len(callArgRegisters), len(f.Args))
	}

	// Evaluate target first (before we mess with argument registers)
	targetReg, _, err := c.evalValue(f.Target)
	if err != nil {
		return err
	}

	// If target is in an argument register, save it to a safe place (X9-X15 are caller-saved scratch)
	targetIsSafe := true
	for _, argReg := range callArgRegisters[:len(f.Args)] {
		if targetReg == argReg {
			targetIsSafe = false
			break
		}
	}
	if !targetIsSafe {
		// Move target to X9 which is caller-saved but not used for arguments
		c.emit(arm64asm.MovReg(arm64asm.Reg64(arm64asm.X9), arm64asm.Reg64(targetReg)))
		c.freeReg(targetReg)
		targetReg = arm64asm.X9
		c.reserveReg(arm64asm.X9)
	}

	// Evaluate all arguments into temporary registers
	argRegs := make([]asm.Variable, len(f.Args))
	for i, arg := range f.Args {
		reg, _, err := c.evalValue(arg)
		if err != nil {
			// Free any already-allocated arg registers
			for j := 0; j < i; j++ {
				c.freeReg(argRegs[j])
			}
			c.freeReg(targetReg)
			return err
		}
		argRegs[i] = reg
	}

	// Move arguments into calling convention registers
	// Handle the parallel assignment problem: if argRegs[j] == callArgRegisters[i] for j > i,
	// moving argRegs[i] to callArgRegisters[i] would clobber the value needed for argRegs[j].
	// Solution: first save any conflicting values to scratch registers.

	// Scratch registers X9-X15 are caller-saved and not used for arguments
	scratchRegs := []asm.Variable{arm64asm.X10, arm64asm.X11, arm64asm.X12, arm64asm.X13, arm64asm.X14, arm64asm.X15}
	scratchIdx := 0

	// Map from original register to saved scratch register
	savedRegs := make(map[asm.Variable]asm.Variable)

	// First pass: identify and save any argument values that would be clobbered
	for i := range argRegs {
		destReg := callArgRegisters[i]
		// Check if any later argument is in this destination register
		for j := i + 1; j < len(argRegs); j++ {
			if argRegs[j] == destReg {
				// argRegs[j] will be clobbered by moving to destReg
				// Save it to a scratch register if not already saved
				if _, saved := savedRegs[argRegs[j]]; !saved {
					if scratchIdx >= len(scratchRegs) {
						// This shouldn't happen with 8 args and 6 scratch regs
						// Fall back to X9 (target should already be moved if needed)
						savedRegs[argRegs[j]] = arm64asm.X9
					} else {
						savedRegs[argRegs[j]] = scratchRegs[scratchIdx]
						scratchIdx++
					}
					c.emit(arm64asm.MovReg(arm64asm.Reg64(savedRegs[argRegs[j]]), arm64asm.Reg64(argRegs[j])))
				}
			}
		}
	}

	// Second pass: move arguments to their destinations
	for i, reg := range argRegs {
		destReg := callArgRegisters[i]
		// Use saved register if the original was clobbered
		srcReg := reg
		if saved, ok := savedRegs[reg]; ok {
			srcReg = saved
		}
		if srcReg != destReg {
			c.emit(arm64asm.MovReg(arm64asm.Reg64(destReg), arm64asm.Reg64(srcReg)))
		}
		c.freeReg(reg)
	}

	// Call the target
	c.emit(arm64asm.CallReg(arm64asm.Reg64(targetReg)))
	c.freeReg(targetReg)

	// Store result if needed
	if f.Result != "" {
		mem, err := c.stackSlotMem(string(f.Result))
		if err != nil {
			return err
		}
		c.emit(arm64asm.MovToMemory(mem, arm64asm.Reg64(arm64asm.X0)))
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
	if reg != arm64asm.X0 {
		c.emit(arm64asm.MovReg(arm64asm.Reg64(arm64asm.X0), arm64asm.Reg64(reg)))
		c.freeReg(reg)
	}
	// Jump to epilogue label which handles SP restoration, X30 restoration, and ret.
	// This ensures proper stack cleanup and link register handling for all return paths.
	c.emit(arm64asm.Jump(c.epilogueLabel))
	c.freeReg(arm64asm.X0)
	return nil
}

// compileCacheFlush emits the ARM64 cache maintenance sequence for self-modifying code.
// This performs:
// 1. DC CVAU (clean data cache) for each cache line in the region
// 2. DSB ISH (data sync barrier)
// 3. IC IVAU (invalidate instruction cache) for each cache line in the region
// 4. DSB ISH
// 5. ISB (instruction sync barrier)
func (c *compiler) compileCacheFlush(frag ir.CacheFlushFragment) error {
	const cacheLineSize int32 = 64 // Apple Silicon cache line size

	// Evaluate base address
	baseReg, _, err := c.evalValue(frag.Base)
	if err != nil {
		return err
	}

	// Evaluate size
	sizeReg, _, err := c.evalValue(frag.Size)
	if err != nil {
		c.freeReg(baseReg)
		return err
	}

	// Calculate end address: end = base + size
	endReg, err := c.allocReg()
	if err != nil {
		c.freeReg(baseReg)
		c.freeReg(sizeReg)
		return err
	}
	c.emit(arm64asm.MovReg(arm64asm.Reg64(endReg), arm64asm.Reg64(baseReg)))
	c.emit(arm64asm.AddRegReg(arm64asm.Reg64(endReg), arm64asm.Reg64(sizeReg)))
	c.freeReg(sizeReg)

	// Current address register (starts at base)
	curReg, err := c.allocReg()
	if err != nil {
		c.freeReg(baseReg)
		c.freeReg(endReg)
		return err
	}
	c.emit(arm64asm.MovReg(arm64asm.Reg64(curReg), arm64asm.Reg64(baseReg)))

	// Loop 1: Clean data cache lines (DC CVAU)
	dcLoopLabel := c.newInternalLabel("dc_loop")
	dcDoneLabel := c.newInternalLabel("dc_done")

	c.emit(asm.MarkLabel(dcLoopLabel))
	// Compare cur >= end
	c.emit(arm64asm.CmpRegReg(arm64asm.Reg64(curReg), arm64asm.Reg64(endReg)))
	c.emit(arm64asm.JumpIfGreaterOrEqual(dcDoneLabel))
	// DC CVAU, cur
	c.emit(arm64asm.DCCVAU(curReg))
	// cur += cacheLineSize
	c.emit(arm64asm.AddRegImm(arm64asm.Reg64(curReg), cacheLineSize))
	c.emit(arm64asm.Jump(dcLoopLabel))
	c.emit(asm.MarkLabel(dcDoneLabel))

	// DSB ISH - ensure all DC CVAU operations complete
	c.emit(arm64asm.DSBISH())

	// Reset cur to base for IC loop
	c.emit(arm64asm.MovReg(arm64asm.Reg64(curReg), arm64asm.Reg64(baseReg)))

	// Loop 2: Invalidate instruction cache lines (IC IVAU)
	icLoopLabel := c.newInternalLabel("ic_loop")
	icDoneLabel := c.newInternalLabel("ic_done")

	c.emit(asm.MarkLabel(icLoopLabel))
	// Compare cur >= end
	c.emit(arm64asm.CmpRegReg(arm64asm.Reg64(curReg), arm64asm.Reg64(endReg)))
	c.emit(arm64asm.JumpIfGreaterOrEqual(icDoneLabel))
	// IC IVAU, cur
	c.emit(arm64asm.ICIVAU(curReg))
	// cur += cacheLineSize
	c.emit(arm64asm.AddRegImm(arm64asm.Reg64(curReg), cacheLineSize))
	c.emit(arm64asm.Jump(icLoopLabel))
	c.emit(asm.MarkLabel(icDoneLabel))

	// DSB ISH - ensure all IC IVAU operations complete
	c.emit(arm64asm.DSBISH())

	// ISB - flush the pipeline
	c.emit(arm64asm.ISB())

	c.freeReg(baseReg)
	c.freeReg(endReg)
	c.freeReg(curReg)

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
		c.emit(arm64asm.MovToMemory(mem, arm64asm.Reg64(reg)))
		return nil
	case ir.I32Var:
		mem, err := c.stackSlotMem(string(dest.Name))
		if err != nil {
			return err
		}
		c.emit(arm64asm.MovToMemory(mem, arm64asm.Reg32(reg)))
		return nil
	case ir.StackSlotMemFragment:
		offset, err := c.stackSlotOffset(dest.SlotID, dest.Disp)
		if err != nil {
			return err
		}
		mem := arm64asm.Mem(arm64asm.Reg64(arm64asm.SP)).WithDisp(offset)
		// Use the destination's width if specified, otherwise fall back to source width
		dstWidth := dest.Width
		if dstWidth == 0 {
			dstWidth = width
		}
		switch dstWidth {
		case ir.Width8:
			c.emit(arm64asm.MovToMemory(mem, arm64asm.Reg8(reg)))
		case ir.Width16:
			c.emit(arm64asm.MovToMemory(mem, arm64asm.Reg16(reg)))
		case ir.Width32:
			c.emit(arm64asm.MovToMemory(mem, arm64asm.Reg32(reg)))
		default:
			c.emit(arm64asm.MovToMemory(mem, arm64asm.Reg64(reg)))
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
		mem := arm64asm.Mem(arm64asm.Reg64(baseReg)).WithDisp(disp)
		// Use the destination's width if specified, otherwise fall back to source width
		dstWidth := dest.Width
		if dstWidth == 0 {
			dstWidth = width
		}
		switch dstWidth {
		case ir.Width8:
			c.emit(arm64asm.MovToMemory(mem, arm64asm.Reg8(reg)))
		case ir.Width16:
			c.emit(arm64asm.MovToMemory(mem, arm64asm.Reg16(reg)))
		case ir.Width32:
			c.emit(arm64asm.MovToMemory(mem, arm64asm.Reg32(reg)))
		default:
			c.emit(arm64asm.MovToMemory(mem, arm64asm.Reg64(reg)))
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
		mem := arm64asm.Mem(arm64asm.Reg64(baseReg)).WithDisp(disp)
		// Use the destination's width if specified, otherwise fall back to source width
		dstWidth := dest.Width
		if dstWidth == 0 {
			dstWidth = width
		}
		switch dstWidth {
		case ir.Width8:
			c.emit(arm64asm.MovToMemory(mem, arm64asm.Reg8(reg)))
		case ir.Width16:
			c.emit(arm64asm.MovToMemory(mem, arm64asm.Reg16(reg)))
		case ir.Width32:
			c.emit(arm64asm.MovToMemory(mem, arm64asm.Reg32(reg)))
		default:
			c.emit(arm64asm.MovToMemory(mem, arm64asm.Reg64(reg)))
		}
		c.freeReg(baseReg)
		return nil
	case asm.Variable:
		c.emit(arm64asm.MovReg(arm64asm.Reg64(dest), srcReg))
		return nil
	case arm64asm.Reg:
		c.emit(arm64asm.MovReg(dest, srcReg))
		return nil
	default:
		return fmt.Errorf("ir: cannot assign to %T", dst)
	}
}

func regForWidth(reg asm.Variable, width ir.ValueWidth) arm64asm.Reg {
	switch width {
	case ir.Width8:
		return arm64asm.Reg8(reg)
	case ir.Width16:
		return arm64asm.Reg16(reg)
	case ir.Width32:
		return arm64asm.Reg32(reg)
	default:
		return arm64asm.Reg64(reg)
	}
}

func (c *compiler) emitConditionJump(cond ir.Condition, trueLabel, falseLabel asm.Label) error {
	switch cv := cond.(type) {
	case ir.IsNegativeCondition:
		reg, _, err := c.evalValue(cv.Value)
		if err != nil {
			return err
		}
		c.emit(arm64asm.TestZero(arm64asm.Reg64(reg)))
		c.emit(arm64asm.JumpIfNegative(trueLabel))
		c.freeReg(reg)
		c.emit(arm64asm.Jump(falseLabel))
		return nil
	case ir.IsZeroCondition:
		reg, _, err := c.evalValue(cv.Value)
		if err != nil {
			return err
		}
		c.emit(arm64asm.TestZero(arm64asm.Reg64(reg)))
		c.emit(arm64asm.JumpIfZero(trueLabel))
		c.freeReg(reg)
		c.emit(arm64asm.Jump(falseLabel))
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
		c.emit(arm64asm.CmpRegReg(arm64asm.Reg64(leftReg), arm64asm.Reg64(rightReg)))
		c.freeReg(rightReg)
		c.freeReg(leftReg)
		switch cv.Kind {
		case ir.CompareEqual:
			c.emit(arm64asm.JumpIfEqual(trueLabel))
		case ir.CompareNotEqual:
			c.emit(arm64asm.JumpIfNotEqual(trueLabel))
		case ir.CompareLess:
			c.emit(arm64asm.JumpIfLess(trueLabel))
		case ir.CompareLessOrEqual:
			c.emit(arm64asm.JumpIfLessOrEqual(trueLabel))
		case ir.CompareGreater:
			c.emit(arm64asm.JumpIfGreater(trueLabel))
		case ir.CompareGreaterOrEqual:
			c.emit(arm64asm.JumpIfGreaterOrEqual(trueLabel))
		default:
			return fmt.Errorf("ir: unsupported comparison kind %d", cv.Kind)
		}
		c.emit(arm64asm.Jump(falseLabel))
		return nil
	default:
		return fmt.Errorf("ir: unsupported condition %T", cond)
	}
}

func (c *compiler) compileSyscall(sc ir.SyscallFragment, needResult bool) (asm.Variable, error) {
	wasRAXUsed := c.usedRegs[arm64asm.X0]
	c.reserveReg(arm64asm.X0)

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
			c.emit(arm64asm.LoadAddress(arm64asm.Reg64(reg), a.Target))
			args[idx] = asm.Variable(reg)
			regs = append(regs, reg)
		case ir.StackSlotPtrFragment:
			offset, err := c.stackSlotOffset(a.SlotID, a.Disp)
			if err != nil {
				for _, r := range regs {
					c.freeReg(r)
				}
				return 0, err
			}
			preferred := syscallPreferredRegisters(idx, argRegs)
			reg, err := c.allocRegPrefer(preferred...)
			if err != nil {
				for _, r := range regs {
					c.freeReg(r)
				}
				return 0, err
			}
			c.emit(arm64asm.MovRegFromSP(arm64asm.Reg64(reg)))
			if offset != 0 {
				c.emit(arm64asm.AddRegImm(arm64asm.Reg64(reg), offset))
			}
			args[idx] = asm.Variable(reg)
			regs = append(regs, reg)
		case ir.OpFragment:
			// Evaluate the expression
			reg, _, err := c.evalValue(a)
			if err != nil {
				for _, r := range regs {
					c.freeReg(r)
				}
				return 0, err
			}
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

	c.emit(arm64asm.Syscall(sc.Num, args...))

	for _, reg := range regs {
		c.freeReg(reg)
	}

	if needResult {
		return arm64asm.X0, nil
	}

	if !wasRAXUsed {
		c.freeReg(arm64asm.X0)
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
			val = arm64asm.Reg32(reg)
		default:
			val = arm64asm.Reg64(reg)
		}

		values = append(values, val)
		regs = append(regs, reg)
	}

	c.emit(arm64asm.Printf(p.Format, values...))

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
		c.emit(arm64asm.MovFromMemory(arm64asm.Reg32(reg), mem))
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
		c.emit(arm64asm.MovZX8(arm64asm.Reg64(reg), mem))
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
		mem := arm64asm.Mem(arm64asm.Reg64(base)).WithDisp(disp)
		width := v.Width
		if width == 0 {
			width = ir.Width64
		}
		switch width {
		case ir.Width8:
			c.emit(arm64asm.MovZX8(arm64asm.Reg64(base), mem))
		case ir.Width16:
			c.emit(arm64asm.MovZX16(arm64asm.Reg64(base), mem))
		case ir.Width32:
			c.emit(arm64asm.MovFromMemory(arm64asm.Reg32(base), mem))
		default:
			c.emit(arm64asm.MovFromMemory(arm64asm.Reg64(base), mem))
		}
		return base, width, nil
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
		mem := arm64asm.Mem(arm64asm.Reg64(base)).WithDisp(disp)
		width := v.Width
		if width == 0 {
			width = ir.Width64
		}
		switch width {
		case ir.Width8:
			c.emit(arm64asm.MovZX8(arm64asm.Reg64(base), mem))
		case ir.Width16:
			c.emit(arm64asm.MovZX16(arm64asm.Reg64(base), mem))
		case ir.Width32:
			c.emit(arm64asm.MovFromMemory(arm64asm.Reg32(base), mem))
		default:
			c.emit(arm64asm.MovFromMemory(arm64asm.Reg64(base), mem))
		}
		return base, width, nil
	case ir.StackSlotMemFragment:
		offset, err := c.stackSlotOffset(v.SlotID, v.Disp)
		if err != nil {
			return 0, 0, err
		}
		reg, err := c.allocReg()
		if err != nil {
			return 0, 0, err
		}
		mem := arm64asm.Mem(arm64asm.Reg64(arm64asm.SP)).WithDisp(offset)
		width := v.Width
		if width == 0 {
			width = ir.Width64
		}
		switch width {
		case ir.Width8:
			c.emit(arm64asm.MovZX8(arm64asm.Reg64(reg), mem))
		case ir.Width16:
			c.emit(arm64asm.MovZX16(arm64asm.Reg64(reg), mem))
		case ir.Width32:
			c.emit(arm64asm.MovFromMemory(arm64asm.Reg32(reg), mem))
		default:
			c.emit(arm64asm.MovFromMemory(arm64asm.Reg64(reg), mem))
		}
		return reg, width, nil
	case ir.StackSlotPtrFragment:
		offset, err := c.stackSlotOffset(v.SlotID, v.Disp)
		if err != nil {
			return 0, 0, err
		}
		reg, err := c.allocRegPrefer(arm64asm.X0)
		if err != nil {
			return 0, 0, err
		}
		c.emit(arm64asm.MovRegFromSP(arm64asm.Reg64(reg)))
		if offset != 0 {
			c.emit(arm64asm.AddRegImm(arm64asm.Reg64(reg), offset))
		}
		return reg, ir.Width64, nil
	case ir.Int64:
		reg, err := c.allocRegPrefer(arm64asm.X0)
		if err != nil {
			return 0, 0, err
		}
		c.emit(arm64asm.MovImmediate(arm64asm.Reg64(reg), int64(v)))
		return reg, ir.Width64, nil
	case ir.Int32:
		reg, err := c.allocRegPrefer(arm64asm.X0)
		if err != nil {
			return 0, 0, err
		}
		c.emit(arm64asm.MovImmediate(arm64asm.Reg32(reg), int64(v)))
		return reg, ir.Width32, nil
	case ir.Int16:
		reg, err := c.allocRegPrefer(arm64asm.X0)
		if err != nil {
			return 0, 0, err
		}
		c.emit(arm64asm.MovImmediate(arm64asm.Reg16(reg), int64(v)))
		return reg, ir.Width16, nil
	case ir.Int8:
		reg, err := c.allocRegPrefer(arm64asm.X0)
		if err != nil {
			return 0, 0, err
		}
		c.emit(arm64asm.MovImmediate(arm64asm.Reg8(reg), int64(v)))
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
		reg, err := c.allocRegPrefer(arm64asm.X0)
		if err != nil {
			return 0, 0, err
		}
		c.emit(arm64asm.LoadAddress(arm64asm.Reg64(reg), v.Target))
		return reg, ir.Width64, nil
	case ir.MethodPointerFragment:
		reg, err := c.allocRegPrefer(arm64asm.X0)
		if err != nil {
			return 0, 0, err
		}
		// Use LoadImmediate64FromLiteral to store the placeholder in the literal pool
		// as raw bytes, allowing BuildStandaloneProgram to scan and replace it.
		c.emit(arm64asm.LoadImmediate64FromLiteral(arm64asm.Reg64(reg), methodPointerPlaceholder(v.Name)))
		return reg, ir.Width64, nil
	case ir.GlobalPointerFragment:
		reg, err := c.allocRegPrefer(arm64asm.X0)
		if err != nil {
			return 0, 0, err
		}
		// Use LoadImmediate64FromLiteral to store the placeholder in the literal pool
		// as raw bytes, allowing BuildStandaloneProgram to scan and replace it.
		c.emit(arm64asm.LoadImmediate64FromLiteral(arm64asm.Reg64(reg), globalPointerPlaceholder(v.Name)))
		return reg, ir.Width64, nil
	default:
		if imm, ok := toInt64(v); ok {
			reg, err := c.allocRegPrefer(arm64asm.X0)
			if err != nil {
				return 0, 0, err
			}
			c.emit(arm64asm.MovImmediate(arm64asm.Reg64(reg), imm))
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
		c.emit(arm64asm.AddRegReg(arm64asm.Reg64(leftReg), arm64asm.Reg64(rightReg)))
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
		c.emit(arm64asm.SubRegReg(arm64asm.Reg64(leftReg), arm64asm.Reg64(rightReg)))
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
		c.emit(arm64asm.ShrRegImm(arm64asm.Reg64(leftReg), uint32(shift)))
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
		c.emit(arm64asm.ShlRegImm(arm64asm.Reg64(leftReg), uint32(shift)))
		return leftReg, ir.Width64, nil
	case ir.OpAnd:
		leftReg, _, err := c.evalValue(op.Left)
		if err != nil {
			return 0, 0, err
		}
		if imm, ok := toInt64(op.Right); ok {
			tmp, err := c.allocRegPrefer(arm64asm.X0)
			if err != nil {
				c.freeReg(leftReg)
				return 0, 0, err
			}
			c.emit(arm64asm.MovImmediate(arm64asm.Reg64(tmp), imm))
			c.emit(arm64asm.AndRegReg(arm64asm.Reg64(leftReg), arm64asm.Reg64(tmp)))
			c.freeReg(tmp)
			return leftReg, ir.Width64, nil
		}
		rightReg, _, err := c.evalValue(op.Right)
		if err != nil {
			c.freeReg(leftReg)
			return 0, 0, err
		}
		c.emit(arm64asm.AndRegReg(arm64asm.Reg64(leftReg), arm64asm.Reg64(rightReg)))
		c.freeReg(rightReg)
		return leftReg, ir.Width64, nil
	case ir.OpOr:
		leftReg, _, err := c.evalValue(op.Left)
		if err != nil {
			return 0, 0, err
		}
		if imm, ok := toInt64(op.Right); ok {
			tmp, err := c.allocRegPrefer(arm64asm.X0)
			if err != nil {
				c.freeReg(leftReg)
				return 0, 0, err
			}
			c.emit(arm64asm.MovImmediate(arm64asm.Reg64(tmp), imm))
			c.emit(arm64asm.OrRegReg(arm64asm.Reg64(leftReg), arm64asm.Reg64(tmp)))
			c.freeReg(tmp)
			return leftReg, ir.Width64, nil
		}
		rightReg, _, err := c.evalValue(op.Right)
		if err != nil {
			c.freeReg(leftReg)
			return 0, 0, err
		}
		c.emit(arm64asm.OrRegReg(arm64asm.Reg64(leftReg), arm64asm.Reg64(rightReg)))
		c.freeReg(rightReg)
		return leftReg, ir.Width64, nil
	case ir.OpXor:
		leftReg, _, err := c.evalValue(op.Left)
		if err != nil {
			return 0, 0, err
		}
		if imm, ok := toInt64(op.Right); ok {
			tmp, err := c.allocRegPrefer(arm64asm.X0)
			if err != nil {
				c.freeReg(leftReg)
				return 0, 0, err
			}
			c.emit(arm64asm.MovImmediate(arm64asm.Reg64(tmp), imm))
			c.emit(arm64asm.XorRegReg(arm64asm.Reg64(leftReg), arm64asm.Reg64(tmp)))
			c.freeReg(tmp)
			return leftReg, ir.Width64, nil
		}
		rightReg, _, err := c.evalValue(op.Right)
		if err != nil {
			c.freeReg(leftReg)
			return 0, 0, err
		}
		c.emit(arm64asm.XorRegReg(arm64asm.Reg64(leftReg), arm64asm.Reg64(rightReg)))
		c.freeReg(rightReg)
		return leftReg, ir.Width64, nil
	default:
		return 0, 0, fmt.Errorf("ir: unsupported op kind %d", op.Kind)
	}
}

func (c *compiler) stackSlotMem(name string) (arm64asm.Memory, error) {
	offset, ok := c.varOffsets[name]
	if !ok {
		return arm64asm.Memory{}, fmt.Errorf("ir: unknown variable %q", name)
	}
	return arm64asm.Mem(arm64asm.Reg64(arm64asm.SP)).WithDisp(offset), nil
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
	c.emit(arm64asm.MovFromMemory(arm64asm.Reg64(reg), mem))
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
	c.emit(arm64asm.MovFromMemory(arm64asm.Reg64(reg), mem))
	return reg, nil
}

func (c *compiler) loadGlobalAddress(name string) (asm.Variable, error) {
	reg, err := c.allocRegPrefer(arm64asm.X0)
	if err != nil {
		return 0, err
	}
	// Use LoadImmediate64FromLiteral to store the placeholder in the literal pool
	// as raw bytes, allowing BuildStandaloneProgram to scan and replace it.
	c.emit(arm64asm.LoadImmediate64FromLiteral(arm64asm.Reg64(reg), globalPointerPlaceholder(name)))
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
	if reg == arm64asm.SP {
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

// methodMakesCalls returns true if the method contains any ir.CallFragment,
// indicating that it makes calls to other methods. Used to determine if X30
// (link register) needs to be saved/restored.
func methodMakesCalls(f ir.Fragment) bool {
	switch v := f.(type) {
	case nil:
		return false
	case ir.Block:
		for _, inner := range v {
			if methodMakesCalls(inner) {
				return true
			}
		}
		return false
	case ir.Method:
		return methodMakesCalls(ir.Block(v))
	case ir.IfFragment:
		if methodMakesCalls(v.Then) {
			return true
		}
		if v.Otherwise != nil && methodMakesCalls(v.Otherwise) {
			return true
		}
		return false
	case ir.LabelFragment:
		return methodMakesCalls(v.Block)
	case ir.StackSlotFragment:
		if v.Body != nil {
			return methodMakesCalls(v.Body)
		}
		return false
	case ir.CallFragment:
		return true
	default:
		return false
	}
}

// methodUsesPrintf returns true if the method contains any ir.PrintfFragment.
// Printf has significant stack usage (240 bytes for register saves + buffer)
// that must be accounted for in the frame size to prevent stack corruption.
func methodUsesPrintf(f ir.Fragment) bool {
	switch v := f.(type) {
	case nil:
		return false
	case ir.Block:
		for _, inner := range v {
			if methodUsesPrintf(inner) {
				return true
			}
		}
		return false
	case ir.Method:
		return methodUsesPrintf(ir.Block(v))
	case ir.IfFragment:
		if methodUsesPrintf(v.Then) {
			return true
		}
		if v.Otherwise != nil && methodUsesPrintf(v.Otherwise) {
			return true
		}
		return false
	case ir.LabelFragment:
		return methodUsesPrintf(v.Block)
	case ir.StackSlotFragment:
		if v.Body != nil {
			return methodUsesPrintf(v.Body)
		}
		return false
	case ir.PrintfFragment:
		return true
	case ir.AssignFragment:
		return methodUsesPrintf(v.Src) || methodUsesPrintf(v.Dst)
	case ir.SyscallFragment:
		for _, arg := range v.Args {
			if methodUsesPrintf(arg) {
				return true
			}
		}
		return false
	default:
		return false
	}
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
	case ir.CallFragment:
		collectVariables(v.Target, vars)
		for _, arg := range v.Args {
			collectVariables(arg, vars)
		}
		if v.Result != "" {
			vars[string(v.Result)] = struct{}{}
		}
	case ir.ConstantBytesFragment:
		// constants do not reference stack variables
	case ir.ConstantPointerFragment:
		// pointer fragments also do not reference stack variables
	default:
	}
}

func collectGlobals(f ir.Fragment, globals map[string]struct{}) {
	switch v := f.(type) {
	case nil:
	case ir.Block:
		for _, inner := range v {
			collectGlobals(inner, globals)
		}
	case ir.Method:
		collectGlobals(ir.Block(v), globals)
	case ir.AssignFragment:
		collectGlobals(v.Dst, globals)
		collectGlobals(v.Src, globals)
	case ir.SyscallFragment:
		for _, arg := range v.Args {
			collectGlobals(arg, globals)
		}
	case ir.IfFragment:
		collectConditionGlobals(v.Cond, globals)
		collectGlobals(v.Then, globals)
		if v.Otherwise != nil {
			collectGlobals(v.Otherwise, globals)
		}
	case ir.LabelFragment:
		collectGlobals(v.Block, globals)
	case ir.ReturnFragment:
		collectGlobals(v.Value, globals)
	case ir.PrintfFragment:
		for _, arg := range v.Args {
			collectGlobals(arg, globals)
		}
	case ir.CallFragment:
		collectGlobals(v.Target, globals)
		for _, arg := range v.Args {
			collectGlobals(arg, globals)
		}
	case ir.OpFragment:
		collectGlobals(v.Left, globals)
		collectGlobals(v.Right, globals)
	case ir.MemVar:
		if v.Disp != nil {
			collectGlobals(v.Disp, globals)
		}
	case ir.GlobalMem:
		if v.Name != "" {
			globals[v.Name] = struct{}{}
		}
		if v.Disp != nil {
			collectGlobals(v.Disp, globals)
		}
	case ir.GlobalPointerFragment:
		if v.Name != "" {
			globals[v.Name] = struct{}{}
		}
	case ir.StackSlotFragment:
		if v.Body != nil {
			collectGlobals(v.Body, globals)
		}
	case ir.StackSlotMemFragment:
		if v.Disp != nil {
			collectGlobals(v.Disp, globals)
		}
	case ir.StackSlotPtrFragment:
		if v.Disp != nil {
			collectGlobals(v.Disp, globals)
		}
	default:
	}
}

func collectConditionGlobals(cond ir.Condition, globals map[string]struct{}) {
	switch cv := cond.(type) {
	case ir.IsNegativeCondition:
		collectGlobals(cv.Value, globals)
	case ir.IsZeroCondition:
		collectGlobals(cv.Value, globals)
	case ir.CompareCondition:
		collectGlobals(cv.Left, globals)
		collectGlobals(cv.Right, globals)
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

	// Validate that any referenced globals are declared. Relying on scanning the
	// emitted machine code for global-token prefixes can false-positive,
	// especially on ARM64.
	usedGlobals := make(map[string]struct{})
	for _, method := range p.Methods {
		collectGlobals(ir.Block(method), usedGlobals)
	}
	if len(usedGlobals) > 0 {
		missing := make([]string, 0)
		for name := range usedGlobals {
			if p.Globals == nil {
				missing = append(missing, name)
				continue
			}
			if _, ok := p.Globals[name]; !ok {
				missing = append(missing, name)
			}
		}
		if len(missing) > 0 {
			sort.Strings(missing)
			return asm.Program{}, fmt.Errorf("ir: referenced globals are not declared in Program.Globals: %s", strings.Join(missing, ", "))
		}
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

	entryProg, err := arm64asm.EmitProgram(entryFrag)
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
		prog, err := arm64asm.EmitProgram(frag)
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
