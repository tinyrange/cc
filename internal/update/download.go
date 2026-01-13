package update

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DownloadProgress represents the current download progress.
type DownloadProgress struct {
	Current        int64
	Total          int64
	Status         string
	BytesPerSecond float64       // Current download speed in bytes per second
	ETA            time.Duration // Estimated time remaining (-1 if unknown)
}

// ProgressCallback is called during download with progress updates.
type ProgressCallback func(progress DownloadProgress)

// Downloader handles downloading update files.
type Downloader struct {
	client   *http.Client
	callback ProgressCallback
}

// NewDownloader creates a new downloader.
func NewDownloader() *Downloader {
	return &Downloader{
		client: &http.Client{
			Timeout: 0, // No timeout for large downloads
		},
	}
}

// SetProgressCallback sets the callback for progress updates.
func (d *Downloader) SetProgressCallback(callback ProgressCallback) {
	d.callback = callback
}

// Download downloads a file from the given URL to the destination path.
func (d *Downloader) Download(url, destPath string) error {
	d.reportProgress(DownloadProgress{Status: "Connecting..."})

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "CCApp-Updater")

	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %s", resp.Status)
	}

	// Create parent directory if needed
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	// Create destination file
	out, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer out.Close()

	d.reportProgress(DownloadProgress{
		Status: "Downloading...",
		Total:  resp.ContentLength,
	})

	// Copy with progress
	reader := &progressReader{
		reader:   resp.Body,
		total:    resp.ContentLength,
		callback: d.callback,
	}

	if _, err := io.Copy(out, reader); err != nil {
		os.Remove(destPath)
		return fmt.Errorf("write file: %w", err)
	}

	return nil
}

// DownloadToStaging downloads the update to a staging directory and returns the path.
// For macOS, it extracts the .app bundle from the zip.
// For other platforms, it downloads the binary directly.
// If expectedChecksum is provided (non-empty), the download is verified against it.
func (d *Downloader) DownloadToStaging(url string, goos string, expectedChecksum string) (string, error) {
	// Create staging directory
	stagingDir := filepath.Join(os.TempDir(), fmt.Sprintf("ccapp-update-%d", time.Now().Unix()))
	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		return "", fmt.Errorf("create staging dir: %w", err)
	}

	// Download the file
	downloadPath := filepath.Join(stagingDir, "download")
	if err := d.Download(url, downloadPath); err != nil {
		os.RemoveAll(stagingDir)
		return "", err
	}

	// Verify checksum if provided
	if expectedChecksum != "" {
		d.reportProgress(DownloadProgress{Status: "Verifying checksum..."})

		if err := d.VerifyChecksum(downloadPath, expectedChecksum); err != nil {
			os.RemoveAll(stagingDir)
			return "", fmt.Errorf("checksum verification failed: %w", err)
		}
	}

	// For macOS, we need to extract the zip
	if goos == "darwin" && strings.HasSuffix(url, ".zip") {
		d.reportProgress(DownloadProgress{Status: "Extracting..."})

		if err := extractZip(downloadPath, stagingDir); err != nil {
			os.RemoveAll(stagingDir)
			return "", fmt.Errorf("extract zip: %w", err)
		}

		// Remove the zip file
		os.Remove(downloadPath)
	}

	return stagingDir, nil
}

// VerifyChecksum verifies the SHA256 checksum of a file.
func (d *Downloader) VerifyChecksum(filePath, expectedHash string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}

	actualHash := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(actualHash, expectedHash) {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedHash, actualHash)
	}

	return nil
}

func (d *Downloader) reportProgress(p DownloadProgress) {
	if d.callback != nil {
		d.callback(p)
	}
}

// progressReader wraps an io.Reader and reports progress.
type progressReader struct {
	reader   io.Reader
	total    int64
	current  int64
	callback ProgressCallback

	// Timing for speed/ETA calculation
	startTime  time.Time
	lastUpdate time.Time
	lastBytes  int64
	speed      float64 // smoothed bytes per second
}

func (r *progressReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.current += int64(n)

	if r.callback != nil {
		now := time.Now()

		// Initialize timing on first read
		if r.startTime.IsZero() {
			r.startTime = now
			r.lastUpdate = now
			r.lastBytes = 0
		}

		// Calculate speed using exponential moving average
		elapsed := now.Sub(r.lastUpdate).Seconds()
		if elapsed >= 0.1 { // Update speed every 100ms minimum
			bytesThisInterval := r.current - r.lastBytes
			instantSpeed := float64(bytesThisInterval) / elapsed

			// Smooth the speed with exponential moving average (alpha = 0.3)
			if r.speed == 0 {
				r.speed = instantSpeed
			} else {
				r.speed = 0.3*instantSpeed + 0.7*r.speed
			}

			r.lastUpdate = now
			r.lastBytes = r.current
		}

		// Calculate ETA
		var eta time.Duration = -1
		if r.speed > 0 && r.total > 0 {
			remaining := r.total - r.current
			if remaining > 0 {
				eta = time.Duration(float64(remaining)/r.speed) * time.Second
			} else {
				eta = 0
			}
		}

		r.callback(DownloadProgress{
			Current:        r.current,
			Total:          r.total,
			Status:         "Downloading...",
			BytesPerSecond: r.speed,
			ETA:            eta,
		})
	}

	return n, err
}

// extractZip extracts a zip file to the destination directory.
func extractZip(zipPath, destDir string) error {
	// Get absolute path of destination to ensure consistent comparison
	absDestDir, err := filepath.Abs(destDir)
	if err != nil {
		return fmt.Errorf("get absolute path: %w", err)
	}
	absDestDir = filepath.Clean(absDestDir)

	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		// Clean the file name from the archive
		name := filepath.Clean(f.Name)

		// Reject entries that start with .. after cleaning
		if strings.HasPrefix(name, "..") {
			return fmt.Errorf("invalid file path in archive: %s", f.Name)
		}

		// Reject symlinks in archive to prevent symlink attacks
		if f.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlinks not allowed in update archive: %s", f.Name)
		}

		// Build the target path
		fpath := filepath.Join(absDestDir, name)
		fpath = filepath.Clean(fpath)

		// Security check: ensure the path is within destDir
		// The path must start with destDir followed by a separator (or be exactly destDir)
		if !strings.HasPrefix(fpath, absDestDir+string(filepath.Separator)) && fpath != absDestDir {
			return fmt.Errorf("invalid file path (directory traversal attempt): %s", f.Name)
		}

		if f.FileInfo().IsDir() {
			os.MkdirAll(fpath, f.Mode())
			continue
		}

		if err := os.MkdirAll(filepath.Dir(fpath), 0o755); err != nil {
			return err
		}

		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			outFile.Close()
			return err
		}

		_, err = io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()

		if err != nil {
			return err
		}
	}

	return nil
}
