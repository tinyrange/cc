package client

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestControlRequestsHonorCancellation(t *testing.T) {
	tests := []struct {
		name string
		call func(context.Context, *Client) error
	}{
		{
			name: "get",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.InstanceStatusContext(ctx)
				return err
			},
		},
		{
			name: "post",
			call: func(ctx context.Context, c *Client) error {
				return c.ShutdownInstanceContext(ctx)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			started := make(chan struct{})
			handlerDone := make(chan struct{})
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(started)
				select {
				case <-r.Context().Done():
				case <-time.After(2 * time.Second):
					http.Error(w, "request was not canceled", http.StatusInternalServerError)
				}
				close(handlerDone)
			}))
			defer srv.Close()

			ctx, cancel := context.WithCancel(t.Context())
			result := make(chan error, 1)
			c := &Client{url: srv.URL, client: *srv.Client()}
			go func() {
				result <- tt.call(ctx, c)
			}()

			select {
			case <-started:
			case <-time.After(2 * time.Second):
				t.Fatal("request did not reach server")
			}
			cancel()

			select {
			case err := <-result:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("request error = %v, want context cancellation", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("request did not return after cancellation")
			}
			select {
			case <-handlerDone:
			case <-time.After(2 * time.Second):
				t.Fatal("server did not observe request cancellation")
			}
		})
	}
}

func TestClientDialHonorsRequestContext(t *testing.T) {
	dialStarted := make(chan struct{})
	c := NewClientContext("http://cc.invalid", func(ctx context.Context) (net.Conn, error) {
		close(dialStarted)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan error, 1)
	go func() {
		result <- c.HealthCheckContext(ctx)
	}()

	select {
	case <-dialStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("client did not start dialing")
	}
	cancel()

	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("dial error = %v, want context cancellation", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("dial did not return after cancellation")
	}
}

func TestProgressStreamUsesCallerLifetime(t *testing.T) {
	handlerDone := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{\"status\":\"downloading\",\"bytes_downloaded\":1}\n"))
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
		close(handlerDone)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(t.Context())
	c := &Client{url: srv.URL, client: *srv.Client()}
	var got ProgressEvent
	err := c.PullImageStreamContext(ctx, "image", PullImageRequest{}, func(event ProgressEvent) error {
		got = event
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("stream error = %v, want context cancellation", err)
	}
	if got.Status != "downloading" || got.BytesDownloaded != 1 {
		t.Fatalf("progress event = %+v", got)
	}
	select {
	case <-handlerDone:
	case <-time.After(2 * time.Second):
		t.Fatal("server did not observe stream cancellation")
	}
}
