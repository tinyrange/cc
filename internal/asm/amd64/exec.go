//go:build linux && amd64

package amd64

import (
	"encoding/binary"
	"fmt"
	"reflect"
	"unsafe"

	"github.com/tinyrange/cc/internal/asm"
	"golang.org/x/sys/unix"
)

type Func struct {
	entry uintptr
	call  func(...any) uintptr
	prog  asm.Program
}

// Call executes the compiled assembly with the provided arguments.
func (fn Func) Call(args ...any) uintptr {
	if fn.call == nil {
		panic("asm.Func: call on zero value")
	}
	return fn.call(args...)
}

// Entry returns the entrypoint address of the compiled fragment.
func (fn Func) Entry() uintptr {
	return fn.entry
}

// Program returns a deep copy of the Program backing the compiled function.
func (fn Func) Program() asm.Program {
	return fn.prog.Clone()
}

func Compile(f asm.Fragment) (Func, func(), error) {
	prog, err := EmitProgram(f)
	if err != nil {
		return Func{}, nil, fmt.Errorf("emit assembly program: %w", err)
	}

	fn, release, err := PrepareAssemblyWithArgs(prog.Bytes(), prog.Relocations(), prog.BSSSize())
	if err != nil {
		return Func{}, nil, fmt.Errorf("prepare assembly with args: %w", err)
	}

	fn.prog = prog.Clone()

	return fn, release, nil
}

func MustCompile(f asm.Fragment) Func {
	fn, _, err := Compile(f)
	if err != nil {
		panic(err)
	}
	return fn
}

func PrepareAssembly(code []byte, relocations []int, bssSize ...int) (func(), func(), error) {
	bss := 0
	if len(bssSize) > 0 {
		bss = bssSize[0]
	}
	entry, release, err := createAssemblyTrampoline(code, relocations, bss)
	if err != nil {
		return nil, nil, err
	}

	return func() {
		callAssemblyEntry(entry)
	}, release, nil
}

// PrepareAssemblyWithArgs is like PrepareAssembly, but allows calling the assembled code with up to
// six integer or pointer arguments (passed in the System V calling convention registers).
// It also accepts an optional bssSize parameter to allocate space for BSS (globals).
func PrepareAssemblyWithArgs(code []byte, relocations []int, bssSize ...int) (Func, func(), error) {
	bss := 0
	if len(bssSize) > 0 {
		bss = bssSize[0]
	}
	entry, release, err := createAssemblyTrampoline(code, relocations, bss)
	if err != nil {
		return Func{}, nil, err
	}

	call := func(args ...any) uintptr {
		if len(args) > maxAssemblyArguments {
			panic(fmt.Sprintf("assembly call accepts at most %d arguments, got %d", maxAssemblyArguments, len(args)))
		}

		if len(args) == 0 {
			return callAssemblyEntryWithArgs(entry, nil, 0)
		}

		buf := make([]uintptr, len(args))
		for idx, arg := range args {
			value, err := assemblyArgValue(arg)
			if err != nil {
				panic(err)
			}
			buf[idx] = value
		}

		return callAssemblyEntryWithArgs(entry, &buf[0], uintptr(len(buf)))
	}

	return Func{
		entry: entry,
		call:  call,
	}, release, nil
}

const maxAssemblyArguments = 6

func assemblyArgValue(arg any) (uintptr, error) {
	switch v := arg.(type) {
	case nil:
		return 0, nil
	case uintptr:
		return v, nil
	case unsafe.Pointer:
		return uintptr(v), nil
	case int:
		return uintptr(v), nil
	case int8:
		return uintptr(uint8(v)), nil
	case int16:
		return uintptr(uint16(v)), nil
	case int32:
		return uintptr(uint32(v)), nil
	case int64:
		return uintptr(v), nil
	case uint:
		return uintptr(v), nil
	case uint8:
		return uintptr(v), nil
	case uint16:
		return uintptr(v), nil
	case uint32:
		return uintptr(v), nil
	case uint64:
		return uintptr(v), nil
	}

	val := reflect.ValueOf(arg)
	if !val.IsValid() {
		return 0, fmt.Errorf("unsupported argument <invalid>")
	}

	switch val.Kind() {
	case reflect.Pointer, reflect.UnsafePointer:
		if val.IsNil() {
			return 0, nil
		}
		return uintptr(val.Pointer()), nil
	}

	return 0, fmt.Errorf("unsupported argument type %T", arg)
}

func createAssemblyTrampoline(code []byte, relocations []int, bssSize int) (uintptr, func(), error) {
	size := len(code)
	if size == 0 {
		return 0, nil, fmt.Errorf("empty code")
	}

	pageSize := unix.Getpagesize()

	// Round up code size to page boundary so BSS is always on a separate page.
	// This allows us to mprotect code pages as RX while keeping BSS pages RW.
	codeAllocSize := ((size + pageSize - 1) / pageSize) * pageSize

	// Total allocation: page-aligned code + BSS
	totalSize := codeAllocSize + bssSize
	allocSize := ((totalSize + pageSize - 1) / pageSize) * pageSize

	mem, err := unix.Mmap(-1, 0, allocSize, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_PRIVATE|unix.MAP_ANON)
	if err != nil {
		return 0, nil, fmt.Errorf("mmap assembly region: %w", err)
	}
	release := true
	defer func() {
		if release {
			_ = unix.Munmap(mem)
		}
	}()

	copy(mem, code)

	base := uintptr(unsafe.Pointer(&mem[0]))

	// Relocations need to be adjusted for the page-aligned BSS offset.
	// The compiler calculates BSS at align(len(code), 16), but we put it at codeAllocSize.
	// We need to adjust any relocation values that point into the BSS region.
	bssAdjustment := uint64(codeAllocSize - size)
	codeSize := uint64(size)

	for _, reloc := range relocations {
		offset := int(reloc)
		if offset < 0 || offset+8 > len(mem) {
			return 0, nil, fmt.Errorf("assembly relocation offset %d out of range (code len %d)", offset, len(mem))
		}
		value := binary.LittleEndian.Uint64(mem[offset:])

		// Check if this relocation points into the BSS region (beyond code size).
		// If so, adjust it to account for the page-aligned BSS placement.
		if value >= codeSize {
			value += bssAdjustment
		}

		binary.LittleEndian.PutUint64(mem[offset:], value+uint64(base))
	}

	// Make code region executable. BSS region (if any) remains writable.
	if err := unix.Mprotect(mem[:codeAllocSize], unix.PROT_READ|unix.PROT_EXEC); err != nil {
		return 0, nil, fmt.Errorf("mprotect code region: %w", err)
	}

	release = false

	return base, func() {
		_ = unix.Munmap(mem)
	}, nil
}

// callAssemblyEntry jumps to the provided code pointer and never returns on success.
func callAssemblyEntry(entry uintptr)

func callAssemblyEntryWithArgs(entry uintptr, args *uintptr, nargs uintptr) uintptr
