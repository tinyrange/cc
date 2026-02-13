package api

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"
)

func skipUnlessVMTests(t *testing.T) {
	t.Helper()
	if os.Getenv("CC_RUN_VM_TESTS") != "1" {
		t.Skip("CC_RUN_VM_TESTS not set")
	}
}

func createTestInstance(t *testing.T) Instance {
	t.Helper()

	client, err := NewOCIClient()
	if err != nil {
		t.Fatalf("NewOCIClient: %v", err)
	}

	source, err := client.Pull(context.Background(), "alpine:latest")
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}

	inst, err := New(source)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { inst.Close() })

	return inst
}

func TestStreamingStdout(t *testing.T) {
	skipUnlessVMTests(t)

	inst := createTestInstance(t)

	var buf bytes.Buffer
	cmd := inst.Command("echo", "hello")
	cmd.SetStdout(&buf)

	if err := cmd.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !strings.Contains(buf.String(), "hello") {
		t.Fatalf("expected 'hello' in stdout, got: %q", buf.String())
	}
}

func TestStreamingStderr(t *testing.T) {
	skipUnlessVMTests(t)

	inst := createTestInstance(t)

	var buf bytes.Buffer
	cmd := inst.Command("sh", "-c", "echo err >&2")
	cmd.SetStderr(&buf)

	if err := cmd.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !strings.Contains(buf.String(), "err") {
		t.Fatalf("expected 'err' in stderr, got: %q", buf.String())
	}
}

func TestStreamingCombined(t *testing.T) {
	skipUnlessVMTests(t)

	inst := createTestInstance(t)

	var buf bytes.Buffer
	cmd := inst.Command("sh", "-c", "echo out; echo err >&2")
	cmd.SetStdout(&buf)
	cmd.SetStderr(&buf)

	if err := cmd.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "out") {
		t.Fatalf("expected 'out' in output, got: %q", output)
	}
	if !strings.Contains(output, "err") {
		t.Fatalf("expected 'err' in output, got: %q", output)
	}
}

func TestStdoutPipe(t *testing.T) {
	skipUnlessVMTests(t)

	inst := createTestInstance(t)

	cmd := inst.Command("seq", "1", "10")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe: %v", err)
	}

	if err := cmd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	data, err := io.ReadAll(stdout)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}

	if err := cmd.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 10 {
		t.Fatalf("expected 10 lines, got %d: %q", len(lines), string(data))
	}
}

func TestStderrPipe(t *testing.T) {
	skipUnlessVMTests(t)

	inst := createTestInstance(t)

	cmd := inst.Command("sh", "-c", "seq 1 5 >&2")
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatalf("StderrPipe: %v", err)
	}

	if err := cmd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	data, err := io.ReadAll(stderr)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}

	if err := cmd.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 5 {
		t.Fatalf("expected 5 lines, got %d: %q", len(lines), string(data))
	}
}

func TestStdinPipe(t *testing.T) {
	skipUnlessVMTests(t)

	inst := createTestInstance(t)

	cmd := inst.Command("cat")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("StdinPipe: %v", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe: %v", err)
	}

	if err := cmd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Write to stdin and close
	testData := "hello from pipe"
	if _, err := io.WriteString(stdin, testData); err != nil {
		t.Fatalf("WriteString: %v", err)
	}
	stdin.Close()

	// Read from stdout
	data, err := io.ReadAll(stdout)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}

	if err := cmd.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}

	if !strings.Contains(string(data), testData) {
		t.Fatalf("expected %q in output, got: %q", testData, string(data))
	}
}

func TestStreamingWithExitCode(t *testing.T) {
	skipUnlessVMTests(t)

	inst := createTestInstance(t)

	var buf bytes.Buffer
	cmd := inst.Command("sh", "-c", "echo output; exit 42")
	cmd.SetStdout(&buf)

	err := cmd.Run()
	if err == nil {
		t.Fatal("expected error for non-zero exit code")
	}

	if !strings.Contains(buf.String(), "output") {
		t.Fatalf("expected 'output' in stdout, got: %q", buf.String())
	}

	if cmd.ExitCode() != 42 {
		t.Fatalf("expected exit code 42, got: %d", cmd.ExitCode())
	}
}

func TestOutputStillWorks(t *testing.T) {
	skipUnlessVMTests(t)

	inst := createTestInstance(t)

	// Verify Output() still works (backward compatibility)
	data, err := inst.Command("echo", "backward compat").Output()
	if err != nil {
		t.Fatalf("Output: %v", err)
	}

	if !strings.Contains(string(data), "backward compat") {
		t.Fatalf("expected 'backward compat' in output, got: %q", string(data))
	}

	// Verify CombinedOutput() still works
	data, err = inst.Command("echo", "combined test").CombinedOutput()
	if err != nil {
		t.Fatalf("CombinedOutput: %v", err)
	}

	if !strings.Contains(string(data), "combined test") {
		t.Fatalf("expected 'combined test' in output, got: %q", string(data))
	}
}
