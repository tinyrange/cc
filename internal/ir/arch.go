package ir

import (
	"fmt"
	"sync"

	"github.com/tinyrange/cc/internal/asm"
)

// Architecture identifies the backend required to lower IR fragments into
// machine code.
type Architecture string

const (
	ArchitectureInvalid Architecture = ""
	ArchitectureX86_64  Architecture = "x86_64"
	ArchitectureARM64   Architecture = "arm64"
)

// Backend exposes the architecture-specific pieces required by the top-level
// IR helpers (for example BuildStandaloneProgramForArch).
type Backend interface {
	BuildStandaloneProgram(p *Program) (asm.Program, error)
}

var (
	backendsMu sync.RWMutex
	backends   = make(map[Architecture]Backend)
)

// RegisterBackend wires an architecture-specific backend into the shared IR
// helpers. It panics when attempting to register the same architecture more
// than once so mistakes are caught during init.
func RegisterBackend(arch Architecture, backend Backend) {
	if arch == ArchitectureInvalid {
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

func lookupBackend(arch Architecture) (Backend, error) {
	backendsMu.RLock()
	defer backendsMu.RUnlock()

	if backend, ok := backends[arch]; ok {
		return backend, nil
	}
	if arch == ArchitectureInvalid {
		return nil, fmt.Errorf("ir: architecture must be specified")
	}
	return nil, fmt.Errorf("ir: no backend registered for %q", arch)
}

// BuildStandaloneProgramForArch lowers the requested Program using the backend
// registered for arch.
func BuildStandaloneProgramForArch(arch Architecture, prog *Program) (asm.Program, error) {
	if prog == nil {
		return asm.Program{}, fmt.Errorf("ir: program must be non-nil")
	}
	backend, err := lookupBackend(arch)
	if err != nil {
		return asm.Program{}, err
	}
	return backend.BuildStandaloneProgram(prog)
}
