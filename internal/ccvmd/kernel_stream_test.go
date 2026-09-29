package ccvmd

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/kernel/alpine"
	"j5.nz/cc/internal/vm"
)

func TestKernelDownloadStreamCachedAndError(t *testing.T) {
	for _, cached := range []bool{true, false} {
		name := "error"
		if cached {
			name = "cached"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if cached {
				pkg, symvers := filepath.Join(root, "kernel.tar"), filepath.Join(root, "Module.symvers")
				for _, path := range []string{pkg, symvers} {
					if err := os.WriteFile(path, nil, 0o600); err != nil {
						t.Fatal(err)
					}
				}
				meta, err := json.Marshal(map[string]string{"version": "test", "source": "alpine:test", "package_file": pkg, "module_symvers_file": symvers})
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, "kernel.json"), meta, 0o600); err != nil {
					t.Fatal(err)
				}
			} else {
				// Fail locally before attempting any network download.
				root = filepath.Join(root, "not-a-directory")
				if err := os.WriteFile(root, nil, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			manager := alpine.NewManager(root)
			if cached && manager.Status().Status != "downloaded" {
				t.Fatalf("invalid local cache fixture: %+v", manager.Status())
			}
			srv := httptest.NewServer(newMux(&server{kernel: manager, vms: vm.NewManagerWithHost(nil)}, nil, func() {}, ServerOptions{}))
			defer srv.Close()
			api := client.NewClient(srv.URL, nil)
			for attempt := 0; attempt < 2; attempt++ {
				var events []client.ProgressEvent
				err := api.DownloadKernelStreamContext(t.Context(), client.DownloadRequest{}, func(event client.ProgressEvent) error {
					events = append(events, event)
					return nil
				})
				if (err == nil) != cached {
					t.Fatalf("cached=%t: error=%v", cached, err)
				}
				want := "error"
				if cached {
					want = "downloaded"
				}
				if len(events) != 1 || events[0].Status != want || events[0].Blob != "" {
					t.Fatalf("terminal events = %+v, want one %s", events, want)
				}
				if cached && manager.Status().Status != "downloaded" {
					t.Fatalf("kernel not ready: %+v", manager.Status())
				}
			}
		})
	}
}

func TestProgressEventsDoNotRepeatResponseHeaders(t *testing.T) {
	var diagnostics bytes.Buffer
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		for _, status := range []string{"downloading", "downloaded"} {
			if err := writeProgressEvent(w, client.ProgressEvent{Status: status}); err != nil {
				t.Error(err)
			}
		}
	}))
	srv.Config.ErrorLog = log.New(&diagnostics, "", 0)
	srv.Start()
	defer srv.Close()
	api := client.NewClient(srv.URL, nil)
	if err := api.DownloadKernelStreamContext(t.Context(), client.DownloadRequest{}, nil); err != nil {
		t.Fatal(err)
	}
	// Close waits for the handler before inspecting its log.
	srv.Close()
	if strings.Contains(diagnostics.String(), "superfluous") {
		t.Fatalf("HTTP diagnostics: %s", &diagnostics)
	}
}
