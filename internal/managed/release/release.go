package release

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"j5.nz/cc/internal/download"
)

type ReaderFactory func(io.Reader) (io.ReadCloser, error)

type Artifact struct {
	CacheDir        string
	Family          string
	Version         string
	Arch            string
	Mirror          string
	Name            string
	URLPath         string
	LocalCandidates []string
}

func FirstExisting(paths ...string) string {
	for _, candidate := range paths {
		if st, err := os.Stat(candidate); err == nil && st.Size() > 0 {
			return candidate
		}
	}
	return ""
}

func EnsureArtifact(ctx context.Context, artifact Artifact) (string, error) {
	if local := FirstExisting(artifact.LocalCandidates...); local != "" {
		return local, nil
	}
	if artifact.CacheDir == "" {
		return "", fmt.Errorf("artifact cache dir is required")
	}
	if artifact.Family == "" {
		return "", fmt.Errorf("artifact family is required")
	}
	if artifact.Version == "" {
		return "", fmt.Errorf("artifact version is required")
	}
	if artifact.Arch == "" {
		return "", fmt.Errorf("artifact arch is required")
	}
	if artifact.Name == "" {
		return "", fmt.Errorf("artifact name is required")
	}
	dir := filepath.Join(artifact.CacheDir, artifact.Family, artifact.Version, artifact.Arch)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create %s cache dir: %w", artifact.Family, err)
	}
	target := filepath.Join(dir, artifact.Name)
	if st, err := os.Stat(target); err == nil && st.Size() > 0 {
		return target, nil
	}
	if artifact.Mirror == "" {
		return "", fmt.Errorf("artifact mirror is required")
	}
	urlPath := strings.TrimLeft(artifact.URLPath, "/")
	if urlPath == "" {
		urlPath = artifact.Name
	}
	url := strings.TrimRight(artifact.Mirror, "/") + "/" + urlPath
	if err := Download(ctx, url, target); err != nil {
		return "", err
	}
	return target, nil
}

func EnsureDecompressed(ctx context.Context, source, target string, open ReaderFactory) (string, error) {
	if target == "" {
		target = strings.TrimSuffix(source, filepath.Ext(source)) + ".tar"
	}
	if st, err := os.Stat(target); err == nil && st.Size() > 0 {
		if src, srcErr := os.Stat(source); srcErr == nil && !st.ModTime().Before(src.ModTime()) {
			return target, nil
		}
	}
	f, err := os.Open(source)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", source, err)
	}
	defer f.Close()
	r, err := open(f)
	if err != nil {
		return "", err
	}
	defer r.Close()
	if err := writeFileAtomically(ctx, target, r); err != nil {
		return "", err
	}
	return target, nil
}

func Download(ctx context.Context, url, target string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create download request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: unexpected HTTP status %s", url, resp.Status)
	}
	if resp.ContentLength <= 0 {
		return fmt.Errorf("download %s: server did not provide a positive content length", url)
	}
	return writeResponseAtomically(ctx, target, resp)
}

func writeResponseAtomically(ctx context.Context, target string, resp *http.Response) error {
	tmp := target + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("create %s: %w", tmp, err)
	}
	_, copyErr := download.Copy(ctx, out, resp, download.Budget{MaxBytes: resp.ContentLength, ExpectedBytes: resp.ContentLength})
	closeErr := out.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(tmp)
		return errors.Join(copyErr, closeErr)
	}
	return os.Rename(tmp, target)
}

func writeFileAtomically(ctx context.Context, target string, r io.Reader) error {
	tmp := target + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("create %s: %w", tmp, err)
	}
	maxBytes, budgetErr := download.FilesystemBudget(target)
	if budgetErr != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("determine expanded artifact budget: %w", budgetErr)
	}
	_, copyErr := download.CopyReader(ctx, out, r, maxBytes)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("write %s: %w", tmp, copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("close %s: %w", tmp, closeErr)
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("install %s: %w", target, err)
	}
	return nil
}
