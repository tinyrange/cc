package api

import (
	"bytes"
	"context"
	"errors"
	"log"
	"os"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if err := EnsureExecutableIsSigned(); err != nil {
		log.Fatalf("Failed to sign executable: %v", err)
	}
	os.Exit(m.Run())
}

// testMemoryOption is a local memory option for tests.
type testMemoryOption struct{ sizeMB uint64 }

func (testMemoryOption) IsOption()        {}
func (o testMemoryOption) SizeMB() uint64 { return o.sizeMB }

func withMemoryMB(size uint64) Option {
	return testMemoryOption{sizeMB: size}
}

// TestCapture tests stdout/stderr capture functionality.
// These tests require a hypervisor and pull the alpine:latest image.

func setupAlpineForCapture(t *testing.T, ctx context.Context) InstanceSource {
	t.Helper()

	client, err := NewOCIClient()
	if err != nil {
		t.Fatalf("NewOCIClient() error = %v", err)
	}

	source, err := client.Pull(ctx, "alpine:latest")
	if err != nil {
		t.Fatalf("Pull() error = %v", err)
	}

	return source
}

func TestCapture_OutputStdout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	source := setupAlpineForCapture(t, ctx)

	inst, err := New(source, withMemoryMB(128))
	if err != nil {
		if errors.Is(err, ErrHypervisorUnavailable) {
			t.Skip("Skipping: hypervisor unavailable")
		}
		t.Fatalf("New() error = %v", err)
	}
	defer inst.Close()

	// Test Output() - should capture stdout
	cmd := inst.CommandContext(ctx, "/bin/echo", "hello world")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("Output() error = %v", err)
	}

	expected := []byte("hello world\n")
	if !bytes.Equal(out, expected) {
		t.Errorf("Output() = %q, want %q", out, expected)
	}
}

func TestCapture_OutputMultiLine(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	source := setupAlpineForCapture(t, ctx)

	inst, err := New(source, withMemoryMB(128))
	if err != nil {
		if errors.Is(err, ErrHypervisorUnavailable) {
			t.Skip("Skipping: hypervisor unavailable")
		}
		t.Fatalf("New() error = %v", err)
	}
	defer inst.Close()

	// Use printf to test multi-line output
	cmd := inst.CommandContext(ctx, "/bin/sh", "-c", "echo line1; echo line2; echo line3")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("Output() error = %v", err)
	}

	expected := []byte("line1\nline2\nline3\n")
	if !bytes.Equal(out, expected) {
		t.Errorf("Output() = %q, want %q", out, expected)
	}
}

func TestCapture_CombinedOutput(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	source := setupAlpineForCapture(t, ctx)

	inst, err := New(source, withMemoryMB(128))
	if err != nil {
		if errors.Is(err, ErrHypervisorUnavailable) {
			t.Skip("Skipping: hypervisor unavailable")
		}
		t.Fatalf("New() error = %v", err)
	}
	defer inst.Close()

	// Test CombinedOutput() - should capture both stdout and stderr
	cmd := inst.CommandContext(ctx, "/bin/sh", "-c", "echo stdout; echo stderr >&2")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CombinedOutput() error = %v", err)
	}

	// Combined output should contain both (order may vary)
	if !bytes.Contains(out, []byte("stdout")) {
		t.Errorf("CombinedOutput() missing 'stdout': got %q", out)
	}
	if !bytes.Contains(out, []byte("stderr")) {
		t.Errorf("CombinedOutput() missing 'stderr': got %q", out)
	}
}

func TestCapture_SeparateStdoutStderr(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	source := setupAlpineForCapture(t, ctx)

	inst, err := New(source, withMemoryMB(128))
	if err != nil {
		if errors.Is(err, ErrHypervisorUnavailable) {
			t.Skip("Skipping: hypervisor unavailable")
		}
		t.Fatalf("New() error = %v", err)
	}
	defer inst.Close()

	// Test separate stdout/stderr capture
	cmd := inst.CommandContext(ctx, "/bin/sh", "-c", "echo stdout_msg; echo stderr_msg >&2")

	var stdout, stderr bytes.Buffer
	cmd.SetStdout(&stdout)
	cmd.SetStderr(&stderr)

	if err := cmd.Run(); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if !bytes.Equal(stdout.Bytes(), []byte("stdout_msg\n")) {
		t.Errorf("stdout = %q, want %q", stdout.Bytes(), "stdout_msg\n")
	}
	if !bytes.Equal(stderr.Bytes(), []byte("stderr_msg\n")) {
		t.Errorf("stderr = %q, want %q", stderr.Bytes(), "stderr_msg\n")
	}
}

func TestCapture_StderrOnly(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	source := setupAlpineForCapture(t, ctx)

	inst, err := New(source, withMemoryMB(128))
	if err != nil {
		if errors.Is(err, ErrHypervisorUnavailable) {
			t.Skip("Skipping: hypervisor unavailable")
		}
		t.Fatalf("New() error = %v", err)
	}
	defer inst.Close()

	// Test command that only writes to stderr
	cmd := inst.CommandContext(ctx, "/bin/sh", "-c", "echo error >&2; exit 1")

	var stderr bytes.Buffer
	cmd.SetStderr(&stderr)

	err = cmd.Run()
	if err == nil {
		t.Fatal("Run() expected error for exit 1, got nil")
	}

	if !bytes.Equal(stderr.Bytes(), []byte("error\n")) {
		t.Errorf("stderr = %q, want %q", stderr.Bytes(), "error\n")
	}
}

func TestCapture_LargeOutput(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	source := setupAlpineForCapture(t, ctx)

	inst, err := New(source, withMemoryMB(128))
	if err != nil {
		if errors.Is(err, ErrHypervisorUnavailable) {
			t.Skip("Skipping: hypervisor unavailable")
		}
		t.Fatalf("New() error = %v", err)
	}
	defer inst.Close()

	// Generate ~100KB of output (1000 lines of 100 chars each)
	cmd := inst.CommandContext(ctx, "/bin/sh", "-c",
		"i=0; while [ $i -lt 1000 ]; do printf '%.100d\\n' $i; i=$((i+1)); done")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("Output() error = %v", err)
	}

	// Should have 1000 lines, each with 100 chars + newline = 101 chars
	expectedLen := 1000 * 101
	if len(out) != expectedLen {
		t.Errorf("Output() length = %d, want %d", len(out), expectedLen)
	}
}

func TestCapture_EmptyOutput(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	source := setupAlpineForCapture(t, ctx)

	inst, err := New(source, withMemoryMB(128))
	if err != nil {
		if errors.Is(err, ErrHypervisorUnavailable) {
			t.Skip("Skipping: hypervisor unavailable")
		}
		t.Fatalf("New() error = %v", err)
	}
	defer inst.Close()

	// Command that produces no output
	cmd := inst.CommandContext(ctx, "/bin/true")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("Output() error = %v", err)
	}

	if len(out) != 0 {
		t.Errorf("Output() = %q, want empty", out)
	}
}

func TestCapture_StderrViaCat(t *testing.T) {
	// Use /bin/cat to write directly to stderr via file descriptor
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	source := setupAlpineForCapture(t, ctx)

	inst, err := New(source, withMemoryMB(128))
	if err != nil {
		if errors.Is(err, ErrHypervisorUnavailable) {
			t.Skip("Skipping: hypervisor unavailable")
		}
		t.Fatalf("New() error = %v", err)
	}
	defer inst.Close()

	// Write to /dev/stderr using echo
	cmd := inst.CommandContext(ctx, "/bin/sh", "-c", "echo test > /dev/stderr")

	var stderr bytes.Buffer
	cmd.SetStderr(&stderr)

	if err := cmd.Run(); err != nil {
		t.Logf("Run() error = %v (this may be expected)", err)
	}

	t.Logf("stderr bytes: %q (len=%d)", stderr.Bytes(), stderr.Len())
	if stderr.Len() == 0 {
		t.Errorf("stderr is empty, expected 'test\\n'")
	}
}

func TestCapture_DebugFlags(t *testing.T) {
	// Debug test to verify capture flags are being set correctly
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	source := setupAlpineForCapture(t, ctx)

	inst, err := New(source, withMemoryMB(128))
	if err != nil {
		if errors.Is(err, ErrHypervisorUnavailable) {
			t.Skip("Skipping: hypervisor unavailable")
		}
		t.Fatalf("New() error = %v", err)
	}
	defer inst.Close()

	// Run a simple echo that writes to both stdout and stderr
	cmd := inst.CommandContext(ctx, "/bin/sh", "-c", "echo OUT; echo ERR >&2")

	var stdout, stderr bytes.Buffer
	cmd.SetStdout(&stdout)
	cmd.SetStderr(&stderr)

	if err := cmd.Run(); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	t.Logf("stdout: %q (len=%d)", stdout.Bytes(), stdout.Len())
	t.Logf("stderr: %q (len=%d)", stderr.Bytes(), stderr.Len())

	if stdout.String() != "OUT\n" {
		t.Errorf("stdout = %q, want %q", stdout.String(), "OUT\n")
	}
	if stderr.String() != "ERR\n" {
		t.Errorf("stderr = %q, want %q", stderr.String(), "ERR\n")
	}
}

func TestCapture_StdoutOnly(t *testing.T) {
	// Test stdout only without stderr capture to verify basic capture works
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	source := setupAlpineForCapture(t, ctx)

	inst, err := New(source, withMemoryMB(128))
	if err != nil {
		if errors.Is(err, ErrHypervisorUnavailable) {
			t.Skip("Skipping: hypervisor unavailable")
		}
		t.Fatalf("New() error = %v", err)
	}
	defer inst.Close()

	// Run a command that writes to both stdout and stderr, but only capture stdout
	cmd := inst.CommandContext(ctx, "/bin/sh", "-c", "echo OUT; echo ERR >&2")

	var stdout bytes.Buffer
	cmd.SetStdout(&stdout)
	// Note: not setting stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	t.Logf("stdout: %q (len=%d)", stdout.Bytes(), stdout.Len())

	if stdout.String() != "OUT\n" {
		t.Errorf("stdout = %q, want %q", stdout.String(), "OUT\n")
	}
}

func TestCapture_StdinBasic(t *testing.T) {
	// Test basic stdin functionality with cat
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	source := setupAlpineForCapture(t, ctx)

	inst, err := New(source, withMemoryMB(128))
	if err != nil {
		if errors.Is(err, ErrHypervisorUnavailable) {
			t.Skip("Skipping: hypervisor unavailable")
		}
		t.Fatalf("New() error = %v", err)
	}
	defer inst.Close()

	// Use cat to echo stdin to stdout
	cmd := inst.CommandContext(ctx, "/bin/cat")
	cmd.SetStdin(bytes.NewReader([]byte("hello world")))

	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("Output() error = %v", err)
	}

	expected := []byte("hello world")
	if !bytes.Equal(out, expected) {
		t.Errorf("Output() = %q, want %q", out, expected)
	}
}

func TestCapture_StdinMultiLine(t *testing.T) {
	// Test multi-line stdin
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	source := setupAlpineForCapture(t, ctx)

	inst, err := New(source, withMemoryMB(128))
	if err != nil {
		if errors.Is(err, ErrHypervisorUnavailable) {
			t.Skip("Skipping: hypervisor unavailable")
		}
		t.Fatalf("New() error = %v", err)
	}
	defer inst.Close()

	input := "line1\nline2\nline3\n"
	cmd := inst.CommandContext(ctx, "/bin/cat")
	cmd.SetStdin(bytes.NewReader([]byte(input)))

	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("Output() error = %v", err)
	}

	if !bytes.Equal(out, []byte(input)) {
		t.Errorf("Output() = %q, want %q", out, input)
	}
}

func TestCapture_StdinLarge(t *testing.T) {
	// Test large stdin (>64KB pipe buffer) to ensure forked writer works
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	source := setupAlpineForCapture(t, ctx)

	inst, err := New(source, withMemoryMB(128))
	if err != nil {
		if errors.Is(err, ErrHypervisorUnavailable) {
			t.Skip("Skipping: hypervisor unavailable")
		}
		t.Fatalf("New() error = %v", err)
	}
	defer inst.Close()

	// 100KB of data - exceeds 64KB pipe buffer
	input := bytes.Repeat([]byte("x"), 100*1024)
	cmd := inst.CommandContext(ctx, "/bin/cat")
	cmd.SetStdin(bytes.NewReader(input))

	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("Output() error = %v", err)
	}

	if !bytes.Equal(out, input) {
		t.Errorf("Output() length = %d, want %d", len(out), len(input))
		if len(out) > 0 && len(input) > 0 {
			// Show first and last bytes for debugging
			t.Logf("Output first 10 bytes: %q", out[:min(10, len(out))])
			t.Logf("Output last 10 bytes: %q", out[max(0, len(out)-10):])
		}
	}
}

func TestCapture_StdinWithStdoutStderr(t *testing.T) {
	// Test stdin with both stdout and stderr capture
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	source := setupAlpineForCapture(t, ctx)

	inst, err := New(source, withMemoryMB(128))
	if err != nil {
		if errors.Is(err, ErrHypervisorUnavailable) {
			t.Skip("Skipping: hypervisor unavailable")
		}
		t.Fatalf("New() error = %v", err)
	}
	defer inst.Close()

	// Use a shell command that reads stdin and writes to both stdout and stderr
	cmd := inst.CommandContext(ctx, "/bin/sh", "-c", "cat; echo 'done' >&2")
	cmd.SetStdin(bytes.NewReader([]byte("input data\n")))

	var stdout, stderr bytes.Buffer
	cmd.SetStdout(&stdout)
	cmd.SetStderr(&stderr)

	if err := cmd.Run(); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if stdout.String() != "input data\n" {
		t.Errorf("stdout = %q, want %q", stdout.String(), "input data\n")
	}
	if stderr.String() != "done\n" {
		t.Errorf("stderr = %q, want %q", stderr.String(), "done\n")
	}
}

func TestCapture_StdinEmpty(t *testing.T) {
	// Test empty stdin (should not block)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	source := setupAlpineForCapture(t, ctx)

	inst, err := New(source, withMemoryMB(128))
	if err != nil {
		if errors.Is(err, ErrHypervisorUnavailable) {
			t.Skip("Skipping: hypervisor unavailable")
		}
		t.Fatalf("New() error = %v", err)
	}
	defer inst.Close()

	// Empty stdin should result in empty output
	cmd := inst.CommandContext(ctx, "/bin/cat")
	cmd.SetStdin(bytes.NewReader([]byte{}))

	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("Output() error = %v", err)
	}

	if len(out) != 0 {
		t.Errorf("Output() = %q, want empty", out)
	}
}
