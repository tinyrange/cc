package initx

import (
	"testing"

	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/ir"
)

func TestBuildContainerInitProgram_Compiles(t *testing.T) {
	t.Parallel()

	archs := []hv.CpuArchitecture{
		hv.ArchitectureX86_64,
		hv.ArchitectureARM64,
	}

	for _, arch := range archs {
		arch := arch
		t.Run(string(arch), func(t *testing.T) {
			t.Parallel()

			prog, err := BuildContainerInitProgram(ContainerInitConfig{
				Arch:          arch,
				Cmd:           []string{"/bin/sh", "-c", "echo hi"},
				Env:           []string{"PATH=/bin:/usr/bin"},
				WorkDir:       "/",
				EnableNetwork: true,
				Exec:          false,
			})
			if err != nil {
				t.Fatalf("BuildContainerInitProgram: %v", err)
			}

			if _, err := ir.BuildStandaloneProgramForArch(arch, prog); err != nil {
				t.Fatalf("BuildStandaloneProgramForArch(%s): %v", arch, err)
			}
		})
	}
}

func TestBuildContainerInitProgram_CommandLoopVsock(t *testing.T) {
	t.Parallel()

	archs := []hv.CpuArchitecture{
		hv.ArchitectureX86_64,
		hv.ArchitectureARM64,
	}

	for _, arch := range archs {
		arch := arch
		t.Run(string(arch), func(t *testing.T) {
			t.Parallel()

			// Vsock is always used for command loop mode (MMIO path has been removed)
			prog, err := BuildContainerInitProgram(ContainerInitConfig{
				Arch:          arch,
				Cmd:           nil, // Not needed in CommandLoop mode
				Env:           []string{"PATH=/bin:/usr/bin"},
				WorkDir:       "/",
				EnableNetwork: true,
				Exec:          false,
				CommandLoop:   true,
			})
			if err != nil {
				t.Fatalf("BuildContainerInitProgram: %v", err)
			}

			if _, err := ir.BuildStandaloneProgramForArch(arch, prog); err != nil {
				t.Fatalf("BuildStandaloneProgramForArch(%s): %v", arch, err)
			}
		})
	}
}
