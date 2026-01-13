package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// AppSettings stores application-level settings
type AppSettings struct {
	OnboardingCompleted   bool      `json:"onboarding_completed"`
	AutoUpdateEnabled     bool      `json:"auto_update_enabled"`
	InstallPath           string    `json:"install_path,omitempty"`
	InstalledAt           time.Time `json:"installed_at,omitempty"`
	CleanupPending        string    `json:"cleanup_pending,omitempty"` // Path to delete on next startup
	CreateDesktopShortcut bool      `json:"create_desktop_shortcut"`   // Create desktop/start menu shortcut (Windows/Linux)
}

// SettingsStore manages persistent storage of application settings
type SettingsStore struct {
	path     string
	settings AppSettings
}

// NewSettingsStore creates or loads the settings store
func NewSettingsStore() (*SettingsStore, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}

	storePath := filepath.Join(configDir, "ccapp", "settings.json")
	store := &SettingsStore{path: storePath}

	if err := store.load(); err != nil && !os.IsNotExist(err) {
		// If file exists but is corrupted, treat as first run
		// Log warning but don't fail
		store.settings = AppSettings{}
	}

	return store, nil
}

func (s *SettingsStore) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &s.settings)
}

func (s *SettingsStore) save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o644)
}

// Get returns the current settings
func (s *SettingsStore) Get() AppSettings {
	return s.settings
}

// Set updates and persists settings
func (s *SettingsStore) Set(settings AppSettings) error {
	s.settings = settings
	return s.save()
}

// SetOnboardingCompleted marks onboarding as completed
func (s *SettingsStore) SetOnboardingCompleted(completed bool) error {
	s.settings.OnboardingCompleted = completed
	return s.save()
}

// SetAutoUpdateEnabled sets the auto-update preference
func (s *SettingsStore) SetAutoUpdateEnabled(enabled bool) error {
	s.settings.AutoUpdateEnabled = enabled
	return s.save()
}

// SetInstallInfo records where the app was installed
func (s *SettingsStore) SetInstallInfo(path string) error {
	s.settings.InstallPath = path
	s.settings.InstalledAt = time.Now()
	return s.save()
}

// SetCleanupPending schedules a path for deletion on next startup
func (s *SettingsStore) SetCleanupPending(path string) error {
	s.settings.CleanupPending = path
	return s.save()
}

// ClearCleanupPending clears the pending cleanup path
func (s *SettingsStore) ClearCleanupPending() error {
	s.settings.CleanupPending = ""
	return s.save()
}
