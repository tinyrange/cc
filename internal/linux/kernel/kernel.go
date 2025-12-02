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

	GetModule(name string) ([]byte, error)
}

type alpineKernel struct {
	pkg *alpine.AlpinePackage
}

func (k *alpineKernel) Open() (File, error) {
	return k.pkg.Open("boot/vmlinuz-virt")
}

func (k *alpineKernel) Size() (int64, error) {
	return k.pkg.Size("boot/vmlinuz-virt")
}

func (k *alpineKernel) GetModule(name string) ([]byte, error) {
	// find a .ko.gz file matching the module name
	for _, file := range k.pkg.ListFiles() {
		if strings.HasSuffix(file, name+".ko.gz") {
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
	}

	return nil, fmt.Errorf("module %q not found", name)
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
