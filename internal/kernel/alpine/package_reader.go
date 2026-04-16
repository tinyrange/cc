package alpine

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

func (m *Manager) ReadPackageFile(ctx context.Context, repo, packageName, innerPath string) ([]byte, error) {
	entry, err := m.lookupPackageEntry(ctx, repo, packageName)
	if err != nil {
		return nil, err
	}
	filename := fmt.Sprintf("%s-%s.apk", entry.Name, entry.Version)
	destDir := filepath.Join(m.root, "packages", repo)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return nil, fmt.Errorf("create package dir: %w", err)
	}
	destPath := filepath.Join(destDir, filename)
	if _, err := os.Stat(destPath); err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("stat package file: %w", err)
		}
		if err := m.downloadFile(ctx, m.repoPackageURL(repo, filename), destPath); err != nil {
			return nil, fmt.Errorf("download package %q: %w", packageName, err)
		}
	}

	f, err := os.Open(destPath)
	if err != nil {
		return nil, fmt.Errorf("open package file: %w", err)
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("open package gzip: %w", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("package file %q not found in %s", innerPath, packageName)
		}
		if err != nil {
			return nil, fmt.Errorf("read package tar: %w", err)
		}
		if hdr.Name != innerPath {
			continue
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			return nil, fmt.Errorf("read package file %q: %w", innerPath, err)
		}
		return data, nil
	}
}

func (m *Manager) lookupPackageEntry(ctx context.Context, repo, packageName string) (indexEntry, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.repoIndexURL(repo), nil)
	if err != nil {
		return indexEntry{}, err
	}
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return indexEntry{}, fmt.Errorf("request APKINDEX: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return indexEntry{}, fmt.Errorf("request APKINDEX: status %s", resp.Status)
	}
	indexData, err := readAPKIndex(resp.Body)
	if err != nil {
		return indexEntry{}, err
	}
	entry, ok := indexData[packageName]
	if !ok {
		return indexEntry{}, fmt.Errorf("package %q not found in APKINDEX", packageName)
	}
	if entry.Arch != m.arch {
		return indexEntry{}, fmt.Errorf("package arch %q does not match expected %q", entry.Arch, m.arch)
	}
	return entry, nil
}

func (m *Manager) repoIndexURL(repo string) string {
	return stringsTrimRightSlash(m.mirror) + "/" + m.version + "/" + repo + "/" + m.arch + "/APKINDEX.tar.gz"
}

func (m *Manager) repoPackageURL(repo, filename string) string {
	return stringsTrimRightSlash(m.mirror) + "/" + m.version + "/" + repo + "/" + m.arch + "/" + filename
}

func stringsTrimRightSlash(value string) string {
	for len(value) > 0 && value[len(value)-1] == '/' {
		value = value[:len(value)-1]
	}
	return value
}
