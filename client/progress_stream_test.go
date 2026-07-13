package client

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestProgressStreamRequiresTerminalSuccess(t *testing.T) {
	for _, test := range []struct {
		name          string
		events        []ProgressEvent
		wantStatuses  []string
		wantPremature bool
		wantError     bool
	}{
		{
			name:          "truncated after progress",
			events:        []ProgressEvent{{Status: "downloading", Artifact: "kernel"}},
			wantStatuses:  []string{"downloading"},
			wantPremature: true,
		},
		{
			name:          "sub-artifact completion is not terminal",
			events:        []ProgressEvent{{Status: "downloaded", Artifact: "image", Blob: "cvmfs cache"}},
			wantStatuses:  []string{"downloaded"},
			wantPremature: true,
		},
		{
			name: "terminal success",
			events: []ProgressEvent{
				{Status: "downloading", Artifact: "image"},
				{Status: "downloaded", Artifact: "image"},
			},
			wantStatuses: []string{"downloading", "downloaded"},
		},
		{
			name:         "terminal error",
			events:       []ProgressEvent{{Status: "error", Error: "pull failed"}},
			wantStatuses: []string{"error"},
			wantError:    true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/x-ndjson")
				enc := json.NewEncoder(w)
				for _, event := range test.events {
					if err := enc.Encode(event); err != nil {
						t.Errorf("write event: %v", err)
						return
					}
				}
			}))
			defer srv.Close()

			client := &Client{url: srv.URL, client: *srv.Client()}
			var statuses []string
			err := client.postJSONProgressStreamContext(t.Context(), "/progress", struct{}{}, func(event ProgressEvent) error {
				statuses = append(statuses, event.Status)
				return nil
			}, progressStatusDownloaded)
			if got := errors.Is(err, ErrProgressStreamEndedBeforeTerminal); got != test.wantPremature {
				t.Fatalf("premature error = %t, want %t (error: %v)", got, test.wantPremature, err)
			}
			if (err != nil) != (test.wantPremature || test.wantError) {
				t.Fatalf("error = %v", err)
			}
			if !reflect.DeepEqual(statuses, test.wantStatuses) {
				t.Fatalf("callback statuses = %v, want %v", statuses, test.wantStatuses)
			}
		})
	}
}
