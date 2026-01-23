// Package api provides the internal implementation for the public cc API.
// It wraps the internal infrastructure to provide a user-friendly interface.
package api

import (
	"crypto/rand"
	"errors"
	"fmt"
)

// Common sentinel errors.
var (
	ErrNotRunning    = errors.New("instance not running")
	ErrAlreadyClosed = errors.New("instance already closed")
	ErrTimeout       = errors.New("operation timed out")
)

// generateID returns a new unique identifier for instances.
func generateID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "instance-unknown"
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
