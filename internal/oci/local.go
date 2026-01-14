package oci

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/tinyrange/cc/internal/hv"
)

// LoadFromDir loads an image from a prebaked directory containing config.json and
// layer *.idx/*.contents files.
func LoadFromDir(dir string) (*Image, error) {
	configPath := filepath.Join(dir, "config.json")
	f, err := os.Open(configPath)
	if err != nil {
		return nil, fmt.Errorf("open config: %w", err)
	}
	defer f.Close()

	var cfg RuntimeConfig
	if err := json.NewDecoder(f).Decode(&cfg); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}

	// Parse User field to UID/GID if not already set
	if cfg.User != "" && cfg.UID == nil {
		uid, gid := parseUserString(cfg.User)
		cfg.UID = uid
		cfg.GID = gid
	}

	img := &Image{
		Config: cfg,
		Dir:    dir,
	}

	for _, layerHash := range cfg.Layers {
		hash := strings.TrimPrefix(layerHash, "sha256:")
		img.Layers = append(img.Layers, ImageLayer{
			Hash:         layerHash,
			IndexPath:    filepath.Join(dir, hash+".idx"),
			ContentsPath: filepath.Join(dir, hash+".contents"),
		})
	}

	return img, nil
}

// parseUserString parses a Docker-style user string to UID/GID.
// Supports formats: "uid", "uid:gid", "username" (username returns nil).
func parseUserString(user string) (*int, *int) {
	// Try "uid:gid" format
	if parts := strings.SplitN(user, ":", 2); len(parts) == 2 {
		uid, err := strconv.Atoi(parts[0])
		if err != nil {
			return nil, nil // Not numeric
		}
		uidPtr := &uid
		gid, err := strconv.Atoi(parts[1])
		if err != nil {
			return uidPtr, uidPtr // GID defaults to UID
		}
		gidPtr := &gid
		return uidPtr, gidPtr
	}

	// Try numeric UID only
	uid, err := strconv.Atoi(user)
	if err != nil {
		return nil, nil // Username string - can't resolve without /etc/passwd
	}
	uidPtr := &uid
	return uidPtr, uidPtr
}

// LoadFromDirForArch loads an image from a prebaked directory and validates
// that the image architecture matches the expected architecture.
func LoadFromDirForArch(dir string, expectedArch hv.CpuArchitecture) (*Image, error) {
	img, err := LoadFromDir(dir)
	if err != nil {
		return nil, err
	}

	// Validate architecture if image has one specified
	if img.Config.Architecture != "" {
		expectedOCI, err := toOciArchFromHV(expectedArch)
		if err != nil {
			return nil, err
		}

		if img.Config.Architecture != expectedOCI {
			return nil, &ArchitectureMismatchError{
				Expected: expectedOCI,
				Actual:   img.Config.Architecture,
			}
		}
	}

	return img, nil
}

// ArchitectureMismatchError is returned when an image's architecture doesn't
// match the expected architecture.
type ArchitectureMismatchError struct {
	Expected string
	Actual   string
}

func (e *ArchitectureMismatchError) Error() string {
	return fmt.Sprintf("image architecture mismatch: image is for %s but expected %s", e.Actual, e.Expected)
}

// toOciArchFromHV converts a hypervisor architecture to OCI architecture string.
func toOciArchFromHV(arch hv.CpuArchitecture) (string, error) {
	switch arch {
	case hv.ArchitectureX86_64:
		return "amd64", nil
	case hv.ArchitectureARM64:
		return "arm64", nil
	default:
		return "", fmt.Errorf("unsupported architecture: %s", arch)
	}
}

// ExportToDir copies the prebaked runtime artifacts for img into dstDir.
// The destination will contain config.json plus all referenced *.idx/*.contents.
func ExportToDir(img *Image, dstDir string) error {
	if img == nil {
		return fmt.Errorf("nil image")
	}
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return fmt.Errorf("create dst dir: %w", err)
	}

	if err := copyFile(filepath.Join(img.Dir, "config.json"), filepath.Join(dstDir, "config.json")); err != nil {
		return err
	}

	for _, layer := range img.Layers {
		if err := copyFile(layer.IndexPath, filepath.Join(dstDir, filepath.Base(layer.IndexPath))); err != nil {
			return err
		}
		if err := copyFile(layer.ContentsPath, filepath.Join(dstDir, filepath.Base(layer.ContentsPath))); err != nil {
			return err
		}
	}

	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("create dir for %s: %w", dst, err)
	}

	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("create %s: %w", dst, err)
	}

	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return fmt.Errorf("copy %s -> %s: %w", src, dst, err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close %s: %w", dst, err)
	}
	return nil
}
