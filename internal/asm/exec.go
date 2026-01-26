package asm

// NativeFunc represents a compiled function that can execute natively.
// This interface is implemented by architecture-specific Func types.
type NativeFunc interface {
	// Call executes the compiled assembly with the provided arguments.
	// Arguments are passed according to the calling convention of the target
	// architecture (System V AMD64 ABI for x86-64, AAPCS64 for ARM64).
	Call(args ...any) uintptr

	// Entry returns the entrypoint address of the compiled fragment.
	Entry() uintptr

	// Program returns a deep copy of the Program backing the compiled function.
	Program() Program
}
