//go:build darwin && arm64

package hvf

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/imagefs"
	"j5.nz/cc/internal/oci"
	"j5.nz/cc/internal/vmruntime"
)

func TestExtractManagedExecResultIgnoresOtherExecTraffic(t *testing.T) {
	serial := "" +
		commandBeginMarker + "1\n" +
		commandOutputMarker + "1:aGVsbG8g\n" +
		commandBeginMarker + "2\n" +
		commandOutputMarker + "2:d29ybGQ=\n" +
		commandOutputMarker + "1:dGhlcmU=\n" +
		commandExitMarkerPref + "2:7\n" +
		commandExitMarkerPref + "1:0\n"

	exitCode, output, ok := extractManagedExecResult(serial, "1", false)
	if !ok {
		t.Fatal("extractManagedExecResult(..., 1) = not ready, want result")
	}
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, want 0", exitCode)
	}
	if output != "hello there" {
		t.Fatalf("output = %q, want %q", output, "hello there")
	}

	exitCode, output, ok = extractManagedExecResult(serial, "2", false)
	if !ok {
		t.Fatal("extractManagedExecResult(..., 2) = not ready, want result")
	}
	if exitCode != 7 {
		t.Fatalf("exitCode = %d, want 7", exitCode)
	}
	if output != "world" {
		t.Fatalf("output = %q, want %q", output, "world")
	}
}

func TestContainerSessionExecAllowsConcurrentCommands(t *testing.T) {
	rootfs := t.TempDir()
	for _, name := range []string{"one", "two"} {
		path := filepath.Join(rootfs, "bin", name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", path, err)
		}
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatalf("WriteFile(%q) error = %v", path, err)
		}
	}

	transcript := newSerialTranscript()
	control := &fakeVsockConn{
		writeFn: func(data []byte) (int, error) {
			var req guestExecRequest
			if err := json.Unmarshal(data[:len(data)-1], &req); err != nil {
				return 0, err
			}
			if req.Kind != "" && req.Kind != "exec" {
				return len(data), nil
			}
			go func() {
				switch req.Command[0] {
				case "/bin/one":
					_, _ = transcript.Write([]byte(commandBeginMarker + req.ID + "\n"))
					time.Sleep(5 * time.Millisecond)
					_, _ = transcript.Write([]byte(commandOutputMarker + req.ID + ":b25l\n"))
					time.Sleep(15 * time.Millisecond)
					_, _ = transcript.Write([]byte(commandExitMarkerPref + req.ID + ":0\n"))
				case "/bin/two":
					time.Sleep(2 * time.Millisecond)
					_, _ = transcript.Write([]byte(commandBeginMarker + req.ID + "\n"))
					time.Sleep(5 * time.Millisecond)
					_, _ = transcript.Write([]byte(commandOutputMarker + req.ID + ":dHdv\n"))
					time.Sleep(5 * time.Millisecond)
					_, _ = transcript.Write([]byte(commandExitMarkerPref + req.ID + ":0\n"))
				default:
					_, _ = transcript.Write([]byte(commandBeginMarker + req.ID + "\n"))
					_, _ = transcript.Write([]byte(commandExitMarkerPref + req.ID + ":127\n"))
				}
			}()
			return len(data), nil
		},
	}

	session := &ContainerSession{
		image: &oci.Image{
			RootFSDir: rootfs,
			RootFS:    imagefs.NewHostFS(rootfs, nil),
			Config: oci.RuntimeConfig{
				Env: []string{"PATH=/bin"},
			},
		},
		baseEnv:    []string{"PATH=/bin"},
		workDir:    "/",
		control:    control,
		transcript: transcript,
	}

	var wg sync.WaitGroup
	type result struct {
		cmd    string
		output string
		err    error
	}
	resultsCh := make(chan result, 2)
	for _, cmd := range []string{"/bin/one", "/bin/two"} {
		wg.Add(1)
		go func(cmd string) {
			defer wg.Done()
			resp, err := session.Exec(context.Background(), client.ExecRequest{
				Command: []string{cmd},
			})
			resultsCh <- result{cmd: cmd, output: resp.Output, err: err}
		}(cmd)
	}
	wg.Wait()
	close(resultsCh)

	got := map[string]string{}
	for res := range resultsCh {
		if res.err != nil {
			t.Fatalf("Exec(%q) error = %v", res.cmd, res.err)
		}
		got[res.cmd] = res.output
	}

	if got["/bin/one"] != "one" {
		t.Fatalf("output for /bin/one = %q, want %q", got["/bin/one"], "one")
	}
	if got["/bin/two"] != "two" {
		t.Fatalf("output for /bin/two = %q, want %q", got["/bin/two"], "two")
	}
	if !slices.Equal(control.commands(), []string{"/bin/one", "/bin/two"}) && !slices.Equal(control.commands(), []string{"/bin/two", "/bin/one"}) {
		t.Fatalf("unexpected control commands = %v", control.commands())
	}
}

func TestContainerSessionExecIncludesStdin(t *testing.T) {
	rootfs := t.TempDir()
	path := filepath.Join(rootfs, "bin", "cat")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", path, err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}

	transcript := newSerialTranscript()
	var got guestExecRequest
	control := &fakeVsockConn{
		writeFn: func(data []byte) (int, error) {
			if err := json.Unmarshal(data[:len(data)-1], &got); err != nil {
				return 0, err
			}
			go func() {
				_, _ = transcript.Write([]byte(commandBeginMarker + got.ID + "\n"))
				_, _ = transcript.Write([]byte(commandExitMarkerPref + got.ID + ":0\n"))
			}()
			return len(data), nil
		},
	}

	session := &ContainerSession{
		image: &oci.Image{
			RootFSDir: rootfs,
			RootFS:    imagefs.NewHostFS(rootfs, nil),
			Config: oci.RuntimeConfig{
				Env: []string{"PATH=/bin"},
			},
		},
		baseEnv:    []string{"PATH=/bin"},
		workDir:    "/",
		control:    control,
		transcript: transcript,
	}

	_, err := session.Exec(context.Background(), client.ExecRequest{
		Command: []string{"/bin/cat"},
		Stdin:   []byte("hello\n"),
	})
	if err != nil {
		t.Fatalf("Exec() error = %v", err)
	}
	if string(got.Stdin) != "hello\n" {
		t.Fatalf("serialized stdin = %q, want %q", string(got.Stdin), "hello\n")
	}
}

func TestContainerSessionExecStreamForwardsTTYControl(t *testing.T) {
	rootfs := t.TempDir()
	path := filepath.Join(rootfs, "bin", "sh")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", path, err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}

	transcript := newSerialTranscript()
	var (
		mu       sync.Mutex
		requests []guestExecRequest
		execID   string
	)
	control := &fakeVsockConn{
		writeFn: func(data []byte) (int, error) {
			var req guestExecRequest
			if err := json.Unmarshal(data[:len(data)-1], &req); err != nil {
				return 0, err
			}
			mu.Lock()
			requests = append(requests, req)
			if req.Kind == "exec" {
				execID = req.ID
			}
			mu.Unlock()
			if req.Kind == "exec" {
				go func(id string) {
					time.Sleep(5 * time.Millisecond)
					_, _ = transcript.Write([]byte(commandBeginMarker + id + "\n"))
					time.Sleep(10 * time.Millisecond)
					_, _ = transcript.Write([]byte(commandExitMarkerPref + id + ":0\n"))
				}(req.ID)
			}
			return len(data), nil
		},
	}

	session := &ContainerSession{
		image: &oci.Image{
			RootFSDir: rootfs,
			RootFS:    imagefs.NewHostFS(rootfs, nil),
			Config: oci.RuntimeConfig{
				Env: []string{"PATH=/bin"},
			},
		},
		baseEnv:    []string{"PATH=/bin"},
		workDir:    "/",
		control:    control,
		transcript: transcript,
	}

	inputs := make(chan client.ExecInput, 4)
	go func() {
		inputs <- client.ExecInput{Kind: "signal", Signal: "INT"}
		inputs <- client.ExecInput{Kind: "resize", Cols: 120, Rows: 40}
		close(inputs)
	}()

	if err := session.ExecStream(context.Background(), client.ExecRequest{
		Command: []string{"/bin/sh"},
		TTY:     true,
		Cols:    100,
		Rows:    30,
	}, inputs, nil); err != nil {
		t.Fatalf("ExecStream() error = %v", err)
	}

	mu.Lock()
	got := append([]guestExecRequest(nil), requests...)
	gotExecID := execID
	mu.Unlock()
	if len(got) < 4 {
		t.Fatalf("control requests = %d, want at least 4", len(got))
	}
	if got[0].Kind != "exec" || !got[0].TTY || got[0].Cols != 100 || got[0].Rows != 30 {
		t.Fatalf("exec request = %#v, want tty exec with initial size", got[0])
	}
	if got[1].Kind != "signal" || got[1].ID != gotExecID || got[1].Signal != "INT" {
		t.Fatalf("signal request = %#v, want INT for exec %q", got[1], gotExecID)
	}
	if got[2].Kind != "resize" || got[2].ID != gotExecID || got[2].Cols != 120 || got[2].Rows != 40 {
		t.Fatalf("resize request = %#v, want 120x40 for exec %q", got[2], gotExecID)
	}
	if got[3].Kind != "stdin_close" || got[3].ID != gotExecID {
		t.Fatalf("stdin_close request = %#v, want close for exec %q", got[3], gotExecID)
	}
}

type fakeVsockConn struct {
	mu      sync.Mutex
	writes  [][]byte
	writeFn func([]byte) (int, error)
}

func (f *fakeVsockConn) Read(p []byte) (int, error) {
	_ = p
	return 0, io.EOF
}

func (f *fakeVsockConn) Write(p []byte) (int, error) {
	f.mu.Lock()
	f.writes = append(f.writes, append([]byte(nil), p...))
	fn := f.writeFn
	f.mu.Unlock()
	if fn != nil {
		return fn(p)
	}
	return len(p), nil
}

func (f *fakeVsockConn) Close() error {
	return nil
}

func (f *fakeVsockConn) LocalPort() uint32 {
	return 0
}

func (f *fakeVsockConn) RemotePort() uint32 {
	return vmruntime.ControlPort
}

func (f *fakeVsockConn) commands() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.writes))
	for _, payload := range f.writes {
		var req guestExecRequest
		if err := json.Unmarshal(payload[:len(payload)-1], &req); err == nil && len(req.Command) > 0 {
			out = append(out, req.Command[0])
		}
	}
	return out
}
