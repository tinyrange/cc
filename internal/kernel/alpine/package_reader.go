package alpine

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

func (m *Manager) ReadPackageFile(ctx context.Context, repo, packageName, innerPath string) ([]byte, error) {
	cacheKey := repo + "\x00" + packageName + "\x00" + innerPath
	m.mu.Lock()
	if data, ok := m.packageFiles[cacheKey]; ok {
		cached := append([]byte(nil), data...)
		m.mu.Unlock()
		return cached, nil
	}
	m.mu.Unlock()

	entry, err := m.lookupPackageEntry(ctx, repo, packageName)
	if err != nil {
		return nil, err
	}
	filename := fmt.Sprintf("%s-%s.apk", entry.Name, entry.Version)
	destDir := filepath.Join(m.root, "packages", repo)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return nil, fmt.Errorf("create package dir: %w", err)
	}
	apkPath := filepath.Join(destDir, filename)
	if _, err := os.Stat(apkPath); err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("stat package file: %w", err)
		}
		if err := m.downloadFile(ctx, m.repoPackageURL(repo, filename), apkPath); err != nil {
			return nil, fmt.Errorf("download package %q: %w", packageName, err)
		}
	}
	tarPath := filepath.Join(destDir, fmt.Sprintf("%s-%s.tar", entry.Name, entry.Version))
	if _, err := os.Stat(tarPath); err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("stat package tar file: %w", err)
		}
		if err := decompressAPKToTar(apkPath, tarPath); err != nil {
			return nil, err
		}
	}
	index, err := m.cachedTarIndex(tarPath)
	if err != nil {
		return nil, err
	}
	entryInfo, ok := index[innerPath]
	if !ok {
		return nil, fmt.Errorf("package file %q not found in %s", innerPath, packageName)
	}
	f, err := os.Open(tarPath)
	if err != nil {
		return nil, fmt.Errorf("open package tar file: %w", err)
	}
	defer f.Close()
	data := make([]byte, entryInfo.Size)
	n, err := f.ReadAt(data, entryInfo.Offset)
	if err != nil && n != len(data) {
		return nil, fmt.Errorf("read package file %q: %w", innerPath, err)
	}
	if n != len(data) {
		return nil, fmt.Errorf("read package file %q: short read %d/%d", innerPath, n, len(data))
	}
	m.mu.Lock()
	m.packageFiles[cacheKey] = append([]byte(nil), data...)
	cached := append([]byte(nil), m.packageFiles[cacheKey]...)
	m.mu.Unlock()
	return cached, nil
}

func (m *Manager) ExtractPackageFile(ctx context.Context, repo, packageName, innerPath string) (string, error) {
	entry, err := m.lookupPackageEntry(ctx, repo, packageName)
	if err != nil {
		return "", err
	}
	filename := fmt.Sprintf("%s-%s.apk", entry.Name, entry.Version)
	destDir := filepath.Join(m.root, "packages", repo)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", fmt.Errorf("create package dir: %w", err)
	}
	apkPath := filepath.Join(destDir, filename)
	if _, err := os.Stat(apkPath); err != nil {
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("stat package file: %w", err)
		}
		if err := m.downloadFile(ctx, m.repoPackageURL(repo, filename), apkPath); err != nil {
			return "", fmt.Errorf("download package %q: %w", packageName, err)
		}
	}
	tarPath := filepath.Join(destDir, fmt.Sprintf("%s-%s.tar", entry.Name, entry.Version))
	if _, err := os.Stat(tarPath); err != nil {
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("stat package tar file: %w", err)
		}
		if err := decompressAPKToTar(apkPath, tarPath); err != nil {
			return "", err
		}
	}
	index, err := m.cachedTarIndex(tarPath)
	if err != nil {
		return "", err
	}
	entryInfo, ok := index[innerPath]
	if !ok {
		return "", fmt.Errorf("package file %q not found in %s", innerPath, packageName)
	}
	outPath := filepath.Join(m.root, "extracted", repo, entry.Name+"-"+entry.Version, filepath.FromSlash(innerPath))
	if info, err := os.Stat(outPath); err == nil && info.Mode().IsRegular() && info.Size() == entryInfo.Size {
		return outPath, nil
	} else if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("stat extracted package file: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return "", fmt.Errorf("create extracted package dir: %w", err)
	}
	src, err := os.Open(tarPath)
	if err != nil {
		return "", fmt.Errorf("open package tar file: %w", err)
	}
	defer src.Close()
	if _, err := src.Seek(entryInfo.Offset, 0); err != nil {
		return "", fmt.Errorf("seek package tar file %q: %w", innerPath, err)
	}
	tmpPath := outPath + ".tmp"
	dst, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(entryInfo.Mode))
	if err != nil {
		return "", fmt.Errorf("create extracted package file: %w", err)
	}
	if _, err := io.CopyN(dst, src, entryInfo.Size); err != nil {
		_ = dst.Close()
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("extract package file %q: %w", innerPath, err)
	}
	if err := dst.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("close extracted package file %q: %w", innerPath, err)
	}
	if err := os.Rename(tmpPath, outPath); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("finalize extracted package file %q: %w", innerPath, err)
	}
	return outPath, nil
}

func (m *Manager) lookupPackageEntry(ctx context.Context, repo, packageName string) (indexEntry, error) {
	m.mu.Lock()
	if repoIndex, ok := m.repoIndexes[repo]; ok {
		if entry, ok := repoIndex[packageName]; ok {
			m.mu.Unlock()
			return entry, nil
		}
	}
	m.mu.Unlock()

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
	m.mu.Lock()
	m.repoIndexes[repo] = indexData
	m.mu.Unlock()
	return entry, nil
}

func (m *Manager) cachedTarIndex(tarPath string) (map[string]tarIndexEntry, error) {
	m.mu.Lock()
	if index, ok := m.tarIndexes[tarPath]; ok {
		m.mu.Unlock()
		return index, nil
	}
	m.mu.Unlock()

	index, err := buildTarIndex(tarPath)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	m.tarIndexes[tarPath] = index
	m.mu.Unlock()
	return index, nil
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
