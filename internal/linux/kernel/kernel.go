package kernel

import (
	"compress/gzip"
	"fmt"
	"io"
	"strings"

	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/linux/kernel/alpine"
)

type File interface {
	io.Reader
	io.ReaderAt
}

type Kernel interface {
	Open() (File, error)
	Size() (int64, error)

	GetModuleDepends(name string) ([]string, error)
	OpenModule(name string) ([]byte, error)
}

type alpineKernel struct {
	pkg *alpine.AlpinePackage

	dependsList map[string][]string
}

func (k *alpineKernel) Open() (File, error) {
	return k.pkg.Open("boot/vmlinuz-virt")
}

func (k *alpineKernel) Size() (int64, error) {
	return k.pkg.Size("boot/vmlinuz-virt")
}

func (k *alpineKernel) findFile(suffix string) (string, error) {
	for _, file := range k.pkg.ListFiles() {
		if strings.HasSuffix(file, suffix) {
			return file, nil
		}
	}
	return "", fmt.Errorf("file with suffix %q not found", suffix)
}

func (k *alpineKernel) loadModuleDepends() error {
	depends, err := k.findFile("modules.dep")
	if err != nil {
		return err
	}

	r, err := k.pkg.Open(depends)
	if err != nil {
		return err
	}

	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid module.depends line: %q", line)
		}

		moduleName := strings.TrimSpace(parts[0])
		depList := strings.Split(strings.TrimSpace(parts[1]), ",")

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
	file, err := k.findFile(name + ".ko.gz")
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

func LoadForArchitecture(arch hv.CpuArchitecture) (Kernel, error) {
	dl := alpine.AlpineDownloader{}

	if err := dl.SetForArchitecture(arch, "local/alpine_cache"); err != nil {
		return nil, err
	}

	pkg, err := dl.Download("linux-virt")
	if err != nil {
		return nil, err
	}

	return &alpineKernel{
		pkg: pkg,
	}, nil
}
