package alpine

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"

	"j5.nz/cc/client"
)

const (
	defaultMirror  = "https://dl-cdn.alpinelinux.org"
	defaultVersion = "latest-stable"
	defaultRepo    = "main"
)

type Manager struct {
	root        string
	mirror      string
	version     string
	repo        string
	arch        string
	packageName string
	httpClient  *http.Client

	mu          sync.Mutex
	downloading bool
	lastErr     error
}

type metadata struct {
	Version     string `json:"version"`
	Source      string `json:"source"`
	PackageName string `json:"package_name"`
	PackageFile string `json:"package_file"`
	Arch        string `json:"arch"`
}

type indexEntry struct {
	Name    string
	Version string
	Arch    string
}

type Tristate int

const (
	TristateNo Tristate = iota
	TristateYes
	TristateModule
)

type Module struct {
	Name string
	Data []byte
}

func NewManager(root string) *Manager {
	return &Manager{
		root:        root,
		mirror:      defaultMirror,
		version:     defaultVersion,
		repo:        defaultRepo,
		arch:        defaultArch(),
		packageName: defaultPackageName(),
		httpClient:  http.DefaultClient,
	}
}

func (m *Manager) Status() client.KernelState {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.statusLocked()
}

func (m *Manager) PackagePath() (string, error) {
	meta, err := m.readMetadata()
	if err != nil {
		return "", err
	}
	return meta.PackageFile, nil
}

func (m *Manager) ReadKernel() ([]byte, error) {
	meta, err := m.readMetadata()
	if err != nil {
		return nil, err
	}

	f, err := os.Open(meta.PackageFile)
	if err != nil {
		return nil, fmt.Errorf("open kernel package: %w", err)
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("open kernel package gzip: %w", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	candidates := []string{"boot/vmlinuz-virt", "boot/vmlinuz-lts"}
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("kernel image not found in package")
		}
		if err != nil {
			return nil, fmt.Errorf("read kernel package tar: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		if !slices.Contains(candidates, hdr.Name) {
			continue
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			return nil, fmt.Errorf("read kernel image %q: %w", hdr.Name, err)
		}
		return data, nil
	}
}

func (m *Manager) KernelVersion() (string, error) {
	path, err := m.findModuleFile("modules.dep")
	if err != nil {
		return "", err
	}
	parts := strings.Split(path, "/")
	if len(parts) < 4 {
		return "", fmt.Errorf("unexpected modules.dep path %q", path)
	}
	return parts[len(parts)-2], nil
}

func (m *Manager) PlanModuleLoad(configVars []string, moduleMap map[string]string) ([]Module, error) {
	config, err := m.readKernelConfig()
	if err != nil {
		return nil, err
	}
	depends, prefix, err := m.readModuleDependencies()
	if err != nil {
		return nil, err
	}

	var planned []Module
	seen := map[string]bool{}
	var loadModule func(string) error
	loadModule = func(name string) error {
		if seen[name] {
			return nil
		}
		seen[name] = true

		for _, dep := range depends[name] {
			if err := loadModule(dep); err != nil {
				return err
			}
		}

		data, err := m.readModuleFile(prefix + name)
		if err != nil {
			return fmt.Errorf("read module %q: %w", name, err)
		}
		planned = append(planned, Module{Name: strings.TrimSuffix(filepath.Base(name), ".ko.gz"), Data: data})
		return nil
	}

	for _, configVar := range configVars {
		state, ok := config[configVar]
		if !ok {
			return nil, fmt.Errorf("kernel config %q not found", configVar)
		}
		switch state {
		case TristateYes:
		case TristateNo:
			return nil, fmt.Errorf("required kernel config %q is disabled", configVar)
		case TristateModule:
			moduleName, ok := moduleMap[configVar]
			if !ok {
				return nil, fmt.Errorf("no module mapping for %q", configVar)
			}
			if err := loadModule(moduleName); err != nil {
				return nil, err
			}
		}
	}

	return planned, nil
}

func (m *Manager) Ensure(ctx context.Context) error {
	m.mu.Lock()
	if m.downloading {
		m.mu.Unlock()
		return fmt.Errorf("kernel download already in progress")
	}
	m.downloading = true
	m.lastErr = nil
	m.mu.Unlock()

	err := m.ensureDownloaded(ctx)

	m.mu.Lock()
	m.downloading = false
	m.lastErr = err
	m.mu.Unlock()

	return err
}

func (m *Manager) statusLocked() client.KernelState {
	if m.downloading {
		return client.KernelState{
			Status: "downloading",
			Source: m.sourceName(),
		}
	}

	meta, err := m.readMetadata()
	if err == nil {
		return client.KernelState{
			Status:  "downloaded",
			Version: meta.Version,
			Source:  meta.Source,
		}
	}
	if m.lastErr != nil {
		return client.KernelState{
			Status: "error",
			Error:  m.lastErr.Error(),
			Source: m.sourceName(),
		}
	}
	return client.KernelState{
		Status: "missing",
		Source: m.sourceName(),
	}
}

func (m *Manager) ensureDownloaded(ctx context.Context) error {
	if err := os.MkdirAll(m.root, 0o755); err != nil {
		return fmt.Errorf("create kernel cache dir: %w", err)
	}

	entry, err := m.fetchIndexEntry(ctx)
	if err != nil {
		return err
	}

	filename := fmt.Sprintf("%s-%s.apk", entry.Name, entry.Version)
	destDir := filepath.Join(m.root, "packages")
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("create kernel package dir: %w", err)
	}
	destPath := filepath.Join(destDir, filename)

	if err := m.downloadFile(ctx, m.packageURL(filename), destPath); err != nil {
		return err
	}

	meta := metadata{
		Version:     entry.Version,
		Source:      m.sourceName(),
		PackageName: entry.Name,
		PackageFile: destPath,
		Arch:        entry.Arch,
	}

	buf, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal kernel metadata: %w", err)
	}
	if err := os.WriteFile(m.metadataPath(), buf, 0o644); err != nil {
		return fmt.Errorf("write kernel metadata: %w", err)
	}

	return nil
}

func (m *Manager) readKernelConfig() (map[string]Tristate, error) {
	packagePath, err := m.PackagePath()
	if err != nil {
		return nil, err
	}
	f, err := os.Open(packagePath)
	if err != nil {
		return nil, fmt.Errorf("open kernel package: %w", err)
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("open kernel package gzip: %w", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	config := map[string]Tristate{}
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read kernel package tar: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg || !strings.HasPrefix(hdr.Name, "boot/config-") {
			continue
		}
		scanner := bufio.NewScanner(tr)
		for scanner.Scan() {
			line := scanner.Text()
			switch {
			case strings.HasPrefix(line, "# CONFIG_") && strings.HasSuffix(line, " is not set"):
				key := strings.TrimSuffix(strings.TrimPrefix(line, "# "), " is not set")
				config[key] = TristateNo
			case strings.HasPrefix(line, "CONFIG_"):
				key, value, ok := strings.Cut(line, "=")
				if !ok {
					continue
				}
				switch value {
				case "y":
					config[key] = TristateYes
				case "m":
					config[key] = TristateModule
				}
			}
		}
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("scan kernel config: %w", err)
		}
		return config, nil
	}
	return nil, fmt.Errorf("kernel config not found in package")
}

func (m *Manager) readModuleDependencies() (map[string][]string, string, error) {
	path, err := m.findModuleFile("modules.dep")
	if err != nil {
		return nil, "", err
	}
	data, err := m.readRawPackageFile(path)
	if err != nil {
		return nil, "", err
	}
	prefix := strings.TrimSuffix(path, "modules.dep")
	depends := map[string][]string{}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		moduleName, depsRaw, ok := strings.Cut(line, ":")
		if !ok {
			return nil, "", fmt.Errorf("invalid modules.dep line %q", line)
		}
		moduleName = strings.TrimSpace(moduleName)
		for _, dep := range strings.Fields(strings.TrimSpace(depsRaw)) {
			depends[moduleName] = append(depends[moduleName], dep)
		}
		if _, ok := depends[moduleName]; !ok {
			depends[moduleName] = nil
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, "", fmt.Errorf("scan modules.dep: %w", err)
	}
	return depends, prefix, nil
}

func (m *Manager) findModuleFile(suffix string) (string, error) {
	packagePath, err := m.PackagePath()
	if err != nil {
		return "", err
	}
	f, err := os.Open(packagePath)
	if err != nil {
		return "", fmt.Errorf("open kernel package: %w", err)
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return "", fmt.Errorf("open kernel package gzip: %w", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return "", fmt.Errorf("module file with suffix %q not found", suffix)
		}
		if err != nil {
			return "", fmt.Errorf("read kernel package tar: %w", err)
		}
		if hdr.Typeflag == tar.TypeReg && strings.HasSuffix(hdr.Name, suffix) {
			return hdr.Name, nil
		}
	}
}

func (m *Manager) readModuleFile(path string) ([]byte, error) {
	data, err := m.readRawPackageFile(path)
	if err != nil {
		return nil, err
	}
	if strings.HasSuffix(path, ".gz") {
		gzr, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("open module gzip %q: %w", path, err)
		}
		defer gzr.Close()
		data, err = io.ReadAll(gzr)
		if err != nil {
			return nil, fmt.Errorf("read module %q: %w", path, err)
		}
	}
	return data, nil
}

func (m *Manager) readRawPackageFile(path string) ([]byte, error) {
	packagePath, err := m.PackagePath()
	if err != nil {
		return nil, err
	}
	f, err := os.Open(packagePath)
	if err != nil {
		return nil, fmt.Errorf("open kernel package: %w", err)
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("open kernel package gzip: %w", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("package file %q not found", path)
		}
		if err != nil {
			return nil, fmt.Errorf("read kernel package tar: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg || hdr.Name != path {
			continue
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			return nil, fmt.Errorf("read package file %q: %w", path, err)
		}
		return data, nil
	}
}

func (m *Manager) fetchIndexEntry(ctx context.Context) (indexEntry, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.indexURL(), nil)
	if err != nil {
		return indexEntry{}, err
	}
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return indexEntry{}, fmt.Errorf("download kernel index: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return indexEntry{}, fmt.Errorf("download kernel index: status %s", resp.Status)
	}

	indexData, err := readAPKIndex(resp.Body)
	if err != nil {
		return indexEntry{}, err
	}

	entry, ok := indexData[m.packageName]
	if !ok {
		return indexEntry{}, fmt.Errorf("kernel package %q not found in APKINDEX", m.packageName)
	}
	if entry.Arch != m.arch {
		return indexEntry{}, fmt.Errorf("kernel package arch %q does not match expected %q", entry.Arch, m.arch)
	}
	return entry, nil
}

func (m *Manager) downloadFile(ctx context.Context, url, destPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("download kernel package: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download kernel package: status %s", resp.Status)
	}

	tmpPath := destPath + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("create kernel package file: %w", err)
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write kernel package: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close kernel package: %w", err)
	}
	if err := os.Rename(tmpPath, destPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("finalize kernel package: %w", err)
	}
	return nil
}

func readAPKIndex(r io.Reader) (map[string]indexEntry, error) {
	gzr, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("open APKINDEX gzip: %w", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("APKINDEX entry not found")
		}
		if err != nil {
			return nil, fmt.Errorf("read APKINDEX tar: %w", err)
		}
		if hdr.Name != "APKINDEX" {
			continue
		}
		return parseAPKIndex(tr)
	}
}

func parseAPKIndex(r io.Reader) (map[string]indexEntry, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	out := map[string]indexEntry{}
	var current indexEntry

	flush := func() {
		if current.Name != "" {
			out[current.Name] = current
		}
		current = indexEntry{}
	}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			flush()
			continue
		}
		if len(line) < 3 || line[1] != ':' {
			return nil, fmt.Errorf("invalid APKINDEX line %q", line)
		}

		key := line[:1]
		value := line[2:]

		switch key {
		case "P":
			current.Name = value
		case "V":
			current.Version = value
		case "A":
			current.Arch = value
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan APKINDEX: %w", err)
	}
	flush()
	return out, nil
}

func (m *Manager) readMetadata() (metadata, error) {
	var ret metadata
	buf, err := os.ReadFile(m.metadataPath())
	if err != nil {
		return ret, err
	}
	if err := json.Unmarshal(buf, &ret); err != nil {
		return ret, fmt.Errorf("decode kernel metadata: %w", err)
	}
	if ret.Version == "" || ret.Source == "" || ret.PackageFile == "" {
		return ret, errors.New("kernel metadata is incomplete")
	}
	return ret, nil
}

func (m *Manager) metadataPath() string {
	return filepath.Join(m.root, "kernel.json")
}

func (m *Manager) sourceName() string {
	return "alpine:" + m.version
}

func (m *Manager) indexURL() string {
	return strings.TrimRight(m.mirror, "/") + "/" + m.version + "/" + m.repo + "/" + m.arch + "/APKINDEX.tar.gz"
}

func (m *Manager) packageURL(filename string) string {
	return strings.TrimRight(m.mirror, "/") + "/" + m.version + "/" + m.repo + "/" + m.arch + "/" + filename
}

func defaultArch() string {
	switch runtime.GOARCH {
	case "arm64":
		return "aarch64"
	case "amd64":
		return "x86_64"
	default:
		return runtime.GOARCH
	}
}

func defaultPackageName() string {
	if defaultArch() == "riscv64" {
		return "linux-lts"
	}
	return "linux-virt"
}
