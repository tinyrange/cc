package dockerfile

import (
	"path/filepath"
	"strings"
)

// ValidateDockerfileSize checks that the input doesn't exceed the maximum size.
func ValidateDockerfileSize(data []byte) error {
	if len(data) > MaxDockerfileSize {
		return ErrDockerfileTooLarge
	}
	return nil
}

// ValidatePath ensures a path stays within the context root.
// It prevents path traversal attacks via ".." components.
func ValidatePath(contextRoot, path string) error {
	// Check for null bytes
	if strings.Contains(path, "\x00") {
		return &ParseError{Message: "path contains null byte"}
	}

	// Clean the path
	cleaned := filepath.Clean(path)

	// Reject absolute paths in source (they should be relative to context)
	if filepath.IsAbs(cleaned) {
		// For COPY/ADD, source paths should be relative
		// Strip leading / and continue
		cleaned = strings.TrimPrefix(cleaned, "/")
		cleaned = filepath.Clean(cleaned)
	}

	// Check for path traversal
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return ErrPathTraversal
	}

	// Also check within the path for any .. components
	for part := range strings.SplitSeq(cleaned, string(filepath.Separator)) {
		if part == ".." {
			return ErrPathTraversal
		}
	}

	// Resolve to absolute and verify containment
	if contextRoot != "" {
		abs := filepath.Join(contextRoot, cleaned)
		rel, err := filepath.Rel(contextRoot, abs)
		if err != nil {
			return &ParseError{Message: "cannot resolve path"}
		}
		if strings.HasPrefix(rel, "..") {
			return ErrPathTraversal
		}
	}

	return nil
}

// ValidateDestPath validates a destination path inside the container.
func ValidateDestPath(path string) error {
	// Check for null bytes
	if strings.Contains(path, "\x00") {
		return &ParseError{Message: "destination path contains null byte"}
	}

	// Destination paths should be absolute or will be made absolute
	// relative to WORKDIR, so we don't restrict them as much
	return nil
}

// SanitizeErrorMessage removes potentially sensitive information from error messages.
func SanitizeErrorMessage(msg string) string {
	// For now, just return the message as-is
	// In production, this could strip absolute paths, etc.
	return msg
}
