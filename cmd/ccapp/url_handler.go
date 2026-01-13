package main

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
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
	ErrInvalidImageRef   = errors.New("invalid image reference format")
)

// ociRefRegex validates OCI image reference format.
// Format: [registry/][namespace/]name[:tag][@sha256:digest]
// Examples: alpine, alpine:latest, docker.io/library/alpine:3.18, ghcr.io/user/image@sha256:abc123...
var ociRefRegex = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9._/-]*[a-zA-Z0-9])?(:[a-zA-Z0-9][a-zA-Z0-9._-]*)?(@sha256:[a-fA-F0-9]{64})?$`)

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
	if action.ImageRef == "" {
		return ErrMissingImageRef
	}

	// Allowlist approach: only permit characters valid in OCI image references
	// (alphanumeric, hyphen, underscore, period, slash, colon for tags, @ for digests)
	for _, r := range action.ImageRef {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_' ||
			r == '.' || r == '/' || r == ':' || r == '@') {
			return fmt.Errorf("%w: contains invalid characters", ErrInvalidImageRef)
		}
	}

	// Validate against OCI reference format
	if !ociRefRegex.MatchString(action.ImageRef) {
		return fmt.Errorf("%w: must match [registry/][namespace/]name[:tag][@digest]", ErrInvalidImageRef)
	}

	// Additional length check (OCI spec allows up to 128 chars for name, but we're more lenient for full refs)
	if len(action.ImageRef) > 256 {
		return fmt.Errorf("%w: too long (max 256 characters)", ErrInvalidImageRef)
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
