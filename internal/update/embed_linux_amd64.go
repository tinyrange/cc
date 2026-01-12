//go:build linux && amd64

package update

import _ "embed"

//go:embed installers/ccinstaller_linux_amd64
var installerBinary []byte

// GetInstaller returns the embedded installer binary for the current platform.
func GetInstaller() ([]byte, error) {
	return installerBinary, nil
}

// InstallerFilename returns the filename for the installer.
func InstallerFilename() string {
	return "ccinstaller"
}
