package oci

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"j5.nz/cc/client"
	intcvmfs "j5.nz/cc/internal/cvmfs"
	"j5.nz/cc/internal/fsmeta"
	"j5.nz/cc/internal/imagefs"
	"j5.nz/cc/internal/simg"
)

const defaultRegistry = "https://registry-1.docker.io/v2"
const sharedCacheEnv = "CCX3_OCI_SHARED_CACHE_DIR"
const sharedCacheSchemaVersion = "3"

const (
	SourceKindOCI           = "oci"
	SourceKindSIMG          = "simg"
	SourceKindCVMFS         = "cvmfs"
	SourceKindDockerArchive = "docker-archive"
)

type Store struct {
	root          string
	httpClient    *http.Client
	CVMFSActivity func(int)

	mu          sync.Mutex
	downloading map[string]bool
	lastErr     map[string]error
}

type metadata struct {
	Name               string      `json:"name"`
	Source             string      `json:"source"`
	SourceKind         string      `json:"source_kind,omitempty"`
	CVMFSRootHash      string      `json:"cvmfs_root_hash,omitempty"`
	CVMFSMirrors       []string    `json:"cvmfs_mirrors,omitempty"`
	Architecture       string      `json:"architecture,omitempty"`
	RootFSDir          string      `json:"rootfs_dir"`
	MetadataPath       string      `json:"metadata_path,omitempty"`
	IndexPath          string      `json:"index_path,omitempty"`
	PackedContentsPath string      `json:"packed_contents_path,omitempty"`
	Env                []string    `json:"env,omitempty"`
	Entrypoint         []string    `json:"entrypoint,omitempty"`
	Cmd                []string    `json:"cmd,omitempty"`
	WorkingDir         string      `json:"working_dir,omitempty"`
	User               string      `json:"user,omitempty"`
	Labels             []labelPair `json:"labels,omitempty"`
}

type labelPair struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type Image struct {
	Name         string
	Source       string
	SourceKind   string
	Architecture string
	RootFSDir    string
	FSMetadata   map[string]fsmeta.Entry
	RootFS       imagefs.Directory
	Config       RuntimeConfig
}

type SourceSpec struct {
	Kind string
	Raw  string
}

type PullOptions struct {
	Prefetch        bool
	PrefetchWorkers int
	CVMFSMirrors    []string
	Report          func(client.ProgressEvent)
}

func reportPullProgress(report func(client.ProgressEvent), event client.ProgressEvent) {
	if report == nil {
		return
	}
	report(event)
}

func normalizedMirrors(mirrors []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(mirrors))
	for _, mirror := range mirrors {
		mirror = strings.TrimRight(strings.TrimSpace(mirror), "/")
		if mirror == "" || seen[mirror] {
			continue
		}
		seen[mirror] = true
		out = append(out, mirror)
	}
	return out
}

type RuntimeConfig struct {
	Env        []string
	Entrypoint []string
	Cmd        []string
	WorkingDir string
	User       string
	Labels     map[string]string
}

type manifestList struct {
	SchemaVersion int             `json:"schemaVersion"`
	MediaType     string          `json:"mediaType"`
	Manifests     []manifestEntry `json:"manifests"`
}

type manifestEntry struct {
	MediaType string   `json:"mediaType"`
	Digest    string   `json:"digest"`
	Platform  platform `json:"platform"`
}

type platform struct {
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
	Variant      string `json:"variant,omitempty"`
}

type manifest struct {
	SchemaVersion int          `json:"schemaVersion"`
	MediaType     string       `json:"mediaType"`
	Config        descriptor   `json:"config"`
	Layers        []descriptor `json:"layers"`
}

type descriptor struct {
	MediaType string `json:"mediaType"`
	Digest    string `json:"digest"`
	Size      int64  `json:"size,omitempty"`
}

type imageConfig struct {
	Architecture string `json:"architecture"`
	Config       struct {
		Env        []string          `json:"Env"`
		Entrypoint stringSlice       `json:"Entrypoint"`
		Cmd        []string          `json:"Cmd"`
		WorkingDir string            `json:"WorkingDir"`
		User       string            `json:"User"`
		Labels     map[string]string `json:"Labels"`
	} `json:"config"`
}

type stringSlice []string

func (s *stringSlice) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	switch trimmed {
	case "", "null":
		*s = nil
		return nil
	}
	if strings.HasPrefix(trimmed, "[") {
		var arr []string
		if err := json.Unmarshal(data, &arr); err != nil {
			return err
		}
		*s = arr
		return nil
	}
	var single string
	if err := json.Unmarshal(data, &single); err != nil {
		return err
	}
	*s = []string{single}
	return nil
}

type registryContext struct {
	client   *http.Client
	registry string
	token    string
}

type tokenResponse struct {
	Token       string `json:"token"`
	AccessToken string `json:"access_token"`
}

func NewStore(root string) *Store {
	return &Store{
		root:        root,
		httpClient:  http.DefaultClient,
		downloading: map[string]bool{},
		lastErr:     map[string]error{},
	}
}

func (s *Store) Root() string {
	return s.root
}

func (s *Store) List() ([]client.ImageState, error) {
	if err := os.MkdirAll(s.root, 0o755); err != nil {
		return nil, fmt.Errorf("create image store: %w", err)
	}
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return nil, fmt.Errorf("read image store: %w", err)
	}
	ret := make([]client.ImageState, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		state, err := s.Get(entry.Name())
		if err != nil {
			continue
		}
		ret = append(ret, state)
	}
	sort.Slice(ret, func(i, j int) bool { return ret[i].Name < ret[j].Name })
	return ret, nil
}

func (s *Store) Get(name string) (client.ImageState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getLocked(name)
}

func (s *Store) Open(name string) (*Image, error) {
	meta, err := s.readMetadata(name)
	if err != nil {
		return nil, err
	}
	var entries map[string]fsmeta.Entry
	if meta.MetadataPath != "" {
		buf, err := os.ReadFile(meta.MetadataPath)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("read fs metadata: %w", err)
		}
		if len(buf) > 0 {
			if err := json.Unmarshal(buf, &entries); err != nil {
				return nil, fmt.Errorf("decode fs metadata: %w", err)
			}
		}
	}
	rootFS := imagefs.NewHostFS(meta.RootFSDir, entries)
	if meta.IndexPath != "" {
		indexBuf, err := os.ReadFile(meta.IndexPath)
		if err != nil {
			return nil, fmt.Errorf("read fs index: %w", err)
		}
		index, err := decodeFSIndex(indexBuf)
		if err != nil {
			return nil, fmt.Errorf("decode fs index: %w", err)
		}
		if meta.SourceKind == SourceKindCVMFS {
			cvmfsClient := &intcvmfs.Client{
				HTTPClient: s.httpClient,
				CacheDir:   cvmfsCacheDir(s.sharedRoot()),
				OnActivity: s.cvmfsActivity,
				Mirrors:    meta.CVMFSMirrors,
			}
			rootFS, err = buildCVMFSIndexedRootFS(cvmfsClient, meta.PackedContentsPath, index)
			if err != nil {
				return nil, fmt.Errorf("build cvmfs rootfs: %w", err)
			}
		} else {
			rootFS, err = buildIndexedRootFS(meta.RootFSDir, index)
			if err != nil {
				return nil, fmt.Errorf("build indexed rootfs: %w", err)
			}
		}
		return &Image{
			Name:         meta.Name,
			Source:       meta.Source,
			SourceKind:   meta.SourceKind,
			Architecture: meta.Architecture,
			RootFSDir:    meta.RootFSDir,
			FSMetadata:   entries,
			RootFS:       rootFS,
			Config: RuntimeConfig{
				Env:        append([]string(nil), meta.Env...),
				Entrypoint: append([]string(nil), meta.Entrypoint...),
				Cmd:        append([]string(nil), meta.Cmd...),
				WorkingDir: meta.WorkingDir,
				User:       meta.User,
				Labels:     labelsFromPairs(meta.Labels),
			},
		}, nil
	}
	if meta.SourceKind == SourceKindSIMG {
		rootFS, entries, arch, err := simg.BuildImageFS(filepath.Join(meta.RootFSDir, "rootfs.simg"))
		if err != nil {
			return nil, fmt.Errorf("build simg rootfs: %w", err)
		}
		return &Image{
			Name:         meta.Name,
			Source:       meta.Source,
			SourceKind:   meta.SourceKind,
			Architecture: firstNonEmpty(meta.Architecture, arch),
			RootFSDir:    meta.RootFSDir,
			FSMetadata:   entries,
			RootFS:       rootFS,
			Config: RuntimeConfig{
				Env:        append([]string(nil), meta.Env...),
				Entrypoint: append([]string(nil), meta.Entrypoint...),
				Cmd:        append([]string(nil), meta.Cmd...),
				WorkingDir: meta.WorkingDir,
				User:       meta.User,
				Labels:     labelsFromPairs(meta.Labels),
			},
		}, nil
	}
	return &Image{
		Name:         meta.Name,
		Source:       meta.Source,
		SourceKind:   meta.SourceKind,
		Architecture: meta.Architecture,
		RootFSDir:    meta.RootFSDir,
		FSMetadata:   entries,
		RootFS:       rootFS,
		Config: RuntimeConfig{
			Env:        append([]string(nil), meta.Env...),
			Entrypoint: append([]string(nil), meta.Entrypoint...),
			Cmd:        append([]string(nil), meta.Cmd...),
			WorkingDir: meta.WorkingDir,
			User:       meta.User,
			Labels:     labelsFromPairs(meta.Labels),
		},
	}, nil
}

func (s *Store) Pull(ctx context.Context, name, source string, options ...PullOptions) (client.ImageState, error) {
	if name == "" {
		return client.ImageState{}, fmt.Errorf("image name is required")
	}
	if source == "" {
		return client.ImageState{}, fmt.Errorf("image source is required")
	}
	spec, err := ParseSource(source)
	if err != nil {
		return client.ImageState{}, err
	}
	var opts PullOptions
	if len(options) > 0 {
		opts = options[0]
	}
	if state, ok, err := s.existingState(name, spec); err != nil {
		return client.ImageState{}, err
	} else if ok {
		reportPullProgress(opts.Report, client.ProgressEvent{Status: "available", Artifact: name})
		if err := s.maybePrefetchCVMFSImage(ctx, name, spec, opts); err != nil {
			return client.ImageState{}, err
		}
		reportPullProgress(opts.Report, client.ProgressEvent{Status: "downloaded", Artifact: name})
		return state, nil
	}
	if state, ok, err := s.restoreFromSharedCache(name, spec); err != nil {
		return client.ImageState{}, err
	} else if ok {
		reportPullProgress(opts.Report, client.ProgressEvent{Status: "restored", Artifact: name})
		if err := s.maybePrefetchCVMFSImage(ctx, name, spec, opts); err != nil {
			return client.ImageState{}, err
		}
		reportPullProgress(opts.Report, client.ProgressEvent{Status: "downloaded", Artifact: name})
		return state, nil
	}

	s.mu.Lock()
	if s.downloading[name] {
		s.mu.Unlock()
		return client.ImageState{}, fmt.Errorf("image %q download already in progress", name)
	}
	s.downloading[name] = true
	delete(s.lastErr, name)
	s.mu.Unlock()

	err = s.pull(ctx, name, spec, opts)

	s.mu.Lock()
	delete(s.downloading, name)
	s.lastErr[name] = err
	state, stateErr := s.getLocked(name)
	s.mu.Unlock()

	if err != nil {
		return client.ImageState{}, err
	}
	return state, stateErr
}

func (s *Store) pull(ctx context.Context, name string, spec SourceSpec, options PullOptions) error {
	if err := os.MkdirAll(s.root, 0o755); err != nil {
		return fmt.Errorf("create image store: %w", err)
	}
	if err := os.MkdirAll(s.sharedRoot(), 0o755); err != nil {
		return fmt.Errorf("create shared image store: %w", err)
	}

	sharedName := sharedImageKey(spec)
	shared := NewStore(s.sharedRoot())
	shared.httpClient = s.httpClient
	if _, ok, err := shared.existingState(sharedName, spec); err != nil {
		return err
	} else if !ok {
		if err := shared.pullDirect(ctx, sharedName, spec, options); err != nil {
			return err
		}
	}
	return s.cloneFromStore(shared, sharedName, name, spec)
}

func (s *Store) pullDirect(ctx context.Context, name string, spec SourceSpec, options PullOptions) error {
	switch spec.Kind {
	case SourceKindOCI:
		return s.pullOCIDirect(ctx, name, spec)
	case SourceKindSIMG:
		return s.pullSIMGDirect(ctx, name, spec)
	case SourceKindCVMFS:
		return s.pullCVMFSDirect(ctx, name, spec, options)
	case SourceKindDockerArchive:
		return s.pullDockerArchiveDirect(ctx, name, spec)
	default:
		return fmt.Errorf("unsupported image source kind %q", spec.Kind)
	}
}

func (s *Store) pullSIMGDirect(ctx context.Context, name string, spec SourceSpec) error {
	imageDir := s.imageDir(name)
	tmpDir := imageDir + ".tmp"
	if err := os.RemoveAll(tmpDir); err != nil {
		return fmt.Errorf("remove temp image dir: %w", err)
	}
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return fmt.Errorf("create temp image dir: %w", err)
	}
	simgPath := filepath.Join(tmpDir, "rootfs.simg")
	if err := s.fetchSIMG(ctx, spec.Raw, simgPath); err != nil {
		return err
	}
	return s.finalizeSIMGImage(name, spec, imageDir, tmpDir, simgPath)
}

func (s *Store) pullCVMFSDirect(ctx context.Context, name string, spec SourceSpec, options PullOptions) error {
	reportPullProgress(options.Report, client.ProgressEvent{Status: "resolving", Artifact: name, Blob: "cvmfs"})
	imageDir := s.imageDir(name)
	tmpDir := imageDir + ".tmp"
	if err := os.RemoveAll(tmpDir); err != nil {
		return fmt.Errorf("remove temp image dir: %w", err)
	}
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return fmt.Errorf("create temp image dir: %w", err)
	}
	cvmfsClient := &intcvmfs.Client{
		HTTPClient: s.httpClient,
		CacheDir:   cvmfsCacheDir(s.sharedRoot()),
		OnActivity: s.cvmfsActivity,
		Mirrors:    options.CVMFSMirrors,
	}
	normalizedSource := normalizeCVMFSSource(spec.Raw)
	rootTarget, isDir, err := resolveCVMFSRootTarget(cvmfsClient, normalizedSource)
	if err != nil {
		return err
	}
	if !isDir {
		return fmt.Errorf("resolve cvmfs container root: %q is not a container directory", spec.Raw)
	}
	rootHash, err := cvmfsClient.ManifestRootHash(normalizedSource)
	if err != nil {
		return fmt.Errorf("read cvmfs manifest root hash: %w", err)
	}
	reportPullProgress(options.Report, client.ProgressEvent{Status: "indexing", Artifact: name, Blob: rootHash})
	nodes, entries, arch, ok, err := loadCVMFSDirectoryIndexCache(cvmfsClient.CacheDir, rootHash, rootTarget)
	if err != nil {
		return fmt.Errorf("load cached cvmfs rootfs index: %w", err)
	}
	if !ok {
		nodes, entries, arch, err = buildCVMFSDirectoryIndex(cvmfsClient, rootTarget)
		if err != nil {
			return fmt.Errorf("index cvmfs rootfs: %w", err)
		}
		if err := saveCVMFSDirectoryIndexCache(cvmfsClient.CacheDir, rootHash, rootTarget, nodes, entries, arch); err != nil {
			return fmt.Errorf("cache cvmfs rootfs index: %w", err)
		}
	}
	if options.Prefetch {
		cachedNodes, err := prefetchCVMFSFiles(ctx, cvmfsClient, nodes, options.PrefetchWorkers, name, options.Report)
		if err != nil {
			return fmt.Errorf("prefetch cvmfs rootfs: %w", err)
		}
		nodes = cachedNodes
	}
	fsMetaBuf, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal fs metadata: %w", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "rootfs.metadata.json"), fsMetaBuf, 0o644); err != nil {
		return fmt.Errorf("write fs metadata: %w", err)
	}
	indexBuf, err := encodeIndexedNodes(nodes)
	if err != nil {
		return fmt.Errorf("marshal fs index: %w", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "rootfs.index.json"), indexBuf, 0o644); err != nil {
		return fmt.Errorf("write fs index: %w", err)
	}
	deployMetadata, err := extractCVMFSDeployMetadata(cvmfsClient, "", nodes)
	if err != nil {
		return fmt.Errorf("extract cvmfs deploy metadata: %w", err)
	}
	meta := metadata{
		Name:          name,
		Source:        spec.Raw,
		SourceKind:    spec.Kind,
		CVMFSRootHash: rootHash,
		CVMFSMirrors:  normalizedMirrors(options.CVMFSMirrors),
		Architecture:  arch,
		RootFSDir:     imageDir,
		MetadataPath:  filepath.Join(imageDir, "rootfs.metadata.json"),
		IndexPath:     filepath.Join(imageDir, "rootfs.index.json"),
		Env:           deployMetadata.Env,
	}
	if err := os.RemoveAll(imageDir); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove old image dir: %w", err)
	}
	if err := os.Rename(tmpDir, imageDir); err != nil {
		return fmt.Errorf("activate image dir: %w", err)
	}
	if err := s.writeMetadata(name, meta); err != nil {
		return err
	}
	reportPullProgress(options.Report, client.ProgressEvent{Status: "downloaded", Artifact: name})
	return nil
}

func extractCVMFSDeployMetadata(client *intcvmfs.Client, packedPath string, nodes []indexedNode) (simgDeployMetadata, error) {
	var envTexts []string
	var buildYAML string
	sorted := append([]indexedNode(nil), nodes...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })
	for _, node := range sorted {
		if node.Kind != indexedKindFile {
			continue
		}
		isEnvFile := strings.HasPrefix(node.Path, "/.singularity.d/env/") && strings.HasSuffix(node.Path, ".sh")
		if !isEnvFile && node.Path != "/build.yaml" {
			continue
		}
		text, err := readCVMFSIndexedNodeText(client, packedPath, node)
		if err != nil {
			return simgDeployMetadata{}, err
		}
		if isEnvFile {
			envTexts = append(envTexts, text)
			continue
		}
		buildYAML = text
	}
	return extractDeployMetadataTexts(envTexts, buildYAML), nil
}

func readCVMFSIndexedNodeText(client *intcvmfs.Client, packedPath string, node indexedNode) (string, error) {
	if node.Size > maxSIMGMetadataFileSize {
		node.Size = maxSIMGMetadataFileSize
	}
	if node.Packed {
		if packedPath == "" {
			return "", fmt.Errorf("packed path is required for %q", node.Path)
		}
		file, err := os.Open(packedPath)
		if err != nil {
			return "", err
		}
		defer file.Close()
		buf := make([]byte, node.Size)
		n, err := file.ReadAt(buf, int64(node.PackedOffset))
		if err != nil && err != io.EOF {
			return "", err
		}
		return string(buf[:n]), nil
	}
	if node.CVMFSTarget == "" {
		return "", nil
	}
	if client == nil {
		return "", fmt.Errorf("cvmfs client is required for %q", node.Path)
	}
	data, err := client.ReadFile(node.CVMFSTarget)
	if err != nil {
		return "", err
	}
	if len(data) > maxSIMGMetadataFileSize {
		data = data[:maxSIMGMetadataFileSize]
	}
	return string(data), nil
}

func (s *Store) finalizeSIMGImage(name string, spec SourceSpec, imageDir, tmpDir, simgPath string) error {
	rootFS, entries, arch, err := simg.BuildImageFS(simgPath)
	if err != nil {
		return fmt.Errorf("index simg: %w", err)
	}
	deployMetadata := extractSIMGDeployMetadata(rootFS)
	metadataPath := filepath.Join(imageDir, "rootfs.metadata.json")
	fsMetaBuf, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal fs metadata: %w", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "rootfs.metadata.json"), fsMetaBuf, 0o644); err != nil {
		return fmt.Errorf("write fs metadata: %w", err)
	}
	meta := metadata{
		Name:         name,
		Source:       spec.Raw,
		SourceKind:   spec.Kind,
		Architecture: arch,
		RootFSDir:    imageDir,
		MetadataPath: metadataPath,
		Env:          deployMetadata.Env,
	}
	if err := os.RemoveAll(imageDir); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove old image dir: %w", err)
	}
	if err := os.Rename(tmpDir, imageDir); err != nil {
		return fmt.Errorf("activate image dir: %w", err)
	}
	if err := s.writeMetadata(name, meta); err != nil {
		return err
	}
	return nil
}

func (s *Store) maybePrefetchCVMFSImage(ctx context.Context, name string, spec SourceSpec, options PullOptions) error {
	if spec.Kind != SourceKindCVMFS || !options.Prefetch {
		return nil
	}
	meta, err := s.readMetadata(name)
	if err != nil {
		return fmt.Errorf("read image metadata for prefetch: %w", err)
	}
	if meta.IndexPath == "" {
		return nil
	}
	indexBuf, err := os.ReadFile(meta.IndexPath)
	if err != nil {
		return fmt.Errorf("read fs index for prefetch: %w", err)
	}
	nodes, err := decodeFSIndex(indexBuf)
	if err != nil {
		return fmt.Errorf("decode fs index for prefetch: %w", err)
	}
	cvmfsClient := &intcvmfs.Client{
		HTTPClient: s.httpClient,
		CacheDir:   cvmfsCacheDir(s.sharedRoot()),
		OnActivity: s.cvmfsActivity,
		Mirrors:    options.CVMFSMirrors,
	}
	cachedNodes, err := prefetchCVMFSFiles(ctx, cvmfsClient, nodes, options.PrefetchWorkers, name, options.Report)
	if err != nil {
		return err
	}
	indexBuf, err = encodeIndexedNodes(cachedNodes)
	if err != nil {
		return fmt.Errorf("marshal fs index after prefetch: %w", err)
	}
	if err := os.WriteFile(meta.IndexPath, indexBuf, 0o644); err != nil {
		return fmt.Errorf("write fs index after prefetch: %w", err)
	}
	if meta.PackedContentsPath != "" {
		_ = os.Remove(meta.PackedContentsPath)
		meta.PackedContentsPath = ""
	}
	return s.writeMetadata(name, meta)
}

func normalizeCVMFSSource(source string) string {
	lower := strings.ToLower(source)
	if strings.HasPrefix(lower, "http+cvmfs://") {
		u, err := url.Parse("https://" + source[len("http+cvmfs://"):])
		if err == nil {
			queryPath := strings.TrimSpace(u.Query().Get("path"))
			if queryPath != "" {
				u.RawQuery = ""
				u.Path = strings.TrimRight(u.Path, "/") + "/" + strings.TrimPrefix(path.Clean("/"+queryPath), "/")
				return u.String()
			}
		}
		return "https://" + source[len("http+cvmfs://"):]
	}
	return source
}

func (s *Store) fetchSIMG(ctx context.Context, source, destPath string) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return fmt.Errorf("create simg dir: %w", err)
	}
	tmpPath := destPath + ".tmp"
	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
		if err != nil {
			return err
		}
		resp, err := s.httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("download simg: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("download simg: status %s", resp.Status)
		}
		dst, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			return err
		}
		if _, err := io.Copy(dst, resp.Body); err != nil {
			_ = dst.Close()
			_ = os.Remove(tmpPath)
			return err
		}
		if err := dst.Close(); err != nil {
			_ = os.Remove(tmpPath)
			return err
		}
		return os.Rename(tmpPath, destPath)
	}
	info, err := os.Stat(source)
	if err != nil {
		return fmt.Errorf("stat simg source: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("simg source must be a file")
	}
	if err := copyFile(source, tmpPath, 0o644); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("copy simg source: %w", err)
	}
	return os.Rename(tmpPath, destPath)
}

func (s *Store) pullOCIDirect(ctx context.Context, name string, spec SourceSpec) error {
	registry, imageName, tag, err := ParseImageRef(spec.Raw)
	if err != nil {
		return err
	}
	reg := &registryContext{client: s.httpClient, registry: registry}

	imageDir := s.imageDir(name)
	tmpDir := imageDir + ".tmp"
	if err := os.RemoveAll(tmpDir); err != nil {
		return fmt.Errorf("remove temp image dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(tmpDir, "blobs"), 0o755); err != nil {
		return fmt.Errorf("create temp image dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(tmpDir, "layers"), 0o755); err != nil {
		return fmt.Errorf("create layers dir: %w", err)
	}

	mani, err := s.fetchManifest(ctx, reg, imageName, tag, preferredManifestArchitectures()...)
	if err != nil {
		return err
	}

	cfgBlob, err := s.fetchBlob(ctx, reg, imageName, mani.Config.Digest)
	if err != nil {
		return fmt.Errorf("fetch config blob: %w", err)
	}
	var cfg imageConfig
	if err := json.Unmarshal(cfgBlob, &cfg); err != nil {
		return fmt.Errorf("decode image config: %w", err)
	}

	build := newIndexedBuildState()
	for _, layer := range mani.Layers {
		layerTarRel := filepath.Join("layers", digestToFileName(layer.Digest)+".tar")
		layerTarPath := filepath.Join(tmpDir, layerTarRel)
		if err := s.fetchLayerTar(ctx, reg, imageName, layer, layerTarPath); err != nil {
			return fmt.Errorf("cache layer %s: %w", layer.Digest, err)
		}
		if err := applyIndexedLayer(layerTarPath, layerTarRel, build.merged, build.fsEntries); err != nil {
			return fmt.Errorf("index layer %s: %w", layer.Digest, err)
		}
	}
	return s.finalizeIndexedImage(name, spec, imageDir, tmpDir, cfg, build)
}

type indexedBuildState struct {
	merged    map[string]*indexedNode
	fsEntries map[string]fsmeta.Entry
}

func newIndexedBuildState() indexedBuildState {
	fsEntries := map[string]fsmeta.Entry{
		"/": {UID: 0, GID: 0, Mode: fsmeta.LinuxModeFromFileMode(os.ModeDir | 0o755)},
	}
	return indexedBuildState{
		fsEntries: fsEntries,
		merged: map[string]*indexedNode{
			"/": {
				Path: "/",
				Kind: indexedKindDir,
				Mode: fsEntries["/"].Mode,
				UID:  0,
				GID:  0,
			},
		},
	}
}

func (s *Store) finalizeIndexedImage(name string, spec SourceSpec, imageDir, tmpDir string, cfg imageConfig, build indexedBuildState) error {
	ensureIndexedParents(build.merged, build.fsEntries)
	indexPath := filepath.Join(imageDir, "rootfs.index.json")
	indexBuf, err := encodeFSIndex(build.merged)
	if err != nil {
		return fmt.Errorf("marshal fs index: %w", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "rootfs.index.json"), indexBuf, 0o644); err != nil {
		return fmt.Errorf("write fs index: %w", err)
	}
	metadataPath := filepath.Join(imageDir, "rootfs.metadata.json")
	fsMetaBuf, err := json.MarshalIndent(build.fsEntries, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal fs metadata: %w", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "rootfs.metadata.json"), fsMetaBuf, 0o644); err != nil {
		return fmt.Errorf("write fs metadata: %w", err)
	}

	meta := metadata{
		Name:         name,
		Source:       spec.Raw,
		SourceKind:   spec.Kind,
		Architecture: cfg.Architecture,
		RootFSDir:    imageDir,
		MetadataPath: metadataPath,
		IndexPath:    indexPath,
		Env:          append([]string(nil), cfg.Config.Env...),
		Entrypoint:   append([]string(nil), cfg.Config.Entrypoint...),
		Cmd:          append([]string(nil), cfg.Config.Cmd...),
		WorkingDir:   cfg.Config.WorkingDir,
		User:         cfg.Config.User,
		Labels:       labelPairsFromMap(cfg.Config.Labels),
	}

	if err := os.RemoveAll(imageDir); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove old image dir: %w", err)
	}
	if err := os.Rename(tmpDir, imageDir); err != nil {
		return fmt.Errorf("activate image dir: %w", err)
	}
	if err := s.writeMetadata(name, meta); err != nil {
		return err
	}
	return nil
}

func (s *Store) existingState(name string, spec SourceSpec) (client.ImageState, bool, error) {
	meta, err := s.readMetadata(name)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return client.ImageState{}, false, nil
		}
		return client.ImageState{}, false, err
	}
	if meta.Source != spec.Raw || meta.SourceKind != spec.Kind {
		return client.ImageState{}, false, nil
	}
	if spec.Kind == SourceKindCVMFS {
		if meta.CVMFSRootHash == "" {
			return client.ImageState{}, false, nil
		}
		currentHash, err := s.currentCVMFSRootHash(spec, meta.CVMFSMirrors)
		if err != nil {
			return client.ImageState{}, false, err
		}
		if currentHash != meta.CVMFSRootHash {
			return client.ImageState{}, false, nil
		}
	}
	if !dirExists(meta.RootFSDir) {
		return client.ImageState{}, false, nil
	}
	return client.ImageState{Name: meta.Name, Source: meta.Source, SourceKind: meta.SourceKind, Status: "downloaded"}, true, nil
}

func (s *Store) restoreFromSharedCache(name string, spec SourceSpec) (client.ImageState, bool, error) {
	shared := NewStore(s.sharedRoot())
	sharedName := sharedImageKey(spec)
	meta, err := shared.readMetadata(sharedName)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return client.ImageState{}, false, nil
		}
		return client.ImageState{}, false, err
	}
	if meta.Source != spec.Raw || meta.SourceKind != spec.Kind || !dirExists(meta.RootFSDir) {
		return client.ImageState{}, false, nil
	}
	if spec.Kind == SourceKindCVMFS {
		if meta.CVMFSRootHash == "" {
			return client.ImageState{}, false, nil
		}
		currentHash, err := s.currentCVMFSRootHash(spec, meta.CVMFSMirrors)
		if err != nil {
			return client.ImageState{}, false, err
		}
		if currentHash != meta.CVMFSRootHash {
			return client.ImageState{}, false, nil
		}
	}
	if !dirExists(meta.RootFSDir) {
		return client.ImageState{}, false, nil
	}
	if err := s.cloneFromStore(shared, sharedName, name, spec); err != nil {
		return client.ImageState{}, false, err
	}
	return client.ImageState{Name: name, Source: spec.Raw, SourceKind: spec.Kind, Status: "downloaded"}, true, nil
}

func (s *Store) currentCVMFSRootHash(spec SourceSpec, mirrors []string) (string, error) {
	cvmfsClient := &intcvmfs.Client{
		HTTPClient: s.httpClient,
		CacheDir:   cvmfsCacheDir(s.sharedRoot()),
		OnActivity: s.cvmfsActivity,
		Mirrors:    mirrors,
	}
	return cvmfsClient.ManifestRootHash(normalizeCVMFSSource(spec.Raw))
}

func (s *Store) cvmfsActivity(event intcvmfs.ActivityEvent) {
	if s == nil || s.CVMFSActivity == nil {
		return
	}
	s.CVMFSActivity(event.Bytes)
}

func (s *Store) cloneFromStore(src *Store, srcName, dstName string, spec SourceSpec) error {
	srcMeta, err := src.readMetadata(srcName)
	if err != nil {
		return err
	}
	srcDir := src.imageDir(srcName)
	dstDir := s.imageDir(dstName)
	tmpDir := dstDir + ".tmp"
	if err := os.RemoveAll(tmpDir); err != nil {
		return fmt.Errorf("remove temp image dir: %w", err)
	}
	if err := copyTree(srcDir, tmpDir); err != nil {
		return fmt.Errorf("copy cached image: %w", err)
	}
	meta := srcMeta
	meta.Name = dstName
	meta.Source = spec.Raw
	meta.SourceKind = spec.Kind
	meta.RootFSDir = dstDir
	if meta.MetadataPath != "" {
		meta.MetadataPath = filepath.Join(dstDir, "rootfs.metadata.json")
	}
	if meta.IndexPath != "" {
		meta.IndexPath = filepath.Join(dstDir, "rootfs.index.json")
	}
	buf, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal image metadata: %w", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "image.json"), buf, 0o644); err != nil {
		return fmt.Errorf("write image metadata: %w", err)
	}
	if err := os.RemoveAll(dstDir); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove old image dir: %w", err)
	}
	if err := os.Rename(tmpDir, dstDir); err != nil {
		return fmt.Errorf("activate image dir: %w", err)
	}
	return nil
}

func (s *Store) fetchManifest(ctx context.Context, reg *registryContext, imageName, tag string, archs ...string) (manifest, error) {
	body, mediaType, err := s.getJSONBlob(ctx, reg, "/"+imageName+"/manifests/"+tag, []string{
		"application/vnd.docker.distribution.manifest.list.v2+json",
		"application/vnd.oci.image.index.v1+json",
		"application/vnd.docker.distribution.manifest.v2+json",
		"application/vnd.oci.image.manifest.v1+json",
	})
	if err != nil {
		return manifest{}, err
	}

	if isManifestMediaType(mediaType) {
		var mani manifest
		if err := json.Unmarshal(body, &mani); err != nil {
			return manifest{}, fmt.Errorf("decode manifest: %w", err)
		}
		return mani, nil
	}

	var index manifestList
	if err := json.Unmarshal(body, &index); err != nil {
		return manifest{}, fmt.Errorf("decode manifest list: %w", err)
	}

	for _, arch := range archs {
		for _, entry := range index.Manifests {
			if entry.Platform.OS == "linux" && entry.Platform.Architecture == arch {
				body, _, err := s.getJSONBlob(ctx, reg, "/"+imageName+"/manifests/"+entry.Digest, []string{
					"application/vnd.docker.distribution.manifest.v2+json",
					"application/vnd.oci.image.manifest.v1+json",
				})
				if err != nil {
					return manifest{}, err
				}
				var mani manifest
				if err := json.Unmarshal(body, &mani); err != nil {
					return manifest{}, fmt.Errorf("decode manifest: %w", err)
				}
				return mani, nil
			}
		}
	}

	return manifest{}, fmt.Errorf("manifest for %v not found", archs)
}

func (s *Store) fetchBlob(ctx context.Context, reg *registryContext, imageName, digest string) ([]byte, error) {
	blobPath := filepath.Join(s.root, "_blobs", digestToFileName(digest))
	if data, err := os.ReadFile(blobPath); err == nil {
		return data, nil
	}

	body, _, err := s.getJSONBlob(ctx, reg, "/"+imageName+"/blobs/"+digest, nil)
	if err == nil {
		if err := os.MkdirAll(filepath.Dir(blobPath), 0o755); err == nil {
			_ = os.WriteFile(blobPath, body, 0o644)
		}
		return body, nil
	}

	data, err := s.getRawBlob(ctx, reg, "/"+imageName+"/blobs/"+digest)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(blobPath), 0o755); err == nil {
		_ = os.WriteFile(blobPath, data, 0o644)
	}
	return data, nil
}

func (s *Store) fetchLayerTar(ctx context.Context, reg *registryContext, imageName string, layer descriptor, dstPath string) error {
	blobPath := filepath.Join(s.root, "_blobs", digestToFileName(layer.Digest))
	if file, err := os.Open(blobPath); err == nil {
		defer file.Close()
		return writeLayerTarFromReader(dstPath, layer.MediaType, file)
	}

	resp, err := reg.do(ctx, "/"+imageName+"/blobs/"+layer.Digest, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return writeLayerTarFromReader(dstPath, layer.MediaType, resp.Body)
}

func (s *Store) getJSONBlob(ctx context.Context, reg *registryContext, path string, accept []string) ([]byte, string, error) {
	resp, err := reg.do(ctx, path, accept)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("read response body: %w", err)
	}
	return data, resp.Header.Get("Content-Type"), nil
}

func (s *Store) getRawBlob(ctx context.Context, reg *registryContext, path string) ([]byte, error) {
	resp, err := reg.do(ctx, path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}
	return data, nil
}

func (reg *registryContext) do(ctx context.Context, path string, accept []string) (*http.Response, error) {
	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reg.registry+path, nil)
		if err != nil {
			return nil, err
		}
		if reg.token != "" {
			req.Header.Set("Authorization", "Bearer "+reg.token)
		}
		for _, value := range accept {
			req.Header.Add("Accept", value)
		}

		resp, err := reg.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("registry request: %w", err)
		}
		if resp.StatusCode == http.StatusUnauthorized && attempt == 0 {
			if err := reg.authorize(ctx, resp.Header.Get("www-authenticate")); err != nil {
				resp.Body.Close()
				return nil, err
			}
			resp.Body.Close()
			continue
		}
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return nil, fmt.Errorf("registry request failed: %s (%s)", resp.Status, strings.TrimSpace(string(body)))
		}
		return resp, nil
	}
	return nil, fmt.Errorf("registry authorization failed")
}

func (reg *registryContext) authorize(ctx context.Context, header string) error {
	params, err := parseAuthenticate(header)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s?service=%s&scope=%s", params["realm"], params["service"], params["scope"]),
		nil,
	)
	if err != nil {
		return err
	}
	resp, err := reg.client.Do(req)
	if err != nil {
		return fmt.Errorf("request registry token: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("token request failed: %s", resp.Status)
	}
	var token tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return fmt.Errorf("decode token response: %w", err)
	}
	switch {
	case token.Token != "":
		reg.token = token.Token
	case token.AccessToken != "":
		reg.token = token.AccessToken
	default:
		return fmt.Errorf("token response missing token")
	}
	return nil
}

func (s *Store) getLocked(name string) (client.ImageState, error) {
	if s.downloading[name] {
		meta, err := s.readMetadata(name)
		if err == nil {
			return client.ImageState{Name: name, Source: meta.Source, SourceKind: meta.SourceKind, Status: "downloading"}, nil
		}
		return client.ImageState{Name: name, Status: "downloading"}, nil
	}

	meta, err := s.readMetadata(name)
	if err == nil {
		return client.ImageState{Name: meta.Name, Source: meta.Source, SourceKind: meta.SourceKind, Status: "downloaded"}, nil
	}
	if lastErr := s.lastErr[name]; lastErr != nil {
		return client.ImageState{Name: name, Status: "error", Error: lastErr.Error()}, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return client.ImageState{}, fmt.Errorf("image %q not found", name)
	}
	return client.ImageState{}, err
}

func (s *Store) writeMetadata(name string, meta metadata) error {
	dir := s.imageDir(name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create image dir: %w", err)
	}
	buf, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal image metadata: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "image.json"), buf, 0o644); err != nil {
		return fmt.Errorf("write image metadata: %w", err)
	}
	return nil
}

func (s *Store) readMetadata(name string) (metadata, error) {
	var ret metadata
	buf, err := os.ReadFile(filepath.Join(s.imageDir(name), "image.json"))
	if err != nil {
		return ret, err
	}
	if err := json.Unmarshal(buf, &ret); err != nil {
		return ret, fmt.Errorf("decode image metadata: %w", err)
	}
	if ret.Name == "" {
		ret.Name = name
	}
	if ret.Source == "" {
		return ret, errors.New("image metadata missing source")
	}
	if ret.SourceKind == "" {
		spec, err := ParseSource(ret.Source)
		if err != nil {
			return ret, fmt.Errorf("infer source kind: %w", err)
		}
		ret.SourceKind = spec.Kind
	}
	return ret, nil
}

func (s *Store) imageDir(name string) string {
	return filepath.Join(s.root, name)
}

func (s *Store) sharedRoot() string {
	if root := strings.TrimSpace(os.Getenv(sharedCacheEnv)); root != "" {
		return root
	}
	cacheRoot, err := os.UserCacheDir()
	if err != nil || cacheRoot == "" {
		return filepath.Join(os.TempDir(), "ccx3-oci-cache")
	}
	return filepath.Join(cacheRoot, "ccx3", "oci")
}

func (img *Image) Command(override []string) []string {
	if len(override) > 0 {
		if len(img.Config.Entrypoint) > 0 {
			return append(append([]string(nil), img.Config.Entrypoint...), override...)
		}
		return append([]string(nil), override...)
	}
	if len(img.Config.Entrypoint) > 0 && len(img.Config.Cmd) > 0 {
		out := append([]string(nil), img.Config.Entrypoint...)
		return append(out, img.Config.Cmd...)
	}
	if len(img.Config.Entrypoint) > 0 {
		return append([]string(nil), img.Config.Entrypoint...)
	}
	return append([]string(nil), img.Config.Cmd...)
}

func ParseSource(source string) (SourceSpec, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		return SourceSpec{}, fmt.Errorf("image source is required")
	}
	lower := strings.ToLower(source)
	switch {
	case strings.HasPrefix(lower, "docker-archive:"):
		return SourceSpec{Kind: SourceKindDockerArchive, Raw: source}, nil
	case strings.HasPrefix(lower, "http+cvmfs://"):
		return SourceSpec{Kind: SourceKindCVMFS, Raw: source}, nil
	case strings.HasPrefix(lower, "cvmfs://"):
		return SourceSpec{Kind: SourceKindCVMFS, Raw: source}, nil
	case strings.HasPrefix(lower, "/cvmfs/"):
		return SourceSpec{Kind: SourceKindCVMFS, Raw: source}, nil
	case (strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")) && strings.Contains(lower, "/cvmfs/"):
		return SourceSpec{Kind: SourceKindCVMFS, Raw: source}, nil
	case strings.HasSuffix(lower, ".simg"), strings.HasSuffix(lower, ".sif"):
		return SourceSpec{Kind: SourceKindSIMG, Raw: source}, nil
	default:
		if _, _, _, err := ParseImageRef(source); err == nil {
			return SourceSpec{Kind: SourceKindOCI, Raw: source}, nil
		}
		return SourceSpec{}, fmt.Errorf("unsupported image source %q", source)
	}
}

func ParseImageRef(imageRef string) (registry string, image string, tag string, err error) {
	if strings.TrimSpace(imageRef) == "" {
		return "", "", "", fmt.Errorf("image source is required")
	}
	image = imageRef
	tag = "latest"

	lastSlash := strings.LastIndex(imageRef, "/")
	lastColon := strings.LastIndex(imageRef, ":")
	if lastColon > lastSlash {
		image = imageRef[:lastColon]
		tag = imageRef[lastColon+1:]
	}

	firstSlash := strings.Index(image, "/")
	if firstSlash != -1 {
		firstComponent := image[:firstSlash]
		isHostname := strings.Contains(firstComponent, ".") || strings.Contains(firstComponent, ":") || firstComponent == "localhost"
		if isHostname {
			registry = firstComponent
			image = image[firstSlash+1:]
		}
	}

	if registry == "" || registry == "docker.io" {
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

func parseAuthenticate(value string) (map[string]string, error) {
	if value == "" {
		return nil, fmt.Errorf("missing authenticate header")
	}
	value = strings.TrimPrefix(value, "Bearer ")
	parts := strings.Split(value, ",")
	ret := map[string]string{}
	for _, part := range parts {
		key, val, ok := strings.Cut(part, "=")
		if !ok {
			return nil, fmt.Errorf("malformed authenticate header segment %q", part)
		}
		ret[strings.TrimSpace(key)] = strings.Trim(val, "\" ")
	}
	return ret, nil
}

func applyLayer(rootfsDir, mediaType string, blob []byte, entries map[string]fsmeta.Entry) error {
	var src io.Reader = bytes.NewReader(blob)
	if isGzipMediaType(mediaType, blob) {
		gzr, err := gzip.NewReader(src)
		if err != nil {
			return fmt.Errorf("open gzip layer: %w", err)
		}
		defer gzr.Close()
		src = gzr
	}
	tr := tar.NewReader(src)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read layer tar: %w", err)
		}

		name, err := sanitizeArchivePath(hdr.Name)
		if err != nil {
			return err
		}
		base := path.Base(name)
		dir := path.Dir(name)
		hostPath := filepath.Join(rootfsDir, filepath.FromSlash(name))

		if base == ".wh..wh..opq" {
			opaqueDir := filepath.Join(rootfsDir, filepath.FromSlash(dir))
			if err := clearDirectoryContents(opaqueDir); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			opaquePrefix := fsmeta.Normalize(dir)
			for key := range entries {
				if key != opaquePrefix && strings.HasPrefix(key, opaquePrefix+"/") {
					delete(entries, key)
				}
			}
			continue
		}
		if strings.HasPrefix(base, ".wh.") {
			deletedName := path.Join(dir, strings.TrimPrefix(base, ".wh."))
			deleted := filepath.Join(rootfsDir, filepath.FromSlash(deletedName))
			if err := os.RemoveAll(deleted); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("apply whiteout %s: %w", deleted, err)
			}
			delete(entries, fsmeta.Normalize(deletedName))
			continue
		}

		if err := os.MkdirAll(filepath.Dir(hostPath), 0o755); err != nil {
			return err
		}
		if err := os.RemoveAll(hostPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(hostPath, hdr.FileInfo().Mode().Perm()); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			f, err := os.OpenFile(hostPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, hdr.FileInfo().Mode().Perm())
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				_ = f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if err := os.Symlink(hdr.Linkname, hostPath); err != nil {
				return err
			}
		case tar.TypeLink:
			target, err := sanitizeArchivePath(hdr.Linkname)
			if err != nil {
				return err
			}
			if err := os.Link(filepath.Join(rootfsDir, filepath.FromSlash(target)), hostPath); err != nil {
				return err
			}
		case tar.TypeXGlobalHeader:
		default:
			return fmt.Errorf("unsupported layer entry type %d for %s", hdr.Typeflag, name)
		}
		entries[fsmeta.Normalize(name)] = fsmeta.Entry{
			UID:  uint32(hdr.Uid),
			GID:  uint32(hdr.Gid),
			Mode: fsmeta.LinuxModeFromTarHeader(hdr),
		}
	}
}

func sanitizeArchivePath(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("empty archive path")
	}
	name = path.Clean(strings.TrimPrefix(name, "/"))
	if name == "." || strings.HasPrefix(name, "../") {
		return "", fmt.Errorf("invalid archive path %q", name)
	}
	return name, nil
}

func clearDirectoryContents(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(dir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func isManifestMediaType(mediaType string) bool {
	return strings.Contains(mediaType, "manifest.v1+json") || strings.Contains(mediaType, "manifest.v2+json")
}

func isGzipMediaType(mediaType string, blob []byte) bool {
	if strings.Contains(mediaType, "gzip") {
		return true
	}
	return len(blob) >= 2 && blob[0] == 0x1f && blob[1] == 0x8b
}

func digestToFileName(digest string) string {
	if strings.HasPrefix(digest, "sha256:") {
		return strings.TrimPrefix(digest, "sha256:")
	}
	sum := sha256.Sum256([]byte(digest))
	return hex.EncodeToString(sum[:])
}

func sharedImageKey(spec SourceSpec) string {
	sum := sha256.Sum256([]byte(sharedCacheSchemaVersion + "\n" + nativeArch() + "\n" + spec.Kind + "\n" + spec.Raw))
	return hex.EncodeToString(sum[:16])
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func copyTree(srcDir, dstDir string) error {
	return filepath.WalkDir(srcDir, func(current string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcDir, current)
		if err != nil {
			return err
		}
		target := filepath.Join(dstDir, rel)
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		switch mode := info.Mode(); {
		case mode.IsDir():
			return os.MkdirAll(target, mode.Perm())
		case mode&os.ModeSymlink != 0:
			link, err := os.Readlink(current)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		case mode.IsRegular():
			return copyFile(current, target, mode.Perm())
		default:
			return fmt.Errorf("unsupported file mode %v at %s", mode, current)
		}
	})
}

func copyFile(srcPath, dstPath string, mode os.FileMode) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.OpenFile(dstPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()
		return err
	}
	return dst.Close()
}

func nativeArch() string {
	switch runtime.GOARCH {
	case "arm64":
		return "arm64"
	case "amd64":
		return "amd64"
	default:
		return runtime.GOARCH
	}
}

func preferredManifestArchitectures() []string {
	out := []string{nativeArch()}
	if nativeArch() == "arm64" {
		out = append(out, "amd64")
	}
	return out
}

func labelPairsFromMap(labels map[string]string) []labelPair {
	if len(labels) == 0 {
		return nil
	}
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]labelPair, 0, len(keys))
	for _, key := range keys {
		out = append(out, labelPair{Key: key, Value: labels[key]})
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func labelsFromPairs(pairs []labelPair) map[string]string {
	if len(pairs) == 0 {
		return nil
	}
	out := make(map[string]string, len(pairs))
	for _, pair := range pairs {
		out[pair.Key] = pair.Value
	}
	return out
}
