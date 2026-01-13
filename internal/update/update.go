// Package update handles checking for and downloading application updates.
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	Checksum       string // SHA256 checksum (empty if not available)
	ChecksumURL    string // URL to checksums file (for fetching)
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
	Checksum      string    `json:"checksum,omitempty"`     // SHA256 checksum (empty if not available)
	ChecksumURL   string    `json:"checksum_url,omitempty"` // URL to checksums file
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
	return c.CheckWithContext(context.Background())
}

// CheckWithContext checks for updates with context for cancellation/timeout.
func (c *Checker) CheckWithContext(ctx context.Context) UpdateStatus {
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
	return c.fetchAndCache(ctx)
}

// ForceCheck bypasses the cache and always fetches from the API.
func (c *Checker) ForceCheck() UpdateStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.fetchAndCache(context.Background())
}

// ForceUpdate returns an UpdateStatus that will trigger an update regardless of version.
// This is useful for testing the update flow.
func (c *Checker) ForceUpdate() UpdateStatus {
	c.mu.Lock()
	defer c.mu.Unlock()

	status := c.fetchAndCache(context.Background())
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
func (c *Checker) fetchAndCache(ctx context.Context) UpdateStatus {
	release, err := c.fetchLatestRelease(ctx)
	if err != nil {
		c.logger.Warn("failed to fetch latest release", "error", err)
		return UpdateStatus{Error: err}
	}

	// Find the appropriate download asset for this platform
	downloadURL, downloadSize, assetName := c.findAsset(release.Assets)

	// Find the checksums file URL
	checksumURL := c.findChecksumsAsset(release.Assets)

	// Fetch and parse checksum if available
	var checksum string
	if checksumURL != "" && assetName != "" {
		var err error
		checksum, err = c.fetchChecksum(checksumURL, assetName)
		if err != nil {
			c.logger.Warn("failed to fetch checksum", "error", err, "url", checksumURL)
		}
	}

	cached := cachedStatus{
		LatestVersion: release.TagName,
		ReleaseURL:    release.HTMLURL,
		ReleaseNotes:  release.Body,
		DownloadURL:   downloadURL,
		DownloadSize:  downloadSize,
		Checksum:      checksum,
		ChecksumURL:   checksumURL,
		CheckedAt:     time.Now(),
	}

	if err := c.saveCache(cached); err != nil {
		c.logger.Warn("failed to save update cache", "error", err)
	}

	status := c.buildStatus(cached)
	c.lastStatus = &status
	return status
}

// fetchLatestRelease fetches the latest release from GitHub with retry logic.
func (c *Checker) fetchLatestRelease(ctx context.Context) (*ReleaseInfo, error) {
	var lastErr error
	backoff := 500 * time.Millisecond

	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
			backoff *= 2 // Exponential backoff
		}

		release, err := c.doFetchRelease(ctx)
		if err == nil {
			return release, nil
		}
		lastErr = err

		// Don't retry on permanent errors or context cancellation
		if strings.Contains(err.Error(), "no releases found") || ctx.Err() != nil {
			return nil, err
		}
		c.logger.Debug("fetch release attempt failed, retrying", "attempt", attempt+1, "error", err)
	}

	return nil, lastErr
}

// doFetchRelease performs a single fetch attempt.
func (c *Checker) doFetchRelease(ctx context.Context) (*ReleaseInfo, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", GitHubReleasesAPI, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "CCApp/"+c.currentVersion)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	// Support optional GitHub token for higher rate limits
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		// Basic validation - GitHub tokens have known prefixes
		if strings.HasPrefix(token, "ghp_") || strings.HasPrefix(token, "github_pat_") {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch release: %w", err)
	}
	defer resp.Body.Close()

	// Handle rate limiting gracefully
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == 429 {
		return nil, fmt.Errorf("rate limited by GitHub API (try again later)")
	}
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
// Returns the download URL, size, and asset name (for checksum lookup).
func (c *Checker) findAsset(assets []ReleaseAsset) (string, int64, string) {
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
				return asset.BrowserDownloadURL, asset.Size, asset.Name
			}
		}
	}

	// Fallback: return empty, caller should handle gracefully
	return "", 0, ""
}

// findChecksumsAsset finds the URL for the checksums file in release assets.
func (c *Checker) findChecksumsAsset(assets []ReleaseAsset) string {
	// Look for common checksum file names
	checksumNames := []string{
		"checksums.txt",
		"SHA256SUMS",
		"SHA256SUMS.txt",
		"CHECKSUMS",
		"CHECKSUMS.txt",
	}

	for _, name := range checksumNames {
		for _, asset := range assets {
			if strings.EqualFold(asset.Name, name) {
				return asset.BrowserDownloadURL
			}
		}
	}

	return ""
}

// fetchChecksum fetches the checksums file and extracts the checksum for the given asset.
func (c *Checker) fetchChecksum(checksumURL, assetName string) (string, error) {
	req, err := http.NewRequest("GET", checksumURL, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("User-Agent", "CCApp/"+c.currentVersion)

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch checksums: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status: %s", resp.Status)
	}

	// Limit read size to prevent abuse
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024)) // 1MB max
	if err != nil {
		return "", fmt.Errorf("read body: %w", err)
	}

	// Parse the checksums file (format: "checksum  filename" or "checksum *filename")
	return parseChecksumsFile(string(body), assetName)
}

// parseChecksumsFile parses a SHA256SUMS-style file and returns the checksum for the given filename.
// Supports formats:
//   - "abc123  filename" (GNU coreutils style, two spaces)
//   - "abc123 *filename" (binary mode indicator)
//   - "abc123 filename" (single space)
func parseChecksumsFile(content, filename string) (string, error) {
	lines := strings.Split(content, "\n")
	filenameLower := strings.ToLower(filename)

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Split on whitespace - checksum is always first
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}

		checksum := parts[0]
		// The filename might have a leading * for binary mode
		fname := strings.TrimPrefix(parts[len(parts)-1], "*")

		if strings.EqualFold(fname, filename) || strings.EqualFold(fname, filenameLower) {
			// Validate checksum looks like SHA256 (64 hex chars)
			if len(checksum) == 64 && isHex(checksum) {
				return strings.ToLower(checksum), nil
			}
		}
	}

	return "", fmt.Errorf("checksum not found for %s", filename)
}

// isHex returns true if the string contains only hexadecimal characters.
func isHex(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
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
		Checksum:       cached.Checksum,
		ChecksumURL:    cached.ChecksumURL,
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
