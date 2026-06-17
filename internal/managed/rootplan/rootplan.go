package rootplan

import (
	"fmt"
	"io/fs"

	"j5.nz/cc/internal/imagefs"
)

type File struct {
	Path string
	Mode fs.FileMode
	Data []byte
}

type Device struct {
	Path string
	Mode fs.FileMode
	RDev uint32
}

type Symlink struct {
	Path   string
	Target string
}

func AddFiles(overlay *imagefs.Overlay, files []File) error {
	for _, file := range files {
		if err := overlay.AddFile(file.Path, file.Mode, file.Data); err != nil {
			return fmt.Errorf("overlay %s: %w", file.Path, err)
		}
	}
	return nil
}

func AddSymlinks(overlay *imagefs.Overlay, symlinks []Symlink) error {
	for _, link := range symlinks {
		if err := overlay.AddSymlink(link.Path, link.Target); err != nil {
			return fmt.Errorf("add symlink %s: %w", link.Path, err)
		}
	}
	return nil
}

func AddDevices(overlay *imagefs.Overlay, devices []Device) error {
	for _, dev := range devices {
		if err := overlay.AddDevice(dev.Path, dev.Mode, dev.RDev); err != nil {
			return fmt.Errorf("add %s: %w", dev.Path, err)
		}
	}
	return nil
}
