//go:build windows

package main

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows/registry"
)

// RegisterURLScheme registers the crumblecracker:// URL scheme with Windows.
// This creates registry entries under HKEY_CURRENT_USER\Software\Classes\crumblecracker.
func RegisterURLScheme() error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}

	// Create HKEY_CURRENT_USER\Software\Classes\crumblecracker
	schemeKey, _, err := registry.CreateKey(registry.CURRENT_USER, `Software\Classes\crumblecracker`, registry.ALL_ACCESS)
	if err != nil {
		return fmt.Errorf("create scheme key: %w", err)
	}
	defer schemeKey.Close()

	// Set default value and URL Protocol marker
	if err := schemeKey.SetStringValue("", "URL:CrumbleCracker Protocol"); err != nil {
		return fmt.Errorf("set scheme default value: %w", err)
	}
	if err := schemeKey.SetStringValue("URL Protocol", ""); err != nil {
		return fmt.Errorf("set URL Protocol: %w", err)
	}

	// Create DefaultIcon key
	iconKey, _, err := registry.CreateKey(schemeKey, `DefaultIcon`, registry.ALL_ACCESS)
	if err != nil {
		return fmt.Errorf("create DefaultIcon key: %w", err)
	}
	defer iconKey.Close()
	if err := iconKey.SetStringValue("", fmt.Sprintf(`"%s",1`, exePath)); err != nil {
		return fmt.Errorf("set DefaultIcon value: %w", err)
	}

	// Create shell\open\command key
	shellKey, _, err := registry.CreateKey(schemeKey, `shell`, registry.ALL_ACCESS)
	if err != nil {
		return fmt.Errorf("create shell key: %w", err)
	}
	defer shellKey.Close()

	openKey, _, err := registry.CreateKey(shellKey, `open`, registry.ALL_ACCESS)
	if err != nil {
		return fmt.Errorf("create open key: %w", err)
	}
	defer openKey.Close()

	commandKey, _, err := registry.CreateKey(openKey, `command`, registry.ALL_ACCESS)
	if err != nil {
		return fmt.Errorf("create command key: %w", err)
	}
	defer commandKey.Close()

	// Command value: "C:\path\to\ccapp.exe" "%1"
	commandValue := fmt.Sprintf(`"%s" "%%1"`, exePath)
	if err := commandKey.SetStringValue("", commandValue); err != nil {
		return fmt.Errorf("set command value: %w", err)
	}

	return nil
}

// UnregisterURLScheme removes the crumblecracker:// URL scheme from Windows registry.
func UnregisterURLScheme() error {
	err := registry.DeleteKey(registry.CURRENT_USER, `Software\Classes\crumblecracker`)
	if err != nil && err != registry.ErrNotExist {
		return fmt.Errorf("delete scheme key: %w", err)
	}
	return nil
}

// IsURLSchemeRegistered checks if the URL scheme is already registered.
func IsURLSchemeRegistered() bool {
	key, err := registry.OpenKey(registry.CURRENT_USER, `Software\Classes\crumblecracker`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	key.Close()
	return true
}
