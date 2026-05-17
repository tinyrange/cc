package vmruntime

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"j5.nz/cc/client"
)

func TestExtractManagedExecResult(t *testing.T) {
	id := "42"
	text := strings.Join([]string{
		"noise",
		CommandBeginMarker + id,
		CommandOutputMarker + id + ":" + base64.StdEncoding.EncodeToString([]byte("hello\n")),
		CommandErrorMarker + id + ":" + base64.StdEncoding.EncodeToString([]byte("warn\n")),
		CommandExitMarkerPref + id + ":7",
		"",
	}, "\n")

	code, output, _, ok := ExtractManagedExecResult(text, id, false)
	if !ok {
		t.Fatal("ExtractManagedExecResult() ok = false")
	}
	if code != 7 {
		t.Fatalf("exit code = %d, want 7", code)
	}
	if output != "hello\nwarn" {
		t.Fatalf("output = %q, want combined stdout/stderr", output)
	}
}

func TestExtractManagedExecResultParsesUsage(t *testing.T) {
	id := "9"
	payload, err := json.Marshal(client.ResourceUsage{WallSeconds: 1.2, CPUSeconds: 0.8, MaxRSSBytes: 4096})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	text := strings.Join([]string{
		CommandBeginMarker + id,
		CommandUsageMarker + id + ":" + base64.StdEncoding.EncodeToString(payload),
		CommandExitMarkerPref + id + ":0",
	}, "\n")
	_, _, usage, ok := ExtractManagedExecResult(text, id, false)
	if !ok {
		t.Fatal("ExtractManagedExecResult() ok = false")
	}
	if usage == nil || usage.WallSeconds != 1.2 || usage.CPUSeconds != 0.8 || usage.MaxRSSBytes != 4096 {
		t.Fatalf("usage = %#v", usage)
	}
}

func TestExtractManagedExecResultDmesgReturnsProtocolSegment(t *testing.T) {
	id := "42"
	text := strings.Join([]string{
		"kernel noise",
		CommandBeginMarker + id,
		CommandOutputMarker + id + ":" + base64.StdEncoding.EncodeToString([]byte("hello\n")),
		CommandExitMarkerPref + id + ":0",
		"",
	}, "\n")

	code, output, _, ok := ExtractManagedExecResult(text, id, true)
	if !ok {
		t.Fatal("ExtractManagedExecResult() ok = false")
	}
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if strings.Contains(output, "kernel noise") {
		t.Fatalf("dmesg output included pre-begin noise: %q", output)
	}
	if !strings.Contains(output, CommandOutputMarker+id+":") {
		t.Fatalf("dmesg output did not include protocol segment: %q", output)
	}
}
