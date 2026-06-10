package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
