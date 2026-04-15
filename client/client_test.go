package client

import (
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
