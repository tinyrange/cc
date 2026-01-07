package initx

import (
	"testing"

	"github.com/tinyrange/cc/internal/hv"
)

func TestBuildFromRTG(t *testing.T) {
	// Test basic RTG compilation without preload modules
	cfg := BuilderConfig{
		Arch:           hv.ArchitectureX86_64,
		PreloadModules: nil,
	}

	prog, err := BuildFromRTG(cfg)
	if err != nil {
		t.Fatalf("BuildFromRTG failed: %v", err)
	}

	if prog == nil {
		t.Fatal("BuildFromRTG returned nil program")
	}

	if prog.Entrypoint != "main" {
		t.Errorf("expected entrypoint 'main', got %q", prog.Entrypoint)
	}

	main, ok := prog.Methods["main"]
	if !ok {
		t.Fatal("main method not found in program")
	}

	if len(main) == 0 {
		t.Error("main method is empty")
	}

	t.Logf("RTG init program compiled successfully with %d fragments in main", len(main))
}

func TestBuildFromRTGARM64(t *testing.T) {
	// Test ARM64 variant
	cfg := BuilderConfig{
		Arch:           hv.ArchitectureARM64,
		PreloadModules: nil,
	}

	prog, err := BuildFromRTG(cfg)
	if err != nil {
		t.Fatalf("BuildFromRTG failed for ARM64: %v", err)
	}

	if prog == nil {
		t.Fatal("BuildFromRTG returned nil program for ARM64")
	}

	t.Logf("RTG init program compiled successfully for ARM64")
}
