package oci

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/tinyrange/cc/internal/archive"
	"github.com/tinyrange/cc/internal/hv"
)

// Manifest types for OCI/Docker registry protocol

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

// Pull downloads an OCI image and returns it ready for use.
func (c *Client) Pull(imageRef string) (*Image, error) {
	return c.PullForArch(imageRef, hv.ArchitectureNative)
}

// PullForArch downloads an OCI image for a specific architecture.
func (c *Client) PullForArch(imageRef string, arch hv.CpuArchitecture) (*Image, error) {
	// Check if this is a local tar file
	if IsLocalTar(imageRef) {
		return c.LoadFromTar(imageRef, arch)
	}

	registry, imageName, tag, err := ParseImageRef(imageRef)
	if err != nil {
		return nil, fmt.Errorf("parse image ref %q: %w", imageRef, err)
	}

	ociArch, err := toOciArchitecture(arch)
	if err != nil {
		return nil, err
	}

	ctx := &registryContext{
		logger:   c.logger,
		client:   c.client,
		registry: registry,
	}

	// Create output directory for this image (include architecture in cache key)
	imageHash := sanitizeForFilename(imageRef + "-" + ociArch)
	outputDir := filepath.Join(c.cacheDir, "images", imageHash)
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return nil, fmt.Errorf("create output directory: %w", err)
	}

	// Check if already cached
	configPath := filepath.Join(outputDir, "config.json")
	if _, err := os.Stat(configPath); err == nil {
		return c.loadCachedImage(outputDir)
	}

	manifest, err := c.fetchManifestForArch(ctx, imageName, ociArch, tag)
	if err != nil {
		return nil, err
	}

	return c.processManifest(ctx, outputDir, imageName, manifest)
}

func (c *Client) loadCachedImage(dir string) (*Image, error) {
	return LoadFromDir(dir)
}

func toOciArchitecture(arch hv.CpuArchitecture) (string, error) {
	switch arch {
	case hv.ArchitectureX86_64:
		return "amd64", nil
	case hv.ArchitectureARM64:
		return "arm64", nil
	default:
		return "", fmt.Errorf("unsupported architecture: %s", arch)
	}
}

func (c *Client) fetchManifestForArch(ctx *registryContext, image string, arch string, tag string) (imageManifest, error) {
	indexPath := fmt.Sprintf("/%s/manifests/%s", image, tag)
	accept := []string{
		"application/vnd.docker.distribution.manifest.list.v2+json",
		"application/vnd.oci.image.index.v1+json",
		"application/vnd.docker.distribution.manifest.v2+json",
		"application/vnd.oci.image.manifest.v1+json",
	}

	cachePath, err := c.fetchToCache(ctx, indexPath, accept)
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
		return c.buildFromV1Index(ctx, image, arch, index1)
	}

	var index imageIndexV2
	if err := json.Unmarshal(data, &index); err != nil {
		return imageManifest{}, fmt.Errorf("decode image index: %w", err)
	}

	return c.buildFromIndex(ctx, image, arch, index)
}

func (c *Client) buildFromV1Index(ctx *registryContext, image string, arch string, index imageIndexV1) (imageManifest, error) {
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

func (c *Client) buildFromIndex(ctx *registryContext, image string, arch string, index imageIndexV2) (imageManifest, error) {
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
	_, err := c.readJSON(ctx,
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

func (c *Client) processManifest(ctx *registryContext, outputDir string, image string, manifest imageManifest) (*Image, error) {
	var cfg RuntimeConfig

	// Calculate total blob count for progress tracking
	blobCount := len(manifest.Layers)
	if manifest.Config.Digest != "" {
		blobCount++
	}
	blobIndex := 0

	if manifest.Config.Digest != "" {
		c.SetBlobContext(blobIndex, blobCount)
		blobIndex++
		configPath, err := c.fetchToCache(ctx, fmt.Sprintf("/%s/blobs/%s", image, manifest.Config.Digest), nil)
		if err != nil {
			return nil, fmt.Errorf("fetch image config %s: %w", manifest.Config.Digest, err)
		}

		f, err := os.Open(configPath)
		if err != nil {
			return nil, fmt.Errorf("open image config %s: %w", configPath, err)
		}

		var imageCfg imageConfig
		if err := json.NewDecoder(f).Decode(&imageCfg); err != nil {
			f.Close()
			return nil, fmt.Errorf("decode image config %s: %w", configPath, err)
		}
		f.Close()

		populateRuntimeConfig(&cfg, imageCfg)
	}

	img := &Image{
		Dir: outputDir,
	}

	for _, layer := range manifest.Layers {
		c.SetBlobContext(blobIndex, blobCount)
		blobIndex++
		layerPath, err := c.fetchToCache(ctx, fmt.Sprintf("/%s/blobs/%s", image, layer.Digest), nil)
		if err != nil {
			return nil, fmt.Errorf("fetch layer %s: %w", layer.Digest, err)
		}

		cfg.Layers = append(cfg.Layers, layer.Digest)

		compression, err := compressionFromMediaType(layer.MediaType)
		if err != nil {
			return nil, err
		}

		hash := strings.TrimPrefix(layer.Digest, "sha256:")
		indexPath := filepath.Join(outputDir, hash+".idx")
		contentsPath := filepath.Join(outputDir, hash+".contents")

		// Check if layer already processed
		if _, err := os.Stat(indexPath); err == nil {
			img.Layers = append(img.Layers, ImageLayer{
				Hash:         layer.Digest,
				IndexPath:    indexPath,
				ContentsPath: contentsPath,
			})
			continue
		}

		f, err := os.Open(layerPath)
		if err != nil {
			return nil, fmt.Errorf("open cached layer %s: %w", layer.Digest, err)
		}

		if err := makeLayerFromTar(hash, f, compression, outputDir); err != nil {
			f.Close()
			return nil, fmt.Errorf("process layer %s: %w", layer.Digest, err)
		}
		f.Close()

		img.Layers = append(img.Layers, ImageLayer{
			Hash:         layer.Digest,
			IndexPath:    indexPath,
			ContentsPath: contentsPath,
		})
	}

	img.Config = cfg

	// Write config.json
	configPath := filepath.Join(outputDir, "config.json")
	f, err := os.Create(configPath)
	if err != nil {
		return nil, fmt.Errorf("create config file: %w", err)
	}
	defer f.Close()

	encoder := json.NewEncoder(f)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(cfg); err != nil {
		return nil, fmt.Errorf("encode config: %w", err)
	}

	return img, nil
}

func makeLayerFromTar(hash string, r io.Reader, compression string, outputDir string) error {
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

	indexFile, err := os.Create(filepath.Join(outputDir, hash+".idx"))
	if err != nil {
		return fmt.Errorf("create layer index file: %w", err)
	}
	defer indexFile.Close()

	contentsFile, err := os.Create(filepath.Join(outputDir, hash+".contents"))
	if err != nil {
		return fmt.Errorf("create layer contents file: %w", err)
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

		// Handle OCI/overlayfs whiteout files.
		//
		// - ".wh.<name>" means <name> is deleted in this layer.
		// - ".wh..wh..opq" marks the containing directory as "opaque" (i.e. do not
		//   inherit lower-layer children). This does NOT delete the directory itself.
		//
		// We preserve the ".wh..wh..opq" marker as a metadata entry so the layered
		// filesystem view can apply opaque semantics during directory traversal.
		if path.Base(hdr.Name) == ".wh..wh..opq" {
			// Keep hdr.Name as-is; treat as metadata (no contents).
			deleted = false
		} else if strings.HasPrefix(path.Base(hdr.Name), ".wh.") {
			deleted = true
			hdr.Name = path.Join(path.Dir(hdr.Name), path.Base(hdr.Name)[4:])
		}

		info := hdr.FileInfo()

		var typeFlag archive.EntryKind

		switch hdr.Typeflag {
		case tar.TypeReg:
			typeFlag = archive.EntryKindRegular
		case tar.TypeDir:
			typeFlag = archive.EntryKindDirectory
		case tar.TypeChar:
			continue
		case tar.TypeBlock:
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
		} else if path.Base(hdr.Name) == ".wh..wh..opq" {
			// Record opaque directory marker as metadata.
			typeFlag = archive.EntryKindExtended
			hdr.Size = 0
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

func populateRuntimeConfig(cfg *RuntimeConfig, imageCfg imageConfig) {
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
	cfg.Architecture = imageCfg.Architecture

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

// dockerManifestEntry represents an entry in Docker's manifest.json
type dockerManifestEntry struct {
	Config       string                       `json:"Config"`
	RepoTags     []string                     `json:"RepoTags"`
	Layers       []string                     `json:"Layers"`
	LayerSources map[string]dockerLayerSource `json:"LayerSources,omitempty"`
}

// dockerLayerSource contains metadata about a layer
type dockerLayerSource struct {
	MediaType string `json:"mediaType"`
	Size      int64  `json:"size"`
	Digest    string `json:"digest"`
}

// LoadFromTar loads a Docker image from a tar archive created by `docker save`.
func (c *Client) LoadFromTar(tarPath string, arch hv.CpuArchitecture) (*Image, error) {
	// Resolve the tar path
	if strings.HasPrefix(tarPath, "./") {
		wd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("get working directory: %w", err)
		}
		tarPath = filepath.Join(wd, tarPath[2:])
	}
	tarPath, err := filepath.Abs(tarPath)
	if err != nil {
		return nil, fmt.Errorf("resolve tar path: %w", err)
	}

	// Create output directory for this image
	imageHash := sanitizeForFilename(tarPath)
	outputDir := filepath.Join(c.cacheDir, "images", imageHash)
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return nil, fmt.Errorf("create output directory: %w", err)
	}

	// Check if already cached
	configPath := filepath.Join(outputDir, "config.json")
	if _, err := os.Stat(configPath); err == nil {
		return c.loadCachedImage(outputDir)
	}

	// Open the tar file
	tarFile, err := os.Open(tarPath)
	if err != nil {
		return nil, fmt.Errorf("open tar file %s: %w", tarPath, err)
	}
	defer tarFile.Close()

	tr := tar.NewReader(tarFile)

	// Read manifest.json
	var manifestEntries []dockerManifestEntry
	var manifestData []byte
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read tar header: %w", err)
		}

		if hdr.Name == "manifest.json" {
			manifestData, err = io.ReadAll(tr)
			if err != nil {
				return nil, fmt.Errorf("read manifest.json: %w", err)
			}
			if err := json.Unmarshal(manifestData, &manifestEntries); err != nil {
				return nil, fmt.Errorf("parse manifest.json: %w", err)
			}
			break
		}
	}

	if len(manifestEntries) == 0 {
		return nil, fmt.Errorf("no images found in tar archive")
	}

	// Use the first image entry
	entry := manifestEntries[0]

	// Reopen tar to read image config and layers
	tarFile.Close()
	tarFile, err = os.Open(tarPath)
	if err != nil {
		return nil, fmt.Errorf("reopen tar file: %w", err)
	}
	defer tarFile.Close()

	tr = tar.NewReader(tarFile)

	// Read image config (json file)
	var imageCfg imageConfig
	var configRead bool
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read tar header: %w", err)
		}

		if hdr.Name == entry.Config {
			if err := json.NewDecoder(tr).Decode(&imageCfg); err != nil {
				return nil, fmt.Errorf("decode image config: %w", err)
			}
			configRead = true
			break
		}
	}

	if !configRead {
		return nil, fmt.Errorf("image config %s not found in tar", entry.Config)
	}

	// Build runtime config
	var cfg RuntimeConfig
	populateRuntimeConfig(&cfg, imageCfg)

	// Process layers
	img := &Image{
		Dir: outputDir,
	}

	// Reopen tar again to read layers
	tarFile.Close()
	tarFile, err = os.Open(tarPath)
	if err != nil {
		return nil, fmt.Errorf("reopen tar file for layers: %w", err)
	}
	defer tarFile.Close()

	tr = tar.NewReader(tarFile)

	// Create a set of layer paths for quick lookup, and a map to store processed layers
	layerSet := make(map[string]bool)
	processedLayers := make(map[string]ImageLayer)
	for _, layerPath := range entry.Layers {
		layerSet[layerPath] = false // false = not yet processed
	}

	// Process layers one at a time as we encounter them in the tar
	// Note: We process in tar order but will reorder according to manifest later
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read tar header: %w", err)
		}

		// Check if this is a layer file we need to process
		if _, isLayer := layerSet[hdr.Name]; !isLayer {
			continue
		}

		// Read layer data (one layer at a time, not all layers)
		data, err := io.ReadAll(tr)
		if err != nil {
			return nil, fmt.Errorf("read layer %s: %w", hdr.Name, err)
		}

		// Determine compression from LayerSources if available
		compression := "gzip" // default to gzip for older format
		if entry.LayerSources != nil {
			// Extract digest from layer path (format: blobs/sha256/<hash>)
			layerHash := strings.TrimPrefix(hdr.Name, "blobs/sha256/")
			if layerSource, ok := entry.LayerSources["sha256:"+layerHash]; ok {
				compressionType, err := compressionFromMediaType(layerSource.MediaType)
				if err == nil {
					compression = compressionType
				}
			}
		}

		// Generate a hash for this layer
		hash := fmt.Sprintf("sha256:%x", sha256.Sum256(data))
		hashPrefix := strings.TrimPrefix(hash, "sha256:")

		indexPath := filepath.Join(outputDir, hashPrefix+".idx")
		contentsPath := filepath.Join(outputDir, hashPrefix+".contents")

		// Check if layer already processed (cached)
		if _, err := os.Stat(indexPath); err == nil {
			processedLayers[hdr.Name] = ImageLayer{
				Hash:         hash,
				IndexPath:    indexPath,
				ContentsPath: contentsPath,
			}
			layerSet[hdr.Name] = true // mark as processed
			continue
		}

		// Process layer tar
		layerReader := bytes.NewReader(data)
		if err := makeLayerFromTar(hashPrefix, layerReader, compression, outputDir); err != nil {
			return nil, fmt.Errorf("process layer %s: %w", hdr.Name, err)
		}

		processedLayers[hdr.Name] = ImageLayer{
			Hash:         hash,
			IndexPath:    indexPath,
			ContentsPath: contentsPath,
		}
		layerSet[hdr.Name] = true // mark as processed
	}

	// Verify all layers were found and build the final layer list in manifest order
	for _, layerPath := range entry.Layers {
		if !layerSet[layerPath] {
			return nil, fmt.Errorf("layer %s not found in tar", layerPath)
		}
		layer := processedLayers[layerPath]
		cfg.Layers = append(cfg.Layers, layer.Hash)
		img.Layers = append(img.Layers, layer)
	}

	img.Config = cfg

	// Write config.json
	f, err := os.Create(configPath)
	if err != nil {
		return nil, fmt.Errorf("create config file: %w", err)
	}
	defer f.Close()

	encoder := json.NewEncoder(f)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(cfg); err != nil {
		return nil, fmt.Errorf("encode config: %w", err)
	}

	return img, nil
}
