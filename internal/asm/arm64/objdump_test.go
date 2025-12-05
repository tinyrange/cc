package arm64

import (
	"fmt"
	"testing"

	"github.com/tinyrange/cc/internal/asm"
	"github.com/tinyrange/cc/internal/asm/testutil"
	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/linux/defs"
	"github.com/tinyrange/cc/internal/linux/syscallnum"
)

func TestKitchenSinkDisassemblyARM64(t *testing.T) {
	frag, expect := buildARM64KitchenSink()

	prog, err := EmitProgram(frag)
	if err != nil {
		t.Fatalf("EmitProgram failed: %v", err)
	}

	lines := testutil.DisassembleWithTool(t, "llvm-objdump", prog.Bytes(), testutil.MachineAArch64,
		"-d", "--no-show-raw-insn")
	testutil.VerifyExpectations(t, lines, expect)
}

type armSinkBuilder struct {
	fragments    []asm.Fragment
	expectations []testutil.Expectation
}

func (b *armSinkBuilder) append(frag asm.Fragment) {
	if frag == nil {
		return
	}
	b.fragments = append(b.fragments, frag)
}

func (b *armSinkBuilder) add(name, mnemonic string, frag asm.Fragment, contains ...string) {
	b.append(frag)
	b.expectations = append(b.expectations, testutil.Expectation{
		Name:     name,
		Mnemonic: mnemonic,
		Contains: contains,
	})
}

func (b *armSinkBuilder) addMulti(frag asm.Fragment, exps ...testutil.Expectation) {
	b.append(frag)
	b.expectations = append(b.expectations, exps...)
}

func (b *armSinkBuilder) fragment() asm.Fragment {
	return asm.Group(b.fragments)
}

func buildARM64KitchenSink() (asm.Fragment, []testutil.Expectation) {
	const dataVar asm.Variable = 7

	var builder armSinkBuilder
	builder.append(LoadConstantBytes(dataVar, []byte("arm64 kitchen sink const data block")))

	builder.addMulti(MovImmediate(Reg64(X0), 0x1122334455667788),
		testutil.Expectation{Name: "mov_imm_movz", Mnemonic: "mov", Contains: []string{"x0", "#0x7788"}},
		testutil.Expectation{Name: "mov_imm_movk_16", Mnemonic: "movk", Contains: []string{"#0x5566", "lsl #16"}},
		testutil.Expectation{Name: "mov_imm_movk_32", Mnemonic: "movk", Contains: []string{"#0x3344", "lsl #32"}},
		testutil.Expectation{Name: "mov_imm_movk_48", Mnemonic: "movk", Contains: []string{"#0x1122", "lsl #48"}},
	)

	builder.add("mov_reg", "mov", MovReg(Reg64(X2), Reg64(X1)), "x2", "x1")
	builder.add("add_reg_imm", "add", AddRegImm(Reg64(X3), 0x123), "#0x123", "x3")
	builder.add("add_reg_reg", "add", AddRegReg(Reg64(X4), Reg64(X5)), "x4", "x5")
	builder.add("sub_reg_reg", "sub", SubRegReg(Reg64(X6), Reg64(X7)), "x6", "x7")
	builder.add("cmp_reg_reg", "cmp", CmpRegReg(Reg64(X8), Reg64(X9)), "x8", "x9")
	builder.add("cmp_reg_imm", "cmp", CmpRegImm(Reg64(X10), 0x3f), "x10", "#0x3f")
	builder.add("test_zero", "tst", TestZero(Reg64(X11)), "x11")
	builder.add("shift_left", "lsl", ShlRegImm(Reg64(X12), 4), "x12", "#4")
	builder.add("shift_right", "lsr", ShrRegImm(Reg64(X13), 5), "x13", "#5")
	builder.add("and_reg_reg", "and", AndRegReg(Reg64(X14), Reg64(X15)), "x14", "x15")

	builder.add("mov_to_mem_64", "str", MovToMemory64(Mem(Reg64(X16)).WithDisp(0x40), Reg64(X0)), "x0", "[x16", "#0x40")
	builder.add("mov_to_mem_32", "str", MovToMemory32(Mem(Reg64(X17)).WithDisp(0x24), Reg32(X1)), "w1", "[x17", "#0x24")
	builder.add("mov_to_mem_8", "strb", MovToMemory8(Mem(Reg64(X18)).WithDisp(0x5), Reg32(X2)), "w2", "[x18", "#0x5")
	builder.add("mov_to_mem_16", "strh", MovToMemory(Mem(Reg64(X19)).WithDisp(0x12), Reg16(X3)), "w3", "[x19", "#0x12")

	builder.add("mov_from_mem_64", "ldr", MovFromMemory64(Reg64(X4), Mem(Reg64(X20)).WithDisp(0x18)), "x4", "[x20", "#0x18")
	builder.add("mov_from_mem_32", "ldr", MovFromMemory32(Reg32(X5), Mem(Reg64(X21)).WithDisp(0x1c)), "w5", "[x21", "#0x1c")
	builder.add("mov_from_mem_8", "ldrb", MovFromMemory8(Reg32(X6), Mem(Reg64(X22)).WithDisp(0x7)), "w6", "[x22", "#0x7")
	builder.add("mov_from_mem_16", "ldrh", MovFromMemory(Reg16(X7), Mem(Reg64(X23)).WithDisp(0x10)), "w7", "[x23", "#0x10")

	builder.add("movzx8", "ldrb", MovZX8(Reg64(X24), Mem(Reg64(X25)).WithDisp(0x6)), "w24", "[x25", "#0x6")
	builder.add("movzx16", "ldrh", MovZX16(Reg64(X26), Mem(Reg64(X27)).WithDisp(0x14)), "w26", "[x27", "#0x14")

	builder.add("load_address", "ldr", LoadAddress(Reg64(X28), dataVar), "x28")

	jumpLabel := asm.Label("arm64_jump_target")
	builder.add("jump", "b", Jump(jumpLabel))
	builder.append(asm.MarkLabel(jumpLabel))

	condLabels := []struct {
		name     string
		mnemonic string
		branch   asm.Fragment
		label    asm.Label
	}{
		{name: "jump_if_equal", mnemonic: "b.eq", branch: JumpIfEqual(asm.Label("arm64_eq")), label: asm.Label("arm64_eq")},
		{name: "jump_if_not_equal", mnemonic: "b.ne", branch: JumpIfNotEqual(asm.Label("arm64_ne")), label: asm.Label("arm64_ne")},
		{name: "jump_if_zero", mnemonic: "b.eq", branch: JumpIfZero(asm.Label("arm64_zero")), label: asm.Label("arm64_zero")},
		{name: "jump_if_not_zero", mnemonic: "b.ne", branch: JumpIfNotZero(asm.Label("arm64_nz")), label: asm.Label("arm64_nz")},
		{name: "jump_if_greater", mnemonic: "b.gt", branch: JumpIfGreater(asm.Label("arm64_gt")), label: asm.Label("arm64_gt")},
		{name: "jump_if_greater_or_equal", mnemonic: "b.ge", branch: JumpIfGreaterOrEqual(asm.Label("arm64_ge")), label: asm.Label("arm64_ge")},
		{name: "jump_if_less", mnemonic: "b.lt", branch: JumpIfLess(asm.Label("arm64_lt")), label: asm.Label("arm64_lt")},
		{name: "jump_if_less_or_equal", mnemonic: "b.le", branch: JumpIfLessOrEqual(asm.Label("arm64_le")), label: asm.Label("arm64_le")},
		{name: "jump_if_negative", mnemonic: "b.mi", branch: JumpIfNegative(asm.Label("arm64_mi")), label: asm.Label("arm64_mi")},
	}
	for _, entry := range condLabels {
		builder.add(entry.name, entry.mnemonic, entry.branch)
		builder.append(asm.MarkLabel(entry.label))
	}

	callTarget := asm.Label("arm64_call_target")
	builder.add("call_label", "bl", Call(callTarget))
	builder.append(asm.MarkLabel(callTarget))

	builder.add("call_reg", "blr", CallReg(Reg64(X29)), "x29")
	builder.add("hvc", "hvc", Hvc(), "#0")

	sysGetSockName := fmt.Sprintf("#0x%x", syscallnum.MustNumber(hv.ArchitectureARM64, defs.SYS_GETSOCKNAME))
	builder.addMulti(Syscall(defs.SYS_GETSOCKNAME, asm.Immediate(5)),
		testutil.Expectation{Name: "syscall_mov_imm", Mnemonic: "mov", Contains: []string{"x8", sysGetSockName}},
		testutil.Expectation{Name: "syscall_arg", Mnemonic: "mov", Contains: []string{"x0", "#0x5"}},
		testutil.Expectation{Name: "syscall_svc", Mnemonic: "svc", Contains: []string{"#0"}},
	)

	builder.add("ret", "ret", Ret())

	return builder.fragment(), builder.expectations
}
