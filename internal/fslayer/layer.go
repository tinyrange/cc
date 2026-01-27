// Package fslayer provides filesystem layer storage and management for snapshots.
package fslayer

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/tinyrange/cc/internal/archive"
)

// LayerEntry represents a single entry in a filesystem layer.
type LayerEntry struct {
	Path    string
	Kind    LayerEntryKind
	Mode    fs.FileMode
	UID     int
	GID     int
	ModTime time.Time
	Size    int64
	Data    []byte // For regular files, the content; for symlinks, the target
}

// LayerEntryKind describes the type of layer entry.
type LayerEntryKind uint8

const (
	LayerEntryRegular LayerEntryKind = iota
	LayerEntryDirectory
	LayerEntrySymlink
	LayerEntryDeleted // Whiteout marker
)

// LayerData holds all modifications in a filesystem layer.
type LayerData struct {
	Entries []LayerEntry
}

// Layer represents a stored filesystem layer.
type Layer struct {
	Hash         string // SHA256 hash of the layer contents
	IndexPath    string // Path to .idx file
	ContentsPath string // Path to .contents file
}

// WriteLayer writes a LayerData to disk using the archive format.
// Returns the Layer metadata including the content hash.
func WriteLayer(data *LayerData, dir string) (*Layer, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create layer dir: %w", err)
	}

	// Create temporary files for writing
	tmpIdx, err := os.CreateTemp(dir, "layer-*.idx.tmp")
	if err != nil {
		return nil, fmt.Errorf("create temp index: %w", err)
	}
	tmpIdxPath := tmpIdx.Name()
	defer func() {
		tmpIdx.Close()
		os.Remove(tmpIdxPath)
	}()

	tmpContents, err := os.CreateTemp(dir, "layer-*.contents.tmp")
	if err != nil {
		return nil, fmt.Errorf("create temp contents: %w", err)
	}
	tmpContentsPath := tmpContents.Name()
	defer func() {
		tmpContents.Close()
		os.Remove(tmpContentsPath)
	}()

	// Create archive writer
	writer, err := archive.NewArchiveWriter(tmpIdx, tmpContents)
	if err != nil {
		return nil, fmt.Errorf("create archive writer: %w", err)
	}
	// Disable padding for layer files to save space
	writer.DisablePadding()

	// Hash all content as we write
	contentHash := sha256.New()

	for _, entry := range data.Entries {
		ef := &archive.EntryFactory{}
		ef.Name(entry.Path)
		ef.Mode(entry.Mode)
		ef.Owner(entry.UID, entry.GID)
		ef.ModTime(entry.ModTime)

		var reader io.Reader
		switch entry.Kind {
		case LayerEntryRegular:
			ef.Kind(archive.EntryKindRegular)
			ef.Size(int64(len(entry.Data)))
			if len(entry.Data) > 0 {
				reader = &hashingReader{
					r:    &byteReader{data: entry.Data},
					hash: contentHash,
				}
			}
		case LayerEntryDirectory:
			ef.Kind(archive.EntryKindDirectory)
		case LayerEntrySymlink:
			ef.Kind(archive.EntryKindSymlink)
			ef.Linkname(string(entry.Data))
		case LayerEntryDeleted:
			ef.Kind(archive.EntryKindDeleted)
		}

		// Hash the entry path for determinism
		contentHash.Write([]byte(entry.Path))
		contentHash.Write([]byte{byte(entry.Kind)})

		if err := writer.WriteEntry(ef, reader); err != nil {
			return nil, fmt.Errorf("write entry %s: %w", entry.Path, err)
		}
	}

	// Close files to flush
	if err := tmpIdx.Close(); err != nil {
		return nil, fmt.Errorf("close index: %w", err)
	}
	if err := tmpContents.Close(); err != nil {
		return nil, fmt.Errorf("close contents: %w", err)
	}

	// Generate final hash
	hash := hex.EncodeToString(contentHash.Sum(nil))

	// Rename to final paths
	idxPath := filepath.Join(dir, hash+".idx")
	contentsPath := filepath.Join(dir, hash+".contents")

	// Check if layer already exists (content-addressable)
	if _, err := os.Stat(idxPath); err == nil {
		// Layer already exists, clean up temps and return existing
		return &Layer{
			Hash:         hash,
			IndexPath:    idxPath,
			ContentsPath: contentsPath,
		}, nil
	}

	if err := os.Rename(tmpIdxPath, idxPath); err != nil {
		return nil, fmt.Errorf("rename index: %w", err)
	}
	if err := os.Rename(tmpContentsPath, contentsPath); err != nil {
		// Try to clean up the index file
		os.Remove(idxPath)
		return nil, fmt.Errorf("rename contents: %w", err)
	}

	return &Layer{
		Hash:         hash,
		IndexPath:    idxPath,
		ContentsPath: contentsPath,
	}, nil
}

// ReadLayer reads a layer from disk.
func ReadLayer(dir, hash string) (*Layer, error) {
	idxPath := filepath.Join(dir, hash+".idx")
	contentsPath := filepath.Join(dir, hash+".contents")

	if _, err := os.Stat(idxPath); err != nil {
		return nil, fmt.Errorf("index not found: %w", err)
	}
	if _, err := os.Stat(contentsPath); err != nil {
		return nil, fmt.Errorf("contents not found: %w", err)
	}

	return &Layer{
		Hash:         hash,
		IndexPath:    idxPath,
		ContentsPath: contentsPath,
	}, nil
}

// byteReader implements io.Reader for a byte slice.
type byteReader struct {
	data []byte
	pos  int
}

func (r *byteReader) Read(p []byte) (n int, err error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n = copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

// hashingReader wraps a reader and hashes all data read.
type hashingReader struct {
	r    io.Reader
	hash io.Writer
}

func (r *hashingReader) Read(p []byte) (n int, err error) {
	n, err = r.r.Read(p)
	if n > 0 {
		r.hash.Write(p[:n])
	}
	return n, err
}
