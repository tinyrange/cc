package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/net/websocket"
)

func TestWebSocketExecCompletionStopsOpenInputSender(t *testing.T) {
	mux := http.NewServeMux()
	mux.Handle("/vm/run/stream", websocket.Server{
		Handshake: func(*websocket.Config, *http.Request) error { return nil },
		Handler: func(ws *websocket.Conn) {
			var req RunRequest
			if err := websocket.JSON.Receive(ws, &req); err != nil {
				t.Errorf("receive request: %v", err)
				return
			}
			if err := websocket.JSON.Send(ws, ExecEvent{Kind: "exit"}); err != nil {
				t.Errorf("send exit: %v", err)
			}
		},
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	client := &Client{url: srv.URL, client: *srv.Client()}

	for i := 0; i < 10; i++ {
		inputs := make(chan ExecInput)
		if err := client.RunInteractiveStreamContext(t.Context(), RunRequest{Command: []string{"true"}}, inputs, nil); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
		select {
		case inputs <- ExecInput{Kind: "stdin", Input: "after exit"}:
			t.Fatalf("run %d input sender still receives after completion", i)
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func TestWebSocketExecCancellationStopsInputSender(t *testing.T) {
	mux := http.NewServeMux()
	mux.Handle("/vm/run/stream", websocket.Server{
		Handshake: func(*websocket.Config, *http.Request) error { return nil },
		Handler: func(ws *websocket.Conn) {
			var req RunRequest
			if err := websocket.JSON.Receive(ws, &req); err != nil {
				return
			}
			var input ExecInput
			_ = websocket.JSON.Receive(ws, &input)
		},
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	client := &Client{url: srv.URL, client: *srv.Client()}
	inputs := make(chan ExecInput)
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	err := client.RunInteractiveStreamContext(ctx, RunRequest{Command: []string{"sleep"}}, inputs, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancellation error = %v, want %v", err, context.DeadlineExceeded)
	}
	select {
	case inputs <- ExecInput{Kind: "stdin", Input: "after cancellation"}:
		t.Fatal("input sender still receives after cancellation")
	case <-time.After(20 * time.Millisecond):
	}
}

func TestWebSocketInputSenderReportsSendFailure(t *testing.T) {
	mux := http.NewServeMux()
	mux.Handle("/ws", websocket.Server{
		Handshake: func(*websocket.Config, *http.Request) error { return nil },
		Handler: func(ws *websocket.Conn) {
			var input ExecInput
			_ = websocket.JSON.Receive(ws, &input)
		},
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	wsURL, err := websocketURL(srv.URL, "/ws")
	if err != nil {
		t.Fatalf("WebSocket URL: %v", err)
	}
	ws, err := websocket.Dial(wsURL, "", srv.URL)
	if err != nil {
		t.Fatalf("dial WebSocket: %v", err)
	}
	if err := ws.Close(); err != nil {
		t.Fatalf("close WebSocket: %v", err)
	}
	inputs := make(chan ExecInput, 1)
	inputs <- ExecInput{Kind: "stdin", Input: "cannot send"}

	select {
	case err := <-streamExecInputsToWebSocket(t.Context(), ws, inputs):
		if err == nil {
			t.Fatal("send failure was not returned")
		}
	case <-time.After(time.Second):
		t.Fatal("input sender did not return")
	}
}
