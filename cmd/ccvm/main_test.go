package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/websocket"
	"j5.nz/cc/client"
)

func TestWantsExecEventStream(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/vm/run?stream=1", nil)
	if !wantsExecEventStream(req) {
		t.Fatal("wantsExecEventStream(query) = false, want true")
	}

	req = httptest.NewRequest(http.MethodPost, "/vm/run", nil)
	req.Header.Set("Accept", "application/x-ndjson")
	if !wantsExecEventStream(req) {
		t.Fatal("wantsExecEventStream(accept) = false, want true")
	}

	req = httptest.NewRequest(http.MethodPost, "/vm/run", nil)
	if wantsExecEventStream(req) {
		t.Fatal("wantsExecEventStream(default) = true, want false")
	}
}

func TestWriteExecEventStream(t *testing.T) {
	rec := httptest.NewRecorder()
	writeExecEventStream(rec, client.ExecResponse{ExitCode: 7, Output: "hello"})

	if got := rec.Header().Get("Content-Type"); got != "application/x-ndjson" {
		t.Fatalf("Content-Type = %q, want application/x-ndjson", got)
	}

	lines := strings.Split(strings.TrimSpace(rec.Body.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("event line count = %d, want 2", len(lines))
	}

	var outputEvent client.ExecEvent
	if err := json.Unmarshal([]byte(lines[0]), &outputEvent); err != nil {
		t.Fatalf("Unmarshal(output event) error = %v", err)
	}
	if outputEvent.Kind != "output" || outputEvent.Output != "hello" {
		t.Fatalf("output event = %#v, want kind=output output=hello", outputEvent)
	}

	var exitEvent client.ExecEvent
	if err := json.Unmarshal([]byte(lines[1]), &exitEvent); err != nil {
		t.Fatalf("Unmarshal(exit event) error = %v", err)
	}
	if exitEvent.Kind != "exit" || exitEvent.ExitCode != 7 {
		t.Fatalf("exit event = %#v, want kind=exit exit_code=7", exitEvent)
	}
}

func TestServeRunWebSocket(t *testing.T) {
	ts := httptest.NewServer(websocket.Server{
		Handshake: func(*websocket.Config, *http.Request) error { return nil },
		Handler: func(ws *websocket.Conn) {
			serveRunWebSocket(ws, func(_ context.Context, req client.ExecRequest, inputs <-chan client.ExecInput, onEvent func(client.ExecEvent) error) error {
				if got := strings.Join(req.Command, " "); got != "echo hi" {
					t.Fatalf("command = %q, want %q", got, "echo hi")
				}
				var gotInput client.ExecInput
				select {
				case gotInput = <-inputs:
				case <-time.After(time.Second):
					t.Fatal("timed out waiting for stdin input")
				}
				if gotInput.Kind != "stdin" || string(gotInput.Data) != "yo" {
					t.Fatalf("input = %#v, want stdin yo", gotInput)
				}
				if err := onEvent(client.ExecEvent{Kind: "stdout", Stream: "stdout", Output: "hi", Data: []byte("hi")}); err != nil {
					return err
				}
				return onEvent(client.ExecEvent{Kind: "exit", ExitCode: 0})
			})
		},
	})
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")
	ws, err := websocket.Dial(wsURL, "", ts.URL)
	if err != nil {
		t.Fatalf("websocket.Dial() error = %v", err)
	}
	defer ws.Close()

	if err := websocket.JSON.Send(ws, client.ExecRequest{Command: []string{"echo", "hi"}}); err != nil {
		t.Fatalf("JSON.Send(req) error = %v", err)
	}
	if err := websocket.JSON.Send(ws, client.ExecInput{Kind: "stdin", Data: []byte("yo")}); err != nil {
		t.Fatalf("JSON.Send(input) error = %v", err)
	}

	var outputEvent client.ExecEvent
	if err := websocket.JSON.Receive(ws, &outputEvent); err != nil {
		t.Fatalf("JSON.Receive(output) error = %v", err)
	}
	if outputEvent.Kind != "stdout" || outputEvent.Output != "hi" {
		t.Fatalf("output event = %#v, want output hi", outputEvent)
	}

	var exitEvent client.ExecEvent
	if err := websocket.JSON.Receive(ws, &exitEvent); err != nil {
		t.Fatalf("JSON.Receive(exit) error = %v", err)
	}
	if exitEvent.Kind != "exit" || exitEvent.ExitCode != 0 {
		t.Fatalf("exit event = %#v, want exit 0", exitEvent)
	}
}
