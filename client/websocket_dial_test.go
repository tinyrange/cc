package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestWebSocketHandshakeHonorsContextCancellation(t *testing.T) {
	started := make(chan string, 2)
	finished := make(chan string, 2)
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		started <- r.URL.Path
		<-r.Context().Done()
		finished <- r.URL.Path
	}))
	defer srv.Close()
	client := &Client{url: srv.URL, client: *srv.Client()}

	for _, test := range []struct {
		name string
		path string
		run  func(context.Context) error
	}{
		{
			name: "interactive run",
			path: "/vm/run/stream",
			run: func(ctx context.Context) error {
				return client.RunInteractiveStreamContext(ctx, RunRequest{Command: []string{"true"}}, nil, nil)
			},
		},
		{
			name: "exec",
			path: "/vm/run",
			run: func(ctx context.Context) error {
				return client.ExecStreamContext(ctx, ExecRequest{Command: []string{"true"}}, nil, nil)
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			result := make(chan error, 1)
			go func() {
				result <- test.run(ctx)
			}()
			select {
			case path := <-started:
				if path != test.path {
					t.Fatalf("handshake path = %q, want %q", path, test.path)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("handshake did not start")
			}
			cancel()
			var err error
			select {
			case err = <-result:
			case <-time.After(2 * time.Second):
				t.Fatal("canceled handshake did not return")
			}
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("handshake error = %v, want %v", err, context.Canceled)
			}
			var dialErr *WebSocketDialError
			if !errors.As(err, &dialErr) {
				t.Fatalf("handshake error type = %T, want *WebSocketDialError", err)
			}
			wantEndpoint, err := websocketURL(srv.URL, test.path)
			if err != nil {
				t.Fatalf("WebSocket URL: %v", err)
			}
			if dialErr.Endpoint != wantEndpoint {
				t.Fatalf("dial endpoint = %q, want %q", dialErr.Endpoint, wantEndpoint)
			}
			select {
			case path := <-finished:
				if path != test.path {
					t.Fatalf("finished handshake path = %q, want %q", path, test.path)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("partial handshake connection was not closed")
			}
		})
	}
}
