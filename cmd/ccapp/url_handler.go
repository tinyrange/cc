package main

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// URLAction represents a parsed crumblecracker:// URL action.
type URLAction struct {
	Action   string // "run"
	ImageRef string // OCI image reference (e.g., "alpine:latest", "ghcr.io/user/image:tag")
}

// Errors for URL parsing.
var (
	ErrInvalidURLScheme  = errors.New("invalid URL scheme: expected crumblecracker://")
	ErrInvalidURLAction  = errors.New("invalid URL action")
	ErrMissingImageRef   = errors.New("missing image reference")
	ErrUnsupportedAction = errors.New("unsupported action")
)

// ParseCrumbleCrackerURL parses a crumblecracker:// URL into a URLAction.
//
// Format: crumblecracker://run/<image-ref>
//
// Examples:
//   - crumblecracker://run/alpine:latest
//   - crumblecracker://run/nginx:1.25
//   - crumblecracker://run/ghcr.io/user/image:tag
func ParseCrumbleCrackerURL(rawURL string) (*URLAction, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse URL: %w", err)
	}

	// Validate scheme.
	if u.Scheme != "crumblecracker" {
		return nil, ErrInvalidURLScheme
	}

	// The action is in the host part.
	// For crumblecracker://run/alpine, host is "run".
	action := u.Host
	if action == "" {
		return nil, ErrInvalidURLAction
	}

	// The image reference is in the path.
	// For crumblecracker://run/alpine:latest, path is "/alpine:latest".
	// For crumblecracker://run/ghcr.io/user/image:tag, path is "/ghcr.io/user/image:tag".
	imageRef := strings.TrimPrefix(u.Path, "/")
	if imageRef == "" {
		return nil, ErrMissingImageRef
	}

	return &URLAction{
		Action:   action,
		ImageRef: imageRef,
	}, nil
}

// ValidateURLAction validates the parsed URL action.
func ValidateURLAction(action *URLAction) error {
	if action == nil {
		return ErrInvalidURLAction
	}

	// Currently only "run" action is supported.
	switch action.Action {
	case "run":
		// Valid action.
	default:
		return fmt.Errorf("%w: %s", ErrUnsupportedAction, action.Action)
	}

	// Validate image reference format.
	// Basic validation: must not be empty and must not contain spaces.
	if action.ImageRef == "" {
		return ErrMissingImageRef
	}
	if strings.ContainsAny(action.ImageRef, " \t\n\r") {
		return fmt.Errorf("invalid image reference: contains whitespace")
	}

	return nil
}

// SanitizeImageNameForDisplay returns a display-safe version of the image name.
// This truncates very long names and escapes any problematic characters.
func SanitizeImageNameForDisplay(imageRef string) string {
	const maxLen = 80
	if len(imageRef) > maxLen {
		return imageRef[:maxLen-3] + "..."
	}
	return imageRef
}
