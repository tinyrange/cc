package initx

import "fmt"

// ExitError represents a non-zero exit code returned from an initx payload.
type ExitError struct {
	Code int
}

// Error implements the error interface.
func (e *ExitError) Error() string {
	return fmt.Sprintf("initx program exited with code %d", e.Code)
}
