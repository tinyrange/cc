//go:build linux && amd64

package kvm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"sync"
	"testing"
	"time"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/vmruntime"
)

func TestManagedSessionExecStreamForwardsInputsAndStreamsEvents(t *testing.T) {
	transcript := vmruntime.NewSerialTranscript()
	writes := make(chan vmruntime.ManagedExecRequest, 8)
	conn := &fakeVsockConn{
		writeFn: func(data []byte) (int, error) {
			var req vmruntime.ManagedExecRequest
			if err := json.Unmarshal(data, &req); err != nil {
				t.Errorf("decode control write: %v", err)
				return len(data), nil
			}
			writes <- req
			if req.Kind == "stdin_close" {
				transcript.Write([]byte(vmruntime.CommandBeginMarker + req.ID + "\n"))
				transcript.Write([]byte(vmruntime.CommandOutputMarker + req.ID + ":" + base64.StdEncoding.EncodeToString([]byte("out")) + "\n"))
				transcript.Write([]byte(vmruntime.CommandErrorMarker + req.ID + ":" + base64.StdEncoding.EncodeToString([]byte("err")) + "\n"))
				transcript.Write([]byte(vmruntime.CommandExitMarkerPref + req.ID + ":7\n"))
			}
			return len(data), nil
		},
	}
	session := &ManagedSession{
		control:    conn,
		transcript: transcript,
		serialOut:  vmruntime.NewSerialTranscript(),
	}

	inputs := make(chan client.ExecInput, 4)
	inputs <- client.ExecInput{Kind: "stdin", Data: []byte("hello\n")}
	inputs <- client.ExecInput{Kind: "signal", Signal: "INT"}
	inputs <- client.ExecInput{Kind: "resize", Cols: 120, Rows: 40}
	close(inputs)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var events []client.ExecEvent
	err := session.ExecStream(ctx, client.ExecRequest{
		Command: []string{"/bin/sh"},
		TTY:     true,
		Cols:    100,
		Rows:    30,
	}, inputs, func(event client.ExecEvent) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatalf("ExecStream() error = %v", err)
	}

	got := drainExecRequests(writes)
	if len(got) != 5 {
		t.Fatalf("control write count = %d, want 5: %#v", len(got), got)
	}
	execID := got[0].ID
	if got[0].Kind != "exec" || !got[0].TTY || got[0].Cols != 100 || got[0].Rows != 30 {
		t.Fatalf("exec request = %#v", got[0])
	}
	if got[1].Kind != "stdin" || got[1].ID != execID || string(got[1].Stdin) != "hello\n" {
		t.Fatalf("stdin request = %#v", got[1])
	}
	if got[2].Kind != "signal" || got[2].ID != execID || got[2].Signal != "INT" {
		t.Fatalf("signal request = %#v", got[2])
	}
	if got[3].Kind != "resize" || got[3].ID != execID || got[3].Cols != 120 || got[3].Rows != 40 {
		t.Fatalf("resize request = %#v", got[3])
	}
	if got[4].Kind != "stdin_close" || got[4].ID != execID {
		t.Fatalf("stdin_close request = %#v", got[4])
	}

	if len(events) != 3 {
		t.Fatalf("event count = %d, want 3: %#v", len(events), events)
	}
	if events[0].Kind != "stdout" || events[0].Output != "out" {
		t.Fatalf("stdout event = %#v", events[0])
	}
	if events[1].Kind != "stderr" || events[1].Output != "err" {
		t.Fatalf("stderr event = %#v", events[1])
	}
	if events[2].Kind != "exit" || events[2].ExitCode != 7 {
		t.Fatalf("exit event = %#v", events[2])
	}
}

func TestManagedSessionExecStreamClosesStdinWhenNoInputStream(t *testing.T) {
	transcript := vmruntime.NewSerialTranscript()
	writes := make(chan vmruntime.ManagedExecRequest, 4)
	conn := &fakeVsockConn{
		writeFn: func(data []byte) (int, error) {
			var req vmruntime.ManagedExecRequest
			if err := json.Unmarshal(data, &req); err != nil {
				t.Errorf("decode control write: %v", err)
				return len(data), nil
			}
			writes <- req
			if req.Kind == "stdin_close" {
				transcript.Write([]byte(vmruntime.CommandExitMarkerPref + req.ID + ":0\n"))
			}
			return len(data), nil
		},
	}
	session := &ManagedSession{
		control:    conn,
		transcript: transcript,
		serialOut:  vmruntime.NewSerialTranscript(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := session.ExecStream(ctx, client.ExecRequest{Command: []string{"true"}}, nil, nil); err != nil {
		t.Fatalf("ExecStream() error = %v", err)
	}

	got := drainExecRequests(writes)
	if len(got) != 2 {
		t.Fatalf("control write count = %d, want 2: %#v", len(got), got)
	}
	if got[0].Kind != "exec" || got[1].Kind != "stdin_close" || got[0].ID != got[1].ID {
		t.Fatalf("control writes = %#v", got)
	}
}

func drainExecRequests(ch <-chan vmruntime.ManagedExecRequest) []vmruntime.ManagedExecRequest {
	var out []vmruntime.ManagedExecRequest
	for {
		select {
		case req := <-ch:
			out = append(out, req)
		default:
			return out
		}
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
	return 0
}
