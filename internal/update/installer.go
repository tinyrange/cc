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

// adHocSign performs ad-hoc code signing on macOS.
func adHocSign(path string) error {
	cmd := exec.Command("codesign", "-s", "-", path)
	return cmd.Run()
}
