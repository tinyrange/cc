package client

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"golang.org/x/net/websocket"
)

func TestHTTPAndWebSocketUseConfiguredDialer(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.Handle("/vm/run/stream", websocket.Server{
		Handshake: func(*websocket.Config, *http.Request) error { return nil },
		Handler: func(ws *websocket.Conn) {
			var req RunRequest
			if err := websocket.JSON.Receive(ws, &req); err != nil {
				t.Errorf("receive request: %v", err)
				return
			}
			var input ExecInput
			if err := websocket.JSON.Receive(ws, &input); err != nil {
				t.Errorf("receive stdin close: %v", err)
				return
			}
			if err := websocket.JSON.Send(ws, ExecEvent{Kind: "exit"}); err != nil {
				t.Errorf("send exit: %v", err)
			}
		},
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	serverAddress := srv.Listener.Addr().String()
	for _, test := range []struct {
		name string
		new  func(*atomic.Uint64) *Client
	}{
		{
			name: "context dialer",
			new: func(dials *atomic.Uint64) *Client {
				return NewClientContext("http://configured.invalid", func(ctx context.Context) (net.Conn, error) {
					dials.Add(1)
					return (&net.Dialer{}).DialContext(ctx, "tcp", serverAddress)
				})
			},
		},
		{
			name: "legacy dialer",
			new: func(dials *atomic.Uint64) *Client {
				return NewClient("http://configured.invalid", func() (net.Conn, error) {
					dials.Add(1)
					return net.Dial("tcp", serverAddress)
				})
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var dials atomic.Uint64
			client := test.new(&dials)
			if err := client.HealthCheck(); err != nil {
				t.Fatalf("HTTP health check: %v", err)
			}
			if err := client.RunInteractiveStreamContext(t.Context(), RunRequest{Command: []string{"true"}}, nil, nil); err != nil {
				t.Fatalf("WebSocket exec: %v", err)
			}
			if got := dials.Load(); got != 2 {
				t.Fatalf("configured dial count = %d, want 2", got)
			}
		})
	}
}
