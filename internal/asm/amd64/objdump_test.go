package amd64

import (
	"testing"

	"github.com/tinyrange/cc/internal/asm"
	"github.com/tinyrange/cc/internal/asm/testutil"
)

func TestKitchenSinkDisassemblyAMD64(t *testing.T) {
	frag, expect := buildAMD64KitchenSink()

	prog, err := EmitProgram(frag)
	if err != nil {
		t.Fatalf("EmitProgram failed: %v", err)
	}

	lines := testutil.DisassembleWithObjdump(t, prog.Bytes(), testutil.MachineX86_64, "-M", "att")
	testutil.VerifyExpectations(t, lines, expect)
}

type sinkBuilder struct {
	fragments    []asm.Fragment
	expectations []testutil.Expectation
}

func (b *sinkBuilder) append(frag asm.Fragment) {
	if frag == nil {
		return
	}
	b.fragments = append(b.fragments, frag)
}

func (b *sinkBuilder) add(name, mnemonic string, frag asm.Fragment, contains ...string) {
	b.append(frag)
	b.expectations = append(b.expectations, testutil.Expectation{
		Name:     name,
		Mnemonic: mnemonic,
		Contains: contains,
	})
}

func (b *sinkBuilder) fragment() asm.Fragment {
	return asm.Group(b.fragments)
}

func buildAMD64KitchenSink() (asm.Fragment, []testutil.Expectation) {
	const dataVar asm.Variable = 1

	var builder sinkBuilder
	builder.append(LoadConstantBytes(dataVar, []byte("kitchen sink data block")))

	builder.add("mov_imm", "movabs", MovImmediate(Reg64(RAX), 0x1122334455667788), "$0x1122334455667788,%rax")
	builder.add("mov_reg", "mov", MovReg(Reg64(R9), Reg64(R10)), "%r10,%r9")
	builder.add("mov_to_memory", "mov", MovToMemory(Mem(Reg64(RSP)).WithDisp(0x28), Reg64(RAX)), "%rax,0x28(%rsp)")
	builder.add("mov_from_memory", "mov", MovFromMemory(Reg64(RBX), Mem(Reg64(RSP)).WithDisp(0x18)), "0x18(%rsp),%rbx")
	builder.add("call_reg", "call", CallReg(Reg64(R11)), "*%r11")

	builder.add("movzx8", "", MovZX8(Reg64(R12), Mem(Reg64(RDI)).WithDisp(0x10)), "movz", "0x10(%rdi)", "%r12")
	builder.add("movzx16", "", MovZX16(Reg64(R13), Mem(Reg64(RSI)).WithDisp(0x14)), "movz", "0x14(%rsi)", "%r13")
	builder.add("mov_store_imm8", "movb", MovStoreImm8(Mem(Reg64(RDX)).WithDisp(0x5), 0x7f), "$0x7f,0x5(%rdx)")

	builder.add("add_reg_imm", "add", AddRegImm(Reg64(RAX), 0x21), "$0x21,%rax")
	builder.add("add_reg_reg", "add", AddRegReg(Reg64(R14), Reg64(R15)), "%r15,%r14")
	builder.add("sub_reg_reg", "sub", SubRegReg(Reg64(R13), Reg64(R12)), "%r12,%r13")
	builder.add("or_reg_reg", "or", OrRegReg(Reg64(R11), Reg64(R10)), "%r10,%r11")
	builder.add("cmp_reg_imm", "cmp", CmpRegImm(Reg64(R9), 0x44), "$0x44,%r9")
	builder.add("cmp_reg_reg", "cmp", CmpRegReg(Reg64(R8), Reg64(RCX)), "%rcx,%r8")
	builder.add("and_reg_reg", "and", AndRegReg(Reg64(RDX), Reg64(RSI)), "%rsi,%rdx")
	builder.add("and_reg_imm", "and", AndRegImm(Reg64(RDI), 0xff), "$0xff,%rdi")
	builder.add("or_reg_imm", "or", OrRegImm(Reg64(RBP), 0x33), "$0x33,%rbp")
	builder.add("xor_reg_reg", "xor", XorRegReg(Reg64(RBX), Reg64(RCX)), "%rcx,%rbx")
	builder.add("imul_reg_imm", "imul", ImulRegImm(Reg64(RAX), Reg64(RCX), 3), "$0x3,%rcx,%rax")

	builder.add("hlt", "hlt", Hlt())
	builder.add("out_dx_al", "out", OutDXAL(), "%al,(%dx)")
	builder.add("rdmsr", "rdmsr", Rdmsr())
	builder.add("shr_reg_imm", "shr", ShrRegImm(Reg64(RDX), 2), "$0x2,%rdx")
	builder.add("shl_reg_imm", "shl", ShlRegImm(Reg64(RCX), 3), "$0x3,%rcx")
	builder.add("wrmsr", "wrmsr", Wrmsr())

	builder.add("load_address", "movabs", LoadAddress(Reg64(R8), dataVar), "%r8")
	builder.add("lea_relative", "lea", LeaRelative(Reg64(R15), 0x40), "0x40(%rip),%r15")

	builder.add("jump_if_not_equal", "jne", JumpIfNotEqual(asm.Label("label_jne")))
	builder.append(asm.MarkLabel("label_jne"))
	builder.add("jump_if_not_zero", "jne", JumpIfNotZero(asm.Label("label_jnz")))
	builder.append(asm.MarkLabel("label_jnz"))
	builder.add("jump_if_above_or_equal", "jae", JumpIfAboveOrEqual(asm.Label("label_jae")))
	builder.append(asm.MarkLabel("label_jae"))
	builder.add("jump_if_below_or_equal", "jbe", JumpIfBelowOrEqual(asm.Label("label_jbe")))
	builder.append(asm.MarkLabel("label_jbe"))
	builder.add("jump_if_equal", "je", JumpIfEqual(asm.Label("label_je")))
	builder.append(asm.MarkLabel("label_je"))
	builder.add("jump_if_less", "jl", JumpIfLess(asm.Label("label_jl")))
	builder.append(asm.MarkLabel("label_jl"))
	builder.add("jump_if_above", "ja", JumpIfAbove(asm.Label("label_ja")))
	builder.append(asm.MarkLabel("label_ja"))
	builder.add("jump_if_greater", "jg", JumpIfGreater(asm.Label("label_jg")))
	builder.append(asm.MarkLabel("label_jg"))

	builder.add("ret", "ret", Ret())

	return builder.fragment(), builder.expectations
}
