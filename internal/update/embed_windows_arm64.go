//go:build windows && arm64

package update

import _ "embed"

//go:embed installers/ccinstaller_windows_arm64.exe
var installerBinary []byte

// GetInstaller returns the embedded installer binary for the current platform.
func GetInstaller() ([]byte, error) {
	return installerBinary, nil
}

// InstallerFilename returns the filename for the installer.
func InstallerFilename() string {
	return "ccinstaller.exe"
}
