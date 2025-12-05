package syscallnum

import (
	"testing"

	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/linux/defs"
)

func TestNumber(t *testing.T) {
	tests := []struct {
		name string
		arch hv.CpuArchitecture
		sc   defs.Syscall
		want int
	}{
		{name: "amd64_exit", arch: hv.ArchitectureX86_64, sc: defs.SYS_EXIT, want: 60},
		{name: "arm64_exit", arch: hv.ArchitectureARM64, sc: defs.SYS_EXIT, want: 93},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Number(tt.arch, tt.sc)
			if err != nil {
				t.Fatalf("Number returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("Number(%v, %v)=%d, want %d", tt.arch, tt.sc, got, tt.want)
			}
		})
	}
}
