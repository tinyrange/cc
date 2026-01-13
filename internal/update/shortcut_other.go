//go:build !windows

package update

// createWindowsShortcut is a no-op on non-Windows platforms.
// This function is only called when runtime.GOOS == "windows",
// so this stub should never be executed.
func createWindowsShortcut(appPath string) error {
	return nil
}
