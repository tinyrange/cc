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
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"j5.nz/cc/client"
)

const defaultRegistry = "https://registry-1.docker.io/v2"

type Store struct {
	root       string
	httpClient *http.Client

	mu          sync.Mutex
	downloading map[string]bool
	lastErr     map[string]error
}

type metadata struct {
	Name         string      `json:"name"`
	Source       string      `json:"source"`
	Architecture string      `json:"architecture,omitempty"`
	RootFSDir    string      `json:"rootfs_dir"`
	Env          []string    `json:"env,omitempty"`
	Entrypoint   []string    `json:"entrypoint,omitempty"`
	Cmd          []string    `json:"cmd,omitempty"`
	WorkingDir   string      `json:"working_dir,omitempty"`
	User         string      `json:"user,omitempty"`
	Labels       []labelPair `json:"labels,omitempty"`
}

type labelPair struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type Image struct {
	Name         string
	Source       string
	Architecture string
	RootFSDir    string
	Config       RuntimeConfig
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
	return &Image{
		Name:         meta.Name,
		Source:       meta.Source,
		Architecture: meta.Architecture,
		RootFSDir:    meta.RootFSDir,
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

func (s *Store) Pull(ctx context.Context, name, source string) (client.ImageState, error) {
	if name == "" {
		return client.ImageState{}, fmt.Errorf("image name is required")
	}
	if source == "" {
		return client.ImageState{}, fmt.Errorf("image source is required")
	}

	s.mu.Lock()
	if s.downloading[name] {
		s.mu.Unlock()
		return client.ImageState{}, fmt.Errorf("image %q download already in progress", name)
	}
	s.downloading[name] = true
	delete(s.lastErr, name)
	s.mu.Unlock()

	err := s.pull(ctx, name, source)

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

func (s *Store) pull(ctx context.Context, name, source string) error {
	if err := os.MkdirAll(s.root, 0o755); err != nil {
		return fmt.Errorf("create image store: %w", err)
	}

	registry, imageName, tag, err := ParseImageRef(source)
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

	mani, err := s.fetchManifest(ctx, reg, imageName, tag, nativeArch())
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

	rootfsDir := filepath.Join(tmpDir, "rootfs")
	if err := os.MkdirAll(rootfsDir, 0o755); err != nil {
		return fmt.Errorf("create rootfs dir: %w", err)
	}
	for _, layer := range mani.Layers {
		layerBlob, err := s.fetchBlob(ctx, reg, imageName, layer.Digest)
		if err != nil {
			return fmt.Errorf("fetch layer %s: %w", layer.Digest, err)
		}
		if err := applyLayer(rootfsDir, layer.MediaType, layerBlob); err != nil {
			return fmt.Errorf("apply layer %s: %w", layer.Digest, err)
		}
	}

	meta := metadata{
		Name:         name,
		Source:       source,
		Architecture: cfg.Architecture,
		RootFSDir:    filepath.Join(imageDir, "rootfs"),
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

func (s *Store) fetchManifest(ctx context.Context, reg *registryContext, imageName, tag, arch string) (manifest, error) {
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

	return manifest{}, fmt.Errorf("manifest for linux/%s not found", arch)
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
			return client.ImageState{Name: name, Source: meta.Source, Status: "downloading"}, nil
		}
		return client.ImageState{Name: name, Status: "downloading"}, nil
	}

	meta, err := s.readMetadata(name)
	if err == nil {
		return client.ImageState{Name: meta.Name, Source: meta.Source, Status: "downloaded"}, nil
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
	return ret, nil
}

func (s *Store) imageDir(name string) string {
	return filepath.Join(s.root, name)
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

func applyLayer(rootfsDir, mediaType string, blob []byte) error {
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
			continue
		}
		if strings.HasPrefix(base, ".wh.") {
			deleted := filepath.Join(rootfsDir, filepath.FromSlash(path.Join(dir, strings.TrimPrefix(base, ".wh."))))
			if err := os.RemoveAll(deleted); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("apply whiteout %s: %w", deleted, err)
			}
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
