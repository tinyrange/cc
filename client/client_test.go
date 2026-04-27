package client

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"golang.org/x/net/websocket"
)

func TestClientRunEvents(t *testing.T) {
	ts := httptest.NewServer(websocket.Server{
		Handshake: func(*websocket.Config, *http.Request) error { return nil },
		Handler: func(ws *websocket.Conn) {
			var req ExecRequest
			if err := websocket.JSON.Receive(ws, &req); err != nil {
				t.Fatalf("JSON.Receive(req) error = %v", err)
			}
			if len(req.Command) != 2 || req.Command[0] != "echo" || req.Command[1] != "hello" {
				t.Fatalf("req = %#v", req)
			}
			if err := websocket.JSON.Send(ws, ExecEvent{Kind: "stdout", Stream: "stdout", Output: "hello", Data: []byte("hello")}); err != nil {
				t.Fatalf("JSON.Send(output) error = %v", err)
			}
			if err := websocket.JSON.Send(ws, ExecEvent{Kind: "exit", ExitCode: 0}); err != nil {
				t.Fatalf("JSON.Send(exit) error = %v", err)
			}
		},
	})
	defer ts.Close()

	c := NewClient(ts.URL, func() (net.Conn, error) {
		return nil, nil
	})
	c.client = *ts.Client()

	events, err := c.ExecEvents(ExecRequest{
		Command: []string{"echo", "hello"},
	})
	if err != nil {
		t.Fatalf("ExecEvents() error = %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("event count = %d, want 2", len(events))
	}
	if events[0].Kind != "stdout" || events[0].Output != "hello" {
		t.Fatalf("events[0] = %#v", events[0])
	}
	if events[1].Kind != "exit" || events[1].ExitCode != 0 {
		t.Fatalf("events[1] = %#v", events[1])
	}
}

func TestClientExecStream(t *testing.T) {
	ts := httptest.NewServer(websocket.Server{
		Handshake: func(*websocket.Config, *http.Request) error { return nil },
		Handler: func(ws *websocket.Conn) {
			var req ExecRequest
			if err := websocket.JSON.Receive(ws, &req); err != nil {
				t.Fatalf("JSON.Receive(req) error = %v", err)
			}
			for _, event := range []ExecEvent{
				{Kind: "stdout", Stream: "stdout", Output: "he", Data: []byte("he")},
				{Kind: "stderr", Stream: "stderr", Output: "llo", Data: []byte("llo")},
				{Kind: "exit", ExitCode: 0},
			} {
				if err := websocket.JSON.Send(ws, event); err != nil {
					t.Fatalf("JSON.Send(event) error = %v", err)
				}
			}
		},
	})
	defer ts.Close()

	c := NewClient(ts.URL, func() (net.Conn, error) {
		return nil, nil
	})
	c.client = *ts.Client()

	var got []ExecEvent
	if err := c.ExecStream(ExecRequest{Command: []string{"echo", "hello"}}, nil, func(event ExecEvent) error {
		got = append(got, event)
		return nil
	}); err != nil {
		t.Fatalf("ExecStream() error = %v", err)
	}

	if len(got) != 3 {
		t.Fatalf("event count = %d, want 3", len(got))
	}
	if got[0].Kind != "stdout" || string(got[0].Data) != "he" || got[1].Kind != "stderr" || string(got[1].Data) != "llo" || got[2].Kind != "exit" {
		t.Fatalf("events = %#v", got)
	}
}

func TestClientDownloadKernelStream(t *testing.T) {
	var gotRequest bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRequest = true
		if r.URL.Path != "/kernel/download" || r.URL.Query().Get("stream") != "1" {
			t.Fatalf("request URL = %s, want /kernel/download?stream=1", r.URL.String())
		}
		if got := r.Header.Get("Accept"); got != "application/x-ndjson" {
			t.Fatalf("Accept = %q, want application/x-ndjson", got)
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		_ = json.NewEncoder(w).Encode(ProgressEvent{Status: "downloading", Artifact: "kernel", BytesDownloaded: 1, BytesTotal: 2})
		_ = json.NewEncoder(w).Encode(ProgressEvent{Status: "downloaded", Artifact: "kernel", BytesDownloaded: 2, BytesTotal: 2})
	}))
	defer ts.Close()

	c := NewClient(ts.URL, nil)
	c.client = *ts.Client()

	var events []ProgressEvent
	if err := c.DownloadKernelStream(DownloadRequest{}, func(event ProgressEvent) error {
		events = append(events, event)
		return nil
	}); err != nil {
		t.Fatalf("DownloadKernelStream() error = %v", err)
	}
	if !gotRequest || len(events) != 2 || events[1].Status != "downloaded" {
		t.Fatalf("events = %#v, gotRequest=%v", events, gotRequest)
	}
}

func TestClientPullImageStreamErrorEvent(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		_ = json.NewEncoder(w).Encode(ProgressEvent{Status: "error", Artifact: "alpine", Error: "boom"})
	}))
	defer ts.Close()

	c := NewClient(ts.URL, nil)
	c.client = *ts.Client()

	err := c.PullImageStream("alpine", PullImageRequest{Source: "alpine.simg"}, nil)
	if err == nil || err.Error() != "boom" {
		t.Fatalf("PullImageStream() error = %v, want boom", err)
	}
}

func TestClientCreateInstanceStream(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/vm" || r.URL.Query().Get("stream") != "1" {
			t.Fatalf("request URL = %s, want /vm?stream=1", r.URL.String())
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		_ = json.NewEncoder(w).Encode(BootEvent{Kind: "status", Message: "starting VM"})
		_ = json.NewEncoder(w).Encode(BootEvent{Kind: "ready", State: InstanceState{Status: "running", Image: "alpine"}})
	}))
	defer ts.Close()

	c := NewClient(ts.URL, nil)
	c.client = *ts.Client()

	var events []BootEvent
	state, err := c.CreateInstanceStream(CreateInstanceRequest{Image: "alpine"}, func(event BootEvent) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatalf("CreateInstanceStream() error = %v", err)
	}
	if state.Status != "running" || state.Image != "alpine" || len(events) != 2 {
		t.Fatalf("state = %#v events = %#v", state, events)
	}
}

func TestClientCreateInstanceStreamRequiresReadyEvent(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		_ = json.NewEncoder(w).Encode(BootEvent{Kind: "status", Message: "starting VM"})
	}))
	defer ts.Close()

	c := NewClient(ts.URL, nil)
	c.client = *ts.Client()

	_, err := c.CreateInstanceStream(CreateInstanceRequest{Image: "alpine"}, nil)
	if err == nil || err.Error() != "boot stream ended before ready" {
		t.Fatalf("CreateInstanceStream() error = %v, want ready error", err)
	}
}
