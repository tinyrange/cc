//go:build linux && amd64

package amd64

import gosyscall "syscall"

// MustCompileUnaryInt compiles the fragment into a function that takes and returns Go ints.
func MustCompileUnaryInt(f Fragment) func(int) int {
	fn := MustCompile(f)
	return func(arg int) int {
		return int(int64(fn.Call(arg)))
	}
}

// MustCompileUnaryInt32 compiles the fragment into a function operating on int32 values.
func MustCompileUnaryInt32(f Fragment) func(int32) int32 {
	fn := MustCompile(f)
	return func(arg int32) int32 {
		return int32(int64(fn.Call(arg)))
	}
}

// MustCompileUnaryInt64 compiles the fragment into a function operating on int64 values.
func MustCompileUnaryInt64(f Fragment) func(int64) int64 {
	fn := MustCompile(f)
	return func(arg int64) int64 {
		return int64(fn.Call(arg))
	}
}

// MustCompileErrno0 compiles the fragment into a no-argument function that returns a syscall.Errno.
// The fragment is expected to return zero on success, or a negative errno value on failure.
func MustCompileErrno0(f Fragment) func() gosyscall.Errno {
	fn := MustCompile(f)
	return func() gosyscall.Errno {
		res := int64(fn.Call())
		if res >= 0 {
			return 0
		}
		return gosyscall.Errno(-res)
	}
}
