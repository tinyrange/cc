package main

import (
	"log/slog"
	"os"
	"path/filepath"

	"github.com/tinyrange/cc/internal/update"
	"gopkg.in/yaml.v3"
)

const SiteConfigFilename = "site-config.yml"

// SiteConfig holds deployment-wide configuration that can be placed next to the app bundle.
// This allows enterprise deployments to pre-configure settings.
type SiteConfig struct {
	SkipOnboarding    bool  `yaml:"skip_onboarding"`
	AutoUpdateEnabled *bool `yaml:"auto_update_enabled"` // pointer to distinguish unset vs false
}

// GetSiteConfigPath returns the path where site-config.yml should be located.
// This is the directory containing the app bundle (macOS) or executable (other platforms).
func GetSiteConfigPath() (string, error) {
	targetPath, err := update.GetTargetPath()
	if err != nil {
		return "", err
	}

	// Get the directory containing the app/executable
	dir := filepath.Dir(targetPath)
	return filepath.Join(dir, SiteConfigFilename), nil
}

// LoadSiteConfig reads and parses the site config file.
// Returns an empty config if the file doesn't exist.
func LoadSiteConfig() SiteConfig {
	configPath, err := GetSiteConfigPath()
	if err != nil {
		slog.Debug("failed to get site config path", "error", err)
		return SiteConfig{}
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("failed to read site config", "path", configPath, "error", err)
		}
		return SiteConfig{}
	}

	var config SiteConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		slog.Warn("failed to parse site config", "path", configPath, "error", err)
		return SiteConfig{}
	}

	slog.Info("loaded site config", "path", configPath)
	return config
}
