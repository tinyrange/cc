//go:build linux

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const desktopFileName = "crumblecracker-url.desktop"

// getDesktopFilePath returns the path to the .desktop file for URL handling.
func getDesktopFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}
	return filepath.Join(home, ".local", "share", "applications", desktopFileName), nil
}

// RegisterURLScheme registers the crumblecracker:// URL scheme on Linux.
// This creates a .desktop file and registers it as the handler for the scheme.
func RegisterURLScheme() error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}

	desktopPath, err := getDesktopFilePath()
	if err != nil {
		return err
	}

	// Ensure directory exists
	dir := filepath.Dir(desktopPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create applications directory: %w", err)
	}

	// Validate executable path to prevent shell injection and broken Exec lines
	// Desktop file Exec values may be passed through a shell in some implementations
	// Also reject spaces since we don't quote the path (quoting causes issues with field code expansion)
	if strings.ContainsAny(exePath, "\"'`$\\ ") {
		return fmt.Errorf("executable path contains unsafe characters: %s", exePath)
	}

	// Create .desktop file content
	// Avoid quoting to prevent issues with field code expansion in some DEs
	content := fmt.Sprintf(`[Desktop Entry]
Name=CrumbleCracker URL Handler
Comment=Handle crumblecracker:// URLs
Exec=%s %%u
Type=Application
MimeType=x-scheme-handler/crumblecracker;
NoDisplay=true
Terminal=false
Categories=Utility;
`, exePath)

	// Write .desktop file
	if err := os.WriteFile(desktopPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write desktop file: %w", err)
	}

	// Register as default handler using xdg-mime
	cmd := exec.Command("xdg-mime", "default", desktopFileName, "x-scheme-handler/crumblecracker")
	if err := cmd.Run(); err != nil {
		// Try updating the database anyway
		exec.Command("update-desktop-database", dir).Run()
		return fmt.Errorf("register mime handler: %w", err)
	}

	// Update desktop database
	exec.Command("update-desktop-database", dir).Run()

	return nil
}

// UnregisterURLScheme removes the URL scheme registration on Linux.
func UnregisterURLScheme() error {
	desktopPath, err := getDesktopFilePath()
	if err != nil {
		return err
	}

	if err := os.Remove(desktopPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove desktop file: %w", err)
	}

	// Update desktop database
	dir := filepath.Dir(desktopPath)
	exec.Command("update-desktop-database", dir).Run()

	return nil
}

// IsURLSchemeRegistered checks if the URL scheme is registered on Linux.
func IsURLSchemeRegistered() bool {
	desktopPath, err := getDesktopFilePath()
	if err != nil {
		return false
	}
	_, err = os.Stat(desktopPath)
	return err == nil
}
