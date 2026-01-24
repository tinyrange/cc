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

