package update

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// LaunchInstaller extracts the embedded installer and launches it to perform the update.
// This function does not return on success - it launches the installer and exits the current process.
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

	// Exit this process so the installer can replace us
	os.Exit(0)

	return nil // Unreachable
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

// LaunchAppAndExit launches the app at the given path and exits the current process.
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

	// Exit this process
	os.Exit(0)

	return nil // Unreachable
}

// DeleteApp removes the app at the specified path.
func DeleteApp(appPath string) error {
	if appPath == "" {
		return nil
	}

	// Safety check: don't delete system directories
	if appPath == "/" || appPath == "/Applications" || appPath == "/usr" {
		return fmt.Errorf("refusing to delete system path: %s", appPath)
	}

	return os.RemoveAll(appPath)
}
