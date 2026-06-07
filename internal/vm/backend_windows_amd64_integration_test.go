//go:build windows && amd64

package vm

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/kernel/alpine"
	"j5.nz/cc/internal/oci"
)

func TestWindowsRuntimeBackendRunCommand(t *testing.T) {
	if os.Getenv("CCX3_WHP_BOOT") == "" {
		t.Skip("set CCX3_WHP_BOOT=1 to run the windows amd64 WHP boot probe")
	}

	ctx, cancel := context.WithTimeout(context.Background(), windowsBootTestTimeout(t))
	defer cancel()
	backend := newWindowsRuntimeBackendForTest(t, ctx)

	resp, err := backend.Run(ctx, client.RunRequest{
		Image:    "alpine",
		Command:  []string{"sh", "-c", "echo windows-amd64-ok"},
		MemoryMB: 256,
	})
	if err != nil {
		t.Fatalf("backend.Run() error = %v\noutput:\n%s", err, resp.Output)
	}
	if resp.ExitCode != 0 {
		t.Fatalf("backend.Run().ExitCode = %d, want 0\noutput:\n%s", resp.ExitCode, resp.Output)
	}
	if strings.TrimSpace(resp.Output) != "windows-amd64-ok" {
		t.Fatalf("backend.Run().Output = %q, want windows-amd64-ok", resp.Output)
	}
}

func TestWindowsRuntimeBackendPortForwardToGuestWebServer(t *testing.T) {
	if os.Getenv("CCX3_WHP_BOOT") == "" {
		t.Skip("set CCX3_WHP_BOOT=1 to run the windows amd64 WHP boot probe")
	}

	ctx, cancel := context.WithTimeout(context.Background(), windowsBootTestTimeout(t))
	defer cancel()
	backend := newWindowsRuntimeBackendForTest(t, ctx)
	hostPort := reserveWindowsRuntimeForwardPort(t)

	inst, err := backend.Start(ctx, client.CreateInstanceRequest{
		Image: "alpine",
		Network: &client.NetworkConfig{
			Enabled: true,
			PortForwards: []client.PortForward{{
				Protocol:  "tcp",
				HostAddr:  "127.0.0.1",
				HostPort:  hostPort,
				GuestPort: 8080,
			}},
		},
		MemoryMB: 256,
	})
	if err != nil {
		t.Fatalf("backend.Start() error = %v", err)
	}
	defer inst.Close()

	resp, err := inst.Exec(ctx, client.ExecRequest{
		Command: []string{"sh", "-c", "while true; do printf 'HTTP/1.1 200 OK\\r\\nContent-Length: 23\\r\\nConnection: close\\r\\n\\r\\nwindows-portforward-ok\\n' | nc -l -p 8080; done >/tmp/cc-port-forward.log 2>&1 & echo server-ready"},
	})
	if err != nil {
		t.Fatalf("start guest web server error = %v\noutput:\n%s", err, resp.Output)
	}
	if resp.ExitCode != 0 || !strings.Contains(resp.Output, "server-ready") {
		t.Fatalf("start guest web server exit=%d output:\n%s", resp.ExitCode, resp.Output)
	}

	body := fetchWindowsRuntimeURL(t, fmt.Sprintf("http://127.0.0.1:%d/", hostPort), 5*time.Second)
	if strings.TrimSpace(body) != "windows-portforward-ok" {
		t.Fatalf("unexpected forwarded response %q", body)
	}
}

func TestWindowsRuntimeBackendStartThenExec(t *testing.T) {
	if os.Getenv("CCX3_WHP_BOOT") == "" {
		t.Skip("set CCX3_WHP_BOOT=1 to run the windows amd64 WHP boot probe")
	}

	ctx, cancel := context.WithTimeout(context.Background(), windowsBootTestTimeout(t))
	defer cancel()
	backend := newWindowsRuntimeBackendForTest(t, ctx)
	mgr := NewManagerWithBackend(backend)

	state, err := mgr.Start(ctx, client.CreateInstanceRequest{
		Image:    "alpine",
		MemoryMB: 256,
	})
	if err != nil {
		t.Fatalf("mgr.Start() error = %v", err)
	}
	if state.Status != "running" {
		t.Fatalf("mgr.Start().Status = %q, want running", state.Status)
	}
	defer mgr.Shutdown(context.Background())

	resp, err := mgr.Run(ctx, client.RunRequest{
		Image:   "alpine",
		Command: []string{"sh", "-c", "echo windows-amd64-start-ok"},
	})
	if err != nil {
		t.Fatalf("mgr.Run() error = %v\noutput:\n%s", err, resp.Output)
	}
	if resp.ExitCode != 0 {
		t.Fatalf("mgr.Run().ExitCode = %d, want 0\noutput:\n%s", resp.ExitCode, resp.Output)
	}
	if strings.TrimSpace(resp.Output) != "windows-amd64-start-ok" {
		t.Fatalf("mgr.Run().Output = %q, want windows-amd64-start-ok", resp.Output)
	}
}

func TestWindowsRuntimeBackendRunStreamForwardsStdin(t *testing.T) {
	if os.Getenv("CCX3_WHP_BOOT") == "" {
		t.Skip("set CCX3_WHP_BOOT=1 to run the windows amd64 WHP boot probe")
	}

	ctx, cancel := context.WithTimeout(context.Background(), windowsBootTestTimeout(t))
	defer cancel()
	backend := newWindowsRuntimeBackendForTest(t, ctx)
	mgr := NewManagerWithBackend(backend)

	state, err := mgr.Start(ctx, client.CreateInstanceRequest{
		Image:    "alpine",
		MemoryMB: 256,
	})
	if err != nil {
		t.Fatalf("mgr.Start() error = %v", err)
	}
	if state.Status != "running" {
		t.Fatalf("mgr.Start().Status = %q, want running", state.Status)
	}
	defer mgr.Shutdown(context.Background())

	inputs := make(chan client.ExecInput, 2)
	inputs <- client.ExecInput{Kind: "stdin", Input: "vm-stream-input\n"}
	close(inputs)

	var events []client.ExecEvent
	err = mgr.RunStream(ctx, client.RunRequest{
		Image:   "alpine",
		Command: []string{"sh", "-c", "cat"},
	}, inputs, func(event client.ExecEvent) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatalf("mgr.RunStream() error = %v", err)
	}
	var output strings.Builder
	exitSeen := false
	for _, event := range events {
		if event.Kind == "stdout" {
			output.WriteString(event.Output)
		}
		if event.Kind == "exit" {
			exitSeen = true
			if event.ExitCode != 0 {
				t.Fatalf("stream exit code = %d, want 0", event.ExitCode)
			}
		}
	}
	if !exitSeen {
		t.Fatalf("stream did not emit exit event: %#v", events)
	}
	if output.String() != "vm-stream-input\n" {
		t.Fatalf("stream output = %q, want vm-stream-input\\n", output.String())
	}
}

func TestWindowsRuntimeBackendStartWithWritableShare(t *testing.T) {
	if os.Getenv("CCX3_WHP_BOOT") == "" {
		t.Skip("set CCX3_WHP_BOOT=1 to run the windows amd64 WHP boot probe")
	}

	ctx, cancel := context.WithTimeout(context.Background(), windowsBootTestTimeout(t))
	defer cancel()
	root := t.TempDir()
	shareDir := filepath.Join(root, "share")
	if err := os.MkdirAll(shareDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(share) error = %v", err)
	}
	backend := newWindowsRuntimeBackendForTest(t, ctx)
	mgr := NewManagerWithBackend(backend)

	state, err := mgr.Start(ctx, client.CreateInstanceRequest{
		Image: "alpine",
		Shares: []client.ShareMount{{
			Source:   shareDir,
			Mount:    "/work",
			Writable: true,
		}},
		MemoryMB: 256,
	})
	if err != nil {
		t.Fatalf("mgr.Start() error = %v", err)
	}
	if state.Status != "running" {
		t.Fatalf("mgr.Start().Status = %q, want running", state.Status)
	}
	defer mgr.Shutdown(context.Background())

	resp, err := mgr.Run(ctx, client.RunRequest{
		Image:   "alpine",
		Command: []string{"/bin/sh", "-lc", "echo hello-windows-share > /work/hello.txt && cat /work/hello.txt"},
	})
	if err != nil {
		t.Fatalf("mgr.Run() error = %v\noutput:\n%s", err, resp.Output)
	}
	if resp.ExitCode != 0 {
		t.Fatalf("mgr.Run().ExitCode = %d, want 0\noutput:\n%s", resp.ExitCode, resp.Output)
	}
	if strings.TrimSpace(resp.Output) != "hello-windows-share" {
		t.Fatalf("mgr.Run().Output = %q, want hello-windows-share", resp.Output)
	}
	buf, err := os.ReadFile(filepath.Join(shareDir, "hello.txt"))
	if err != nil {
		t.Fatalf("ReadFile(host share) error = %v", err)
	}
	if strings.TrimSpace(string(buf)) != "hello-windows-share" {
		t.Fatalf("host share contents = %q, want hello-windows-share", string(buf))
	}
}

func newWindowsRuntimeBackendForTest(t *testing.T, ctx context.Context) Backend {
	t.Helper()
	fixture := filepath.Join("..", "..", "fixtures", "alpine.simg")
	if _, err := os.Stat(fixture); err != nil {
		t.Skipf("local alpine fixture unavailable: %v", err)
	}

	root := t.TempDir()
	kernel := alpine.NewManager(filepath.Join(root, "kernel"))
	if err := kernel.Ensure(ctx); err != nil {
		t.Fatalf("kernel.Ensure() error = %v", err)
	}
	store := oci.NewStore(filepath.Join(root, "images"))
	if _, err := store.Pull(ctx, "alpine", fixture); err != nil {
		t.Fatalf("store.Pull() error = %v", err)
	}
	return NewRuntimeBackend(kernel, store, filepath.Join(root, "guestinit"))
}

func reserveWindowsRuntimeForwardPort(t testing.TB) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve local tcp port: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func fetchWindowsRuntimeURL(t testing.TB, url string, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			body, readErr := io.ReadAll(resp.Body)
			closeErr := resp.Body.Close()
			if readErr == nil {
				readErr = closeErr
			}
			if readErr == nil && resp.StatusCode == http.StatusOK {
				return string(body)
			}
			if readErr != nil {
				lastErr = readErr
			} else {
				lastErr = fmt.Errorf("status %s body %q", resp.Status, string(body))
			}
		} else {
			lastErr = err
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("fetch %s did not succeed within %s: %v", url, timeout, lastErr)
	return ""
}
