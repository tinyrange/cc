package dockerfile

import (
	"errors"
	"fmt"
)

// Security limit constants.
const (
	MaxDockerfileSize    = 1 << 20 // 1MB
	MaxLineLength        = 1 << 20 // 1MB
	MaxInstructionCount  = 1000
	MaxVariableCount     = 100
	MaxVariableExpansion = 10 // Maximum recursion depth for variable expansion
)

// Sentinel errors for security violations.
var (
	ErrDockerfileTooLarge     = errors.New("dockerfile exceeds maximum size")
	ErrTooManyInstructions    = errors.New("dockerfile exceeds maximum instruction count")
	ErrTooManyVariables       = errors.New("dockerfile exceeds maximum variable count")
	ErrVariableExpansionLoop  = errors.New("variable expansion loop or depth exceeded")
	ErrPathTraversal          = errors.New("path escapes build context")
	ErrInvalidPath            = errors.New("invalid path")
	ErrMissingFrom            = errors.New("dockerfile must start with FROM instruction")
	ErrUnsupportedInstruction = errors.New("unsupported instruction")
)

// ParseError represents an error during Dockerfile parsing.
type ParseError struct {
	Line    int    // Line number (1-indexed, 0 if not applicable)
	Message string // Error description
	Hint    string // Optional hint for fixing the error
}

func (e *ParseError) Error() string {
	if e.Line > 0 {
		if e.Hint != "" {
			return fmt.Sprintf("line %d: %s (hint: %s)", e.Line, e.Message, e.Hint)
		}
		return fmt.Sprintf("line %d: %s", e.Line, e.Message)
	}
	if e.Hint != "" {
		return fmt.Sprintf("%s (hint: %s)", e.Message, e.Hint)
	}
	return e.Message
}

// UnsupportedError indicates an unsupported Dockerfile feature.
type UnsupportedError struct {
	Feature string // Description of the unsupported feature
	Line    int    // Line number where it was encountered
}

func (e *UnsupportedError) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("line %d: unsupported feature: %s", e.Line, e.Feature)
	}
	return fmt.Sprintf("unsupported feature: %s", e.Feature)
}

func (e *UnsupportedError) Is(target error) bool {
	return target == ErrUnsupportedInstruction
}

// PathTraversalError indicates a path that escapes the build context.
type PathTraversalError struct {
	Path string
	Line int
}

func (e *PathTraversalError) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("line %d: path %q escapes build context", e.Line, e.Path)
	}
	return fmt.Sprintf("path %q escapes build context", e.Path)
}

func (e *PathTraversalError) Is(target error) bool {
	return target == ErrPathTraversal
}

// BuildError represents an error during Dockerfile building (after parsing).
type BuildError struct {
	Op      string // Operation that failed (e.g., "COPY", "RUN")
	Line    int    // Line number
	Message string // Error description
	Err     error  // Underlying error
}

func (e *BuildError) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("%s (line %d): %s", e.Op, e.Line, e.Message)
	}
	return fmt.Sprintf("%s: %s", e.Op, e.Message)
}

func (e *BuildError) Unwrap() error {
	return e.Err
}
