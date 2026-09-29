package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"j5.nz/cc/client"
)

// Re-execute the test binary as either the short-lived CLI or its long-lived
// daemon so the regression exercises actual inherited OS pipe handles.
func TestMain(m *testing.M) {
	if os.Getenv("CC_TEST_DAEMON_HELPER") == "1" && len(os.Args) > 1 && os.Args[1] == "-cache-dir" {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			panic(err)
		}
		server := &http.Server{ReadHeaderTimeout: time.Second}
		mux := http.NewServeMux()
		mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprintln(os.Stderr, "daemon health check")
			fmt.Fprintln(w, `{}`)
		})
		mux.HandleFunc("/shutdown", func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprintln(w, `{}`)
			go server.Shutdown(context.Background())
		})
		server.Handler = mux
		// Bound the helper lifetime even if its parent fails before cleanup.
		time.AfterFunc(30*time.Second, func() { server.Close() })
		if err := json.NewEncoder(os.Stdout).Encode(client.ServerHello{Addr: listener.Addr().String(), Scheme: "http"}); err != nil {
			panic(err)
		}
		_ = server.Serve(listener)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestConnectBackendDoesNotKeepCallerPipesOpen(t *testing.T) {
	if os.Getenv("CC_TEST_DAEMON_HELPER") == "1" {
		cache := os.Getenv("CC_TEST_DAEMON_CACHE")
		if _, err := connectBackend(os.Args[0], cache, filepath.Join(cache, "ccvm.json")); err != nil {
			t.Fatal(err)
		}
		return
	}
	cache := t.TempDir()
	statePath := filepath.Join(cache, "ccvm.json")
	t.Cleanup(func() {
		if state, err := readDaemonState(statePath); err == nil {
			if err := newClient(state.Addr).Shutdown(); err != nil {
				t.Errorf("shutdown helper: %v", err)
			}
		}
	})
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestConnectBackendDoesNotKeepCallerPipesOpen$")
	cmd.Env = append(os.Environ(), "CC_TEST_DAEMON_HELPER=1", "CC_TEST_DAEMON_CACHE="+cache)
	cmd.WaitDelay = time.Second
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("captured CLI failed: %v\n%s", err, output)
	}
	state, err := readDaemonState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := newClient(state.Addr).HealthCheck(); err != nil {
		t.Fatalf("daemon did not survive CLI exit: %v", err)
	}
	log, err := os.ReadFile(filepath.Join(cache, "ccvm.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(log), "daemon health check") {
		t.Fatalf("daemon log = %q", log)
	}
}
