package testutil

import (
	"fmt"
	"testing"
)

// Expectation describes a single instruction that should appear in the
// disassembly output.
type Expectation struct {
	Name     string
	Mnemonic string
	Contains []string
}

func (e Expectation) match(line DisasmLine) error {
	if e.Mnemonic != "" && line.Mnemonic != e.Mnemonic {
		return fmt.Errorf("mnemonic=%s, want %s", line.Mnemonic, e.Mnemonic)
	}
	for _, needle := range e.Contains {
		if !line.Contains(needle) {
			return fmt.Errorf("missing %q in %q", needle, line.Normalized)
		}
	}
	return nil
}

// VerifyExpectations walks the objdump output and ensures each expectation is
// satisfied in order. Extra instructions after all expectations are ignored so
// padding emitted for alignment does not fail the test.
func VerifyExpectations(t *testing.T, lines []DisasmLine, expect []Expectation) {
	t.Helper()
	if len(lines) < len(expect) {
		t.Fatalf("objdump returned %d instructions, want at least %d", len(lines), len(expect))
	}
	for idx, exp := range expect {
		line := lines[idx]
		if err := exp.match(line); err != nil {
			t.Fatalf("instruction %q mismatch at line %d: %v\nline: %s", exp.Name, idx, err, line.Text)
		}
	}
}
