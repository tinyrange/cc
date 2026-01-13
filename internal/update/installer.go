package update

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// ErrInstallerLaunched is returned when the installer has been successfully launched.
// The caller should perform cleanup and exit with code 0.
var ErrInstallerLaunched = errors.New("installer launched successfully, caller should exit")

// ErrAppLaunched is returned when the app has been successfully launched after an update.
// The caller should exit with code 0.
var ErrAppLaunched = errors.New("app launched successfully, caller should exit")

// LaunchInstaller extracts the embedded installer and launches it to perform the update.
// Returns ErrInstallerLaunched on success - the caller should perform cleanup and exit.
func LaunchInstaller(stagingDir, targetPath string) error {
	// Get the embedded installer binary
	installerData, err := GetInstaller()
	if err != nil {
		return fmt.Errorf("get installer: %w", err)
	}

	if len(installerData) == 0 {
		return fmt.Errorf("embedded installer is empty (placeholder not replaced)")
	}

	// Extract installer to temp directory
	tempDir, err := os.MkdirTemp("", "ccinstaller-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}

	installerPath := filepath.Join(tempDir, InstallerFilename())
	if err := os.WriteFile(installerPath, installerData, 0755); err != nil {
		os.RemoveAll(tempDir)
		return fmt.Errorf("write installer: %w", err)
	}

	// On macOS, ad-hoc sign the installer so Gatekeeper allows it
	if runtime.GOOS == "darwin" {
		if err := adHocSign(installerPath); err != nil {
			// Log but don't fail - might work without signing
			fmt.Fprintf(os.Stderr, "warning: failed to sign installer: %v\n", err)
		}
	}

	// Get current process PID to pass to installer
	pid := os.Getpid()

	// Build installer arguments
	args := []string{
		"-staging", stagingDir,
		"-target", targetPath,
		"-pid", fmt.Sprintf("%d", pid),
		"-restart",
	}

	// Launch installer
	cmd := exec.Command(installerPath, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		os.RemoveAll(tempDir)
		return fmt.Errorf("start installer: %w", err)
	}

	// Return sentinel error - caller should perform cleanup and exit
	return ErrInstallerLaunched
}

// GetTargetPath returns the path to the current application that should be replaced.
// On macOS, this returns the .app bundle path.
// On other platforms, this returns the executable path.
func GetTargetPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("get executable path: %w", err)
	}

	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return "", fmt.Errorf("eval symlinks: %w", err)
	}

	// On macOS, we need to find the .app bundle
	if runtime.GOOS == "darwin" {
		return findAppBundle(exe), nil
	}

	return exe, nil
}

// findAppBundle finds the .app bundle containing the given executable path.
func findAppBundle(exePath string) string {
	// Walk up the path looking for .app
	path := exePath
	for {
		if strings.HasSuffix(path, ".app") {
			return path
		}

		parent := filepath.Dir(path)
		if parent == path {
			// Reached root, no .app found - return the executable path
			return exePath
		}
		path = parent
	}
}

// Entitlements for macOS code signing (hypervisor access)
const entitlementsPlist = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>com.apple.security.hypervisor</key>
    <true/>
</dict>
</plist>`

// adHocSign performs ad-hoc code signing on macOS.
func adHocSign(path string) error {
	cmd := exec.Command("codesign", "-s", "-", path)
	return cmd.Run()
}

// signWithEntitlements performs ad-hoc code signing with hypervisor entitlements on macOS.
func signWithEntitlements(appPath string) error {
	// Write entitlements to a temp file
	tmpFile, err := os.CreateTemp("", "entitlements-*.xml")
	if err != nil {
		return fmt.Errorf("create temp entitlements file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(entitlementsPlist); err != nil {
		tmpFile.Close()
		return fmt.Errorf("write entitlements: %w", err)
	}
	tmpFile.Close()

	// Sign with entitlements, force re-sign, and deep sign for bundles
	args := []string{
		"-s", "-",
		"--force",
		"--entitlements", tmpFile.Name(),
	}

	// For .app bundles, add --deep to sign all nested code
	if strings.HasSuffix(appPath, ".app") {
		args = append(args, "--deep")
	}

	args = append(args, appPath)

	cmd := exec.Command("codesign", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("codesign failed: %w\noutput: %s", err, string(output))
	}

	return nil
}

// IsInStandardLocation returns true if the app is running from a standard install location.
func IsInStandardLocation() bool {
	targetPath, err := GetTargetPath()
	if err != nil {
		return false
	}

	switch runtime.GOOS {
	case "darwin":
		// Check if in ~/Applications or /Applications
		home, err := os.UserHomeDir()
		if err != nil {
			return false
		}
		userApps := filepath.Join(home, "Applications")
		return strings.HasPrefix(targetPath, userApps) ||
			strings.HasPrefix(targetPath, "/Applications")
	case "linux":
		// Check if in /usr/local/bin, ~/.local/bin, or /opt
		home, err := os.UserHomeDir()
		if err != nil {
			return false
		}
		localBin := filepath.Join(home, ".local", "bin")
		return strings.HasPrefix(targetPath, "/usr/local/bin") ||
			strings.HasPrefix(targetPath, localBin) ||
			strings.HasPrefix(targetPath, "/opt")
	case "windows":
		// Check if in Program Files or user AppData
		return strings.Contains(strings.ToLower(targetPath), "program files") ||
			strings.Contains(strings.ToLower(targetPath), "appdata")
	}
	return false
}

// GetUserApplicationsDir returns the user's Applications directory for the current platform.
func GetUserApplicationsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}

	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Applications"), nil
	case "linux":
		return filepath.Join(home, ".local", "bin"), nil
	case "windows":
		appData, err := os.UserConfigDir()
		if err != nil {
			return "", fmt.Errorf("get config dir: %w", err)
		}
		return filepath.Join(appData, "Programs"), nil
	}
	return "", fmt.Errorf("unsupported platform: %s", runtime.GOOS)
}

// CopyAppToLocation copies the current app to the target directory.
// Returns the path to the copied app.
func CopyAppToLocation(targetDir string) (string, error) {
	sourcePath, err := GetTargetPath()
	if err != nil {
		return "", fmt.Errorf("get source path: %w", err)
	}

	// Ensure target directory exists
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return "", fmt.Errorf("create target dir: %w", err)
	}

	// Get the app name from the source path
	appName := filepath.Base(sourcePath)
	targetPath := filepath.Join(targetDir, appName)

	// Remove existing app at target if it exists
	if _, err := os.Stat(targetPath); err == nil {
		if err := os.RemoveAll(targetPath); err != nil {
			return "", fmt.Errorf("remove existing app: %w", err)
		}
	}

	// Copy the app
	if runtime.GOOS == "darwin" && strings.HasSuffix(sourcePath, ".app") {
		// On macOS, copy the entire .app bundle directory
		if err := copyDir(sourcePath, targetPath); err != nil {
			return "", fmt.Errorf("copy app bundle: %w", err)
		}

		// Re-sign the copied app with hypervisor entitlements
		if err := signWithEntitlements(targetPath); err != nil {
			// Log but don't fail - might work without signing
			fmt.Fprintf(os.Stderr, "warning: failed to sign copied app: %v\n", err)
		}
	} else {
		// On other platforms, copy the binary
		if err := copyFile(sourcePath, targetPath); err != nil {
			return "", fmt.Errorf("copy binary: %w", err)
		}
		// Make executable
		if err := os.Chmod(targetPath, 0o755); err != nil {
			return "", fmt.Errorf("chmod: %w", err)
		}
	}

	return targetPath, nil
}

// copyFile copies a single file from src to dst.
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

// copyDir recursively copies a directory from src to dst.
func copyDir(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dst, srcInfo.Mode()); err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			// Handle symlinks
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if info.Mode()&os.ModeSymlink != 0 {
				link, err := os.Readlink(srcPath)
				if err != nil {
					return err
				}
				if err := os.Symlink(link, dstPath); err != nil {
					return err
				}
			} else {
				if err := copyFileWithMode(srcPath, dstPath, info.Mode()); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// copyFileWithMode copies a file preserving its mode.
func copyFileWithMode(src, dst string, mode os.FileMode) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, mode)
}

// LaunchAppAndExit launches the app at the given path.
// Returns ErrAppLaunched on success - the caller should exit with code 0.
func LaunchAppAndExit(appPath string) error {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "darwin":
		// On macOS, use 'open' to launch the .app bundle
		if strings.HasSuffix(appPath, ".app") {
			cmd = exec.Command("open", "-n", appPath)
		} else {
			cmd = exec.Command(appPath)
		}
	case "windows":
		cmd = exec.Command(appPath)
	default:
		cmd = exec.Command(appPath)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("launch app: %w", err)
	}

	// Return sentinel error - caller should exit
	return ErrAppLaunched
}

// DeleteApp removes the app at the specified path with safety checks.
func DeleteApp(appPath string) error {
	if appPath == "" {
		return nil
	}

	// Resolve symlinks to get the real path
	realPath, err := filepath.EvalSymlinks(appPath)
	if err != nil {
		// If we can't resolve the path, it might not exist anymore
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("resolve path: %w", err)
	}

	// Clean the path to normalize it
	realPath = filepath.Clean(realPath)

	// Verify it's in an expected location
	if !isValidAppLocation(realPath) {
		return fmt.Errorf("refusing to delete: path not in expected location: %s", appPath)
	}

	// Verify it looks like our app (name contains crumblecracker or ccapp)
	if !isOurApp(realPath) {
		return fmt.Errorf("refusing to delete: doesn't look like CrumbleCracker: %s", appPath)
	}

	return os.RemoveAll(realPath)
}

// isValidAppLocation checks if the path is within an expected app location.
func isValidAppLocation(path string) bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}

	// Platform-specific valid locations
	switch runtime.GOOS {
	case "darwin":
		// Valid: ~/Applications, /Applications, /Users/*/Applications
		// Also allow temporary locations for cleanup
		return strings.HasPrefix(path, filepath.Join(home, "Applications")) ||
			strings.HasPrefix(path, "/Applications/") ||
			strings.HasPrefix(path, os.TempDir())
	case "linux":
		// Valid: ~/.local/bin, /usr/local/bin, /opt, /tmp
		localBin := filepath.Join(home, ".local", "bin")
		return strings.HasPrefix(path, localBin) ||
			strings.HasPrefix(path, "/usr/local/bin/") ||
			strings.HasPrefix(path, "/opt/") ||
			strings.HasPrefix(path, os.TempDir())
	case "windows":
		// Valid: Program Files, AppData, user profile
		programFiles := os.Getenv("ProgramFiles")
		programFilesX86 := os.Getenv("ProgramFiles(x86)")
		appData := os.Getenv("LOCALAPPDATA")
		return strings.HasPrefix(strings.ToLower(path), strings.ToLower(programFiles)) ||
			strings.HasPrefix(strings.ToLower(path), strings.ToLower(programFilesX86)) ||
			strings.HasPrefix(strings.ToLower(path), strings.ToLower(appData)) ||
			strings.HasPrefix(strings.ToLower(path), strings.ToLower(os.TempDir()))
	}
	return false
}

// isOurApp checks if the path looks like a CrumbleCracker installation.
func isOurApp(path string) bool {
	name := strings.ToLower(filepath.Base(path))

	// Check for expected app names
	validNames := []string{
		"crumblecracker",
		"ccapp",
	}

	for _, valid := range validNames {
		if strings.Contains(name, valid) {
			return true
		}
	}

	// Check for expected extensions
	switch runtime.GOOS {
	case "darwin":
		// Accept .app bundles
		if strings.HasSuffix(name, ".app") {
			return true
		}
	case "windows":
		// Accept .exe files
		if strings.HasSuffix(name, ".exe") {
			return true
		}
	}

	return false
}

// CreateDesktopShortcut creates a desktop shortcut/entry for the app.
// On Windows: Creates a .lnk file in the Start Menu Programs folder.
// On Linux: Creates a .desktop file in ~/.local/share/applications/.
// On macOS: No-op (apps in ~/Applications are already discoverable via Spotlight/Launchpad).
func CreateDesktopShortcut(appPath string) error {
	switch runtime.GOOS {
	case "darwin":
		// macOS apps in ~/Applications are already discoverable
		return nil
	case "windows":
		return createWindowsShortcut(appPath)
	case "linux":
		return createLinuxDesktopEntry(appPath)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

// RemoveDesktopShortcut removes the desktop shortcut/entry for the app.
func RemoveDesktopShortcut() error {
	switch runtime.GOOS {
	case "darwin":
		return nil
	case "windows":
		return removeWindowsShortcut()
	case "linux":
		return removeLinuxDesktopEntry()
	default:
		return nil
	}
}

// escapePowershellString escapes a string for safe use in PowerShell single-quoted strings.
// Single quotes are escaped by doubling them.
func escapePowershellString(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

// validatePathForScript validates that a path is safe to use in shell scripts.
// Returns an error if the path contains dangerous characters.
func validatePathForScript(path string) error {
	// Check for null bytes
	if strings.ContainsRune(path, 0) {
		return fmt.Errorf("path contains null byte")
	}

	// Check for control characters (ASCII 0-31 except tab which is sometimes valid)
	for _, r := range path {
		if r < 32 && r != '\t' {
			return fmt.Errorf("path contains control character: %U", r)
		}
	}

	return nil
}

// createWindowsShortcut creates a .lnk shortcut in the Start Menu.
func createWindowsShortcut(appPath string) error {
	// Validate path before using in PowerShell
	if err := validatePathForScript(appPath); err != nil {
		return fmt.Errorf("invalid app path: %w", err)
	}

	// Get the Start Menu Programs directory
	appData := os.Getenv("APPDATA")
	if appData == "" {
		return fmt.Errorf("APPDATA environment variable not set")
	}

	startMenuDir := filepath.Join(appData, "Microsoft", "Windows", "Start Menu", "Programs")
	shortcutPath := filepath.Join(startMenuDir, "CrumbleCracker.lnk")

	// Escape paths for safe use in PowerShell single-quoted strings
	escapedShortcutPath := escapePowershellString(shortcutPath)
	escapedAppPath := escapePowershellString(appPath)
	escapedWorkDir := escapePowershellString(filepath.Dir(appPath))

	// Use PowerShell to create the shortcut
	script := fmt.Sprintf(`
$WshShell = New-Object -ComObject WScript.Shell
$Shortcut = $WshShell.CreateShortcut('%s')
$Shortcut.TargetPath = '%s'
$Shortcut.WorkingDirectory = '%s'
$Shortcut.Description = 'CrumbleCracker - Run Linux containers'
$Shortcut.Save()
`, escapedShortcutPath, escapedAppPath, escapedWorkDir)

	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("powershell shortcut creation failed: %w\noutput: %s", err, string(output))
	}

	return nil
}

// removeWindowsShortcut removes the Start Menu shortcut.
func removeWindowsShortcut() error {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		return nil
	}

	shortcutPath := filepath.Join(appData, "Microsoft", "Windows", "Start Menu", "Programs", "CrumbleCracker.lnk")
	if err := os.Remove(shortcutPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// linuxDesktopEntry is the content of the .desktop file for Linux.
const linuxDesktopEntryTemplate = `[Desktop Entry]
Name=CrumbleCracker
Comment=Run Linux containers with native performance
Exec=%s
Icon=%s
Type=Application
Categories=Development;Emulator;System;
Terminal=false
StartupNotify=true
`

// createLinuxDesktopEntry creates a .desktop file in ~/.local/share/applications/.
func createLinuxDesktopEntry(appPath string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("get home dir: %w", err)
	}

	// Create the applications directory if it doesn't exist
	appsDir := filepath.Join(home, ".local", "share", "applications")
	if err := os.MkdirAll(appsDir, 0o755); err != nil {
		return fmt.Errorf("create applications dir: %w", err)
	}

	desktopPath := filepath.Join(appsDir, "crumblecracker.desktop")

	// For the icon, we'll use the app path for now (apps can provide icons separately)
	// In a real deployment, you'd want to install an icon to ~/.local/share/icons/
	iconPath := appPath // Placeholder - ideally would be a proper icon path

	content := fmt.Sprintf(linuxDesktopEntryTemplate, appPath, iconPath)
	if err := os.WriteFile(desktopPath, []byte(content), 0o755); err != nil {
		return fmt.Errorf("write desktop file: %w", err)
	}

	// Update the desktop database (best effort)
	_ = exec.Command("update-desktop-database", appsDir).Run()

	return nil
}

// removeLinuxDesktopEntry removes the .desktop file.
func removeLinuxDesktopEntry() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	desktopPath := filepath.Join(home, ".local", "share", "applications", "crumblecracker.desktop")
	if err := os.Remove(desktopPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
