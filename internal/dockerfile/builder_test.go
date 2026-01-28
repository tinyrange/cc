package dockerfile

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestBuildSimpleDockerfile(t *testing.T) {
	dockerfile := []byte(`FROM alpine:3.19
RUN echo "hello world"
`)

	df, err := Parse(dockerfile)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	builder := NewBuilder(df)
	result, err := builder.Build()
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	if result.ImageRef != "alpine:3.19" {
		t.Errorf("expected ImageRef alpine:3.19, got %s", result.ImageRef)
	}

	if len(result.Ops) != 1 {
		t.Errorf("expected 1 operation, got %d", len(result.Ops))
	}
}

func TestBuildWithEnv(t *testing.T) {
	// Note: $PATH in ENV is a shell variable, not a Dockerfile ARG
	// The parser should NOT expand it, leaving it for the shell
	dockerfile := []byte(`FROM alpine
ENV MYVAR=myvalue
RUN echo $MYVAR
`)

	df, err := Parse(dockerfile)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	builder := NewBuilder(df)
	result, err := builder.Build()
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	if len(result.Env) != 1 {
		t.Fatalf("expected 1 env var, got %d", len(result.Env))
	}

	// The env should be set
	if result.Env[0] != "MYVAR=myvalue" {
		t.Errorf("unexpected env: %s", result.Env[0])
	}
}

func TestBuildWithWorkdir(t *testing.T) {
	dockerfile := []byte(`FROM alpine
WORKDIR /app
WORKDIR subdir
RUN pwd
`)

	df, err := Parse(dockerfile)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	builder := NewBuilder(df)
	result, err := builder.Build()
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	// Final workdir should be /app/subdir
	if result.WorkDir != "/app/subdir" {
		t.Errorf("expected workdir /app/subdir, got %s", result.WorkDir)
	}

	// Should have mkdir ops for each WORKDIR
	// WORKDIR /app -> mkdir -p /app
	// WORKDIR subdir -> mkdir -p /app/subdir
	// RUN pwd
	if len(result.Ops) != 3 {
		t.Errorf("expected 3 ops, got %d", len(result.Ops))
	}
}

func TestBuildWithCopy(t *testing.T) {
	// Create a temp directory with a test file
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test content"), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	dockerfile := []byte(`FROM alpine
COPY test.txt /app/test.txt
`)

	df, err := Parse(dockerfile)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	ctx, err := NewDirBuildContext(tempDir)
	if err != nil {
		t.Fatalf("NewDirBuildContext failed: %v", err)
	}

	builder := NewBuilder(df).WithContext(ctx)
	result, err := builder.Build()
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	if len(result.Ops) != 1 {
		t.Errorf("expected 1 op, got %d", len(result.Ops))
	}

	// Verify the op has a cache key
	if result.Ops[0].CacheKey() == "" {
		t.Error("expected non-empty cache key")
	}
}

func TestBuildWithCopyMissingFile(t *testing.T) {
	dockerfile := []byte(`FROM alpine
COPY nonexistent.txt /app/
`)

	df, err := Parse(dockerfile)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	ctx, err := NewDirBuildContext(t.TempDir())
	if err != nil {
		t.Fatalf("NewDirBuildContext failed: %v", err)
	}

	builder := NewBuilder(df).WithContext(ctx)
	_, err = builder.Build()
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestBuildWithCopyNoContext(t *testing.T) {
	dockerfile := []byte(`FROM alpine
COPY file.txt /app/
`)

	df, err := Parse(dockerfile)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	builder := NewBuilder(df) // No context
	_, err = builder.Build()
	if err == nil {
		t.Fatal("expected error for COPY without context")
	}
}

func TestBuildWithBuildArg(t *testing.T) {
	dockerfile := []byte(`ARG VERSION=default
FROM alpine:$VERSION
`)

	df, err := Parse(dockerfile)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	// Test with default
	builder := NewBuilder(df)
	result, err := builder.Build()
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	if result.ImageRef != "alpine:default" {
		t.Errorf("expected alpine:default, got %s", result.ImageRef)
	}

	// Test with override
	df2, err := Parse(dockerfile)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	builder2 := NewBuilder(df2).WithBuildArg("VERSION", "3.19")
	result2, err := builder2.Build()
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	if result2.ImageRef != "alpine:3.19" {
		t.Errorf("expected alpine:3.19, got %s", result2.ImageRef)
	}
}

func TestBuildRuntimeConfig(t *testing.T) {
	dockerfile := []byte(`FROM alpine
USER nobody
EXPOSE 80 443
LABEL version="1.0" app="test"
CMD ["echo", "hello"]
ENTRYPOINT ["/entrypoint.sh"]
SHELL ["/bin/bash", "-c"]
`)

	df, err := Parse(dockerfile)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	builder := NewBuilder(df)
	result, err := builder.Build()
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	cfg := result.RuntimeConfig

	if cfg.User != "nobody" {
		t.Errorf("expected user nobody, got %s", cfg.User)
	}

	if len(cfg.ExposePorts) != 2 || cfg.ExposePorts[0] != "80" {
		t.Errorf("unexpected expose ports: %v", cfg.ExposePorts)
	}

	if cfg.Labels["version"] != "1.0" || cfg.Labels["app"] != "test" {
		t.Errorf("unexpected labels: %v", cfg.Labels)
	}

	if len(cfg.Cmd) != 2 || cfg.Cmd[0] != "echo" {
		t.Errorf("unexpected cmd: %v", cfg.Cmd)
	}

	if len(cfg.Entrypoint) != 1 || cfg.Entrypoint[0] != "/entrypoint.sh" {
		t.Errorf("unexpected entrypoint: %v", cfg.Entrypoint)
	}

	if len(cfg.Shell) != 2 || cfg.Shell[0] != "/bin/bash" {
		t.Errorf("unexpected shell: %v", cfg.Shell)
	}
}

func TestBuildRunShellVsExec(t *testing.T) {
	tests := []struct {
		name       string
		dockerfile string
		wantShell  bool
	}{
		{
			name:       "shell form",
			dockerfile: "FROM alpine\nRUN echo hello\n",
			wantShell:  true,
		},
		{
			name:       "exec form",
			dockerfile: "FROM alpine\nRUN [\"echo\", \"hello\"]\n",
			wantShell:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			df, err := Parse([]byte(tc.dockerfile))
			if err != nil {
				t.Fatalf("Parse failed: %v", err)
			}

			builder := NewBuilder(df)
			result, err := builder.Build()
			if err != nil {
				t.Fatalf("Build failed: %v", err)
			}

			if len(result.Ops) != 1 {
				t.Fatalf("expected 1 op, got %d", len(result.Ops))
			}

			// For shell form, the command should be wrapped
			// For exec form, it should not
			op := result.Ops[0].(*runOp)
			if tc.wantShell {
				// Should be ["/bin/sh", "-c", "echo hello"]
				if len(op.cmd) != 3 || op.cmd[0] != "/bin/sh" {
					t.Errorf("expected shell-wrapped command, got %v", op.cmd)
				}
			} else {
				// Should be ["echo", "hello"]
				if len(op.cmd) != 2 || op.cmd[0] != "echo" {
					t.Errorf("expected direct command, got %v", op.cmd)
				}
			}
		})
	}
}

func TestBuildOpCacheKeys(t *testing.T) {
	dockerfile := []byte(`FROM alpine
RUN echo hello
`)

	df, err := Parse(dockerfile)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	builder := NewBuilder(df)
	result, err := builder.Build()
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	// Cache keys should be deterministic
	key1 := result.Ops[0].CacheKey()

	// Build again
	df2, _ := Parse(dockerfile)
	builder2 := NewBuilder(df2)
	result2, _ := builder2.Build()
	key2 := result2.Ops[0].CacheKey()

	if key1 != key2 {
		t.Errorf("cache keys should be deterministic: %s != %s", key1, key2)
	}
}

func TestDirBuildContext(t *testing.T) {
	tempDir := t.TempDir()

	// Create test files
	if err := os.WriteFile(filepath.Join(tempDir, "file.txt"), []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(tempDir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "subdir", "nested.txt"), []byte("nested"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, err := NewDirBuildContext(tempDir)
	if err != nil {
		t.Fatalf("NewDirBuildContext failed: %v", err)
	}

	// Test Open
	rc, err := ctx.Open("file.txt")
	if err != nil {
		t.Errorf("Open failed: %v", err)
	} else {
		rc.Close()
	}

	// Test Stat
	info, err := ctx.Stat("file.txt")
	if err != nil {
		t.Errorf("Stat failed: %v", err)
	} else if info.Name() != "file.txt" {
		t.Errorf("unexpected name: %s", info.Name())
	}

	// Test nested path
	rc, err = ctx.Open("subdir/nested.txt")
	if err != nil {
		t.Errorf("Open nested failed: %v", err)
	} else {
		rc.Close()
	}

	// Test path traversal rejection
	_, err = ctx.Open("../escape")
	if err == nil {
		t.Error("expected error for path traversal")
	}
}

// mockInstance implements the Instance interface for testing Apply methods.
type mockInstance struct {
	commands   [][]string
	files      map[string][]byte
	currentDir string
}

func newMockInstance() *mockInstance {
	return &mockInstance{
		files: make(map[string][]byte),
	}
}

func (m *mockInstance) CommandContext(_ context.Context, name string, args ...string) Cmd {
	cmd := append([]string{name}, args...)
	return &mockCmd{
		inst: m,
		cmd:  cmd,
	}
}

func (m *mockInstance) WriteFile(name string, data []byte, _ os.FileMode) error {
	m.files[name] = data
	return nil
}

type mockCmd struct {
	inst    *mockInstance
	cmd     []string
	env     []string
	workDir string
}

func (c *mockCmd) Run() error {
	c.inst.commands = append(c.inst.commands, c.cmd)
	return nil
}

func (c *mockCmd) SetEnv(env []string) Cmd {
	c.env = env
	return c
}

func (c *mockCmd) SetDir(dir string) Cmd {
	c.workDir = dir
	return c
}

func TestRunOpApply(t *testing.T) {
	op := &runOp{
		cmd:     []string{"/bin/sh", "-c", "echo hello"},
		env:     []string{"FOO=bar"},
		workDir: "/app",
	}

	inst := newMockInstance()
	ctx := context.Background()

	if err := op.Apply(ctx, inst); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	if len(inst.commands) != 1 {
		t.Errorf("expected 1 command, got %d", len(inst.commands))
	}
}

func TestReaderOpApply(t *testing.T) {
	op := &readerOp{
		data:        []byte("test content"),
		dst:         "/app/file.txt",
		contentHash: "abc123",
	}

	inst := newMockInstance()
	ctx := context.Background()

	if err := op.Apply(ctx, inst); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	if string(inst.files["/app/file.txt"]) != "test content" {
		t.Errorf("file not written correctly")
	}
}
