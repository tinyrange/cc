package client

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"golang.org/x/net/websocket"
)

func TestNDJSONExecStreamTerminalProtocol(t *testing.T) {
	testExecStreamTerminalProtocol(t, func(t *testing.T, events []ExecEvent, onEvent func(ExecEvent) error) error {
		t.Helper()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/x-ndjson")
			enc := json.NewEncoder(w)
			for _, event := range events {
				if err := enc.Encode(event); err != nil {
					t.Errorf("write event: %v", err)
					return
				}
			}
		}))
		defer srv.Close()
		client := &Client{url: srv.URL, client: *srv.Client()}
		return client.postJSONExecStream(t.Context(), "/exec", struct{}{}, onEvent)
	})
}

func TestWebSocketExecStreamTerminalProtocol(t *testing.T) {
	testExecStreamTerminalProtocol(t, func(t *testing.T, events []ExecEvent, onEvent func(ExecEvent) error) error {
		t.Helper()
		mux := http.NewServeMux()
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
				for _, event := range events {
					if err := websocket.JSON.Send(ws, event); err != nil {
						t.Errorf("send event: %v", err)
						return
					}
				}
			},
		})
		srv := httptest.NewServer(mux)
		defer srv.Close()
		client := &Client{url: srv.URL, client: *srv.Client()}
		return client.RunInteractiveStreamContext(t.Context(), RunRequest{Command: []string{"true"}}, nil, onEvent)
	})
}

func testExecStreamTerminalProtocol(t *testing.T, run func(*testing.T, []ExecEvent, func(ExecEvent) error) error) {
	t.Helper()
	for _, test := range []struct {
		name          string
		events        []ExecEvent
		wantKinds     []string
		wantPremature bool
		wantDuplicate bool
		wantError     bool
	}{
		{
			name:          "premature EOF",
			events:        []ExecEvent{{Kind: "stdout", Output: "partial"}},
			wantKinds:     []string{"stdout"},
			wantPremature: true,
		},
		{
			name:      "nonzero exit is terminal",
			events:    []ExecEvent{{Kind: "stdout", Output: "done"}, {Kind: "exit", ExitCode: 7}},
			wantKinds: []string{"stdout", "exit"},
		},
		{
			name:          "duplicate exit",
			events:        []ExecEvent{{Kind: "exit"}, {Kind: "exit"}},
			wantKinds:     []string{"exit"},
			wantDuplicate: true,
		},
		{
			name:          "error then exit",
			events:        []ExecEvent{{Kind: "error", Error: "failed"}, {Kind: "exit"}},
			wantKinds:     []string{"error"},
			wantDuplicate: true,
		},
		{
			name:      "terminal error",
			events:    []ExecEvent{{Kind: "error", Error: "failed"}},
			wantKinds: []string{"error"},
			wantError: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var kinds []string
			err := run(t, test.events, func(event ExecEvent) error {
				kinds = append(kinds, event.Kind)
				return nil
			})
			if got := errors.Is(err, ErrExecStreamEndedBeforeTerminal); got != test.wantPremature {
				t.Fatalf("premature error = %t, want %t (error: %v)", got, test.wantPremature, err)
			}
			if got := errors.Is(err, ErrExecStreamDuplicateTerminal); got != test.wantDuplicate {
				t.Fatalf("duplicate error = %t, want %t (error: %v)", got, test.wantDuplicate, err)
			}
			if (err != nil) != (test.wantPremature || test.wantDuplicate || test.wantError) {
				t.Fatalf("error = %v", err)
			}
			if !reflect.DeepEqual(kinds, test.wantKinds) {
				t.Fatalf("callback kinds = %v, want %v", kinds, test.wantKinds)
			}
		})
	}
}
