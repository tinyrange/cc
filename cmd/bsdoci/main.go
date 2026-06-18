package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/ulikunitz/xz"
	"j5.nz/cc/internal/managed/release"
)

const (
	ociImageManifestMediaType = "application/vnd.oci.image.manifest.v1+json"
	ociImageConfigMediaType   = "application/vnd.oci.image.config.v1+json"
	ociLayerTarGzipMediaType  = "application/vnd.oci.image.layer.v1.tar+gzip"
	bsdKernelMediaType        = "application/vnd.tinyrange.bsd.kernel.v1"
)

type options struct {
	family         string
	version        string
	arch           string
	mirror         string
	cacheDir       string
	sourceDir      string
	trimProfile    string
	sourceCacheOut string
	out            string
	pushRef        string
	dryRun         bool
	plainHTTP      bool
}

type sourceSpec struct {
	family     string
	version    string
	arch       string
	mirror     string
	kernelName string
	kernelURL  string
	rootName   string
	rootComp   compression
	rootSets   []rootSet
}

type rootSet struct {
	name string
	url  string
	comp compression
}

type compression string

const (
	compressionGzip compression = "gzip"
	compressionXZ   compression = "xz"
)

type descriptor struct {
	MediaType   string            `json:"mediaType"`
	Digest      string            `json:"digest"`
	Size        int64             `json:"size"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

type manifest struct {
	SchemaVersion int               `json:"schemaVersion"`
	MediaType     string            `json:"mediaType"`
	Config        descriptor        `json:"config"`
	Layers        []descriptor      `json:"layers"`
	Annotations   map[string]string `json:"annotations,omitempty"`
}

type index struct {
	SchemaVersion int               `json:"schemaVersion"`
	MediaType     string            `json:"mediaType"`
	Manifests     []indexDescriptor `json:"manifests"`
}

type indexDescriptor struct {
	descriptor
	Platform platform `json:"platform"`
}

type platform struct {
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
}

type imageConfig struct {
	Architecture string            `json:"architecture"`
	OS           string            `json:"os"`
	Created      string            `json:"created"`
	Config       imageConfigFields `json:"config"`
	RootFS       imageRootFS       `json:"rootfs"`
	Labels       map[string]string `json:"-"`
}

type imageConfigFields struct {
	Labels map[string]string `json:"Labels,omitempty"`
}

type imageRootFS struct {
	Type    string   `json:"type"`
	DiffIDs []string `json:"diff_ids"`
}

type artifact struct {
	spec         sourceSpec
	rootLayer    blob
	kernelBlob   blob
	configBlob   blob
	manifestBlob blob
}

type blob struct {
	mediaType string
	digest    string
	size      int64
	path      string
	data      []byte
}

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "bsdoci: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout io.Writer) error {
	opts, err := parseOptions(args)
	if err != nil {
		return err
	}
	spec, err := resolveSpec(opts)
	if err != nil {
		return err
	}
	if opts.cacheDir == "" {
		opts.cacheDir = filepath.Join(os.TempDir(), "cc-bsdoci")
	}
	workDir := filepath.Join(opts.cacheDir, "work", spec.family, spec.version, spec.arch)
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return fmt.Errorf("create work dir: %w", err)
	}

	kernelSource, rootSources, err := ensureSources(ctx, opts, spec)
	if err != nil {
		return err
	}
	trim := trimRulesForProfile(spec.family, opts.trimProfile)
	art, err := buildArtifact(ctx, spec, kernelSource, rootSources, workDir, trim)
	if err != nil {
		return err
	}
	if opts.sourceCacheOut != "" {
		if err := writeSourceCache(ctx, spec, kernelSource, rootSources, opts.sourceCacheOut, trim); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "wrote source cache: %s\n", opts.sourceCacheOut)
	}
	if opts.out != "" {
		if err := writeOCILayout(opts.out, art); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "wrote OCI layout: %s\n", opts.out)
	}
	if opts.pushRef != "" {
		if err := pushArtifact(ctx, opts.pushRef, art, pushOptions{dryRun: opts.dryRun, plainHTTP: opts.plainHTTP}, stdout); err != nil {
			return err
		}
	}
	if opts.out == "" && opts.pushRef == "" {
		fmt.Fprintf(stdout, "built %s %s %s\n", spec.family, spec.version, spec.arch)
		fmt.Fprintf(stdout, "rootfs %s %d bytes\n", art.rootLayer.digest, art.rootLayer.size)
		fmt.Fprintf(stdout, "kernel %s %d bytes\n", art.kernelBlob.digest, art.kernelBlob.size)
	}
	return nil
}

func parseOptions(args []string) (options, error) {
	var opts options
	fs := flag.NewFlagSet("bsdoci", flag.ContinueOnError)
	fs.StringVar(&opts.family, "family", "", "BSD family: openbsd, freebsd, or netbsd")
	fs.StringVar(&opts.version, "version", "", "BSD release version")
	fs.StringVar(&opts.arch, "arch", "", "BSD guest architecture")
	fs.StringVar(&opts.mirror, "mirror", "", "upstream mirror base URL")
	fs.StringVar(&opts.cacheDir, "cache-dir", "", "download and build cache directory")
	fs.StringVar(&opts.sourceDir, "source-dir", "", "directory containing predownloaded source artifacts")
	fs.StringVar(&opts.trimProfile, "trim-profile", "", "trim profile: empty/none or runtime")
	fs.StringVar(&opts.sourceCacheOut, "source-cache-out", "", "write trimmed release-set artifacts to this cache directory for boot verification")
	fs.StringVar(&opts.out, "out", "", "write an OCI image layout to this directory")
	fs.StringVar(&opts.pushRef, "push", "", "push to an OCI registry reference, for example localhost:5000/tinyrange/cc-openbsd:7.9")
	fs.BoolVar(&opts.dryRun, "dry-run", false, "print push operations without contacting the registry")
	fs.BoolVar(&opts.plainHTTP, "plain-http", false, "use http for registry push")
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	if fs.NArg() != 0 {
		return options{}, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	opts.family = strings.ToLower(strings.TrimSpace(opts.family))
	if opts.family == "" {
		return options{}, fmt.Errorf("--family is required")
	}
	opts.trimProfile = strings.ToLower(strings.TrimSpace(opts.trimProfile))
	if opts.trimProfile == "none" {
		opts.trimProfile = ""
	}
	if opts.trimProfile != "" && opts.trimProfile != "runtime" {
		return options{}, fmt.Errorf("unsupported --trim-profile %q", opts.trimProfile)
	}
	return opts, nil
}

func resolveSpec(opts options) (sourceSpec, error) {
	spec := sourceSpec{family: opts.family, version: opts.version, arch: opts.arch, mirror: strings.TrimRight(opts.mirror, "/")}
	switch spec.family {
	case "openbsd":
		if spec.version == "" {
			spec.version = "7.9"
		}
		if spec.arch == "" {
			spec.arch = "amd64"
		}
		if spec.mirror == "" {
			spec.mirror = "https://mirror.aarnet.edu.au/pub/OpenBSD"
		}
		noDot := strings.ReplaceAll(spec.version, ".", "")
		spec.kernelName = "bsd"
		spec.kernelURL = spec.version + "/" + spec.arch + "/bsd"
		spec.rootName = "base" + noDot + ".tgz"
		spec.rootComp = compressionGzip
		spec.rootSets = []rootSet{
			{name: spec.rootName, url: spec.version + "/" + spec.arch + "/" + spec.rootName, comp: compressionGzip},
			{name: "comp" + noDot + ".tgz", url: spec.version + "/" + spec.arch + "/comp" + noDot + ".tgz", comp: compressionGzip},
		}
	case "freebsd":
		if spec.version == "" {
			spec.version = "15.1"
		}
		if spec.arch == "" {
			spec.arch = "amd64"
		}
		if spec.mirror == "" {
			spec.mirror = "https://mirror.aarnet.edu.au/pub/FreeBSD/releases"
		}
		spec.kernelName = "kernel.txz"
		spec.kernelURL = spec.arch + "/" + spec.version + "-RELEASE/kernel.txz"
		spec.rootName = "base.txz"
		spec.rootComp = compressionXZ
		spec.rootSets = []rootSet{
			{name: spec.rootName, url: spec.arch + "/" + spec.version + "-RELEASE/base.txz", comp: compressionXZ},
		}
	case "netbsd":
		if spec.version == "" {
			spec.version = "10.1"
		}
		if spec.arch == "" {
			spec.arch = "amd64"
		}
		if spec.mirror == "" {
			spec.mirror = "https://mirror.aarnet.edu.au/pub/netbsd"
		}
		spec.kernelName = "netbsd-GENERIC.gz"
		if spec.arch == "evbarm-aarch64" {
			spec.kernelName = "netbsd-GENERIC64.img.gz"
		}
		spec.kernelURL = "NetBSD-" + spec.version + "/" + spec.arch + "/binary/kernel/" + spec.kernelName
		spec.rootName = "base.tar.xz"
		spec.rootComp = compressionXZ
		spec.rootSets = []rootSet{
			{name: spec.rootName, url: "NetBSD-" + spec.version + "/" + spec.arch + "/binary/sets/base.tar.xz", comp: compressionXZ},
			{name: "comp.tar.xz", url: "NetBSD-" + spec.version + "/" + spec.arch + "/binary/sets/comp.tar.xz", comp: compressionXZ},
		}
	default:
		return sourceSpec{}, fmt.Errorf("unsupported --family %q", opts.family)
	}
	return spec, nil
}

func ensureSources(ctx context.Context, opts options, spec sourceSpec) (kernelPath string, rootPaths []string, err error) {
	if opts.sourceDir != "" {
		kernelPath = filepath.Join(opts.sourceDir, spec.kernelName)
		if err := requireFile(kernelPath); err != nil {
			return "", nil, err
		}
		for _, set := range spec.rootSets {
			rootPath := filepath.Join(opts.sourceDir, set.name)
			if err := requireFile(rootPath); err != nil {
				return "", nil, err
			}
			rootPaths = append(rootPaths, rootPath)
		}
		return kernelPath, rootPaths, nil
	}
	cacheDir := filepath.Join(opts.cacheDir, "sources")
	kernelPath, err = release.EnsureArtifact(ctx, release.Artifact{
		CacheDir: cacheDir,
		Family:   spec.family,
		Version:  spec.version,
		Arch:     spec.arch,
		Mirror:   spec.mirror,
		Name:     spec.kernelName,
		URLPath:  spec.kernelURL,
	})
	if err != nil {
		return "", nil, fmt.Errorf("ensure kernel source: %w", err)
	}
	for _, set := range spec.rootSets {
		rootPath, err := release.EnsureArtifact(ctx, release.Artifact{
			CacheDir: cacheDir,
			Family:   spec.family,
			Version:  spec.version,
			Arch:     spec.arch,
			Mirror:   spec.mirror,
			Name:     set.name,
			URLPath:  set.url,
		})
		if err != nil {
			return "", nil, fmt.Errorf("ensure rootfs source %s: %w", set.name, err)
		}
		rootPaths = append(rootPaths, rootPath)
	}
	return kernelPath, rootPaths, nil
}

func requireFile(path string) error {
	st, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("required source %s: %w", path, err)
	}
	if st.Size() == 0 {
		return fmt.Errorf("required source %s is empty", path)
	}
	return nil
}

func buildArtifact(ctx context.Context, spec sourceSpec, kernelSource string, rootSources []string, workDir string, trim trimRules) (artifact, error) {
	rootLayerPath := filepath.Join(workDir, "rootfs.tar.gz")
	rootDiffID, err := writeRootLayer(ctx, spec, rootSources, rootLayerPath, trim)
	if err != nil {
		return artifact{}, err
	}
	kernelBytes, err := readKernel(spec.family, kernelSource)
	if err != nil {
		return artifact{}, err
	}
	kernelPath := filepath.Join(workDir, "kernel")
	if err := os.WriteFile(kernelPath, kernelBytes, 0o644); err != nil {
		return artifact{}, fmt.Errorf("write kernel blob: %w", err)
	}
	rootBlob, err := fileBlob(rootLayerPath, ociLayerTarGzipMediaType)
	if err != nil {
		return artifact{}, err
	}
	kernelBlob, err := fileBlob(kernelPath, bsdKernelMediaType)
	if err != nil {
		return artifact{}, err
	}
	labels := artifactLabels(spec, kernelSource, rootSources)
	cfg := imageConfig{
		Architecture: ociArch(spec.arch),
		OS:           "unknown",
		Created:      time.Unix(0, 0).UTC().Format(time.RFC3339),
		Config:       imageConfigFields{Labels: labels},
		RootFS:       imageRootFS{Type: "layers", DiffIDs: []string{rootDiffID}},
	}
	configData, err := json.Marshal(cfg)
	if err != nil {
		return artifact{}, fmt.Errorf("marshal config: %w", err)
	}
	configBlob := dataBlob(configData, ociImageConfigMediaType)
	m := manifest{
		SchemaVersion: 2,
		MediaType:     ociImageManifestMediaType,
		Config:        configBlob.descriptor(),
		Layers: []descriptor{
			withAnnotations(rootBlob.descriptor(), map[string]string{
				"io.tinyrange.bsd.layer.kind":    "rootfs",
				"io.tinyrange.bsd.rootfs.format": "tar+gzip",
			}),
			withAnnotations(kernelBlob.descriptor(), map[string]string{
				"io.tinyrange.bsd.layer.kind": "kernel",
			}),
		},
		Annotations: labels,
	}
	manifestData, err := json.Marshal(m)
	if err != nil {
		return artifact{}, fmt.Errorf("marshal manifest: %w", err)
	}
	return artifact{
		spec:         spec,
		rootLayer:    rootBlob,
		kernelBlob:   kernelBlob,
		configBlob:   configBlob,
		manifestBlob: dataBlob(manifestData, ociImageManifestMediaType),
	}, nil
}

func artifactLabels(spec sourceSpec, kernelSource string, rootSources []string) map[string]string {
	labels := map[string]string{
		"org.opencontainers.image.source":       "https://github.com/tinyrange/vmsh",
		"org.opencontainers.image.version":      spec.version,
		"io.tinyrange.bsd.family":               spec.family,
		"io.tinyrange.bsd.version":              spec.version,
		"io.tinyrange.bsd.guest_arch":           spec.arch,
		"io.tinyrange.bsd.kernel.media_type":    bsdKernelMediaType,
		"io.tinyrange.bsd.rootfs.format":        "tar+gzip",
		"io.tinyrange.bsd.source.mirror":        spec.mirror,
		"io.tinyrange.bsd.source.kernel_sha256": fileSHA256Label(kernelSource),
		"io.tinyrange.bsd.source.rootfs_sha256": strings.Join(fileSHA256Labels(rootSources), ","),
	}
	return labels
}

func fileSHA256Labels(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		out = append(out, fileSHA256Label(path))
	}
	return out
}

func fileSHA256Label(path string) string {
	digest, err := fileSHA256(path)
	if err != nil {
		return ""
	}
	return digest
}

func writeRootLayer(ctx context.Context, spec sourceSpec, sources []string, target string, trim trimRules) (string, error) {
	return writeFilteredTar(ctx, sources, spec.rootSets, target, compressionGzip, trim, true)
}

func writeSourceCache(ctx context.Context, spec sourceSpec, kernelSource string, rootSources []string, cacheDir string, trim trimRules) error {
	dir := filepath.Join(cacheDir, spec.family, spec.version, spec.arch)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create source cache dir: %w", err)
	}
	if err := copyFile(kernelSource, filepath.Join(dir, spec.kernelName)); err != nil {
		return fmt.Errorf("copy kernel source: %w", err)
	}
	if _, err := writeFilteredTar(ctx, rootSources, spec.rootSets, filepath.Join(dir, spec.rootName), spec.rootComp, trim, false); err != nil {
		return fmt.Errorf("write trimmed root source: %w", err)
	}
	return nil
}

func writeFilteredTar(ctx context.Context, sources []string, sets []rootSet, target string, targetComp compression, trim trimRules, computeDiffID bool) (string, error) {
	if len(sources) != len(sets) {
		return "", fmt.Errorf("root source count %d does not match set count %d", len(sources), len(sets))
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", fmt.Errorf("create root layer dir: %w", err)
	}
	out, err := os.Create(target)
	if err != nil {
		return "", fmt.Errorf("create root layer: %w", err)
	}
	var compressed io.WriteCloser
	switch targetComp {
	case compressionGzip:
		gzw := gzip.NewWriter(out)
		gzw.Name = "rootfs.tar"
		gzw.ModTime = time.Unix(0, 0)
		compressed = gzw
	case compressionXZ:
		xzw, err := xz.NewWriter(out)
		if err != nil {
			_ = out.Close()
			return "", fmt.Errorf("create root xz writer: %w", err)
		}
		compressed = xzw
	default:
		_ = out.Close()
		return "", fmt.Errorf("unsupported target compression %q", targetComp)
	}
	diffHash := sha256.New()
	tarOut := io.Writer(compressed)
	if computeDiffID {
		tarOut = io.MultiWriter(compressed, diffHash)
	}
	tw := tar.NewWriter(tarOut)
	for i, source := range sources {
		if err := appendFilteredTarSource(ctx, tw, source, sets[i].comp, trim); err != nil {
			_ = tw.Close()
			_ = compressed.Close()
			_ = out.Close()
			return "", err
		}
	}
	if err := tw.Close(); err != nil {
		_ = compressed.Close()
		_ = out.Close()
		return "", fmt.Errorf("close rootfs tar: %w", err)
	}
	if err := compressed.Close(); err != nil {
		_ = out.Close()
		return "", fmt.Errorf("close rootfs compression: %w", err)
	}
	if err := out.Close(); err != nil {
		return "", fmt.Errorf("close root layer: %w", err)
	}
	if !computeDiffID {
		return "", nil
	}
	return "sha256:" + hex.EncodeToString(diffHash.Sum(nil)), nil
}

func appendFilteredTarSource(ctx context.Context, tw *tar.Writer, source string, sourceComp compression, trim trimRules) error {
	in, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open rootfs source %s: %w", source, err)
	}
	defer in.Close()
	var r io.Reader = in
	var closer io.Closer
	switch sourceComp {
	case compressionGzip:
		gz, err := gzip.NewReader(in)
		if err != nil {
			return fmt.Errorf("open rootfs gzip %s: %w", source, err)
		}
		closer = gz
		r = gz
	case compressionXZ:
		xzr, err := xz.NewReader(in)
		if err != nil {
			return fmt.Errorf("open rootfs xz %s: %w", source, err)
		}
		r = xzr
	default:
		return fmt.Errorf("unsupported rootfs compression %q", sourceComp)
	}
	if closer != nil {
		defer closer.Close()
	}
	tr := tar.NewReader(r)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read rootfs tar %s: %w", source, err)
		}
		normalizeTarHeader(hdr)
		if trim.Skip(hdr.Name) {
			continue
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return fmt.Errorf("write rootfs tar header %s: %w", hdr.Name, err)
		}
		if hdr.Typeflag == tar.TypeReg || hdr.Typeflag == tar.TypeRegA {
			if _, err := io.Copy(tw, tr); err != nil {
				return fmt.Errorf("write rootfs tar entry %s: %w", hdr.Name, err)
			}
		}
	}
}

func normalizeTarHeader(hdr *tar.Header) {
	hdr.Name = cleanTarName(hdr.Name)
	hdr.ModTime = time.Unix(0, 0)
	hdr.AccessTime = time.Unix(0, 0)
	hdr.ChangeTime = time.Unix(0, 0)
	hdr.PAXRecords = nil
	hdr.Format = tar.FormatPAX
}

func cleanTarName(name string) string {
	clean := path.Clean("/" + strings.TrimPrefix(name, "/"))
	if clean == "/" {
		return "."
	}
	return strings.TrimPrefix(clean, "/")
}

type trimRules struct {
	family  string
	profile string
}

func trimRulesForProfile(family, profile string) trimRules {
	return trimRules{family: family, profile: profile}
}

func (r trimRules) Skip(name string) bool {
	if r.profile == "" {
		return false
	}
	clean := cleanTarName(name)
	if clean == "." {
		return false
	}
	if strings.HasPrefix(clean, "usr/share/relink/") || clean == "usr/share/relink" {
		return true
	}
	if skipTrimPrefix(clean,
		"usr/share/man",
		"usr/local/man",
		"usr/share/doc",
		"usr/share/info",
		"usr/share/examples",
		"usr/share/openssl/man",
		"usr/tests",
		"usr/share/locale",
	) {
		return true
	}
	switch r.family {
	case "freebsd":
		if skipTrimPrefix(clean, "var/db/etcupdate/current/usr/share/man", "var/db/etcupdate/current/usr/share/examples") {
			return true
		}
	case "netbsd":
		if skipTrimPrefix(clean, "usr/share/doc/reference") {
			return true
		}
	}
	return false
}

func skipTrimPrefix(clean string, prefixes ...string) bool {
	for _, prefix := range prefixes {
		if clean == prefix || strings.HasPrefix(clean, prefix+"/") {
			return true
		}
	}
	return false
}

func readKernel(family, source string) ([]byte, error) {
	switch family {
	case "openbsd":
		data, err := os.ReadFile(source)
		if err != nil {
			return nil, fmt.Errorf("read OpenBSD kernel: %w", err)
		}
		return data, nil
	case "freebsd":
		return readKernelFromXZTar(source, "boot/kernel/kernel")
	case "netbsd":
		data, err := os.ReadFile(source)
		if err != nil {
			return nil, fmt.Errorf("read NetBSD kernel: %w", err)
		}
		if len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b {
			gz, err := gzip.NewReader(bytes.NewReader(data))
			if err != nil {
				return nil, fmt.Errorf("open NetBSD kernel gzip: %w", err)
			}
			defer gz.Close()
			data, err = io.ReadAll(gz)
			if err != nil {
				return nil, fmt.Errorf("decompress NetBSD kernel: %w", err)
			}
		}
		return data, nil
	default:
		return nil, fmt.Errorf("unsupported kernel family %q", family)
	}
}

func readKernelFromXZTar(source, want string) ([]byte, error) {
	in, err := os.Open(source)
	if err != nil {
		return nil, fmt.Errorf("open FreeBSD kernel set: %w", err)
	}
	defer in.Close()
	xzr, err := xz.NewReader(in)
	if err != nil {
		return nil, fmt.Errorf("open FreeBSD kernel xz: %w", err)
	}
	tr := tar.NewReader(xzr)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("FreeBSD kernel set does not contain %s", want)
		}
		if err != nil {
			return nil, fmt.Errorf("read FreeBSD kernel set: %w", err)
		}
		if cleanTarName(hdr.Name) != want {
			continue
		}
		return io.ReadAll(tr)
	}
}

func writeOCILayout(dir string, art artifact) error {
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("remove old OCI layout: %w", err)
	}
	blobDir := filepath.Join(dir, "blobs", "sha256")
	if err := os.MkdirAll(blobDir, 0o755); err != nil {
		return fmt.Errorf("create OCI layout: %w", err)
	}
	for _, blob := range art.blobs() {
		if err := installBlob(blobDir, blob); err != nil {
			return err
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "oci-layout"), []byte(`{"imageLayoutVersion":"1.0.0"}`+"\n"), 0o644); err != nil {
		return fmt.Errorf("write oci-layout: %w", err)
	}
	idx := index{
		SchemaVersion: 2,
		MediaType:     "application/vnd.oci.image.index.v1+json",
		Manifests: []indexDescriptor{{
			descriptor: withAnnotations(art.manifestBlob.descriptor(), map[string]string{
				"org.opencontainers.image.ref.name": art.spec.version,
			}),
			Platform: platform{OS: "unknown", Architecture: ociArch(art.spec.arch)},
		}},
	}
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal OCI index: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.json"), append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write OCI index: %w", err)
	}
	return nil
}

func installBlob(blobDir string, blob blob) error {
	target := filepath.Join(blobDir, strings.TrimPrefix(blob.digest, "sha256:"))
	if blob.path != "" {
		return copyFile(blob.path, target)
	}
	return os.WriteFile(target, blob.data, 0o644)
}

func copyFile(source, target string) error {
	in, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open blob source: %w", err)
	}
	defer in.Close()
	out, err := os.Create(target)
	if err != nil {
		return fmt.Errorf("create blob: %w", err)
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return fmt.Errorf("copy blob: %w", err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close blob: %w", err)
	}
	return nil
}

type pushOptions struct {
	dryRun    bool
	plainHTTP bool
}

func pushArtifact(ctx context.Context, ref string, art artifact, opts pushOptions, stdout io.Writer) error {
	target, err := parseRegistryRef(ref, opts.plainHTTP)
	if err != nil {
		return err
	}
	for _, blob := range []blob{art.configBlob, art.rootLayer, art.kernelBlob} {
		if opts.dryRun {
			fmt.Fprintf(stdout, "dry-run push blob %s %s %d bytes\n", blob.mediaType, blob.digest, blob.size)
			continue
		}
		if err := target.pushBlob(ctx, blob); err != nil {
			return err
		}
	}
	if opts.dryRun {
		fmt.Fprintf(stdout, "dry-run push manifest %s %s %d bytes\n", ref, art.manifestBlob.digest, art.manifestBlob.size)
		return nil
	}
	return target.pushManifest(ctx, art.manifestBlob)
}

type registryTarget struct {
	baseURL     string
	repo        string
	tag         string
	client      *http.Client
	authUser    string
	authToken   string
	bearerToken string
}

func parseRegistryRef(ref string, plainHTTP bool) (registryTarget, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return registryTarget{}, fmt.Errorf("registry reference is required")
	}
	slash := strings.Index(ref, "/")
	if slash <= 0 {
		return registryTarget{}, fmt.Errorf("registry reference must include host/repo:tag")
	}
	host := ref[:slash]
	repoTag := ref[slash+1:]
	colon := strings.LastIndex(repoTag, ":")
	if colon <= 0 || colon == len(repoTag)-1 {
		return registryTarget{}, fmt.Errorf("registry reference must include a tag")
	}
	repo := repoTag[:colon]
	tag := repoTag[colon+1:]
	scheme := "https"
	if plainHTTP || strings.HasPrefix(host, "localhost") || strings.HasPrefix(host, "127.") {
		scheme = "http"
	}
	return registryTarget{
		baseURL:   scheme + "://" + host,
		repo:      strings.Trim(repo, "/"),
		tag:       tag,
		client:    http.DefaultClient,
		authUser:  registryAuthUser(),
		authToken: registryAuthToken(),
	}, nil
}

func (t registryTarget) pushBlob(ctx context.Context, blob blob) error {
	if err := t.ensureBearerToken(ctx); err != nil {
		return err
	}
	postURL := fmt.Sprintf("%s/v2/%s/blobs/uploads/", t.baseURL, t.repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, postURL, nil)
	if err != nil {
		return err
	}
	t.authorize(req)
	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("start blob upload %s: %w", blob.digest, err)
	}
	resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("start blob upload %s: %s", blob.digest, resp.Status)
	}
	loc := resp.Header.Get("Location")
	if loc == "" {
		return fmt.Errorf("registry did not return blob upload location")
	}
	uploadURL, err := resolveLocation(t.baseURL, loc)
	if err != nil {
		return err
	}
	q := uploadURL.Query()
	q.Set("digest", blob.digest)
	uploadURL.RawQuery = q.Encode()
	body, err := blob.reader()
	if err != nil {
		return err
	}
	defer body.Close()
	req, err = http.NewRequestWithContext(ctx, http.MethodPut, uploadURL.String(), body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.ContentLength = blob.size
	t.authorize(req)
	resp, err = t.client.Do(req)
	if err != nil {
		return fmt.Errorf("complete blob upload %s: %w", blob.digest, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("complete blob upload %s: %s", blob.digest, resp.Status)
	}
	return nil
}

func (t registryTarget) pushManifest(ctx context.Context, blob blob) error {
	if err := t.ensureBearerToken(ctx); err != nil {
		return err
	}
	body, err := blob.reader()
	if err != nil {
		return err
	}
	defer body.Close()
	manifestURL := fmt.Sprintf("%s/v2/%s/manifests/%s", t.baseURL, t.repo, t.tag)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, manifestURL, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", ociImageManifestMediaType)
	req.ContentLength = blob.size
	t.authorize(req)
	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("push manifest: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("push manifest: %s", resp.Status)
	}
	return nil
}

func (t *registryTarget) ensureBearerToken(ctx context.Context) error {
	if t.bearerToken != "" || t.authToken == "" {
		return nil
	}
	u, err := url.Parse(t.baseURL)
	if err != nil {
		return err
	}
	if u.Host != "ghcr.io" {
		return nil
	}
	user := t.authUser
	if user == "" {
		user = "oauth2"
	}
	tokenURL := fmt.Sprintf("%s/token?service=ghcr.io&scope=%s", t.baseURL, url.QueryEscape("repository:"+t.repo+":pull,push"))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, tokenURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(user+":"+t.authToken)))
	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("request GHCR bearer token: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("request GHCR bearer token: %s", resp.Status)
	}
	var tr struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return fmt.Errorf("decode GHCR bearer token: %w", err)
	}
	if tr.Token == "" {
		return fmt.Errorf("GHCR bearer token response did not include a token")
	}
	t.bearerToken = tr.Token
	return nil
}

func (t registryTarget) authorize(req *http.Request) {
	if t.bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+t.bearerToken)
	}
}

func registryAuthUser() string {
	for _, key := range []string{"GHCR_USER", "GITHUB_ACTOR"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func registryAuthToken() string {
	for _, key := range []string{"GHCR_TOKEN", "GITHUB_TOKEN", "CR_PAT"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func resolveLocation(base, raw string) (*url.URL, error) {
	loc, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse upload location: %w", err)
	}
	if loc.IsAbs() {
		return loc, nil
	}
	baseURL, err := url.Parse(base)
	if err != nil {
		return nil, err
	}
	return baseURL.ResolveReference(loc), nil
}

func (a artifact) blobs() []blob {
	return []blob{a.configBlob, a.rootLayer, a.kernelBlob, a.manifestBlob}
}

func (b blob) descriptor() descriptor {
	return descriptor{MediaType: b.mediaType, Digest: b.digest, Size: b.size}
}

func withAnnotations(desc descriptor, annotations map[string]string) descriptor {
	desc.Annotations = annotations
	return desc
}

func (b blob) reader() (io.ReadCloser, error) {
	if b.path != "" {
		return os.Open(b.path)
	}
	return io.NopCloser(bytes.NewReader(b.data)), nil
}

func fileBlob(path, mediaType string) (blob, error) {
	digest, err := fileSHA256(path)
	if err != nil {
		return blob{}, err
	}
	st, err := os.Stat(path)
	if err != nil {
		return blob{}, fmt.Errorf("stat blob: %w", err)
	}
	return blob{mediaType: mediaType, digest: "sha256:" + digest, size: st.Size(), path: path}, nil
}

func dataBlob(data []byte, mediaType string) blob {
	sum := sha256.Sum256(data)
	return blob{mediaType: mediaType, digest: "sha256:" + hex.EncodeToString(sum[:]), size: int64(len(data)), data: data}
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open for sha256: %w", err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hash %s: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func ociArch(guestArch string) string {
	switch guestArch {
	case "evbarm-aarch64", "arm64", "aarch64":
		return "arm64"
	default:
		return guestArch
	}
}
