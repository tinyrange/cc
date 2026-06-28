package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/websocket"
)

func TestClientRunForwardStatusAndErrorDecoding(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/vm/run", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("run method = %s", r.Method)
		}
		var req RunRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode run request: %v", err)
		}
		if req.ID != "vm one" || strings.Join(req.Command, " ") != "echo ok" {
			t.Fatalf("run request = %+v", req)
		}
		writeTestJSON(w, http.StatusOK, ExecResponse{ExitCode: 3, Output: "ran"})
	})
	mux.HandleFunc("/vm/forward", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("id") != "vm one" {
			t.Fatalf("forward id query = %q", r.URL.RawQuery)
		}
		var req PortForward
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode forward request: %v", err)
		}
		if req.HostPort != 8080 || req.GuestPort != 80 {
			t.Fatalf("forward request = %+v", req)
		}
		writeTestJSON(w, http.StatusOK, req)
	})
	mux.HandleFunc("/vm/status", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("id") != "vm one" {
			t.Fatalf("status id query = %q", r.URL.RawQuery)
		}
		writeTestJSON(w, http.StatusOK, InstanceState{ID: "vm one", Status: "running"})
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeTestJSON(w, http.StatusServiceUnavailable, ErrorResponse{Error: "daemon offline"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := &Client{url: srv.URL, client: *srv.Client()}
	resp, err := c.RunIn("vm one", RunRequest{Command: []string{"echo", "ok"}})
	if err != nil {
		t.Fatalf("RunIn: %v", err)
	}
	if resp.ExitCode != 3 || resp.Output != "ran" {
		t.Fatalf("RunIn response = %+v", resp)
	}
	if err := c.AddPortForwardTo("vm one", PortForward{Protocol: "tcp", HostPort: 8080, GuestPort: 80}); err != nil {
		t.Fatalf("AddPortForwardTo: %v", err)
	}
	state, err := c.InstanceStatusOf("vm one")
	if err != nil {
		t.Fatalf("InstanceStatusOf: %v", err)
	}
	if state.Status != "running" {
		t.Fatalf("state = %+v", state)
	}
	if err := c.HealthCheck(); err == nil || err.Error() != "daemon offline" {
		t.Fatalf("HealthCheck error = %v", err)
	}
}

func TestClientBearerTokenIsSent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Fatalf("Authorization = %q", got)
		}
		writeTestJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}))
	defer srv.Close()

	c := &Client{
		url:       srv.URL,
		authToken: "secret",
		client: http.Client{
			Transport: &authTransport{
				base: srv.Client().Transport,
				token: func() string {
					return "secret"
				},
			},
		},
	}
	if err := c.HealthCheck(); err != nil {
		t.Fatalf("health check: %v", err)
	}
}

func TestClientRunStreamDecodesNDJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/vm/run" || r.URL.Query().Get("stream") != "1" {
			t.Fatalf("stream request target = %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		if got := r.Header.Get("Accept"); got != "application/x-ndjson" {
			t.Fatalf("Accept = %q", got)
		}
		var req RunRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode run stream request: %v", err)
		}
		if req.Image != "alpine" || strings.Join(req.Command, " ") != "echo hi" {
			t.Fatalf("run stream request = %+v", req)
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(http.StatusOK)
		enc := json.NewEncoder(w)
		_ = enc.Encode(ExecEvent{Kind: "stdout", Output: "hi"})
		_ = enc.Encode(ExecEvent{Kind: "exit", ExitCode: 7})
	}))
	defer srv.Close()

	c := &Client{url: srv.URL, client: *srv.Client()}
	var events []ExecEvent
	err := c.RunStreamContext(
		t.Context(),
		RunRequest{Image: "alpine", Command: []string{"echo", "hi"}},
		func(event ExecEvent) error {
			events = append(events, event)
			return nil
		},
	)
	if err != nil {
		t.Fatalf("RunStreamContext: %v", err)
	}
	if len(events) != 2 || events[0].Kind != "stdout" || events[1].Kind != "exit" || events[1].ExitCode != 7 {
		t.Fatalf("events = %+v", events)
	}
}

func TestClientRunInteractiveStreamHalfClosesStdin(t *testing.T) {
	inputsSeen := make(chan []ExecInput, 1)
	errs := make(chan error, 1)
	mux := http.NewServeMux()
	mux.Handle("/vm/run/stream", websocket.Server{
		Handshake: func(*websocket.Config, *http.Request) error { return nil },
		Handler: func(ws *websocket.Conn) {
			var req RunRequest
			if err := websocket.JSON.Receive(ws, &req); err != nil {
				errs <- err
				return
			}
			var got []ExecInput
			for {
				var input ExecInput
				if err := websocket.JSON.Receive(ws, &input); err != nil {
					errs <- err
					return
				}
				got = append(got, input)
				if input.Kind == "stdin_close" {
					break
				}
			}
			_ = ws.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
			var extra ExecInput
			if err := websocket.JSON.Receive(ws, &extra); err == nil {
				got = append(got, extra)
			}
			_ = ws.SetReadDeadline(time.Time{})
			inputsSeen <- got
			if err := websocket.JSON.Send(ws, ExecEvent{Kind: "stdout", Output: "after-eof"}); err != nil {
				errs <- err
				return
			}
			if err := websocket.JSON.Send(ws, ExecEvent{Kind: "exit", ExitCode: 0}); err != nil {
				errs <- err
				return
			}
			errs <- nil
		},
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	inputs := make(chan ExecInput, 4)
	inputs <- ExecInput{Kind: "stdin", Input: "hello"}
	inputs <- ExecInput{Kind: "stdin_close"}
	inputs <- ExecInput{Kind: "stdin", Input: "ignored-after-close"}
	close(inputs)

	c := &Client{url: srv.URL, client: *srv.Client()}
	var output strings.Builder
	err := c.RunInteractiveStreamIn("vm", RunRequest{Image: "alpine", Command: []string{"sh"}}, inputs, func(event ExecEvent) error {
		if event.Kind == "stdout" {
			output.WriteString(event.Output)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("RunInteractiveStreamIn: %v", err)
	}
	if output.String() != "after-eof" {
		t.Fatalf("output = %q", output.String())
	}
	select {
	case got := <-inputsSeen:
		if len(got) != 2 || got[0].Kind != "stdin" || got[0].Input != "hello" || got[1].Kind != "stdin_close" {
			t.Fatalf("inputs = %+v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("server did not receive stdin EOF")
	}
	if err := <-errs; err != nil {
		t.Fatalf("server error: %v", err)
	}
}

func TestClientRunInteractiveStreamNilInputsClosesStdin(t *testing.T) {
	inputsSeen := make(chan []ExecInput, 1)
	errs := make(chan error, 1)
	mux := http.NewServeMux()
	mux.Handle("/vm/run/stream", websocket.Server{
		Handshake: func(*websocket.Config, *http.Request) error { return nil },
		Handler: func(ws *websocket.Conn) {
			var req RunRequest
			if err := websocket.JSON.Receive(ws, &req); err != nil {
				errs <- err
				return
			}
			var got []ExecInput
			var input ExecInput
			if err := websocket.JSON.Receive(ws, &input); err != nil {
				errs <- err
				return
			}
			got = append(got, input)
			inputsSeen <- got
			if err := websocket.JSON.Send(ws, ExecEvent{Kind: "exit", ExitCode: 0}); err != nil {
				errs <- err
				return
			}
			errs <- nil
		},
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := &Client{url: srv.URL, client: *srv.Client()}
	if err := c.RunInteractiveStreamIn("vm", RunRequest{Image: "alpine", Command: []string{"true"}}, nil, func(ExecEvent) error {
		return nil
	}); err != nil {
		t.Fatalf("RunInteractiveStreamIn: %v", err)
	}
	select {
	case got := <-inputsSeen:
		if len(got) != 1 || got[0].Kind != "stdin_close" {
			t.Fatalf("inputs = %+v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("server did not receive stdin EOF")
	}
	if err := <-errs; err != nil {
		t.Fatalf("server error: %v", err)
	}
}

func TestClientURLHelpers(t *testing.T) {
	got, err := websocketURL("https://example.test/base?x=1", "/vm/run")
	if err != nil {
		t.Fatalf("websocketURL: %v", err)
	}
	if got != "wss://example.test/vm/run" {
		t.Fatalf("websocketURL = %q", got)
	}
	if _, err := websocketURL("unix:///tmp/socket", "/vm/run"); err == nil {
		t.Fatalf("unsupported websocket URL unexpectedly succeeded")
	}
	if got := idQuery(" vm one "); got != "?id=vm+one" {
		t.Fatalf("idQuery = %q", got)
	}
	if got := idQuery(" "); got != "" {
		t.Fatalf("blank idQuery = %q", got)
	}
}

func writeTestJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
