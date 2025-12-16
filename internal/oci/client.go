package oci

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/schollz/progressbar/v3"
)

const (
	defaultRegistry = "https://registry-1.docker.io/v2"
)

// Client is an OCI registry client that handles image pulling and caching.
type Client struct {
	cacheDir string
	logger   *slog.Logger
	client   *http.Client
}

// NewClient creates a new OCI client with the specified cache directory.
func NewClient(cacheDir string) (*Client, error) {
	if cacheDir == "" {
		cfg, err := os.UserConfigDir()
		if err != nil {
			return nil, fmt.Errorf("get user config dir: %w", err)
		}
		cacheDir = filepath.Join(cfg, "cc", "oci")
	}

	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return nil, fmt.Errorf("create cache directory %s: %w", cacheDir, err)
	}

	return &Client{
		cacheDir: cacheDir,
		logger:   slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})),
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
	}, nil
}

// registryContext holds state for communicating with a single registry.
type registryContext struct {
	logger   *slog.Logger
	client   *http.Client
	registry string
	token    string
}

func (ctx *registryContext) makeRequest(method string, url string, accept []string) (*http.Request, error) {
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return nil, err
	}

	if ctx.token != "" {
		req.Header.Set("Authorization", "Bearer "+ctx.token)
	}

	for _, val := range accept {
		req.Header.Add("Accept", val)
	}

	return req, nil
}

func (ctx *registryContext) handleResponse(resp *http.Response) (bool, error) {
	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusUnauthorized:
		authHeader := resp.Header.Get("www-authenticate")
		resp.Body.Close()

		authParams, err := parseAuthenticate(authHeader)
		if err != nil {
			return false, fmt.Errorf("parse authenticate header: %w", err)
		}

		tokenURL := fmt.Sprintf("%s?service=%s&scope=%s",
			authParams["realm"],
			authParams["service"],
			authParams["scope"])

		ctx.logger.Debug("requesting registry token", slog.String("url", tokenURL))

		req, err := http.NewRequest(http.MethodGet, tokenURL, nil)
		if err != nil {
			return false, fmt.Errorf("build token request: %w", err)
		}

		resp, err := ctx.client.Do(req)
		if err != nil {
			return false, fmt.Errorf("request registry token: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return false, fmt.Errorf("token request failed: %s", resp.Status)
		}

		var token tokenResponse
		if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
			return false, fmt.Errorf("decode token response: %w", err)
		}

		if token.Token != "" {
			ctx.token = token.Token
		} else if token.AccessToken != "" {
			ctx.token = token.AccessToken
		} else {
			return false, errors.New("token response missing token field")
		}

		return false, nil
	default:
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return false, fmt.Errorf("registry request failed: %s (%s)", resp.Status, strings.TrimSpace(string(body)))
	}
}

type tokenResponse struct {
	Token       string `json:"token"`
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
	IssuedAt    string `json:"issued_at"`
}

func parseAuthenticate(value string) (map[string]string, error) {
	if value == "" {
		return nil, fmt.Errorf("missing authenticate header")
	}

	value = strings.TrimPrefix(value, "Bearer ")

	tokens := strings.Split(value, ",")
	ret := make(map[string]string)

	for _, token := range tokens {
		key, val, ok := strings.Cut(token, "=")
		if !ok {
			return nil, fmt.Errorf("malformed authenticate header segment %q", token)
		}
		val = strings.Trim(val, "\" ")
		key = strings.TrimSpace(key)
		ret[key] = val
	}

	return ret, nil
}

func (c *Client) cacheKey(path string, accept []string) string {
	sum := sha256.Sum256([]byte(path + "\x00" + strings.Join(accept, ",")))
	sanitized := sanitizeForFilename(path)
	return fmt.Sprintf("%s_%s", sanitized, hex.EncodeToString(sum[:8]))
}

func sanitizeForFilename(value string) string {
	value = strings.TrimPrefix(value, "/")
	var b strings.Builder
	for _, r := range value {
		switch r {
		case '/', '\\', ':', '?', '*', '"', '<', '>', '|', ' ':
			b.WriteByte('_')
		default:
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "root"
	}
	return b.String()
}

func (c *Client) fetchToCache(ctx *registryContext, path string, accept []string) (string, error) {
	cacheName := c.cacheKey(path, accept)
	cachePath := filepath.Join(c.cacheDir, cacheName)

	if _, err := os.Stat(cachePath); err == nil {
		ctx.logger.Debug("cache hit", slog.String("cache", cachePath))
		return cachePath, nil
	}

	const maxAttempts = 3
	for attempt := 0; attempt < maxAttempts; attempt++ {
		req, err := ctx.makeRequest(http.MethodGet, ctx.registry+path, accept)
		if err != nil {
			return "", fmt.Errorf("build registry request: %w", err)
		}

		resp, err := ctx.client.Do(req)
		if err != nil {
			return "", fmt.Errorf("execute registry request: %w", err)
		}

		ok, err := ctx.handleResponse(resp)
		if err != nil {
			return "", err
		}

		if !ok {
			continue
		}

		defer resp.Body.Close()

		tmpFile, err := os.CreateTemp(c.cacheDir, "oci_*")
		if err != nil {
			return "", fmt.Errorf("create temp cache file: %w", err)
		}

		title := fmt.Sprintf("download %s", path)
		var writer io.Writer = tmpFile
		var bar *progressbar.ProgressBar

		if resp.ContentLength > 0 {
			bar = progressbar.DefaultBytes(resp.ContentLength, title)
		} else {
			bar = progressbar.DefaultBytes(-1, title)
		}

		if bar != nil {
			defer bar.Close()
			writer = io.MultiWriter(tmpFile, bar)
		}

		if _, err := io.Copy(writer, resp.Body); err != nil {
			tmpFile.Close()
			os.Remove(tmpFile.Name())
			return "", fmt.Errorf("write cache file: %w", err)
		}

		if err := tmpFile.Close(); err != nil {
			os.Remove(tmpFile.Name())
			return "", fmt.Errorf("close cache file: %w", err)
		}

		if err := os.Rename(tmpFile.Name(), cachePath); err != nil {
			os.Remove(tmpFile.Name())
			return "", fmt.Errorf("finalize cache file: %w", err)
		}

		ctx.logger.Debug("cached registry response", slog.String("cache", cachePath), slog.String("path", path))
		return cachePath, nil
	}

	return "", fmt.Errorf("failed to fetch %s after %d attempts", path, maxAttempts)
}

func (c *Client) readJSON(ctx *registryContext, path string, accept []string, out any) (string, error) {
	cachePath, err := c.fetchToCache(ctx, path, accept)
	if err != nil {
		return "", err
	}

	f, err := os.Open(cachePath)
	if err != nil {
		return "", fmt.Errorf("open cache file %s: %w", cachePath, err)
	}
	defer f.Close()

	if err := json.NewDecoder(f).Decode(out); err != nil {
		return "", fmt.Errorf("decode %s: %w", cachePath, err)
	}

	return cachePath, nil
}

// IsLocalTar checks if the image reference is a local tar file.
func IsLocalTar(imageRef string) bool {
	return (strings.HasPrefix(imageRef, "./") || strings.HasPrefix(imageRef, "../") || strings.HasPrefix(imageRef, "/")) && strings.HasSuffix(imageRef, ".tar")
}

// ParseImageRef parses an OCI image reference into registry, image, and tag.
func ParseImageRef(imageRef string) (registry string, image string, tag string, err error) {
	image, tag, ok := strings.Cut(imageRef, ":")
	if !ok {
		tag = "latest"
	}

	if strings.Contains(image, ".") {
		registry, image, ok = strings.Cut(image, "/")
		if !ok {
			return "", "", "", fmt.Errorf("invalid OCI image format %s", imageRef)
		}
	}

	if registry == "" {
		registry = defaultRegistry
	}

	if registry == "docker.io" {
		registry = defaultRegistry
	}

	if !strings.HasPrefix(registry, "http://") && !strings.HasPrefix(registry, "https://") {
		registry = "https://" + registry
	}

	if !strings.HasSuffix(registry, "/v2") {
		registry += "/v2"
	}

	if registry == defaultRegistry && !strings.Contains(image, "/") {
		image = "library/" + image
	}

	return registry, image, tag, nil
}
