package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"log/slog"

	"github.com/schollz/progressbar/v3"
	"github.com/tinyrange/cc/internal/archive"
)

const (
	defaultRegistry = "https://registry-1.docker.io/v2"
)

type outputDirectory struct {
	outputDir string
}

type runtimeConfig struct {
	Layers     []string          `json:"layers"`
	Env        []string          `json:"env,omitempty"`
	Entrypoint []string          `json:"entrypoint,omitempty"`
	Cmd        []string          `json:"cmd,omitempty"`
	WorkingDir string            `json:"workingDir,omitempty"`
	User       string            `json:"user,omitempty"`
	UID        *int              `json:"uid,omitempty"`
	GID        *int              `json:"gid,omitempty"`
	Labels     map[string]string `json:"labels,omitempty"`
}

func (od *outputDirectory) writeConfig(cfg runtimeConfig) error {
	configPath := filepath.Join(od.outputDir, "config.json")
	f, err := os.Create(configPath)
	if err != nil {
		return fmt.Errorf("create config file %s: %w", configPath, err)
	}
	defer f.Close()

	encoder := json.NewEncoder(f)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)

	if err := encoder.Encode(cfg); err != nil {
		return fmt.Errorf("encode config file %s: %w", configPath, err)
	}

	return nil
}

func (od *outputDirectory) makeLayerFromTarFile(hash string, r io.Reader, compression string, oci bool) error {
	hash = strings.TrimPrefix(hash, "sha256:")

	var reader io.Reader = r

	if compression == "gzip" {
		gr, err := gzip.NewReader(r)
		if err != nil {
			return fmt.Errorf("create gzip reader: %w", err)
		}
		defer gr.Close()
		reader = gr
	}

	tr := tar.NewReader(reader)

	indexFile, err := os.Create(filepath.Join(od.outputDir, hash+".idx"))
	if err != nil {
		return fmt.Errorf("create layer file: %w", err)
	}
	defer indexFile.Close()

	contentsFile, err := os.Create(filepath.Join(od.outputDir, hash+".contents"))
	if err != nil {
		return fmt.Errorf("create layer file: %w", err)
	}
	defer contentsFile.Close()

	w, err := archive.NewArchiveWriter(indexFile, contentsFile)
	if err != nil {
		return fmt.Errorf("create archive writer: %w", err)
	}

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		} else if err != nil {
			return err
		}

		deleted := false

		if oci {
			if path.Base(hdr.Name) == ".wh..wh..opq" {
				deleted = true
				hdr.Name = path.Dir(hdr.Name)
			} else if strings.HasPrefix(path.Base(hdr.Name), ".wh.") {
				deleted = true
				hdr.Name = path.Join(path.Dir(hdr.Name), path.Base(hdr.Name)[4:])
			}
		}

		info := hdr.FileInfo()

		var typeFlag archive.EntryKind

		switch hdr.Typeflag {
		case tar.TypeReg:
			typeFlag = archive.EntryKindRegular
		case tar.TypeDir:
			typeFlag = archive.EntryKindDirectory
		case tar.TypeChar:
			// TODO(joshua): Handle character devices.
			continue
		case tar.TypeBlock:
			// TODO(joshua): Handle block devices.
			continue
		case tar.TypeSymlink:
			typeFlag = archive.EntryKindSymlink
		case tar.TypeLink:
			typeFlag = archive.EntryKindHardlink
		case tar.TypeXGlobalHeader:
			continue
		default:
			return fmt.Errorf("unknown type flag: %d", hdr.Typeflag)
		}

		if deleted {
			typeFlag = archive.EntryKindDeleted
		}

		var fact archive.EntryFactory

		if err := w.WriteEntry(fact.
			Kind(typeFlag).
			Name(hdr.Name).
			Linkname(hdr.Linkname).
			Size(hdr.Size).
			Mode(info.Mode()).
			Owner(hdr.Uid, hdr.Gid).
			ModTime(hdr.ModTime), tr); err != nil {
			return err
		}
	}

	return nil
}

func newOutputDirectory(path string, overwrite bool) (*outputDirectory, error) {
	if _, err := os.Stat(path); err == nil {
		if overwrite {
			if err := os.RemoveAll(path); err != nil {
				return nil, fmt.Errorf("remove existing output directory %s: %w", path, err)
			}
		} else {
			return nil, fmt.Errorf("output directory %s already exists", path)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("stat output directory %s: %w", path, err)
	}

	if err := os.MkdirAll(path, 0o755); err != nil {
		return nil, fmt.Errorf("create output directory %s: %w", path, err)
	}

	return &outputDirectory{
		outputDir: path,
	}, nil
}

type stringSlice []string

func (s *stringSlice) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	switch {
	case len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")):
		*s = nil
		return nil
	case trimmed[0] == '[':
		var arr []string
		if err := json.Unmarshal(trimmed, &arr); err != nil {
			return err
		}
		*s = arr
		return nil
	default:
		var single string
		if err := json.Unmarshal(trimmed, &single); err != nil {
			return err
		}
		*s = []string{single}
		return nil
	}
}

type ociToArchive struct {
	cacheDir string
	logger   *slog.Logger
	client   *http.Client
}

type imagePlatform struct {
	Architecture string `json:"architecture"`
	Os           string `json:"os"`
	Variant      string `json:"variant"`
}

type imageManifestIdentifier struct {
	MediaType string        `json:"mediaType"`
	Size      uint64        `json:"size"`
	Digest    string        `json:"digest"`
	Platform  imagePlatform `json:"platform"`
}

type imageIndexV2 struct {
	SchemaVersion int                       `json:"schemaVersion"`
	MediaType     string                    `json:"mediaType"`
	Manifests     []imageManifestIdentifier `json:"manifests"`
}

type tokenResponse struct {
	Token       string `json:"token"`
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
	IssuedAt    string `json:"issued_at"`
}

type imageConfigIdentifier struct {
	MediaType string `json:"mediaType"`
	Size      uint64 `json:"size"`
	Digest    string `json:"digest"`
}

type imageLayerIdentifier struct {
	MediaType string `json:"mediaType"`
	Size      uint64 `json:"size"`
	Digest    string `json:"digest"`
}

type imageManifest struct {
	SchemaVersion int                    `json:"schemaVersion"`
	MediaType     string                 `json:"mediaType"`
	Config        imageConfigIdentifier  `json:"config"`
	Layers        []imageLayerIdentifier `json:"layers"`
}

type imageLayerV1 struct {
	BlobSum string `json:"blobSum"`
}

type imageIndexV1 struct {
	SchemaVersion int            `json:"schemaVersion"`
	Name          string         `json:"name"`
	Tag           string         `json:"tag"`
	Architecture  string         `json:"architecture"`
	FsLayers      []imageLayerV1 `json:"fsLayers"`
}

type imageConfigHistory struct {
	Created    time.Time `json:"created"`
	CreatedBy  string    `json:"created_by"`
	Comment    string    `json:"comment"`
	EmptyLayer bool      `json:"empty_layer"`
}

type imageConfigInfo struct {
	Hostname     string            `json:"Hostname"`
	Domainname   string            `json:"Domainname"`
	User         string            `json:"User"`
	AttachStdin  bool              `json:"AttachStdin"`
	AttachStdout bool              `json:"AttachStdout"`
	AttachStderr bool              `json:"AttachStderr"`
	Tty          bool              `json:"Tty"`
	OpenStdin    bool              `json:"OpenStdin"`
	StdinOnce    bool              `json:"StdinOnce"`
	Env          []string          `json:"Env"`
	Cmd          []string          `json:"Cmd"`
	Image        string            `json:"Image"`
	Volumes      any               `json:"Volumes"`
	WorkingDir   string            `json:"WorkingDir"`
	Entrypoint   stringSlice       `json:"Entrypoint"`
	OnBuild      any               `json:"OnBuild"`
	Labels       map[string]string `json:"Labels"`
}

type imageConfig struct {
	Config       imageConfigInfo      `json:"config"`
	Architecture string               `json:"architecture"`
	History      []imageConfigHistory `json:"history"`
}

type dockerSaveManifestEntry struct {
	Config   string   `json:"Config"`
	RepoTags []string `json:"RepoTags"`
	Layers   []string `json:"Layers"`
}

type ociRegistryContext struct {
	logger   *slog.Logger
	client   *http.Client
	registry string
	token    string
}

func (ctx *ociRegistryContext) makeRequest(method string, url string, accept []string) (*http.Request, error) {
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

func (ctx *ociRegistryContext) responseHandler(resp *http.Response) (bool, error) {
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

func (o *ociToArchive) cacheKey(path string, accept []string) string {
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

func normalizeTarName(name string) string {
	for strings.HasPrefix(name, "./") {
		name = strings.TrimPrefix(name, "./")
	}
	return name
}

func (o *ociToArchive) fetchToCache(ctx *ociRegistryContext, path string, accept []string) (string, error) {
	cacheName := o.cacheKey(path, accept)
	cachePath := filepath.Join(o.cacheDir, cacheName)

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

		ok, err := ctx.responseHandler(resp)
		if err != nil {
			return "", err
		}

		if !ok {
			// responseHandler closed resp.Body on reauth; retry.
			continue
		}

		defer resp.Body.Close()

		tmpFile, err := os.CreateTemp(o.cacheDir, "oci_*")
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

func (o *ociToArchive) readJSON(ctx *ociRegistryContext, path string, accept []string, out any) (string, error) {
	cachePath, err := o.fetchToCache(ctx, path, accept)
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

func parseOciImage(ociImage string) (registry string, image string, tag string, err error) {
	image, tag, ok := strings.Cut(ociImage, ":")
	if !ok {
		tag = "latest"
	}

	if strings.Contains(image, ".") {
		registry, image, ok = strings.Cut(image, "/")
		if !ok {
			return "", "", "", fmt.Errorf("invalid OCI image format %s", ociImage)
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

func toOciArchitecture(goarch string) (string, error) {
	switch goarch {
	case "amd64":
		return "amd64", nil
	case "arm64":
		return "arm64", nil
	default:
		return "", fmt.Errorf("unsupported architecture: %s", goarch)
	}
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

func (o *ociToArchive) fetchManifestForArch(ctx *ociRegistryContext, image string, arch string, tag string) (imageManifest, error) {
	indexPath := fmt.Sprintf("/%s/manifests/%s", image, tag)
	accept := []string{
		"application/vnd.docker.distribution.manifest.list.v2+json",
		"application/vnd.oci.image.index.v1+json",
		"application/vnd.docker.distribution.manifest.v2+json",
		"application/vnd.oci.image.manifest.v1+json",
	}

	cachePath, err := o.fetchToCache(ctx, indexPath, accept)
	if err != nil {
		return imageManifest{}, err
	}

	data, err := os.ReadFile(cachePath)
	if err != nil {
		return imageManifest{}, fmt.Errorf("read cache file %s: %w", cachePath, err)
	}

	var manifest imageManifest
	if err := json.Unmarshal(data, &manifest); err == nil && manifest.Config.Digest != "" {
		return manifest, nil
	}

	var index1 imageIndexV1
	if err := json.Unmarshal(data, &index1); err == nil && index1.SchemaVersion == 1 && len(index1.FsLayers) > 0 {
		return o.buildFromV1Index(ctx, image, arch, index1)
	}

	var index imageIndexV2
	if err := json.Unmarshal(data, &index); err != nil {
		return imageManifest{}, fmt.Errorf("decode image index: %w", err)
	}

	return o.buildFromIndex(ctx, image, arch, index)
}

func (o *ociToArchive) buildFromV1Index(ctx *ociRegistryContext, image string, arch string, index imageIndexV1) (imageManifest, error) {
	if index.Architecture != "" && index.Architecture != arch {
		return imageManifest{}, fmt.Errorf("index architecture mismatch: %s != %s", index.Architecture, arch)
	}

	var layers []imageLayerIdentifier
	for _, layer := range index.FsLayers {
		layers = append(layers, imageLayerIdentifier{
			Digest:    layer.BlobSum,
			MediaType: "application/vnd.docker.image.rootfs.diff.tar.gzip",
		})
	}

	return imageManifest{
		SchemaVersion: 1,
		MediaType:     "application/vnd.docker.distribution.manifest.v1+json",
		Layers:        layers,
	}, nil
}

func (o *ociToArchive) buildFromIndex(ctx *ociRegistryContext, image string, arch string, index imageIndexV2) (imageManifest, error) {
	var manifestID *imageManifestIdentifier
	for _, manifest := range index.Manifests {
		if manifest.Platform.Architecture == arch {
			manifestCopy := manifest
			manifestID = &manifestCopy
			break
		}
	}

	if manifestID == nil {
		return imageManifest{}, fmt.Errorf("manifest for architecture %s not found", arch)
	}

	var manifest imageManifest
	_, err := o.readJSON(ctx,
		fmt.Sprintf("/%s/manifests/%s", image, manifestID.Digest),
		[]string{"application/vnd.oci.image.manifest.v1+json", "application/vnd.docker.distribution.manifest.v2+json"},
		&manifest)
	if err != nil {
		return imageManifest{}, err
	}

	return manifest, nil
}

func compressionFromMediaType(mediaType string) (string, error) {
	switch mediaType {
	case "application/vnd.docker.image.rootfs.diff.tar.gzip",
		"application/vnd.oci.image.layer.v1.tar+gzip",
		"application/vnd.oci.image.layer.v1.tar+gzip;variant=gzip":
		return "gzip", nil
	case "application/vnd.oci.image.layer.v1.tar",
		"application/vnd.docker.image.rootfs.diff.tar":
		return "none", nil
	default:
		if strings.Contains(mediaType, "gzip") {
			return "gzip", nil
		}
		return "", fmt.Errorf("unsupported media type %s", mediaType)
	}
}

func populateRuntimeConfigFromImageConfig(cfg *runtimeConfig, imageCfg imageConfig) {
	if len(imageCfg.Config.Env) > 0 {
		cfg.Env = append(cfg.Env, imageCfg.Config.Env...)
	}
	if len(imageCfg.Config.Cmd) > 0 {
		cfg.Cmd = append(cfg.Cmd, imageCfg.Config.Cmd...)
	}
	if len(imageCfg.Config.Entrypoint) > 0 {
		cfg.Entrypoint = append(cfg.Entrypoint, imageCfg.Config.Entrypoint...)
	}
	cfg.WorkingDir = imageCfg.Config.WorkingDir

	if len(imageCfg.Config.Labels) > 0 {
		cfg.Labels = make(map[string]string, len(imageCfg.Config.Labels))
		for k, v := range imageCfg.Config.Labels {
			cfg.Labels[k] = v
		}
	}

	user, uid, gid := parseUser(imageCfg.Config.User)
	if user != "" {
		cfg.User = user
	}
	if uid != nil {
		cfg.UID = uid
	}
	if gid != nil {
		cfg.GID = gid
	}
}

func readTarEntryByName(f *os.File, target string) ([]byte, error) {
	normalized := normalizeTarName(target)
	if normalized == "" {
		return nil, fmt.Errorf("invalid tar entry name %q", target)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("seek docker save: %w", err)
	}
	tr := tar.NewReader(f)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("entry %s not found in docker save", target)
		}
		if err != nil {
			return nil, fmt.Errorf("read docker save entry: %w", err)
		}
		if normalizeTarName(hdr.Name) != normalized {
			continue
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			return nil, fmt.Errorf("read docker save entry %s: %w", target, err)
		}
		return data, nil
	}
}

func detectLayerCompression(rs io.ReadSeeker) (string, error) {
	var header [3]byte
	n, err := rs.Read(header[:])
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read layer header: %w", err)
	}
	if _, err := rs.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("rewind layer: %w", err)
	}
	if n >= 2 && header[0] == 0x1f && header[1] == 0x8b {
		return "gzip", nil
	}
	return "none", nil
}

func (o *ociToArchive) processDockerSaveLayer(f *os.File, layerPath string, out *outputDirectory) (string, error) {
	normalized := normalizeTarName(layerPath)
	if normalized == "" {
		return "", fmt.Errorf("invalid layer path %q", layerPath)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("seek docker save: %w", err)
	}
	tr := tar.NewReader(f)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return "", fmt.Errorf("layer %s not found in docker save", layerPath)
		}
		if err != nil {
			return "", fmt.Errorf("iterate docker save: %w", err)
		}
		if normalizeTarName(hdr.Name) != normalized {
			continue
		}
		tmp, err := os.CreateTemp(o.cacheDir, "o2a-layer-*.tar")
		if err != nil {
			return "", fmt.Errorf("create temp file for layer %s: %w", layerPath, err)
		}
		defer os.Remove(tmp.Name())
		defer tmp.Close()

		hasher := sha256.New()
		writer := io.MultiWriter(tmp, hasher)
		if _, err := io.Copy(writer, tr); err != nil {
			return "", fmt.Errorf("copy layer %s: %w", layerPath, err)
		}

		digest := hex.EncodeToString(hasher.Sum(nil))

		if _, err := tmp.Seek(0, io.SeekStart); err != nil {
			return "", fmt.Errorf("rewind layer %s: %w", layerPath, err)
		}

		compression, err := detectLayerCompression(tmp)
		if err != nil {
			return "", fmt.Errorf("detect compression for %s: %w", layerPath, err)
		}

		if _, err := tmp.Seek(0, io.SeekStart); err != nil {
			return "", fmt.Errorf("rewind layer %s: %w", layerPath, err)
		}

		if err := out.makeLayerFromTarFile("sha256:"+digest, tmp, compression, true); err != nil {
			return "", fmt.Errorf("process layer %s: %w", layerPath, err)
		}

		return digest, nil
	}
}

func (o *ociToArchive) makeFromDockerSave(out *outputDirectory, tarPath string) error {
	f, err := os.Open(tarPath)
	if err != nil {
		return fmt.Errorf("open docker save %s: %w", tarPath, err)
	}
	defer f.Close()

	manifestData, err := readTarEntryByName(f, "manifest.json")
	if err != nil {
		return err
	}

	var manifests []dockerSaveManifestEntry
	if err := json.Unmarshal(manifestData, &manifests); err != nil {
		return fmt.Errorf("decode docker save manifest: %w", err)
	}
	if len(manifests) == 0 {
		return errors.New("docker save manifest is empty")
	}

	entry := manifests[0]
	if entry.Config == "" {
		return errors.New("docker save manifest missing config entry")
	}

	configData, err := readTarEntryByName(f, entry.Config)
	if err != nil {
		return fmt.Errorf("read config %s: %w", entry.Config, err)
	}

	var imageCfg imageConfig
	if err := json.Unmarshal(configData, &imageCfg); err != nil {
		return fmt.Errorf("decode image config %s: %w", entry.Config, err)
	}

	var cfg runtimeConfig
	populateRuntimeConfigFromImageConfig(&cfg, imageCfg)

	for _, layer := range entry.Layers {
		digest, err := o.processDockerSaveLayer(f, layer, out)
		if err != nil {
			return fmt.Errorf("process docker save layer %s: %w", layer, err)
		}
		cfg.Layers = append(cfg.Layers, "sha256:"+digest)
	}

	return out.writeConfig(cfg)
}

func (o *ociToArchive) processManifest(ctx *ociRegistryContext, out *outputDirectory, image string, manifest imageManifest) error {
	var cfg runtimeConfig

	if manifest.Config.Digest != "" {
		configPath, err := o.fetchToCache(ctx, fmt.Sprintf("/%s/blobs/%s", image, manifest.Config.Digest), nil)
		if err != nil {
			return fmt.Errorf("fetch image config %s: %w", manifest.Config.Digest, err)
		}

		f, err := os.Open(configPath)
		if err != nil {
			return fmt.Errorf("open image config %s: %w", configPath, err)
		}

		var imageCfg imageConfig
		if err := json.NewDecoder(f).Decode(&imageCfg); err != nil {
			f.Close()
			return fmt.Errorf("decode image config %s: %w", configPath, err)
		}
		f.Close()

		populateRuntimeConfigFromImageConfig(&cfg, imageCfg)
	}

	for _, layer := range manifest.Layers {
		layerPath, err := o.fetchToCache(ctx, fmt.Sprintf("/%s/blobs/%s", image, layer.Digest), nil)
		if err != nil {
			return fmt.Errorf("fetch layer %s: %w", layer.Digest, err)
		}

		cfg.Layers = append(cfg.Layers, layer.Digest)

		compression, err := compressionFromMediaType(layer.MediaType)
		if err != nil {
			return err
		}

		f, err := os.Open(layerPath)
		if err != nil {
			return fmt.Errorf("open cached layer %s: %w", layer.Digest, err)
		}

		if err := out.makeLayerFromTarFile(layer.Digest, f, compression, true); err != nil {
			f.Close()
			return fmt.Errorf("process layer %s: %w", layer.Digest, err)
		}

		f.Close()
	}

	if err := out.writeConfig(cfg); err != nil {
		return err
	}

	return nil
}

func (o *ociToArchive) makeFromTag(out *outputDirectory, tag string) error {
	registry, image, version, err := parseOciImage(tag)
	if err != nil {
		return fmt.Errorf("parse tag %q: %w", tag, err)
	}

	arch, err := toOciArchitecture(runtime.GOARCH)
	if err != nil {
		return err
	}

	ctx := &ociRegistryContext{
		logger:   o.logger,
		client:   o.client,
		registry: registry,
	}

	manifest, err := o.fetchManifestForArch(ctx, image, arch, version)
	if err != nil {
		return err
	}

	return o.processManifest(ctx, out, image, manifest)
}

func (o *ociToArchive) Main() error {
	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)

	fromTag := fs.String("fromTag", "", "OCI image tag to convert from (e.g. 'alpine:latest')")
	fromDockerSave := fs.String("fromDockerSave", "", "Path to a Docker 'docker save' tarball to convert from")
	outputPath := fs.String("output", "", "Output directory to write the archive to")
	overwrite := fs.Bool("overwrite", false, "Overwrite the output directory if it exists")
	cacheDir := fs.String("cacheDir", "local/cache", "Directory to use for caching OCI downloads")

	if err := fs.Parse(os.Args[1:]); err != nil {
		return fmt.Errorf("parse flags: %w", err)
	}

	if (*fromTag == "") == (*fromDockerSave == "") {
		return fmt.Errorf("exactly one of fromTag or fromDockerSave must be provided")
	}

	if *cacheDir == "" {
		return fmt.Errorf("cacheDir is required")
	}

	if err := os.MkdirAll(*cacheDir, 0o755); err != nil {
		return fmt.Errorf("create cache directory %s: %w", *cacheDir, err)
	}

	o.cacheDir = *cacheDir
	o.logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if *fromTag != "" {
		o.client = &http.Client{
			Timeout: 60 * time.Second,
		}
	}

	if *outputPath == "" {
		return fmt.Errorf("output is required")
	}

	out, err := newOutputDirectory(*outputPath, *overwrite)
	if err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	if *fromDockerSave != "" {
		return o.makeFromDockerSave(out, *fromDockerSave)
	}

	return o.makeFromTag(out, *fromTag)
}

func main() {
	ota := &ociToArchive{}
	if err := ota.Main(); err != nil {
		fmt.Fprintf(os.Stderr, "ocitoarchive: %v\n", err)
		os.Exit(1)
	}
}

func parseUser(value string) (string, *int, *int) {
	user := strings.TrimSpace(value)
	if user == "" {
		return "", nil, nil
	}

	var uidPtr, gidPtr *int

	parts := strings.Split(user, ":")
	if len(parts) > 0 && parts[0] != "" {
		if uid, err := strconv.Atoi(parts[0]); err == nil {
			uidVal := uid
			uidPtr = &uidVal
		}
	}

	if len(parts) > 1 && parts[1] != "" {
		if gid, err := strconv.Atoi(parts[1]); err == nil {
			gidVal := gid
			gidPtr = &gidVal
		}
	}

	return user, uidPtr, gidPtr
}
