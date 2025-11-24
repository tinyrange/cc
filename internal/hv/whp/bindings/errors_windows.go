//go:build windows

package bindings

import (
	"errors"
	"fmt"
)

func errFmt(kind string, value uint32) string {
	return fmt.Sprintf("whp: %s 0x%08X", kind, value)
}

// AsHRESULT attempts to extract an HRESULT from the provided error.
func AsHRESULT(err error) (HRESULT, bool) {
	var hErr HRESULTError
	if errors.As(err, &hErr) {
		return HRESULT(hErr), true
	}
	return 0, false
}
