package bundle

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/tinyrange/cc/internal/oci"
	"gopkg.in/yaml.v3"
)

const (
	MetadataFilename = "ccbundle.yaml"
	DefaultImageDir  = "image"
)

// Metadata describes a prebaked “cc bundle” folder on disk.
type Metadata struct {
	Version     int    `yaml:"version"`
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Icon        string `yaml:"icon,omitempty"`

	Boot BootConfig `yaml:"boot"`
}

type BootConfig struct {
	ImageDir string   `yaml:"imageDir"`
	Command  []string `yaml:"command,omitempty"`

	CPUs     int    `yaml:"cpus,omitempty"`
	MemoryMB uint64 `yaml:"memoryMB,omitempty"`
	Exec     bool   `yaml:"exec,omitempty"`
	Dmesg    bool   `yaml:"dmesg,omitempty"`
}

func (m *Metadata) normalize() {
	if m.Version == 0 {
		m.Version = 1
	}
	if m.Name == "" {
		m.Name = "{{name}}"
	}
	if m.Description == "" {
		m.Description = "{{description}}"
	}
	if m.Boot.ImageDir == "" {
		m.Boot.ImageDir = DefaultImageDir
	}
}

func IsBundleDir(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, MetadataFilename))
	return err == nil
}

// ValidateBundleDir validates that a directory is a valid bundle with valid metadata.
func ValidateBundleDir(dir string) error {
	if !IsBundleDir(dir) {
		return fmt.Errorf("missing %s", MetadataFilename)
	}

	meta, err := LoadMetadata(dir)
	if err != nil {
		return fmt.Errorf("invalid metadata: %w", err)
	}

	imageDir := filepath.Join(dir, meta.Boot.ImageDir)
	if _, err := os.Stat(imageDir); os.IsNotExist(err) {
		return fmt.Errorf("image directory not found: %s", imageDir)
	}

	return nil
}

func LoadMetadata(dir string) (Metadata, error) {
	data, err := os.ReadFile(filepath.Join(dir, MetadataFilename))
	if err != nil {
		return Metadata{}, fmt.Errorf("read %s: %w", MetadataFilename, err)
	}

	var meta Metadata
	if err := yaml.Unmarshal(data, &meta); err != nil {
		return Metadata{}, fmt.Errorf("parse %s: %w", MetadataFilename, err)
	}
	meta.normalize()
	return meta, nil
}

// Load reads bundle metadata and loads the prebaked OCI image from disk.
func Load(dir string) (Metadata, *oci.Image, error) {
	meta, err := LoadMetadata(dir)
	if err != nil {
		return Metadata{}, nil, err
	}

	imgDir := filepath.Join(dir, meta.Boot.ImageDir)
	img, err := oci.LoadFromDir(imgDir)
	if err != nil {
		return Metadata{}, nil, fmt.Errorf("load prebaked image: %w", err)
	}
	return meta, img, nil
}

// WriteTemplate writes a metadata YAML file. Callers should have already created
// the bundle directory and any referenced files (image/, icons, etc.).
func WriteTemplate(dir string, meta Metadata) error {
	meta.normalize()

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create bundle dir: %w", err)
	}

	f, err := os.Create(filepath.Join(dir, MetadataFilename))
	if err != nil {
		return fmt.Errorf("create %s: %w", MetadataFilename, err)
	}
	defer f.Close()

	enc := yaml.NewEncoder(f)
	enc.SetIndent(2)
	if err := enc.Encode(&meta); err != nil {
		return fmt.Errorf("encode %s: %w", MetadataFilename, err)
	}
	if err := enc.Close(); err != nil {
		return fmt.Errorf("close %s: %w", MetadataFilename, err)
	}
	return nil
}
