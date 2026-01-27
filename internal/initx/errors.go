package initx

import (
	"errors"
	"fmt"
)

// ErrVMTerminated is returned when a vsock operation fails because
// the virtual machine has terminated unexpectedly.
var ErrVMTerminated = errors.New("virtual machine terminated")

// ExitError represents a non-zero exit code returned from an initx payload.
type ExitError struct {
	Code int
}

// Error implements the error interface.
func (e *ExitError) Error() string {
	if e.Code < 0 {
		return fmt.Sprintf("initx program exited with errno 0x%x", -e.Code)
	}
	return fmt.Sprintf("initx program exited with code %d", e.Code)
}
