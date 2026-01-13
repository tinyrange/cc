//go:build !windows && !linux

package main

// RegisterURLScheme is a no-op on platforms other than Windows and Linux.
// On macOS, URL scheme registration is handled via Info.plist in the app bundle.
func RegisterURLScheme() error {
	return nil
}

// UnregisterURLScheme is a no-op on platforms other than Windows and Linux.
func UnregisterURLScheme() error {
	return nil
}

// IsURLSchemeRegistered always returns true on macOS (handled by Info.plist).
func IsURLSchemeRegistered() bool {
	return true
}
