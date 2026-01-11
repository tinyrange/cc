package window

// Clipboard provides cross-platform clipboard access for text data.
type Clipboard interface {
	// GetText returns the current text content from the system clipboard.
	// Returns an empty string if the clipboard is empty or doesn't contain text.
	GetText() string

	// SetText copies the given text to the system clipboard.
	// Returns an error if the operation fails.
	SetText(text string) error
}

// GetClipboard returns the system clipboard implementation.
// This function is implemented in platform-specific files.
func GetClipboard() Clipboard {
	return getClipboard()
}
