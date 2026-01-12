// Package update handles checking for and downloading application updates.
package update

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"golang.org/x/mod/semver"
)

const (
	// GitHubReleasesAPI is the endpoint for checking releases.
	// We use /releases instead of /releases/latest because /latest only returns
	// non-prerelease, non-draft releases.
	GitHubReleasesAPI = "https://api.github.com/repos/tinyrange/cc/releases"

	// ReleasesPageURL is the URL to the releases page for manual downloads.
	ReleasesPageURL = "https://github.com/tinyrange/cc/releases"

	// CheckInterval is how long to cache update check results.
	CheckInterval = 24 * time.Hour

	// CacheFilename is the name of the cache file.
	CacheFilename = "update_check.json"
)

// ReleaseAsset represents a downloadable asset from a GitHub release.
type ReleaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
	ContentType        string `json:"content_type"`
}

// ReleaseInfo represents a GitHub release.
type ReleaseInfo struct {
	TagName     string         `json:"tag_name"`
	Name        string         `json:"name"`
	HTMLURL     string         `json:"html_url"`
	PublishedAt string         `json:"published_at"`
	Prerelease  bool           `json:"prerelease"`
	Body        string         `json:"body"`
	Assets      []ReleaseAsset `json:"assets"`
}

// UpdateStatus represents the result of an update check.
type UpdateStatus struct {
	Available      bool
	CurrentVersion string
	LatestVersion  string
	ReleaseURL     string
	ReleaseNotes   string
	DownloadURL    string
	DownloadSize   int64
	CheckedAt      time.Time
	Error          error
}

// cachedStatus is stored to disk to avoid repeated API calls.
type cachedStatus struct {
	LatestVersion string    `json:"latest_version"`
	ReleaseURL    string    `json:"release_url"`
	ReleaseNotes  string    `json:"release_notes"`
	DownloadURL   string    `json:"download_url"`
	DownloadSize  int64     `json:"download_size"`
	CheckedAt     time.Time `json:"checked_at"`
}

// Checker manages update checking with caching.
type Checker struct {
	currentVersion string
	cacheDir       string
	mu             sync.RWMutex
	lastStatus     *UpdateStatus
	client         *http.Client
	logger         *slog.Logger
}

// NewChecker creates a new update checker.
func NewChecker(currentVersion, cacheDir string) *Checker {
	return &Checker{
		currentVersion: currentVersion,
		cacheDir:       cacheDir,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		logger: slog.Default(),
	}
}

// SetLogger sets the logger for the checker.
func (c *Checker) SetLogger(logger *slog.Logger) {
	c.logger = logger
}

// Check checks for updates, using cache if available and fresh.
func (c *Checker) Check() UpdateStatus {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Try to load from cache first
	cached, err := c.loadCache()
	if err == nil && time.Since(cached.CheckedAt) < CheckInterval {
		c.logger.Debug("using cached update check", "checked_at", cached.CheckedAt)
		status := c.buildStatus(cached)
		c.lastStatus = &status
		return status
	}

	// Cache miss or expired, fetch from API
	return c.fetchAndCache()
}

// ForceCheck bypasses the cache and always fetches from the API.
func (c *Checker) ForceCheck() UpdateStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.fetchAndCache()
}

// ForceUpdate returns an UpdateStatus that will trigger an update regardless of version.
// This is useful for testing the update flow.
func (c *Checker) ForceUpdate() UpdateStatus {
	c.mu.Lock()
	defer c.mu.Unlock()

	status := c.fetchAndCache()
	if status.Error != nil {
		return status
	}

	// Force available even if versions match
	status.Available = true
	c.lastStatus = &status
	return status
}

// LastStatus returns the last known update status.
func (c *Checker) LastStatus() *UpdateStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastStatus
}

// CurrentVersion returns the current application version.
func (c *Checker) CurrentVersion() string {
	return c.currentVersion
}

// fetchAndCache fetches the latest release info and caches it.
func (c *Checker) fetchAndCache() UpdateStatus {
	release, err := c.fetchLatestRelease()
	if err != nil {
		c.logger.Warn("failed to fetch latest release", "error", err)
		return UpdateStatus{Error: err}
	}

	// Find the appropriate download asset for this platform
	downloadURL, downloadSize := c.findAsset(release.Assets)

	cached := cachedStatus{
		LatestVersion: release.TagName,
		ReleaseURL:    release.HTMLURL,
		ReleaseNotes:  release.Body,
		DownloadURL:   downloadURL,
		DownloadSize:  downloadSize,
		CheckedAt:     time.Now(),
	}

	if err := c.saveCache(cached); err != nil {
		c.logger.Warn("failed to save update cache", "error", err)
	}

	status := c.buildStatus(cached)
	c.lastStatus = &status
	return status
}

// fetchLatestRelease fetches the latest release from GitHub.
func (c *Checker) fetchLatestRelease() (*ReleaseInfo, error) {
	req, err := http.NewRequest("GET", GitHubReleasesAPI, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "CCApp/"+c.currentVersion)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("no releases found (repository may not exist or has no releases)")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %s", resp.Status)
	}

	var releases []ReleaseInfo
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if len(releases) == 0 {
		return nil, fmt.Errorf("no releases found")
	}

	// Return the first (latest) release
	return &releases[0], nil
}

// findAsset finds the download URL for the current platform.
func (c *Checker) findAsset(assets []ReleaseAsset) (string, int64) {
	// Build expected asset name based on platform
	goos := runtime.GOOS
	goarch := runtime.GOARCH

	// Expected patterns:
	// CrumbleCracker_darwin_arm64.zip
	// CrumbleCracker_windows_amd64.exe
	// CrumbleCracker_linux_amd64
	var patterns []string
	switch goos {
	case "darwin":
		patterns = []string{
			fmt.Sprintf("CrumbleCracker_%s_%s.zip", goos, goarch),
			fmt.Sprintf("CrumbleCracker-%s-%s.zip", goos, goarch),
		}
	case "windows":
		patterns = []string{
			fmt.Sprintf("CrumbleCracker_%s_%s.exe", goos, goarch),
			fmt.Sprintf("CrumbleCracker-%s-%s.exe", goos, goarch),
		}
	default:
		patterns = []string{
			fmt.Sprintf("CrumbleCracker_%s_%s", goos, goarch),
			fmt.Sprintf("CrumbleCracker-%s-%s", goos, goarch),
		}
	}

	for _, pattern := range patterns {
		for _, asset := range assets {
			if strings.EqualFold(asset.Name, pattern) {
				return asset.BrowserDownloadURL, asset.Size
			}
		}
	}

	// Fallback: return empty, caller should handle gracefully
	return "", 0
}

// buildStatus creates an UpdateStatus from cached data.
func (c *Checker) buildStatus(cached cachedStatus) UpdateStatus {
	available := c.isNewer(cached.LatestVersion)

	return UpdateStatus{
		Available:      available,
		CurrentVersion: c.currentVersion,
		LatestVersion:  cached.LatestVersion,
		ReleaseURL:     cached.ReleaseURL,
		ReleaseNotes:   cached.ReleaseNotes,
		DownloadURL:    cached.DownloadURL,
		DownloadSize:   cached.DownloadSize,
		CheckedAt:      cached.CheckedAt,
	}
}

// isNewer returns true if the given version is newer than the current version.
func (c *Checker) isNewer(latestVersion string) bool {
	current := c.currentVersion
	latest := latestVersion

	// Normalize to have "v" prefix for semver comparison
	if !strings.HasPrefix(current, "v") {
		current = "v" + current
	}
	if !strings.HasPrefix(latest, "v") {
		latest = "v" + latest
	}

	// Dev versions always show updates
	if current == "vdev" || current == "v0.0.0" {
		return true
	}

	// Use semver comparison
	if !semver.IsValid(current) || !semver.IsValid(latest) {
		// Fall back to string comparison if not valid semver
		return latest > current
	}

	return semver.Compare(latest, current) > 0
}

// cachePath returns the path to the cache file.
func (c *Checker) cachePath() string {
	return filepath.Join(c.cacheDir, CacheFilename)
}

// loadCache loads the cached update status from disk.
func (c *Checker) loadCache() (cachedStatus, error) {
	data, err := os.ReadFile(c.cachePath())
	if err != nil {
		return cachedStatus{}, err
	}

	var cached cachedStatus
	if err := json.Unmarshal(data, &cached); err != nil {
		return cachedStatus{}, err
	}

	return cached, nil
}

// saveCache saves the update status to disk.
func (c *Checker) saveCache(cached cachedStatus) error {
	if err := os.MkdirAll(c.cacheDir, 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cached, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(c.cachePath(), data, 0o644)
}
