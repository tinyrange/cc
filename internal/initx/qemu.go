package initx

import (
	"fmt"

	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/linux/qemu"
)

// PrepareQEMUEmulation downloads the QEMU static binary and prepares the
// configuration for cross-architecture emulation.
//
// hostArch is the architecture of the host (the VM kernel's architecture).
// targetArch is the architecture of the container binaries to emulate.
// cacheDir is the directory to cache downloaded packages.
func PrepareQEMUEmulation(hostArch, targetArch hv.CpuArchitecture, cacheDir string) (*QEMUEmulationConfig, error) {
	if hostArch == targetArch {
		return nil, fmt.Errorf("no emulation needed: host and target architectures are the same (%s)", hostArch)
	}

	// Get emulator info for the target architecture
	emulatorInfo, ok := qemu.GetEmulatorInfo(targetArch)
	if !ok {
		return nil, fmt.Errorf("unsupported target architecture for QEMU emulation: %s", targetArch)
	}

	// Get binfmt configuration for the target architecture
	binfmtConfig, ok := qemu.GetBinfmtConfig(targetArch)
	if !ok {
		return nil, fmt.Errorf("no binfmt configuration for architecture: %s", targetArch)
	}

	// Download the QEMU binary
	downloader, err := qemu.NewDownloader(hostArch, cacheDir)
	if err != nil {
		return nil, fmt.Errorf("create QEMU downloader: %w", err)
	}

	binary, err := downloader.DownloadEmulator(targetArch)
	if err != nil {
		return nil, fmt.Errorf("download QEMU emulator for %s: %w", targetArch, err)
	}

	return &QEMUEmulationConfig{
		TargetArch:         targetArch,
		Binary:             binary,
		BinaryPath:         emulatorInfo.GuestPath,
		BinfmtRegistration: binfmtConfig.RegistrationString(),
	}, nil
}

// NeedsQEMUEmulation returns true if the image architecture differs from
// the host architecture and QEMU emulation is supported for that combination.
func NeedsQEMUEmulation(hostArch, imageArch hv.CpuArchitecture) bool {
	if hostArch == imageArch {
		return false
	}

	// Check if we have emulator support for the target architecture
	_, ok := qemu.GetEmulatorInfo(imageArch)
	return ok
}
