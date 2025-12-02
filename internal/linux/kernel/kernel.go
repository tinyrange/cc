package kernel

import (
	"io"

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
