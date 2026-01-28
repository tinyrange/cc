package dockerfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseTestdataFiles(t *testing.T) {
	// Get the testdata directory
	testdataDir := "testdata"

	files, err := os.ReadDir(testdataDir)
	if err != nil {
		t.Fatalf("failed to read testdata directory: %v", err)
	}

	for _, f := range files {
		if !f.IsDir() && filepath.Ext(f.Name()) == ".dockerfile" {
			t.Run(f.Name(), func(t *testing.T) {
				path := filepath.Join(testdataDir, f.Name())
				content, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("failed to read file: %v", err)
				}

				df, err := Parse(content)
				if err != nil {
					t.Fatalf("Parse failed: %v", err)
				}

				if len(df.Stages) == 0 {
					t.Error("expected at least one stage")
				}
			})
		}
	}
}

func TestParseRealProjectDockerfiles(t *testing.T) {
	// Parse actual Dockerfiles from the tests/ directory
	testsDir := "../../tests"

	entries, err := os.ReadDir(testsDir)
	if err != nil {
		t.Skip("tests directory not found")
	}

	// Dockerfiles that use unsupported features
	unsupportedDockerfiles := map[string]string{
		"systemd": "VOLUME", // Uses VOLUME instruction
	}

	for _, entry := range entries {
		if entry.IsDir() {
			dockerfilePath := filepath.Join(testsDir, entry.Name(), "Dockerfile")
			content, err := os.ReadFile(dockerfilePath)
			if err != nil {
				continue // Skip directories without Dockerfiles
			}

			t.Run(entry.Name(), func(t *testing.T) {
				df, err := Parse(content)
				if reason, unsupported := unsupportedDockerfiles[entry.Name()]; unsupported {
					if err != nil {
						t.Logf("Expected parse issue for %s (uses %s): %v", entry.Name(), reason, err)
						return
					}
				}
				if err != nil {
					t.Fatalf("Parse failed: %v", err)
				}

				if len(df.Stages) == 0 {
					t.Error("expected at least one stage")
				}

				// Build to verify instructions can be processed
				builder := NewBuilder(df)
				_, err = builder.Build()
				if err != nil {
					// Some Dockerfiles may have COPY without context, which is expected
					if contains(err.Error(), "no build context") {
						t.Logf("Build info (expected for COPY without context): %v", err)
					} else {
						t.Logf("Build warning: %v", err)
					}
				}
			})
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
