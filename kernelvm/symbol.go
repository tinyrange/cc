package kernelvm

import (
	"debug/elf"
	"fmt"
	"io"
)

type Symbol struct {
	Name                  string
	Address, Size, Offset uint64
}

// SymbolAt observes the nearest original executable-section symbol preceding
// address. Offset explicitly distinguishes an exact symbol from a nearby one.
// It does not relocate, execute, or patch kernel bytes.
func SymbolAt(source io.ReaderAt, size int64, address uint64) (*Symbol, error) {
	if source == nil || size <= 0 || size > 256<<20 {
		return nil, fmt.Errorf("kernel symbol input must be 1..256 MiB")
	}
	image, err := elf.NewFile(io.NewSectionReader(source, 0, size))
	if err != nil {
		return nil, err
	}
	defer image.Close()
	for _, section := range image.Sections {
		if section.Type == elf.SHT_SYMTAB || section.Type == elf.SHT_DYNSYM || section.Type == elf.SHT_STRTAB {
			if section.Size > 32<<20 {
				return nil, fmt.Errorf("kernel symbol table exceeds 32 MiB")
			}
		}
	}
	symbols, err := image.Symbols()
	if err == elf.ErrNoSymbols {
		symbols, err = image.DynamicSymbols()
	}
	if err == elf.ErrNoSymbols {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var result *Symbol
	for _, symbol := range symbols {
		index := int(symbol.Section)
		if index <= 0 || index >= len(image.Sections) || image.Sections[index].Flags&elf.SHF_EXECINSTR == 0 || symbol.Name == "" || symbol.Value > address {
			continue
		}
		if result == nil || symbol.Value > result.Address {
			result = &Symbol{Name: symbol.Name, Address: symbol.Value, Size: symbol.Size, Offset: address - symbol.Value}
		}
	}
	return result, nil
}
