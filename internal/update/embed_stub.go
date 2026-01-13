//go:build !embed_installer

package update

import "errors"

// GetInstaller returns an error when the installer is not embedded.
// Build with -tags embed_installer to embed the installer binary.
func GetInstaller() ([]byte, error) {
	return nil, errors.New("installer not embedded: build with -tags embed_installer")
}

// InstallerFilename returns the filename for the installer.
func InstallerFilename() string {
	return "ccinstaller"
}
