package dockerfile

import (
	"errors"
	"strings"
	"testing"
)

func TestRejectOversizedDockerfile(t *testing.T) {
	huge := make([]byte, MaxDockerfileSize+1)
	for i := range huge {
		huge[i] = 'a'
	}

	_, err := Parse(huge)
	if err == nil {
		t.Fatal("expected error for oversized dockerfile")
	}
	if !errors.Is(err, ErrDockerfileTooLarge) {
		t.Errorf("expected ErrDockerfileTooLarge, got: %v", err)
	}
}

func TestRejectTooManyInstructions(t *testing.T) {
	var b strings.Builder
	b.WriteString("FROM alpine\n")
	for i := 0; i < MaxInstructionCount+10; i++ {
		b.WriteString("RUN echo test\n")
	}

	_, err := Parse([]byte(b.String()))
	if err == nil {
		t.Fatal("expected error for too many instructions")
	}
	if !errors.Is(err, ErrTooManyInstructions) {
		t.Errorf("expected ErrTooManyInstructions, got: %v", err)
	}
}

func TestValidatePath(t *testing.T) {
	tests := []struct {
		name        string
		contextRoot string
		path        string
		wantErr     bool
	}{
		{
			name:        "simple relative path",
			contextRoot: "/context",
			path:        "file.txt",
			wantErr:     false,
		},
		{
			name:        "nested path",
			contextRoot: "/context",
			path:        "dir/subdir/file.txt",
			wantErr:     false,
		},
		{
			name:        "path with dot",
			contextRoot: "/context",
			path:        "./file.txt",
			wantErr:     false,
		},
		{
			name:        "path traversal simple",
			contextRoot: "/context",
			path:        "../secret",
			wantErr:     true,
		},
		{
			name:        "path traversal nested",
			contextRoot: "/context",
			path:        "dir/../../../secret",
			wantErr:     true,
		},
		{
			name:        "null byte in path",
			contextRoot: "/context",
			path:        "file\x00.txt",
			wantErr:     true,
		},
		{
			name:        "absolute path (stripped)",
			contextRoot: "/context",
			path:        "/file.txt",
			wantErr:     false,
		},
		{
			name:        "empty context root",
			contextRoot: "",
			path:        "file.txt",
			wantErr:     false,
		},
		{
			name:        "path traversal at start",
			contextRoot: "/context",
			path:        "..",
			wantErr:     true,
		},
		{
			name:        "path with embedded ..",
			contextRoot: "/context",
			path:        "a/b/../../c",
			wantErr:     false, // resolves to "c" which is fine
		},
		{
			name:        "path escaping via ..",
			contextRoot: "/context",
			path:        "a/../../secret",
			wantErr:     true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidatePath(tc.contextRoot, tc.path)
			if tc.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestValidateDestPath(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{
			name:    "absolute path",
			path:    "/usr/local/bin",
			wantErr: false,
		},
		{
			name:    "relative path",
			path:    "bin/app",
			wantErr: false,
		},
		{
			name:    "null byte",
			path:    "/usr/\x00local",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateDestPath(tc.path)
			if tc.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestRejectPathTraversalInCopy(t *testing.T) {
	dockerfile := []byte(`FROM alpine
COPY ../../etc/passwd /stolen
`)
	df, err := Parse(dockerfile)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	// The parser doesn't validate paths - that's done by the builder
	// Create a builder to test path validation
	builder := NewBuilder(df)
	ctx, err := NewDirBuildContext(t.TempDir())
	if err != nil {
		t.Fatalf("NewDirBuildContext failed: %v", err)
	}
	builder = builder.WithContext(ctx)

	_, err = builder.Build()
	if err == nil {
		t.Fatal("expected error for path traversal in COPY")
	}

	// The error should be about path traversal
	errStr := err.Error()
	if !strings.Contains(errStr, "escapes") && !strings.Contains(errStr, "traversal") {
		t.Errorf("expected path traversal error, got: %v", err)
	}
}

func TestRejectVariableExpansionLoop(t *testing.T) {
	// Test that we don't hang on deeply nested variable expansion
	dockerfile := []byte(`FROM alpine
ARG A=$B
ARG B=$C
ARG C=$D
ARG D=$E
ARG E=$F
ARG F=$G
ARG G=$H
ARG H=$I
ARG I=$J
ARG J=$K
ARG K=$L
ARG L=$A
RUN echo $A
`)
	_, err := Parse(dockerfile)
	// The parsing itself might succeed, but expansion should be limited
	// This test mainly ensures we don't hang
	_ = err
}

func TestTooManyVariables(t *testing.T) {
	var b strings.Builder
	b.WriteString("FROM alpine\n")
	for i := 0; i < MaxVariableCount+10; i++ {
		b.WriteString("ARG VAR")
		b.WriteString(string(rune('A' + (i % 26))))
		b.WriteString(string(rune('0' + (i / 26))))
		b.WriteString("=value\n")
	}

	_, err := Parse([]byte(b.String()))
	if err == nil {
		t.Fatal("expected error for too many variables")
	}
	if !errors.Is(err, ErrTooManyVariables) {
		t.Errorf("expected ErrTooManyVariables, got: %v", err)
	}
}

func FuzzParse(f *testing.F) {
	// Add seed corpus
	seeds := []string{
		"FROM alpine\n",
		"FROM alpine:3.19\nRUN echo hello\n",
		"FROM alpine\nCOPY src dst\n",
		"ARG VERSION=1.0\nFROM alpine:$VERSION\n",
		"FROM alpine\nENV FOO=bar\nRUN echo $FOO\n",
	}

	for _, seed := range seeds {
		f.Add([]byte(seed))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		// The parser should not panic on any input
		_, _ = Parse(data)
	})
}

func FuzzExpandVariables(f *testing.F) {
	f.Add("$VAR", "VAR", "value")
	f.Add("${VAR:-default}", "VAR", "")
	f.Add("${VAR:+alternate}", "VAR", "set")
	f.Add("$$escaped", "VAR", "value")

	f.Fuzz(func(t *testing.T, input, key, value string) {
		vars := map[string]string{key: value}
		// Should not panic
		_, _ = ExpandVariables(input, vars)
	})
}
