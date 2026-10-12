package kernelvm

import (
	"bytes"
	"debug/elf"
	"fmt"
)

// Validate segment ranges before private loader slicing. Kernel instruction
// bytes are never patched; this only checks the supplied handoff input.
func validateBSDKernel(kernel []byte) error {
	image, err := elf.NewFile(bytes.NewReader(kernel))
	if err != nil {
		return fmt.Errorf("BSD kernel ELF: %w", err)
	}
	defer image.Close()
	if image.Class != elf.ELFCLASS64 || image.Data != elf.ELFDATA2LSB || image.Machine != elf.EM_X86_64 || image.Type != elf.ET_EXEC {
		return fmt.Errorf("BSD kernel must be an amd64 little-endian ELF executable")
	}
	loads := 0
	for _, segment := range image.Progs {
		if segment.Type != elf.PT_LOAD {
			continue
		}
		loads++
		if segment.Filesz > segment.Memsz || segment.Off > uint64(len(kernel)) || segment.Filesz > uint64(len(kernel))-segment.Off || segment.Memsz > 8<<30 || segment.Paddr > ^uint64(0)-segment.Memsz {
			return fmt.Errorf("BSD kernel has invalid load segment ranges")
		}
	}
	if loads == 0 {
		return fmt.Errorf("BSD kernel has no load segments")
	}
	return nil
}
