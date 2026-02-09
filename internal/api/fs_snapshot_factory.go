package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/tinyrange/cc/internal/fslayer"
	"github.com/tinyrange/cc/internal/oci"
)

// FSLayerOp represents an operation in the filesystem snapshot factory.
type FSLayerOp interface {
	// CacheKey returns a deterministic key for this operation.
	CacheKey() string
	// Apply executes the operation on a running instance.
	Apply(ctx context.Context, inst Instance) error
}

// runOp represents a Run operation.
type runOp struct {
	cmd     []string
	env     []string
	workDir string
	user    string
}

func (o *runOp) CacheKey() string {
	return fslayer.RunOpKey(o.cmd, o.env, o.workDir, o.user)
}

func (o *runOp) Apply(ctx context.Context, inst Instance) error {
	cmd := inst.CommandContext(ctx, o.cmd[0], o.cmd[1:]...)
	for _, e := range o.env {
		if key, value, ok := strings.Cut(e, "="); ok {
			cmd = cmd.SetEnv(key, value)
		}
	}
	if o.workDir != "" {
		cmd = cmd.SetDir(o.workDir)
	}
	if o.user != "" {
		cmd = cmd.SetUser(o.user)
	}
	return cmd.Run()
}

// copyOp represents a Copy operation.
type copyOp struct {
	src         string // Host path
	dst         string // Guest path
	contentHash string // Hash of source content
	hashErr     error  // Error from hashing source (checked at Apply time)
}

func (o *copyOp) CacheKey() string {
	return fslayer.CopyOpKey(o.src, o.dst, o.contentHash)
}

func (o *copyOp) Apply(ctx context.Context, inst Instance) error {
	if o.hashErr != nil {
		return fmt.Errorf("source %s: %w", o.src, o.hashErr)
	}
	// Read source file
	data, err := os.ReadFile(o.src)
	if err != nil {
		return fmt.Errorf("read source %s: %w", o.src, err)
	}

	// Write to guest
	return inst.WriteFile(o.dst, data, 0o644)
}

// fromOp represents a From operation (base image).
type fromOp struct {
	imageRef string
}

func (o *fromOp) CacheKey() string {
	return "from:" + o.imageRef
}

func (o *fromOp) Apply(ctx context.Context, inst Instance) error {
	// From is handled specially - it sets the base image
	return nil
}

// FilesystemSnapshotFactory builds filesystem snapshots using Dockerfile-like operations.
type FilesystemSnapshotFactory struct {
	client       OCIClient
	cacheDir     string
	ops          []FSLayerOp
	excludes     []string
	env          []string
	workDir      string
	buildOptions []Option
}

// NewFilesystemSnapshotFactory creates a new factory for building filesystem snapshots.
func NewFilesystemSnapshotFactory(client OCIClient, cacheDir string) *FilesystemSnapshotFactory {
	return &FilesystemSnapshotFactory{
		client:   client,
		cacheDir: cacheDir,
	}
}

func (f *FilesystemSnapshotFactory) SetBuildOptions(opts ...Option) {
	f.buildOptions = append(f.buildOptions, opts...)
}

// From sets the base image for the snapshot.
// This must be called first or the chain will fail at build time.
func (f *FilesystemSnapshotFactory) From(imageRef string) *FilesystemSnapshotFactory {
	f.ops = append(f.ops, &fromOp{imageRef: imageRef})
	return f
}

// Run adds a command execution operation.
func (f *FilesystemSnapshotFactory) Run(cmd ...string) *FilesystemSnapshotFactory {
	f.ops = append(f.ops, &runOp{
		cmd:     cmd,
		env:     append([]string{}, f.env...), // Copy current env
		workDir: f.workDir,
	})
	return f
}

// Copy adds a file copy operation from host to guest.
func (f *FilesystemSnapshotFactory) Copy(src, dst string) *FilesystemSnapshotFactory {
	// Compute content hash of source file
	var contentHash string
	var hashErr error
	data, err := os.ReadFile(src)
	if err != nil {
		hashErr = err
	} else {
		h := sha256.Sum256(data)
		contentHash = hex.EncodeToString(h[:])
	}

	f.ops = append(f.ops, &copyOp{
		src:         src,
		dst:         dst,
		contentHash: contentHash,
		hashErr:     hashErr,
	})
	return f
}

// CopyReader adds a copy operation from an io.Reader to a guest path.
func (f *FilesystemSnapshotFactory) CopyReader(r io.Reader, dst string) *FilesystemSnapshotFactory {
	// Read all data to compute hash
	data, err := io.ReadAll(r)
	if err != nil {
		// Store empty hash - will fail at build time
		data = nil
	}

	h := sha256.Sum256(data)
	contentHash := hex.EncodeToString(h[:])

	// Use a special readerOp
	f.ops = append(f.ops, &readerOp{
		data:        data,
		dst:         dst,
		contentHash: contentHash,
	})
	return f
}

// readerOp represents a copy from data to guest.
type readerOp struct {
	data        []byte
	dst         string
	contentHash string
}

func (o *readerOp) CacheKey() string {
	return fslayer.CopyOpKey("reader", o.dst, o.contentHash)
}

func (o *readerOp) Apply(ctx context.Context, inst Instance) error {
	return inst.WriteFile(o.dst, o.data, 0o644)
}

// Exclude adds path patterns to exclude from snapshots.
func (f *FilesystemSnapshotFactory) Exclude(patterns ...string) *FilesystemSnapshotFactory {
	f.excludes = append(f.excludes, patterns...)
	return f
}

// Env sets environment variables for subsequent Run operations.
func (f *FilesystemSnapshotFactory) Env(env ...string) *FilesystemSnapshotFactory {
	f.env = append(f.env, env...)
	return f
}

// WorkDir sets the working directory for subsequent Run operations.
func (f *FilesystemSnapshotFactory) WorkDir(dir string) *FilesystemSnapshotFactory {
	f.workDir = dir
	return f
}

// Build executes all operations and returns the final filesystem snapshot.
// It checks the cache for existing snapshots and only executes necessary operations.
func (f *FilesystemSnapshotFactory) Build(ctx context.Context) (FilesystemSnapshot, error) {
	if len(f.ops) == 0 {
		return nil, fmt.Errorf("no operations defined")
	}

	// First operation must be From
	fromOp, ok := f.ops[0].(*fromOp)
	if !ok {
		return nil, fmt.Errorf("first operation must be From()")
	}

	// Compute full cache key from operation chain
	fullCacheKey := f.computeFullCacheKey()

	// Check if final result is cached
	if fslayer.ManifestExists(f.cacheDir, fullCacheKey) {
		manifest, err := fslayer.LoadManifest(f.cacheDir, fullCacheKey)
		if err == nil {
			return f.loadFromManifest(ctx, manifest)
		}
		// If loading fails, rebuild
	}

	// Pull base image
	baseSource, err := f.client.Pull(ctx, fromOp.imageRef)
	if err != nil {
		return nil, fmt.Errorf("pull base image: %w", err)
	}

	// Find the first operation that needs to be executed (not cached)
	startIdx := 1 // Skip From operation
	var currentSnap FilesystemSnapshot
	currentSnap = baseSource.(FilesystemSnapshot)

	for i := 1; i < len(f.ops); i++ {
		// Compute cache key up to this point
		partialKey := f.computePartialCacheKey(i)
		if fslayer.ManifestExists(f.cacheDir, partialKey) {
			manifest, err := fslayer.LoadManifest(f.cacheDir, partialKey)
			if err == nil {
				snap, err := f.loadFromManifest(ctx, manifest)
				if err == nil {
					if currentSnap != baseSource {
						currentSnap.Close()
					}
					currentSnap = snap
					startIdx = i + 1
					continue
				}
			}
		}
		break
	}

	// Execute remaining operations
	for i := startIdx; i < len(f.ops); i++ {
		op := f.ops[i]

		// Create instance from current snapshot (no timeout for factory builds)
		inst, err := New(currentSnap, f.buildOptions...)
		if err != nil {
			return nil, fmt.Errorf("create instance for op %d: %w", i, err)
		}

		// Apply operation
		if err := op.Apply(ctx, inst); err != nil {
			inst.Close()
			return nil, fmt.Errorf("apply op %d (%s): %w", i, op.CacheKey(), err)
		}

		// Take snapshot
		snap, err := inst.SnapshotFilesystem(
			WithExcludes(f.excludes...),
			WithCacheDir(f.cacheDir),
		)
		if err != nil {
			inst.Close()
			return nil, fmt.Errorf("snapshot after op %d: %w", i, err)
		}

		// Close instance
		inst.Close()

		// Save manifest for this snapshot
		partialKey := f.computePartialCacheKey(i)
		if fsSnap, ok := snap.(*fsSnapshotSource); ok {
			manifest := &fslayer.FSManifest{
				Version:      1,
				CacheKey:     partialKey,
				Layers:       fsSnap.layers,
				BaseImageRef: fromOp.imageRef,
				Architecture: string(fsSnap.arch),
			}
			if err := fslayer.SaveManifest(manifest, f.cacheDir); err != nil {
				// Log warning but continue
			}
		}

		// Update current snapshot
		if currentSnap != baseSource {
			currentSnap.Close()
		}
		currentSnap = snap
	}

	return currentSnap, nil
}

// computeFullCacheKey computes the cache key for the complete operation chain.
func (f *FilesystemSnapshotFactory) computeFullCacheKey() string {
	return f.computePartialCacheKey(len(f.ops) - 1)
}

// computePartialCacheKey computes the cache key up to (and including) operation at index.
func (f *FilesystemSnapshotFactory) computePartialCacheKey(upToIdx int) string {
	h := sha256.New()
	for i := 0; i <= upToIdx && i < len(f.ops); i++ {
		h.Write([]byte(f.ops[i].CacheKey()))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:32]
}

// loadFromManifest loads a filesystem snapshot from a persisted manifest.
func (f *FilesystemSnapshotFactory) loadFromManifest(ctx context.Context, manifest *fslayer.FSManifest) (FilesystemSnapshot, error) {
	// Pull base image
	baseSource, err := f.client.Pull(ctx, manifest.BaseImageRef)
	if err != nil {
		return nil, fmt.Errorf("pull base image: %w", err)
	}

	src, ok := baseSource.(*ociSource)
	if !ok {
		return nil, fmt.Errorf("unexpected source type: %T", baseSource)
	}

	// Load snapshot layers
	var layers []oci.ImageLayer
	for _, hash := range manifest.Layers {
		layer, err := fslayer.ReadLayer(f.cacheDir, hash)
		if err != nil {
			return nil, fmt.Errorf("read layer %s: %w", hash, err)
		}
		layers = append(layers, oci.ImageLayer{
			Hash:         layer.Hash,
			IndexPath:    layer.IndexPath,
			ContentsPath: layer.ContentsPath,
		})
	}

	// Create layered container filesystem
	lcfs, err := oci.NewLayeredContainerFS(src.cfs, layers)
	if err != nil {
		return nil, fmt.Errorf("create layered fs: %w", err)
	}

	return &fsSnapshotSource{
		lcfs:      lcfs,
		baseImage: src.image,
		parent:    src,
		cacheKey:  manifest.CacheKey,
		arch:      src.arch,
		layers:    manifest.Layers,
	}, nil
}
