package kernel

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/linux/kernel/alpine"
)

type File interface {
	io.Reader
	io.ReaderAt
}

type Tristate int

const (
	TristateNo Tristate = iota
	TristateYes
	TristateModule
)

func (t Tristate) String() string {
	switch t {
	case TristateNo:
		return "no"
	case TristateYes:
		return "yes"
	case TristateModule:
		return "module"
	default:
		return "unknown"
	}
}

type Module struct {
	Name string
	Data []byte
}

type Kernel interface {
	Open() (File, error)
	Size() (int64, error)

	GetModuleDepends(name string) ([]string, error)
	OpenModule(name string) ([]byte, error)

	GetConfig() (map[string]Tristate, error)
	GetDependMap() (map[string][]string, error)

	GetSystemMap() (io.ReaderAt, error)

	PlanModuleLoad(
		configVars []string,
		moduleMap map[string]string,
	) ([]Module, error)
}

type alpineKernel struct {
	pkg *alpine.AlpinePackage

	arch hv.CpuArchitecture

	dependsList  map[string][]string
	modulePrefix string
}

// GetSystemMap implements Kernel.
func (k *alpineKernel) GetSystemMap() (io.ReaderAt, error) {
	f, err := k.findFileWithPrefix("boot/System.map")
	if err != nil {
		return nil, err
	}

	return k.pkg.Open(f)
}

func (k *alpineKernel) Open() (File, error) {
	if k.arch == hv.ArchitectureRISCV64 {
		return k.pkg.Open("boot/vmlinuz-lts")
	}

	return k.pkg.Open("boot/vmlinuz-virt")
}

func (k *alpineKernel) Size() (int64, error) {
	if k.arch == hv.ArchitectureRISCV64 {
		return k.pkg.Size("boot/vmlinuz-lts")
	}

	return k.pkg.Size("boot/vmlinuz-virt")
}

func (k *alpineKernel) findFileWithPrefix(prefix string) (string, error) {
	for _, file := range k.pkg.ListFiles() {
		if strings.HasPrefix(file, prefix) {
			return file, nil
		}
	}
	return "", fmt.Errorf("file with prefix %q not found", prefix)
}

func (k *alpineKernel) findFileWithSuffix(suffix string) (string, error) {
	for _, file := range k.pkg.ListFiles() {
		if strings.HasSuffix(file, suffix) {
			return file, nil
		}
	}
	return "", fmt.Errorf("file with suffix %q not found", suffix)
}

func (k *alpineKernel) loadModuleDepends() error {
	depends, err := k.findFileWithSuffix("modules.dep")
	if err != nil {
		return err
	}

	k.modulePrefix = strings.TrimSuffix(depends, "modules.dep")

	r, err := k.pkg.Open(depends)
	if err != nil {
		return err
	}

	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	for line := range strings.SplitSeq(string(data), "\n") {
		if line == "" {
			continue
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid module.depends line: %q", line)
		}

		moduleName := strings.TrimSpace(parts[0])
		depList := strings.Split(strings.TrimSpace(parts[1]), " ")

		k.dependsList[moduleName] = []string{}
		for _, dep := range depList {
			dep = strings.TrimSpace(dep)
			if dep != "" {
				k.dependsList[moduleName] = append(k.dependsList[moduleName], dep)
			}
		}
	}

	return nil
}

func (k *alpineKernel) GetDependMap() (map[string][]string, error) {
	if k.dependsList == nil {
		k.dependsList = make(map[string][]string)
		if err := k.loadModuleDepends(); err != nil {
			return nil, err
		}
	}

	return k.dependsList, nil
}

func (k *alpineKernel) GetModuleDepends(name string) ([]string, error) {
	if k.dependsList == nil {
		k.dependsList = make(map[string][]string)
		if err := k.loadModuleDepends(); err != nil {
			return nil, err
		}
	}

	depends, ok := k.dependsList[name]
	if !ok {
		return nil, fmt.Errorf("module %q not found in depends list", name)
	}

	return depends, nil
}

func (k *alpineKernel) OpenModule(name string) ([]byte, error) {
	file, err := k.findFileWithSuffix(name + ".ko.gz")
	if err != nil {
		return nil, err
	}

	r, err := k.pkg.Open(file)
	if err != nil {
		return nil, err
	}

	gzReader, err := gzip.NewReader(r)
	if err != nil {
		return nil, err
	}
	defer gzReader.Close()

	data, err := io.ReadAll(gzReader)
	if err != nil {
		return nil, err
	}

	return data, nil
}

func (k *alpineKernel) GetConfig() (map[string]Tristate, error) {
	configFile, err := k.findFileWithPrefix("boot/config-")
	if err != nil {
		return nil, err
	}

	r, err := k.pkg.Open(configFile)
	if err != nil {
		return nil, err
	}

	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	configs := make(map[string]Tristate)
	for line := range strings.SplitSeq(string(data), "\n") {
		if strings.HasPrefix(line, "# CONFIG_") && strings.HasSuffix(line, " is not set") {
			key := strings.TrimPrefix(line, "# ")
			key = strings.TrimSuffix(key, " is not set")
			configs[key] = TristateNo
		} else if strings.HasPrefix(line, "CONFIG_") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) != 2 {
				continue
			}
			key := parts[0]
			value := parts[1]
			switch value {
			case "y":
				configs[key] = TristateYes
			case "m":
				configs[key] = TristateModule
			}
		}
	}

	return configs, nil
}

// PlanModuleLoad implements Kernel.
func (k *alpineKernel) PlanModuleLoad(configs []string, moduleMap map[string]string) ([]Module, error) {
	// configs maps from config name (e.g. CONFIG_FOO) to module filename.

	// load the config
	config, err := k.GetConfig()
	if err != nil {
		return nil, fmt.Errorf("get kernel config: %v", err)
	}

	// load the module depends
	if k.dependsList == nil {
		k.dependsList = make(map[string][]string)
		if err := k.loadModuleDepends(); err != nil {
			return nil, fmt.Errorf("load module depends: %v", err)
		}
	}

	var ret []Module

	var loadModule func(moduleName string) error
	loadModule = func(moduleName string) error {
		// check if already loaded
		for _, mod := range ret {
			if mod.Name == moduleName {
				return nil
			}
		}

		// load dependencies first
		depends, err := k.GetModuleDepends(moduleName)
		if err != nil {
			return fmt.Errorf("get depends for module %q: %v", moduleName, err)
		}
		for _, dep := range depends {
			if err := loadModule(dep); err != nil {
				return fmt.Errorf("load dependency %q of module %q: %v", dep, moduleName, err)
			}
		}

		// load the module data
		f, err := k.pkg.Open(k.modulePrefix + moduleName)
		if err != nil {
			return fmt.Errorf("open module %q: %v", moduleName, err)
		}

		gzReader, err := gzip.NewReader(f)
		if err != nil {
			return fmt.Errorf("create gzip reader for module %q: %v", moduleName, err)
		}
		defer gzReader.Close()

		data, err := io.ReadAll(gzReader)
		if err != nil {
			return fmt.Errorf("read module %q: %v", moduleName, err)
		}

		ret = append(ret, Module{
			Name: moduleName,
			Data: data,
		})

		return nil
	}

	for _, configVar := range configs {
		state, ok := config[configVar]
		if !ok {
			return nil, fmt.Errorf("config variable %q not found in kernel config", configVar)
		}

		switch state {
		case TristateYes:
			// nothing to do
		case TristateNo:
			return nil, fmt.Errorf("required config variable %q is not enabled in kernel", configVar)
		case TristateModule:
			// find the module name in the module map
			moduleName, ok := moduleMap[configVar]
			if !ok {
				return nil, fmt.Errorf("no module mapped for config variable %q", configVar)
			}

			if err := loadModule(moduleName); err != nil {
				return nil, err
			}
		}
	}

	return ret, nil
}

// GPUModuleConfigs lists the kernel config options needed for GPU support
var GPUModuleConfigs = []string{
	"CONFIG_DRM",
	"CONFIG_DRM_KMS_HELPER",
	"CONFIG_DRM_VIRTIO_GPU",
	"CONFIG_VIRTIO_INPUT",
	// Needed for /dev/input/event* nodes (wlroots/libinput expects evdev devices).
	"CONFIG_INPUT_EVDEV",
}

// GPUModuleMap maps kernel config options to module file paths
var GPUModuleMap = map[string]string{
	"CONFIG_DRM":            "kernel/drivers/gpu/drm/drm.ko.gz",
	"CONFIG_DRM_KMS_HELPER": "kernel/drivers/gpu/drm/drm_kms_helper.ko.gz",
	"CONFIG_DRM_VIRTIO_GPU": "kernel/drivers/gpu/drm/virtio/virtio-gpu.ko.gz",
	"CONFIG_VIRTIO_INPUT":   "kernel/drivers/virtio/virtio_input.ko.gz",
	"CONFIG_INPUT_EVDEV":    "kernel/drivers/input/evdev.ko.gz",
}

var defaultCachePath = ""

func GetDefaultCachePath() (string, error) {
	if defaultCachePath == "" {
		cfg, err := os.UserConfigDir()
		if err != nil {
			return "", fmt.Errorf("get user config dir: %v", err)
		}
		defaultCachePath = filepath.Join(cfg, "cc")
	}

	return defaultCachePath, nil
}

func SetDefaultCachePath(path string) {
	defaultCachePath = path
}

func LoadForArchitecture(arch hv.CpuArchitecture) (Kernel, error) {
	dl := alpine.AlpineDownloader{}

	cachePath, err := GetDefaultCachePath()
	if err != nil {
		return nil, err
	}

	if err := dl.SetForArchitecture(arch, cachePath); err != nil {
		return nil, err
	}

	pkgName := "linux-virt"
	if arch == hv.ArchitectureRISCV64 {
		pkgName = "linux-lts"
	}

	pkg, err := dl.Download(pkgName)
	if err != nil {
		return nil, err
	}

	return &alpineKernel{
		arch: arch,
		pkg:  pkg,
	}, nil
}

// LoadFromDirectory loads a kernel from a pre-cached local directory.
// This is used for offline/bundled distribution where packages are stored
// next to the executable. The directory should contain files like:
// linux-virt-aarch64.idx, linux-virt-aarch64.bin for ARM64
// linux-virt-x86_64.idx, linux-virt-x86_64.bin for x86_64
func LoadFromDirectory(arch hv.CpuArchitecture, dir string) (Kernel, error) {
	pkgName := "linux-virt"
	if arch == hv.ArchitectureRISCV64 {
		pkgName = "linux-lts"
	}

	var archName string
	switch arch {
	case hv.ArchitectureX86_64:
		archName = "x86_64"
	case hv.ArchitectureARM64:
		archName = "aarch64"
	case hv.ArchitectureRISCV64:
		archName = "riscv64"
	default:
		return nil, fmt.Errorf("unsupported architecture: %v", arch)
	}

	// Look for package files: linux-virt-aarch64.idx/.bin
	basePath := filepath.Join(dir, fmt.Sprintf("%s-%s", pkgName, archName))

	pkg, err := alpine.OpenLocalPackage(basePath)
	if err != nil {
		return nil, fmt.Errorf("open local kernel package: %w", err)
	}

	return &alpineKernel{
		arch: arch,
		pkg:  pkg,
	}, nil
}

// LoadForArchitectureWithFallback tries to load a kernel from a local resources
// directory first, then falls back to downloading from the network.
// If localResourceDir is empty, it behaves the same as LoadForArchitecture.
func LoadForArchitectureWithFallback(arch hv.CpuArchitecture, localResourceDir string) (Kernel, error) {
	// Try local resources directory first
	if localResourceDir != "" {
		if k, err := LoadFromDirectory(arch, localResourceDir); err == nil {
			return k, nil
		}
		// Local not found, fall through to download
	}

	// Fall back to normal behavior (cache + download)
	return LoadForArchitecture(arch)
}

// ExportResources downloads the kernel for the given architecture and copies
// the cached files to destDir with standardized names for offline distribution.
// Files are named: linux-virt-<arch>.idx and linux-virt-<arch>.bin
func ExportResources(arch hv.CpuArchitecture, destDir string) error {
	dl := alpine.AlpineDownloader{}

	cachePath, err := GetDefaultCachePath()
	if err != nil {
		return err
	}

	if err := dl.SetForArchitecture(arch, cachePath); err != nil {
		return err
	}

	pkgName := "linux-virt"
	if arch == hv.ArchitectureRISCV64 {
		pkgName = "linux-lts"
	}

	var archName string
	switch arch {
	case hv.ArchitectureX86_64:
		archName = "x86_64"
	case hv.ArchitectureARM64:
		archName = "aarch64"
	case hv.ArchitectureRISCV64:
		archName = "riscv64"
	default:
		return fmt.Errorf("unsupported architecture: %v", arch)
	}

	// Download and get the cache path
	cacheBasePath, _, err := dl.DownloadAndGetPath(pkgName)
	if err != nil {
		return fmt.Errorf("download kernel: %w", err)
	}

	// Create destination directory
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("create dest dir: %w", err)
	}

	// Copy .idx and .bin files with standardized names
	destBasePath := filepath.Join(destDir, fmt.Sprintf("%s-%s", pkgName, archName))

	if err := copyFile(cacheBasePath+".idx", destBasePath+".idx"); err != nil {
		return fmt.Errorf("copy idx: %w", err)
	}
	if err := copyFile(cacheBasePath+".bin", destBasePath+".bin"); err != nil {
		return fmt.Errorf("copy bin: %w", err)
	}

	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
