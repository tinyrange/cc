package ir

import (
	"fmt"
	"sync"

	"github.com/tinyrange/cc/internal/asm"
	"github.com/tinyrange/cc/internal/hv"
)

// Backend exposes the architecture-specific pieces required by the top-level
// IR helpers (for example BuildStandaloneProgramForArch).
type Backend interface {
	BuildStandaloneProgram(p *Program) (asm.Program, error)
}

// NativeBackend extends Backend with native execution support.
// Implementations are only available on platforms that support native code execution.
type NativeBackend interface {
	Backend
	// PrepareNativeExecution prepares a compiled program for native execution.
	// Returns a callable function, a cleanup function, and an error.
	// The cleanup function must be called when the function is no longer needed.
	PrepareNativeExecution(prog asm.Program) (asm.NativeFunc, func(), error)
}

var (
	backendsMu sync.RWMutex
	backends   = make(map[hv.CpuArchitecture]Backend)
)

// RegisterBackend wires an architecture-specific backend into the shared IR
// helpers. It panics when attempting to register the same architecture more
// than once so mistakes are caught during init.
func RegisterBackend(arch hv.CpuArchitecture, backend Backend) {
	if arch == hv.ArchitectureInvalid {
		panic("ir: cannot register backend for invalid architecture")
	}
	if backend == nil {
		panic("ir: backend must be non-nil")
	}

	backendsMu.Lock()
	defer backendsMu.Unlock()

	if _, exists := backends[arch]; exists {
		panic(fmt.Sprintf("ir: backend for %s already registered", arch))
	}
	backends[arch] = backend
}

func lookupBackend(arch hv.CpuArchitecture) (Backend, error) {
	backendsMu.RLock()
	defer backendsMu.RUnlock()

	if backend, ok := backends[arch]; ok {
		return backend, nil
	}
	if arch == hv.ArchitectureInvalid {
		return nil, fmt.Errorf("ir: architecture must be specified")
	}
	return nil, fmt.Errorf("ir: no backend registered for %q", arch)
}

// BuildStandaloneProgramForArch lowers the requested Program using the backend
// registered for arch.
func BuildStandaloneProgramForArch(arch hv.CpuArchitecture, prog *Program) (asm.Program, error) {
	if prog == nil {
		return asm.Program{}, fmt.Errorf("ir: program must be non-nil")
	}
	backend, err := lookupBackend(arch)
	if err != nil {
		return asm.Program{}, err
	}
	return backend.BuildStandaloneProgram(prog)
}

// LookupNativeBackend returns the NativeBackend for the specified architecture.
// Returns an error if no backend is registered or if the backend does not
// support native execution.
func LookupNativeBackend(arch hv.CpuArchitecture) (NativeBackend, error) {
	backend, err := lookupBackend(arch)
	if err != nil {
		return nil, err
	}
	native, ok := backend.(NativeBackend)
	if !ok {
		return nil, fmt.Errorf("ir: backend for %q does not support native execution", arch)
	}
	return native, nil
}
