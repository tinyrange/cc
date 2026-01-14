package qemu

import (
	"fmt"
	"io"

	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/linux/kernel/alpine"
)

// EmulatorInfo describes a QEMU static binary for a target architecture.
type EmulatorInfo struct {
	// TargetArch is the architecture this emulator can run binaries for.
	TargetArch hv.CpuArchitecture
	// PackageName is the Alpine package name (e.g., "qemu-aarch64").
	PackageName string
	// BinaryPath is the path inside the APK (e.g., "usr/bin/qemu-aarch64").
	BinaryPath string
	// GuestPath is where to install the binary in the guest (e.g., "/usr/bin/qemu-aarch64-static").
	GuestPath string
}

var emulatorInfoMap = map[hv.CpuArchitecture]EmulatorInfo{
	hv.ArchitectureARM64: {
		TargetArch:  hv.ArchitectureARM64,
		PackageName: "qemu-aarch64",
		BinaryPath:  "usr/bin/qemu-aarch64",
		GuestPath:   "/usr/bin/qemu-aarch64-static",
	},
	hv.ArchitectureX86_64: {
		TargetArch:  hv.ArchitectureX86_64,
		PackageName: "qemu-x86_64",
		BinaryPath:  "usr/bin/qemu-x86_64",
		GuestPath:   "/usr/bin/qemu-x86_64-static",
	},
}

// GetEmulatorInfo returns the emulator info for the given target architecture.
func GetEmulatorInfo(targetArch hv.CpuArchitecture) (EmulatorInfo, bool) {
	info, ok := emulatorInfoMap[targetArch]
	return info, ok
}

// SupportedEmulatorArchitectures returns the list of architectures we can emulate.
func SupportedEmulatorArchitectures() []hv.CpuArchitecture {
	return []hv.CpuArchitecture{
		hv.ArchitectureARM64,
		hv.ArchitectureX86_64,
	}
}

// Downloader downloads QEMU user emulation packages from Alpine's community repository.
type Downloader struct {
	alpine *alpine.AlpineDownloader
}

// NewDownloader creates a new QEMU downloader.
// The hostArch parameter determines which Alpine repository to use (the QEMU
// static binaries need to be compiled for the host architecture).
func NewDownloader(hostArch hv.CpuArchitecture, cacheDir string) (*Downloader, error) {
	dl := &alpine.AlpineDownloader{
		Repo: "community",
	}

	if err := dl.SetForArchitecture(hostArch, cacheDir); err != nil {
		return nil, fmt.Errorf("set alpine downloader for architecture: %w", err)
	}

	return &Downloader{alpine: dl}, nil
}

// DownloadEmulator downloads and extracts the QEMU binary for the given target architecture.
// Returns the binary data ready to be written to the guest filesystem.
func (d *Downloader) DownloadEmulator(targetArch hv.CpuArchitecture) ([]byte, error) {
	info, ok := GetEmulatorInfo(targetArch)
	if !ok {
		return nil, fmt.Errorf("unsupported target architecture: %s", targetArch)
	}

	pkg, err := d.alpine.Download(info.PackageName)
	if err != nil {
		return nil, fmt.Errorf("download package %s: %w", info.PackageName, err)
	}
	defer pkg.Close()

	// Open the binary file inside the package
	f, err := pkg.Open(info.BinaryPath)
	if err != nil {
		return nil, fmt.Errorf("open binary %s in package: %w", info.BinaryPath, err)
	}

	// Read the entire binary
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("read binary %s: %w", info.BinaryPath, err)
	}

	return data, nil
}
