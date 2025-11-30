//go:build windows

package bindings

import (
	"errors"
)

// AsHRESULT attempts to extract an HRESULT from the provided error.
func AsHRESULT(err error) (HRESULT, bool) {
	var hErr HRESULTError
	if errors.As(err, &hErr) {
		return HRESULT(hErr), true
	}
	return 0, false
}
