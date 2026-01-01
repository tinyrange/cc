package oci

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
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
	defer func() { _ = out.Close() }()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy %s -> %s: %w", src, dst, err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close %s: %w", dst, err)
	}
	return nil
}


